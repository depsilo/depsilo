package distribution

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"depsilo/internal/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	BundleFormat    = "depsilo/transfer-bundle/v1"
	SignatureFormat = "depsilo/transfer-signature/v1"
	BundleKindFull  = "full"

	MaxTransferManifestBytes = 64 << 10
	MaxSignatureBytes        = 4 << 10

	memberTransfer     = "transfer.json"
	memberSignature    = "transfer.sig"
	memberSet          = "set.json"
	objectMemberPrefix = "objects/"

	// Every member is written as USTAR, whose 11 octal size digits cap one
	// member just below 8 GiB. A fixed format keeps the bundle length exactly
	// computable, so an interrupted HTTP export arrives as a short body instead
	// of an apparently clean end of stream.
	maxBundleObjectBytes int64 = 1<<33 - 1
	tarBlock                   = 512
	// Accept the zero padding conventional tar tools add up to one record.
	maxTrailingZeroBytes = 20 * tarBlock
)

var (
	ErrBundle             = errors.New("invalid transfer bundle")
	ErrUntrusted          = errors.New("transfer bundle signer is not trusted")
	ErrSigningUnavailable = errors.New("transfer signing key is not configured")
)

// TransferManifest is the signed description of one full transfer bundle. It
// binds the exact dependency-set manifest bytes through SetID; the object
// inventory itself is the set manifest's object list, in bundle order.
type TransferManifest struct {
	Format               string    `json:"format"`
	RequiredCapabilities []string  `json:"required_capabilities"`
	Kind                 string    `json:"kind"`
	SetID                string    `json:"set_id"`
	Name                 string    `json:"name"`
	Revision             string    `json:"revision"`
	SetManifestSize      int64     `json:"set_manifest_size"`
	ObjectCount          int       `json:"object_count"`
	TotalBytes           int64     `json:"total_bytes"`
	CreatedAt            time.Time `json:"created_at"`
}

// TransferSignature is a detached Ed25519 signature over the exact
// transfer.json bytes. KeyID selects a configured key; it never confers trust.
type TransferSignature struct {
	Format    string `json:"format"`
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
}

// BundleSummary describes a structurally valid bundle whose content matched
// its declarations. SignatureVerified is false when no trust decision was made.
type BundleSummary struct {
	SetID             string `json:"set_id"`
	Name              string `json:"name"`
	Revision          string `json:"revision"`
	SignerKeyID       string `json:"signer_key_id"`
	ObjectCount       int    `json:"object_count"`
	TotalBytes        int64  `json:"total_bytes"`
	BundleBytes       int64  `json:"bundle_bytes"`
	SignatureVerified bool   `json:"signature_verified"`
}

// ImportResult is returned with or without an error once the bundle's signed
// identity is known, so a failed import can still be inspected and retried.
type ImportResult struct {
	SetID          string        `json:"set_id"`
	Name           string        `json:"name"`
	Revision       string        `json:"revision"`
	SignerKeyID    string        `json:"signer_key_id"`
	ObjectCount    int           `json:"object_count"`
	WrittenObjects int           `json:"written_objects"`
	ReusedObjects  int           `json:"reused_objects"`
	Verification   *Verification `json:"verification,omitempty"`
	Set            *Set          `json:"set,omitempty"`
}

// decodeStrict is the shared strict-JSON rule for every distribution document:
// one top-level value, no duplicate or case-aliased keys, no unknown fields.
func decodeStrict(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSON(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(value)
}

func parseTransferManifest(raw []byte) (TransferManifest, error) {
	var m TransferManifest
	if err := decodeStrict(raw, &m); err != nil {
		return m, fmt.Errorf("%w: malformed transfer manifest", ErrBundle)
	}
	if m.Format != BundleFormat {
		return m, fmt.Errorf("%w: unsupported bundle format", ErrBundle)
	}
	if len(m.RequiredCapabilities) != 0 {
		return m, fmt.Errorf("%w: bundle requires unsupported capabilities", ErrBundle)
	}
	if m.Kind != BundleKindFull || !validDigest(m.SetID) || !identifier.MatchString(m.Name) || !identifier.MatchString(m.Revision) ||
		m.SetManifestSize <= 0 || m.SetManifestSize > MaxManifestBytes || m.ObjectCount <= 0 || m.ObjectCount > MaxObjects ||
		m.TotalBytes <= 0 || m.CreatedAt.IsZero() {
		return m, fmt.Errorf("%w: transfer manifest fields", ErrBundle)
	}
	return m, nil
}

func parseSignature(raw []byte) (TransferSignature, []byte, error) {
	var s TransferSignature
	if err := decodeStrict(raw, &s); err != nil {
		return s, nil, fmt.Errorf("%w: malformed signature", ErrBundle)
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(s.Signature)
	if s.Format != SignatureFormat || s.Algorithm != "ed25519" || !validDigest(s.KeyID) || err != nil || len(signature) != ed25519.SignatureSize {
		return s, nil, fmt.Errorf("%w: signature fields", ErrBundle)
	}
	return s, signature, nil
}

type bundleHeader struct {
	transferRaw, signatureRaw, setRaw []byte
	transfer                          TransferManifest
	signature                         TransferSignature
	manifest                          Manifest
}

// nextMember accepts only a regular USTAR-style member with the exact
// expected name. Links, devices, directories, PAX records (including global
// headers), sparse files and any reordering are rejected before payload use.
func nextMember(tr *tar.Reader, name string) (*tar.Header, error) {
	header, err := tr.Next()
	if err == io.EOF {
		return nil, fmt.Errorf("%w: missing member %s", ErrBundle, name)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: unreadable archive before %s", ErrBundle, name)
	}
	if header.Typeflag != tar.TypeReg || header.Name != name || header.Linkname != "" || len(header.PAXRecords) != 0 {
		return nil, fmt.Errorf("%w: expected regular member %s", ErrBundle, name)
	}
	return header, nil
}

func readMember(tr *tar.Reader, name string, limit int64) ([]byte, error) {
	header, err := nextMember(tr, name)
	if err != nil {
		return nil, err
	}
	if header.Size <= 0 || header.Size > limit {
		return nil, fmt.Errorf("%w: member %s size", ErrBundle, name)
	}
	raw := make([]byte, header.Size)
	if _, err := io.ReadFull(tr, raw); err != nil {
		return nil, fmt.Errorf("%w: truncated member %s", ErrBundle, name)
	}
	return raw, nil
}

// readBundleHeader authenticates transfer.json before parsing it further, then
// binds the exact set manifest bytes. No object payload has been read yet.
func readBundleHeader(tr *tar.Reader, verify func([]byte, TransferSignature, []byte) error) (*bundleHeader, error) {
	h := &bundleHeader{}
	var err error
	if h.transferRaw, err = readMember(tr, memberTransfer, MaxTransferManifestBytes); err != nil {
		return nil, err
	}
	if h.signatureRaw, err = readMember(tr, memberSignature, MaxSignatureBytes); err != nil {
		return nil, err
	}
	var signature []byte
	if h.signature, signature, err = parseSignature(h.signatureRaw); err != nil {
		return nil, err
	}
	if err = verify(h.transferRaw, h.signature, signature); err != nil {
		return nil, err
	}
	if h.transfer, err = parseTransferManifest(h.transferRaw); err != nil {
		return nil, err
	}
	if h.setRaw, err = readMember(tr, memberSet, MaxManifestBytes); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(h.setRaw)
	if int64(len(h.setRaw)) != h.transfer.SetManifestSize || hex.EncodeToString(sum[:]) != h.transfer.SetID {
		return nil, fmt.Errorf("%w: set manifest does not match the signed identity", ErrBundle)
	}
	if h.manifest, err = ParseManifest(h.setRaw); err != nil {
		return nil, fmt.Errorf("%w: set manifest: %v", ErrBundle, err)
	}
	var total int64
	for _, o := range h.manifest.Objects {
		if o.Size > maxBundleObjectBytes {
			return nil, fmt.Errorf("%w: object exceeds the bundle member limit", ErrBundle)
		}
		total += o.Size
	}
	if h.manifest.Name != h.transfer.Name || h.manifest.Revision != h.transfer.Revision ||
		len(h.manifest.Objects) != h.transfer.ObjectCount || total != h.transfer.TotalBytes {
		return nil, fmt.Errorf("%w: set manifest contradicts the transfer manifest", ErrBundle)
	}
	return h, nil
}

func nextObject(tr *tar.Reader, o Object) error {
	header, err := nextMember(tr, objectMemberPrefix+o.SHA256)
	if err != nil {
		return err
	}
	if header.Size != o.Size {
		return fmt.Errorf("%w: object member size differs from its declaration", ErrBundle)
	}
	return nil
}

// archiveBytes is the exact length of a conforming bundle through its
// end-of-archive marker: one header block per member plus padded payloads.
func (h *bundleHeader) archiveBytes() int64 {
	n := memberBytes(int64(len(h.transferRaw))) + memberBytes(int64(len(h.signatureRaw))) + memberBytes(int64(len(h.setRaw)))
	for _, o := range h.manifest.Objects {
		n += memberBytes(o.Size)
	}
	return n + 2*tarBlock
}

// finishBundle rejects undeclared members, a missing end-of-archive marker
// and trailing data. archive/tar reports a stream cut at a member boundary as
// a clean end, so the consumed length must also match the declared layout;
// this also rejects extra extended headers that would change the length.
func finishBundle(tr *tar.Reader, counter *countingReader, want int64) error {
	if _, err := tr.Next(); err != io.EOF {
		return fmt.Errorf("%w: undeclared member after the declared objects", ErrBundle)
	}
	if counter.n != want {
		return fmt.Errorf("%w: archive is truncated or has unexpected headers", ErrBundle)
	}
	trailing, err := io.ReadAll(io.LimitReader(counter, maxTrailingZeroBytes+1))
	if err != nil {
		return fmt.Errorf("%w: unreadable archive trailer", ErrBundle)
	}
	if len(trailing) > maxTrailingZeroBytes || bytes.IndexFunc(trailing, func(r rune) bool { return r != 0 }) >= 0 {
		return fmt.Errorf("%w: data after the archive end", ErrBundle)
	}
	return nil
}

type countingReader struct {
	reader io.Reader
	n      int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.n += int64(n)
	return n, err
}

// InspectBundle checks bundle structure and every object's length and SHA-256
// without a trust decision or any storage write. It lets an exporter confirm
// that the file it saved is complete; it does not authenticate the signer.
func InspectBundle(ctx context.Context, r io.Reader) (*BundleSummary, error) {
	counter := &countingReader{reader: contextReader{ctx: ctx, reader: r}}
	tr := tar.NewReader(counter)
	h, err := readBundleHeader(tr, func([]byte, TransferSignature, []byte) error { return nil })
	if err != nil {
		return nil, ctxOr(ctx, err)
	}
	for _, o := range h.manifest.Objects {
		if err := nextObject(tr, o); err != nil {
			return nil, ctxOr(ctx, err)
		}
		v := &checkedReader{ctx: ctx, reader: tr, want: o, hash: sha256.New()}
		if _, err := io.Copy(io.Discard, v); err != nil || !v.complete {
			return nil, ctxOr(ctx, ErrIntegrity)
		}
	}
	want := h.archiveBytes()
	if err := finishBundle(tr, counter, want); err != nil {
		return nil, ctxOr(ctx, err)
	}
	return &BundleSummary{SetID: h.transfer.SetID, Name: h.transfer.Name, Revision: h.transfer.Revision,
		SignerKeyID: h.signature.KeyID, ObjectCount: h.transfer.ObjectCount, TotalBytes: h.transfer.TotalBytes,
		BundleBytes: counter.n}, nil
}

func ctxOr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

// Export is a prepared, signed full bundle. Write streams it; Size is exact.
type Export struct {
	store    *Store
	manifest Manifest
	transfer TransferManifest
	members  [3]struct {
		name string
		data []byte
	}
	Size        int64
	SetID       string
	SignerKeyID string
	Filename    string
}

func memberBytes(size int64) int64 { return tarBlock + (size+tarBlock-1)/tarBlock*tarBlock }

// PrepareExport signs a full bundle for a content-verified set. It checks
// presence and sizes up front; hashes are verified again while streaming, so a
// later corruption aborts the stream rather than emitting unverified bytes.
// It does not hold the store lock while the caller streams.
func (s *Store) PrepareExport(ctx context.Context, id string) (*Export, error) {
	if s.keys.Signer == nil {
		return nil, ErrSigningUnavailable
	}
	record, err := s.record(ctx, id)
	if err != nil {
		return nil, err
	}
	set, err := s.view(ctx, record)
	if err != nil {
		return nil, err
	}
	if set.ContentStatus != "content_verified" {
		return nil, ErrIncomplete
	}
	var total int64
	for _, o := range set.Manifest.Objects {
		if o.Size > maxBundleObjectBytes {
			return nil, fmt.Errorf("%w: object exceeds the bundle member limit", ErrInvalid)
		}
		meta, err := s.storage.Stat(ctx, objectKey(o.SHA256))
		if err != nil || meta == nil || meta.Size != o.Size {
			return nil, ErrIncomplete
		}
		total += o.Size
	}
	transfer := TransferManifest{Format: BundleFormat, RequiredCapabilities: []string{}, Kind: BundleKindFull,
		SetID: record.ID, Name: record.Name, Revision: record.Revision, SetManifestSize: int64(len(record.Manifest)),
		ObjectCount: len(set.Manifest.Objects), TotalBytes: total, CreatedAt: time.Now().UTC().Truncate(time.Second)}
	transferRaw, err := json.MarshalIndent(transfer, "", "  ")
	if err != nil {
		return nil, err
	}
	transferRaw = append(transferRaw, '\n')
	public := s.keys.Signer.Public().(ed25519.PublicKey)
	signature := TransferSignature{Format: SignatureFormat, Algorithm: "ed25519", KeyID: KeyID(public),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(s.keys.Signer, transferRaw))}
	signatureRaw, err := json.MarshalIndent(signature, "", "  ")
	if err != nil {
		return nil, err
	}
	signatureRaw = append(signatureRaw, '\n')
	e := &Export{store: s, manifest: set.Manifest, transfer: transfer, SetID: record.ID, SignerKeyID: signature.KeyID,
		Filename: record.Name + "-" + record.Revision + ".depsilo-bundle.tar"}
	e.members[0].name, e.members[0].data = memberTransfer, transferRaw
	e.members[1].name, e.members[1].data = memberSignature, signatureRaw
	e.members[2].name, e.members[2].data = memberSet, append([]byte(nil), record.Manifest...)
	for _, m := range e.members {
		e.Size += memberBytes(int64(len(m.data)))
	}
	for _, o := range set.Manifest.Objects {
		e.Size += memberBytes(o.Size)
	}
	e.Size += 2 * tarBlock
	return e, nil
}

func (e *Export) header(name string, size int64) *tar.Header {
	return &tar.Header{Typeflag: tar.TypeReg, Name: name, Size: size, Mode: 0o644,
		ModTime: e.transfer.CreatedAt, Format: tar.FormatUSTAR}
}

// Write streams exactly e.Size bytes on success. Any error leaves a short or
// invalid archive, which InspectBundle and Import reject.
func (e *Export) Write(ctx context.Context, w io.Writer) error {
	tw := tar.NewWriter(w)
	for _, m := range e.members {
		if err := tw.WriteHeader(e.header(m.name, int64(len(m.data)))); err != nil {
			return err
		}
		if _, err := tw.Write(m.data); err != nil {
			return err
		}
	}
	for _, o := range e.manifest.Objects {
		if err := e.writeObject(ctx, tw, o); err != nil {
			return err
		}
	}
	return tw.Close()
}

func (e *Export) writeObject(ctx context.Context, tw *tar.Writer, o Object) error {
	r, size, err := e.store.storage.Get(ctx, objectKey(o.SHA256))
	if err != nil {
		return ctxOr(ctx, ErrIncomplete)
	}
	defer r.Close()
	if size != o.Size {
		return ErrIntegrity
	}
	if err := tw.WriteHeader(e.header(objectMemberPrefix+o.SHA256, o.Size)); err != nil {
		return err
	}
	v := &checkedReader{ctx: ctx, reader: r, want: o, hash: sha256.New()}
	if _, err := io.Copy(tw, v); err != nil {
		return ctxOr(ctx, err)
	}
	if !v.complete {
		return ErrIntegrity
	}
	return nil
}

func (s *Store) verifyTrusted(raw []byte, meta TransferSignature, signature []byte) error {
	for _, key := range s.keys.Trusted {
		if KeyID(key) != meta.KeyID {
			continue
		}
		if !ed25519.Verify(key, raw, signature) {
			return fmt.Errorf("%w: signature does not verify", ErrUntrusted)
		}
		return nil
	}
	return ErrUntrusted
}

// Import authenticates and streams a full bundle into unpublished storage.
// It never changes an active-version pointer. An interrupted or failed import
// leaves the set staged or incomplete; repeating it reuses verified content.
func (s *Store) Import(ctx context.Context, r io.Reader, actor string) (*ImportResult, error) {
	if len(s.keys.Trusted) == 0 {
		return nil, fmt.Errorf("%w: no trusted transfer keys are configured", ErrUntrusted)
	}
	if len(actor) > 128 {
		return nil, fmt.Errorf("%w: actor", ErrInvalid)
	}
	counter := &countingReader{reader: contextReader{ctx: ctx, reader: r}}
	tr := tar.NewReader(counter)
	h, err := readBundleHeader(tr, s.verifyTrusted)
	if err != nil {
		return nil, ctxOr(ctx, err)
	}
	result := &ImportResult{SetID: h.transfer.SetID, Name: h.transfer.Name, Revision: h.transfer.Revision,
		SignerKeyID: h.signature.KeyID, ObjectCount: h.transfer.ObjectCount}
	if _, err := s.Stage(ctx, h.setRaw, actor); err != nil {
		return result, err
	}
	for _, o := range h.manifest.Objects {
		if err := nextObject(tr, o); err != nil {
			return result, ctxOr(ctx, err)
		}
		upload, err := s.Put(ctx, h.transfer.SetID, o.SHA256, tr)
		if err != nil {
			return result, ctxOr(ctx, err)
		}
		if upload.Reused {
			result.ReusedObjects++
		} else {
			result.WrittenObjects++
		}
	}
	if err := finishBundle(tr, counter, h.archiveBytes()); err != nil {
		return result, ctxOr(ctx, err)
	}
	if result.Verification, err = s.Verify(ctx, h.transfer.SetID); err != nil {
		return result, err
	}
	if len(result.Verification.Issues) > 0 {
		result.Set, _ = s.Get(ctx, h.transfer.SetID)
		return result, ErrIncomplete
	}
	// Attribute the set to the signer only after every declared byte arrived,
	// the archive ended exactly, and the stored content verified. A failed
	// import therefore never presents itself as signed provenance.
	if err := s.recordProvenance(ctx, h, actor); err != nil {
		return result, err
	}
	result.Verification.ProvenanceStatus = "preparer_attested"
	if result.Set, err = s.Get(ctx, h.transfer.SetID); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Store) recordProvenance(ctx context.Context, h *bundleHeader, actor string) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	row := db.DistributionSetProvenance{SetID: h.transfer.SetID, Origin: "bundle_import", SignerKeyID: h.signature.KeyID,
		TransferManifest: h.transferRaw, Signature: h.signatureRaw, ImportedBy: actor, ImportedAt: time.Now().UTC()}
	// Delete takes the same lock, but the set may have been removed between
	// Stage and here. Never leave a row that a later, unrelated Stage of the
	// same bytes would inherit.
	return s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&db.DistributionSet{}).Where("id = ?", row.SetID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "set_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"origin", "signer_key_id", "transfer_manifest", "signature", "imported_by", "imported_at"})}).
			Create(&row).Error
	})
}
