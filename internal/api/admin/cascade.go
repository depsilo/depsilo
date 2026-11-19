package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"depsilo/internal/api/contract"
	"depsilo/internal/cascade"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

// CascadeRuntime is the cascade state the server runs with. Admin changes it
// without a restart.
type CascadeRuntime interface {
	Settings() config.CascadeConfig
	InstanceID() string
}

// CascadeHandler exposes the deployment-level cascade topology to the Admin
// UI. Peer tokens are intentionally never part of this response.
type CascadeHandler struct {
	runtime  CascadeRuntime
	registry *upstream.Registry
}

func NewCascadeHandler(runtime CascadeRuntime, registry *upstream.Registry) *CascadeHandler {
	return &CascadeHandler{runtime: runtime, registry: registry}
}

func (h *CascadeHandler) Info(c *gin.Context) {
	settings := h.runtime.Settings()
	response := contract.AdminCascadeInfo{
		Enabled:           settings.Enabled,
		InstanceID:        h.runtime.InstanceID(),
		RelayPath:         cascade.RelayPath,
		MaxHops:           settings.MaxHops,
		MaxTTLSeconds:     int64(settings.MaxTTL.Seconds()),
		AllowInsecureHTTP: settings.AllowInsecureHTTP,
		DirectFallback:    settings.DirectFallback,
		Peers:             make([]contract.AdminCascadePeer, 0, len(settings.Peers)),
	}
	var peers *upstream.PeerSet
	if h.registry != nil {
		peers = h.registry.Peers()
	}
	for _, peer := range settings.Peers {
		item := contract.AdminCascadePeer{
			Name:               peer.Name,
			URL:                peer.URL,
			ForwardCredentials: peer.ForwardCredentials,
		}
		if status, ok := peers.Status(peer.Name); ok {
			snapshot := status.Snapshot()
			item.LastSuccessAt = contract.NullableTime(snapshot.LastSuccessAt)
			item.LastFallbackAt = contract.NullableTime(snapshot.LastFallbackAt)
			item.LastFailure = snapshot.LastFailure
			item.FallbackCount = snapshot.FallbackCount
		}
		response.Peers = append(response.Peers, item)
	}
	c.JSON(http.StatusOK, response)
}
