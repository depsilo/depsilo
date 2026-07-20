package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/rules"
)

// RulesHandler handles CRUD operations for package rules.
type RulesHandler struct {
	store  *rules.Store
	engine *rules.Engine
}

// NewRulesHandler creates a new RulesHandler.
func NewRulesHandler(store *rules.Store, engine *rules.Engine) *RulesHandler {
	return &RulesHandler{store: store, engine: engine}
}

// List returns all package rules.
func (h *RulesHandler) List(c *gin.Context) {
	items, err := h.store.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// Create adds a new package rule.
func (h *RulesHandler) Create(c *gin.Context) {
	var input ruleInput
	if err := decodeRuleJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "request body must contain only valid rule fields"})
		return
	}
	if err := input.normalizeAndValidate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	createdBy := "system"
	if principal, ok := middleware.PrincipalFromContext(c); ok && strings.TrimSpace(principal.Username) != "" {
		createdBy = principal.Username
	}
	rule := db.PackageRule{
		Ecosystem:   input.Ecosystem,
		PackageName: input.PackageName,
		Version:     input.Version,
		Action:      input.Action,
		Reason:      input.Reason,
		CreatedBy:   createdBy,
	}
	if err := h.store.Create(&rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	h.engine.InvalidateCache()
	c.JSON(http.StatusCreated, rule)
}

// Update modifies an existing package rule.
func (h *RulesHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}
	var input ruleUpdateInput
	if err := decodeRuleJSON(c, &input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "request body must contain only editable rule fields"})
		return
	}
	updates, err := input.normalizeAndValidate()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	rule, err := h.store.Update(uint(id), updates)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "rule not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "failed to update rule"})
		}
		return
	}
	h.engine.InvalidateCache()
	c.JSON(http.StatusOK, rule)
}

// Delete removes a package rule.
func (h *RulesHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}
	if err := h.store.Delete(uint(id)); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "rule not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "failed to delete rule"})
		}
		return
	}
	h.engine.InvalidateCache()
	c.Status(http.StatusNoContent)
}

// Test evaluates a package against the current rules without blocking.
func (h *RulesHandler) Test(c *gin.Context) {
	var req ruleTestInput
	if err := decodeRuleJSON(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "request body must contain only valid rule test fields"})
		return
	}
	if err := req.normalizeAndValidate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	allowed, rule, err := h.engine.Check(c.Request.Context(), req.Ecosystem, req.Package, req.Version)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "ERROR", "message": err.Error()})
		return
	}
	resp := gin.H{"allowed": allowed, "matched_rule": nil}
	if rule != nil {
		resp["matched_rule"] = rule
		resp["reason"] = rule.Reason
	}
	c.JSON(http.StatusOK, resp)
}
