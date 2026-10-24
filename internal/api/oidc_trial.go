package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"depsilo/internal/api/admin"
	"depsilo/internal/audit"
	"depsilo/internal/config"
	"depsilo/internal/identity"
	"depsilo/internal/middleware"
	"github.com/gin-gonic/gin"
)

type authenticationDraftStore interface {
	PreviewAuthenticationSourceDraft(context.Context, config.AuthenticationSourceDraft) (config.OIDCConfig, error)
}

type oidcTrial struct {
	Manager           *identity.Manager
	Owner             uint
	CredentialVersion uint64
	ExpiresAt         time.Time
}

func (h *OIDCHandler) ConfigureDraftPreview(store authenticationDraftStore) { h.preview = store }

func (h *OIDCHandler) previewManager(c *gin.Context) *identity.Manager {
	if h.preview == nil {
		oidcError(c, identity.ErrUnavailable)
		return nil
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	body, err := io.ReadAll(c.Request.Body)
	draft, valid := decodeAuthenticationDraft(body)
	if err != nil || !valid {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid authentication draft"})
		return nil
	}
	cfg, err := h.preview.PreviewAuthenticationSourceDraft(c.Request.Context(), draft)
	if err != nil {
		admin.WriteAuthenticationError(c, err)
		return nil
	}
	m, err := identity.NewTrial(draft.ID, cfg)
	if err != nil {
		oidcError(c, identity.ErrUnavailable)
		return nil
	}
	return m
}

// Use exact, non-null envelope fields and the same settings syntax as CRUD.
// A diagnostic must never silently check a different candidate than the form.
func decodeAuthenticationDraft(body []byte) (config.AuthenticationSourceDraft, bool) {
	var draft config.AuthenticationSourceDraft
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return draft, false
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return draft, false
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return draft, false
		}
		switch key {
		case "id":
			err = json.Unmarshal(value, &draft.ID)
		case "protocol":
			err = json.Unmarshal(value, &draft.Protocol)
		case "create":
			err = json.Unmarshal(value, &draft.Create)
		case "settings":
			if !admin.ValidAuthenticationSettingsObject(value) {
				return draft, false
			}
			err = json.Unmarshal(value, &draft.Settings)
		default:
			return draft, false
		}
		if err != nil {
			return draft, false
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return draft, false
	}
	return draft, seen["id"] && seen["protocol"] && seen["create"] && seen["settings"]
}

func (h *OIDCHandler) Check(c *gin.Context) {
	if !h.browserRequest(c, nil) {
		return
	}
	if !h.reserve(c, "admin-check") {
		return
	}
	m := h.previewManager(c)
	if m == nil {
		return
	}
	err := m.Check(c.Request.Context())
	h.auditTrial(c, m.ID(), "identity.check", err == nil)
	if err != nil {
		oidcError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"checked_at": time.Now().UTC(), "configuration_valid": true, "discovery_valid": true, "jwks_readable": true})
}

func (h *OIDCHandler) StartTrial(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	owner, ok := middleware.PrincipalFromContext(c)
	if !ok || owner.AuthMethod != middleware.AuthMethodJWT || owner.LoginMethod != "password" || !owner.CanManageCredentials {
		oidcError(c, identity.ErrDenied)
		return
	}
	if !h.reserve(c, "admin-trial") {
		return
	}
	m := h.previewManager(c)
	if m == nil || !h.browserRequest(c, m) {
		return
	}
	h.trialMu.Lock()
	h.pruneTrialsLocked()
	full := len(h.trials) >= 128
	h.trialMu.Unlock()
	if full {
		oidcError(c, identity.ErrRateLimited)
		return
	}
	start, err := m.Begin(c.Request.Context(), "test", "/admin/authentication?source="+url.QueryEscape(m.ID()), 0, 0)
	if err != nil {
		oidcError(c, err)
		return
	}
	authorization, _ := url.Parse(start.AuthorizationURL)
	h.trialMu.Lock()
	h.pruneTrialsLocked()
	if len(h.trials) >= 128 {
		h.trialMu.Unlock()
		oidcError(c, identity.ErrRateLimited)
		return
	}
	h.trials["state:"+authorization.Query().Get("state")] = oidcTrial{Manager: m, Owner: owner.ID, CredentialVersion: owner.CredentialVersion, ExpiresAt: time.Now().Add(5 * time.Minute)}
	h.trialMu.Unlock()
	h.cookie(c, m, "trial_flow_"+m.ID(), start.BrowserToken, 300)
	h.auditTrial(c, m.ID(), "identity.trial.start", true)
	c.JSON(http.StatusOK, gin.H{"authorization_url": start.AuthorizationURL})
}

func (h *OIDCHandler) pruneTrialsLocked() {
	for key, trial := range h.trials {
		if !time.Now().Before(trial.ExpiresAt) {
			delete(h.trials, key)
		}
	}
}

func (h *OIDCHandler) trialCallback(c *gin.Context, id string) bool {
	state := c.Query("state")
	h.trialMu.Lock()
	h.pruneTrialsLocked()
	trial, found := h.trials["state:"+state]
	h.trialMu.Unlock()
	if !found {
		return false
	}
	m := trial.Manager
	callback, _ := url.Parse(m.Config().RedirectURL)
	if id != m.ID() || callback.Path != c.Request.URL.Path {
		c.Redirect(http.StatusSeeOther, "/admin/auth/complete")
		return true
	}
	browser, _ := c.Cookie(h.cookieName(m, "trial_flow_"+m.ID()))
	key, err := m.Callback(c.Request.Context(), state, browser, c.Query("code"), c.Query("error"))
	if err == nil {
		h.trialMu.Lock()
		delete(h.trials, "state:"+state)
		trial.ExpiresAt = time.Now().Add(time.Minute)
		h.trials["complete:"+key] = trial
		h.trialMu.Unlock()
		h.cookie(c, m, "trial_flow_"+m.ID(), "", -1)
		h.cookie(c, m, "complete", "@trial."+key, 60)
	}
	c.Redirect(http.StatusSeeOther, "/admin/auth/complete")
	return true
}

func (h *OIDCHandler) completeTrial(c *gin.Context) bool {
	var key string
	for _, name := range []string{"__Host-depsilo_oidc_complete", "depsilo_oidc_complete"} {
		value, _ := c.Cookie(name)
		if strings.HasPrefix(value, "@trial.") {
			key = strings.TrimPrefix(value, "@trial.")
			break
		}
	}
	if key == "" {
		return false
	}
	h.trialMu.Lock()
	h.pruneTrialsLocked()
	trial, found := h.trials["complete:"+key]
	h.trialMu.Unlock()
	if !found {
		oidcError(c, identity.ErrExpired)
		return true
	}
	m := trial.Manager
	if !h.browserRequest(c, m) {
		return true
	}
	h.trialMu.Lock()
	_, found = h.trials["complete:"+key]
	delete(h.trials, "complete:"+key)
	h.trialMu.Unlock()
	if !found {
		oidcError(c, identity.ErrExpired)
		return true
	}
	h.cookie(c, m, "complete", "", -1)
	owner, ok := middleware.PrincipalFromContext(c)
	if !ok || owner.ID != trial.Owner || owner.CredentialVersion != trial.CredentialVersion || owner.LoginMethod != "password" || !owner.CanManageCredentials {
		oidcError(c, identity.ErrDenied)
		return true
	}
	result, err := m.Complete(c.Request.Context(), key)
	if err != nil {
		oidcError(c, err)
		return true
	}
	h.auditTrial(c, m.ID(), "identity.trial.complete", result.Trial.Outcome == "success")
	c.JSON(http.StatusOK, gin.H{"mode": "test", "provider": gin.H{"id": m.ID(), "name": sourceName(m), "protocol": "oidc"}, "outcome": result.Trial.Outcome, "authenticated": result.Trial.Authenticated, "mapped_role": result.Trial.MappedRole, "display_name": result.Trial.DisplayName, "code": result.Trial.Code, "checked_at": result.Trial.CheckedAt, "return_to": result.ReturnTo})
	return true
}

func (h *OIDCHandler) auditTrial(c *gin.Context, source, action string, success bool) {
	owner, ok := middleware.PrincipalFromContext(c)
	if ok && h.db != nil {
		_ = audit.RecordManagementEvent(c.Request.Context(), h.db, audit.ManagementEvent{ActorID: owner.ID, Action: action, Target: source, Success: success})
	}
}
