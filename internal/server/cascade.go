package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/cascade"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/relay"
	"depsilo/internal/upstream"
)

const cascadeInstanceStateKey = "cascade.instance_id"

// resolveCascadeIdentity returns this node's stable cascade identity. An
// operator-supplied value always wins; otherwise the first startup generates
// one and persists it alongside the other control-plane state so loop
// detection keeps working across restarts.
func resolveCascadeIdentity(database *gorm.DB, configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	var state db.ControlPlaneState
	err := database.Where("key = ?", cascadeInstanceStateKey).First(&state).Error
	if err == nil {
		if cascade.ValidInstanceID(state.Value) {
			return state.Value, nil
		}
		return "", fmt.Errorf("persisted cascade instance ID %q is invalid", state.Value)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", fmt.Errorf("read cascade instance ID: %w", err)
	}
	generated, err := cascade.NewInstanceID()
	if err != nil {
		return "", err
	}
	if err := database.Create(&db.ControlPlaneState{Key: cascadeInstanceStateKey, Value: generated}).Error; err != nil {
		return "", fmt.Errorf("persist cascade instance ID: %w", err)
	}
	return generated, nil
}

// cascadePeerSet resolves configured peers into the immutable lookup used by
// pool construction and Admin mutation validation.
func cascadePeerSet(settings config.CascadeConfig, identity string) (*upstream.PeerSet, error) {
	entries := make([]upstream.PeerEgress, 0, len(settings.Peers))
	for _, peer := range settings.Peers {
		parsed, err := url.Parse(peer.URL)
		if err != nil {
			return nil, fmt.Errorf("parse cascade peer %s URL: %w", peer.Name, err)
		}
		token := peer.Token
		if token == "" {
			// An empty peer token inherits the shared secret. Validation never
			// materializes it, so the config file stays authoritative about
			// which peers carry explicit overrides.
			token = settings.Token
		}
		entries = append(entries, upstream.PeerEgress{
			Name:               peer.Name,
			URL:                parsed,
			Token:              token,
			LocalInstanceID:    identity,
			MaxHops:            settings.MaxHops,
			ForwardCredentials: peer.ForwardCredentials,
			DirectFallback:     settings.DirectFallback,
		})
	}
	return upstream.NewPeerSet(entries), nil
}

// cascadeRuntime holds the cascade settings the server runs with and applies
// a new section without a restart: it swaps the relay endpoint's handler and
// the parents bound upstreams egress through.
type cascadeRuntime struct {
	identity string
	registry *upstream.Registry
	newRelay func(config.CascadeConfig) *relay.Handler

	mu       sync.RWMutex
	settings config.CascadeConfig
	relay    *relay.Handler
}

func newCascadeRuntime(identity string, registry *upstream.Registry, settings config.CascadeConfig, newRelay func(config.CascadeConfig) *relay.Handler) *cascadeRuntime {
	runtime := &cascadeRuntime{identity: identity, registry: registry, newRelay: newRelay, settings: settings}
	if settings.Enabled {
		runtime.relay = newRelay(settings)
	}
	return runtime
}

// Apply puts settings into effect.
func (r *cascadeRuntime) Apply(settings config.CascadeConfig) error {
	var peers *upstream.PeerSet
	if settings.Enabled {
		resolved, err := cascadePeerSet(settings, r.identity)
		if err != nil {
			return err
		}
		peers = resolved
	}
	if err := r.registry.SetPeers(peers); err != nil {
		return err
	}
	var handler *relay.Handler
	if settings.Enabled {
		handler = r.newRelay(settings)
	}
	r.mu.Lock()
	r.settings, r.relay = settings, handler
	r.mu.Unlock()
	zap.L().Info("cascade settings applied",
		zap.Bool("relay_enabled", settings.Enabled),
		zap.Int("peers", len(settings.Peers)),
	)
	return nil
}

// Settings returns the cascade settings in effect.
func (r *cascadeRuntime) Settings() config.CascadeConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	settings := r.settings
	settings.Peers = append([]config.CascadePeerConfig(nil), r.settings.Peers...)
	return settings
}

// InstanceID returns this node's cascade identity.
func (r *cascadeRuntime) InstanceID() string { return r.identity }

// ServeHTTP serves the relay endpoint while cascade is enabled and answers
// 404 otherwise, as the route did before it existed at all.
func (r *cascadeRuntime) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	r.mu.RLock()
	handler := r.relay
	r.mu.RUnlock()
	if handler == nil {
		http.NotFound(writer, request)
		return
	}
	handler.ServeHTTP(writer, request)
}
