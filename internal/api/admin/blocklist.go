package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/asyncruntime"
	"depsilo/internal/blocklist"
)

// BlocklistHandler serves the known-malicious blocklist admin
// endpoints: sync status, manual sync trigger, and override CRUD.
// Open-source per docs/DIRECTION.md Task 2 (NOT Pro-gated); blocked /
// bypassed request events flow through the quarantine events endpoint
// (action = malware_blocked / malware_bypassed).
type BlocklistHandler struct {
	store  *blocklist.Store
	syncer *blocklist.Syncer
	tasks  asyncruntime.Submitter
}

// NewBlocklistHandler binds manual syncs to the server's async runtime.
func NewBlocklistHandler(tasks asyncruntime.Submitter, store *blocklist.Store, syncer *blocklist.Syncer) *BlocklistHandler {
	return &BlocklistHandler{
		store:  store,
		syncer: syncer,
		tasks:  tasks,
	}
}

// disabled guards the read endpoints: when [supply_chain.blocklist]
// enabled = false the server wires a nil store, and status answers
// with an explicit state instead of a panic or a misleading 500.
func (h *BlocklistHandler) disabled(c *gin.Context) bool {
	if h.store == nil {
		c.JSON(http.StatusOK, gin.H{"enabled": false})
		return true
	}
	return false
}

// disabledMutation guards the write endpoints — a mutation against a
// disabled subsystem is a client error, not a 200 no-op (v0.8.0
// review finding).
func (h *BlocklistHandler) disabledMutation(c *gin.Context) bool {
	if h.store == nil {
		c.JSON(http.StatusConflict, gin.H{"code": "BLOCKLIST_DISABLED", "message": "the malicious blocklist is disabled in config ([supply_chain.blocklist])"})
		return true
	}
	return false
}

// ── GET /admin/blocklist/status ───────────────────────────────────

func (h *BlocklistHandler) Status(c *gin.Context) {
	if h.disabled(c) {
		return
	}
	ctx := c.Request.Context()
	st, err := h.store.SyncState(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	perEco, total, err := h.store.EntryCounts(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	resp := gin.H{
		"enabled":         true,
		"last_sync_at":    st.LastSyncAt,
		"last_success_at": st.LastSuccessAt,
		"last_error":      st.LastError,
		"duration_ms":     st.DurationMs,
		"entry_count":     total,
		"per_ecosystem":   perEco,
		"ecosystems":      blocklist.SyncedEcosystems(),
	}
	if h.syncer != nil {
		resp["running"] = h.syncer.Running()
		resp["sync_interval_seconds"] = int64(h.syncer.Interval().Seconds())
		if st.LastSyncAt != nil {
			resp["next_sync_at"] = st.LastSyncAt.Add(h.syncer.Interval())
		}
	}
	c.JSON(http.StatusOK, resp)
}

// ── POST /admin/blocklist/sync ────────────────────────────────────
//
// Kicks a server-owned task and returns immediately — a full
// refresh downloads several archives and can take minutes; the UI
// polls /status for the outcome. It outlives the triggering HTTP request but
// is cancelled and joined during server shutdown.

func (h *BlocklistHandler) TriggerSync(c *gin.Context) {
	if h.disabledMutation(c) {
		return
	}
	if h.syncer == nil {
		c.JSON(http.StatusConflict, gin.H{"code": "NO_SYNCER", "message": "blocklist syncer not running"})
		return
	}
	err := h.syncer.TryStartSync(h.tasks)
	switch {
	case err == nil:
	case errors.Is(err, blocklist.ErrSyncInProgress):
		c.JSON(http.StatusConflict, gin.H{"code": "SYNC_RUNNING", "message": "a sync is already in progress"})
		return
	case errors.Is(err, blocklist.ErrSyncUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SERVER_SHUTTING_DOWN", "message": "blocklist sync is unavailable"})
		return
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	zap.L().Info("blocklist: manual sync triggered", zap.Uint("actor", userIDFromContext(c)))
	c.JSON(http.StatusAccepted, gin.H{"status": "sync started"})
}

// ── GET /admin/blocklist/overrides ────────────────────────────────

func (h *BlocklistHandler) ListOverrides(c *gin.Context) {
	if h.disabled(c) {
		return
	}
	items, err := h.store.ListOverrides(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "now": time.Now().UTC()})
}

// ── POST /admin/blocklist/overrides ───────────────────────────────

type createOverrideReq struct {
	Ecosystem string `json:"ecosystem" binding:"required"`
	Package   string `json:"package" binding:"required"`
	Version   string `json:"version"` // empty = every version
	Reason    string `json:"reason" binding:"required"`
}

func (h *BlocklistHandler) CreateOverride(c *gin.Context) {
	if h.disabledMutation(c) {
		return
	}
	var req createOverrideReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	if len(strings.TrimSpace(req.Reason)) < 3 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "reason must be at least 3 characters — overrides are audited decisions"})
		return
	}
	// Canonicalize + validate the ecosystem: an override stored under
	// an OSV spelling ("PyPI", "crates.io") would return 201 but never
	// match a request — a silently dead exemption during an incident
	// is the worst failure mode (v0.8.0 review finding).
	eco := blocklist.CanonicalEcosystem(strings.ToLower(strings.TrimSpace(req.Ecosystem)))
	if !blocklist.IsSyncedEcosystem(eco) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "BAD_ECOSYSTEM",
			"message": fmt.Sprintf("unknown ecosystem %q — the blocklist covers: %s", req.Ecosystem, strings.Join(blocklist.SyncedEcosystems(), ", ")),
		})
		return
	}
	ov, err := h.store.CreateOverride(
		c.Request.Context(),
		eco,
		strings.TrimSpace(req.Package),
		strings.TrimSpace(req.Version),
		strings.TrimSpace(req.Reason),
		userIDFromContext(c),
	)
	if err != nil {
		zap.L().Error("blocklist: create override",
			zap.String("ecosystem", eco),
			zap.String("package", req.Package),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	// Tell the operator whether the exemption actually corresponds to
	// a blocklist entry — a typo'd package name would otherwise show a
	// live countdown while the 451 keeps firing.
	matches, mErr := h.store.HasEntries(c.Request.Context(), eco, req.Package)
	if mErr != nil {
		zap.L().Warn("blocklist: HasEntries", zap.Error(mErr))
	}
	c.JSON(http.StatusCreated, gin.H{
		"id": ov.ID, "ecosystem": ov.Ecosystem, "package": ov.Package,
		"version": ov.Version, "reason": ov.Reason, "actor_id": ov.ActorID,
		"created_at": ov.CreatedAt, "expires_at": ov.ExpiresAt,
		"matches_blocklist": matches,
	})
}

// ── DELETE /admin/blocklist/overrides/:id ─────────────────────────

type revokeOverrideReq struct {
	Reason string `json:"reason" binding:"required"`
}

func (h *BlocklistHandler) RevokeOverride(c *gin.Context) {
	if h.disabledMutation(c) {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid override id"})
		return
	}
	var req revokeOverrideReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	if len(strings.TrimSpace(req.Reason)) < 3 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "reason must be at least 3 characters"})
		return
	}
	if err := h.store.RevokeOverride(c.Request.Context(), uint(id), strings.TrimSpace(req.Reason), userIDFromContext(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": id})
}
