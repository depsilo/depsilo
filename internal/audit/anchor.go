package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// AnchorFormat identifies one checkpoint line.
const AnchorFormat = "depsilo/audit-anchor/v1"

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

// Anchor writes chain-head checkpoints to an append-only file.
type Anchor struct {
	db       *gorm.DB
	path     string
	interval time.Duration

	mu        sync.Mutex
	lastID    uint
	lastHash  string
	lastError string
	lastWrite time.Time
	haveLast  bool
}

// NewAnchor creates an anchor writer. An empty path disables it.
func NewAnchor(database *gorm.DB, path string, interval time.Duration) *Anchor {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	return &Anchor{db: database, path: strings.TrimSpace(path), interval: interval}
}

// Enabled reports whether a checkpoint file is configured.
func (a *Anchor) Enabled() bool { return a != nil && a.path != "" }

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

// WriteCheckpoint appends the current head unless the last checkpoint already
// names it. It reports whether a new line was written.
func (a *Anchor) WriteCheckpoint(ctx context.Context) (bool, error) {
	if !a.Enabled() {
		return false, nil
	}
	headID, headHash, err := ChainHead(ctx, a.db)
	if err != nil {
		a.recordError(err)
		return false, err
	}
	if headHash == "" {
		// Nothing chained yet: anchoring an empty head would only add noise.
		return false, nil
	}
	a.mu.Lock()
	haveLast, lastID, lastHash := a.haveLast, a.lastID, a.lastHash
	a.mu.Unlock()
	if !haveLast {
		if last, ok := lastCheckpoint(a.path); ok {
			haveLast, lastID, lastHash = true, last.HeadID, last.HeadHash
			a.mu.Lock()
			a.haveLast, a.lastID, a.lastHash = haveLast, lastID, lastHash
			a.mu.Unlock()
		}
	}
	if haveLast && lastID == headID && lastHash == headHash {
		return false, nil
	}
	checkpoint := ChainCheckpoint{
		Format: AnchorFormat, CheckedAt: time.Now().UTC(),
		HeadID: headID, HeadHash: headHash,
	}
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		a.recordError(err)
		return false, err
	}
	if dir := filepath.Dir(a.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			a.recordError(err)
			return false, fmt.Errorf("create anchor directory: %w", err)
		}
	}
	file, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		a.recordError(err)
		return false, fmt.Errorf("open anchor file: %w", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		a.recordError(err)
		return false, fmt.Errorf("append anchor checkpoint: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		a.recordError(err)
		return false, fmt.Errorf("sync anchor checkpoint: %w", err)
	}
	if err := file.Close(); err != nil {
		a.recordError(err)
		return false, err
	}
	a.mu.Lock()
	a.haveLast, a.lastID, a.lastHash = true, headID, headHash
	a.lastError = ""
	a.lastWrite = checkpoint.CheckedAt
	a.mu.Unlock()
	return true, nil
}

func (a *Anchor) recordError(err error) {
	a.mu.Lock()
	a.lastError = err.Error()
	a.mu.Unlock()
}

// Status returns the anchor's own view for the admin surface.
func (a *Anchor) Status() (path string, lastWrite time.Time, lastError string) {
	if a == nil {
		return "", time.Time{}, ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.path, a.lastWrite, a.lastError
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
