package audit

import (
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
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	var items []db.AuditLog
	// Order via datetime() too — otherwise rows whose stored zone-suffix
	// differs (legacy CST vs new UTC-Z) get sorted lexicographically and
	// "16:10+08:00" wrongly outranks "08:25Z" despite being earlier.
	if err := query.Order("datetime(created_at) DESC").
		Offset((q.Page - 1) * q.PageSize).
		Limit(q.PageSize).
		Find(&items).Error; err != nil {
		return nil, err
	}

	return &QueryResult{
		Total: total,
		Page:  q.Page,
		Items: items,
	}, nil
}

// RunExportQuery returns filtered audit rows, capped at 10,000. HTTP-layer
// DTO mapping and credential policy are deliberately applied by the caller.
func RunExportQuery(database *gorm.DB, q Query) ([]db.AuditLog, error) {
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
	err := query.Order("datetime(created_at) DESC").Limit(10000).Find(&items).Error
	return items, err
}
