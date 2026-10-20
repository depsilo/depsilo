package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

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
	c.JSON(http.StatusOK, state)
}

func (h *DockerBootstrapHandler) Enable(c *gin.Context) {
	state, err := h.store.EnableDockerHub(c.Request.Context())
	if err != nil {
		writeSettingsError(c, err)
		return
	}
	c.JSON(http.StatusOK, state)
}
