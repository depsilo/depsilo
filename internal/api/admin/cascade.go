package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

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

type cascadePeerResponse struct {
	Name               string `json:"name"`
	URL                string `json:"url"`
	ForwardCredentials bool   `json:"forward_credentials"`
}

type cascadeInfoResponse struct {
	Enabled       bool                  `json:"enabled"`
	InstanceID    string                `json:"instance_id"`
	MaxHops       int                   `json:"max_hops"`
	MaxTTLSeconds int64                 `json:"max_ttl_seconds"`
	Peers         []cascadePeerResponse `json:"peers"`
}

func (h *CascadeHandler) Info(c *gin.Context) {
	response := cascadeInfoResponse{
		Enabled:       h.settings.Enabled,
		InstanceID:    h.instanceID,
		MaxHops:       h.settings.MaxHops,
		MaxTTLSeconds: int64(h.settings.MaxTTL.Seconds()),
		Peers:         make([]cascadePeerResponse, 0, len(h.settings.Peers)),
	}
	for _, peer := range h.settings.Peers {
		response.Peers = append(response.Peers, cascadePeerResponse{
			Name:               peer.Name,
			URL:                peer.URL,
			ForwardCredentials: peer.ForwardCredentials,
		})
	}
	c.JSON(http.StatusOK, response)
}
