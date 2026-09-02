package admin

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/packagepolicy"
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
		if errors.Is(err, packagepolicy.ErrInvalidRule) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
			return
		}
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
		if errors.Is(err, packagepolicy.ErrInvalidRule) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
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
	if h == nil || h.engine == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":    "PACKAGE_POLICY_UNAVAILABLE",
			"message": "package policy engine is unavailable",
		})
		return
	}
	evaluation, err := h.engine.Explain(c.Request.Context(), req.Ecosystem, req.Package, req.Version)
	if err != nil {
		status := http.StatusInternalServerError
		code := "ERROR"
		if errors.Is(err, rules.ErrPolicyIntegrity) || errors.Is(err, rules.ErrPolicyEvaluation) {
			status = http.StatusServiceUnavailable
			code = "PACKAGE_POLICY_UNEVALUABLE"
		} else if errors.Is(err, rules.ErrRuleStoreUnavailable) {
			status = http.StatusServiceUnavailable
			code = "PACKAGE_POLICY_UNAVAILABLE"
		}
		c.JSON(status, gin.H{"code": code, "message": err.Error()})
		return
	}
	// Keep the original compact response fields for existing Admin clients,
	// then add the explainable decision surface. Candidates are already sorted
	// by the Engine's explicit precedence tuple and carry their own match
	// levels, so this handler never reimplements policy semantics.
	resp := gin.H{
		"allowed":           evaluation.Allowed,
		"matched_rule":      evaluation.MatchedRule,
		"winning_rule":      evaluation.WinningRule,
		"winner":            evaluation.Winner,
		"reason":            evaluation.Reason,
		"winner_reason":     evaluation.WinnerReason,
		"precedence_reason": evaluation.PrecedenceReason,
		"candidates":        evaluation.Candidates,
	}
	if evaluation.PolicyStatus != nil {
		resp["policy_status"] = policyStatusPayload(*evaluation.PolicyStatus)
	}
	c.JSON(http.StatusOK, resp)
}

// policyStatusPayload keeps the Admin response stable and avoids exposing Go
// time.Time's zero value or the rules package's internal field names. The
// snapshot status is additive metadata: it does not alter the decision that
// was already made by Engine.Explain.
func policyStatusPayload(status rules.PolicyStatus) gin.H {
	loaded := status.LastSuccessfulRefresh
	if loaded.IsZero() {
		loaded = status.SnapshotLoadedAt
	}
	state := status.Status
	if state == "" {
		if status.Degraded {
			state = "degraded"
		} else if loaded.IsZero() {
			state = "unavailable"
		} else {
			state = "healthy"
		}
	}
	age := status.SnapshotAgeSeconds
	if !loaded.IsZero() {
		age = time.Since(loaded).Seconds()
		if age < 0 || math.IsNaN(age) || math.IsInf(age, 0) {
			age = 0
		}
	} else {
		age = 0
	}
	var loadedAt any
	if !loaded.IsZero() {
		loadedAt = loaded.UTC().Format(time.RFC3339Nano)
	}
	mode := status.OnLoadError
	if mode == "" {
		mode = rules.DefaultOnLoadErrorPolicy
	}
	return gin.H{
		"status":                  state,
		"degraded":                status.Degraded,
		"using_stale_snapshot":    status.UsingStaleSnapshot,
		"last_successful_refresh": loadedAt,
		"snapshot_loaded_at":      loadedAt,
		"snapshot_age_seconds":    age,
		"refresh_failures":        status.RefreshFailures,
		"on_load_error":           mode,
	}
}
