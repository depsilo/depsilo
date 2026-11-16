package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

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
	if counts == nil {
		counts = []upstreamupdates.EcosystemCount{}
	}

	var nextCursor *string
	if page.NextCursor != "" {
		nextCursor = &page.NextCursor
	}
	c.JSON(http.StatusOK, struct {
		Items      []upstreamupdates.HistoryEvent   `json:"items"`
		Total      int64                            `json:"total"`
		NextCursor *string                          `json:"next_cursor"`
		Ecosystems []upstreamupdates.EcosystemCount `json:"ecosystems"`
	}{
		Items:      page.Items,
		Total:      page.Total,
		NextCursor: nextCursor,
		Ecosystems: counts,
	})
}

func writeUpstreamUpdateInternalError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{
		"code":    "INTERNAL",
		"message": "failed to list upstream update history",
	})
}
