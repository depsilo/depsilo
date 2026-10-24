package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"depsilo/internal/api/admin"
	"depsilo/internal/api/credentialurl"
	"depsilo/internal/config"
	"depsilo/internal/identity"
	"depsilo/internal/middleware"
	"github.com/gin-gonic/gin"
)

// Directory passwords are accepted only as same-origin JSON over browser HTTPS
// (including TLS termination) or a loopback development origin. They are never
// placed in URLs, redirects, cookies, logs or durable authentication records.
func (h *OIDCHandler) directoryBrowserRequest(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	if c.ContentType() != "application/json" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "JSON request required"})
		return false
	}
	if site := c.GetHeader("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		oidcError(c, identity.ErrDenied)
		return false
	}
	origin, err := url.Parse(c.GetHeader("Origin"))
	if err != nil || origin.Host == "" || !strings.EqualFold(origin.Host, c.Request.Host) || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		oidcError(c, identity.ErrDenied)
		return false
	}
	if !credentialurl.SecureTransport(c.Request) {
		c.JSON(http.StatusForbidden, gin.H{"code": "HTTPS_REQUIRED", "message": "directory login requires HTTPS or loopback"})
		return false
	}
	return true
}

func directorySecure(c *gin.Context) bool {
	origin, _ := url.Parse(c.GetHeader("Origin"))
	return c.Request.TLS != nil || origin != nil && origin.Scheme == "https"
}

func directoryCookieName(c *gin.Context, kind string) string {
	if directorySecure(c) {
		return "__Host-depsilo_oidc_" + kind
	}
	return "depsilo_oidc_" + kind
}

func (h *OIDCHandler) LDAPLogin(c *gin.Context) {
	if !h.directoryBrowserRequest(c) {
		return
	}
	var req struct {
		ProviderID string `json:"provider_id"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		ReturnTo   string `json:"return_to"`
	}
	if !h.bindDirectoryRequest(c, &req, "provider_id", "username", "password", "return_to") {
		return
	}
	if !h.reserve(c, "proof:ldap:"+req.ProviderID+":"+strings.ToLower(strings.TrimSpace(req.Username))) {
		return
	}
	m, err := h.registry.Select(req.ProviderID)
	if err != nil {
		ldapError(c, err)
		return
	}
	if m.Protocol() != "ldap" {
		ldapError(c, identity.ErrLDAPUnavailable)
		return
	}
	h.clearChoice(c)
	h.cookie(c, m, "complete", "", -1)
	result, err := m.LDAPAuthenticate(c.Request.Context(), "login", req.ReturnTo, req.Username, req.Password, 0, 0)
	if err != nil {
		ldapError(c, err)
		return
	}
	h.respondIdentityResult(c, m, result)
}

func (h *OIDCHandler) LDAPLink(c *gin.Context) {
	if !h.directoryBrowserRequest(c) {
		return
	}
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok || principal.AuthMethod != middleware.AuthMethodJWT {
		oidcError(c, identity.ErrDenied)
		return
	}
	if !h.reserve(c, "proof:"+strconv.FormatUint(uint64(principal.ID), 10)) {
		return
	}
	var req struct {
		ProviderID        string `json:"provider_id"`
		Password          string `json:"password"`
		Username          string `json:"username"`
		DirectoryPassword string `json:"directory_password"`
	}
	if !h.bindDirectoryRequest(c, &req, "provider_id", "password", "username", "directory_password") {
		return
	}
	m, err := h.registry.Select(req.ProviderID)
	if err != nil {
		ldapError(c, err)
		return
	}
	if m.Protocol() != "ldap" {
		ldapError(c, identity.ErrLDAPUnavailable)
		return
	}
	user, err := m.ProvePassword(c.Request.Context(), principal.ID, principal.CredentialVersion, req.Password)
	if err != nil {
		oidcError(c, err)
		return
	}
	// Directory guesses share the login endpoint's per-account budget even
	// when several already-authenticated local users attempt a binding.
	allowed, retry := h.limiter.reserve("account", "proof:ldap:"+req.ProviderID+":"+strings.ToLower(strings.TrimSpace(req.Username)))
	if !allowed {
		c.Header("Retry-After", strconv.FormatInt(int64((retry+time.Second-1)/time.Second), 10))
		oidcError(c, identity.ErrRateLimited)
		return
	}
	result, err := m.LDAPAuthenticate(c.Request.Context(), "link", "/admin/account", req.Username, req.DirectoryPassword, user.ID, user.CredentialVersion)
	if err != nil {
		ldapError(c, err)
		return
	}
	h.respondIdentityResult(c, m, result)
}

func (h *OIDCHandler) bindDirectoryRequest(c *gin.Context, target any, fields ...string) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxLoginRequestBody)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || !exactJSONObject(body, fields...) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid directory login request"})
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid directory login request"})
		return false
	}
	return true
}

// exactJSONObject rejects duplicate, aliased and null fields before typed
// decoding, so ambiguous browser input never reaches credential verification.
func exactJSONObject(body []byte, fields ...string) bool {
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || seen[key] {
			return false
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	token, err = decoder.Token()
	return err == nil && token == json.Delim('}') && decoder.Decode(new(any)) == io.EOF
}

type ldapAuthenticationDraftStore interface {
	PreviewLDAPAuthenticationSourceDraft(context.Context, config.AuthenticationSourceDraft) (config.AuthenticationSourceConfig, error)
}

func (h *OIDCHandler) ldapPreview(c *gin.Context, trial bool) (*identity.Manager, string, string) {
	store, ok := h.preview.(ldapAuthenticationDraftStore)
	if !ok {
		ldapError(c, identity.ErrUnavailable)
		return nil, "", ""
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	body, err := io.ReadAll(c.Request.Body)
	fields := []string{"id", "protocol", "create", "settings"}
	if trial {
		fields = append(fields, "username", "password")
	}
	var envelope struct {
		ID       string          `json:"id"`
		Protocol string          `json:"protocol"`
		Create   *bool           `json:"create"`
		Settings json.RawMessage `json:"settings"`
		Username string          `json:"username"`
		Password string          `json:"password"`
	}
	if err != nil || !exactJSONObject(body, fields...) || json.Unmarshal(body, &envelope) != nil || envelope.Protocol != "ldap" || envelope.Create == nil || !admin.ValidAuthenticationProtocolSettingsObject(envelope.Settings, "ldap") {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid LDAP authentication draft"})
		return nil, "", ""
	}
	var patch config.OIDCSettingsPatch
	if json.Unmarshal(envelope.Settings, &patch) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid LDAP authentication settings"})
		return nil, "", ""
	}
	cfg, err := store.PreviewLDAPAuthenticationSourceDraft(c.Request.Context(), config.AuthenticationSourceDraft{ID: envelope.ID, Protocol: envelope.Protocol, Create: *envelope.Create, Settings: patch})
	if err != nil {
		admin.WriteAuthenticationError(c, err)
		return nil, "", ""
	}
	m, err := identity.NewLDAPTrial(envelope.ID, cfg)
	if err != nil {
		ldapError(c, identity.ErrUnavailable)
		return nil, "", ""
	}
	return m, envelope.Username, envelope.Password
}

func (h *OIDCHandler) LDAPCheck(c *gin.Context) {
	if !h.directoryBrowserRequest(c) || !h.reserve(c, "admin-check") {
		return
	}
	m, _, _ := h.ldapPreview(c, false)
	if m == nil {
		return
	}
	err := m.CheckLDAP(c.Request.Context())
	h.auditTrial(c, m.ID(), "identity.check", err == nil)
	if err != nil {
		ldapError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"checked_at": time.Now().UTC(), "configuration_valid": true, "tls_valid": true, "bind_valid": true, "directory_readable": true})
}

func (h *OIDCHandler) LDAPTrial(c *gin.Context) {
	if !h.directoryBrowserRequest(c) {
		return
	}
	owner, ok := middleware.PrincipalFromContext(c)
	if !ok || owner.AuthMethod != middleware.AuthMethodJWT || owner.LoginMethod != "password" || !owner.CanManageCredentials {
		oidcError(c, identity.ErrDenied)
		return
	}
	if !h.reserve(c, "admin-trial") {
		return
	}
	m, username, password := h.ldapPreview(c, true)
	if m == nil {
		return
	}
	result, err := m.LDAPAuthenticate(c.Request.Context(), "test", "/admin/authentication?source="+url.QueryEscape(m.ID()), username, password, 0, 0)
	if err != nil || result.Trial == nil {
		h.auditTrial(c, m.ID(), "identity.trial.complete", false)
		ldapError(c, err)
		return
	}
	h.auditTrial(c, m.ID(), "identity.trial.complete", result.Trial.Outcome == "success")
	c.JSON(http.StatusOK, gin.H{"mode": "test", "provider": gin.H{"id": m.ID(), "name": sourceName(m), "protocol": "ldap"}, "outcome": result.Trial.Outcome, "authenticated": result.Trial.Authenticated, "mapped_role": result.Trial.MappedRole, "display_name": result.Trial.DisplayName, "code": result.Trial.Code, "checked_at": result.Trial.CheckedAt, "return_to": result.ReturnTo})
}

func ldapError(c *gin.Context, err error) {
	if err == nil || errors.Is(err, identity.ErrUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "LDAP_UNAVAILABLE", "message": "LDAP_UNAVAILABLE"})
		return
	}
	oidcError(c, err)
}
