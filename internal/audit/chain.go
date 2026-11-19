package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

// ChainHashVersion prefixes every chain payload so the formula can evolve
// without silently reinterpreting existing hashes.
const ChainHashVersion = "depsilo/audit-chain/v1"

// chainPayload is the canonical hash input. Field order is fixed by the
// struct, the timestamp is normalized to UTC RFC3339Nano, and hashes are
// excluded, so a third party with the same rows can recompute every hash.
//
// The database-assigned id is deliberately not part of the payload: the chain
// binds content and order through prev_hash, and re-ordering or duplicating a
// row breaks the link at the next hash.
type chainPayload struct {
	HashVersion string `json:"hash_version"`
	PrevHash    string `json:"prev_hash"`
	Ecosystem   string `json:"ecosystem"`
	PackageName string `json:"package_name"`
	Version     string `json:"version"`
	Action      string `json:"action"`
	CacheResult string `json:"cache_result"`
	ClientIP    string `json:"client_ip"`
	UserAgent   string `json:"user_agent"`
	UpstreamURL string `json:"upstream_url"`
	LatencyMs   int64  `json:"latency_ms"`
	BytesSent   int64  `json:"bytes_sent"`
	StatusCode  int    `json:"status_code"`
	CreatedAt   string `json:"created_at"`
	RequestID   string `json:"request_id"`
}

// ComputeAuditHash returns the chain hash for one row given the previous
// hash. The row's own PrevHash/Hash fields are ignored.
func ComputeAuditHash(prevHash string, row db.AuditLog) string {
	payload := chainPayload{
		HashVersion: ChainHashVersion,
		PrevHash:    prevHash,
		Ecosystem:   row.Ecosystem,
		PackageName: row.PackageName,
		Version:     row.Version,
		Action:      row.Action,
		CacheResult: row.CacheResult,
		ClientIP:    row.ClientIP,
		UserAgent:   row.UserAgent,
		UpstreamURL: row.UpstreamURL,
		LatencyMs:   row.LatencyMs,
		BytesSent:   row.BytesSent,
		StatusCode:  row.StatusCode,
		CreatedAt:   row.CreatedAt.UTC().Format(time.RFC3339Nano),
		RequestID:   row.RequestID,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		// The payload contains only strings and integers.
		panic(fmt.Sprintf("audit chain payload must marshal: %v", err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// AppendAuditRows chains and inserts rows in one transaction. Callers may pass
// a root handle or an existing transaction; nested transactions use
// savepoints. A concurrent writer that wins the chain head makes the unique
// prev_hash index fail, and the append retries against the new head instead of
// forking the chain. Root transactions also restart when a concurrent SQLite
// commit invalidates their read snapshot.
func AppendAuditRows(ctx context.Context, database *gorm.DB, rows []db.AuditLog) error {
	if database == nil || len(rows) == 0 {
		return nil
	}
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		pending := append([]db.AuditLog(nil), rows...)
		err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			prev, nextID, err := chainHead(tx)
			if err != nil {
				return err
			}
			for index := range pending {
				if nextID != 0 {
					// Every row was pruned. AUTOINCREMENT already keeps new
					// ids above the deleted ones; the explicit id also holds
					// when the sqlite_sequence entry is missing, so exporter
					// cursors and anchors never see a reused id.
					pending[index].ID = nextID + uint(index)
				}
				// Each row needs its own prev value: sharing one variable would
				// make every row in the batch point at the final hash and break
				// the unique prev_hash index.
				previous := prev
				pending[index].PrevHash = &previous
				pending[index].Hash = ComputeAuditHash(prev, pending[index])
				prev = pending[index].Hash
			}
			return tx.Create(&pending).Error
		})
		if err == nil {
			return nil
		}
		lastErr = err
		if !isChainAppendConflict(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 2 * time.Millisecond)
	}
	return fmt.Errorf("append audit rows after chain retries: %w", lastErr)
}

// chainHead returns the hash the next row links to. When retention pruned
// every row, it also returns the id the next row must take (0 otherwise).
func chainHead(tx *gorm.DB) (string, uint, error) {
	var head db.AuditLog
	err := tx.Order("id DESC").Limit(1).Find(&head).Error
	if err != nil {
		return "", 0, fmt.Errorf("read audit chain head: %w", err)
	}
	if head.ID != 0 {
		return head.Hash, 0, nil
	}
	base, ok, err := LoadChainBase(tx.Statement.Context, tx)
	if err != nil {
		return "", 0, err
	}
	if !ok {
		// Never pruned: the genesis row chains from the empty string.
		return "", 0, nil
	}
	return base.Hash, base.ID + 1, nil
}

func isChainAppendConflict(err error) bool {
	if err == nil {
		return false
	}
	// A concurrent writer can invalidate the WAL snapshot between chainHead
	// and Create. Busy timeouts cannot repair SQLITE_BUSY_SNAPSHOT: rerun the
	// whole transaction, including its chain-head read, after rollback.
	var sqliteError interface{ Code() int }
	if errors.As(err, &sqliteError) && sqliteError.Code()&0xff == 5 {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") &&
		(strings.Contains(message, "prev_hash") || strings.Contains(message, "audit_logs.id"))
}

// ChainReport describes one verification pass. BaseID/BaseHash identify the
// last row removed by retention, which the first retained row links to;
// BaseCreatedAt is that row's time and nil when nothing was pruned.
type ChainReport struct {
	OK             bool       `json:"ok"`
	RowsScanned    int64      `json:"rows_scanned"`
	ChainedRows    int64      `json:"chained_rows"`
	UnchainedRows  int64      `json:"unchained_rows"`
	FirstChainedID uint       `json:"first_chained_id,omitempty"`
	BaseID         uint       `json:"base_id,omitempty"`
	BaseHash       string     `json:"base_hash,omitempty"`
	BaseCreatedAt  *time.Time `json:"base_created_at,omitempty"`
	HeadID         uint       `json:"head_id,omitempty"`
	HeadHash       string     `json:"head_hash,omitempty"`
	BrokenAtID     uint       `json:"broken_at_id,omitempty"`
	Reason         string     `json:"reason,omitempty"`
	ScannedAt      time.Time  `json:"scanned_at"`
}

// VerifyChain walks audit_logs in id order and recomputes the hash chain.
// Rows written before the chain existed are reported as an unchained prefix.
// When retention pruned the oldest rows, the walk starts from the recorded
// chain base: the first retained row must link to the base hash, and a first
// row that does not start the chain without a recorded base is a break. Any
// missing, reordered, or modified row after the first chained row makes OK
// false and names the first broken id.
func VerifyChain(ctx context.Context, database *gorm.DB, batchSize int) (ChainReport, error) {
	report := ChainReport{OK: true, ScannedAt: time.Now().UTC()}
	if database == nil {
		return report, errors.New("verify audit chain: nil database")
	}
	if batchSize <= 0 {
		batchSize = 1000
	}
	// Retention deletes the prefix and moves the base while this runs: read
	// the base and every row from one WAL snapshot, or a sweep committed in
	// between would look like a break.
	err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return verifyChainSnapshot(ctx, tx, batchSize, &report)
	})
	return report, err
}

func verifyChainSnapshot(ctx context.Context, database *gorm.DB, batchSize int, report *ChainReport) error {
	base, hasBase, err := LoadChainBase(ctx, database)
	if err != nil {
		return err
	}
	prev := ""
	started := false
	if hasBase {
		report.BaseID, report.BaseHash = base.ID, base.Hash
		baseCreatedAt := base.CreatedAt
		report.BaseCreatedAt = &baseCreatedAt
		prev, started = base.Hash, base.Hash != ""
	}
	fail := func(row db.AuditLog, reason string) {
		report.OK = false
		report.BrokenAtID = row.ID
		report.Reason = reason
	}
	var batch []db.AuditLog
	err = database.WithContext(ctx).Order("id").FindInBatches(&batch, batchSize, func(_ *gorm.DB, _ int) error {
		for _, row := range batch {
			if report.BrokenAtID != 0 {
				return nil
			}
			report.RowsScanned++
			if hasBase && row.ID <= base.ID {
				fail(row, "row precedes the pruned chain base")
				return nil
			}
			if row.Hash == "" {
				if started {
					fail(row, "row is not chained after the chain started")
					return nil
				}
				report.UnchainedRows++
				continue
			}
			first := !started
			if first {
				started = true
			}
			if report.FirstChainedID == 0 {
				report.FirstChainedID = row.ID
			}
			rowPrev := ""
			if row.PrevHash != nil {
				rowPrev = *row.PrevHash
			}
			if rowPrev != prev {
				switch {
				case first && !hasBase:
					fail(row, "first chained row does not start the chain and no pruned chain base is recorded")
				case report.ChainedRows == 0 && hasBase:
					fail(row, "prev_hash does not link to the pruned chain base")
				default:
					fail(row, "prev_hash does not link to the previous row")
				}
				return nil
			}
			if expected := ComputeAuditHash(prev, row); expected != row.Hash {
				fail(row, "hash does not match the row content")
				return nil
			}
			report.ChainedRows++
			report.HeadID = row.ID
			report.HeadHash = row.Hash
			prev = row.Hash
		}
		return nil
	}).Error
	if err != nil {
		return fmt.Errorf("scan audit chain: %w", err)
	}
	return nil
}
