package upstreamupdates

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

const (
	eventDetailLimit   = 512
	eventWriteAttempts = 3
	historyCursorV1    = 1
	defaultHistoryPage = 50
	maxHistoryPage     = 500
	maxLegacyOffset    = 100000
	maxCursorLength    = 4096
)

var (
	// ErrInvalidObservation identifies a malformed write without exposing
	// storage details to the producer.
	ErrInvalidObservation = errors.New("invalid upstream update observation")
	// ErrInvalidHistoryQuery identifies malformed filters or cursors so HTTP
	// adapters can map them to a stable client error.
	ErrInvalidHistoryQuery = errors.New("invalid upstream update history query")
)

// Observation is one completed proactive metadata check. CacheEntryID is the
// durable subject identity; the visible fields are snapshots for Operators.
type Observation struct {
	CacheEntryID uint
	Ecosystem    string
	Upstream     string
	Package      string
	Result       string
	Detail       string
	Latency      time.Duration
	ObservedAt   time.Time
}

// Receipt describes whether Record opened a new episode or extended the
// current consecutive unchanged/error episode.
type Receipt struct {
	EventID         uint
	Coalesced       bool
	OccurrenceCount uint64
}

// HistoryQuery supports stable cursor pagination and a compatibility offset
// path. Cursor and LegacyOffset are mutually exclusive.
type HistoryQuery struct {
	Limit        int
	Cursor       string
	LegacyOffset *int
	Ecosystem    string
	Upstream     string
	Package      string
	Result       string
}

// HistoryEvent is the read-only domain projection returned to API adapters.
// CreatedAt remains the legacy first-observed field.
type HistoryEvent struct {
	ID              uint      `json:"id"`
	CacheEntryID    uint      `json:"cache_entry_id"`
	Ecosystem       string    `json:"ecosystem"`
	Upstream        string    `json:"upstream"`
	Package         string    `json:"package"`
	Result          string    `json:"result"`
	Detail          string    `json:"detail"`
	LatencyMs       int64     `json:"latency_ms"`
	OccurrenceCount uint64    `json:"occurrence_count"`
	FirstSeenAt     time.Time `json:"first_seen_at"`
	LastSeenAt      time.Time `json:"last_seen_at"`
	CreatedAt       time.Time `json:"created_at"`
}

// HistoryPage is a stable snapshot page. Total counts episodes, while each
// item's OccurrenceCount reports how many observations that episode contains.
type HistoryPage struct {
	Items      []HistoryEvent
	Total      int64
	NextCursor string
}

// History owns event normalization, episode coalescing and pagination. GORM
// remains an implementation detail because SQLite is a local-substitutable
// dependency and both production and tests use the real adapter.
type History struct {
	database *gorm.DB
	recordMu sync.Mutex
}

// NewHistory constructs the durable upstream update history Module.
func NewHistory(database *gorm.DB) (*History, error) {
	if database == nil {
		return nil, errors.New("upstream update history: database is required")
	}
	return &History{database: database}, nil
}

// Record stores an observation. Consecutive semantically identical
// unchanged/error observations extend one episode; updated observations always
// create an immutable history item.
func (h *History) Record(ctx context.Context, observation Observation) (Receipt, error) {
	if ctx == nil {
		return Receipt{}, fmt.Errorf("%w: context is required", ErrInvalidObservation)
	}
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	normalized, err := normalizeObservation(observation)
	if err != nil {
		return Receipt{}, err
	}

	// Producer sweeps are serial today. The lock also makes one shared History
	// instance safe for concurrent callers without a second durable state table.
	h.recordMu.Lock()
	defer h.recordMu.Unlock()

	var receipt Receipt
	for attempt := 0; attempt < eventWriteAttempts; attempt++ {
		receipt = Receipt{}
		err = h.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var latest db.UpstreamUpdateEvent
			queryErr := tx.
				Where("cache_entry_id = ?", normalized.CacheEntryID).
				Order("id DESC").
				First(&latest).Error
			if queryErr != nil && !errors.Is(queryErr, gorm.ErrRecordNotFound) {
				return queryErr
			}

			if queryErr == nil && canCoalesce(latest, normalized) {
				count := latest.OccurrenceCount
				if count == 0 {
					count = 1
				}
				count++
				lastSeen := normalized.ObservedAt
				if latest.LastSeenAt.After(lastSeen) {
					lastSeen = latest.LastSeenAt
				}
				if latest.CreatedAt.After(lastSeen) {
					lastSeen = latest.CreatedAt
				}
				if updateErr := tx.Model(&db.UpstreamUpdateEvent{}).
					Where("id = ?", latest.ID).
					Updates(map[string]any{
						"latency_ms":       normalized.Latency.Milliseconds(),
						"last_seen_at":     lastSeen,
						"occurrence_count": count,
					}).Error; updateErr != nil {
					return updateErr
				}
				receipt = Receipt{EventID: latest.ID, Coalesced: true, OccurrenceCount: count}
				return nil
			}

			event := db.UpstreamUpdateEvent{
				CacheEntryID:    normalized.CacheEntryID,
				Ecosystem:       normalized.Ecosystem,
				Upstream:        normalized.Upstream,
				Package:         normalized.Package,
				Result:          normalized.Result,
				Detail:          normalized.Detail,
				LatencyMs:       normalized.Latency.Milliseconds(),
				OccurrenceCount: 1,
				LastSeenAt:      normalized.ObservedAt,
				CreatedAt:       normalized.ObservedAt,
			}
			if createErr := tx.Create(&event).Error; createErr != nil {
				return createErr
			}
			receipt = Receipt{EventID: event.ID, OccurrenceCount: 1}
			return nil
		})
		if err == nil {
			return receipt, nil
		}
		if attempt+1 == eventWriteAttempts {
			break
		}
		if waitErr := waitForHistoryRetry(ctx, attempt); waitErr != nil {
			return Receipt{}, errors.Join(err, waitErr)
		}
	}
	return Receipt{}, fmt.Errorf("record upstream update observation: %w", err)
}

// List returns either a stable keyset page or the explicit legacy offset page.
func (h *History) List(ctx context.Context, query HistoryQuery) (HistoryPage, error) {
	if ctx == nil {
		return HistoryPage{}, fmt.Errorf("%w: context is required", ErrInvalidHistoryQuery)
	}
	if err := ctx.Err(); err != nil {
		return HistoryPage{}, err
	}
	normalized, err := normalizeHistoryQuery(query)
	if err != nil {
		return HistoryPage{}, err
	}
	if normalized.legacyOffset != nil {
		return h.listOffset(ctx, normalized)
	}
	return h.listCursor(ctx, normalized)
}

type normalizedObservation struct {
	CacheEntryID uint
	Ecosystem    string
	Upstream     string
	Package      string
	Result       string
	Detail       string
	Latency      time.Duration
	ObservedAt   time.Time
}

func normalizeObservation(observation Observation) (normalizedObservation, error) {
	result := strings.TrimSpace(observation.Result)
	if observation.CacheEntryID == 0 {
		return normalizedObservation{}, fmt.Errorf("%w: cache entry ID is required", ErrInvalidObservation)
	}
	if !validHistoryResult(result) {
		return normalizedObservation{}, fmt.Errorf("%w: unsupported result", ErrInvalidObservation)
	}
	ecosystem := sanitizeEventValue(observation.Ecosystem, 32)
	packageName := sanitizeEventValue(observation.Package, 256)
	if ecosystem == "" || packageName == "" {
		return normalizedObservation{}, fmt.Errorf("%w: ecosystem and package are required", ErrInvalidObservation)
	}
	latency := observation.Latency
	if latency < 0 {
		latency = 0
	}
	observedAt := observation.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	return normalizedObservation{
		CacheEntryID: observation.CacheEntryID,
		Ecosystem:    ecosystem,
		Upstream:     sanitizeEventValue(observation.Upstream, 128),
		Package:      packageName,
		Result:       result,
		Detail:       sanitizeEventValue(observation.Detail, eventDetailLimit),
		Latency:      latency,
		ObservedAt:   observedAt.UTC(),
	}, nil
}

func canCoalesce(latest db.UpstreamUpdateEvent, observation normalizedObservation) bool {
	if observation.Result != ResultUnchanged && observation.Result != ResultError {
		return false
	}
	return latest.CacheEntryID == observation.CacheEntryID &&
		latest.Ecosystem == observation.Ecosystem &&
		latest.Upstream == observation.Upstream &&
		latest.Package == observation.Package &&
		latest.Result == observation.Result &&
		latest.Detail == observation.Detail
}

func waitForHistoryRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(1<<attempt) * 25 * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type normalizedHistoryQuery struct {
	limit        int
	cursor       string
	legacyOffset *int
	ecosystem    string
	upstream     string
	packageName  string
	result       string
	fingerprint  string
}

type historyCursor struct {
	Version        int    `json:"v"`
	SnapshotMaxID  uint   `json:"max_id"`
	AfterCreatedAt string `json:"after_created_at"`
	AfterID        uint   `json:"after_id"`
	FilterHash     string `json:"filter_hash"`
}

func normalizeHistoryQuery(query HistoryQuery) (normalizedHistoryQuery, error) {
	limit := query.Limit
	if limit == 0 {
		limit = defaultHistoryPage
	}
	if limit < 1 || limit > maxHistoryPage {
		return normalizedHistoryQuery{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidHistoryQuery, maxHistoryPage)
	}
	cursor := strings.TrimSpace(query.Cursor)
	if cursor != "" && query.LegacyOffset != nil {
		return normalizedHistoryQuery{}, fmt.Errorf("%w: cursor and offset are mutually exclusive", ErrInvalidHistoryQuery)
	}
	var offset *int
	if query.LegacyOffset != nil {
		value := *query.LegacyOffset
		if value < 0 || value > maxLegacyOffset {
			return normalizedHistoryQuery{}, fmt.Errorf("%w: offset is out of range", ErrInvalidHistoryQuery)
		}
		offset = &value
	}
	result := strings.TrimSpace(query.Result)
	if result != "" && !validHistoryResult(result) {
		return normalizedHistoryQuery{}, fmt.Errorf("%w: unsupported result", ErrInvalidHistoryQuery)
	}
	normalized := normalizedHistoryQuery{
		limit:        limit,
		cursor:       cursor,
		legacyOffset: offset,
		ecosystem:    strings.TrimSpace(query.Ecosystem),
		upstream:     strings.TrimSpace(query.Upstream),
		packageName:  strings.TrimSpace(query.Package),
		result:       result,
	}
	normalized.fingerprint = historyFilterFingerprint(normalized)
	return normalized, nil
}

func validHistoryResult(result string) bool {
	return result == ResultUpdated || result == ResultUnchanged || result == ResultError
}

func historyFilterFingerprint(query normalizedHistoryQuery) string {
	canonical := struct {
		Ecosystem string `json:"ecosystem"`
		Upstream  string `json:"upstream"`
		Package   string `json:"package"`
		Result    string `json:"result"`
	}{query.ecosystem, query.upstream, query.packageName, query.result}
	payload, _ := json.Marshal(canonical)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (h *History) filtered(ctx context.Context, query normalizedHistoryQuery) *gorm.DB {
	database := h.database.WithContext(ctx).Model(&db.UpstreamUpdateEvent{})
	if query.ecosystem != "" {
		database = database.Where("ecosystem = ?", query.ecosystem)
	}
	if query.upstream != "" {
		database = database.Where("upstream = ?", query.upstream)
	}
	if query.result != "" {
		database = database.Where("result = ?", query.result)
	}
	if query.packageName != "" {
		database = database.Where(`package LIKE ? ESCAPE '\'`, "%"+escapeLike(query.packageName)+"%")
	}
	return database
}

func (h *History) listOffset(ctx context.Context, query normalizedHistoryQuery) (HistoryPage, error) {
	var total int64
	if err := h.filtered(ctx, query).Count(&total).Error; err != nil {
		return HistoryPage{}, fmt.Errorf("count upstream update history: %w", err)
	}
	var rows []db.UpstreamUpdateEvent
	if err := h.filtered(ctx, query).
		Order("created_at DESC, id DESC").
		Limit(query.limit).
		Offset(*query.legacyOffset).
		Find(&rows).Error; err != nil {
		return HistoryPage{}, fmt.Errorf("list upstream update history: %w", err)
	}
	return HistoryPage{Items: projectHistoryEvents(rows), Total: total}, nil
}

func (h *History) listCursor(ctx context.Context, query normalizedHistoryQuery) (HistoryPage, error) {
	var cursor historyCursor
	var afterTime time.Time
	if query.cursor == "" {
		if err := h.filtered(ctx, query).
			Select("COALESCE(MAX(id), 0)").
			Scan(&cursor.SnapshotMaxID).Error; err != nil {
			return HistoryPage{}, fmt.Errorf("capture upstream update history snapshot: %w", err)
		}
		cursor.Version = historyCursorV1
		cursor.FilterHash = query.fingerprint
		if cursor.SnapshotMaxID == 0 {
			return HistoryPage{Items: []HistoryEvent{}}, nil
		}
	} else {
		var err error
		cursor, afterTime, err = decodeHistoryCursor(query.cursor, query.fingerprint)
		if err != nil {
			return HistoryPage{}, err
		}
	}
	var snapshotTotal int64
	if err := h.filtered(ctx, query).
		Where("id <= ?", cursor.SnapshotMaxID).
		Count(&snapshotTotal).Error; err != nil {
		return HistoryPage{}, fmt.Errorf("count upstream update history snapshot: %w", err)
	}

	database := h.filtered(ctx, query).Where("id <= ?", cursor.SnapshotMaxID)
	if !afterTime.IsZero() {
		database = database.Where("(created_at, id) < (?, ?)", afterTime, cursor.AfterID)
	}
	var rows []db.UpstreamUpdateEvent
	if err := database.
		Order("created_at DESC, id DESC").
		Limit(query.limit + 1).
		Find(&rows).Error; err != nil {
		return HistoryPage{}, fmt.Errorf("list upstream update history snapshot: %w", err)
	}

	page := HistoryPage{Items: []HistoryEvent{}, Total: snapshotTotal}
	if len(rows) > query.limit {
		rows = rows[:query.limit]
		last := rows[len(rows)-1]
		cursor.AfterCreatedAt = last.CreatedAt.UTC().Format(time.RFC3339Nano)
		cursor.AfterID = last.ID
		page.NextCursor = encodeHistoryCursor(cursor)
	}
	page.Items = projectHistoryEvents(rows)
	return page, nil
}

func encodeHistoryCursor(cursor historyCursor) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeHistoryCursor(encoded, filterHash string) (historyCursor, time.Time, error) {
	if len(encoded) > maxCursorLength {
		return historyCursor{}, time.Time{}, fmt.Errorf("%w: cursor is too long", ErrInvalidHistoryQuery)
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return historyCursor{}, time.Time{}, fmt.Errorf("%w: malformed cursor", ErrInvalidHistoryQuery)
	}
	var cursor historyCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return historyCursor{}, time.Time{}, fmt.Errorf("%w: malformed cursor", ErrInvalidHistoryQuery)
	}
	if cursor.Version != historyCursorV1 ||
		cursor.SnapshotMaxID == 0 ||
		cursor.AfterID == 0 ||
		cursor.AfterID > cursor.SnapshotMaxID ||
		cursor.FilterHash != filterHash {
		return historyCursor{}, time.Time{}, fmt.Errorf("%w: cursor does not match this query", ErrInvalidHistoryQuery)
	}
	afterTime, err := time.Parse(time.RFC3339Nano, cursor.AfterCreatedAt)
	if err != nil || afterTime.IsZero() {
		return historyCursor{}, time.Time{}, fmt.Errorf("%w: malformed cursor position", ErrInvalidHistoryQuery)
	}
	return cursor, afterTime.UTC(), nil
}

func projectHistoryEvents(rows []db.UpstreamUpdateEvent) []HistoryEvent {
	items := make([]HistoryEvent, 0, len(rows))
	for _, row := range rows {
		count := row.OccurrenceCount
		if count == 0 {
			count = 1
		}
		lastSeen := row.LastSeenAt
		if lastSeen.IsZero() {
			lastSeen = row.CreatedAt
		}
		items = append(items, HistoryEvent{
			ID:              row.ID,
			CacheEntryID:    row.CacheEntryID,
			Ecosystem:       row.Ecosystem,
			Upstream:        row.Upstream,
			Package:         row.Package,
			Result:          row.Result,
			Detail:          row.Detail,
			LatencyMs:       row.LatencyMs,
			OccurrenceCount: count,
			FirstSeenAt:     row.CreatedAt,
			LastSeenAt:      lastSeen,
			CreatedAt:       row.CreatedAt,
		})
	}
	return items
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func sanitizeEventValue(value string, limit int) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			value = "***"
		} else {
			value = (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String() + "/***"
		}
	}
	return truncateUTF8(value, limit)
}

func truncateUTF8(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
