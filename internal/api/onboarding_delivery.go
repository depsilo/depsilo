package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/accesslog"
	"depsilo/internal/db"
)

const maxOnboardingDeliveryCursorLength = 1024

// The two log writers commit independently. A joined delivery is new when
// either side passed its watermark, even if the other side was read earlier.
// Fixed upper bounds and a pair key make pagination stable without server
// session state. This is an observation cursor, never a client identity.
type onboardingDeliveryCursor struct {
	Version       int  `json:"v"`
	AuditAfter    uint `json:"a"`
	AccessAfter   uint `json:"x"`
	Paging        bool `json:"p,omitempty"`
	AuditThrough  uint `json:"at,omitempty"`
	AccessThrough uint `json:"xt,omitempty"`
	LastAudit     uint `json:"la,omitempty"`
	LastAccess    uint `json:"lx,omitempty"`
}

func (cursor onboardingDeliveryCursor) encode() string {
	body, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(body)
}

func parseOnboardingDeliveryCursor(raw string) (onboardingDeliveryCursor, error) {
	var cursor onboardingDeliveryCursor
	invalid := errors.New("delivery_after_id must be a valid delivery cursor")
	if raw == "" || len(raw) > maxOnboardingDeliveryCursorLength {
		return cursor, invalid
	}
	body, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(body, &cursor) != nil || cursor.Version != 1 {
		return cursor, invalid
	}
	// IDs must fit SQLite's signed integer domain and remain exactly
	// representable by the browser's existing numeric event IDs.
	const maxID = 1<<53 - 1
	for _, id := range []uint{cursor.AuditAfter, cursor.AccessAfter, cursor.AuditThrough, cursor.AccessThrough, cursor.LastAudit, cursor.LastAccess} {
		if uint64(id) > maxID {
			return cursor, invalid
		}
	}
	if cursor.Paging {
		if cursor.AuditThrough < cursor.AuditAfter || cursor.AccessThrough < cursor.AccessAfter ||
			cursor.LastAudit == 0 || cursor.LastAccess == 0 ||
			cursor.LastAudit > cursor.AuditThrough || cursor.LastAccess > cursor.AccessThrough ||
			(cursor.LastAudit <= cursor.AuditAfter && cursor.LastAccess <= cursor.AccessAfter) {
			return cursor, invalid
		}
	} else if cursor.AuditThrough != 0 || cursor.AccessThrough != 0 || cursor.LastAudit != 0 || cursor.LastAccess != 0 {
		return cursor, invalid
	}
	return cursor, nil
}

type onboardingDeliveryEvent struct {
	ID                         uint      `json:"id"`
	RequestID                  string    `json:"request_id"`
	Ecosystem                  string    `json:"ecosystem"`
	PackageName                string    `json:"package_name"`
	Version                    string    `json:"version"`
	CacheKey                   string    `json:"cache_key"`
	Method                     string    `json:"method"`
	DependencyKind             string    `json:"dependency_kind"`
	DeliveryResult             string    `json:"delivery_result"`
	BytesSent                  int64     `json:"bytes_sent"`
	StatusCode                 int       `json:"status_code"`
	Outcome                    string    `json:"outcome"`
	CreatedAt                  time.Time `json:"created_at"`
	SuccessfulArtifactDownload bool      `json:"successful_artifact_download"`
}

type onboardingDeliveryRow struct {
	Delivery onboardingDeliveryEvent `gorm:"embedded"`
	AuditID  uint
}

func onboardingAccessMaxID(ctx context.Context, database *gorm.DB) (uint, error) {
	var result struct{ MaxID uint }
	err := database.WithContext(ctx).Model(&db.AccessLog{}).
		Select("COALESCE(MAX(id), 0) AS max_id").Scan(&result).Error
	return result.MaxID, err
}

func (h *OnboardingHandler) deliveryPage(ctx context.Context, cursor onboardingDeliveryCursor, startedAt time.Time, auditMax uint) ([]onboardingDeliveryEvent, string, bool, error) {
	if !cursor.Paging {
		accessMax, err := onboardingAccessMaxID(ctx, h.db)
		if err != nil {
			return nil, "", false, err
		}
		// Retention can remove all rows while an observation is open. Never
		// move its watermarks backward and replay surviving history.
		cursor.AuditThrough = max(cursor.AuditAfter, auditMax)
		cursor.AccessThrough = max(cursor.AccessAfter, accessMax)
	}

	rows := make([]onboardingDeliveryRow, 0, onboardingEventPageSize+1)
	query := h.db.WithContext(ctx).Table("audit_logs AS audit").
		Select(`access.id, audit.id AS audit_id, access.request_id, access.adapter_type AS ecosystem,
			audit.package_name, audit.version, access.cache_key, access.method,
			access.delivery_result, access.bytes_sent, access.status_code, access.cache_result AS outcome,
			audit.created_at`).
		Joins(`JOIN access_logs AS access ON access.request_id = audit.request_id
			AND access.adapter_type = audit.ecosystem AND access.package_name = audit.package_name`).
		Where("audit.request_id <> '' AND audit.package_name <> ''").
		Where("audit.action IN ?", onboardingAuditActions).
		Where("audit.created_at >= ?", startedAt.UTC()).
		Where("audit.id <= ? AND access.id <= ?", cursor.AuditThrough, cursor.AccessThrough).
		Where("(audit.id > ? OR access.id > ?)", cursor.AuditAfter, cursor.AccessAfter)
	if cursor.Paging {
		query = query.Where("(audit.id > ? OR (audit.id = ? AND access.id > ?))",
			cursor.LastAudit, cursor.LastAudit, cursor.LastAccess)
	}
	if err := query.Order("audit.id ASC, access.id ASC").Limit(onboardingEventPageSize + 1).Scan(&rows).Error; err != nil {
		return nil, "", false, err
	}
	hasMore := len(rows) > onboardingEventPageSize
	if hasMore {
		rows = rows[:onboardingEventPageSize]
		cursor.Paging = true
		cursor.LastAudit = rows[len(rows)-1].AuditID
		cursor.LastAccess = rows[len(rows)-1].Delivery.ID
	} else {
		cursor = onboardingDeliveryCursor{
			Version: 1, AuditAfter: cursor.AuditThrough, AccessAfter: cursor.AccessThrough,
		}
	}
	events := make([]onboardingDeliveryEvent, 0, len(rows))
	for _, row := range rows {
		event := row.Delivery
		event.DependencyKind = accesslog.DependencyRequestKind(event.Ecosystem, event.CacheKey)
		event.SuccessfulArtifactDownload = accesslog.SuccessfulArtifactDownload(accesslog.Event{
			AdapterType: event.Ecosystem, Method: event.Method, CacheKey: event.CacheKey,
			DependencyKind: event.DependencyKind, BytesSent: event.BytesSent,
			StatusCode: event.StatusCode, CacheResult: event.Outcome, DeliveryResult: event.DeliveryResult,
		})
		events = append(events, event)
	}
	return events, cursor.encode(), hasMore, nil
}
