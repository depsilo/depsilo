package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/accesslog"
	"depsilo/internal/adapter"
	dockeradapter "depsilo/internal/adapter/docker"
	"depsilo/internal/adapter/pypi"
	"depsilo/internal/api"
	"depsilo/internal/asyncruntime"
	"depsilo/internal/audit"
	"depsilo/internal/blocklist"
	"depsilo/internal/cache"
	"depsilo/internal/compilecache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/entitlement"
	"depsilo/internal/license"
	"depsilo/internal/middleware"
	"depsilo/internal/notify"
	"depsilo/internal/quarantine"
	"depsilo/internal/quarantine/resolvers"
	"depsilo/internal/rules"
	"depsilo/internal/security"
	"depsilo/internal/tamper"
	"depsilo/internal/trial"
	"depsilo/internal/upstream"
	"depsilo/internal/upstreamupdates"
)

// StartServer initializes all components and starts the HTTP server.
// Returns the *http.Server for lifecycle control by the caller.
// Cancelling the provided context requests an orderly server shutdown. The
// server owns a detached runtime context so HTTP handlers drain before their
// background dependencies are cancelled.
func StartServer(ctx context.Context, logLevel zap.AtomicLevel) (_ *http.Server, resultErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("start server: %w", err)
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	parsed, err := zap.ParseAtomicLevel(cfg.Server.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("parse server.log_level: %w", err)
	}
	logLevel.SetLevel(parsed.Level())
	settingsStore := config.NewStore(cfg.ConfigPath, cfg, logLevel)
	r := gin.New()
	if err := configureTrustedProxies(r, cfg.Server.TrustedProxies); err != nil {
		return nil, fmt.Errorf("configure server.trusted_proxies: %w", err)
	}
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	shutdownTrigger := ctx
	serverCtx, cancelServer := newServerRuntimeContext(ctx)
	started := false
	var accessRecorder accesslog.Recorder
	var registry *upstream.Registry
	var cacheMgr *cache.Manager
	var securityScanner *security.Scanner
	var osvFetcher *security.Fetcher
	background := asyncruntime.New(serverCtx)
	resources := newServerResources(cancelServer, listener.Close)
	resources.background = background
	submitBackground := func(name string, task asyncruntime.Task) error {
		if err := background.Submit(task); err != nil {
			return fmt.Errorf("start %s: %w", name, err)
		}
		return nil
	}
	defer func() {
		if started {
			return
		}
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 35*time.Second)
		cleanupErr := resources.Close(cleanupCtx)
		cancelCleanup()
		if cleanupErr == nil {
			return
		}
		resultErr = errors.Join(resultErr, fmt.Errorf("clean up failed server startup: %w", cleanupErr))
		go closeResourcesWithRetry(resources, func(err error) {
			zap.L().Warn("failed server startup cleanup attempt; retrying", zap.Error(err))
		})
	}()
	zap.L().Info("config loaded",
		zap.String("db_driver", cfg.Database.Driver),
		zap.String("storage_type", cfg.Storage.Type),
	)

	// Initialize database
	database, err := db.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("access database pool: %w", err)
	}
	resources.closeDatabase = newAsyncCloseAdapter(sqlDatabase.Close)
	if err := db.AutoMigrate(database); err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	definitions := standardEcosystemDefinitions(cfg)
	bootstrap, err := upstream.ReconcileBootstrap(database, seedSources(definitions))
	if err != nil {
		return nil, fmt.Errorf("reconcile upstream control plane: %w", err)
	}
	registry, err = upstream.NewRegistry(database, bootstrap.ActiveEcosystems)
	if err != nil {
		return nil, fmt.Errorf("build upstream registry: %w", err)
	}
	resources.closeRegistry = resourceCloseFunc(registry.CloseContext)
	pools := registry.Pools()
	activeDefs, err := activeDefinitions(definitions, bootstrap.ActiveEcosystems)
	if err != nil {
		return nil, err
	}

	// Interactive first-run setup creates the administrator from wizard input.
	// Configured/headless deployments use environment credentials or a random
	// one-time password emitted to the server log.
	if err := api.EnsureInitialAdmin(database, cfg.IsDefault); err != nil {
		return nil, fmt.Errorf("ensure initial administrator: %w", err)
	}

	// Backfill PackageName for existing cache entries
	backfillPackageNames(database)

	// Access log rollup pipeline: one-shot backfill from access_logs into
	// the rollup tables (no-op if already populated), then start the
	// async batched recorder so future requests stream through it. Raw-only
	// mode revokes fine-history readiness before the recorder starts.
	if cfg.AccessLog.BackfillOnStart {
		if err := accesslog.BackfillIfEmpty(serverCtx, database); err != nil {
			zap.L().Warn("access log rollup backfill failed", zap.Error(err))
		}
	}
	if err := prepareFiveMinuteHistory(serverCtx, database, cfg.AccessLog); err != nil {
		return nil, err
	}
	accessRecorder = accesslog.NewRecorder(database, accesslog.Config{
		Enabled:       cfg.AccessLog.RollupEnabled,
		BatchSize:     cfg.AccessLog.BatchSize,
		BatchInterval: cfg.AccessLog.BatchInterval,
	})
	resources.accessRecorder = accessRecorder
	if err := submitBackground("access log compactor", func(ctx context.Context) {
		accesslog.StartCompactor(ctx, database)
	}); err != nil {
		return nil, err
	}
	if err := submitBackground("access log retention", func(ctx context.Context) {
		accesslog.StartRetention(ctx, database, accesslog.RetentionConfig{
			RawDays:        cfg.AccessLog.RetentionDays,
			FiveMinuteDays: 8,
			RollupDays:     cfg.AccessLog.RollupRetentionDays,
		})
	}); err != nil {
		return nil, err
	}

	// Initialize storage
	var storage cache.Storage
	switch cfg.Storage.Type {
	case "local":
		storage, err = cache.NewLocalStorage(cfg.Storage.Path)
		if err != nil {
			return nil, fmt.Errorf("init local storage: %w", err)
		}
	case "s3":
		storage, err = cache.NewS3Storage(
			cfg.Storage.Endpoint,
			cfg.Storage.Bucket,
			cfg.Storage.Region,
			cfg.Storage.AccessKey,
			cfg.Storage.SecretKey,
		)
		if err != nil {
			return nil, fmt.Errorf("init s3 storage: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Storage.Type)
	}

	// Initialize event bus and cache manager
	eventBus := cache.NewEventBus()
	// Immutable-artifact threshold for tamper detection: artifacts are
	// fetched with ttl=ttl_blob and metadata with ttl=ttl_index, so
	// ttl >= ttl_blob selects long-lived artifacts. Metadata is independently
	// excluded by its adapter cache-key shape, so unusual ttl_index values do
	// not turn mutable indexes into immutable content. An explicit config
	// override still wins for artifact verification policy.
	immutableThreshold := cfg.SupplyChain.TamperDetection.ImmutableThresholdOverride()
	if immutableThreshold <= 0 {
		immutableThreshold = cfg.Cache.TTLBlob
	}
	cacheMgr = cache.NewManager(storage, database, eventBus, immutableThreshold)
	resources.cacheManager = cacheMgr
	cacheRetention, err := cache.NewRetention(cacheMgr, cache.DefaultRetentionPolicy(
		int64(cfg.Cache.MaxSizeGB)*1024*1024*1024,
		cfg.Cache.LRUThreshold,
	))
	if err != nil {
		return nil, fmt.Errorf("configure cache retention: %w", err)
	}

	// Compiler artifacts have different trust, retention and capacity
	// semantics from package artifacts. Always use a separate storage root or
	// bucket, even though both data domains share the Storage implementation.
	var compileCacheService *compilecache.Service
	compileCacheAuthorizer := compilecache.NewAuthorizer(database)
	if cfg.CompileCache.Enabled {
		compileStorageConfig := cfg.CompileCache.Storage
		if cfg.Storage.Type == "local" && compileStorageConfig.Type == "local" {
			overlaps, overlapErr := localStoragePathsOverlap(cfg.Storage.Path, compileStorageConfig.Path)
			if overlapErr != nil {
				return nil, fmt.Errorf("compare package and compiler cache paths: %w", overlapErr)
			}
			if overlaps {
				return nil, errors.New("compile_cache.storage.path must not overlap storage.path")
			}
		}
		if cfg.Storage.Type == "s3" && compileStorageConfig.Type == "s3" &&
			strings.EqualFold(strings.TrimRight(cfg.Storage.Endpoint, "/"), strings.TrimRight(compileStorageConfig.Endpoint, "/")) &&
			cfg.Storage.Bucket == compileStorageConfig.Bucket {
			return nil, errors.New("compile_cache.storage.bucket must be separate from the package-cache bucket")
		}
		var compileStorage cache.Storage
		switch compileStorageConfig.Type {
		case "local":
			compileStorage, err = cache.NewPrivateLocalStorage(compileStorageConfig.Path)
		case "s3":
			compileStorage, err = cache.NewS3Storage(
				compileStorageConfig.Endpoint,
				compileStorageConfig.Bucket,
				compileStorageConfig.Region,
				compileStorageConfig.AccessKey,
				compileStorageConfig.SecretKey,
			)
		default:
			err = fmt.Errorf("unsupported storage type %q", compileStorageConfig.Type)
		}
		if err != nil {
			return nil, fmt.Errorf("init compiler-cache storage: %w", err)
		}
		compileCacheService, err = compilecache.NewService(compileStorage, database, compilecache.Limits{
			MaxBytes:               int64(cfg.CompileCache.MaxSizeGB) * 1024 * 1024 * 1024,
			MaxEntries:             cfg.CompileCache.MaxEntries,
			MaxEntryBytes:          int64(cfg.CompileCache.MaxEntrySizeMB) * 1024 * 1024,
			NamespaceMaxBytes:      int64(cfg.CompileCache.NamespaceMaxSizeGB) * 1024 * 1024 * 1024,
			NamespaceMaxEntries:    cfg.CompileCache.NamespaceMaxEntries,
			MaxConcurrentUploads:   cfg.CompileCache.MaxConcurrentUploads,
			MaxQueuedUploads:       cfg.CompileCache.MaxQueuedUploads,
			MaxInflightUploadBytes: int64(cfg.CompileCache.MaxInflightUploadSizeMB) * 1024 * 1024,
			UploadTimeout:          cfg.CompileCache.UploadTimeout,
			MaxConcurrentDownloads: cfg.CompileCache.MaxConcurrentDownloads,
			DownloadTimeout:        cfg.CompileCache.DownloadTimeout,
			HighWatermarkPercent:   cfg.CompileCache.LRUThreshold,
		})
		if err != nil {
			return nil, fmt.Errorf("init compiler cache: %w", err)
		}
		if err := compileCacheService.ProcessPendingDeletions(serverCtx, 1000); err != nil {
			return nil, fmt.Errorf("retry compiler-cache deletions: %w", err)
		}
		// No request can be active during single-instance startup, so every
		// unreferenced generation (including local .tmp files) is safe to reclaim.
		if err := compileCacheService.Reconcile(serverCtx, 0); err != nil {
			return nil, fmt.Errorf("reconcile compiler cache: %w", err)
		}
		if _, err := compileCacheService.EnforceLimits(serverCtx); err != nil {
			return nil, fmt.Errorf("enforce compiler-cache limits: %w", err)
		}
		compileCacheService.SetObserver(compilecache.Observer{
			StatsUpdated: func(stats compilecache.Stats) {
				api.M.CompileCacheSizeBytes.Set(float64(stats.SizeBytes))
				api.M.CompileCacheEntries.Set(float64(stats.Entries))
			},
			Evicted: func(reason string, entries int) {
				api.M.CompileCacheEvictions.WithLabelValues(reason).Add(float64(entries))
			},
		})
		if err := submitBackground("compiler-cache maintenance", compileCacheService.RunMaintenance); err != nil {
			return nil, err
		}
		zap.L().Info("compiler cache enabled",
			zap.String("ccache_endpoint", "/ccache/v1/{namespace}"),
			zap.String("sccache_endpoint", "/sccache/v1/{namespace}"),
			zap.String("public_url", cfg.CompileCache.PublicURL),
			zap.String("storage_type", compileStorageConfig.Type),
			zap.Int("max_size_gb", cfg.CompileCache.MaxSizeGB),
			zap.Int("max_entry_size_mb", cfg.CompileCache.MaxEntrySizeMB),
		)
		if strings.HasPrefix(strings.ToLower(cfg.CompileCache.PublicURL), "http://") && cfg.CompileCache.AllowInsecureHTTP {
			zap.L().Warn("compiler-cache bearer credentials are using explicitly enabled plaintext HTTP; restrict access to a trusted LAN/VPN")
		}
	}

	// Sync webhook configs from config.toml to DB
	syncWebhookConfigs(database, cfg.Webhooks)

	// Initialize license manager
	licenseManager := license.NewManagerWithSubmitter(background, cfg.License, database)
	if err := submitBackground("license validator", func(ctx context.Context) {
		licenseManager.Start(ctx)
	}); err != nil {
		return nil, err
	}

	// Phase 5: construct trial + checker BEFORE audit and rules
	// (these modules query IsPro to decide whether to record entries / enforce rules)
	trialManager, err := trial.NewManager(database)
	if err != nil {
		zap.L().Warn("trial.NewManager failed, continuing with nil trial", zap.Error(err))
		// trialManager is nil but NewChecker handles that
	}
	checker := entitlement.NewChecker(licenseManager, trialManager)

	rulesStore := rules.NewStore(database)
	rulesEngine := rules.NewEngine(rulesStore, checker)

	auditLogger := audit.NewLogger(database, checker)
	if err := submitBackground("audit logger", func(ctx context.Context) {
		auditLogger.Start(ctx)
	}); err != nil {
		return nil, err
	}

	// Supply-chain quarantine (T1 Task 1 — minimum release age). Built
	// from cfg.SupplyChain; nil-safe if the operator hasn't configured
	// anything (the default-thresholds map ships pre-populated, so the
	// system protects even an empty config). Failure to build the
	// checker (e.g. an unsupported mode) is a startup error — operators
	// must hear about misconfiguration before the first request.
	quarantinePolicy, err := quarantine.NewPolicy(quarantine.Config{
		MinReleaseAge: cfg.SupplyChain.MinReleaseAge,
		Mode:          cfg.SupplyChain.Mode,
		Allow:         cfg.SupplyChain.Allow,
		FailClosed:    cfg.SupplyChain.FailClosed,
	})
	if err != nil {
		return nil, fmt.Errorf("quarantine policy: %w", err)
	}
	quarantineStore := quarantine.NewStore(database)
	quarantineLookup := quarantine.NewLookup(quarantineStore, resolvers.NewRegistry())
	quarantineChecker, err := quarantine.NewChecker(quarantinePolicy, quarantineLookup, quarantineStore)
	if err != nil {
		return nil, fmt.Errorf("quarantine checker: %w", err)
	}
	requestScope := adapter.NewRequestScope(
		accessRecorder,
		auditLogger,
		quarantine.Wrap(quarantineChecker),
	)

	// Known-malicious blocklist (DIRECTION Task 2) — wired in as the
	// checker's step 0. Enabled by default; the sync scheduler degrades
	// on failure (blocking continues on the last good dataset; no data
	// at all means no malware blocking, never a broken proxy).
	var blocklistStore *blocklist.Store
	var blocklistSyncer *blocklist.Syncer
	blCfg := blocklist.Config{
		Enabled:      cfg.SupplyChain.Blocklist.Enabled,
		SyncInterval: cfg.SupplyChain.Blocklist.SyncInterval,
		MirrorURL:    cfg.SupplyChain.Blocklist.MirrorURL,
		Proxy:        cfg.SupplyChain.Blocklist.Proxy,
	}
	if blCfg.IsEnabled() {
		blocklistStore = blocklist.NewStore(database)
		blocklistSyncer, err = blocklist.NewSyncer(blocklistStore, blCfg)
		if err != nil {
			return nil, fmt.Errorf("blocklist: %w", err)
		}
		quarantineChecker.SetBlocklist(blocklistStore.QuarantineBridge())
		if err := submitBackground("blocklist synchronizer", func(ctx context.Context) {
			blocklistSyncer.Start(ctx)
		}); err != nil {
			return nil, err
		}
	} else {
		zap.L().Info("malicious-package blocklist disabled by config")
	}

	// Tamper detection (DIRECTION T1): first-seen SHA-256 of immutable
	// artifacts. A background-refresh mismatch keeps cached bytes and
	// alerts; an LRU miss can alert but cannot restore evicted bytes.
	// Enabled by default; nil disables persistence, comparison, and alerts.
	var tamperRecorder *tamper.Recorder
	if cfg.SupplyChain.TamperDetection.IsEnabled() {
		tamperRecorder = tamper.NewRecorder(database)
		cacheMgr.SetTamperRecorder(tamperRecorder)
	} else {
		zap.L().Info("tamper detection disabled by config")
	}

	// Webhook notification engine
	webhookNotifier := notify.New(database, background)
	if err := webhookNotifier.LoadConfigs(serverCtx); err != nil {
		zap.L().Warn("failed to load webhook configs", zap.Error(err))
	}
	if err := submitBackground("webhook scheduler", func(ctx context.Context) {
		notify.StartScheduler(ctx, webhookNotifier, notify.SchedulerConfig{
			Pools:   pools,
			Checker: checker,
		})
	}); err != nil {
		return nil, err
	}
	dispatchWebhook := func(event notify.Event) {
		if err := webhookNotifier.Dispatch(event); err != nil &&
			serverCtx.Err() == nil && !errors.Is(err, asyncruntime.ErrClosed) {
			zap.L().Warn("webhook dispatch was not admitted",
				zap.String("event_type", event.Type),
				zap.Error(err),
			)
		}
	}

	// Quarantine → webhook bridge. Installed AFTER the notifier exists
	// so the closure can capture it directly. Done via a setter on the
	// checker so the quarantine package never imports notify — kept
	// loosely coupled per the layering elsewhere. Dispatch is fire-and-forget so the
	// gating decision returns without waiting on webhook delivery; a
	// panicking dispatcher is recovered inside the Checker so misbehaving
	// channels can't cascade into request failures.
	quarantineChecker.SetOnBlock(func(ev db.QuarantineEvent) {
		// Malware blocks page at critical severity — someone in the
		// org just tried to install known malware. Age quarantines
		// stay warnings: a too-young version is an inconvenience.
		if ev.Action == quarantine.ActionMalwareBlocked {
			dispatchWebhook(notify.Event{
				Type:      notify.EventMalwareBlocked,
				Severity:  "critical",
				Title:     fmt.Sprintf("MALWARE blocked: %s %s on %s", ev.Package, ev.Version, ev.Ecosystem),
				Message:   "Known-malicious package version refused (OSV malicious-packages dataset).",
				Detail:    ev.Reason,
				Timestamp: ev.CreatedAt,
			})
			return
		}
		ageStr := formatSeconds(ev.AgeAtCall)
		thresholdStr := formatSeconds(ev.Threshold)
		dispatchWebhook(notify.Event{
			Type:     notify.EventQuarantineBlocked,
			Severity: "warning",
			Title:    fmt.Sprintf("Quarantine: %s %s on %s blocked", ev.Package, ev.Version, ev.Ecosystem),
			Message: fmt.Sprintf(
				"Version published %s ago, below the configured %s minimum release age.",
				ageStr, thresholdStr,
			),
			Detail:    ev.Reason,
			Timestamp: ev.CreatedAt,
		})
	})

	// Tamper → webhook bridge. Same loose coupling as the quarantine
	// bridge: the tamper package never imports notify. Critical
	// severity — a registry swapping bytes under a version is a
	// compromise signal.
	if tamperRecorder != nil {
		tamperRecorder.SetOnTamper(func(ev db.QuarantineEvent) {
			dispatchWebhook(notify.Event{
				Type:      notify.EventTamperDetected,
				Severity:  "critical",
				Title:     fmt.Sprintf("Tamper: %s %s on %s changed upstream", ev.Package, ev.Version, ev.Ecosystem),
				Message:   "An immutable artifact's upstream content changed under the same version. The first-seen bytes are being served; the new content was NOT cached.",
				Detail:    ev.Reason,
				Timestamp: ev.CreatedAt,
			})
		})
	}

	// Security scanner
	secCfg := cfg.Security
	if secCfg.OSVURL == "" {
		secCfg.OSVURL = "https://api.osv.dev"
	}
	if secCfg.ScanInterval == 0 {
		secCfg.ScanInterval = 24 * time.Hour
	}
	if secCfg.CheckTTL == 0 {
		secCfg.CheckTTL = 24 * time.Hour
	}

	osvFetcher = security.NewFetcher(secCfg.OSVURL, secCfg.Proxy)
	securityCatalog, err := security.NewAdvisoryCatalog(database, secCfg.CheckTTL, rulesEngine.InvalidateCache)
	if err != nil {
		return nil, fmt.Errorf("configure security intelligence catalog: %w", err)
	}
	securityScanner = security.NewScanner(database, osvFetcher, securityCatalog)
	resources.closeFetcher = osvFetcher.Close
	resources.securityScanner = securityScanner

	if secCfg.Enabled {
		if err := submitBackground("security scanner", func(ctx context.Context) {
			security.StartBackgroundScan(ctx, securityScanner, secCfg.ScanInterval)
		}); err != nil {
			return nil, err
		}
		zap.L().Info("security vulnerability scanner enabled",
			zap.Duration("scan_interval", secCfg.ScanInterval),
			zap.Duration("check_ttl", secCfg.CheckTTL),
		)
	}

	cacheMgr.SetSecurityScanner(securityScanner)

	if cfg.License.Key == "" {
		zap.L().Info("running as Community edition")
	} else {
		zap.L().Info("license key configured, validation in progress")
	}

	if err := submitBackground("upstream latency cleanup", func(ctx context.Context) {
		upstream.StartLatencyLogCleanup(ctx, database)
	}); err != nil {
		return nil, err
	}
	if err := submitBackground("cache LRU cleanup", func(ctx context.Context) {
		cache.StartLRUCleanup(ctx, cacheRetention, 5*time.Minute)
	}); err != nil {
		return nil, err
	}

	// Setup Gin
	r.Use(middleware.Recovery())
	r.Use(middleware.Logger())
	r.Use(rules.Middleware(rulesEngine))
	r.Use(middleware.ProjectTokenMiddleware(database))

	// Build ordered ecosystem name list (defines UI iteration order)
	ecosystemNames := make([]string, 0, len(activeDefs))
	for _, eco := range activeDefs {
		ecosystemNames = append(ecosystemNames, eco.name)
	}
	if len(cfg.Docker.Registries) > 0 {
		ecosystemNames = append(ecosystemNames, "docker")
	}

	// Register all API routes
	indexRefresher := api.NewCacheIndexRefresher(r, cfg.ExtraIndexes, cfg.Docker)
	api.RegisterRoutes(r, api.Deps{
		LifecycleContext: serverCtx,
		DB:               database,
		Storage:          storage,
		Config:           cfg,
		ConfigStore:      settingsStore,
		Pools:            pools,
		UpstreamRegistry: registry,
		Ecosystems:       ecosystemNames,
		CacheMgr:         cacheMgr,
		CacheRetention:   cacheRetention,
		CompileCache:     compileCacheService,
		CompileCacheAuth: compileCacheAuthorizer,
		IndexRefresher:   indexRefresher,
		EventBus:         eventBus,
		LicenseManager:   licenseManager,
		TrialManager:     trialManager,
		Entitlement:      checker,
		RulesStore:       rulesStore,
		RulesEngine:      rulesEngine,
		SecurityScanner:  securityScanner,
		SecurityCatalog:  securityCatalog,
		WebhookNotifier:  webhookNotifier,
		QuarantineStore:  quarantineStore,
		BlocklistStore:   blocklistStore,
		BlocklistSyncer:  blocklistSyncer,
		Tasks:            background,
	})

	// Project-scoped proxy routes (/p/:slug/...) share Registry-owned Pools
	// with their standard counterparts, so runtime mutations are immediate.
	projectGroup := r.Group("/p/:slug")
	projectGroup.Use(middleware.ProjectSlugMiddleware(database))
	if err := registerActiveAdapters(r, projectGroup, activeDefs, pools, cacheMgr, cfg.Cache, database); err != nil {
		return nil, err
	}

	if len(cfg.Docker.Registries) > 0 {
		dockerHandler := dockeradapter.New(cacheMgr, cfg.Cache, database, cfg.Docker)
		dockerGroup := r.Group("/v2")
		dockerHandler.Register(dockerGroup)
		zap.L().Info("docker registry proxy enabled",
			zap.Int("registries", len(cfg.Docker.Registries)),
			zap.String("default", cfg.Docker.DefaultRegistry),
		)
	}

	// Register extra PyPI-compatible indexes
	for _, idx := range cfg.ExtraIndexes {
		idxPool, err := upstream.NewPool(idx.Upstreams)
		if err != nil {
			return nil, fmt.Errorf("create extra index %s pool: %w", idx.Name, err)
		}
		syncConfigOwnedUpstreams(database, "extra:"+idx.Name, idx.Upstreams)
		upstream.RestoreFromDB(idxPool, database)
		if err := submitBackground("extra index "+idx.Name+" health check", func(ctx context.Context) {
			upstream.StartHealthCheck(ctx, idxPool, database, upstream.DefaultProbeInterval)
		}); err != nil {
			return nil, err
		}

		idxHandler := pypi.NewWithPrefix(cacheMgr, upstream.NewPrioritySelector(idxPool), cfg.Cache, database, "/"+idx.Path, "extra:"+idx.Name)
		idxHandler.Register(r.Group("/" + idx.Path))
		idxHandler.Register(projectGroup.Group("/" + idx.Path))

		zap.L().Info("extra index registered",
			zap.String("name", idx.Name),
			zap.String("path", "/"+idx.Path),
			zap.Int("upstreams", len(idx.Upstreams)),
		)
	}

	// Serve embedded frontend (SPA fallback).
	if err := registerFrontend(r); err != nil {
		return nil, err
	}

	// Start router-driven maintenance only after every API, proxy and frontend
	// route has been registered. Gin's route tree is not safe to mutate while
	// the producer is already serving internal refresh requests.
	if cfg.UpstreamUpdates.IsEnabled() {
		interval, enabled, err := config.ParseUpdateCheckInterval(cfg.UpstreamUpdates.CheckInterval)
		if err != nil {
			return nil, fmt.Errorf("configure upstream metadata updates: %w", err)
		}
		if enabled {
			producer, err := upstreamupdates.New(database, interval, indexRefresher)
			if err != nil {
				return nil, fmt.Errorf("build upstream metadata producer: %w", err)
			}
			if err := submitBackground("upstream metadata producer", producer.Run); err != nil {
				return nil, err
			}
			zap.L().Info("proactive upstream metadata updates enabled", zap.Duration("check_interval", interval))
		}
	} else {
		zap.L().Info("proactive upstream metadata updates disabled by config")
	}

	// Create and start HTTP server
	srv := &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	lifecycle := registerServerLifecycle(srv, resources)
	srv.Handler = lifecycle.track(requestScope.Wrap(r))
	if err := shutdownTrigger.Err(); err != nil {
		return nil, fmt.Errorf("start server: %w", err)
	}
	registry.Start(serverCtx)

	go func() {
		zap.L().Info("starting server", zap.String("addr", addr))
		serveHTTP(srv, listener)
	}()
	started = true
	if shutdownTrigger.Done() != nil {
		go shutdownWhenContextEnds(shutdownTrigger, serverCtx, srv)
	}

	return srv, nil
}

func localStoragePathsOverlap(left, right string) (bool, error) {
	leftAbsolute, err := canonicalStoragePath(left)
	if err != nil {
		return false, err
	}
	rightAbsolute, err := canonicalStoragePath(right)
	if err != nil {
		return false, err
	}
	contains := func(parent, child string) (bool, error) {
		relative, err := filepath.Rel(parent, child)
		if err != nil {
			return false, err
		}
		return relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)), nil
	}
	leftContainsRight, err := contains(leftAbsolute, rightAbsolute)
	if err != nil {
		return false, err
	}
	rightContainsLeft, err := contains(rightAbsolute, leftAbsolute)
	if err != nil {
		return false, err
	}
	return leftContainsRight || rightContainsLeft, nil
}

// canonicalStoragePath resolves every existing symlink component, including
// when the final cache directory has not been created yet. This prevents two
// visually different configured paths from aliasing the same storage root.
func canonicalStoragePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absolute)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func newServerRuntimeContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(context.WithoutCancel(parent))
}

func shutdownWhenContextEnds(trigger, serverCtx context.Context, srv *http.Server) {
	select {
	case <-trigger.Done():
		if err := Shutdown(context.Background(), srv); err != nil {
			zap.L().Error("server shutdown after context cancellation failed", zap.Error(err))
		}
	case <-serverCtx.Done():
		// An explicit Shutdown already reached the resource owner.
	}
}

// formatSeconds renders a seconds-count as a coarse human duration —
// "3d 2h" / "12h" / "30m" / "45s" — for inclusion in webhook
// payloads. The Notifier consumers (Slack / DingTalk / WeCom /
// Feishu) all render plain text in their card bodies so we choose
// a format that reads cleanly there rather than the more precise
// time.Duration default which surfaces as e.g. "264h12m30s".
func formatSeconds(secs int64) string {
	if secs <= 0 {
		return "0s"
	}
	days := secs / 86400
	hours := (secs % 86400) / 3600
	minutes := (secs % 3600) / 60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// syncConfigOwnedUpstreams persists extra-index sources, which remain outside
// the dynamic standard-ecosystem Registry.
