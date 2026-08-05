package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter"
	"depsilo/internal/adapter/pypi"
	"depsilo/internal/api/admin"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstreamupdates"
)

// indexRefreshResponseWriter drains the internal proxy response without
// buffering a potentially large package index or retaining an upstream error
// body that could contain credentials.
type indexRefreshResponseWriter struct {
	header http.Header
	status int
}

func newIndexRefreshResponseWriter() *indexRefreshResponseWriter {
	return &indexRefreshResponseWriter{header: make(http.Header)}
}

func (w *indexRefreshResponseWriter) Header() http.Header { return w.header }

func (w *indexRefreshResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *indexRefreshResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return len(p), nil
}

// Flush lets Gin use this writer for handlers that explicitly flush headers.
func (w *indexRefreshResponseWriter) Flush() {}

func (w *indexRefreshResponseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// NewCacheIndexRefresher returns the shared proxy-aware refresh seam used by
// both manual index refreshes and the proactive update producer.
func NewCacheIndexRefresher(
	router *gin.Engine,
	extraIndexes []config.ExtraIndexConfig,
	dockerConfig config.DockerConfig,
) upstreamupdates.Refresher {
	internalRouter := adapter.SuppressAccessLogging(router)
	return func(ctx context.Context, entry db.CacheEntry) (upstreamupdates.RefreshOutcome, error) {
		proxyPath, err := cacheIndexProxyPath(entry, extraIndexes, dockerConfig)
		if err != nil {
			return upstreamupdates.RefreshOutcome{}, err
		}

		refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		forceCtx, tracker := cache.WithTrackedForceRefresh(refreshCtx)
		req, err := http.NewRequestWithContext(
			forceCtx,
			http.MethodGet,
			proxyPath,
			nil,
		)
		if err != nil {
			return upstreamupdates.RefreshOutcome{}, fmt.Errorf("build internal index refresh request: %w", err)
		}
		req.RemoteAddr = "127.0.0.1:0"
		req.Header.Set("X-Depsilo-Internal-Refresh", "index")

		writer := newIndexRefreshResponseWriter()
		internalRouter.ServeHTTP(writer, req)

		var cacheOutcome cache.RefreshOutcome
		var trackerErr error
		if tracker.Used() {
			cacheOutcome, trackerErr = tracker.Outcome(refreshCtx)
		}
		outcome := upstreamupdates.RefreshOutcome{Upstream: cacheOutcome.Upstream}
		if status := writer.Status(); status < http.StatusOK || status >= http.StatusMultipleChoices {
			return outcome, fmt.Errorf("proxy refresh returned HTTP %d", status)
		}

		if !tracker.Used() {
			return upstreamupdates.RefreshOutcome{}, fmt.Errorf("index proxy did not execute a cache refresh")
		}
		if trackerErr != nil {
			return outcome, fmt.Errorf("persist refreshed index: %w", trackerErr)
		}
		detail := "cached metadata refreshed"
		if cacheOutcome.NotModified {
			detail = "upstream metadata not modified"
		}
		outcome.Changed = !cacheOutcome.NotModified
		outcome.Detail = detail
		return outcome, nil
	}
}

func cacheIndexProxyPath(
	entry db.CacheEntry,
	extraIndexes []config.ExtraIndexConfig,
	dockerConfig config.DockerConfig,
) (string, error) {
	if entry.AdapterType == "docker" {
		return dockerIndexProxyPath(entry, dockerConfig)
	}
	if !strings.HasPrefix(entry.AdapterType, "extra:") {
		return admin.CacheIndexPublicPath(entry)
	}

	name := strings.TrimPrefix(entry.AdapterType, "extra:")
	if name == "" {
		return "", fmt.Errorf("invalid extra index adapter %q", entry.AdapterType)
	}
	var selected *config.ExtraIndexConfig
	for _, index := range extraIndexes {
		if index.Name == name {
			copy := index
			selected = &copy
			break
		}
	}
	if selected == nil {
		return "", fmt.Errorf("no safe public route is configured for extra index %q", name)
	}
	route := strings.Trim(selected.Path, "/")
	if route == "" || !safeConfiguredRoute(route) {
		return "", fmt.Errorf("no safe public route is configured for extra index %q", name)
	}

	var pkg string
	if selected.Kind == config.ExtraIndexKindPyTorch {
		channel, packageName, ok := pypi.ChannelIndexFromCacheKey(entry.AdapterType, entry.Key)
		if !ok {
			return "", fmt.Errorf("unsupported PyTorch channel cache key %q", entry.Key)
		}
		route += "/" + channel
		pkg = packageName
	} else {
		packageName, ok := pypi.IndexPackageFromCacheKey(entry.AdapterType, entry.Key)
		if !ok {
			return "", fmt.Errorf("unsupported extra index cache key %q", entry.Key)
		}
		pkg = packageName
	}
	// Reuse the standard PyPI mapper for package-name and traversal checks,
	// then substitute the configured route prefix.
	synthetic := entry
	synthetic.AdapterType = "pypi"
	synthetic.Key = pypi.IndexCacheKey("pypi", pkg)
	standardPath, err := admin.CacheIndexPublicPath(synthetic)
	if err != nil {
		return "", err
	}
	return "/" + route + strings.TrimPrefix(standardPath, "/pypi"), nil
}

func dockerIndexProxyPath(entry db.CacheEntry, dockerConfig config.DockerConfig) (string, error) {
	if entry.CacheKind != db.CacheKindMetadata {
		return "", fmt.Errorf("cache entry %d is not index metadata", entry.ID)
	}
	rest, ok := strings.CutPrefix(entry.Key, "docker/")
	if !ok {
		return "", fmt.Errorf("unsupported docker index cache key %q", entry.Key)
	}
	registryName, storedPath, ok := strings.Cut(rest, "/")
	if !ok || registryName == "" || storedPath == "" {
		return "", fmt.Errorf("unsupported docker index cache key %q", entry.Key)
	}
	kind, payload, ok := strings.Cut(storedPath, "/")
	if !ok || payload == "" {
		return "", fmt.Errorf("unsupported docker index cache key %q", entry.Key)
	}

	var publicSuffix string
	switch kind {
	case "tags":
		image, ok := strings.CutSuffix(payload, "/list")
		if !ok || image == "" {
			return "", fmt.Errorf("unsupported docker tag-list cache key %q", entry.Key)
		}
		publicSuffix = image + "/tags/list"
	case "manifests":
		lastSlash := strings.LastIndex(payload, "/")
		if lastSlash <= 0 || lastSlash == len(payload)-1 {
			return "", fmt.Errorf("unsupported docker manifest cache key %q", entry.Key)
		}
		image, reference := payload[:lastSlash], payload[lastSlash+1:]
		if strings.HasPrefix(reference, "sha256__") {
			return "", fmt.Errorf("digest-addressed docker manifest is not mutable index metadata")
		}
		publicSuffix = image + "/manifests/" + reference
	default:
		return "", fmt.Errorf("unsupported docker index cache key %q", entry.Key)
	}

	var registry *config.RegistryConfig
	for i := range dockerConfig.Registries {
		if dockerConfig.Registries[i].Name == registryName {
			registry = &dockerConfig.Registries[i]
			break
		}
	}
	if registry == nil {
		return "", fmt.Errorf("docker registry %q is no longer configured", registryName)
	}
	registryURL, err := url.Parse(registry.URL)
	if err != nil || registryURL.Host == "" {
		return "", fmt.Errorf("docker registry %q has an invalid URL", registryName)
	}

	// Resolver recognizes domains containing a dot or port. A plain hostname
	// such as "localhost" can only address the configured default registry, in
	// which case omitting the host selects it unambiguously.
	routePrefix := registryURL.Host
	if !strings.ContainsAny(routePrefix, ".:") {
		if configuredDockerDefaultName(dockerConfig) != registryName {
			return "", fmt.Errorf("docker registry %q cannot be selected through its public route", registryName)
		}
		routePrefix = ""
	}
	publicPath := "/v2/"
	if routePrefix != "" {
		publicPath += routePrefix + "/"
	}
	publicPath += publicSuffix
	if !safeConfiguredRoute(strings.TrimPrefix(publicPath, "/")) {
		return "", fmt.Errorf("unsafe docker index route derived from %q", entry.Key)
	}
	return publicPath, nil
}

func configuredDockerDefaultName(dockerConfig config.DockerConfig) string {
	if len(dockerConfig.Registries) == 0 {
		return ""
	}
	configured := dockerConfig.DefaultRegistry
	if configured != "" {
		for _, registry := range dockerConfig.Registries {
			if registry.Name == configured {
				return registry.Name
			}
			if parsed, err := url.Parse(registry.URL); err == nil &&
				(parsed.Host == configured || parsed.Hostname() == configured) {
				return registry.Name
			}
		}
	}
	return dockerConfig.Registries[0].Name
}

func safeConfiguredRoute(route string) bool {
	if route == "" || strings.ContainsAny(route, "\\?#%\x00\r\n") || strings.Contains(route, "//") {
		return false
	}
	for _, segment := range strings.Split(route, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
