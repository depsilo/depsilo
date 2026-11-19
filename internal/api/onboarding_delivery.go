package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/accesslog"
	"depsilo/internal/api/contract"
	"depsilo/internal/db"
)

const maxOnboardingDeliveryCursorLength = 1024

// onboardingDeliveryCursor is the access-log watermark of an observation.
// Package request outcomes live in the access log alone, so one monotonic ID
// orders them; fixed upper bounds make each poll gap-free without server
// session state. This is an observation cursor, never a client identity.
type onboardingDeliveryCursor struct {
	Version     int  `json:"v"`
	AccessAfter uint `json:"x"`
}

// legacyOnboardingDeliveryCursor is the version 1 cursor, which paired audit
// request rows with access rows. A browser session opened before the upgrade
// continues from its access watermark; a page in progress can replay at most
// one page of deliveries it already saw, which the browser treats as
// idempotent.
type legacyOnboardingDeliveryCursor struct {
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

func newOnboardingDeliveryCursor(accessAfter uint) onboardingDeliveryCursor {
	return onboardingDeliveryCursor{Version: 2, AccessAfter: accessAfter}
}

func parseOnboardingDeliveryCursor(raw string) (onboardingDeliveryCursor, error) {
	var cursor legacyOnboardingDeliveryCursor
	invalid := errors.New("delivery_after_id must be a valid delivery cursor")
	if raw == "" || len(raw) > maxOnboardingDeliveryCursorLength {
		return onboardingDeliveryCursor{}, invalid
	}
	body, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(body, &cursor) != nil {
		return onboardingDeliveryCursor{}, invalid
	}
	// IDs must fit SQLite's signed integer domain and remain exactly
	// representable by the browser's existing numeric event IDs.
	const maxID = 1<<53 - 1
	for _, id := range []uint{cursor.AuditAfter, cursor.AccessAfter, cursor.AuditThrough, cursor.AccessThrough, cursor.LastAudit, cursor.LastAccess} {
		if uint64(id) > maxID {
			return onboardingDeliveryCursor{}, invalid
		}
	}
	switch cursor.Version {
	case 2:
		if cursor.AuditAfter != 0 || cursor.Paging || cursor.AuditThrough != 0 || cursor.AccessThrough != 0 ||
			cursor.LastAudit != 0 || cursor.LastAccess != 0 {
			return onboardingDeliveryCursor{}, invalid
		}
	case 1:
		if cursor.Paging {
			if cursor.AuditThrough < cursor.AuditAfter || cursor.AccessThrough < cursor.AccessAfter ||
				cursor.LastAudit == 0 || cursor.LastAccess == 0 ||
				cursor.LastAudit > cursor.AuditThrough || cursor.LastAccess > cursor.AccessThrough ||
				(cursor.LastAudit <= cursor.AuditAfter && cursor.LastAccess <= cursor.AccessAfter) {
				return onboardingDeliveryCursor{}, invalid
			}
		} else if cursor.AuditThrough != 0 || cursor.AccessThrough != 0 || cursor.LastAudit != 0 || cursor.LastAccess != 0 {
			return onboardingDeliveryCursor{}, invalid
		}
	default:
		return onboardingDeliveryCursor{}, invalid
	}
	return newOnboardingDeliveryCursor(cursor.AccessAfter), nil
}

func onboardingAccessMaxID(ctx context.Context, database *gorm.DB) (uint, error) {
	var result struct{ MaxID uint }
	err := database.WithContext(ctx).Model(&db.AccessLog{}).
		Select("COALESCE(MAX(id), 0) AS max_id").Scan(&result).Error
	return result.MaxID, err
}

// deliveryPage reads the package requests the access log recorded after the
// cursor. An access row is written when the response completes, so its
// request start is the completion time less the latency: a slow request
// already in flight when onboarding began never looks new. The SQL bound on
// completion time is the necessary condition; the start test runs here.
// Request rows an earlier release chained into the audit log are not read,
// so a session spanning the upgrade never sees one request twice.
func (h *OnboardingHandler) deliveryPage(ctx context.Context, cursor onboardingDeliveryCursor, startedAt time.Time) ([]contract.OnboardingDeliveryEvent, string, bool, error) {
	accessMax, err := onboardingAccessMaxID(ctx, h.db)
	if err != nil {
		return nil, "", false, err
	}
	// Retention can remove all rows while an observation is open. Never move
	// the watermark backward and replay surviving history.
	through := max(cursor.AccessAfter, accessMax)

	rows := make([]db.AccessLog, 0, onboardingEventPageSize+1)
	err = h.db.WithContext(ctx).Model(&db.AccessLog{}).
		Select("id, request_id, adapter_type, package_name, cache_key, method, delivery_result, bytes_sent, status_code, cache_result, latency_ms, created_at").
		Where("id > ? AND id <= ?", cursor.AccessAfter, through).
		Where("request_id <> '' AND package_name <> ''").
		Where("created_at >= ?", startedAt.UTC()).
		Order("id ASC").Limit(onboardingEventPageSize + 1).
		Find(&rows).Error
	if err != nil {
		return nil, "", false, err
	}
	hasMore := len(rows) > onboardingEventPageSize
	next := newOnboardingDeliveryCursor(through)
	if hasMore {
		rows = rows[:onboardingEventPageSize]
		next.AccessAfter = rows[len(rows)-1].ID
	}
	events := make([]contract.OnboardingDeliveryEvent, 0, len(rows))
	for _, row := range rows {
		requestStart := row.CreatedAt.Add(-time.Duration(row.LatencyMs) * time.Millisecond).UTC()
		if requestStart.Before(startedAt) {
			continue
		}
		event := contract.OnboardingDeliveryEvent{
			ID: row.ID, RequestID: row.RequestID, Ecosystem: row.AdapterType,
			PackageName: row.PackageName, Version: accesslog.RecordedVersion(row.AdapterType, row.CacheKey, row.StatusCode),
			CacheKey: row.CacheKey, Method: row.Method,
			DependencyKind: accesslog.DependencyRequestKind(row.AdapterType, row.CacheKey),
			DeliveryResult: row.DeliveryResult, BytesSent: row.BytesSent, StatusCode: row.StatusCode,
			Outcome: row.CacheResult, CreatedAt: requestStart,
		}
		event.SuccessfulArtifactDownload = accesslog.SuccessfulArtifactDownload(accesslog.Event{
			AdapterType: event.Ecosystem, Method: event.Method, CacheKey: event.CacheKey,
			DependencyKind: event.DependencyKind, BytesSent: event.BytesSent,
			StatusCode: event.StatusCode, CacheResult: event.Outcome, DeliveryResult: event.DeliveryResult,
		})
		events = append(events, event)
	}
	return events, next.encode(), hasMore, nil
}
