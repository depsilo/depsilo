package server

import (
	"errors"
	"fmt"
	"net/url"

	"gorm.io/gorm"

	"depsilo/internal/cascade"
	"depsilo/internal/config"
	"depsilo/internal/db"
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
		})
	}
	return upstream.NewPeerSet(entries), nil
}
