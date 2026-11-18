package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"depsilo/internal/api/contract"
	"depsilo/internal/cascade"
	"depsilo/internal/config"
)

// CascadeHandler exposes the deployment-level cascade topology to the Admin
// UI. Peer tokens are intentionally never part of this response.
type CascadeHandler struct {
	settings   config.CascadeConfig
	instanceID string
}

func NewCascadeHandler(settings config.CascadeConfig, instanceID string) *CascadeHandler {
	return &CascadeHandler{settings: settings, instanceID: instanceID}
}

func (h *CascadeHandler) Info(c *gin.Context) {
	response := contract.AdminCascadeInfo{
		Enabled:           h.settings.Enabled,
		InstanceID:        h.instanceID,
		RelayPath:         cascade.RelayPath,
		MaxHops:           h.settings.MaxHops,
		MaxTTLSeconds:     int64(h.settings.MaxTTL.Seconds()),
		AllowInsecureHTTP: h.settings.AllowInsecureHTTP,
		Peers:             make([]contract.AdminCascadePeer, 0, len(h.settings.Peers)),
	}
	for _, peer := range h.settings.Peers {
		response.Peers = append(response.Peers, contract.AdminCascadePeer{
			Name:               peer.Name,
			URL:                peer.URL,
			ForwardCredentials: peer.ForwardCredentials,
		})
	}
	c.JSON(http.StatusOK, response)
}
