package db

import (
	"fmt"

	"gorm.io/gorm"
)

// migrateCascadeUpstreamEgress records which optional cascade peer egresses an
// upstream. Existing rows keep direct egress because the column defaults to an
// empty string; enabling cascade never rewrites historical routing on its own.
func migrateCascadeUpstreamEgress(database *gorm.DB) error {
	if !database.Migrator().HasColumn(&UpstreamRecord{}, "Via") {
		if err := database.Migrator().AddColumn(&UpstreamRecord{}, "Via"); err != nil {
			return fmt.Errorf("add upstream via column: %w", err)
		}
	}
	return nil
}
