package admin

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/api/contract"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/identity"
)

// authenticationImpactUserRow is one bound account as the impact query reads it.
type authenticationImpactUserRow struct {
	ID               uint
	Username         string
	PermissionSource string
	Enabled          bool
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
	result := contract.AuthenticationImpact{SourceID: id, ObservedAt: time.Now().UTC(), Users: []contract.AuthenticationImpactUser{}}
	var boundUsers []authenticationImpactUserRow
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
		if err := users().Select("id, username, permission_source, enabled").Order("id ASC").Limit(20).Scan(&boundUsers).Error; err != nil {
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
	for _, user := range boundUsers {
		source := contract.IdentityPermissionSource(user.PermissionSource)
		if source == "" {
			source = contract.IdentityPermissionSourceLocal
		}
		result.Users = append(result.Users, contract.AuthenticationImpactUser{ID: user.ID, Username: user.Username, PermissionSource: source, Enabled: user.Enabled})
	}
	result.UsersTruncated = result.BoundUserCount > int64(len(result.Users))
	c.JSON(http.StatusOK, result)
}

func writeImpactUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{"code": "AUTH_SOURCE_IMPACT_UNAVAILABLE", "message": "authentication source impact is temporarily unavailable"})
}
