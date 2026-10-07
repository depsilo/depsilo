package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"depsilo/internal/cascade"
)

const (
	// cascadeTokenMinBytes keeps the shared peer secret from being trivially
	// guessable. The transport is often plain HTTP on a LAN, so the token is a
	// coordination secret rather than a substitute for network trust.
	cascadeTokenMinBytes = 16
	// cascadeMaxTTLCeiling keeps a peer from pinning a response far beyond the
	// freshness any protocol adapter would ever request.
	cascadeMaxTTLCeiling = 30 * 24 * time.Hour
	// cascadeMaxPeers bounds configuration parsing and outbound fan-out.
	cascadeMaxPeers = 16
	// defaultCascadeMaxHops and defaultCascadeMaxTTL mirror the loader defaults
	// so validation and fingerprinting agree on a config that omits them.
	defaultCascadeMaxHops = cascade.DefaultMaxHops
	defaultCascadeMaxTTL  = 7 * 24 * time.Hour
)

// validateCascadeConfig normalizes defaults and rejects configurations that
// would silently weaken the loop, authentication, or transport guarantees.
func validateCascadeConfig(cfg *Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}
	settings := &cfg.Cascade
	if settings.InstanceID != "" && !cascade.ValidInstanceID(settings.InstanceID) {
		return errors.New("cascade.instance_id must be 1-64 lowercase letters, digits, or '-'")
	}
	if strings.ContainsAny(settings.Token, " \t\r\n") {
		return errors.New("cascade.token must not contain whitespace")
	}
	if settings.Enabled && settings.Token == "" {
		return errors.New("cascade.token is required when cascade.enabled = true")
	}
	if settings.Token != "" && len(settings.Token) < cascadeTokenMinBytes {
		return fmt.Errorf("cascade.token must be at least %d bytes", cascadeTokenMinBytes)
	}
	if settings.MaxHops == 0 {
		settings.MaxHops = defaultCascadeMaxHops
	}
	if settings.MaxHops < 1 || settings.MaxHops > cascade.MaxChainEntries {
		return fmt.Errorf("cascade.max_hops must be between 1 and %d", cascade.MaxChainEntries)
	}
	if settings.MaxTTL == 0 {
		settings.MaxTTL = defaultCascadeMaxTTL
	}
	if settings.MaxTTL < time.Minute || settings.MaxTTL > cascadeMaxTTLCeiling {
		return errors.New("cascade.max_ttl must be between 1m and 720h")
	}
	if len(settings.Peers) > cascadeMaxPeers {
		return fmt.Errorf("cascade.peers supports at most %d entries", cascadeMaxPeers)
	}

	seen := make(map[string]struct{}, len(settings.Peers))
	for index := range settings.Peers {
		peer := &settings.Peers[index]
		if !validCascadePeerName(peer.Name) {
			return fmt.Errorf("cascade.peers[%d].name must match [a-z0-9][a-z0-9-]{0,63}", index)
		}
		if _, duplicate := seen[peer.Name]; duplicate {
			return fmt.Errorf("cascade.peers contains duplicate name %q", peer.Name)
		}
		seen[peer.Name] = struct{}{}
		if err := validateCascadePeerURL(peer.URL, settings.AllowInsecureHTTP); err != nil {
			return fmt.Errorf("cascade.peers[%d] (%s): %w", index, peer.Name, err)
		}
		if peer.Token == "" {
			// Empty means "inherit cascade.token"; keep the file faithful by
			// validating the effective value without materializing it.
			if settings.Enabled && settings.Token == "" {
				return fmt.Errorf("cascade.peers[%d] (%s) has no token and cascade.token is empty", index, peer.Name)
			}
			continue
		}
		if len(peer.Token) < cascadeTokenMinBytes {
			return fmt.Errorf("cascade.peers[%d] (%s).token must be at least %d bytes", index, peer.Name, cascadeTokenMinBytes)
		}
		if strings.ContainsAny(peer.Token, " \t\r\n") {
			return fmt.Errorf("cascade.peers[%d] (%s).token must not contain whitespace", index, peer.Name)
		}
	}

	for _, owner := range standardUpstreamConfigSlices(cfg) {
		for _, upstream := range owner.upstreams {
			if upstream.Via == "" {
				continue
			}
			if _, ok := seen[upstream.Via]; !ok {
				return fmt.Errorf(
					"%s upstream %q references unknown cascade peer %q",
					owner.ecosystem,
					upstream.Name,
					upstream.Via,
				)
			}
		}
	}
	for index := range cfg.ExtraIndexes {
		for _, upstream := range cfg.ExtraIndexes[index].Upstreams {
			if upstream.Via != "" {
				return fmt.Errorf(
					"extra index %q upstream %q cannot use cascade egress; use a standard ecosystem upstream instead",
					cfg.ExtraIndexes[index].Name,
					upstream.Name,
				)
			}
		}
	}
	return nil
}

func validCascadePeerName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for index, char := range name {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= '0' && char <= '9':
		case char == '-' && index > 0:
		default:
			return false
		}
	}
	return true
}

func validateCascadePeerURL(raw string, allowInsecureHTTP bool) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return errors.New("url is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("url must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil {
		return errors.New("url must be an origin without credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("url must not contain a query or fragment")
	}
	if strings.Contains(parsed.EscapedPath(), "..") {
		return errors.New("url path must not contain '..'")
	}
	if parsed.Scheme == "http" && !allowInsecureHTTP && !isLoopbackHost(parsed.Hostname()) {
		return errors.New("http peer URLs require cascade.allow_insecure_http = true or a loopback host")
	}
	return nil
}

type standardUpstreamConfigs struct {
	ecosystem string
	upstreams []UpstreamConfig
}

func standardUpstreamConfigSlices(cfg *Config) []standardUpstreamConfigs {
	return []standardUpstreamConfigs{
		{"pypi", cfg.PyPI.Upstreams},
		{"apt", cfg.APT.Upstreams},
		{"npm", cfg.NPM.Upstreams},
		{"go", cfg.Go.Upstreams},
		{"cargo", cfg.Cargo.Upstreams},
		{"maven", cfg.Maven.Upstreams},
		{"rubygems", cfg.RubyGems.Upstreams},
		{"composer", cfg.Composer.Upstreams},
		{"nuget", cfg.NuGet.Upstreams},
		{"conda", cfg.Conda.Upstreams},
		{"cran", cfg.CRAN.Upstreams},
		{"alpine", cfg.Alpine.Upstreams},
		{"helm", cfg.Helm.Upstreams},
		{"huggingface", cfg.HuggingFace.Upstreams},
	}
}
