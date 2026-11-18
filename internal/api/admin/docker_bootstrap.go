package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"depsilo/internal/api/contract"
	"depsilo/internal/config"
)

type DockerBootstrapHandler struct {
	store *config.Store
}

func NewDockerBootstrapHandler(store *config.Store) *DockerBootstrapHandler {
	return &DockerBootstrapHandler{store: store}
}

func (h *DockerBootstrapHandler) Get(c *gin.Context) {
	state, err := h.store.DockerBootstrap(c.Request.Context())
	if err != nil {
		writeSettingsError(c, err)
		return
	}
	c.JSON(http.StatusOK, toDockerBootstrapState(state))
}

func (h *DockerBootstrapHandler) Enable(c *gin.Context) {
	state, err := h.store.EnableDockerHub(c.Request.Context())
	if err != nil {
		writeSettingsError(c, err)
		return
	}
	c.JSON(http.StatusOK, toDockerBootstrapState(state))
}

func toDockerBootstrapState(state config.DockerBootstrapState) contract.DockerBootstrapState {
	return contract.DockerBootstrapState{
		Configured: state.Configured, Effective: state.Effective, DockerHub: state.DockerHub,
		PendingRestart: state.PendingRestart, ConfigWritable: state.ConfigWritable,
		BlockedByOverride: state.BlockedByOverride,
	}
}
