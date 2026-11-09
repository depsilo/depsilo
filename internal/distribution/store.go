package distribution

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store is the single instance-owned writer for this catalog and its dedicated
// storage. Its semaphore coordinates upload, verification, activation and GC;
// waiters can cancel. Database read operations do not block behind an upload.
type Store struct {
	database *gorm.DB
	storage  cache.Storage
	limits   Limits
	gate     chan struct{}
	keys     TransferKeys
}

func NewStore(database *gorm.DB, storage cache.Storage, limits Limits) (*Store, error) {
	if database == nil || storage == nil || limits.MaxObjectBytes <= 0 || limits.MaxObjectBytes == math.MaxInt64 || limits.MaxStorageBytes < limits.MaxObjectBytes {
		return nil, fmt.Errorf("%w: store dependencies or limits", ErrInvalid)
	}
	return &Store{database: database, storage: storage, limits: limits, gate: make(chan struct{}, 1)}, nil
}

// ConfigureTransfer installs startup-loaded transfer keys. Call it before the
// store serves requests; trust changes are restart-managed configuration.
func (s *Store) ConfigureTransfer(keys TransferKeys) {
	s.keys = TransferKeys{Signer: keys.Signer, Trusted: append([]ed25519.PublicKey(nil), keys.Trusted...)}
}

func (s *Store) lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			s.unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Store) unlock() { <-s.gate }

func objectKey(digest string) string { return "objects/sha256/" + digest[:2] + "/" + digest }

func (s *Store) Stage(ctx context.Context, raw []byte, actor string) (*Set, error) {
	m, err := ParseManifest(raw)
	if err != nil {
		return nil, err
	}
	var declared int64
	for _, o := range m.Objects {
		if o.Size > s.limits.MaxObjectBytes || o.Size > s.limits.MaxStorageBytes-declared {
			return nil, ErrQuota
		}
		declared += o.Size
	}
	if len(actor) > 128 {
		return nil, fmt.Errorf("%w: actor", ErrInvalid)
	}
	if err = s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	sum := sha256.Sum256(raw)
	id := hex.EncodeToString(sum[:])
	var existing db.DistributionSet
	err = s.database.WithContext(ctx).First(&existing, "id = ? OR (name = ? AND revision = ?)", id, m.Name, m.Revision).Error
	if err == nil {
		if existing.ID != id || !bytes.Equal(existing.Manifest, raw) {
			return nil, ErrConflict
		}
		return s.view(ctx, existing)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	record := db.DistributionSet{ID: id, Name: m.Name, Revision: m.Revision, Manifest: append([]byte(nil), raw...), CreatedBy: actor}
	err = s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, o := range m.Objects {
			var conflict int64
			if err := tx.Model(&db.DistributionSetObject{}).Where("digest = ? AND size <> ?", o.SHA256, o.Size).Count(&conflict).Error; err != nil {
				return err
			}
			if conflict > 0 {
				return ErrConflict
			}
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		refs := make([]db.DistributionSetObject, 0, len(m.Objects))
		for _, o := range m.Objects {
			refs = append(refs, db.DistributionSetObject{SetID: id, Digest: o.SHA256, Size: o.Size})
		}
		return tx.CreateInBatches(refs, 100).Error
	})
	if err != nil {
		return nil, err
	}
	return setView(record, nil)
}

func (s *Store) view(ctx context.Context, record db.DistributionSet) (*Set, error) {
	var rows []db.DistributionSetProvenance
	if err := s.database.WithContext(ctx).Where("set_id = ?", record.ID).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return setView(record, nil)
	}
	return setView(record, &rows[0])
}

func setView(record db.DistributionSet, provenance *db.DistributionSetProvenance) (*Set, error) {
	m, err := ParseManifest(record.Manifest)
	if err != nil {
		return nil, fmt.Errorf("stored manifest: %w", err)
	}
	sum := sha256.Sum256(record.Manifest)
	if hex.EncodeToString(sum[:]) != record.ID || m.Name != record.Name || m.Revision != record.Revision {
		return nil, ErrIntegrity
	}
	status := "staged"
	if record.VerifiedAt != nil {
		status = "content_verified"
	} else if record.CheckedAt != nil {
		status = "incomplete"
	}
	view := &Set{ID: record.ID, Name: record.Name, Revision: record.Revision, CreatedAt: record.CreatedAt, VerifiedAt: record.VerifiedAt, CheckedAt: record.CheckedAt, Manifest: m, ContentStatus: status, ProvenanceStatus: "operator_declared", InstallValidation: "not_run", Serving: false}
	if provenance != nil {
		view.ProvenanceStatus = "preparer_attested"
		view.Provenance = &Provenance{Origin: provenance.Origin, SignerKeyID: provenance.SignerKeyID, ImportedBy: provenance.ImportedBy, ImportedAt: provenance.ImportedAt}
	}
	return view, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Set, error) {
	record, err := s.record(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.view(ctx, record)
}

func (s *Store) record(ctx context.Context, id string) (db.DistributionSet, error) {
	var record db.DistributionSet
	if !validDigest(id) {
		return record, ErrNotFound
	}
	if err := s.database.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return record, ErrNotFound
		}
		return record, err
	}
	return record, nil
}

func (s *Store) List(ctx context.Context, limit, offset int) ([]Set, int64, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, 0, ErrInvalid
	}
	var total int64
	if err := s.database.WithContext(ctx).Model(&db.DistributionSet{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []db.DistributionSet
	if err := s.database.WithContext(ctx).Order("created_at DESC, id").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	var provenance []db.DistributionSetProvenance
	if len(ids) > 0 {
		if err := s.database.WithContext(ctx).Where("set_id IN ?", ids).Find(&provenance).Error; err != nil {
			return nil, 0, err
		}
	}
	byID := make(map[string]*db.DistributionSetProvenance, len(provenance))
	for i := range provenance {
		byID[provenance[i].SetID] = &provenance[i]
	}
	sets := make([]Set, 0, len(rows))
	for _, row := range rows {
		view, err := setView(row, byID[row.ID])
		if err != nil {
			return nil, 0, err
		}
		sets = append(sets, *view)
	}
	return sets, total, nil
}

func (s *Store) Put(ctx context.Context, id, digest string, reader io.Reader) (*UploadResult, error) {
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	set, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var wanted *Object
	for i := range set.Manifest.Objects {
		if set.Manifest.Objects[i].SHA256 == digest {
			wanted = &set.Manifest.Objects[i]
			break
		}
	}
	if wanted == nil {
		return nil, ErrNotFound
	}
	if reader == nil {
		return nil, ErrInvalid
	}
	o := *wanted
	// Configuration can be tightened after a draft was staged. Existing bytes
	// remain inspectable, but a new upload must obey the current object limit.
	if o.Size > s.limits.MaxObjectBytes {
		return nil, ErrQuota
	}
	key := objectKey(o.SHA256)
	issue, err := s.checkObject(ctx, o)
	if err != nil {
		return nil, err
	}
	reused := issue == ""
	v := &checkedReader{ctx: ctx, reader: reader, want: o, hash: sha256.New()}
	if reused {
		_, err = io.Copy(io.Discard, v)
	} else {
		used, sizeErr := s.storage.TotalSize(ctx)
		if sizeErr != nil {
			return nil, sizeErr
		}
		if issue != "missing" {
			meta, statErr := s.storage.Stat(ctx, key)
			if statErr != nil {
				return nil, statErr
			}
			used -= meta.Size
		}
		if used < 0 || o.Size > s.limits.MaxStorageBytes-used {
			return nil, ErrQuota
		}
		// Unknown size forces both local and multipart storage to consume the
		// validating reader through EOF. Invalid bytes never commit as an object.
		err = s.storage.Put(ctx, key, v, -1, "application/octet-stream")
	}
	if err != nil {
		return nil, err
	}
	if !v.complete {
		return nil, ErrIntegrity
	}
	now := time.Now().UTC()
	record := db.DistributionObject{Digest: digest, Size: o.Size, StorageKey: key, VerifiedAt: now}
	if err := s.database.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "digest"}}, DoUpdates: clause.AssignmentColumns([]string{"size", "storage_key", "verified_at"})}).Create(&record).Error; err != nil {
		return nil, err
	}
	return &UploadResult{SHA256: digest, Size: o.Size, Reused: reused}, nil
}

// checkedReader yields EOF only after verifying the exact length and digest.
// At most one excess byte is read, bounding malicious or mistaken uploads.
type checkedReader struct {
	ctx      context.Context
	reader   io.Reader
	want     Object
	hash     hash.Hash
	read     int64
	complete bool
}

func (v *checkedReader) Read(p []byte) (int, error) {
	if err := v.ctx.Err(); err != nil {
		return 0, err
	}
	if v.complete {
		return 0, io.EOF
	}
	remaining := v.want.Size - v.read + 1
	if int64(len(p)) > remaining {
		p = p[:int(remaining)]
	}
	n, err := v.reader.Read(p)
	v.read += int64(n)
	_, _ = v.hash.Write(p[:n])
	if v.read > v.want.Size {
		return n, ErrIntegrity
	}
	if err == io.EOF {
		if v.read != v.want.Size || hex.EncodeToString(v.hash.Sum(nil)) != v.want.SHA256 {
			return n, ErrIntegrity
		}
		v.complete = true
	}
	if ctxErr := v.ctx.Err(); ctxErr != nil {
		return n, ctxErr
	}
	// S3's multipart reader uses io.ErrUnexpectedEOF to identify a normal
	// final short part. A producer's error must not impersonate that sentinel.
	if err != nil && err != io.EOF {
		return n, fmt.Errorf("read declared content: %w", err)
	}
	return n, err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (s *Store) checkObject(ctx context.Context, o Object) (string, error) {
	key := objectKey(o.SHA256)
	exists, err := s.storage.Exists(ctx, key)
	if err != nil {
		return "", err
	}
	if !exists {
		return "missing", nil
	}
	r, size, err := s.storage.Get(ctx, key)
	if err != nil {
		return "", err
	}
	defer r.Close()
	if size != o.Size {
		return "size_mismatch", nil
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx: ctx, reader: r}, o.Size+1))
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if n != o.Size {
		return "size_mismatch", nil
	}
	if hex.EncodeToString(h.Sum(nil)) != o.SHA256 {
		return "hash_mismatch", nil
	}
	return "", nil
}

func (s *Store) Verify(ctx context.Context, id string) (*Verification, error) {
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	return s.verify(ctx, id)
}

func (s *Store) verify(ctx context.Context, id string) (*Verification, error) {
	set, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	result := &Verification{SetID: id, ContentStatus: "content_verified", Issues: []Issue{}, ProvenanceStatus: set.ProvenanceStatus, InstallValidation: "not_run", Serving: false}
	for _, o := range set.Manifest.Objects {
		issue, err := s.checkObject(ctx, o)
		if err != nil {
			return nil, err
		}
		if issue != "" {
			result.Issues = append(result.Issues, Issue{SHA256: o.SHA256, Code: issue})
		} else {
			result.VerifiedObjects++
		}
	}
	result.CheckedAt = time.Now().UTC()
	updates := map[string]any{"checked_at": result.CheckedAt, "verified_at": result.CheckedAt}
	if len(result.Issues) > 0 {
		result.ContentStatus = "incomplete"
		updates["verified_at"] = nil
	}
	if err := s.database.WithContext(ctx).Model(&db.DistributionSet{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	return result, nil
}

// Activate selects a retained catalog revision after a fresh integrity check.
// It is deliberately not exposed by HTTP/CLI until offline serving and target
// policy enforcement exist. It does not change the ordinary proxy or Snapshot.
func (s *Store) Activate(ctx context.Context, id string) (*Set, error) {
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	report, err := s.verify(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(report.Issues) > 0 {
		return nil, ErrIncomplete
	}
	set, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	tx, release, err := db.BeginDurableTransaction(ctx, s.database)
	if err != nil {
		return nil, err
	}
	defer release()
	defer tx.Rollback()
	active := db.DistributionActiveSet{Name: set.Name, SetID: id, ActivatedAt: time.Now().UTC()}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "name"}}, DoUpdates: clause.AssignmentColumns([]string{"set_id", "activated_at"})}).Create(&active).Error; err != nil {
		return nil, err
	}
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	return set, nil
}

// Active rechecks durable content; a restored catalog with missing objects
// remains unavailable rather than silently disabling the selected version.
func (s *Store) Active(ctx context.Context, name string) (*Set, error) {
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	var active db.DistributionActiveSet
	if err := s.database.WithContext(ctx).First(&active, "name = ?", name).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	report, err := s.verify(ctx, active.SetID)
	if err != nil {
		return nil, err
	}
	if len(report.Issues) > 0 {
		return nil, ErrIncomplete
	}
	set, err := s.Get(ctx, active.SetID)
	if err == nil && set.Name != name {
		return nil, ErrIntegrity
	}
	return set, err
}

func (s *Store) Delete(ctx context.Context, id string) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&db.DistributionActiveSet{}).Where("set_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrActive
		}
		if err := tx.Where("set_id = ?", id).Delete(&db.DistributionSetObject{}).Error; err != nil {
			return err
		}
		if err := tx.Where("set_id = ?", id).Delete(&db.DistributionSetProvenance{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&db.DistributionSet{}).Error
	})
}

// Prune removes only unreferenced content. Upload and activation share this
// lock, so an in-progress operation cannot lose its object to collection.
// Catalog deletion and physical reclamation are separate, retryable actions.
func (s *Store) Prune(ctx context.Context) (int64, error) {
	if err := s.lock(ctx); err != nil {
		return 0, err
	}
	defer s.unlock()
	objects, err := s.storage.List(ctx, "objects/sha256")
	if err != nil {
		return 0, err
	}
	var removed int64
	if staging, ok := s.storage.(cache.StagingObjectStore); ok {
		items, err := staging.ListStaging(ctx)
		if err != nil {
			return 0, err
		}
		for _, item := range items {
			// A crash remainder is never a published content object, even if a
			// manifest still needs that digest. A later upload can recreate it.
			if !ownedObjectKey(item.Key) {
				continue
			}
			deleted, err := staging.RemoveStaging(ctx, item.Key)
			if err != nil {
				return removed, err
			}
			if deleted {
				removed += item.Size
			}
		}
	}
	for _, o := range objects {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if !ownedObjectKey(o.Key) {
			continue
		}
		digest := o.Key[len(o.Key)-64:]
		var refs int64
		if err := s.database.WithContext(ctx).Model(&db.DistributionSetObject{}).Where("digest = ?", digest).Count(&refs).Error; err != nil {
			return removed, err
		}
		if refs > 0 {
			continue
		}
		if err := s.storage.Delete(ctx, o.Key); err != nil {
			return removed, err
		}
		if err := s.database.WithContext(ctx).Where("digest = ?", digest).Delete(&db.DistributionObject{}).Error; err != nil {
			return removed, err
		}
		removed += o.Size
	}
	// Reconcile rows left behind if a prior process stopped after physical
	// deletion. Referenced rows are preserved even when their bytes are missing.
	err = s.database.WithContext(ctx).Where("NOT EXISTS (SELECT 1 FROM distribution_set_objects r WHERE r.digest = distribution_objects.digest)").Delete(&db.DistributionObject{}).Error
	return removed, err
}

func ownedObjectKey(key string) bool {
	if len(key) != len("objects/sha256/aa/")+64 {
		return false
	}
	digest := key[len(key)-64:]
	return validDigest(digest) && objectKey(digest) == key
}

func (s *Store) Usage(ctx context.Context) (*Usage, error) {
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	u := &Usage{QuotaBytes: s.limits.MaxStorageBytes}
	var err error
	u.Bytes, err = s.storage.TotalSize(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.database.WithContext(ctx).Model(&db.DistributionObject{}).Count(&u.Objects).Error; err != nil {
		return nil, err
	}
	if err = s.database.WithContext(ctx).Model(&db.DistributionSet{}).Count(&u.Sets).Error; err != nil {
		return nil, err
	}
	return u, nil
}
