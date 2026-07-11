package upstream

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"gorm.io/gorm"
)

const (
	SeedMarkerKey       = "upstreams_seeded_v1"
	ActiveEcosystemsKey = "upstreams_active_ecosystems_v1"
)

var supportedEcosystems = [...]string{
	"pypi", "apt", "npm", "go", "cargo", "maven", "rubygems",
	"composer", "nuget", "conda", "cran", "alpine", "helm", "huggingface",
}

var bootstrapMu sync.Mutex

type SeedSource struct {
	Ecosystem string
	Upstreams []config.UpstreamConfig
}

type BootstrapResult struct {
	ActiveEcosystems []string
}

func ReconcileBootstrap(database *gorm.DB, sources []SeedSource) (BootstrapResult, error) {
	explicit, err := indexSeedSources(sources)
	if err != nil {
		return BootstrapResult{}, err
	}
	bootstrapMu.Lock()
	defer bootstrapMu.Unlock()

	active := make([]string, 0)
	err = database.Transaction(func(tx *gorm.DB) error {
		var marker db.ControlPlaneState
		markerErr := tx.First(&marker, "key = ?", SeedMarkerKey).Error
		if markerErr != nil && !errors.Is(markerErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("load seed marker: %w", markerErr)
		}
		seeded := markerErr == nil
		if seeded && marker.Value != "true" {
			return fmt.Errorf("invalid seed marker value %q", marker.Value)
		}
		if seeded {
			var state db.ControlPlaneState
			if err := tx.First(&state, "key = ?", ActiveEcosystemsKey).Error; err != nil {
				return fmt.Errorf("load active ecosystems: %w", err)
			}
			var stored []string
			if err := json.Unmarshal([]byte(state.Value), &stored); err != nil {
				return fmt.Errorf("decode active ecosystems: %w", err)
			}
			if stored == nil {
				return errors.New("decode active ecosystems: expected JSON array")
			}
			active, err = canonicalActive(stored)
			if err != nil {
				return err
			}
		} else {
			for _, ecosystem := range supportedEcosystems {
				var count int64
				if err := tx.Model(&db.UpstreamRecord{}).Where("adapter_type = ?", ecosystem).Count(&count).Error; err != nil {
					return err
				}
				src, configured := explicit[ecosystem]
				if count > 0 || configured && len(src.Upstreams) > 0 {
					active = append(active, ecosystem)
				}
				if configured {
					if err := insertMissingConfigRows(tx, src); err != nil {
						return err
					}
				}
			}
		}

		if seeded {
			activeSet := make(map[string]bool, len(active))
			for _, name := range active {
				activeSet[name] = true
			}
			for _, ecosystem := range supportedEcosystems {
				src, configured := explicit[ecosystem]
				if !configured || activeSet[ecosystem] || len(src.Upstreams) == 0 {
					continue
				}
				if err := insertMissingConfigRows(tx, src); err != nil {
					return err
				}
				activeSet[ecosystem] = true
			}
			active = active[:0]
			for _, ecosystem := range supportedEcosystems {
				if activeSet[ecosystem] {
					active = append(active, ecosystem)
				}
			}
		}

		for _, ecosystem := range active {
			var count int64
			if err := tx.Model(&db.UpstreamRecord{}).Where("adapter_type = ?", ecosystem).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("active ecosystem %s has no upstreams", ecosystem)
			}
		}
		encoded, err := json.Marshal(active)
		if err != nil {
			return err
		}
		if err := tx.Save(&db.ControlPlaneState{Key: SeedMarkerKey, Value: "true"}).Error; err != nil {
			return err
		}
		return tx.Save(&db.ControlPlaneState{Key: ActiveEcosystemsKey, Value: string(encoded)}).Error
	})
	if err != nil {
		return BootstrapResult{}, err
	}
	return BootstrapResult{ActiveEcosystems: append(make([]string, 0, len(active)), active...)}, nil
}

func indexSeedSources(sources []SeedSource) (map[string]SeedSource, error) {
	supported := make(map[string]bool, len(supportedEcosystems))
	for _, ecosystem := range supportedEcosystems {
		supported[ecosystem] = true
	}
	out := make(map[string]SeedSource, len(sources))
	for _, src := range sources {
		if !supported[src.Ecosystem] {
			return nil, fmt.Errorf("unsupported seed ecosystem %q", src.Ecosystem)
		}
		if _, exists := out[src.Ecosystem]; exists {
			return nil, fmt.Errorf("duplicate seed ecosystem %q", src.Ecosystem)
		}
		normalized := SeedSource{Ecosystem: src.Ecosystem, Upstreams: make([]config.UpstreamConfig, 0, len(src.Upstreams))}
		names := make(map[string]bool, len(src.Upstreams))
		for _, item := range src.Upstreams {
			item, err := normalizeSeedUpstream(item)
			if err != nil {
				return nil, fmt.Errorf("invalid seed %s: %w", src.Ecosystem, err)
			}
			if names[item.Name] {
				return nil, fmt.Errorf("invalid seed %s: duplicate upstream name %q", src.Ecosystem, item.Name)
			}
			names[item.Name] = true
			normalized.Upstreams = append(normalized.Upstreams, item)
		}
		out[src.Ecosystem] = normalized
	}
	return out, nil
}

func normalizeSeedUpstream(item config.UpstreamConfig) (config.UpstreamConfig, error) {
	item.Name = strings.TrimSpace(item.Name)
	item.URL = strings.TrimSpace(item.URL)
	item.Proxy = strings.TrimSpace(item.Proxy)
	item.ProbeMode = strings.TrimSpace(item.ProbeMode)
	item.ProbeInterval = strings.TrimSpace(item.ProbeInterval)
	if item.Name == "" || len(item.Name) > 128 {
		return item, errors.New("name must be non-empty and at most 128 bytes")
	}
	if item.Priority <= 0 {
		return item, errors.New("priority must be positive")
	}
	if !validHTTPURL(item.URL) {
		return item, errors.New("url must be an http or https URL with a host")
	}
	if item.Proxy != "" && !validHTTPURL(item.Proxy) {
		return item, errors.New("proxy must be an http or https URL with a host")
	}
	if item.ProbeMode == "" {
		item.ProbeMode = "active"
	}
	if item.ProbeMode != "active" && item.ProbeMode != "passive" {
		return item, errors.New("probe_mode must be active or passive")
	}
	if item.ProbeInterval == "" {
		item.ProbeInterval = DefaultProbeInterval.String()
	}
	interval, err := time.ParseDuration(item.ProbeInterval)
	if err != nil || interval <= 0 {
		return item, errors.New("probe_interval must be a positive Go duration")
	}
	item.ProbeInterval = interval.String()
	return item, nil
}

func validHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func canonicalActive(stored []string) ([]string, error) {
	wanted := make(map[string]bool, len(stored))
	supported := make(map[string]bool, len(supportedEcosystems))
	for _, ecosystem := range supportedEcosystems {
		supported[ecosystem] = true
	}
	for _, ecosystem := range stored {
		if !supported[ecosystem] {
			return nil, fmt.Errorf("unsupported active ecosystem %q", ecosystem)
		}
		if wanted[ecosystem] {
			return nil, fmt.Errorf("duplicate active ecosystem %q", ecosystem)
		}
		wanted[ecosystem] = true
	}
	active := make([]string, 0, len(wanted))
	for _, ecosystem := range supportedEcosystems {
		if wanted[ecosystem] {
			active = append(active, ecosystem)
		}
	}
	return active, nil
}

func insertMissingConfigRows(tx *gorm.DB, src SeedSource) error {
	for _, item := range src.Upstreams {
		var count int64
		if err := tx.Model(&db.UpstreamRecord{}).Where("adapter_type = ? AND name = ?", src.Ecosystem, item.Name).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			continue
		}
		record := db.UpstreamRecord{
			AdapterType:   src.Ecosystem,
			Name:          item.Name,
			URL:           item.URL,
			Proxy:         item.Proxy,
			Priority:      item.Priority,
			ProbeMode:     item.ProbeMode,
			ProbeInterval: item.ProbeInterval,
			Healthy:       true,
			SuccessRate:   1,
		}
		if err := tx.Create(&record).Error; err != nil {
			return fmt.Errorf("seed %s/%s: %w", src.Ecosystem, item.Name, err)
		}
	}
	return nil
}
