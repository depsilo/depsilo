package audit

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

const managementEcosystem = "admin"

// ManagementEvent describes a control-plane mutation recorded in the shared
// audit stream. AuditLog predates management events and is request-shaped, so
// this adapter keeps its established database/API contract while assigning a
// stable meaning to the compatible fields:
//
//   - Ecosystem/PackageName: admin/<target>
//   - UserAgent: operator:<actor id>
//   - Action: the management action
//   - CacheResult/StatusCode: success|error and 200|500
//   - Version: a short, non-sensitive detail
//
// Callers must never put credentials in Detail.
type ManagementEvent struct {
	ActorID  uint
	Action   string
	Target   string
	Success  bool
	ClientIP string
	Detail   string
}

// RecordManagementEvent writes a management mutation synchronously. Callers
// may pass a transaction so the business change and its audit record commit or
// roll back together.
func RecordManagementEvent(ctx context.Context, database *gorm.DB, event ManagementEvent) error {
	if database == nil {
		return nil
	}

	result := "error"
	statusCode := 500
	if event.Success {
		result = "success"
		statusCode = 200
	}
	entry := db.AuditLog{
		Ecosystem:   managementEcosystem,
		PackageName: event.Target,
		Version:     event.Detail,
		Action:      event.Action,
		CacheResult: result,
		ClientIP:    event.ClientIP,
		UserAgent:   fmt.Sprintf("operator:%d", event.ActorID),
		StatusCode:  statusCode,
		CreatedAt:   time.Now().UTC(),
	}
	if err := database.WithContext(ctx).Create(&entry).Error; err != nil {
		return fmt.Errorf("record management audit: %w", err)
	}
	return nil
}
