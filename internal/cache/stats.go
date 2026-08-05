package cache

import (
	"context"
	"errors"
	"fmt"

	"depsilo/internal/db"
)

// Stats is the package-cache capacity snapshot exposed to operators.
type Stats struct {
	SizeBytes int64
	Entries   int64
}

// Stats reports the durable package-cache inventory using one database
// aggregate. It deliberately does not walk local files or paginate an entire
// S3 bucket: this method feeds a periodic metric and must remain O(1) in remote
// storage requests as the cache grows. Orphan reconciliation belongs to a
// separate maintenance path.
func (m *Manager) Stats(ctx context.Context) (Stats, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil || m.db == nil {
		return Stats{}, errors.New("measure cache inventory: database is unavailable")
	}
	var stats Stats
	if err := m.db.WithContext(ctx).
		Model(&db.CacheEntry{}).
		Select("COALESCE(SUM(size), 0) AS size_bytes, COUNT(*) AS entries").
		Scan(&stats).Error; err != nil {
		return Stats{}, fmt.Errorf("measure cache inventory: %w", err)
	}
	return stats, nil
}
