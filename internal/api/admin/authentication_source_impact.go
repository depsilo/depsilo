package admin

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/identity"
)

type AuthenticationSourceImpactUser struct {
	ID               uint   `json:"id"`
	Username         string `json:"username"`
	PermissionSource string `json:"permission_source"`
	Enabled          bool   `json:"enabled"`
}

type AuthenticationSourceImpact struct {
	SourceID                  string                           `json:"source_id"`
	BoundUserCount            int64                            `json:"bound_user_count"`
	DependentManagedUserCount int64                            `json:"dependent_managed_user_count"`
	ActiveSessionCount        int64                            `json:"active_session_count"`
	ObservedAt                time.Time                        `json:"observed_at"`
	Users                     []AuthenticationSourceImpactUser `json:"users"`
	UsersTruncated            bool                             `json:"users_truncated"`
}

type AuthenticationSourceImpactHandler struct {
	store    authenticationSourceStore
	database *gorm.DB
	registry *identity.Registry
}

func NewAuthenticationSourceImpactHandler(store authenticationSourceStore, database *gorm.DB, registry *identity.Registry) *AuthenticationSourceImpactHandler {
	return &AuthenticationSourceImpactHandler{store: store, database: database, registry: registry}
}

func (h *AuthenticationSourceImpactHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := c.Param("id")
	if err := config.ValidateAuthenticationSourceID(id); err != nil {
		WriteAuthenticationError(c, err)
		return
	}
	state, err := h.store.AuthenticationSourceSettings(c.Request.Context())
	if err != nil {
		WriteAuthenticationError(c, err)
		return
	}
	found := false
	for _, source := range state.Sources {
		if source.ID == id {
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"code": "AUTH_SOURCE_NOT_FOUND", "message": "authentication source is not configured or running"})
		return
	}
	result := AuthenticationSourceImpact{SourceID: id, ObservedAt: time.Now().UTC(), Users: []AuthenticationSourceImpactUser{}}
	if h.database == nil {
		writeImpactUnavailable(c)
		return
	}
	err = h.database.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		identities := tx.Model(&db.ExternalIdentity{}).Select("user_id").Where("provider = ?", id)
		users := func() *gorm.DB { return tx.Model(&db.User{}).Where("id IN (?)", identities) }
		if err := users().Count(&result.BoundUserCount).Error; err != nil {
			return err
		}
		otherIdentities := tx.Model(&db.ExternalIdentity{}).Select("user_id").Where("provider <> ?", id)
		if err := users().Where("permission_source IN ? AND id NOT IN (?)", []string{"oidc", "ldap"}, otherIdentities).Count(&result.DependentManagedUserCount).Error; err != nil {
			return err
		}
		if err := users().Select("id, username, permission_source, enabled").Order("id ASC").Limit(20).Scan(&result.Users).Error; err != nil {
			return err
		}
		// Count eligible durable session records, not browser tabs or issued JWTs.
		// Individual JWTs may already be gone or carry an obsolete credential version.
		manager := h.registry.Manager(id)
		if manager.Enabled() {
			source := manager.Config()
			return tx.Model(&db.IdentitySession{}).
				Joins("JOIN external_identities ON external_identities.id = identity_sessions.identity_id AND external_identities.user_id = identity_sessions.user_id").
				Joins("JOIN users ON users.id = identity_sessions.user_id").
				Where("external_identities.provider = ? AND external_identities.issuer = ? AND external_identities.subject <> ?", id, source.Issuer, "").
				Where("identity_sessions.revoked_at IS NULL AND identity_sessions.expires_at > ? AND identity_sessions.config_fingerprint = ?", result.ObservedAt, manager.ConfigFingerprint()).
				Where("users.enabled = ? AND users.role IN ? AND identity_sessions.role IN ?", true, []string{"admin", "readonly"}, []string{"admin", "readonly"}).
				Where("users.permission_source IN ?", []string{"local", "oidc", "ldap", ""}).Count(&result.ActiveSessionCount).Error
		}
		return nil
	})
	if err != nil {
		writeImpactUnavailable(c)
		return
	}
	for i := range result.Users {
		if result.Users[i].PermissionSource == "" {
			result.Users[i].PermissionSource = "local"
		}
	}
	result.UsersTruncated = result.BoundUserCount > int64(len(result.Users))
	c.JSON(http.StatusOK, result)
}

func writeImpactUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AUTH_SOURCE_IMPACT_UNAVAILABLE", "message": "authentication source impact is temporarily unavailable"})
}
