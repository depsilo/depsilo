package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"depsilo/internal/api/credentialurl"
	"depsilo/internal/config"
	"github.com/gin-gonic/gin"
)

type authenticationSourceStore interface {
	AuthenticationSourceSettings(context.Context) (config.AuthenticationSourcesState, error)
	CreateAuthenticationSource(context.Context, config.AuthenticationSourceCreate) (config.AuthenticationSourceState, error)
	UpdateAuthenticationSource(context.Context, string, config.OIDCSettingsPatch) (config.AuthenticationSourceState, error)
	DeleteAuthenticationSource(context.Context, string) error
	RestoreAuthenticationSource(context.Context, string) (config.AuthenticationSourceState, error)
}

type AuthenticationSourceHandler struct{ store authenticationSourceStore }

func NewAuthenticationSourceHandler(store authenticationSourceStore) *AuthenticationSourceHandler {
	return &AuthenticationSourceHandler{store: store}
}

func (h *AuthenticationSourceHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	state, err := h.store.AuthenticationSourceSettings(c.Request.Context())
	if err != nil {
		WriteAuthenticationError(c, err)
		return
	}
	c.JSON(http.StatusOK, state)
}
func (h *AuthenticationSourceHandler) Create(c *gin.Context) {
	var create config.AuthenticationSourceCreate
	if !DecodeAuthenticationSourceRequest(c, &create, true) {
		return
	}
	if !allowLDAPSecretTransport(c, create.Protocol, create.OIDCSettingsPatch) {
		return
	}
	state, err := h.store.CreateAuthenticationSource(c.Request.Context(), create)
	if err != nil {
		WriteAuthenticationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, state)
}
func (h *AuthenticationSourceHandler) Update(c *gin.Context) {
	state, err := h.store.AuthenticationSourceSettings(c.Request.Context())
	if err != nil {
		WriteAuthenticationError(c, err)
		return
	}
	protocol := "oidc"
	for _, source := range state.Sources {
		if source.ID == c.Param("id") {
			protocol = source.Protocol
			break
		}
	}
	var patch config.OIDCSettingsPatch
	if !decodeAuthenticationSourceRequest(c, &patch, false, protocol) {
		return
	}
	if !allowLDAPSecretTransport(c, protocol, patch) {
		return
	}
	updated, err := h.store.UpdateAuthenticationSource(c.Request.Context(), c.Param("id"), patch)
	if err != nil {
		WriteAuthenticationError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}
func (h *AuthenticationSourceHandler) Delete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if err := h.store.DeleteAuthenticationSource(c.Request.Context(), c.Param("id")); err != nil {
		WriteAuthenticationError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func allowLDAPSecretTransport(c *gin.Context, protocol string, patch config.OIDCSettingsPatch) bool {
	if protocol == "ldap" && patch.LDAP != nil && patch.LDAP.BindPassword != nil && !credentialurl.SecureTransport(c.Request) {
		c.JSON(http.StatusForbidden, gin.H{"code": "HTTPS_REQUIRED", "message": "directory credentials require HTTPS or loopback"})
		return false
	}
	return true
}

func (h *AuthenticationSourceHandler) Restore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	body, err := io.ReadAll(c.Request.Body)
	var object map[string]json.RawMessage
	if err != nil || !validAuthenticationObject(body) || json.Unmarshal(body, &object) != nil || len(object) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "expected an empty restoration object"})
		return
	}
	state, err := h.store.RestoreAuthenticationSource(c.Request.Context(), c.Param("id"))
	if err != nil {
		WriteAuthenticationError(c, err)
		return
	}
	c.JSON(http.StatusOK, state)
}

func DecodeAuthenticationSourceRequest(c *gin.Context, target any, create bool) bool {
	return decodeAuthenticationSourceRequest(c, target, create, "oidc")
}

func decodeAuthenticationSourceRequest(c *gin.Context, target any, create bool, protocol string) bool {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	body, err := io.ReadAll(c.Request.Body)
	if create {
		var selector struct {
			Protocol string `json:"protocol"`
		}
		if json.Unmarshal(body, &selector) == nil && selector.Protocol == "ldap" {
			protocol = "ldap"
		}
	}
	if err != nil || !validAuthenticationProtocolObject(body, create, protocol) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "expected one authentication object with known, non-null fields"})
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid authentication field or value"})
		return false
	}
	return true
}

// WriteAuthenticationError exposes stable field codes without reflecting any
// submitted value, provider payload, or raw decoder error.
func WriteAuthenticationError(c *gin.Context, err error) {
	if fields := config.AuthenticationFieldErrors(err); len(fields) > 0 {
		status := http.StatusUnprocessableEntity
		code := "INVALID_SETTING"
		var storeErr *config.StoreError
		if errors.As(err, &storeErr) && storeErr.Code == config.StoreAuthenticationSourceConflict {
			status = http.StatusConflict
			code = string(storeErr.Code)
		}
		c.JSON(status, gin.H{"code": code, "message": "authentication configuration requires attention", "field_errors": fields})
		return
	}

	status := http.StatusInternalServerError
	code := "INTERNAL_ERROR"
	message := "authentication operation failed"
	var storeErr *config.StoreError
	if errors.As(err, &storeErr) {
		switch storeErr.Code {
		case config.StoreInvalidSetting:
			status = http.StatusUnprocessableEntity
			code = string(storeErr.Code)
			message = "invalid authentication configuration"
		case config.StoreAuthenticationSourceNotFound:
			status = http.StatusNotFound
			code = string(storeErr.Code)
			message = "authentication source was not found"
		case config.StoreAuthenticationSourceConflict:
			status = http.StatusConflict
			code = string(storeErr.Code)
			message = "authentication source state conflicts with this operation"
		case config.StoreConfigReadOnly:
			status = http.StatusConflict
			code = string(storeErr.Code)
			message = "authentication configuration is read-only"
		case config.StoreConfigReadFailed:
			code = string(storeErr.Code)
			message = "cannot read authentication configuration"
		case config.StoreConfigWriteFailed:
			code = string(storeErr.Code)
			message = "cannot save authentication configuration"
		}
	}
	c.JSON(status, gin.H{"code": code, "message": message})
}
