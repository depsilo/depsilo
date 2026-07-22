package compilecache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/cache"
	"depsilo/internal/db"
)

var (
	ErrNotFound            = errors.New("compile-cache entry not found")
	ErrTooLarge            = errors.New("compile-cache entry exceeds the configured limit")
	ErrInvalidSize         = errors.New("compile-cache entry size is invalid")
	ErrSizeMismatch        = errors.New("compile-cache body size does not match Content-Length")
	ErrInsufficientStorage = errors.New("compile-cache capacity cannot be reclaimed")
	ErrUploadBusy          = errors.New("compile-cache upload queue is full")
	ErrDownloadBusy        = errors.New("compile-cache download concurrency is full")
	ErrChecksumMismatch    = errors.New("compile-cache artifact checksum mismatch")
)

const (
	compilerArtifactContentType = "application/octet-stream"
	stagingCleanupGrace         = 5 * time.Minute
	maximumUploadTimeout        = 24 * time.Hour
	checksumHoldbackBytes       = 32 << 10
)

// Limits bounds logical capacity, transfers and per-namespace usage.
type Limits struct {
	MaxBytes               int64
	MaxEntries             int64
	MaxEntryBytes          int64
	NamespaceMaxBytes      int64
	NamespaceMaxEntries    int64
	MaxConcurrentUploads   int
	MaxQueuedUploads       int
	MaxInflightUploadBytes int64
	UploadTimeout          time.Duration
	MaxConcurrentDownloads int
	DownloadTimeout        time.Duration
	HighWatermarkPercent   int
}

// Entry is a streamed compiler artifact returned by Open.
type Entry struct {
	Body io.ReadCloser
	Size int64
}

type boundedEntryBody struct {
	body      io.ReadCloser
	release   func()
	once      sync.Once
	closeOnce sync.Once
	closeErr  error
}

// checksumVerifyingBody withholds the final chunk until the complete object
// matches its committed SHA-256. A corrupt response is therefore shorter than
// Content-Length and cannot be accepted as a successful cache hit by a client.
type checksumVerifyingBody struct {
	body       io.ReadCloser
	hasher     hash.Hash
	expected   string
	pending    []byte
	readBuffer []byte
	reachedEOF bool
	verified   bool
	terminal   error
	onMismatch func()
	once       sync.Once
}

func newChecksumVerifyingBody(body io.ReadCloser, expected string, onMismatch func()) *checksumVerifyingBody {
	return &checksumVerifyingBody{
		body: body, hasher: sha256.New(), expected: expected,
		readBuffer: make([]byte, checksumHoldbackBytes), onMismatch: onMismatch,
	}
}

func (body *checksumVerifyingBody) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	if body.terminal != nil {
		return 0, body.terminal
	}
	for {
		if body.reachedEOF {
			if !body.verified {
				body.verified = true
				if hex.EncodeToString(body.hasher.Sum(nil)) != body.expected {
					body.pending = nil
					body.terminal = ErrChecksumMismatch
					if body.onMismatch != nil {
						body.once.Do(body.onMismatch)
					}
					return 0, body.terminal
				}
			}
			if len(body.pending) == 0 {
				body.terminal = io.EOF
				return 0, io.EOF
			}
			n := copy(buffer, body.pending)
			body.pending = body.pending[n:]
			return n, nil
		}

		if len(body.pending) > checksumHoldbackBytes {
			available := len(body.pending) - checksumHoldbackBytes
			if available > len(buffer) {
				available = len(buffer)
			}
			copy(buffer, body.pending[:available])
			body.pending = body.pending[available:]
			return available, nil
		}

		n, err := body.body.Read(body.readBuffer)
		if n > 0 {
			_, _ = body.hasher.Write(body.readBuffer[:n])
			body.pending = append(body.pending, body.readBuffer[:n]...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				body.reachedEOF = true
				continue
			}
			body.pending = nil
			body.terminal = err
			return 0, err
		}
		if n == 0 {
			body.pending = nil
			body.terminal = io.ErrNoProgress
			return 0, body.terminal
		}
	}
}

func (body *checksumVerifyingBody) Close() error { return body.body.Close() }

func (body *boundedEntryBody) releaseSlot() {
	body.once.Do(body.release)
}

func (body *boundedEntryBody) Read(buffer []byte) (int, error) {
	n, err := body.body.Read(buffer)
	if err != nil {
		body.releaseSlot()
	}
	return n, err
}

func (body *boundedEntryBody) Close() error {
	body.releaseSlot()
	body.closeOnce.Do(func() { body.closeErr = body.body.Close() })
	return body.closeErr
}

// PutResult describes a committed artifact upload.
type PutResult struct {
	Created bool
	Size    int64
}

// CleanupResult reports one LRU reclamation pass.
type CleanupResult struct {
	RemovedEntries int   `json:"removed_entries"`
	ReclaimedBytes int64 `json:"reclaimed_bytes"`
	SizeBytes      int64 `json:"size_bytes"`
	Entries        int64 `json:"entries"`
	deletionPaths  []string
}

// Stats is a point-in-time compiler-cache usage snapshot.
type Stats struct {
	SizeBytes      int64 `json:"size_bytes"`
	MaxBytes       int64 `json:"max_bytes"`
	Entries        int64 `json:"entries"`
	MaxEntries     int64 `json:"max_entries"`
	Hits           int64 `json:"hits"`
	NamespaceCount int64 `json:"namespace_count"`
}

// Observer keeps transport-specific metrics outside the cache domain while
// still publishing changes caused by background cleanup and deletion.
type Observer struct {
	StatsUpdated func(Stats)
	Evicted      func(reason string, entries int)
}

type pendingTouch struct {
	Protocol  Protocol
	Namespace string
	Key       string
	Hits      int64
	At        time.Time
}

type resourceUsage struct {
	Bytes   int64
	Entries int64
}

// Service owns compiler-cache storage semantics. The concrete Storage is a
// dedicated instance; callers must never pass the package-cache storage root.
type Service struct {
	storage    cache.Storage
	db         *gorm.DB
	limits     Limits
	now        func() time.Time
	usage      atomic.Int64
	entries    atomic.Int64
	hits       atomic.Int64
	namespaces atomic.Int64

	capacityMu      sync.Mutex
	namespaceUsage  map[string]resourceUsage
	uploadSlots     chan struct{}
	uploadAdmission chan struct{}
	inflight        *semaphore.Weighted
	downloadSlots   chan struct{}
	observerMu      sync.RWMutex
	observer        Observer
	publishMu       sync.Mutex

	keyLocks *keyLockGate
	touchMu  sync.Mutex
	touches  map[string]pendingTouch
}

// NewService constructs the shared compiler-cache Module and loads its logical
// usage ledger from metadata.
func NewService(storage cache.Storage, database *gorm.DB, limits Limits) (*Service, error) {
	if storage == nil {
		return nil, errors.New("compile cache: storage is required")
	}
	if database == nil {
		return nil, errors.New("compile cache: database is required")
	}
	if limits.MaxBytes <= 0 || limits.MaxEntryBytes <= 0 || limits.NamespaceMaxBytes <= 0 || limits.MaxInflightUploadBytes <= 0 {
		return nil, errors.New("compile cache: byte limits must be positive")
	}
	if limits.MaxEntries <= 0 || limits.NamespaceMaxEntries <= 0 {
		return nil, errors.New("compile cache: entry limits must be positive")
	}
	if limits.MaxConcurrentUploads <= 0 {
		return nil, errors.New("compile cache: max concurrent uploads must be positive")
	}
	if limits.MaxQueuedUploads < 0 {
		return nil, errors.New("compile cache: max queued uploads must not be negative")
	}
	if limits.UploadTimeout <= 0 || limits.UploadTimeout > maximumUploadTimeout {
		return nil, errors.New("compile cache: upload timeout must be positive and at most 24 hours")
	}
	if limits.MaxConcurrentDownloads <= 0 {
		return nil, errors.New("compile cache: max concurrent downloads must be positive")
	}
	if limits.DownloadTimeout <= 0 || limits.DownloadTimeout > 24*time.Hour {
		return nil, errors.New("compile cache: download timeout must be positive and at most 24 hours")
	}
	if limits.MaxInflightUploadBytes < limits.MaxEntryBytes {
		return nil, errors.New("compile cache: in-flight upload limit must be at least the per-entry limit")
	}
	if limits.HighWatermarkPercent < 1 || limits.HighWatermarkPercent > 100 {
		return nil, errors.New("compile cache: high watermark must be between 1 and 100")
	}
	service := &Service{
		storage:         storage,
		db:              database,
		limits:          limits,
		now:             time.Now,
		keyLocks:        newKeyLockGate(),
		touches:         make(map[string]pendingTouch),
		namespaceUsage:  make(map[string]resourceUsage),
		uploadSlots:     make(chan struct{}, limits.MaxConcurrentUploads),
		uploadAdmission: make(chan struct{}, limits.MaxConcurrentUploads+limits.MaxQueuedUploads),
		inflight:        semaphore.NewWeighted(limits.MaxInflightUploadBytes),
		downloadSlots:   make(chan struct{}, limits.MaxConcurrentDownloads),
	}
	type aggregate struct {
		Namespace string
		Bytes     int64
		Entries   int64
	}
	var aggregates []aggregate
	if err := database.Model(&db.CompileCacheEntry{}).
		Select("namespace, COALESCE(SUM(size), 0) AS bytes, COUNT(*) AS entries").
		Group("namespace").Scan(&aggregates).Error; err != nil {
		return nil, fmt.Errorf("measure compile-cache metadata: %w", err)
	}
	for _, aggregate := range aggregates {
		service.namespaceUsage[aggregate.Namespace] = resourceUsage{Bytes: aggregate.Bytes, Entries: aggregate.Entries}
		service.usage.Add(aggregate.Bytes)
		service.entries.Add(aggregate.Entries)
		service.namespaces.Add(1)
	}
	return service, nil
}

// MaxEntryBytes returns the largest accepted artifact body.
func (s *Service) MaxEntryBytes() int64 { return s.limits.MaxEntryBytes }

// UploadTimeout returns the end-to-end upload deadline.
func (s *Service) UploadTimeout() time.Duration { return s.limits.UploadTimeout }

// DownloadTimeout returns the end-to-end download deadline.
func (s *Service) DownloadTimeout() time.Duration { return s.limits.DownloadTimeout }

// CheckWritable performs a small, bounded write/delete round trip against the
// configured storage Adapter without creating a logical cache entry.
func (s *Service) CheckWritable(ctx context.Context) error {
	const probePayload = "1"
	probeCtx, cancel := context.WithTimeout(ctx, s.limits.UploadTimeout)
	defer cancel()
	releaseAdmission, err := s.admitUpload()
	if err != nil {
		return err
	}
	defer releaseAdmission()
	releaseTransfer, releaseStaging, err := s.acquireUpload(probeCtx, int64(len(probePayload)))
	if err != nil {
		return err
	}
	transferActive := true
	stagingActive := true
	defer func() {
		if transferActive {
			releaseTransfer()
		}
		if stagingActive {
			releaseStaging()
		}
	}()
	path, err := newProbeObjectPath()
	if err != nil {
		return err
	}
	if err := s.storage.Put(
		probeCtx, path, strings.NewReader(probePayload), int64(len(probePayload)), compilerArtifactContentType,
	); err != nil {
		if ctxErr := probeCtx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("write compiler-cache storage probe: %w", err)
	}
	releaseTransfer()
	transferActive = false
	releaseStaging()
	stagingActive = false
	if err := s.storage.Delete(probeCtx, path); err == nil {
		return nil
	}
	// A successful PUT proves write capability. Preserve cleanup durability if
	// the best-effort DELETE fails or the request is canceled immediately after.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	if err := enqueueDeletion(s.db.WithContext(cleanupCtx), path); err != nil {
		return fmt.Errorf("queue compiler-cache probe cleanup: %w", err)
	}
	return nil
}

// SetObserver installs callbacks for aggregate stats and eviction events.
func (s *Service) SetObserver(observer Observer) {
	s.observerMu.Lock()
	s.observer = observer
	s.observerMu.Unlock()
	s.publishStats()
}

func (s *Service) publishStats() {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	s.observerMu.RLock()
	callback := s.observer.StatsUpdated
	s.observerMu.RUnlock()
	if callback == nil {
		return
	}
	stats, _ := s.Stats(context.Background())
	callback(stats)
}

func (s *Service) publishEvictions(reason string, entries int) {
	if entries == 0 {
		return
	}
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	s.observerMu.RLock()
	callback := s.observer.Evicted
	s.observerMu.RUnlock()
	if callback != nil {
		callback(reason, entries)
	}
}

func cacheIdentity(id ArtifactID) string {
	return cacheIdentityParts(id.protocol, id.namespace, id.key)
}

func cacheIdentityParts(protocol Protocol, namespace, key string) string {
	return string(protocol) + "\x00" + namespace + "\x00" + key
}

// Stat returns the size of a committed artifact or ErrNotFound.
func (s *Service) Stat(ctx context.Context, id ArtifactID) (int64, error) {
	if err := id.validate(); err != nil {
		return 0, err
	}
	unlock, err := s.keyLocks.lock(ctx, cacheIdentity(id))
	if err != nil {
		return 0, err
	}
	defer unlock()
	var entry db.CompileCacheEntry
	lookup := s.db.WithContext(ctx).Where(
		"protocol = ? AND namespace = ? AND key = ?", id.protocol, id.namespace, id.key,
	).Limit(1).Find(&entry)
	if lookup.Error != nil {
		return 0, fmt.Errorf("read compile-cache metadata: %w", lookup.Error)
	}
	if lookup.RowsAffected == 0 {
		return 0, ErrNotFound
	}
	releaseDownload, err := s.acquireDownload()
	if err != nil {
		return 0, err
	}
	defer releaseDownload()
	exists, err := s.storage.Exists(ctx, entry.StoragePath)
	if err != nil {
		return 0, fmt.Errorf("stat compile-cache object: %w", err)
	}
	if !exists {
		if err := s.invalidateEntry(ctx, entry); err != nil {
			return 0, err
		}
		return 0, ErrNotFound
	}
	meta, err := s.storage.Stat(ctx, entry.StoragePath)
	if err != nil {
		return 0, fmt.Errorf("stat compile-cache object: %w", err)
	}
	if meta.Size <= 0 || meta.Size != entry.Size {
		if err := s.invalidateEntry(ctx, entry); err != nil {
			return 0, err
		}
		return 0, ErrNotFound
	}
	return meta.Size, nil
}

// Open streams a committed artifact while holding a bounded download slot.
func (s *Service) Open(ctx context.Context, id ArtifactID) (Entry, error) {
	if err := id.validate(); err != nil {
		return Entry{}, err
	}
	unlock, err := s.keyLocks.lock(ctx, cacheIdentity(id))
	if err != nil {
		return Entry{}, err
	}
	defer unlock()
	var metadata db.CompileCacheEntry
	lookup := s.db.WithContext(ctx).Where(
		"protocol = ? AND namespace = ? AND key = ?", id.protocol, id.namespace, id.key,
	).Limit(1).Find(&metadata)
	if lookup.Error != nil {
		return Entry{}, fmt.Errorf("read compile-cache metadata: %w", lookup.Error)
	}
	if lookup.RowsAffected == 0 {
		return Entry{}, ErrNotFound
	}
	if checksum, decodeErr := hex.DecodeString(metadata.Checksum); decodeErr != nil || len(checksum) != sha256.Size {
		if err := s.invalidateEntry(ctx, metadata); err != nil {
			return Entry{}, err
		}
		return Entry{}, ErrNotFound
	}
	releaseDownload, err := s.acquireDownload()
	if err != nil {
		return Entry{}, err
	}
	downloadActive := true
	defer func() {
		if downloadActive {
			releaseDownload()
		}
	}()
	exists, err := s.storage.Exists(ctx, metadata.StoragePath)
	if err != nil {
		return Entry{}, fmt.Errorf("check compile-cache object: %w", err)
	}
	if !exists {
		if err := s.invalidateEntry(ctx, metadata); err != nil {
			return Entry{}, err
		}
		return Entry{}, ErrNotFound
	}
	body, size, err := s.storage.Get(ctx, metadata.StoragePath)
	if err != nil {
		return Entry{}, fmt.Errorf("open compile-cache object: %w", err)
	}
	if size <= 0 {
		_ = body.Close()
		if err := s.invalidateEntry(ctx, metadata); err != nil {
			return Entry{}, err
		}
		return Entry{}, ErrNotFound
	}
	if size != metadata.Size {
		_ = body.Close()
		if err := s.invalidateEntry(ctx, metadata); err != nil {
			return Entry{}, err
		}
		return Entry{}, ErrNotFound
	}
	s.queueTouch(id)
	downloadActive = false
	verified := newChecksumVerifyingBody(body, metadata.Checksum, func() {
		s.invalidateStreamedEntry(id, metadata)
	})
	return Entry{Body: &boundedEntryBody{body: verified, release: releaseDownload}, Size: size}, nil
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	n, err := reader.reader.Read(buffer)
	reader.count += int64(n)
	return n, err
}

func (s *Service) acquireUpload(ctx context.Context, size int64) (releaseTransfer, releaseStaging func(), err error) {
	select {
	case s.uploadSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	if err := s.inflight.Acquire(ctx, size); err != nil {
		<-s.uploadSlots
		return nil, nil, err
	}
	var transferOnce, stagingOnce sync.Once
	return func() {
			transferOnce.Do(func() { <-s.uploadSlots })
		}, func() {
			stagingOnce.Do(func() { s.inflight.Release(size) })
		}, nil
}

func (s *Service) acquireDownload() (func(), error) {
	select {
	case s.downloadSlots <- struct{}{}:
		return func() { <-s.downloadSlots }, nil
	default:
		return nil, ErrDownloadBusy
	}
}

func (s *Service) admitUpload() (func(), error) {
	select {
	case s.uploadAdmission <- struct{}{}:
		return func() { <-s.uploadAdmission }, nil
	default:
		return nil, ErrUploadBusy
	}
}

// Put atomically stores an artifact after enforcing transfer and capacity
// limits. An incomplete body never replaces a committed generation.
func (s *Service) Put(ctx context.Context, id ArtifactID, body io.Reader, size int64) (PutResult, error) {
	if err := id.validate(); err != nil {
		return PutResult{}, err
	}
	if size <= 0 {
		return PutResult{}, ErrInvalidSize
	}
	if body == nil {
		return PutResult{}, ErrInvalidSize
	}
	if size > s.limits.MaxEntryBytes || size > s.limits.MaxBytes || size > s.limits.NamespaceMaxBytes {
		return PutResult{}, ErrTooLarge
	}
	uploadCtx, cancelUpload := context.WithTimeout(ctx, s.limits.UploadTimeout)
	defer cancelUpload()
	ctx = uploadCtx
	releaseAdmission, err := s.admitUpload()
	if err != nil {
		return PutResult{}, err
	}
	defer releaseAdmission()
	unlock, err := s.keyLocks.lock(ctx, cacheIdentity(id))
	if err != nil {
		return PutResult{}, err
	}
	defer unlock()

	var previous db.CompileCacheEntry
	lookup := s.db.WithContext(ctx).Where(
		"protocol = ? AND namespace = ? AND key = ?", id.protocol, id.namespace, id.key,
	).Limit(1).Find(&previous)
	if lookup.Error != nil {
		return PutResult{}, fmt.Errorf("read compile-cache metadata: %w", lookup.Error)
	}
	created := lookup.RowsAffected == 0
	previousSize := int64(0)
	if !created {
		previousSize = previous.Size
	}
	releaseTransfer, releaseStaging, err := s.acquireUpload(ctx, size)
	if err != nil {
		return PutResult{}, err
	}
	transferActive := true
	stagingActive := true
	defer func() {
		if transferActive {
			releaseTransfer()
		}
		if stagingActive {
			releaseStaging()
		}
	}()
	path, err := newObjectPath(id)
	if err != nil {
		return PutResult{}, err
	}
	// Local storage writes through <generation>.tmp before rename, while S3
	// writes the final generation directly. Pre-register both paths so either
	// crash shape remains reclaimable. The delay always exceeds the longest
	// allowed upload, preventing the deletion worker from racing an active PUT.
	stagingPaths := []string{path, path + ".tmp"}
	notBefore := s.now().UTC().Add(s.limits.UploadTimeout + stagingCleanupGrace)
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, stagingPath := range stagingPaths {
			if err := enqueueDeferredDeletion(tx, stagingPath, notBefore); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return PutResult{}, fmt.Errorf("register compiler-cache staging object: %w", err)
	}
	hasher := sha256.New()
	// Bound even non-HTTP callers to the declared length plus one byte. The
	// extra byte lets us reject an overlong body without streaming arbitrary
	// data into staging storage.
	counter := &countingReader{reader: io.TeeReader(io.LimitReader(body, size+1), hasher)}
	putErr := s.storage.Put(ctx, path, counter, size, compilerArtifactContentType)
	// Release the transfer slot before best-effort cleanup so a slow remote
	// DELETE cannot consume upload concurrency. Keep the staging-byte lease
	// until this generation is either committed or its fast cleanup finishes.
	releaseTransfer()
	transferActive = false
	if putErr != nil {
		s.removeUncommittedObjects(stagingPaths)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return PutResult{}, ctxErr
		}
		return PutResult{}, fmt.Errorf("store compile-cache object: %w", putErr)
	}
	if counter.count != size {
		s.removeUncommittedObjects(stagingPaths)
		return PutResult{}, fmt.Errorf("%w: read %d bytes, expected %d", ErrSizeMismatch, counter.count, size)
	}
	// Only a complete, size-checked upload may trigger LRU reclamation. Doing
	// this before reading the body lets a client evict valid entries merely by
	// declaring a large Content-Length and disconnecting.
	reserve := size - previousSize
	if reserve < 0 {
		reserve = 0
	}
	entryReserve := int64(0)
	if created {
		entryReserve = 1
	}
	if err := s.reserveCapacity(ctx, reserve, entryReserve, id); err != nil {
		s.removeUncommittedObjects(stagingPaths)
		s.publishStats()
		return PutResult{}, err
	}
	reservedBytes := reserve
	reservedEntries := entryReserve
	succeeded := false
	defer func() {
		if !succeeded {
			s.releaseCapacity(id.namespace, reservedBytes, reservedEntries)
		}
		s.publishStats()
	}()
	now := s.now().UTC()
	entry := db.CompileCacheEntry{
		Protocol: string(id.protocol), Namespace: id.namespace, Key: id.key, StoragePath: path, Size: size,
		Checksum: hex.EncodeToString(hasher.Sum(nil)), LastAccessed: now,
	}
	updates := map[string]any{
		"storage_path":  path,
		"size":          size,
		"checksum":      entry.Checksum,
		"last_accessed": now,
		"updated_at":    now,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "protocol"}, {Name: "namespace"}, {Name: "key"}},
			DoUpdates: clause.Assignments(updates),
		}).Create(&entry).Error; err != nil {
			return err
		}
		if err := tx.Where("storage_path IN ?", stagingPaths).Delete(&db.CompileCacheDeletion{}).Error; err != nil {
			return err
		}
		if !created && previous.StoragePath != "" && previous.StoragePath != path {
			return enqueueDeletion(tx, previous.StoragePath)
		}
		return nil
	}); err != nil {
		s.removeUncommittedObjects(stagingPaths)
		return PutResult{}, fmt.Errorf("commit compile-cache metadata: %w", err)
	}
	// The new generation is now both visible and charged to the logical
	// capacity ledger, so it no longer consumes the uncommitted staging budget.
	releaseStaging()
	stagingActive = false
	if size < previousSize {
		s.releaseCapacity(id.namespace, previousSize-size, 0)
	}
	succeeded = true
	if !created && previous.StoragePath != "" && previous.StoragePath != path {
		s.processDeletionPaths([]string{previous.StoragePath})
	}
	return PutResult{Created: created, Size: size}, nil
}

// Delete removes an artifact idempotently and queues durable object cleanup.
func (s *Service) Delete(ctx context.Context, id ArtifactID) (bool, error) {
	if err := id.validate(); err != nil {
		return false, err
	}
	unlock, err := s.keyLocks.lock(ctx, cacheIdentity(id))
	if err != nil {
		return false, err
	}
	defer unlock()
	var entry db.CompileCacheEntry
	lookup := s.db.WithContext(ctx).Where(
		"protocol = ? AND namespace = ? AND key = ?", id.protocol, id.namespace, id.key,
	).Limit(1).Find(&entry)
	if lookup.Error != nil {
		return false, fmt.Errorf("read deleted compile-cache metadata: %w", lookup.Error)
	}
	if lookup.RowsAffected == 0 {
		return false, nil
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := enqueueDeletion(tx, entry.StoragePath); err != nil {
			return err
		}
		return tx.Delete(&entry).Error
	}); err != nil {
		return false, fmt.Errorf("delete compile-cache metadata: %w", err)
	}
	s.releaseCapacity(id.namespace, entry.Size, 1)
	s.processDeletionPaths([]string{entry.StoragePath})
	s.publishStats()
	return true, nil
}

// invalidateEntry turns an externally missing or size-corrupt object into a
// normal cache miss. The caller must hold the entry's exact key lock.
func (s *Service) invalidateEntry(ctx context.Context, entry db.CompileCacheEntry) error {
	deleted := false
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND storage_path = ?", entry.ID, entry.StoragePath).Delete(&db.CompileCacheEntry{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		deleted = true
		return enqueueDeletion(tx, entry.StoragePath)
	}); err != nil {
		return fmt.Errorf("invalidate corrupt compile-cache metadata: %w", err)
	}
	if deleted {
		s.releaseCapacity(entry.Namespace, entry.Size, 1)
		s.processDeletionPaths([]string{entry.StoragePath})
		s.publishStats()
	}
	return nil
}

// invalidateStreamedEntry hides a generation that failed checksum validation.
// Object deletion remains in the durable outbox so the reader can close before
// local Windows or remote storage cleanup is retried.
func (s *Service) invalidateStreamedEntry(id ArtifactID, entry db.CompileCacheEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := s.keyLocks.lock(ctx, cacheIdentity(id))
	if err != nil {
		zap.L().Warn("lock corrupt compiler-cache generation", zap.Error(err))
		return
	}
	defer unlock()
	deleted := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where(
			"id = ? AND protocol = ? AND namespace = ? AND key = ? AND storage_path = ?",
			entry.ID, id.protocol, id.namespace, id.key, entry.StoragePath,
		).Delete(&db.CompileCacheEntry{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		deleted = true
		return enqueueDeletion(tx, entry.StoragePath)
	})
	if err != nil {
		zap.L().Warn("invalidate checksum-corrupt compiler-cache generation", zap.Error(err))
		return
	}
	if deleted {
		s.releaseCapacity(entry.Namespace, entry.Size, 1)
		s.publishStats()
	}
}

func (s *Service) reserveCapacity(ctx context.Context, bytes, entries int64, exclude ArtifactID) error {
	if bytes == 0 && entries == 0 {
		return nil
	}
	s.capacityMu.Lock()
	var deletionPaths []string
	evicted := 0
	defer func() {
		s.capacityMu.Unlock()
		s.processDeletionPaths(deletionPaths)
		s.publishEvictions("lru", evicted)
	}()

	namespaceMaxBytes := min(s.limits.NamespaceMaxBytes, s.limits.MaxBytes)
	namespaceMaxEntries := min(s.limits.NamespaceMaxEntries, s.limits.MaxEntries)
	namespaceCurrent := s.namespaceUsage[exclude.namespace]
	if namespaceCurrent.Bytes+bytes > namespaceMaxBytes || namespaceCurrent.Entries+entries > namespaceMaxEntries {
		result, err := s.cleanupToLocked(
			ctx,
			namespaceMaxBytes-bytes,
			namespaceMaxEntries-entries,
			exclude.namespace,
			exclude,
		)
		deletionPaths = append(deletionPaths, result.deletionPaths...)
		evicted += result.RemovedEntries
		if err != nil {
			return err
		}
	}
	if s.usage.Load()+bytes > s.limits.MaxBytes || s.entries.Load()+entries > s.limits.MaxEntries {
		result, err := s.cleanupToLocked(
			ctx,
			s.limits.MaxBytes-bytes,
			s.limits.MaxEntries-entries,
			"",
			exclude,
		)
		deletionPaths = append(deletionPaths, result.deletionPaths...)
		evicted += result.RemovedEntries
		if err != nil {
			return err
		}
	}
	namespaceCurrent = s.namespaceUsage[exclude.namespace]
	if namespaceCurrent.Bytes+bytes > namespaceMaxBytes || namespaceCurrent.Entries+entries > namespaceMaxEntries ||
		s.usage.Load()+bytes > s.limits.MaxBytes || s.entries.Load()+entries > s.limits.MaxEntries {
		return ErrInsufficientStorage
	}
	s.addCapacityLocked(exclude.namespace, bytes, entries)
	return nil
}

func (s *Service) addCapacityLocked(namespace string, bytes, entries int64) {
	current := s.namespaceUsage[namespace]
	wasEmpty := current.Bytes == 0 && current.Entries == 0
	current.Bytes += bytes
	current.Entries += entries
	if current.Bytes == 0 && current.Entries == 0 {
		delete(s.namespaceUsage, namespace)
		if !wasEmpty {
			s.namespaces.Add(-1)
		}
	} else {
		s.namespaceUsage[namespace] = current
		if wasEmpty {
			s.namespaces.Add(1)
		}
	}
	s.usage.Add(bytes)
	s.entries.Add(entries)
}

func (s *Service) releaseCapacity(namespace string, bytes, entries int64) {
	if bytes == 0 && entries == 0 {
		return
	}
	s.capacityMu.Lock()
	s.addCapacityLocked(namespace, -bytes, -entries)
	s.capacityMu.Unlock()
}

func enqueueDeletion(tx *gorm.DB, storagePath string) error {
	return enqueueDeferredDeletion(tx, storagePath, time.Now().UTC())
}

func enqueueDeferredDeletion(tx *gorm.DB, storagePath string, notBefore time.Time) error {
	if storagePath == "" {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&db.CompileCacheDeletion{
		StoragePath: storagePath,
		NotBefore:   notBefore,
	}).Error
}

func (s *Service) processDeletionPath(ctx context.Context, storagePath string) {
	if storagePath == "" {
		return
	}
	if err := s.storage.Delete(ctx, storagePath); err != nil {
		zap.L().Warn("delete compiler-cache object; queued for retry", zap.String("storage_path", storagePath), zap.Error(err))
		if updateErr := s.db.WithContext(ctx).Model(&db.CompileCacheDeletion{}).
			Where("storage_path = ?", storagePath).
			Updates(map[string]any{"attempts": gorm.Expr("attempts + 1"), "last_error": err.Error()}).Error; updateErr != nil {
			zap.L().Warn("record compiler-cache deletion failure", zap.String("storage_path", storagePath), zap.Error(updateErr))
		}
		return
	}
	if err := s.db.WithContext(ctx).Where("storage_path = ?", storagePath).Delete(&db.CompileCacheDeletion{}).Error; err != nil {
		zap.L().Warn("remove completed compiler-cache deletion", zap.String("storage_path", storagePath), zap.Error(err))
	}
}

func (s *Service) removeUncommittedObjects(storagePaths []string) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Keep the delayed staging markers even when this fast-path DELETE works.
	// A canceled S3 PUT can complete late; the delayed retry then removes that
	// object instead of leaving an untracked generation behind.
	for _, storagePath := range storagePaths {
		if err := s.storage.Delete(cleanupCtx, storagePath); err != nil {
			zap.L().Debug("delete uncommitted compiler-cache object; delayed retry remains queued",
				zap.String("storage_path", storagePath), zap.Error(err))
		}
	}
}

func (s *Service) processDeletionPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, storagePath := range paths {
		s.processDeletionPath(ctx, storagePath)
	}
}

// ProcessPendingDeletions retries durable storage deletions in bounded batches.
func (s *Service) ProcessPendingDeletions(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 100
	}
	var pending []db.CompileCacheDeletion
	if err := s.db.WithContext(ctx).
		Where("not_before <= ?", s.now().UTC()).
		Order("attempts ASC, id ASC").Limit(limit).Find(&pending).Error; err != nil {
		return fmt.Errorf("list compiler-cache deletion queue: %w", err)
	}
	for _, item := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.processDeletionPath(ctx, item.StoragePath)
	}
	return nil
}

func (s *Service) queueTouch(id ArtifactID) {
	now := s.now().UTC()
	s.hits.Add(1)
	identity := cacheIdentity(id)
	s.touchMu.Lock()
	touch := s.touches[identity]
	touch.Protocol = id.protocol
	touch.Namespace = id.namespace
	touch.Key = id.key
	touch.Hits++
	touch.At = now
	s.touches[identity] = touch
	s.touchMu.Unlock()
}

// FlushTouches persists coalesced hit counters and LRU timestamps.
func (s *Service) FlushTouches(ctx context.Context) error {
	s.touchMu.Lock()
	if len(s.touches) == 0 {
		s.touchMu.Unlock()
		return nil
	}
	touches := s.touches
	s.touches = make(map[string]pendingTouch)
	s.touchMu.Unlock()

	failed := make(map[string]pendingTouch)
	var flushErrors []error
	for identity, touch := range touches {
		unlock, err := s.keyLocks.lock(ctx, identity)
		if err == nil {
			// Never upsert here: a queued read must not resurrect an entry
			// deleted by DELETE/cleanup, nor overwrite the path/size of a newer
			// PUT generation.
			err = s.db.WithContext(ctx).Model(&db.CompileCacheEntry{}).
				Where("protocol = ? AND namespace = ? AND key = ?", touch.Protocol, touch.Namespace, touch.Key).
				Updates(map[string]any{
					"hit_count": gorm.Expr("hit_count + ?", touch.Hits),
					"last_accessed": gorm.Expr(
						"CASE WHEN last_accessed < ? THEN ? ELSE last_accessed END",
						touch.At, touch.At,
					),
				}).Error
			unlock()
		}
		if err != nil {
			failed[identity] = touch
			flushErrors = append(flushErrors, err)
		}
	}
	if len(failed) > 0 {
		s.touchMu.Lock()
		for identity, touch := range failed {
			current := s.touches[identity]
			if current.Protocol == "" {
				s.touches[identity] = touch
				continue
			}
			current.Hits += touch.Hits
			if touch.At.After(current.At) {
				current.At = touch.At
			}
			s.touches[identity] = current
		}
		s.touchMu.Unlock()
		return fmt.Errorf("flush compile-cache hits: %w", errors.Join(flushErrors...))
	}
	return nil
}

// Reconcile repairs crash windows before the data plane starts serving. DB
// metadata is the visibility authority; rows without objects are removed and
// old, unreferenced object generations are reclaimed after a grace period.
// Callers should invoke this before exposing the Service to concurrent traffic.
func (s *Service) Reconcile(ctx context.Context, orphanGrace time.Duration) error {
	if orphanGrace < 0 {
		orphanGrace = 0
	}
	objects, err := s.storage.List(ctx, "v1")
	if err != nil {
		return fmt.Errorf("list compiler-cache objects for reconciliation: %w", err)
	}
	objectByPath := make(map[string]cache.ObjectMeta, len(objects))
	for _, object := range objects {
		// Local storage lists a directory while S3 performs a lexical prefix
		// match. Filter explicitly so an S3 object such as v10/... can never be
		// mistaken for a v1 orphan and deleted during startup reconciliation.
		if !strings.HasPrefix(object.Key, "v1/") {
			continue
		}
		objectByPath[object.Key] = object
	}
	objects = nil
	var queuedForDeletion []string
	rebuilt := make(map[string]resourceUsage)
	var logicalSize, logicalEntries int64
	var cursor uint
	for {
		var entries []db.CompileCacheEntry
		if err := s.db.WithContext(ctx).Where("id > ?", cursor).Order("id ASC").Limit(512).Find(&entries).Error; err != nil {
			return fmt.Errorf("list compiler-cache metadata for reconciliation: %w", err)
		}
		if len(entries) == 0 {
			break
		}
		for _, entry := range entries {
			cursor = entry.ID
			id, identityErr := NewArtifactID(Protocol(entry.Protocol), entry.Namespace, entry.Key)
			if identityErr != nil || !objectPathMatchesArtifact(id, entry.StoragePath) {
				if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					if _, exists := objectByPath[entry.StoragePath]; exists {
						if err := enqueueDeletion(tx, entry.StoragePath); err != nil {
							return err
						}
					}
					return tx.Delete(&entry).Error
				}); err != nil {
					return fmt.Errorf("remove invalid compiler-cache metadata: %w", err)
				}
				if _, exists := objectByPath[entry.StoragePath]; exists {
					queuedForDeletion = append(queuedForDeletion, entry.StoragePath)
					delete(objectByPath, entry.StoragePath)
				}
				continue
			}
			object, exists := objectByPath[entry.StoragePath]
			if !exists {
				if err := s.db.WithContext(ctx).Delete(&entry).Error; err != nil {
					return fmt.Errorf("remove missing compiler-cache metadata: %w", err)
				}
				continue
			}
			if entry.Size <= 0 || object.Size != entry.Size {
				if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					if err := enqueueDeletion(tx, entry.StoragePath); err != nil {
						return err
					}
					return tx.Delete(&entry).Error
				}); err != nil {
					return fmt.Errorf("remove corrupt compiler-cache metadata: %w", err)
				}
				queuedForDeletion = append(queuedForDeletion, entry.StoragePath)
				delete(objectByPath, entry.StoragePath)
				continue
			}
			delete(objectByPath, entry.StoragePath)
			usage := rebuilt[entry.Namespace]
			usage.Bytes += entry.Size
			usage.Entries++
			rebuilt[entry.Namespace] = usage
			logicalSize += entry.Size
			logicalEntries++
		}
	}
	s.capacityMu.Lock()
	s.namespaceUsage = rebuilt
	s.usage.Store(logicalSize)
	s.entries.Store(logicalEntries)
	s.namespaces.Store(int64(len(rebuilt)))
	s.capacityMu.Unlock()

	for _, storagePath := range queuedForDeletion {
		s.processDeletionPath(ctx, storagePath)
	}
	cutoff := s.now().UTC().Add(-orphanGrace)
	for _, object := range objectByPath {
		if object.LastModified.After(cutoff) {
			continue
		}
		if err := enqueueDeletion(s.db.WithContext(ctx), object.Key); err != nil {
			return fmt.Errorf("queue orphan compiler-cache object %q: %w", object.Key, err)
		}
		s.processDeletionPath(ctx, object.Key)
	}
	s.publishStats()
	return nil
}

// Cleanup evicts least-recently-used entries to the configured low watermark.
func (s *Service) Cleanup(ctx context.Context) (CleanupResult, error) {
	targetPercent := s.limits.HighWatermarkPercent - 10
	if targetPercent < 1 {
		targetPercent = 1
	}
	targetBytes := s.limits.MaxBytes * int64(targetPercent) / 100
	targetEntries := s.limits.MaxEntries * int64(targetPercent) / 100
	s.capacityMu.Lock()
	result, err := s.cleanupToLocked(ctx, targetBytes, targetEntries, "", ArtifactID{})
	s.capacityMu.Unlock()
	s.processDeletionPaths(result.deletionPaths)
	s.publishEvictions("lru", result.RemovedEntries)
	s.publishStats()
	return result, err
}

// EnforceLimits converges metadata loaded from an older, more permissive
// configuration to the current global and per-namespace limits. It is meant
// to run after Reconcile and before the data plane accepts traffic.
func (s *Service) EnforceLimits(ctx context.Context) (CleanupResult, error) {
	s.capacityMu.Lock()
	result := CleanupResult{SizeBytes: s.usage.Load(), Entries: s.entries.Load()}

	namespaces := make([]string, 0, len(s.namespaceUsage))
	for namespace := range s.namespaceUsage {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)

	namespaceMaxBytes := min(s.limits.NamespaceMaxBytes, s.limits.MaxBytes)
	namespaceMaxEntries := min(s.limits.NamespaceMaxEntries, s.limits.MaxEntries)
	var enforceErr error
	for _, namespace := range namespaces {
		usage := s.namespaceUsage[namespace]
		if usage.Bytes <= namespaceMaxBytes && usage.Entries <= namespaceMaxEntries {
			continue
		}
		partial, err := s.cleanupToLocked(ctx, namespaceMaxBytes, namespaceMaxEntries, namespace, ArtifactID{})
		mergeCleanupResult(&result, partial)
		if err != nil {
			enforceErr = err
			break
		}
	}
	if enforceErr == nil && (s.usage.Load() > s.limits.MaxBytes || s.entries.Load() > s.limits.MaxEntries) {
		partial, err := s.cleanupToLocked(ctx, s.limits.MaxBytes, s.limits.MaxEntries, "", ArtifactID{})
		mergeCleanupResult(&result, partial)
		enforceErr = err
	}
	result.SizeBytes = s.usage.Load()
	result.Entries = s.entries.Load()
	s.capacityMu.Unlock()

	s.processDeletionPaths(result.deletionPaths)
	s.publishEvictions("lru", result.RemovedEntries)
	s.publishStats()
	return result, enforceErr
}

func mergeCleanupResult(target *CleanupResult, source CleanupResult) {
	target.RemovedEntries += source.RemovedEntries
	target.ReclaimedBytes += source.ReclaimedBytes
	target.deletionPaths = append(target.deletionPaths, source.deletionPaths...)
}

// cleanupToLocked evicts LRU entries until the selected global/namespace
// resource counters fit. capacityMu must be held. Candidates are paginated so
// cache cardinality does not become a request-sized memory allocation.
func (s *Service) cleanupToLocked(
	ctx context.Context,
	targetBytes, targetEntries int64,
	scopeNamespace string,
	exclude ArtifactID,
) (CleanupResult, error) {
	result := CleanupResult{SizeBytes: s.usage.Load(), Entries: s.entries.Load()}
	if targetBytes < 0 || targetEntries < 0 {
		return result, ErrInsufficientStorage
	}
	current := func() resourceUsage {
		if scopeNamespace == "" {
			return resourceUsage{Bytes: s.usage.Load(), Entries: s.entries.Load()}
		}
		return s.namespaceUsage[scopeNamespace]
	}
	exceedsTarget := func() bool {
		usage := current()
		return usage.Bytes > targetBytes || usage.Entries > targetEntries
	}
	if !exceedsTarget() {
		return result, nil
	}
	const batchSize = 256
	var cursorTime time.Time
	var cursorID uint
	hasCursor := false
	for exceedsTarget() {
		var candidates []db.CompileCacheEntry
		query := s.db.WithContext(ctx).Order("last_accessed ASC, id ASC").Limit(batchSize)
		if scopeNamespace != "" {
			query = query.Where("namespace = ?", scopeNamespace)
		}
		if hasCursor {
			query = query.Where(
				"(last_accessed > ? OR (last_accessed = ? AND id > ?))",
				cursorTime, cursorTime, cursorID,
			)
		}
		if err := query.Find(&candidates).Error; err != nil {
			return result, fmt.Errorf("list compile-cache cleanup candidates: %w", err)
		}
		if len(candidates) == 0 {
			break
		}
		for _, candidate := range candidates {
			cursorTime, cursorID, hasCursor = candidate.LastAccessed, candidate.ID, true
			if !exceedsTarget() {
				break
			}
			if exclude.protocol != "" && candidate.Protocol == string(exclude.protocol) &&
				candidate.Namespace == exclude.namespace && candidate.Key == exclude.key {
				continue
			}
			identity := cacheIdentityParts(Protocol(candidate.Protocol), candidate.Namespace, candidate.Key)
			// A PUT reserves capacity while holding its exact key lock. Never
			// wait on another active key while capacityMu is held.
			unlock, locked := s.keyLocks.tryLock(identity)
			if !locked {
				continue
			}
			var live db.CompileCacheEntry
			lookup := s.db.WithContext(ctx).Where("id = ?", candidate.ID).Limit(1).Find(&live)
			if lookup.Error != nil {
				unlock()
				return result, fmt.Errorf("reload compile-cache cleanup candidate: %w", lookup.Error)
			}
			if lookup.RowsAffected == 0 {
				unlock()
				continue
			}
			if live.UpdatedAt.After(candidate.UpdatedAt) || live.LastAccessed.After(candidate.LastAccessed) {
				unlock()
				continue
			}
			if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := enqueueDeletion(tx, live.StoragePath); err != nil {
					return err
				}
				return tx.Delete(&live).Error
			}); err != nil {
				unlock()
				return result, fmt.Errorf("delete compile-cache cleanup metadata: %w", err)
			}
			s.addCapacityLocked(live.Namespace, -live.Size, -1)
			unlock()
			result.RemovedEntries++
			result.ReclaimedBytes += live.Size
			result.deletionPaths = append(result.deletionPaths, live.StoragePath)
		}
	}
	result.SizeBytes = s.usage.Load()
	result.Entries = s.entries.Load()
	if exceedsTarget() {
		return result, ErrInsufficientStorage
	}
	return result, nil
}

// Stats returns the current in-memory capacity ledger.
func (s *Service) Stats(_ context.Context) (Stats, error) {
	s.capacityMu.Lock()
	defer s.capacityMu.Unlock()
	return Stats{
		SizeBytes: s.usage.Load(), MaxBytes: s.limits.MaxBytes,
		Entries: s.entries.Load(), MaxEntries: s.limits.MaxEntries,
		Hits: s.hits.Load(), NamespaceCount: s.namespaces.Load(),
	}, nil
}

// RunMaintenance coalesces hit metadata and applies the configured high/low
// watermark policy. It returns promptly when the server lifecycle is cancelled.
func (s *Service) RunMaintenance(ctx context.Context) {
	flushTicker := time.NewTicker(5 * time.Second)
	deletionTicker := time.NewTicker(time.Minute)
	cleanupTicker := time.NewTicker(5 * time.Minute)
	defer flushTicker.Stop()
	defer deletionTicker.Stop()
	defer cleanupTicker.Stop()
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.FlushTouches(flushCtx); err != nil {
			zap.L().Warn("final compile-cache hit flush failed", zap.Error(err))
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-flushTicker.C:
			if err := s.FlushTouches(ctx); err != nil {
				zap.L().Warn("compile-cache hit flush failed", zap.Error(err))
			}
		case <-deletionTicker.C:
			if err := s.ProcessPendingDeletions(ctx, 100); err != nil {
				zap.L().Warn("compiler-cache deletion retry failed", zap.Error(err))
			}
		case <-cleanupTicker.C:
			highBytes := s.limits.MaxBytes * int64(s.limits.HighWatermarkPercent) / 100
			highEntries := s.limits.MaxEntries * int64(s.limits.HighWatermarkPercent) / 100
			if s.usage.Load() <= highBytes && s.entries.Load() <= highEntries {
				continue
			}
			if _, err := s.Cleanup(ctx); err != nil {
				zap.L().Warn("compile-cache cleanup failed", zap.Error(err))
			}
		}
	}
}
