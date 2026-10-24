package api

import (
	"errors"
	"net/http"
	"strings"

	"depsilo/internal/identity"
	"github.com/gin-gonic/gin"
)

func (h *OIDCHandler) choiceCookie(c *gin.Context) (*identity.Manager, string) {
	for _, name := range []string{"__Host-depsilo_oidc_choice", "depsilo_oidc_choice"} {
		value, _ := c.Cookie(name)
		id, key, found := strings.Cut(value, ".")
		manager := h.registry.Manager(id)
		if !found || manager == nil {
			continue
		}
		expected := h.cookieName(manager, "choice")
		if manager.Protocol() == "ldap" {
			expected = directoryCookieName(c, "choice")
		}
		if name == expected {
			return manager, key
		}
	}
	return nil, ""
}

func (h *OIDCHandler) choiceResponse(c *gin.Context, manager *identity.Manager, choice identity.ChoiceInfo) {
	c.JSON(http.StatusOK, gin.H{"mode": "choice", "provider": gin.H{"id": manager.ID(), "name": sourceName(manager), "protocol": manager.Protocol()}, "display_name": choice.DisplayName, "role": choice.Role, "return_to": choice.ReturnTo, "expires_at": choice.ExpiresAt.Unix()})
}

func (h *OIDCHandler) resumeChoice(c *gin.Context) {
	manager, key := h.choiceCookie(c)
	if !manager.Enabled() {
		oidcError(c, identity.ErrExpired)
		return
	}
	choice, err := manager.Choice(key)
	if err != nil {
		oidcError(c, err)
		return
	}
	h.choiceResponse(c, manager, choice)
}

func (h *OIDCHandler) clearChoice(c *gin.Context) {
	manager, key := h.choiceCookie(c)
	if key == "" {
		return
	}
	manager.CancelChoice(key)
	h.cookie(c, manager, "choice", "", -1)
}

func (h *OIDCHandler) Cancel(c *gin.Context) {
	if !h.browserRequest(c, h.browserManager()) {
		return
	}
	h.clearChoice(c)
	c.Status(http.StatusNoContent)
}

func (h *OIDCHandler) Resolve(c *gin.Context) {
	if !h.browserRequest(c, h.browserManager()) {
		return
	}
	var req struct {
		Action   string `json:"action"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !h.bind(c, &req) {
		return
	}
	if req.Action != "create" && req.Action != "link" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid account choice"})
		return
	}
	limit := "choice"
	if req.Action == "link" {
		limit = "proof:username:" + strings.ToLower(strings.TrimSpace(req.Username))
	}
	if !h.reserve(c, limit) {
		return
	}
	manager, key := h.choiceCookie(c)
	if !manager.Enabled() {
		oidcError(c, identity.ErrExpired)
		return
	}
	result, err := manager.ResolveChoice(c.Request.Context(), key, req.Action, req.Username, req.Password)
	if !errors.Is(err, identity.ErrProof) {
		h.cookie(c, manager, "choice", "", -1)
	}
	if err != nil {
		oidcError(c, err)
		return
	}
	h.respondIdentityResult(c, manager, result)
}
