package admin

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/audit"
	"depsilo/internal/db"
)

// AuditExporterHandler manages SIEM audit-routing collectors: create, list,
// update, delete, and send a test batch through the real delivery path.
type AuditExporterHandler struct {
	db        *gorm.DB
	forwarder *audit.Forwarder
}

func NewAuditExporterHandler(database *gorm.DB, forwarder *audit.Forwarder) *AuditExporterHandler {
	return &AuditExporterHandler{db: database, forwarder: forwarder}
}

type auditExporterResponse struct {
	ID             uint       `json:"id"`
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	URL            string     `json:"url"`
	TokenSet       bool       `json:"token_set"`
	Events         string     `json:"events"`
	Enabled        bool       `json:"enabled"`
	Cursor         uint       `json:"cursor"`
	Lag            int64      `json:"lag"`
	DeliveredCount int64      `json:"delivered_count"`
	LastError      string     `json:"last_error"`
	LastAttemptAt  *time.Time `json:"last_attempt_at"`
	LastSuccessAt  *time.Time `json:"last_success_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (h *AuditExporterHandler) List(c *gin.Context) {
	var exporters []db.AuditExporter
	if err := h.db.Order("created_at DESC").Find(&exporters).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	var head uint
	if err := h.db.Model(&db.AuditLog{}).Select("COALESCE(MAX(id), 0)").Scan(&head).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	maskCredentials := !principalCanViewCredentials(c)
	items := make([]auditExporterResponse, 0, len(exporters))
	for _, exporter := range exporters {
		items = append(items, toAuditExporterResponse(exporter, head, maskCredentials))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "audit_head": head})
}

type auditExporterRequest struct {
	Name    string  `json:"name"`
	Kind    string  `json:"kind"`
	URL     string  `json:"url"`
	Token   *string `json:"token"`
	Events  string  `json:"events"`
	Enabled *bool   `json:"enabled"`
}

func (h *AuditExporterHandler) Create(c *gin.Context) {
	var body auditExporterRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid exporter payload"})
		return
	}
	exporter, err := normalizeAuditExporter(body, db.AuditExporter{Enabled: true, Events: "*"})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	// Start at the current head: onboarding must not replay months of history
	// into the collector. Historical rows stay available through the audit
	// export endpoint.
	if err := h.db.Model(&db.AuditLog{}).Select("COALESCE(MAX(id), 0)").Scan(&exporter.Cursor).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	if err := h.db.Create(&exporter).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, toAuditExporterResponse(exporter, exporter.Cursor, false))
}

func (h *AuditExporterHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid exporter id"})
		return
	}
	var exporter db.AuditExporter
	if err := h.db.First(&exporter, uint(id)).Error; err != nil {
		writeAuditExporterLookupError(c, err)
		return
	}
	var body auditExporterRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid exporter payload"})
		return
	}
	updated, err := normalizeAuditExporter(body, exporter)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	updated.ID = exporter.ID
	updated.Cursor = exporter.Cursor
	if err := h.db.Model(&db.AuditExporter{}).Where("id = ?", exporter.ID).Updates(map[string]interface{}{
		"name":    updated.Name,
		"kind":    updated.Kind,
		"url":     updated.URL,
		"token":   updated.Token,
		"events":  updated.Events,
		"enabled": updated.Enabled,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": err.Error()})
		return
	}
	if err := h.db.First(&updated, exporter.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toAuditExporterResponse(updated, updated.Cursor, false))
}

func (h *AuditExporterHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid exporter id"})
		return
	}
	result := h.db.Delete(&db.AuditExporter{}, uint(id))
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DELETE_FAILED", "message": result.Error.Error()})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "exporter not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": id})
}

func (h *AuditExporterHandler) Test(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid exporter id"})
		return
	}
	var exporter db.AuditExporter
	if err := h.db.First(&exporter, uint(id)).Error; err != nil {
		writeAuditExporterLookupError(c, err)
		return
	}
	if h.forwarder == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "FORWARDER_UNAVAILABLE", "message": "audit forwarder is not running"})
		return
	}
	if err := h.forwarder.SendTest(c.Request.Context(), exporter); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"code": "DELIVERY_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"delivered": true})
}

func toAuditExporterResponse(exporter db.AuditExporter, head uint, maskCredentials bool) auditExporterResponse {
	urlValue := exporter.URL
	if maskCredentials {
		urlValue = maskCredentialURL(urlValue)
	}
	var lag int64
	if head > exporter.Cursor {
		lag = int64(head - exporter.Cursor)
	}
	return auditExporterResponse{
		ID: exporter.ID, Name: exporter.Name, Kind: exporter.Kind, URL: urlValue,
		TokenSet: exporter.Token != "", Events: exporter.Events, Enabled: exporter.Enabled,
		Cursor: exporter.Cursor, Lag: lag, DeliveredCount: exporter.DeliveredCount,
		LastError: exporter.LastError, LastAttemptAt: exporter.LastAttemptAt,
		LastSuccessAt: exporter.LastSuccessAt, CreatedAt: exporter.CreatedAt, UpdatedAt: exporter.UpdatedAt,
	}
}

func normalizeAuditExporter(body auditExporterRequest, existing db.AuditExporter) (db.AuditExporter, error) {
	exporter := existing
	if name := strings.TrimSpace(body.Name); name != "" {
		if len(name) > 128 {
			return db.AuditExporter{}, errors.New("name is too long")
		}
		exporter.Name = name
	}
	if exporter.Name == "" {
		return db.AuditExporter{}, errors.New("name is required")
	}
	if kind := strings.TrimSpace(body.Kind); kind != "" {
		switch kind {
		case db.AuditExporterKindNDJSON, db.AuditExporterKindSplunkHEC:
			exporter.Kind = kind
		default:
			return db.AuditExporter{}, errors.New("kind must be ndjson or splunk_hec")
		}
	}
	if exporter.Kind == "" {
		return db.AuditExporter{}, errors.New("kind is required")
	}
	if rawURL := strings.TrimSpace(body.URL); rawURL != "" {
		parsed, err := url.Parse(rawURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return db.AuditExporter{}, errors.New("url must be an absolute http(s) URL")
		}
		exporter.URL = rawURL
	}
	if exporter.URL == "" {
		return db.AuditExporter{}, errors.New("url is required")
	}
	if body.Token != nil {
		exporter.Token = strings.TrimSpace(*body.Token)
	}
	if events := strings.TrimSpace(body.Events); events != "" {
		exporter.Events = normalizeEventFilter(events)
	}
	if exporter.Events == "" {
		exporter.Events = "*"
	}
	if body.Enabled != nil {
		exporter.Enabled = *body.Enabled
	}
	return exporter, nil
}

func normalizeEventFilter(events string) string {
	seen := map[string]bool{}
	tokens := make([]string, 0, 4)
	for _, token := range strings.Split(events, ",") {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" || seen[token] {
			continue
		}
		if token == "*" {
			return "*"
		}
		seen[token] = true
		tokens = append(tokens, token)
	}
	if len(tokens) == 0 {
		return "*"
	}
	return strings.Join(tokens, ",")
}

func writeAuditExporterLookupError(c *gin.Context, err error) {
	if err == gorm.ErrRecordNotFound {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "exporter not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
}
