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
// forking the chain.
func AppendAuditRows(ctx context.Context, database *gorm.DB, rows []db.AuditLog) error {
	if database == nil || len(rows) == 0 {
		return nil
	}
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		pending := append([]db.AuditLog(nil), rows...)
		err := database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			prev, err := chainHead(tx)
			if err != nil {
				return err
			}
			for index := range pending {
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
		if !isChainHeadConflict(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 2 * time.Millisecond)
	}
	return fmt.Errorf("append audit rows after chain retries: %w", lastErr)
}

func chainHead(tx *gorm.DB) (string, error) {
	var head db.AuditLog
	err := tx.Order("id DESC").Limit(1).Find(&head).Error
	if err != nil {
		return "", fmt.Errorf("read audit chain head: %w", err)
	}
	if head.ID == 0 {
		// Empty table: the genesis row chains from the empty string.
		return "", nil
	}
	return head.Hash, nil
}

func isChainHeadConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") && strings.Contains(message, "prev_hash")
}

// ChainReport describes one verification pass.
type ChainReport struct {
	OK             bool      `json:"ok"`
	RowsScanned    int64     `json:"rows_scanned"`
	ChainedRows    int64     `json:"chained_rows"`
	UnchainedRows  int64     `json:"unchained_rows"`
	FirstChainedID uint      `json:"first_chained_id,omitempty"`
	HeadID         uint      `json:"head_id,omitempty"`
	HeadHash       string    `json:"head_hash,omitempty"`
	BrokenAtID     uint      `json:"broken_at_id,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	ScannedAt      time.Time `json:"scanned_at"`
}

// VerifyChain walks audit_logs in id order and recomputes the hash chain.
// Rows written before the chain existed are reported as an unchained prefix.
// Any missing, reordered, or modified row after the first chained row makes
// OK false and names the first broken id.
func VerifyChain(ctx context.Context, database *gorm.DB, batchSize int) (ChainReport, error) {
	report := ChainReport{OK: true, ScannedAt: time.Now().UTC()}
	if database == nil {
		return report, errors.New("verify audit chain: nil database")
	}
	if batchSize <= 0 {
		batchSize = 1000
	}
	prev := ""
	started := false
	var batch []db.AuditLog
	err := database.WithContext(ctx).Order("id").FindInBatches(&batch, batchSize, func(_ *gorm.DB, _ int) error {
		for _, row := range batch {
			if report.BrokenAtID != 0 {
				return nil
			}
			report.RowsScanned++
			if row.Hash == "" {
				if started {
					report.OK = false
					report.BrokenAtID = row.ID
					report.Reason = "row is not chained after the chain started"
					return nil
				}
				report.UnchainedRows++
				continue
			}
			if !started {
				started = true
				report.FirstChainedID = row.ID
			}
			rowPrev := ""
			if row.PrevHash != nil {
				rowPrev = *row.PrevHash
			}
			if rowPrev != prev {
				report.OK = false
				report.BrokenAtID = row.ID
				report.Reason = "prev_hash does not link to the previous row"
				return nil
			}
			if expected := ComputeAuditHash(prev, row); expected != row.Hash {
				report.OK = false
				report.BrokenAtID = row.ID
				report.Reason = "hash does not match the row content"
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
		return report, fmt.Errorf("scan audit chain: %w", err)
	}
	return report, nil
}
