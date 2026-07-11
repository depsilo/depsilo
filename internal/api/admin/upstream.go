package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

type UpstreamHandler struct {
	registry upstreamRegistry
	initErr  error
	legacyDB *gorm.DB
}

type upstreamRegistry interface {
	List() []upstream.RuntimeUpstream
	Create(context.Context, upstream.MutationInput) (upstream.RuntimeUpstream, error)
	Update(context.Context, uint, upstream.MutationInput) (upstream.RuntimeUpstream, error)
	Delete(context.Context, uint) (upstream.RuntimeUpstream, error)
	Check(context.Context, uint) (upstream.RuntimeUpstream, upstream.ProbeResult, error)
}

// NewUpstreamHandler accepts a database temporarily for callers that have not
// yet adopted the runtime registry dependency. Mutations always go through a
// registry, so HTTP success reflects published runtime state.
func NewUpstreamHandler(source any) *UpstreamHandler {
	switch value := source.(type) {
	case upstreamRegistry:
		return &UpstreamHandler{registry: value}
	case *gorm.DB:
		registry, err := registryFromDatabase(value)
		return &UpstreamHandler{registry: registry, initErr: err, legacyDB: value}
	default:
		return &UpstreamHandler{initErr: errors.New("unsupported upstream handler dependency")}
	}
}

func registryFromDatabase(database *gorm.DB) (*upstream.Registry, error) {
	var ecosystems []string
	if err := database.Model(&db.UpstreamRecord{}).Distinct("adapter_type").Order("adapter_type").Pluck("adapter_type", &ecosystems).Error; err != nil {
		return nil, err
	}
	return upstream.NewRegistry(database, ecosystems)
}

func (h *UpstreamHandler) ready(c *gin.Context) bool {
	if h.initErr == nil && h.registry != nil {
		return true
	}
	err := h.initErr
	if err == nil {
		err = errors.New("upstream registry unavailable")
	}
	writeUpstreamError(c, err)
	return false
}

func (h *UpstreamHandler) List(c *gin.Context) {
	if h.legacyDB != nil {
		h.listLegacy(c)
		return
	}
	if !h.ready(c) {
		return
	}
	items := h.registry.List()
	response := make([]adminUpstreamResponse, 0, len(items))
	canViewCredentials := principalCanViewCredentials(c)
	for _, item := range items {
		response = append(response, mapAdminUpstream(item, canViewCredentials))
	}
	c.JSON(http.StatusOK, upstreamListResponse{Items: response, Total: len(response)})
}

func (h *UpstreamHandler) listLegacy(c *gin.Context) {
	var records []db.UpstreamRecord
	result := h.legacyDB.Order("adapter_type, priority").Find(&records)
	if result.Error != nil {
		writeUpstreamError(c, result.Error)
		return
	}
	if !principalCanViewCredentials(c) {
		for i := range records {
			records[i].URL = maskURLUserInfo(records[i].URL)
			records[i].Proxy = maskURLUserInfo(records[i].Proxy)
		}
	}
	c.JSON(http.StatusOK, records)
}

func (h *UpstreamHandler) Create(c *gin.Context) {
	if !h.ready(c) {
		return
	}
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
	if !h.ready(c) {
		return
	}
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
	if !h.ready(c) {
		return
	}
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
	if !h.ready(c) {
		return
	}
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
