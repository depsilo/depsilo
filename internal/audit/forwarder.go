package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

const (
	forwarderBatchSize        = 200
	forwarderMaxBatchesPerRun = 10
	forwarderTick             = 5 * time.Second
	forwarderMinBackoff       = 10 * time.Second
	forwarderMaxBackoff       = 5 * time.Minute
	forwarderRequestTimeout   = 15 * time.Second
)

// Forwarder streams audit_log rows to the configured collectors. The audit log
// table is the durable buffer; delivery is at-least-once with a per-exporter
// cursor, so a collector outage never loses records and a restart resumes
// where the last successful batch ended.
type Forwarder struct {
	db     *gorm.DB
	client *http.Client
	host   string
	batch  int
	tick   time.Duration
	now    func() time.Time

	mu       sync.Mutex
	failures map[uint]*forwarderFailure
}

type forwarderFailure struct {
	count     int
	nextRetry time.Time
}

func NewForwarder(database *gorm.DB) *Forwarder {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "depsilo"
	}
	return &Forwarder{
		db:       database,
		client:   &http.Client{Timeout: forwarderRequestTimeout},
		host:     host,
		batch:    forwarderBatchSize,
		tick:     forwarderTick,
		now:      func() time.Time { return time.Now().UTC() },
		failures: map[uint]*forwarderFailure{},
	}
}

// Start runs the forwarding loop until ctx is cancelled.
func (f *Forwarder) Start(ctx context.Context) {
	ticker := time.NewTicker(f.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := f.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				zap.L().Warn("audit forwarder run failed", zap.Error(err))
			}
		}
	}
}

// RunOnce delivers every currently available batch for each enabled exporter.
// It returns the number of records delivered.
func (f *Forwarder) RunOnce(ctx context.Context) (int, error) {
	var exporters []db.AuditExporter
	if err := f.db.WithContext(ctx).Where("enabled = ?", true).Order("id").Find(&exporters).Error; err != nil {
		return 0, fmt.Errorf("load audit exporters: %w", err)
	}
	delivered := 0
	var errs []error
	for _, exporter := range exporters {
		if !f.ready(exporter.ID) {
			continue
		}
		for batch := 0; batch < forwarderMaxBatchesPerRun; batch++ {
			count, more, err := f.forwardBatch(ctx, exporter)
			delivered += count
			if err != nil {
				errs = append(errs, fmt.Errorf("exporter %s: %w", exporter.Name, err))
				break
			}
			if !more {
				break
			}
		}
	}
	return delivered, errors.Join(errs...)
}

// forwardBatch delivers at most one batch and reports whether more rows remain.
func (f *Forwarder) forwardBatch(ctx context.Context, exporter db.AuditExporter) (int, bool, error) {
	var rows []db.AuditLog
	if err := f.db.WithContext(ctx).
		Where("id > ?", exporter.Cursor).
		Order("id").
		Limit(f.batch).
		Find(&rows).Error; err != nil {
		return 0, false, fmt.Errorf("read audit rows: %w", err)
	}
	if len(rows) == 0 {
		return 0, false, nil
	}
	lastID := rows[len(rows)-1].ID
	more := len(rows) == f.batch

	selected := FilterAuditRows(rows, exporter.Events)
	now := f.now()
	if len(selected) > 0 {
		if err := f.deliver(ctx, exporter, selected); err != nil {
			f.recordFailure(exporter.ID)
			_ = f.db.WithContext(ctx).Model(&db.AuditExporter{}).
				Where("id = ?", exporter.ID).
				Updates(map[string]interface{}{
					"last_error":      truncateError(err.Error()),
					"last_attempt_at": now,
				}).Error
			return 0, more, err
		}
	}

	f.clearFailure(exporter.ID)
	if err := f.db.WithContext(ctx).Model(&db.AuditExporter{}).
		Where("id = ? AND cursor < ?", exporter.ID, lastID).
		Updates(map[string]interface{}{
			"cursor":          lastID,
			"delivered_count": gorm.Expr("delivered_count + ?", len(selected)),
			"last_error":      "",
			"last_attempt_at": now,
			"last_success_at": now,
		}).Error; err != nil {
		return 0, more, fmt.Errorf("advance cursor: %w", err)
	}
	return len(selected), more, nil
}

func (f *Forwarder) deliver(ctx context.Context, exporter db.AuditExporter, rows []db.AuditLog) error {
	payload, contentType, err := BuildAuditPayload(exporter, rows, f.host)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, exporter.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", contentType)
	if exporter.Token != "" {
		switch exporter.Kind {
		case db.AuditExporterKindSplunkHEC:
			request.Header.Set("Authorization", "Splunk "+exporter.Token)
		default:
			request.Header.Set("Authorization", "Bearer "+exporter.Token)
		}
	}
	response, err := f.client.Do(request)
	if err != nil {
		return fmt.Errorf("send batch: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("collector returned HTTP %d", response.StatusCode)
	}
	return nil
}

// SendTest delivers a synthetic audit event through the real delivery path so
// the operator can verify URL, token, and format without waiting for traffic.
func (f *Forwarder) SendTest(ctx context.Context, exporter db.AuditExporter) error {
	sample := db.AuditLog{
		Action:      "exporter_test",
		CacheResult: "test",
		StatusCode:  http.StatusOK,
		CreatedAt:   f.now(),
	}
	return f.deliver(ctx, exporter, []db.AuditLog{sample})
}

func (f *Forwarder) ready(id uint) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	failure := f.failures[id]
	return failure == nil || !f.now().Before(failure.nextRetry)
}

func (f *Forwarder) recordFailure(id uint) {
	f.mu.Lock()
	defer f.mu.Unlock()
	failure := f.failures[id]
	if failure == nil {
		failure = &forwarderFailure{}
		f.failures[id] = failure
	}
	failure.count++
	backoff := forwarderMinBackoff << (failure.count - 1)
	if backoff > forwarderMaxBackoff || backoff <= 0 {
		backoff = forwarderMaxBackoff
	}
	failure.nextRetry = f.now().Add(backoff)
}

func (f *Forwarder) clearFailure(id uint) {
	f.mu.Lock()
	delete(f.failures, id)
	f.mu.Unlock()
}

// FilterAuditRows keeps rows whose action or cache_result matches the
// exporter's event list. "*" (or an empty list) keeps everything.
func FilterAuditRows(rows []db.AuditLog, events string) []db.AuditLog {
	tokens := parseEventFilter(events)
	if len(tokens) == 0 {
		return rows
	}
	filtered := make([]db.AuditLog, 0, len(rows))
	for _, row := range rows {
		if tokens[strings.ToLower(row.Action)] || tokens[strings.ToLower(row.CacheResult)] {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func parseEventFilter(events string) map[string]bool {
	tokens := map[string]bool{}
	for _, token := range strings.Split(events, ",") {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		if token == "*" {
			return nil
		}
		tokens[token] = true
	}
	return tokens
}

// BuildAuditPayload renders one batch for the exporter's collector format.
func BuildAuditPayload(exporter db.AuditExporter, rows []db.AuditLog, host string) ([]byte, string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	switch exporter.Kind {
	case db.AuditExporterKindNDJSON:
		for _, row := range rows {
			if err := encoder.Encode(row); err != nil {
				return nil, "", fmt.Errorf("encode ndjson row: %w", err)
			}
		}
		return buffer.Bytes(), "application/x-ndjson", nil
	case db.AuditExporterKindSplunkHEC:
		for _, row := range rows {
			event := map[string]interface{}{
				"time":       row.CreatedAt.Unix(),
				"host":       host,
				"source":     "depsilo",
				"sourcetype": "depsilo:audit",
				"event":      row,
			}
			if err := encoder.Encode(event); err != nil {
				return nil, "", fmt.Errorf("encode splunk event: %w", err)
			}
		}
		return buffer.Bytes(), "application/json", nil
	default:
		return nil, "", fmt.Errorf("unsupported audit exporter kind %q", exporter.Kind)
	}
}

func truncateError(message string) string {
	const limit = 500
	if len(message) <= limit {
		return message
	}
	return message[:limit]
}
