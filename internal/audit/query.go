package audit

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

// Query holds the filter parameters for audit log queries.
type Query struct {
	Ecosystem   string
	PackageName string
	ClientIP    string
	CacheResult string
	StartTime   time.Time
	EndTime     time.Time
	Page        int
	PageSize    int
}

// QueryResult holds a paginated set of audit log entries.
type QueryResult struct {
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	Items []db.AuditLog `json:"items"`
}

// RunQuery executes a paginated audit log query.
func RunQuery(database *gorm.DB, q Query) (*QueryResult, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 200 {
		q.PageSize = 50
	}

	query := database.Model(&db.AuditLog{})

	if q.Ecosystem != "" {
		query = query.Where("ecosystem = ?", q.Ecosystem)
	}
	if q.PackageName != "" {
		query = query.Where("package_name LIKE ?", "%"+q.PackageName+"%")
	}
	if q.ClientIP != "" {
		query = query.Where("client_ip = ?", q.ClientIP)
	}
	if q.CacheResult != "" {
		query = query.Where("cache_result = ?", q.CacheResult)
	}
	if !q.StartTime.IsZero() {
		// Compare via datetime() so SQLite normalises both sides to UTC
		// regardless of how they were originally stored (with offset, with Z,
		// or naive). Plain string compare would mis-rank "...+08:00" vs "...Z".
		query = query.Where("datetime(created_at) >= datetime(?)", q.StartTime.UTC())
	}
	if !q.EndTime.IsZero() {
		query = query.Where("datetime(created_at) <= datetime(?)", q.EndTime.UTC())
	}

	var total int64
	query.Count(&total)

	var items []db.AuditLog
	// Order via datetime() too — otherwise rows whose stored zone-suffix
	// differs (legacy CST vs new UTC-Z) get sorted lexicographically and
	// "16:10+08:00" wrongly outranks "08:25Z" despite being earlier.
	query.Order("datetime(created_at) DESC").
		Offset((q.Page - 1) * q.PageSize).
		Limit(q.PageSize).
		Find(&items)

	return &QueryResult{
		Total: total,
		Page:  q.Page,
		Items: items,
	}, nil
}

// Export returns audit log entries as CSV bytes, limited to 10000 rows.
func Export(database *gorm.DB, q Query) ([]byte, error) {
	query := database.Model(&db.AuditLog{})

	if q.Ecosystem != "" {
		query = query.Where("ecosystem = ?", q.Ecosystem)
	}
	if q.PackageName != "" {
		query = query.Where("package_name LIKE ?", "%"+q.PackageName+"%")
	}
	if q.ClientIP != "" {
		query = query.Where("client_ip = ?", q.ClientIP)
	}
	if q.CacheResult != "" {
		query = query.Where("cache_result = ?", q.CacheResult)
	}
	if !q.StartTime.IsZero() {
		query = query.Where("datetime(created_at) >= datetime(?)", q.StartTime.UTC())
	}
	if !q.EndTime.IsZero() {
		query = query.Where("datetime(created_at) <= datetime(?)", q.EndTime.UTC())
	}

	var items []db.AuditLog
	query.Order("datetime(created_at) DESC").Limit(10000).Find(&items)

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write([]string{"Time", "Ecosystem", "Package", "Version", "Action", "Result", "Client IP", "Latency(ms)", "Bytes"})

	for _, item := range items {
		w.Write([]string{
			item.CreatedAt.Format(time.RFC3339),
			item.Ecosystem,
			item.PackageName,
			item.Version,
			item.Action,
			item.CacheResult,
			item.ClientIP,
			fmt.Sprintf("%d", item.LatencyMs),
			fmt.Sprintf("%d", item.BytesSent),
		})
	}
	w.Flush()

	return buf.Bytes(), w.Error()
}
