package api

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/identity"
	"depsilo/internal/middleware"
)

type OIDCHandler struct {
	registry  *identity.Registry
	db        *gorm.DB
	auth      config.AuthConfig
	limiter   *loginAttemptLimiter
	ipLimiter *loginAttemptLimiter
	trialMu   sync.Mutex
	trials    map[string]oidcTrial
	preview   authenticationDraftStore
}

func NewOIDCHandler(registry *identity.Registry, database *gorm.DB, auth config.AuthConfig) *OIDCHandler {
	return &OIDCHandler{registry: registry, db: database, auth: auth, limiter: newLoginAttemptLimiter(), ipLimiter: newLoginAttemptLimiter(), trials: make(map[string]oidcTrial)}
}

func (h *OIDCHandler) Providers(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	providers := []gin.H{}
	for _, manager := range h.registry.Managers() {
		if manager.Enabled() {
			providers = append(providers, gin.H{"id": manager.ID(), "name": sourceName(manager), "protocol": manager.Protocol()})
		}
	}
	c.JSON(http.StatusOK, gin.H{"providers": providers})
}

func (h *OIDCHandler) Start(c *gin.Context) {
	var req struct {
		ProviderID string `json:"provider_id"`
		ReturnTo   string `json:"return_to"`
	}
	if !h.browserRequest(c, h.browserManager()) || !h.bind(c, &req) {
		return
	}
	manager, err := h.registry.Select(req.ProviderID)
	if err != nil {
		oidcError(c, err)
		return
	}
	if manager.Protocol() != "oidc" {
		oidcError(c, identity.ErrUnavailable)
		return
	}
	if !h.browserRequest(c, manager) || !h.reserve(c, "start") {
		return
	}
	h.clearChoice(c)
	start, err := manager.Begin(c.Request.Context(), "login", req.ReturnTo, 0, 0)
	if err != nil {
		oidcError(c, err)
		return
	}
	h.cookie(c, manager, flowCookie(manager), start.BrowserToken, 300)
	c.JSON(http.StatusOK, gin.H{"authorization_url": start.AuthorizationURL})
}

func (h *OIDCHandler) Callback(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	id := c.Param("provider_id")
	if id == "" {
		id = identity.ProviderID
	}
	// A failed callback, including a trial, must not complete an unrelated
	// earlier browser flow and replace the current administrator session.
	h.cookie(c, h.browserManager(), "complete", "", -1)
	if h.trialCallback(c, id) {
		return
	}
	manager := h.registry.Manager(id)
	callbackURL, _ := url.Parse(manager.Config().RedirectURL)
	if manager.Enabled() && callbackURL != nil && callbackURL.Path == c.Request.URL.Path {
		browser, _ := c.Cookie(h.cookieName(manager, flowCookie(manager)))
		key, err := manager.Callback(c.Request.Context(), c.Query("state"), browser, c.Query("code"), c.Query("error"))
		if err == nil {
			h.cookie(c, manager, flowCookie(manager), "", -1)
			h.cookie(c, manager, "complete", manager.ID()+"."+key, 60)
		}
	}
	// Never put tokens, identity details or provider errors in redirects.
	c.Redirect(http.StatusSeeOther, "/admin/auth/complete")
}

func (h *OIDCHandler) Complete(c *gin.Context) {
	if h.completeTrial(c) {
		return
	}
	if !h.browserRequest(c, h.browserManager()) {
		return
	}
	key, _ := c.Cookie(h.cookieName(h.browserManager(), "complete"))
	if key == "" {
		h.resumeChoice(c)
		return
	}
	h.cookie(c, h.browserManager(), "complete", "", -1)
	id, tokenKey, found := strings.Cut(key, ".")
	if !found {
		id, tokenKey = identity.ProviderID, key
	}
	manager := h.registry.Manager(id)
	if !manager.Enabled() {
		oidcError(c, identity.ErrExpired)
		return
	}
	result, err := manager.Complete(c.Request.Context(), tokenKey)
	if err != nil {
		oidcError(c, err)
		return
	}
	h.respondIdentityResult(c, manager, result)
}

func (h *OIDCHandler) respondIdentityResult(c *gin.Context, manager *identity.Manager, result identity.Result) {
	if result.Mode == "choice" && result.Choice != nil {
		h.cookie(c, manager, "choice", manager.ID()+"."+result.ChoiceKey, int(time.Until(result.Choice.ExpiresAt).Seconds()))
		h.choiceResponse(c, manager, *result.Choice)
		return
	}
	if result.Mode == "link" {
		c.JSON(http.StatusOK, gin.H{"mode": "link", "result": "linked", "return_to": "/admin/account"})
		return
	}
	token, err := middleware.GenerateIdentityJWT(h.auth.JWTSecret, result.User, result.Session, h.auth.TokenTTL)
	if err != nil {
		oidcError(c, identity.ErrUnavailable)
		return
	}
	expires := time.Now().Add(h.auth.TokenTTL)
	if result.Session.ExpiresAt.Before(expires) {
		expires = result.Session.ExpiresAt
	}
	c.JSON(http.StatusOK, gin.H{"mode": "login", "token": token, "expires_at": expires.Unix(), "return_to": result.ReturnTo})
}

func (h *OIDCHandler) Identities(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok || principal.AuthMethod != middleware.AuthMethodJWT {
		oidcError(c, identity.ErrDenied)
		return
	}
	var user db.User
	if h.db.First(&user, principal.ID).Error != nil {
		oidcError(c, identity.ErrUnavailable)
		return
	}
	var rows []db.ExternalIdentity
	if h.db.Where("user_id = ?", user.ID).Order("created_at ASC").Find(&rows).Error != nil {
		oidcError(c, identity.ErrUnavailable)
		return
	}
	local := (user.PermissionSource == "local" || user.PermissionSource == "") && user.PasswordHash != ""
	items := make([]gin.H, 0, len(rows))
	bound := make(map[string]bool)
	for _, row := range rows {
		manager := h.registry.Manager(row.Provider)
		matches := manager != nil && row.Issuer == manager.Config().Issuer
		protocol := row.LoginProtocol()
		name := strings.ToUpper(protocol)
		if matches {
			name = sourceName(manager)
		}
		bound[row.Provider] = true
		items = append(items, gin.H{"id": row.ID, "provider": row.Provider, "protocol": protocol, "provider_name": name, "provider_matches": matches, "login_available": matches && manager.Enabled(), "display_name": row.DisplayName, "created_at": row.CreatedAt, "last_login_at": row.LastLoginAt, "can_unlink": local})
	}
	source := user.PermissionSource
	if source == "" {
		source = "local"
	}
	providers := make([]gin.H, 0)
	var legacy gin.H
	canLink := false
	for _, manager := range h.registry.Managers() {
		cfg := manager.Config()
		allowed := local && manager.Enabled() && !bound[manager.ID()]
		provider := gin.H{"id": manager.ID(), "protocol": manager.Protocol(), "name": sourceName(manager), "enabled": manager.Enabled(), "can_link": allowed}
		if cfg.SessionTTL > 0 {
			provider["session_ttl_seconds"] = int64(cfg.SessionTTL / time.Second)
		}
		providers = append(providers, provider)
		if manager.ID() == identity.ProviderID {
			legacy = provider
		}
		canLink = canLink || allowed
	}
	// Keep the singleton field for old clients without inventing a default
	// when several explicitly named sources are available.
	if legacy == nil && len(providers) == 1 {
		legacy = providers[0]
	}
	if legacy != nil {
		manager := h.registry.Manager(legacy["id"].(string))
		cfg := manager.Config()
		if cfg.Issuer != "" && manager.Protocol() == "oidc" {
			legacy["issuer"] = cfg.Issuer
		}
		if cfg.RedirectURL != "" {
			legacy["redirect_url"] = cfg.RedirectURL
		}
	}
	response := gin.H{"permission_source": source, "password_enabled": local, "can_link": canLink, "identities": items, "providers": providers, "provider": legacy}
	if principal.CurrentIdentityID != 0 {
		response["current_identity_id"], response["current_provider_id"] = principal.CurrentIdentityID, principal.CurrentProviderID
	}
	c.JSON(http.StatusOK, response)
}

func (h *OIDCHandler) Link(c *gin.Context) {
	if !h.browserRequest(c, nil) {
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
		Password   string `json:"password"`
		ProviderID string `json:"provider_id"`
	}
	if !h.bind(c, &req) {
		return
	}
	manager, err := h.registry.Select(req.ProviderID)
	if err != nil {
		oidcError(c, err)
		return
	}
	if manager.Protocol() != "oidc" {
		oidcError(c, identity.ErrUnavailable)
		return
	}
	user, err := manager.ProvePassword(c.Request.Context(), principal.ID, principal.CredentialVersion, req.Password)
	if err != nil {
		oidcError(c, err)
		return
	}
	start, err := manager.Begin(c.Request.Context(), "link", "/admin/account", user.ID, user.CredentialVersion)
	if err != nil {
		oidcError(c, err)
		return
	}
	h.cookie(c, manager, flowCookie(manager), start.BrowserToken, 300)
	c.JSON(http.StatusOK, gin.H{"authorization_url": start.AuthorizationURL})
}

func (h *OIDCHandler) Unlink(c *gin.Context) {
	if !h.browserRequest(c, nil) {
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
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid identity"})
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !h.bind(c, &req) {
		return
	}
	if err := h.registry.Unlink(c.Request.Context(), principal.ID, principal.CredentialVersion, uint(id), req.Password); err != nil {
		oidcError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *OIDCHandler) browserRequest(c *gin.Context, manager *identity.Manager) bool {
	choiceManager, _ := h.choiceCookie(c)
	if manager != nil && manager.Protocol() == "ldap" || choiceManager != nil && choiceManager.Protocol() == "ldap" || strings.HasPrefix(c.Request.URL.Path, "/api/v1/auth/identity/") && h.browserManager() == nil {
		return h.directoryBrowserRequest(c)
	}
	c.Header("Cache-Control", "no-store")
	// JSON plus same-origin validation rejects form-based login/link CSRF,
	// including sibling origins that SameSite cookies alone would accept.
	if c.ContentType() != "application/json" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "JSON request required"})
		return false
	}
	if site := c.GetHeader("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		oidcError(c, identity.ErrDenied)
		return false
	}
	if origin := c.GetHeader("Origin"); origin != "" {
		// Account unlinking and configuration checks must still work after an operator removes or
		// replaces the provider. Validate against this browser request's host,
		// never against a now-obsolete callback configuration.
		if strings.HasPrefix(c.Request.URL.Path, "/api/v1/auth/identities/") || c.Request.URL.Path == "/api/v1/admin/authentication/check" {
			u, err := url.Parse(origin)
			if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host == c.Request.Host && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" {
				return true
			}
			oidcError(c, identity.ErrDenied)
			return false
		}
		expected, err := url.Parse(manager.Config().RedirectURL)
		actual, parseErr := url.Parse(origin)
		if err != nil || parseErr != nil || expected.Host == "" || !sameOIDCOrigin(expected, actual) {
			oidcError(c, identity.ErrDenied)
			return false
		}
	}
	return true
}

func sameOIDCOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if u.Port() != "" {
			return u.Port()
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return a.Scheme == b.Scheme && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b) && b.User == nil && b.Path == "" && b.RawQuery == "" && b.Fragment == ""
}

func (h *OIDCHandler) bind(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxLoginRequestBody)
	if err := c.ShouldBindJSON(target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid request body"})
		return false
	}
	return true
}

func (h *OIDCHandler) reserve(c *gin.Context, key string) bool {
	allowed, retry := h.ipLimiter.reserve(c.ClientIP(), "oidc")
	if allowed {
		allowed, retry = h.limiter.reserve("oidc", key+":"+c.ClientIP())
	}
	// Password guessing is also bounded per account across source addresses.
	if allowed && strings.HasPrefix(key, "proof:") {
		allowed, retry = h.limiter.reserve("account", key)
	}
	if !allowed {
		c.Header("Retry-After", strconv.FormatInt(int64((retry+time.Second-1)/time.Second), 10))
		oidcError(c, identity.ErrRateLimited)
		return false
	}
	return true
}

func sourceName(manager *identity.Manager) string {
	if name := manager.Config().DisplayName; name != "" {
		return name
	}
	if manager != nil && manager.Protocol() == "ldap" {
		return "LDAP"
	}
	return "OIDC"
}

// Enabled callbacks share one configured browser origin. Disabled sources
// remain inspectable, but cannot determine the active completion cookie scope.
func (h *OIDCHandler) browserManager() *identity.Manager {
	for _, manager := range h.registry.Managers() {
		if manager.Enabled() && manager.Protocol() == "oidc" {
			return manager
		}
	}
	for _, manager := range h.registry.Managers() {
		if manager.Config().RedirectURL != "" {
			return manager
		}
	}
	return nil
}

func flowCookie(manager *identity.Manager) string {
	if manager.ID() == identity.ProviderID {
		return "flow"
	}
	return "flow_" + manager.ID()
}

func (h *OIDCHandler) cookieName(manager *identity.Manager, kind string) string {
	if strings.HasPrefix(manager.Config().RedirectURL, "https://") {
		return "__Host-depsilo_oidc_" + kind
	}
	return "depsilo_oidc_" + kind
}

func (h *OIDCHandler) cookie(c *gin.Context, manager *identity.Manager, kind, value string, age int) {
	if manager != nil && manager.Protocol() == "ldap" {
		http.SetCookie(c.Writer, &http.Cookie{Name: directoryCookieName(c, kind), Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: directorySecure(c), SameSite: http.SameSiteLaxMode})
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: h.cookieName(manager, kind), Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: strings.HasPrefix(manager.Config().RedirectURL, "https://"), SameSite: http.SameSiteLaxMode})
}

func oidcError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "OIDC_UNAVAILABLE"
	switch {
	case errors.Is(err, identity.ErrLDAPUnavailable):
		status, code = http.StatusServiceUnavailable, "LDAP_UNAVAILABLE"
	case errors.Is(err, identity.ErrLDAPCredentials):
		status, code = http.StatusForbidden, "LDAP_AUTHENTICATION_FAILED"
	case errors.Is(err, identity.ErrSelection):
		status, code = http.StatusBadRequest, "PROVIDER_REQUIRED"
	case errors.Is(err, identity.ErrExpired):
		status, code = http.StatusBadRequest, "LOGIN_EXPIRED"
	case errors.Is(err, identity.ErrDenied):
		status, code = http.StatusForbidden, "ACCESS_DENIED"
	case errors.Is(err, identity.ErrProof):
		status, code = http.StatusForbidden, "REAUTHENTICATION_FAILED"
	case errors.Is(err, identity.ErrBound):
		status, code = http.StatusConflict, "IDENTITY_ALREADY_BOUND"
	case errors.Is(err, identity.ErrRateLimited):
		status, code = http.StatusTooManyRequests, "RATE_LIMITED"
	}
	c.JSON(status, gin.H{"code": code, "message": code})
}
