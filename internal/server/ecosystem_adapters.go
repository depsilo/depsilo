package server

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/adapter/alpine"
	"depsilo/internal/adapter/apt"
	"depsilo/internal/adapter/cargo"
	"depsilo/internal/adapter/composer"
	"depsilo/internal/adapter/conda"
	"depsilo/internal/adapter/cran"
	"depsilo/internal/adapter/goproxy"
	"depsilo/internal/adapter/helm"
	"depsilo/internal/adapter/huggingface"
	"depsilo/internal/adapter/maven"
	"depsilo/internal/adapter/npm"
	"depsilo/internal/adapter/nuget"
	"depsilo/internal/adapter/pypi"
	"depsilo/internal/adapter/rubygems"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/ecosystem"
	"depsilo/internal/upstream"
)

type adapterFactory func(*cache.Manager, upstream.Selector, config.CacheConfig, *gorm.DB) adapter.Adapter

type ecosystemDef struct {
	name      string
	route     string
	upstreams []config.UpstreamConfig
	explicit  bool
	factory   adapterFactory
}

type adapterBinding struct {
	upstreams func(*config.Config) []config.UpstreamConfig
	factory   adapterFactory
}

func standardEcosystemDefinitions(cfg *config.Config, npmTarballSigningKey []byte) []ecosystemDef {
	bindings := map[string]adapterBinding{
		"pypi": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.PyPI.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return pypi.New(cm, s, cc, database)
		}},
		"apt": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.APT.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return apt.New(cm, s, cc, database)
		}},
		"npm": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.NPM.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return npm.New(cm, s, cc, database, append([]byte(nil), npmTarballSigningKey...))
		}},
		"go": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.Go.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return goproxy.New(cm, s, cc, database)
		}},
		"cargo": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.Cargo.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return cargo.New(cm, s, cc, database)
		}},
		"maven": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.Maven.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return maven.New(cm, s, cc, database)
		}},
		"rubygems": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.RubyGems.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return rubygems.New(cm, s, cc, database)
		}},
		"composer": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.Composer.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return composer.New(cm, s, cc, database)
		}},
		"nuget": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.NuGet.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return nuget.New(cm, s, cc, database)
		}},
		"conda": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.Conda.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return conda.New(cm, s, cc, database)
		}},
		"cran": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.CRAN.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return cran.New(cm, s, cc, database)
		}},
		"alpine": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.Alpine.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return alpine.New(cm, s, cc, database)
		}},
		"helm": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.Helm.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return helm.New(cm, s, cc, database)
		}},
		"huggingface": {func(cfg *config.Config) []config.UpstreamConfig { return cfg.HuggingFace.Upstreams }, func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, database *gorm.DB) adapter.Adapter {
			return huggingface.New(cm, s, cc, database)
		}},
	}

	metadata := ecosystem.StandardUpstreamDefinitions()
	definitions := make([]ecosystemDef, 0, len(metadata))
	for _, definition := range metadata {
		binding, ok := bindings[definition.Name]
		if !ok {
			panic(fmt.Sprintf("ecosystem catalog entry %q has no compiled adapter binding", definition.Name))
		}
		definitions = append(definitions, ecosystemDef{
			name:      definition.Name,
			route:     definition.Route,
			upstreams: binding.upstreams(cfg),
			explicit:  cfg.ExplicitUpstreamEcosystems[definition.Name],
			factory:   binding.factory,
		})
	}
	if len(definitions) != len(bindings) {
		panic("compiled standard adapter binding is missing from the ecosystem catalog")
	}
	return definitions
}

func seedSources(definitions []ecosystemDef) []upstream.SeedSource {
	sources := make([]upstream.SeedSource, 0, len(definitions))
	for _, definition := range definitions {
		if definition.explicit {
			sources = append(sources, upstream.SeedSource{Ecosystem: definition.name, Upstreams: definition.upstreams})
		}
	}
	return sources
}

func activeDefinitions(definitions []ecosystemDef, active []string) ([]ecosystemDef, error) {
	byName := make(map[string]ecosystemDef, len(definitions))
	for _, definition := range definitions {
		byName[definition.name] = definition
	}
	result := make([]ecosystemDef, 0, len(active))
	for _, ecosystem := range active {
		definition, ok := byName[ecosystem]
		if !ok {
			return nil, fmt.Errorf("active ecosystem %q has no compiled adapter", ecosystem)
		}
		result = append(result, definition)
	}
	return result, nil
}

func registerActiveAdapters(root *gin.Engine, project *gin.RouterGroup, definitions []ecosystemDef, pools map[string]*upstream.Pool, cacheMgr *cache.Manager, cacheConfig config.CacheConfig, database *gorm.DB) error {
	for _, definition := range definitions {
		pool := pools[definition.name]
		if pool == nil {
			return fmt.Errorf("active ecosystem %s has no pool", definition.name)
		}
		handler := definition.factory(cacheMgr, upstream.NewPassiveRecoverySelector(pool), cacheConfig, database)
		handler.Register(root.Group(definition.route))
		handler.Register(project.Group(definition.route))
	}
	return nil
}
