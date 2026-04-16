package security

import (
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/db"
)

// Importer handles offline vulnerability data import.
type Importer struct {
	db *gorm.DB
}

// NewImporter creates a new vulnerability importer.
func NewImporter(database *gorm.DB) *Importer {
	return &Importer{db: database}
}

// Import parses OSV JSON data and upserts vulnerabilities into the database.
// Accepts either a single OSV object or an array of OSV objects.
func (imp *Importer) Import(data []byte) (int, error) {
	var vulns []OSVVulnerability

	if err := json.Unmarshal(data, &vulns); err != nil {
		var single OSVVulnerability
		if err2 := json.Unmarshal(data, &single); err2 != nil {
			return 0, fmt.Errorf("invalid JSON: not an array or single OSV object: %w", err2)
		}
		vulns = []OSVVulnerability{single}
	}

	count := 0
	for _, v := range vulns {
		if v.ID == "" {
			continue
		}

		parsed := ParseVulnerability(v, "")
		if parsed.Ecosystem == "" || parsed.PackageName == "" {
			zap.L().Warn("skipping vulnerability with missing ecosystem/package", zap.String("osv_id", v.ID))
			continue
		}

		err := imp.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "osv_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"affected_ranges", "severity", "cvss_score", "summary", "details", "aliases", "references", "modified_at", "updated_at"}),
		}).Create(parsed).Error
		if err != nil {
			zap.L().Warn("failed to import vulnerability", zap.String("osv_id", v.ID), zap.Error(err))
			continue
		}

		now := time.Now()
		imp.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "ecosystem"}, {Name: "package_name"}},
			DoUpdates: clause.AssignmentColumns([]string{"has_vulnerabilities", "vulnerability_count", "last_fetched_at", "next_fetch_at", "updated_at"}),
		}).Create(&db.VulnerabilityCheck{
			Ecosystem:          parsed.Ecosystem,
			PackageName:        parsed.PackageName,
			HasVulnerabilities: true,
			VulnerabilityCount: 1,
			LastFetchedAt:      now,
			NextFetchAt:        now.Add(24 * time.Hour),
		})

		count++
	}

	return count, nil
}
