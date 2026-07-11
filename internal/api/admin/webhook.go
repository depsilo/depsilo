package admin

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/notify"
)

// WebhookHandler manages webhook configuration CRUD.
type WebhookHandler struct {
	DB       *gorm.DB
	Notifier *notify.Notifier
}

type webhookListResponse struct {
	ID              uint       `json:"id"`
	Name            string     `json:"name"`
	Platform        string     `json:"platform"`
	URL             string     `json:"url"`
	Enabled         bool       `json:"enabled"`
	Events          string     `json:"events"`
	CooldownMinutes int        `json:"cooldown_minutes"`
	LastSentAt      *time.Time `json:"last_sent_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// NewWebhookHandler creates a new WebhookHandler.
func NewWebhookHandler(database *gorm.DB, n *notify.Notifier) *WebhookHandler {
	return &WebhookHandler{DB: database, Notifier: n}
}

// List returns all webhook configurations.
func (h *WebhookHandler) List(c *gin.Context) {
	var configs []db.WebhookConfig
	if err := h.DB.Order("created_at DESC").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	canViewCredentials := principalCanViewCredentials(c)
	items := make([]webhookListResponse, len(configs))
	for i, config := range configs {
		urlValue := config.URL
		if !canViewCredentials {
			urlValue = maskWebhookURL(urlValue)
		}
		items[i] = webhookListResponse{
			ID: config.ID, Name: config.Name, Platform: config.Platform, URL: urlValue,
			Enabled: config.Enabled, Events: config.Events, CooldownMinutes: config.CooldownMinutes,
			LastSentAt: config.LastSentAt, CreatedAt: config.CreatedAt, UpdatedAt: config.UpdatedAt,
		}
	}
	c.JSON(http.StatusOK, items)
}

// Create adds a new webhook configuration.
func (h *WebhookHandler) Create(c *gin.Context) {
	var body struct {
		Name            string `json:"name" binding:"required"`
		Platform        string `json:"platform" binding:"required"`
		URL             string `json:"url" binding:"required"`
		Events          string `json:"events"`
		CooldownMinutes int    `json:"cooldown_minutes"`
		Enabled         *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	events := body.Events
	if events == "" {
		events = "*"
	}
	cooldown := body.CooldownMinutes
	if cooldown <= 0 {
		cooldown = 30
	}

	cfg := db.WebhookConfig{
		Name:            body.Name,
		Platform:        body.Platform,
		URL:             body.URL,
		Events:          events,
		CooldownMinutes: cooldown,
		Enabled:         enabled,
	}
	if err := h.DB.Create(&cfg).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	h.reloadNotifier()
	c.JSON(http.StatusCreated, cfg)
}

// Update modifies an existing webhook configuration.
func (h *WebhookHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	var body struct {
		Name            *string `json:"name"`
		Platform        *string `json:"platform"`
		URL             *string `json:"url"`
		Events          *string `json:"events"`
		CooldownMinutes *int    `json:"cooldown_minutes"`
		Enabled         *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	updates := map[string]interface{}{}
	if body.Name != nil {
		updates["name"] = *body.Name
	}
	if body.Platform != nil {
		updates["platform"] = *body.Platform
	}
	if body.URL != nil {
		updates["url"] = *body.URL
	}
	if body.Events != nil {
		updates["events"] = *body.Events
	}
	if body.CooldownMinutes != nil {
		updates["cooldown_minutes"] = *body.CooldownMinutes
	}
	if body.Enabled != nil {
		updates["enabled"] = *body.Enabled
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "no fields to update"})
		return
	}

	if err := h.DB.Model(&db.WebhookConfig{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	h.reloadNotifier()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Delete removes a webhook configuration.
func (h *WebhookHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	if err := h.DB.Delete(&db.WebhookConfig{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	h.reloadNotifier()
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// Test sends a test notification to the specified webhook.
func (h *WebhookHandler) Test(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	var cfg db.WebhookConfig
	if err := h.DB.First(&cfg, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "webhook not found"})
		return
	}

	testEvent := notify.Event{
		Type:      "test",
		Severity:  "info",
		Title:     "Depsilo webhook test",
		Message:   "If you see this, your webhook is configured correctly! 🎉",
		Timestamp: time.Now(),
	}

	if h.Notifier != nil {
		h.Notifier.Dispatch(context.Background(), testEvent)
	}

	zap.L().Info("webhook test triggered", zap.String("name", cfg.Name), zap.String("platform", cfg.Platform))
	c.JSON(http.StatusOK, gin.H{"status": "test sent"})
}

func (h *WebhookHandler) reloadNotifier() {
	if h.Notifier != nil {
		if err := h.Notifier.LoadConfigs(); err != nil {
			zap.L().Warn("failed to reload webhook configs", zap.Error(err))
		}
	}
}
