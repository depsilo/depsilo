package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"time"

	"go.uber.org/zap"
)

// CascadePeerState is the masked, read-only view of one configured peer.
// Token values never cross this boundary; TokenSet only reports whether an
// explicit override exists (an empty override means "inherit cascade.token").
type CascadePeerState struct {
	Name               string `json:"name"`
	URL                string `json:"url"`
	TokenSet           bool   `json:"token_set"`
	ForwardCredentials bool   `json:"forward_credentials"`
}

// CascadeConfigState is the editable cascade configuration read from
// config.toml, plus the two facts the UI needs before offering an edit:
// whether the file can be written and whether the running process still uses
// an older version of these values.
type CascadeConfigState struct {
	Enabled           bool               `json:"enabled"`
	MaxHops           int                `json:"max_hops"`
	MaxTTL            string             `json:"max_ttl"`
	AllowInsecureHTTP bool               `json:"allow_insecure_http"`
	TokenSet          bool               `json:"token_set"`
	Peers             []CascadePeerState `json:"peers"`
	ConfigWritable    bool               `json:"config_writable"`
	PendingRestart    bool               `json:"pending_restart"`
}

// CascadePeerInput replaces one peer entry. A nil Token keeps the existing
// override for the same peer name; a non-nil empty Token removes the override
// so the peer inherits the shared secret again.
type CascadePeerInput struct {
	Name               string
	URL                string
	Token              *string
	ForwardCredentials bool
}

// CascadeConfigPatch updates only the pointers that are set. Nil Peers keeps
// the configured list untouched; a non-nil empty slice removes every peer.
type CascadeConfigPatch struct {
	Enabled           *bool
	MaxHops           *int
	MaxTTL            *string
	AllowInsecureHTTP *bool
	SharedToken       *string
	Peers             *[]CascadePeerInput
}

func (p CascadeConfigPatch) empty() bool {
	return p.Enabled == nil && p.MaxHops == nil && p.MaxTTL == nil &&
		p.AllowInsecureHTTP == nil && p.SharedToken == nil && p.Peers == nil
}

// CascadeConfigUpdate reports the masked state after a successful write and
// which fields changed. Every cascade field is read at startup, so a changed
// update always requires a restart.
type CascadeConfigUpdate struct {
	State   CascadeConfigState `json:"state"`
	Changed []string           `json:"changed"`
}

// cascadeFingerprint compares the configured file against the running process
// without keeping secret values in memory outside the config struct.
type cascadeFingerprint struct {
	Enabled           bool
	MaxHops           int
	MaxTTL            time.Duration
	AllowInsecureHTTP bool
	TokenHash         string
	Peers             []cascadePeerFingerprint
}

type cascadePeerFingerprint struct {
	Name               string
	URL                string
	TokenHash          string
	ForwardCredentials bool
}

func cascadeTokenHash(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

func fingerprintCascade(settings CascadeConfig) cascadeFingerprint {
	if settings.MaxHops == 0 {
		settings.MaxHops = defaultCascadeMaxHops
	}
	if settings.MaxTTL == 0 {
		settings.MaxTTL = defaultCascadeMaxTTL
	}
	fingerprint := cascadeFingerprint{
		Enabled:           settings.Enabled,
		MaxHops:           settings.MaxHops,
		MaxTTL:            settings.MaxTTL,
		AllowInsecureHTTP: settings.AllowInsecureHTTP,
		TokenHash:         cascadeTokenHash(settings.Token),
		Peers:             make([]cascadePeerFingerprint, 0, len(settings.Peers)),
	}
	for _, peer := range settings.Peers {
		fingerprint.Peers = append(fingerprint.Peers, cascadePeerFingerprint{
			Name:               peer.Name,
			URL:                peer.URL,
			TokenHash:          cascadeTokenHash(peer.Token),
			ForwardCredentials: peer.ForwardCredentials,
		})
	}
	return fingerprint
}

// CascadeConfig returns the masked cascade configuration currently stored in
// config.toml.
func (s *Store) CascadeConfig(ctx context.Context) (CascadeConfigState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return CascadeConfigState{}, err
	}
	cfg, _, _, err := s.readConfigDocument()
	if err != nil {
		return CascadeConfigState{}, &StoreError{Code: StoreConfigReadFailed, Err: err}
	}
	return s.cascadeState(cfg), nil
}

// UpdateCascadeConfig validates and atomically writes a cascade patch. The
// write is serialized with ordinary settings updates so two config writers can
// never race on the same document.
func (s *Store) UpdateCascadeConfig(ctx context.Context, patch CascadeConfigPatch) (CascadeConfigUpdate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return CascadeConfigUpdate{}, err
	}
	if patch.empty() {
		return CascadeConfigUpdate{}, &StoreError{
			Code: StoreInvalidSetting,
			Err:  errors.New("cascade configuration patch is empty"),
		}
	}

	cfg, document, mode, err := s.readConfigDocument()
	if err != nil {
		return CascadeConfigUpdate{}, &StoreError{Code: StoreConfigReadFailed, Err: err}
	}
	before := cfg.Cascade
	before.Peers = append([]CascadePeerConfig(nil), cfg.Cascade.Peers...)

	if err := applyCascadePatch(&cfg.Cascade, patch); err != nil {
		return CascadeConfigUpdate{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}

	// Validate a copy so normalization (hop/TTL defaults) never rewrites the
	// document beyond what the patch asked for.
	candidate := *cfg
	candidate.Cascade.Peers = append([]CascadePeerConfig(nil), cfg.Cascade.Peers...)
	if err := validateCascadeConfig(&candidate); err != nil {
		return CascadeConfigUpdate{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}

	changed := cascadeChangedFields(before, cfg.Cascade)
	if len(changed) == 0 {
		return CascadeConfigUpdate{State: s.cascadeState(cfg)}, nil
	}

	updatedDocument, err := patchCascadeDocument(document, cfg.Cascade, patch)
	if err != nil {
		return CascadeConfigUpdate{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	updatedConfig, err := decodeConfigDocument(updatedDocument)
	if err != nil {
		return CascadeConfigUpdate{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	if !configWritable(s.path) {
		return CascadeConfigUpdate{}, &StoreError{
			Code: StoreConfigReadOnly,
			Err:  fmt.Errorf("config file %q is read-only", s.path),
		}
	}
	outcome, writeErr := s.writer.Write(s.path, updatedDocument, mode)
	if !outcome.committed {
		if writeErr == nil {
			writeErr = errors.New("config writer did not commit the update")
		}
		return CascadeConfigUpdate{}, &StoreError{Code: StoreConfigWriteFailed, Err: writeErr}
	}
	if writeErr != nil {
		// The rename already committed; a directory-sync failure is reported
		// but must not make the UI claim the config was not saved.
		zap.L().Warn("cascade config rename committed but directory sync failed",
			zap.String("path", s.path),
			zap.Error(writeErr),
		)
	}

	return CascadeConfigUpdate{State: s.cascadeState(updatedConfig), Changed: changed}, nil
}

func applyCascadePatch(settings *CascadeConfig, patch CascadeConfigPatch) error {
	if patch.Enabled != nil {
		settings.Enabled = *patch.Enabled
	}
	if patch.MaxHops != nil {
		settings.MaxHops = *patch.MaxHops
	}
	if patch.MaxTTL != nil {
		parsed, err := time.ParseDuration(strings.TrimSpace(*patch.MaxTTL))
		if err != nil {
			return fmt.Errorf("cascade.max_ttl must be a Go duration: %w", err)
		}
		settings.MaxTTL = parsed
	}
	if patch.AllowInsecureHTTP != nil {
		settings.AllowInsecureHTTP = *patch.AllowInsecureHTTP
	}
	if patch.SharedToken != nil {
		settings.Token = strings.TrimSpace(*patch.SharedToken)
	}
	if patch.Peers == nil {
		return nil
	}

	existing := make(map[string]CascadePeerConfig, len(settings.Peers))
	for _, peer := range settings.Peers {
		existing[peer.Name] = peer
	}
	next := make([]CascadePeerConfig, 0, len(*patch.Peers))
	for _, input := range *patch.Peers {
		name := strings.TrimSpace(input.Name)
		peer := CascadePeerConfig{
			Name:               name,
			URL:                strings.TrimRight(strings.TrimSpace(input.URL), "/"),
			ForwardCredentials: input.ForwardCredentials,
		}
		if prior, ok := existing[name]; ok {
			peer.Token = prior.Token
		}
		if input.Token != nil {
			peer.Token = strings.TrimSpace(*input.Token)
		}
		next = append(next, peer)
	}
	settings.Peers = next
	return nil
}

func cascadeChangedFields(before, after CascadeConfig) []string {
	changed := make([]string, 0, 6)
	if before.Enabled != after.Enabled {
		changed = append(changed, "cascade.enabled")
	}
	if before.Token != after.Token {
		changed = append(changed, "cascade.token")
	}
	if before.MaxHops != after.MaxHops {
		changed = append(changed, "cascade.max_hops")
	}
	if before.MaxTTL != after.MaxTTL {
		changed = append(changed, "cascade.max_ttl")
	}
	if before.AllowInsecureHTTP != after.AllowInsecureHTTP {
		changed = append(changed, "cascade.allow_insecure_http")
	}
	if !reflect.DeepEqual(before.Peers, after.Peers) {
		changed = append(changed, "cascade.peers")
	}
	return changed
}

func (s *Store) cascadeState(configured *Config) CascadeConfigState {
	peers := make([]CascadePeerState, 0, len(configured.Cascade.Peers))
	for _, peer := range configured.Cascade.Peers {
		peers = append(peers, CascadePeerState{
			Name:               peer.Name,
			URL:                peer.URL,
			TokenSet:           peer.Token != "",
			ForwardCredentials: peer.ForwardCredentials,
		})
	}
	return CascadeConfigState{
		Enabled:           configured.Cascade.Enabled,
		MaxHops:           configured.Cascade.MaxHops,
		MaxTTL:            compactDuration(configured.Cascade.MaxTTL),
		AllowInsecureHTTP: configured.Cascade.AllowInsecureHTTP,
		TokenSet:          configured.Cascade.Token != "",
		Peers:             peers,
		ConfigWritable:    configWritable(s.path),
		PendingRestart:    !fingerprintCascade(configured.Cascade).equal(s.effectiveCascade),
	}
}

func (f cascadeFingerprint) equal(other cascadeFingerprint) bool {
	return f.Enabled == other.Enabled &&
		f.MaxHops == other.MaxHops &&
		f.MaxTTL == other.MaxTTL &&
		f.AllowInsecureHTTP == other.AllowInsecureHTTP &&
		f.TokenHash == other.TokenHash &&
		reflect.DeepEqual(f.Peers, other.Peers)
}

// readConfigDocument reads and decodes config.toml without touching the
// settings index. A missing file is treated as an empty document so first-run
// state can still create the cascade section.
func (s *Store) readConfigDocument() (*Config, []byte, fs.FileMode, error) {
	document, err := os.ReadFile(s.path)
	mode := fs.FileMode(0o644)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, 0, err
		}
		document = []byte{}
	} else {
		info, statErr := os.Stat(s.path)
		if statErr != nil {
			return nil, nil, 0, statErr
		}
		mode = info.Mode().Perm()
	}
	cfg, err := decodeConfigDocument(document)
	if err != nil {
		return nil, nil, 0, err
	}
	return cfg, document, mode, nil
}
