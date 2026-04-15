package docker

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"strings"
	"time"

	"depsilo/internal/config"
	"go.uber.org/zap"
)

// Registry holds runtime state for a configured registry.
type Registry struct {
	Name     string
	URL      string // e.g. "https://registry-1.docker.io"
	Username string
	Password string
	Client   *http.Client
}

// Resolver maps registry domains to Registry configs.
type Resolver struct {
	registries map[string]*Registry // name → Registry
	domainMap  map[string]string    // domain → registry name
	defaultReg string               // default registry name
}

func NewResolver(cfg config.DockerConfig) *Resolver {
	r := &Resolver{
		registries: make(map[string]*Registry),
		domainMap:  make(map[string]string),
		defaultReg: "",
	}

	for _, rc := range cfg.Registries {
		client := &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
				MaxIdleConnsPerHost: 10,
			},
		}

		if rc.Proxy != "" {
			proxyURL, err := url.Parse(rc.Proxy)
			if err == nil {
				client.Transport = &http.Transport{
					Proxy:               http.ProxyURL(proxyURL),
					TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
					MaxIdleConnsPerHost: 10,
				}
			}
		}

		reg := &Registry{
			Name:     rc.Name,
			URL:      strings.TrimRight(rc.URL, "/"),
			Username: rc.Username,
			Password: rc.Password,
			Client:   client,
		}
		r.registries[rc.Name] = reg

		if u, err := url.Parse(rc.URL); err == nil {
			domain := u.Host
			r.domainMap[domain] = rc.Name
			if host := u.Hostname(); host != domain {
				r.domainMap[host] = rc.Name
			}
		}
	}

	// Resolve default registry
	if cfg.DefaultRegistry != "" {
		for _, reg := range r.registries {
			if reg.Name == cfg.DefaultRegistry {
				r.defaultReg = reg.Name
				break
			}
		}
		if r.defaultReg == "" {
			if name, ok := r.domainMap[cfg.DefaultRegistry]; ok {
				r.defaultReg = name
			}
		}
		if r.defaultReg == "" && len(cfg.Registries) > 0 {
			r.defaultReg = cfg.Registries[0].Name
		}
	} else if len(cfg.Registries) > 0 {
		r.defaultReg = cfg.Registries[0].Name
	}

	// Add common Docker Hub aliases
	if name, ok := r.domainMap["registry-1.docker.io"]; ok {
		r.domainMap["docker.io"] = name
		r.domainMap["index.docker.io"] = name
	}

	// Log warnings for default registry resolution
	if cfg.DefaultRegistry != "" && r.defaultReg == "" {
		zap.L().Warn("docker: configured default_registry not found, no default set",
			zap.String("default_registry", cfg.DefaultRegistry),
		)
	} else if cfg.DefaultRegistry != "" && r.defaultReg != cfg.DefaultRegistry {
		zap.L().Info("docker: default_registry resolved",
			zap.String("configured", cfg.DefaultRegistry),
			zap.String("resolved_to", r.defaultReg),
		)
	}

	return r
}

// Resolve parses a request path and returns the target registry, image name, and endpoint.
func (r *Resolver) Resolve(path string) (reg *Registry, imageName string, endpoint string) {
	parts := strings.SplitN(path, "/", -1)
	if len(parts) < 3 {
		return nil, "", ""
	}

	endpointIdx := -1
	for i, p := range parts {
		if p == "manifests" || p == "blobs" || p == "tags" {
			endpointIdx = i
			break
		}
	}
	if endpointIdx < 1 {
		return nil, "", ""
	}

	firstPart := parts[0]
	endpoint = strings.Join(parts[endpointIdx:], "/")

	if strings.ContainsAny(firstPart, ".:") {
		regName, ok := r.domainMap[firstPart]
		if ok {
			reg = r.registries[regName]
			imageName = strings.Join(parts[1:endpointIdx], "/")
			return reg, imageName, endpoint
		}
		zap.L().Info("docker: unregistered domain in path, using default registry",
			zap.String("domain", firstPart),
			zap.String("default", r.defaultReg),
		)
	}

	if r.defaultReg != "" {
		reg = r.registries[r.defaultReg]
	}
	imageName = strings.Join(parts[0:endpointIdx], "/")
	return reg, imageName, endpoint
}

func (r *Resolver) Default() *Registry {
	if r.defaultReg == "" {
		return nil
	}
	return r.registries[r.defaultReg]
}

func (r *Resolver) HasRegistries() bool {
	return len(r.registries) > 0
}
