package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/api/contract"
	"depsilo/internal/upstreamupdates"
)

// UpstreamUpdateHandler adapts the upstream update history module to the
// Operator HTTP API.
type UpstreamUpdateHandler struct {
	history *upstreamupdates.History
	initErr error
}

// NewUpstreamUpdateHandler constructs the read-only Operator history handler.
// The database argument is retained to keep router wiring backward compatible;
// all persistence behavior lives behind History.
func NewUpstreamUpdateHandler(database *gorm.DB) *UpstreamUpdateHandler {
	history, err := upstreamupdates.NewHistory(database)
	return &UpstreamUpdateHandler{history: history, initErr: err}
}

func (h *UpstreamUpdateHandler) List(c *gin.Context) {
	if h.initErr != nil || h.history == nil {
		writeUpstreamUpdateInternalError(c)
		return
	}

	ecosystems, err := parseEcosystemFilter(c, "ecosystem")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	window, err := parseTimeWindow(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	query := upstreamupdates.HistoryQuery{
		Limit:      parseIntParam(c, "limit", 50, 1, 500),
		Cursor:     strings.TrimSpace(c.Query("cursor")),
		Ecosystems: ecosystems,
		Upstream:   strings.TrimSpace(c.Query("upstream")),
		Package:    strings.TrimSpace(c.Query("package")),
		Result:     strings.TrimSpace(c.Query("result")),
		Since:      window.Start,
		Until:      window.End,
	}
	if _, present := c.GetQuery("offset"); present {
		offset := parseIntParam(c, "offset", 0, 0, 100000)
		query.LegacyOffset = &offset
	}

	page, err := h.history.List(c.Request.Context(), query)
	if err != nil {
		if errors.Is(err, upstreamupdates.ErrInvalidHistoryQuery) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    "BAD_REQUEST",
				"message": "invalid upstream update query",
			})
			return
		}
		writeUpstreamUpdateInternalError(c)
		return
	}

	counts, err := h.history.EcosystemCounts(c.Request.Context(), query)
	if err != nil {
		writeUpstreamUpdateInternalError(c)
		return
	}

	var nextCursor *string
	if page.NextCursor != "" {
		nextCursor = &page.NextCursor
	}
	// The facet is always a list, never null, even when nothing matches.
	ecosystemCounts := make([]contract.EcosystemCount, len(counts))
	for i, count := range counts {
		ecosystemCounts[i] = contract.EcosystemCount{ID: count.ID, Count: count.Count}
	}
	c.JSON(http.StatusOK, contract.AdminUpstreamUpdateListResponse{
		Items: cacheContractSlice(page.Items, func(event upstreamupdates.HistoryEvent) contract.AdminUpstreamUpdateEvent {
			return contract.AdminUpstreamUpdateEvent{
				ID: event.ID, CacheEntryID: event.CacheEntryID, Ecosystem: event.Ecosystem, Upstream: event.Upstream,
				Package: event.Package, Result: contract.UpstreamUpdateResult(event.Result), Detail: event.Detail,
				LatencyMs: event.LatencyMs, OccurrenceCount: event.OccurrenceCount, FirstSeenAt: event.FirstSeenAt,
				LastSeenAt: event.LastSeenAt, CreatedAt: event.CreatedAt,
			}
		}),
		Total:      page.Total,
		NextCursor: nextCursor,
		Ecosystems: ecosystemCounts,
	})
}

func writeUpstreamUpdateInternalError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{
		"code":    "INTERNAL",
		"message": "failed to list upstream update history",
	})
}
