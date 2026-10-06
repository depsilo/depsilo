// Package snapshot implements the freeze / golden-snapshot MVP: promoting the
// cached artifact set into a named, hash-pinned snapshot, exporting and
// importing its manifest, and answering whether an artifact request is inside
// the active snapshot.
package snapshot

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/db"
)

// Format is the exported manifest schema identifier.
const Format = "depsilo/snapshot/v1"

// ActiveStateKey stores the active snapshot ID in control-plane state. An
// empty value means snapshot-only mode is disabled.
const ActiveStateKey = "snapshot_active_id"

// ImportItemLimit bounds one imported manifest.
const ImportItemLimit = 200_000

var (
	// ErrEmptySnapshot marks a promotion attempt with no cached artifact
	// hashes to pin.
	ErrEmptySnapshot = errors.New("no cached artifacts with recorded hashes; fetch artifacts through the proxy first")
	// ErrSnapshotNotFound marks an unknown snapshot.
	ErrSnapshotNotFound = errors.New("snapshot not found")
	// ErrSnapshotNameTaken marks a duplicate snapshot name.
	ErrSnapshotNameTaken = errors.New("a snapshot with this name already exists")
	// ErrSnapshotActive marks an attempt to delete the active snapshot.
	ErrSnapshotActive = errors.New("the active snapshot cannot be deleted; deactivate snapshot-only mode first")
)

// Store owns snapshot persistence and the in-memory active snapshot pointer.
type Store struct {
	db *gorm.DB

	mu         sync.RWMutex
	activeID   uint
	activeName string
}

func NewStore(database *gorm.DB) *Store {
	return &Store{db: database}
}

// Load refreshes the active snapshot pointer from durable state at startup.
func (s *Store) Load(ctx context.Context) error {
	var state db.ControlPlaneState
	err := s.db.WithContext(ctx).First(&state, "key = ?", ActiveStateKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.setActive(0, "")
		return nil
	}
	if err != nil {
		return fmt.Errorf("read active snapshot state: %w", err)
	}
	id, parseErr := strconv.ParseUint(strings.TrimSpace(state.Value), 10, 64)
	if parseErr != nil || id == 0 {
		s.setActive(0, "")
		return nil
	}
	var record db.Snapshot
	if err := s.db.WithContext(ctx).First(&record, uint(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			zap.L().Warn("snapshot: active snapshot no longer exists; disabling snapshot-only mode",
				zap.Uint64("snapshot_id", id))
			s.setActive(0, "")
			return nil
		}
		return fmt.Errorf("load active snapshot: %w", err)
	}
	s.setActive(record.ID, record.Name)
	return nil
}

// Active returns the active snapshot ID (0 when disabled) and its name.
func (s *Store) Active() (uint, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeID, s.activeName
}

func (s *Store) setActive(id uint, name string) {
	s.mu.Lock()
	s.activeID, s.activeName = id, name
	s.mu.Unlock()
}

// Decision is the snapshot-only gate result for one artifact request.
type Decision struct {
	Allowed      bool
	SnapshotID   uint
	SnapshotName string
}

// Check reports whether the exact artifact identity is pinned by the active
// snapshot. Snapshot-only mode disabled always allows.
func (s *Store) Check(ctx context.Context, ecosystem, pkg, version string) (Decision, error) {
	id, name := s.Active()
	if id == 0 {
		return Decision{Allowed: true}, nil
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&db.SnapshotItem{}).
		Where("snapshot_id = ? AND ecosystem = ? AND package = ? AND version = ?", id, ecosystem, pkg, version).
		Count(&count).Error
	if err != nil {
		return Decision{}, fmt.Errorf("snapshot membership lookup: %w", err)
	}
	return Decision{Allowed: count > 0, SnapshotID: id, SnapshotName: name}, nil
}

// CreateFromCache promotes every tamper-detection record into a new snapshot.
func (s *Store) CreateFromCache(ctx context.Context, name, note, actor string) (*db.Snapshot, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("snapshot name is required")
	}
	if len(name) > 128 {
		return nil, errors.New("snapshot name is too long")
	}
	var records []db.TamperRecord
	if err := s.db.WithContext(ctx).
		Where("sha256 <> ''").
		Order("ecosystem, package, version, key").
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("read cached artifacts: %w", err)
	}
	if len(records) == 0 {
		return nil, ErrEmptySnapshot
	}
	items := make([]db.SnapshotItem, 0, len(records))
	var totalBytes int64
	for _, record := range records {
		items = append(items, db.SnapshotItem{
			Ecosystem: record.Ecosystem,
			Package:   record.Package,
			Version:   record.Version,
			CacheKey:  record.Key,
			SHA256:    strings.ToLower(record.SHA256),
			Size:      record.Size,
		})
		totalBytes += record.Size
	}
	return s.insert(ctx, name, note, actor, items, totalBytes)
}

func (s *Store) insert(ctx context.Context, name, note, actor string, items []db.SnapshotItem, totalBytes int64) (*db.Snapshot, error) {
	record := db.Snapshot{
		Name: name, Note: strings.TrimSpace(note), CreatedBy: actor,
		ArtifactCount: int64(len(items)), TotalBytes: totalBytes,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing db.Snapshot
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, "name = ?", name).Error
		if err == nil {
			return ErrSnapshotNameTaken
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		for index := range items {
			items[index].SnapshotID = record.ID
		}
		return tx.CreateInBatches(&items, 500).Error
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrSnapshotNameTaken
		}
		return nil, err
	}
	return &record, nil
}

// List returns every snapshot, newest first.
func (s *Store) List(ctx context.Context) ([]db.Snapshot, error) {
	var records []db.Snapshot
	if err := s.db.WithContext(ctx).Order("id DESC").Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// Get returns one snapshot.
func (s *Store) Get(ctx context.Context, id uint) (*db.Snapshot, error) {
	var record db.Snapshot
	if err := s.db.WithContext(ctx).First(&record, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSnapshotNotFound
		}
		return nil, err
	}
	return &record, nil
}

// Items returns one page of snapshot items plus the total count.
func (s *Store) Items(ctx context.Context, id uint, limit, offset int) ([]db.SnapshotItem, int64, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, 0, err
	}
	query := s.db.WithContext(ctx).Model(&db.SnapshotItem{}).Where("snapshot_id = ?", id)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []db.SnapshotItem
	if err := query.Order("ecosystem, package, version, cache_key").
		Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Delete removes a snapshot that is not active.
func (s *Store) Delete(ctx context.Context, id uint) error {
	if active, _ := s.Active(); active == id {
		return ErrSnapshotActive
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record db.Snapshot
		if err := tx.First(&record, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSnapshotNotFound
			}
			return err
		}
		if err := tx.Where("snapshot_id = ?", id).Delete(&db.SnapshotItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&db.Snapshot{}, id).Error
	})
}

// Activate switches snapshot-only mode to one snapshot, or disables it when
// id is zero. The durable state is written before the in-memory pointer.
func (s *Store) Activate(ctx context.Context, id uint) (*db.Snapshot, error) {
	var record *db.Snapshot
	if id != 0 {
		loaded, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		record = loaded
	}
	value := ""
	if record != nil {
		value = strconv.FormatUint(uint64(record.ID), 10)
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.Assignments(map[string]interface{}{"value": value, "updated_at": time.Now().UTC()}),
	}).Create(&db.ControlPlaneState{Key: ActiveStateKey, Value: value}).Error; err != nil {
		return nil, fmt.Errorf("persist active snapshot: %w", err)
	}
	if record == nil {
		s.setActive(0, "")
	} else {
		s.setActive(record.ID, record.Name)
	}
	return record, nil
}

// manifestItem is one exported artifact.
type manifestItem struct {
	Ecosystem string `json:"ecosystem"`
	Package   string `json:"package"`
	Version   string `json:"version"`
	CacheKey  string `json:"cache_key"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
}

type manifestHeader struct {
	Format        string    `json:"format"`
	Name          string    `json:"name"`
	Note          string    `json:"note"`
	CreatedAt     time.Time `json:"created_at"`
	ArtifactCount int64     `json:"artifact_count"`
	TotalBytes    int64     `json:"total_bytes"`
}

// Export streams one snapshot manifest as JSON.
func (s *Store) Export(ctx context.Context, id uint, writer io.Writer) error {
	record, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	header, err := json.Marshal(manifestHeader{
		Format: Format, Name: record.Name, Note: record.Note,
		CreatedAt: record.CreatedAt, ArtifactCount: record.ArtifactCount, TotalBytes: record.TotalBytes,
	})
	if err != nil {
		return err
	}
	if _, err := writer.Write(header[:len(header)-1]); err != nil {
		return err
	}
	if _, err := io.WriteString(writer, `,"items":[`); err != nil {
		return err
	}
	first := true
	var batch []db.SnapshotItem
	err = s.db.WithContext(ctx).Where("snapshot_id = ?", id).
		Order("ecosystem, package, version, cache_key").
		FindInBatches(&batch, 1000, func(_ *gorm.DB, _ int) error {
			for _, item := range batch {
				encoded, err := json.Marshal(manifestItem{
					Ecosystem: item.Ecosystem, Package: item.Package, Version: item.Version,
					CacheKey: item.CacheKey, SHA256: item.SHA256, Size: item.Size,
				})
				if err != nil {
					return err
				}
				if !first {
					if _, err := io.WriteString(writer, ","); err != nil {
						return err
					}
				}
				first = false
				if _, err := writer.Write(encoded); err != nil {
					return err
				}
			}
			return nil
		}).Error
	if err != nil {
		return err
	}
	_, err = io.WriteString(writer, "]}")
	return err
}

// Import creates a snapshot from an exported manifest. The name may be
// overridden so the same manifest can be imported into several instances.
func (s *Store) Import(ctx context.Context, reader io.Reader, nameOverride, actor string) (*db.Snapshot, error) {
	var manifest struct {
		Format string         `json:"format"`
		Name   string         `json:"name"`
		Note   string         `json:"note"`
		Items  []manifestItem `json:"items"`
	}
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode snapshot manifest: %w", err)
	}
	if manifest.Format != Format {
		return nil, fmt.Errorf("unsupported snapshot manifest format %q", manifest.Format)
	}
	name := strings.TrimSpace(nameOverride)
	if name == "" {
		name = strings.TrimSpace(manifest.Name)
	}
	if name == "" {
		return nil, errors.New("snapshot name is required")
	}
	if len(manifest.Items) == 0 {
		return nil, ErrEmptySnapshot
	}
	if len(manifest.Items) > ImportItemLimit {
		return nil, fmt.Errorf("snapshot manifest has %d items; the import limit is %d", len(manifest.Items), ImportItemLimit)
	}
	items := make([]db.SnapshotItem, 0, len(manifest.Items))
	seen := make(map[string]bool, len(manifest.Items))
	var totalBytes int64
	for _, entry := range manifest.Items {
		ecosystem := strings.TrimSpace(entry.Ecosystem)
		pkg := strings.TrimSpace(entry.Package)
		version := strings.TrimSpace(entry.Version)
		cacheKey := strings.TrimSpace(entry.CacheKey)
		hash := strings.ToLower(strings.TrimSpace(entry.SHA256))
		if ecosystem == "" || pkg == "" || version == "" || cacheKey == "" {
			return nil, errors.New("snapshot item is missing an identity field")
		}
		if len(hash) != 64 {
			return nil, fmt.Errorf("snapshot item %s@%s has an invalid SHA-256", pkg, version)
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return nil, fmt.Errorf("snapshot item %s@%s has an invalid SHA-256", pkg, version)
		}
		if entry.Size < 0 {
			return nil, fmt.Errorf("snapshot item %s@%s has a negative size", pkg, version)
		}
		key := ecosystem + "\x00" + pkg + "\x00" + version + "\x00" + cacheKey
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, db.SnapshotItem{
			Ecosystem: ecosystem, Package: pkg, Version: version,
			CacheKey: cacheKey, SHA256: hash, Size: entry.Size,
		})
		totalBytes += entry.Size
	}
	record, err := s.insert(ctx, name, manifest.Note, actor, items, totalBytes)
	if err != nil {
		return nil, err
	}
	// Seed tamper-detection baselines for artifacts this instance has not seen
	// yet, so a first fetch whose bytes differ from the imported manifest
	// raises a tamper alert instead of silently adopting the upstream bytes.
	// Existing first-seen baselines are never overwritten.
	if err := s.seedTamperBaselines(ctx, items); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Store) seedTamperBaselines(ctx context.Context, items []db.SnapshotItem) error {
	now := time.Now().UTC()
	records := make([]db.TamperRecord, 0, len(items))
	for _, item := range items {
		records = append(records, db.TamperRecord{
			Key: item.CacheKey, Ecosystem: item.Ecosystem, Package: item.Package,
			Version: item.Version, SHA256: item.SHA256, Size: item.Size,
			FirstSeenAt: now, LastVerifiedAt: now,
		})
	}
	if len(records) == 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoNothing: true,
	}).CreateInBatches(&records, 500).Error; err != nil {
		return fmt.Errorf("seed tamper baselines from snapshot: %w", err)
	}
	return nil
}
