package db

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// OriginTrafficCoverageKey marks the instant origin metering became
// authoritative. Rows and rollup buckets written before it have no measured
// upstream bytes, so range responses must present that as "not collected"
// rather than zero.
const OriginTrafficCoverageKey = "origin_traffic_v5"

// migrateOriginTraffic adds measured Depsilo→upstream exchange and byte
// counters to request-shaped rows and every rollup grain the Overview range
// queries read. Existing rows stay at zero; the coverage marker records when
// collection started so the UI never presents the gap as measured traffic.
func migrateOriginTraffic(database *gorm.DB) error {
	for _, model := range []any{&AccessLog{}, &AccessLogFiveMinutely{}, &AccessLogHourly{}, &AccessLogDaily{}} {
		for _, field := range []string{"UpstreamRequests", "UpstreamBytes"} {
			if database.Migrator().HasColumn(model, field) {
				continue
			}
			if err := database.Migrator().AddColumn(model, field); err != nil {
				return fmt.Errorf("add origin traffic column %s: %w", field, err)
			}
		}
	}
	return database.Save(&ControlPlaneState{
		Key:   OriginTrafficCoverageKey,
		Value: time.Now().UTC().Format(time.RFC3339),
	}).Error
}

func ensureSchemaV5Invariants(database *gorm.DB) error {
	for _, model := range []any{&AccessLog{}, &AccessLogFiveMinutely{}, &AccessLogHourly{}, &AccessLogDaily{}} {
		for _, field := range []string{"UpstreamRequests", "UpstreamBytes"} {
			if !database.Migrator().HasColumn(model, field) {
				return fmt.Errorf("origin traffic column %s is missing", field)
			}
		}
	}
	return nil
}
