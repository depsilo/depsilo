package audit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// AnchorFormat identifies one checkpoint line, on disk and on the wire.
const AnchorFormat = "depsilo/audit-anchor/v1"

// anchorRetryDelay bounds how quickly a failed sink is retried; the checkpoint
// interval still governs successful heartbeats.
const anchorRetryDelay = 60 * time.Second

// ChainCheckpoint is the chain head at one point in time, stored outside the
// database so that rewriting the whole audit chain still contradicts a copy
// the operator already moved elsewhere.
type ChainCheckpoint struct {
	Format    string    `json:"format"`
	CheckedAt time.Time `json:"checked_at"`
	HeadID    uint      `json:"head_id"`
	HeadHash  string    `json:"head_hash"`
}

// ChainHead returns the highest chained audit row. An empty hash means no
// chained row exists yet (fresh install or pre-v8 rows only).
func ChainHead(ctx context.Context, database *gorm.DB) (uint, string, error) {
	var head db.AuditLog
	err := database.WithContext(ctx).Order("id DESC").Limit(1).Find(&head).Error
	if err != nil {
		return 0, "", fmt.Errorf("read audit chain head: %w", err)
	}
	return head.ID, head.Hash, nil
}

// Anchor writes chain-head checkpoints to an append-only file and/or pushes
// them to a remote endpoint. Each sink deduplicates on the head, so an
// unchanged chain produces no traffic and a retry after a failure re-sends the
// same checkpoint rather than skipping it.
type Anchor struct {
	db        *gorm.DB
	filePath  string
	remoteURL string
	token     string
	interval  time.Duration
	client    *http.Client
	now       func() time.Time

	mu     sync.Mutex
	file   anchorSink
	remote anchorSink
}

type anchorSink struct {
	configured    bool
	seeded        bool
	lastID        uint
	lastHash      string
	lastSuccessAt time.Time
	lastError     string
	nextAttempt   time.Time
}

// NewAnchor creates an anchor writer. An empty file path and URL disables
// anchoring; an invalid remote URL is a startup error.
func NewAnchor(database *gorm.DB, filePath, remoteURL, token string, interval time.Duration) (*Anchor, error) {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL != "" {
		parsed, err := url.Parse(remoteURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, fmt.Errorf("audit checkpoint_url must be an absolute http(s) URL")
		}
	}
	filePath = strings.TrimSpace(filePath)
	return &Anchor{
		db:        database,
		filePath:  filePath,
		remoteURL: remoteURL,
		token:     strings.TrimSpace(token),
		interval:  interval,
		client:    &http.Client{Timeout: 15 * time.Second},
		now:       func() time.Time { return time.Now().UTC() },
		file:      anchorSink{configured: filePath != ""},
		remote:    anchorSink{configured: remoteURL != ""},
	}, nil
}

// Enabled reports whether at least one sink is configured.
func (a *Anchor) Enabled() bool {
	return a != nil && (a.file.configured || a.remote.configured)
}

// CheckpointPath returns the configured local checkpoint file, if any.
func (a *Anchor) CheckpointPath() string {
	if a == nil {
		return ""
	}
	return a.filePath
}

// Start writes an initial checkpoint and then one per interval.
func (a *Anchor) Start(ctx context.Context) {
	if !a.Enabled() {
		return
	}
	if _, err := a.WriteCheckpoint(ctx); err != nil && !errors.Is(err, context.Canceled) {
		zap.L().Warn("audit anchor: initial checkpoint failed", zap.Error(err))
	}
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := a.WriteCheckpoint(ctx); err != nil && !errors.Is(err, context.Canceled) {
				zap.L().Warn("audit anchor: checkpoint failed", zap.Error(err))
			}
		}
	}
}

// WriteCheckpoint delivers the current head to every configured sink that has
// not already accepted it. It reports whether at least one sink accepted.
func (a *Anchor) WriteCheckpoint(ctx context.Context) (bool, error) {
	if !a.Enabled() {
		return false, nil
	}
	headID, headHash, err := ChainHead(ctx, a.db)
	if err != nil {
		return false, err
	}
	if headHash == "" {
		// Nothing chained yet: anchoring an empty head would only add noise.
		return false, nil
	}
	checkpoint := ChainCheckpoint{
		Format: AnchorFormat, CheckedAt: a.now(),
		HeadID: headID, HeadHash: headHash,
	}
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		return false, err
	}
	now := a.now()
	wrote := false
	var errs []error
	if a.file.configured {
		delivered, err := a.writeFileSink(encoded, headID, headHash, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("checkpoint file: %w", err))
		} else if delivered {
			wrote = true
		}
	}
	if a.remote.configured {
		delivered, err := a.pushRemoteSink(ctx, encoded, headID, headHash, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("checkpoint url: %w", err))
		} else if delivered {
			wrote = true
		}
	}
	return wrote, errors.Join(errs...)
}

func (a *Anchor) writeFileSink(encoded []byte, headID uint, headHash string, now time.Time) (bool, error) {
	a.mu.Lock()
	if !a.file.seeded {
		if last, ok := lastCheckpoint(a.filePath); ok {
			a.file.lastID, a.file.lastHash = last.HeadID, last.HeadHash
		}
		a.file.seeded = true
	}
	if a.file.lastID == headID && a.file.lastHash == headHash {
		a.mu.Unlock()
		return false, nil
	}
	if now.Before(a.file.nextAttempt) {
		a.mu.Unlock()
		return false, nil
	}
	a.mu.Unlock()

	path := a.filePath
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return false, a.failFile(err, now)
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false, a.failFile(err, now)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		return false, a.failFile(err, now)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return false, a.failFile(err, now)
	}
	if err := file.Close(); err != nil {
		return false, a.failFile(err, now)
	}
	a.mu.Lock()
	a.file.lastID, a.file.lastHash = headID, headHash
	a.file.lastError = ""
	a.file.lastSuccessAt = now
	a.file.nextAttempt = time.Time{}
	a.mu.Unlock()
	return true, nil
}

func (a *Anchor) failFile(err error, now time.Time) error {
	a.mu.Lock()
	a.file.lastError = err.Error()
	a.file.nextAttempt = now.Add(a.retryDelay())
	a.mu.Unlock()
	return err
}

// pushRemoteSink posts one NDJSON checkpoint. Receivers deduplicate on
// (head_id, head_hash); a failed attempt is retried after the retry delay.
func (a *Anchor) pushRemoteSink(ctx context.Context, encoded []byte, headID uint, headHash string, now time.Time) (bool, error) {
	a.mu.Lock()
	if a.remote.lastID == headID && a.remote.lastHash == headHash {
		a.mu.Unlock()
		return false, nil
	}
	if now.Before(a.remote.nextAttempt) {
		a.mu.Unlock()
		return false, nil
	}
	a.mu.Unlock()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.remoteURL, bytes.NewReader(append(encoded, '\n')))
	if err != nil {
		return false, a.failRemote(err, now)
	}
	request.Header.Set("Content-Type", "application/x-ndjson")
	if a.token != "" {
		request.Header.Set("Authorization", "Bearer "+a.token)
	}
	response, err := a.client.Do(request)
	if err != nil {
		return false, a.failRemote(err, now)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, a.failRemote(fmt.Errorf("anchor endpoint returned HTTP %d", response.StatusCode), now)
	}
	a.mu.Lock()
	a.remote.seeded = true
	a.remote.lastID, a.remote.lastHash = headID, headHash
	a.remote.lastError = ""
	a.remote.lastSuccessAt = now
	a.remote.nextAttempt = time.Time{}
	a.mu.Unlock()
	return true, nil
}

func (a *Anchor) failRemote(err error, now time.Time) error {
	a.mu.Lock()
	a.remote.lastError = err.Error()
	a.remote.nextAttempt = now.Add(a.retryDelay())
	a.mu.Unlock()
	return err
}

func (a *Anchor) retryDelay() time.Duration {
	if a.interval < anchorRetryDelay {
		return a.interval
	}
	return anchorRetryDelay
}

// AnchorFeedStatus is the live view of both sinks for the admin surface. It
// deliberately omits the remote URL and token, which may embed credentials.
type AnchorFeedStatus struct {
	FileConfigured      bool      `json:"file_configured"`
	FileLastSuccessAt   time.Time `json:"file_last_success_at,omitempty"`
	FileLastError       string    `json:"file_last_error,omitempty"`
	RemoteConfigured    bool      `json:"remote_configured"`
	RemoteLastSuccessAt time.Time `json:"remote_last_success_at,omitempty"`
	RemoteHeadID        uint      `json:"remote_head_id,omitempty"`
	RemoteLastError     string    `json:"remote_last_error,omitempty"`
}

// FeedStatus reports the current sink state.
func (a *Anchor) FeedStatus() AnchorFeedStatus {
	if a == nil {
		return AnchorFeedStatus{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return AnchorFeedStatus{
		FileConfigured:      a.file.configured,
		FileLastSuccessAt:   a.file.lastSuccessAt,
		FileLastError:       a.file.lastError,
		RemoteConfigured:    a.remote.configured,
		RemoteLastSuccessAt: a.remote.lastSuccessAt,
		RemoteHeadID:        a.remote.lastID,
		RemoteLastError:     a.remote.lastError,
	}
}

// LoadCheckpoints reads every valid checkpoint line. Malformed lines are
// skipped with a count so a truncated tail does not hide the rest.
func LoadCheckpoints(path string) ([]ChainCheckpoint, int, error) {
	if strings.TrimSpace(path) == "" {
		return nil, 0, nil
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("open anchor file: %w", err)
	}
	defer file.Close()
	var checkpoints []ChainCheckpoint
	invalid := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var checkpoint ChainCheckpoint
		if err := json.Unmarshal([]byte(line), &checkpoint); err != nil ||
			checkpoint.Format != AnchorFormat || checkpoint.HeadHash == "" {
			invalid++
			continue
		}
		checkpoints = append(checkpoints, checkpoint)
	}
	if err := scanner.Err(); err != nil {
		return nil, invalid, fmt.Errorf("read anchor file: %w", err)
	}
	return checkpoints, invalid, nil
}

func lastCheckpoint(path string) (ChainCheckpoint, bool) {
	checkpoints, _, err := LoadCheckpoints(path)
	if err != nil || len(checkpoints) == 0 {
		return ChainCheckpoint{}, false
	}
	return checkpoints[len(checkpoints)-1], true
}

// AnchorReport describes the cross-check between stored checkpoints and the
// current database.
type AnchorReport struct {
	Configured      bool      `json:"configured"`
	Path            string    `json:"path,omitempty"`
	Checkpoints     int       `json:"checkpoints"`
	InvalidLines    int       `json:"invalid_lines,omitempty"`
	LatestCheckedAt time.Time `json:"latest_checked_at,omitempty"`
	LatestHeadID    uint      `json:"latest_head_id,omitempty"`
	LatestHeadHash  string    `json:"latest_head_hash,omitempty"`
	OK              bool      `json:"ok"`
	BrokenAtID      uint      `json:"broken_at_id,omitempty"`
	Reason          string    `json:"reason,omitempty"`
}

// VerifyAnchors checks that every checkpointed (id, hash) pair still exists in
// the database unchanged. This catches what the chain walk alone cannot: a
// rewritten chain whose links are internally consistent, including truncating
// the head.
func VerifyAnchors(ctx context.Context, database *gorm.DB, path string) (AnchorReport, error) {
	report := AnchorReport{Configured: strings.TrimSpace(path) != "", Path: path, OK: true}
	if !report.Configured {
		return report, nil
	}
	checkpoints, invalid, err := LoadCheckpoints(path)
	if err != nil {
		return report, err
	}
	report.Checkpoints = len(checkpoints)
	report.InvalidLines = invalid
	if len(checkpoints) == 0 {
		return report, nil
	}
	latest := checkpoints[len(checkpoints)-1]
	report.LatestCheckedAt = latest.CheckedAt
	report.LatestHeadID = latest.HeadID
	report.LatestHeadHash = latest.HeadHash
	for _, checkpoint := range checkpoints {
		var row db.AuditLog
		err := database.WithContext(ctx).First(&row, checkpoint.HeadID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			report.OK = false
			report.BrokenAtID = checkpoint.HeadID
			report.Reason = fmt.Sprintf("checkpointed row %d is missing", checkpoint.HeadID)
			return report, nil
		}
		if err != nil {
			return report, fmt.Errorf("read checkpointed row %d: %w", checkpoint.HeadID, err)
		}
		if row.Hash != checkpoint.HeadHash {
			report.OK = false
			report.BrokenAtID = checkpoint.HeadID
			report.Reason = fmt.Sprintf("row %d no longer matches its checkpoint", checkpoint.HeadID)
			return report, nil
		}
	}
	return report, nil
}
