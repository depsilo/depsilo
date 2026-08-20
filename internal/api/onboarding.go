package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

const (
	onboardingStatusStateKey = "onboarding_status_v1"

	onboardingStatusNotStarted = "not_started"
	onboardingStatusCompleted  = "completed"
	onboardingStatusSkipped    = "skipped"

	onboardingEventPageSize       = 64
	maxOnboardingRequestBodyBytes = 4 << 10
)

var onboardingAuditActions = []string{"metadata", "download"}

// OnboardingHandler exposes the small instance-local state and event cursor
// needed by the first-project flow. Request identity stays behind Admin read
// authentication; the anonymous Portal never receives package history.
type OnboardingHandler struct {
	db  *gorm.DB
	now func() time.Time
}

func NewOnboardingHandler(database *gorm.DB) *OnboardingHandler {
	return &OnboardingHandler{db: database, now: time.Now}
}

type onboardingEvent struct {
	ID          uint      `json:"id"`
	Ecosystem   string    `json:"ecosystem"`
	PackageName string    `json:"package_name"`
	Version     string    `json:"version"`
	Outcome     string    `json:"outcome"`
	StatusCode  int       `json:"status_code"`
	CreatedAt   time.Time `json:"created_at"`
}

type onboardingStatusResponse struct {
	Status      string            `json:"status"`
	StartedAt   time.Time         `json:"started_at"`
	Events      []onboardingEvent `json:"events"`
	NextAfterID uint              `json:"next_after_id"`
	HasMore     bool              `json:"has_more"`
}

// Status starts or advances a client-owned onboarding cursor. The initial
// request deliberately returns no historical events: its current maximum audit
// ID and request timestamp form the baseline. Polls then read only package
// events committed after that ID and created after the supplied session
// boundary. The timestamp also excludes older events that an async logger
// happened to flush after the baseline ID was captured.
func (h *OnboardingHandler) Status(c *gin.Context) {
	c.Header("Cache-Control", "no-store")

	status, err := loadOnboardingStatus(c.Request.Context(), h.db)
	if err != nil {
		writeOnboardingDatabaseError(c)
		return
	}
	maxID, err := onboardingAuditMaxID(c.Request.Context(), h.db)
	if err != nil {
		writeOnboardingDatabaseError(c)
		return
	}

	rawAfterID, hasAfterID := c.GetQuery("after_id")
	rawStartedAt, hasStartedAt := c.GetQuery("started_at")
	if !hasAfterID && !hasStartedAt {
		// Capture time after the ID snapshot to form a gap-free barrier:
		// events inserted before MAX(id) are behind the cursor, while events
		// inserted afterward must also have started on or after this boundary.
		requestStartedAt := h.now().UTC()
		c.JSON(http.StatusOK, onboardingStatusResponse{
			Status:      status,
			StartedAt:   requestStartedAt,
			Events:      []onboardingEvent{},
			NextAfterID: maxID,
		})
		return
	}
	if !hasAfterID || !hasStartedAt {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "INVALID_CURSOR",
			"message": "after_id and started_at must be provided together",
		})
		return
	}

	afterID, err := parseOnboardingAfterID(rawAfterID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_CURSOR", "message": err.Error()})
		return
	}
	sessionStartedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(rawStartedAt))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "INVALID_CURSOR",
			"message": "started_at must be an RFC3339 timestamp",
		})
		return
	}

	rows := make([]onboardingEvent, 0, onboardingEventPageSize+1)
	if maxID > afterID {
		err = h.db.WithContext(c.Request.Context()).
			Model(&db.AuditLog{}).
			Select("id, ecosystem, package_name, version, cache_result AS outcome, status_code, created_at").
			Where("id > ? AND id <= ?", afterID, maxID).
			Where("created_at >= ?", sessionStartedAt.UTC()).
			Where("action IN ?", onboardingAuditActions).
			Where("package_name <> ''").
			Order("id ASC").
			Limit(onboardingEventPageSize + 1).
			Find(&rows).Error
		if err != nil {
			writeOnboardingDatabaseError(c)
			return
		}
	}

	hasMore := len(rows) > onboardingEventPageSize
	if hasMore {
		rows = rows[:onboardingEventPageSize]
	}
	nextAfterID := maxID
	if hasMore {
		nextAfterID = rows[len(rows)-1].ID
	}
	c.JSON(http.StatusOK, onboardingStatusResponse{
		Status:      status,
		StartedAt:   sessionStartedAt.UTC(),
		Events:      rows,
		NextAfterID: nextAfterID,
		HasMore:     hasMore,
	})
}

type onboardingStatusUpdate struct {
	Status string `json:"status"`
}

// Update records a terminal choice. not_started is setup-owned so an API
// caller cannot make an existing deployment look like a fresh installation.
func (h *OnboardingHandler) Update(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxOnboardingRequestBodyBytes)
	var request onboardingStatusUpdate
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": "request body must contain an onboarding status"})
		return
	}
	request.Status = strings.TrimSpace(request.Status)
	if request.Status != onboardingStatusCompleted && request.Status != onboardingStatusSkipped {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "INVALID_STATUS",
			"message": "status must be completed or skipped",
		})
		return
	}
	if err := saveOnboardingStatus(c.Request.Context(), h.db, request.Status); err != nil {
		writeOnboardingDatabaseError(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": request.Status})
}

func parseOnboardingAfterID(value string) (uint, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, strconv.IntSize)
	if err != nil {
		return 0, errors.New("after_id must be a non-negative integer")
	}
	return uint(parsed), nil
}

func onboardingAuditMaxID(ctx context.Context, database *gorm.DB) (uint, error) {
	var result struct {
		MaxID uint
	}
	err := database.WithContext(ctx).
		Model(&db.AuditLog{}).
		Select("COALESCE(MAX(id), 0) AS max_id").
		Scan(&result).Error
	return result.MaxID, err
}

func loadOnboardingStatus(ctx context.Context, database *gorm.DB) (string, error) {
	var state db.ControlPlaneState
	err := database.WithContext(ctx).
		First(&state, "key = ?", onboardingStatusStateKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Missing is the compatibility contract for every deployment created
		// before onboarding state existed, and for headless installations.
		return onboardingStatusCompleted, nil
	}
	if err != nil {
		return "", err
	}
	switch state.Value {
	case onboardingStatusNotStarted, onboardingStatusCompleted, onboardingStatusSkipped:
		return state.Value, nil
	default:
		return "", errors.New("invalid persisted onboarding status")
	}
}

func saveOnboardingStatus(ctx context.Context, database *gorm.DB, status string) error {
	return database.WithContext(ctx).Save(&db.ControlPlaneState{
		Key:   onboardingStatusStateKey,
		Value: status,
	}).Error
}

func writeOnboardingDatabaseError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{
		"code":    "DB_ERROR",
		"message": "onboarding state is temporarily unavailable",
	})
}
