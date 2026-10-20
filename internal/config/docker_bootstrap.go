package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
	"go.uber.org/zap"
)

// DockerBootstrapState exposes activation facts without registry URLs or credentials.
type DockerBootstrapState struct {
	Configured        bool `json:"configured"`
	Effective         bool `json:"effective"`
	DockerHub         bool `json:"docker_hub"`
	PendingRestart    bool `json:"pending_restart"`
	ConfigWritable    bool `json:"config_writable"`
	BlockedByOverride bool `json:"blocked_by_override"`
}

func (s *Store) DockerBootstrap(ctx context.Context) (DockerBootstrapState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return DockerBootstrapState{}, err
	}
	cfg, _, _, err := s.readConfigDocument()
	if err != nil {
		return DockerBootstrapState{}, &StoreError{Code: StoreConfigReadFailed, Err: err}
	}
	return s.dockerBootstrapState(cfg.Docker), nil
}

// EnableDockerHub only initializes an empty Docker registry configuration.
// Existing operator-owned registries and credentials are never overwritten.
func (s *Store) EnableDockerHub(ctx context.Context) (DockerBootstrapState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return DockerBootstrapState{}, err
	}
	cfg, document, _, err := s.readConfigDocument()
	if err != nil {
		return DockerBootstrapState{}, &StoreError{Code: StoreConfigReadFailed, Err: err}
	}
	if dockerOverridden() {
		return DockerBootstrapState{}, &StoreError{Code: StoreInvalidSetting, Err: errors.New("Docker environment overrides must be removed before enabling Docker Hub through Admin")}
	}
	if len(cfg.Docker.Registries) > 0 {
		if dockerHubDefault(cfg.Docker) {
			return s.dockerBootstrapState(cfg.Docker), nil
		}
		return DockerBootstrapState{}, &StoreError{Code: StoreInvalidSetting, Err: errors.New("Docker registries are already configured; edit config.toml to change them")}
	}
	if cfg.Docker.DefaultRegistry != "" && cfg.Docker.DefaultRegistry != "docker.io" && cfg.Docker.DefaultRegistry != "dockerhub" && cfg.Docker.DefaultRegistry != "registry-1.docker.io" {
		return DockerBootstrapState{}, &StoreError{Code: StoreInvalidSetting, Err: errors.New("Docker default registry is customized; edit config.toml to enable it")}
	}
	updated, err := appendDockerHub(document)
	if err != nil {
		return DockerBootstrapState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	validated, err := decodeConfigDocument(updated)
	if err != nil {
		return DockerBootstrapState{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	if !configWritable(s.path) {
		return DockerBootstrapState{}, &StoreError{Code: StoreConfigReadOnly, Err: fmt.Errorf("config file %q is read-only", s.path)}
	}
	// The file may be missing or operator-managed with loose permissions. The
	// bootstrap operation always protects the whole document, including secrets
	// in unrelated sections, rather than inheriting a permissive file mode.
	outcome, writeErr := s.writer.Write(s.path, updated, 0o600)
	if !outcome.committed {
		if writeErr == nil {
			writeErr = errors.New("config writer did not commit the update")
		}
		return DockerBootstrapState{}, &StoreError{Code: StoreConfigWriteFailed, Err: writeErr}
	}
	if writeErr != nil || outcome.durabilityErr != nil {
		zap.L().Warn("docker config committed with a durability warning", zap.Error(errors.Join(writeErr, outcome.durabilityErr)))
	}
	return s.dockerBootstrapState(validated.Docker), nil
}

func (s *Store) dockerBootstrapState(configured DockerConfig) DockerBootstrapState {
	active := len(s.effectiveDocker.Registries) > 0
	saved := len(configured.Registries) > 0
	hubSaved := dockerHubDefault(configured)
	hubActive := dockerHubDefault(s.effectiveDocker)
	blocked := dockerOverridden()
	return DockerBootstrapState{
		Configured: saved, Effective: active, DockerHub: hubSaved && hubActive,
		PendingRestart: !blocked && (saved || active) && !reflect.DeepEqual(configured, s.effectiveDocker),
		ConfigWritable: configWritable(s.path), BlockedByOverride: blocked,
	}
}

func dockerHubDefault(cfg DockerConfig) bool {
	for index, registry := range cfg.Registries {
		if strings.TrimRight(registry.URL, "/") != "https://registry-1.docker.io" {
			continue
		}
		if cfg.DefaultRegistry == "" && index == 0 || cfg.DefaultRegistry == registry.Name || cfg.DefaultRegistry == "docker.io" || cfg.DefaultRegistry == "registry-1.docker.io" {
			return true
		}
	}
	return false
}

func dockerOverridden() bool {
	return os.Getenv("DEPSILO_DOCKER_REGISTRIES") != "" || os.Getenv("DEPSILO_DOCKER_DEFAULT_REGISTRY") != ""
}

func appendDockerHub(document []byte) ([]byte, error) {
	var parser unstable.Parser
	parser.Reset(document)
	sectionStart := -1
	insertAt := len(document)
	for parser.NextExpression() {
		expression := parser.Expression()
		if expression.Kind == unstable.KeyValue && sectionStart < 0 {
			key := nodeKey(expression)
			if len(key) == 1 && key[0] == "docker" {
				return nil, errors.New("docker is an inline table; use a [docker] table")
			}
		}
		if expression.Kind != unstable.Table && expression.Kind != unstable.ArrayTable {
			continue
		}
		path := nodeKey(expression)
		start := lineStartAt(document, firstKeyOffset(expression))
		if len(path) == 1 && path[0] == "docker" {
			sectionStart = start
			continue
		}
		if sectionStart >= 0 {
			insertAt = start
			break
		}
	}
	if err := parser.Error(); err != nil {
		return nil, fmt.Errorf("parse Docker configuration: %w", err)
	}
	newline := "\n"
	if bytes.Contains(document, []byte("\r\n")) {
		newline = "\r\n"
	}
	block := ""
	if sectionStart < 0 {
		block = "[docker]" + newline + "default_registry = \"docker.io\"" + newline + newline
	}
	block += "[[docker.registries]]" + newline + "name = \"dockerhub\"" + newline + "url = \"https://registry-1.docker.io\"" + newline
	if insertAt > 0 && document[insertAt-1] != '\n' {
		block = newline + block
	}
	if sectionStart >= 0 && insertAt > 0 {
		block = newline + block
	}
	if insertAt < len(document) {
		block += newline
	}
	updated := make([]byte, 0, len(document)+len(block))
	updated = append(updated, document[:insertAt]...)
	updated = append(updated, block...)
	updated = append(updated, document[insertAt:]...)
	return updated, nil
}
