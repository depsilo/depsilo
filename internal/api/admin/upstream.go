package admin

import (
	"net/http"
	"strconv"

	"depsilo/internal/upstream"
	"github.com/gin-gonic/gin"
)

type UpstreamHandler struct {
	registry *upstream.Registry
}

func NewUpstreamHandler(registry *upstream.Registry) *UpstreamHandler {
	return &UpstreamHandler{registry: registry}
}

func (h *UpstreamHandler) List(c *gin.Context) {
	items := h.registry.List()
	response := make([]adminUpstreamResponse, 0, len(items))
	canViewCredentials := principalCanViewCredentials(c)
	for _, item := range items {
		response = append(response, mapAdminUpstream(item, canViewCredentials))
	}
	c.JSON(http.StatusOK, upstreamListResponse{Items: response, Total: len(response)})
}

func (h *UpstreamHandler) Create(c *gin.Context) {
	request, err := decodeUpstreamMutationRequest(c)
	if err != nil {
		writeBadUpstreamRequest(c, err)
		return
	}
	item, err := h.registry.Create(c.Request.Context(), request.toMutation())
	if err != nil {
		writeUpstreamError(c, err)
		return
	}
	c.JSON(http.StatusCreated, mapAdminUpstream(item, principalCanViewCredentials(c)))
}

func parseUpstreamID(c *gin.Context) (uint, bool) {
	parsed, err := strconv.ParseUint(c.Param("id"), 10, strconv.IntSize)
	if err != nil || parsed == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid upstream id"})
		return 0, false
	}
	return uint(parsed), true
}

func (h *UpstreamHandler) Update(c *gin.Context) {
	id, ok := parseUpstreamID(c)
	if !ok {
		return
	}
	request, err := decodeUpstreamMutationRequest(c)
	if err != nil {
		writeBadUpstreamRequest(c, err)
		return
	}
	item, err := h.registry.Update(c.Request.Context(), id, request.toMutation())
	if err != nil {
		writeUpstreamError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapAdminUpstream(item, principalCanViewCredentials(c)))
}

func (h *UpstreamHandler) Delete(c *gin.Context) {
	id, ok := parseUpstreamID(c)
	if !ok {
		return
	}
	item, err := h.registry.Delete(c.Request.Context(), id)
	if err != nil {
		writeUpstreamError(c, err)
		return
	}
	c.JSON(http.StatusOK, deleteUpstreamResponse{DeletedID: item.ID, AdapterType: item.AdapterType})
}

func (h *UpstreamHandler) Check(c *gin.Context) {
	id, ok := parseUpstreamID(c)
	if !ok {
		return
	}
	item, result, err := h.registry.Check(c.Request.Context(), id)
	if err != nil {
		writeUpstreamError(c, err)
		return
	}
	canViewCredentials := principalCanViewCredentials(c)
	var errorText *string
	if result.Err != nil {
		text := "upstream check failed"
		if canViewCredentials {
			text = result.Err.Error()
		}
		errorText = &text
	}
	c.JSON(http.StatusOK, checkUpstreamResponse{
		Upstream: mapAdminUpstream(item, canViewCredentials),
		Check: checkResultResponse{
			Healthy:   result.Healthy,
			LatencyMS: result.Latency.Milliseconds(),
			CheckedAt: result.CheckedAt,
			Error:     errorText,
		},
	})
}
