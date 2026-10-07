package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

const maxCascadeConfigBodyBytes int64 = 64 << 10

// cascadeConfigStore is the config-writer seam. The real implementation is
// *config.Store, which serializes this write with ordinary settings updates.
type cascadeConfigStore interface {
	CascadeConfig(ctx context.Context) (config.CascadeConfigState, error)
	UpdateCascadeConfig(ctx context.Context, patch config.CascadeConfigPatch) (config.CascadeConfigUpdate, error)
}

// CascadeConfigHandler exposes and updates the cascade section of config.toml.
// Tokens are write-only: responses report TokenSet booleans and never echo a
// secret, while a nil patch field leaves the stored value untouched.
type CascadeConfigHandler struct {
	store    cascadeConfigStore
	registry *upstream.Registry
}

func NewCascadeConfigHandler(store cascadeConfigStore, registry *upstream.Registry) *CascadeConfigHandler {
	return &CascadeConfigHandler{store: store, registry: registry}
}

func (h *CascadeConfigHandler) Get(c *gin.Context) {
	state, err := h.store.CascadeConfig(c.Request.Context())
	if err != nil {
		writeSettingsError(c, err)
		return
	}
	c.JSON(http.StatusOK, state)
}

type cascadePeerRequest struct {
	Name               string  `json:"name"`
	URL                string  `json:"url"`
	Token              *string `json:"token"`
	ForwardCredentials bool    `json:"forward_credentials"`
}

type cascadeConfigRequest struct {
	Enabled           *bool                 `json:"enabled"`
	MaxHops           *int                  `json:"max_hops"`
	MaxTTL            *string               `json:"max_ttl"`
	AllowInsecureHTTP *bool                 `json:"allow_insecure_http"`
	SharedToken       *string               `json:"shared_token"`
	Peers             *[]cascadePeerRequest `json:"peers"`
}

type cascadeConfigUpdateResponse struct {
	State           config.CascadeConfigState `json:"state"`
	Changed         []string                  `json:"changed"`
	RestartRequired bool                      `json:"restart_required"`
}

func (h *CascadeConfigHandler) Update(c *gin.Context) {
	request, err := decodeCascadeConfigRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	patch := request.toPatch()
	if patch.Peers != nil {
		if err := h.validateDatabaseBindings(*patch.Peers); err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"code":    string(config.StoreInvalidSetting),
				"message": err.Error(),
			})
			return
		}
	}
	result, err := h.store.UpdateCascadeConfig(c.Request.Context(), patch)
	if err != nil {
		writeSettingsError(c, err)
		return
	}
	c.JSON(http.StatusOK, cascadeConfigUpdateResponse{
		State:           result.State,
		Changed:         result.Changed,
		RestartRequired: result.State.PendingRestart,
	})
}

func (request cascadeConfigRequest) toPatch() config.CascadeConfigPatch {
	patch := config.CascadeConfigPatch{
		Enabled:           request.Enabled,
		MaxHops:           request.MaxHops,
		MaxTTL:            request.MaxTTL,
		AllowInsecureHTTP: request.AllowInsecureHTTP,
		SharedToken:       request.SharedToken,
	}
	if request.Peers == nil {
		return patch
	}
	peers := make([]config.CascadePeerInput, 0, len(*request.Peers))
	for _, peer := range *request.Peers {
		peers = append(peers, config.CascadePeerInput{
			Name:               peer.Name,
			URL:                peer.URL,
			Token:              peer.Token,
			ForwardCredentials: peer.ForwardCredentials,
		})
	}
	patch.Peers = &peers
	return patch
}

// validateDatabaseBindings keeps a peer removal from silently downgrading
// database-managed upstreams to direct egress. Conf-file upstreams are covered
// by the config validator itself.
func (h *CascadeConfigHandler) validateDatabaseBindings(requested []config.CascadePeerInput) error {
	if h.registry == nil {
		return nil
	}
	allowed := make(map[string]struct{}, len(requested))
	for _, peer := range requested {
		allowed[strings.TrimSpace(peer.Name)] = struct{}{}
	}
	blocked := make([]string, 0)
	for _, item := range h.registry.List() {
		if item.Via == "" {
			continue
		}
		if _, ok := allowed[item.Via]; ok {
			continue
		}
		blocked = append(blocked, fmt.Sprintf("%s/%s -> %s", item.AdapterType, item.Name, item.Via))
	}
	if len(blocked) == 0 {
		return nil
	}
	sort.Strings(blocked)
	return fmt.Errorf(
		"cascade peers still used by upstreams cannot be removed: %s; rebind those upstreams first",
		strings.Join(blocked, ", "),
	)
}

func decodeCascadeConfigRequest(c *gin.Context) (cascadeConfigRequest, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCascadeConfigBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var request cascadeConfigRequest
	if err := decoder.Decode(&request); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return cascadeConfigRequest{}, errors.New("request body too large")
		}
		return cascadeConfigRequest{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return cascadeConfigRequest{}, errors.New("request body must contain one JSON object")
		}
		return cascadeConfigRequest{}, err
	}
	return request, nil
}
