package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/compilecache"
	"depsilo/internal/db"
)

// CompileCacheHandler exposes the Operator control plane for compiler-cache
// status, scoped build credentials and manual LRU cleanup.
type CompileCacheHandler struct {
	db        *gorm.DB
	service   *compilecache.Service
	enabled   bool
	publicURL string
}

// NewCompileCacheHandler constructs the compiler-cache Admin handler.
func NewCompileCacheHandler(database *gorm.DB, service *compilecache.Service, enabled bool, publicURL string) *CompileCacheHandler {
	return &CompileCacheHandler{db: database, service: service, enabled: enabled, publicURL: strings.TrimRight(publicURL, "/")}
}

type createCompileCacheCredentialRequest struct {
	Name        string `json:"name" binding:"required"`
	Namespace   string `json:"namespace" binding:"required"`
	Permissions string `json:"permissions" binding:"required,oneof=readonly readwrite"`
	TTLDays     *int   `json:"ttl_days"`
}

type compileCacheCredentialResponse struct {
	ID          uint       `json:"id"`
	Name        string     `json:"name"`
	Namespace   string     `json:"namespace"`
	Permissions string     `json:"permissions"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Status returns service capacity and protocol-specific client endpoints.
func (h *CompileCacheHandler) Status(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if !h.enabled || h.service == nil {
		c.JSON(http.StatusOK, gin.H{"enabled": false})
		return
	}
	stats, err := h.service.Stats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "STORAGE_UNAVAILABLE", "message": "compiler-cache stats are unavailable"})
		return
	}
	ccacheEndpoint := h.compileCacheEndpoint("{namespace}")
	c.JSON(http.StatusOK, gin.H{
		"enabled": true,
		"endpoints": gin.H{
			"ccache":  ccacheEndpoint,
			"sccache": h.sccacheEndpoint("{namespace}"),
		},
		"endpoint": ccacheEndpoint,
		"stats":    stats,
	})
}

// ListCredentials lists active build credentials without token material.
func (h *CompileCacheHandler) ListCredentials(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	var credentials []db.CompileCacheCredential
	if err := h.db.WithContext(c.Request.Context()).
		Select("id, name, namespace, permissions, expires_at, last_used_at, created_at, updated_at").
		Where("revoked_at IS NULL").
		Order("namespace ASC, name ASC").Find(&credentials).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "failed to list compiler-cache credentials"})
		return
	}
	items := make([]compileCacheCredentialResponse, len(credentials))
	for index, credential := range credentials {
		items[index] = compileCacheCredentialResponse{
			ID: credential.ID, Name: credential.Name, Namespace: credential.Namespace,
			Permissions: credential.Permissions, ExpiresAt: credential.ExpiresAt,
			LastUsedAt: credential.LastUsedAt, CreatedAt: credential.CreatedAt, UpdatedAt: credential.UpdatedAt,
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// CreateCredential creates a namespace-scoped credential and returns its
// ccache and sccache configurations exactly once.
func (h *CompileCacheHandler) CreateCredential(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if !h.enabled || h.publicURL == "" {
		c.JSON(http.StatusConflict, gin.H{"code": "DISABLED", "message": "enable and configure compiler cache before creating credentials"})
		return
	}
	var request createCompileCacheCredentialRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len(request.Name) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "name must contain 1-128 characters"})
		return
	}
	namespace, err := compilecache.NormalizeNamespace(request.Namespace)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	ttlDays := 90
	if request.TTLDays != nil {
		ttlDays = *request.TTLDays
	}
	if ttlDays < 0 || ttlDays > 3650 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "ttl_days must be between 0 and 3650"})
		return
	}
	var count int64
	if err := h.db.WithContext(c.Request.Context()).Model(&db.CompileCacheCredential{}).
		Where("namespace = ? AND name = ?", namespace, request.Name).Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "failed to validate credential name"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"code": "NAME_EXISTS", "message": "a credential with this name already exists in the namespace"})
		return
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "TOKEN_GENERATION_FAILED", "message": "failed to generate credential"})
		return
	}
	plainToken := "depsilo_cc_" + hex.EncodeToString(tokenBytes)
	digest := sha256.Sum256([]byte(plainToken))
	var expiresAt *time.Time
	if ttlDays > 0 {
		expires := time.Now().UTC().AddDate(0, 0, ttlDays)
		expiresAt = &expires
	}
	createdBy, _ := c.Get("user_id")
	createdByID, _ := createdBy.(uint)
	credential := db.CompileCacheCredential{
		Name: request.Name, Namespace: namespace,
		TokenHash: hex.EncodeToString(digest[:]), Permissions: request.Permissions,
		ExpiresAt: expiresAt, CreatedBy: createdByID,
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&credential).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_FAILED", "message": "failed to create compiler-cache credential"})
		return
	}
	ccacheEndpoint := h.compileCacheEndpoint(namespace)
	sccacheEndpoint := h.sccacheEndpoint(namespace)
	remoteStorage := compileCacheRemoteStorage(ccacheEndpoint, plainToken, credential.Permissions)
	c.JSON(http.StatusCreated, gin.H{
		"id": credential.ID, "name": credential.Name, "namespace": credential.Namespace,
		"permissions": credential.Permissions, "expires_at": credential.ExpiresAt,
		"token": plainToken,
		"endpoints": gin.H{
			"ccache":  ccacheEndpoint,
			"sccache": sccacheEndpoint,
		},
		"ccache_remote_storage": remoteStorage,
		"sccache_config":        compileCacheSCCacheConfig(sccacheEndpoint, plainToken),
		// Keep the original ccache-only fields while existing Operators and API
		// clients move to the explicit per-client fields above.
		"endpoint":       ccacheEndpoint,
		"remote_storage": remoteStorage,
		"warning":        "save this token now — it will not be shown again",
	})
}

func compileCacheRemoteStorage(endpoint, token, permissions string) string {
	attributes := make([]string, 0, 2)
	if permissions == "readonly" {
		// Keep cache misses quiet: tell ccache not to attempt a PUT that the
		// server would reject anyway.
		attributes = append(attributes, "read-only=true")
	}
	bearerAttribute := "bearer-token=" + token
	if strings.HasPrefix(strings.ToLower(endpoint), "https://") {
		// HTTPS is handled by ccache 4.13+ storage helpers. Helpers receive
		// backend-specific attributes only when they carry the '@' prefix.
		bearerAttribute = "@" + bearerAttribute
	}
	attributes = append(attributes, bearerAttribute)
	// Pipes are accepted as attribute separators and avoid shell-sensitive
	// whitespace in the copyable environment-variable value.
	return endpoint + "|" + strings.Join(attributes, "|")
}

func compileCacheSCCacheConfig(endpoint, token string) string {
	return "[cache.webdav]\n" +
		"endpoint = " + strconv.Quote(endpoint) + "\n" +
		"token = " + strconv.Quote(token)
}

// DeleteCredential revokes a build credential idempotently.
func (h *CompileCacheHandler) DeleteCredential(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid credential id"})
		return
	}
	now := time.Now().UTC()
	actor, _ := c.Get("user_id")
	actorID, _ := actor.(uint)
	result := h.db.WithContext(c.Request.Context()).Model(&db.CompileCacheCredential{}).
		Where("id = ? AND revoked_at IS NULL", uint(id)).
		Updates(map[string]any{"revoked_at": &now, "revoked_by": actorID})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "failed to revoke credential"})
		return
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := h.db.WithContext(c.Request.Context()).Model(&db.CompileCacheCredential{}).Where("id = ?", uint(id)).Count(&count).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "failed to verify credential"})
			return
		}
		if count == 0 {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "credential not found"})
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// Cleanup triggers compiler-cache LRU reclamation.
func (h *CompileCacheHandler) Cleanup(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	if !h.enabled || h.service == nil {
		c.JSON(http.StatusConflict, gin.H{"code": "DISABLED", "message": "compiler cache is disabled"})
		return
	}
	if err := h.service.FlushTouches(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "STORAGE_UNAVAILABLE", "message": "failed to flush compiler-cache access metadata"})
		return
	}
	result, err := h.service.Cleanup(c.Request.Context())
	if err != nil && !errors.Is(err, compilecache.ErrInsufficientStorage) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "STORAGE_UNAVAILABLE", "message": "compiler-cache cleanup failed"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *CompileCacheHandler) compileCacheEndpoint(namespace string) string {
	return h.publicURL + "/ccache/v1/" + namespace
}

func (h *CompileCacheHandler) sccacheEndpoint(namespace string) string {
	return h.publicURL + "/sccache/v1/" + namespace
}
