package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
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
	"depsilo/internal/backup"
	"depsilo/internal/blocklist"
	"depsilo/internal/cache"
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
	"depsilo/internal/version"
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
	// Gin defaults to debug mode when GIN_MODE is absent, which prints every
	// registered route before the useful startup summary. Production starts use
	// quiet release mode by default while preserving an explicit GIN_MODE=debug.
	if gin.Mode() == gin.DebugMode && strings.TrimSpace(os.Getenv(gin.EnvGinMode)) == "" {
		gin.SetMode(gin.ReleaseMode)
	}
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
	databaseLease, err := backup.HoldDatabase(cfg.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("acquire database runtime lease: %w", err)
	}
	database, err := db.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		_ = databaseLease.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		_ = databaseLease.Close()
		return nil, fmt.Errorf("access database pool: %w", err)
	}
	resources.closeDatabase = newAsyncCloseAdapter(func() error {
		return errors.Join(sqlDatabase.Close(), databaseLease.Close())
	})
	if err := db.AutoMigrate(database); err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	seedDefinitions := standardEcosystemDefinitions(cfg, nil)
	bootstrap, err := upstream.ReconcileBootstrap(database, seedSources(seedDefinitions))
	if err != nil {
		return nil, fmt.Errorf("reconcile upstream control plane: %w", err)
	}
	npmTarballSigningKey, err := deriveActiveNPMTarballSigningKey(cfg.Auth.JWTSecret, bootstrap.ActiveEcosystems)
	if err != nil {
		return nil, fmt.Errorf("configure npm tarball provenance: %w", err)
	}
	definitions := standardEcosystemDefinitions(cfg, npmTarballSigningKey)
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

	// A config file is considered published only alongside a durable
	// administrator. Reconcile old/interrupted states before the headless
	// bootstrap path so a missing administrator reopens recoverable setup
	// instead of creating an inaccessible random account.
	if err := api.ReconcileSetupState(database, cfg); err != nil {
		return nil, fmt.Errorf("reconcile initial administrator: %w", err)
	}
	// Interactive first-run setup creates the administrator from wizard input.
	// Configured/headless deployments with explicit environment credentials
	// retain their bootstrap behavior.
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
	retiredReclaim, err := cacheRetention.ReclaimRetired(serverCtx, db.RetiredHuggingFaceAdapterType)
	if err != nil {
		return nil, fmt.Errorf("reclaim retired Hugging Face cache entries before startup: %w", err)
	}
	if retiredReclaim.Removed > 0 {
		zap.L().Info("reclaimed retired Hugging Face cache entries before startup",
			zap.Int("removed", retiredReclaim.Removed),
			zap.Int64("reclaimed_bytes", retiredReclaim.ReclaimedBytes),
		)
	}

	compilerCache, err := openCompileCacheRuntime(serverCtx, cfg.Storage, cfg.CompileCache, database)
	if err != nil {
		return nil, err
	}
	resources.compileCache = compilerCache

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
	policyOnLoadError, err := rules.ParseOnLoadErrorPolicy(cfg.Policy.OnLoadError)
	if err != nil {
		return nil, fmt.Errorf("policy configuration: %w", err)
	}
	rulesEngine, err := rules.NewEngineWithOptions(
		rulesStore,
		checker,
		rules.WithOnLoadErrorPolicy(policyOnLoadError),
		rules.WithPolicyTelemetry(api.M),
	)
	if err != nil {
		return nil, fmt.Errorf("initialize package policy engine: %w", err)
	}
	api.M.BindPolicyStatusProvider(rulesEngine)

	auditLogger := audit.NewLogger(database)
	if err := submitBackground("audit logger", func(ctx context.Context) {
		auditLogger.Start(ctx)
	}); err != nil {
		return nil, err
	}

	// Supply-chain quarantine (T1 Task 1 — minimum release age). The age
	// gate defaults off for an empty config; explicit legacy threshold tables
	// remain enabled unless the operator sets the new switch false. Failure to
	// build the checker (e.g. an unsupported mode) is a startup error.
	quarantinePolicy, err := quarantine.NewPolicy(quarantine.Config{
		MinReleaseAgeEnabled: cfg.SupplyChain.MinReleaseAgeEnabled,
		MinReleaseAge:        cfg.SupplyChain.MinReleaseAge,
		Mode:                 cfg.SupplyChain.Mode,
		Allow:                cfg.SupplyChain.Allow,
		FailClosed:           cfg.SupplyChain.FailClosed,
	})
	if err != nil {
		return nil, fmt.Errorf("quarantine policy: %w", err)
	}
	if quarantinePolicy.HasActiveThresholds() {
		zap.L().Info("minimum release age quarantine enabled")
	} else if quarantinePolicy.IsAgeGateEnabled() {
		zap.L().Warn("minimum release age quarantine requested but inactive: no source-bound ecosystem thresholds are available")
	} else {
		zap.L().Info("minimum release age quarantine disabled")
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
		rules.Wrap(rulesEngine),
		api.M,
	)

	// Known-malicious blocklist (DIRECTION Task 2) — wired in as the
	// checker's step 0. Enabled by default; the sync scheduler degrades
	// on failure (blocking continues on the last good dataset; no data
	// at all means no malware blocking, never a broken proxy).
	var blocklistStore *blocklist.Store
	var blocklistSyncer *blocklist.Syncer
	blocklistMode := quarantine.ModeBlock
	blCfg := blocklist.Config{
		Enabled:      cfg.SupplyChain.Blocklist.Enabled,
		SyncInterval: cfg.SupplyChain.Blocklist.SyncInterval,
		MirrorURL:    cfg.SupplyChain.Blocklist.MirrorURL,
		Proxy:        cfg.SupplyChain.Blocklist.Proxy,
		Mode:         cfg.SupplyChain.Blocklist.Mode,
	}
	if blCfg.IsEnabled() {
		switch blCfg.Mode {
		case "", "block":
			blocklistMode = quarantine.ModeBlock
		case "warn":
			blocklistMode = quarantine.ModeWarn
		default:
			return nil, fmt.Errorf("blocklist: unsupported mode %q (want block | warn)", blCfg.Mode)
		}
		blocklistStore = blocklist.NewStore(database)
		blocklistSyncer, err = blocklist.NewSyncer(blocklistStore, blCfg)
		if err != nil {
			return nil, fmt.Errorf("blocklist: %w", err)
		}
		quarantineChecker.SetBlocklist(blocklistStore.QuarantineBridge())
		quarantineChecker.SetBlocklistMode(blocklistMode)
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
	securityCatalog, err := security.NewAdvisoryCatalog(database, secCfg.CheckTTL)
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
	if err := submitBackground("cache metrics", func(ctx context.Context) {
		runCacheMetrics(ctx, cacheMgr, time.Minute)
	}); err != nil {
		return nil, err
	}

	// Setup Gin
	extraPackageRuleRoutes := make([]rules.PyPIRouteDescriptor, 0, len(cfg.ExtraIndexes))
	for index := range cfg.ExtraIndexes {
		descriptor, err := rules.NewPyPIRouteDescriptor(
			cfg.ExtraIndexes[index].Path,
			cfg.ExtraIndexes[index].Kind == config.ExtraIndexKindPyTorch,
		)
		if err != nil {
			return nil, fmt.Errorf("configure extra index %s package-policy route: %w", cfg.ExtraIndexes[index].Name, err)
		}
		extraPackageRuleRoutes = append(extraPackageRuleRoutes, descriptor)
	}
	r.Use(middleware.Recovery())
	r.Use(middleware.Logger())
	r.Use(rules.Middleware(rulesEngine, extraPackageRuleRoutes...))
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
		LifecycleContext:           serverCtx,
		DB:                         database,
		Storage:                    storage,
		Config:                     cfg,
		ConfigStore:                settingsStore,
		Pools:                      pools,
		UpstreamRegistry:           registry,
		Ecosystems:                 ecosystemNames,
		CacheMgr:                   cacheMgr,
		CacheRetention:             cacheRetention,
		CompileCache:               compilerCache.handlerDependencies(),
		IndexRefresher:             indexRefresher,
		EventBus:                   eventBus,
		LicenseManager:             licenseManager,
		TrialManager:               trialManager,
		Entitlement:                checker,
		RulesStore:                 rulesStore,
		RulesEngine:                rulesEngine,
		PolicyStatusProvider:       rulesEngine,
		SecurityScanner:            securityScanner,
		SecurityCatalog:            securityCatalog,
		WebhookNotifier:            webhookNotifier,
		QuarantineStore:            quarantineStore,
		QuarantineApprovalsEnabled: quarantinePolicy.HasActiveThresholds(),
		BlocklistStore:             blocklistStore,
		BlocklistSyncer:            blocklistSyncer,
		BlocklistMode:              string(blocklistMode),
		Tasks:                      background,
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

	// Register extra PyPI-compatible indexes. One domain-separated key signs
	// only artifact URLs that were declared by a fetched index page; binding the
	// MAC to each adapter ID prevents references from being replayed across
	// routes with different cache or egress policy.
	var extraIndexArtifactKey []byte
	if len(cfg.ExtraIndexes) > 0 {
		extraIndexArtifactKey, err = derivePyPIArtifactSigningKey(cfg.Auth.JWTSecret)
		if err != nil {
			return nil, err
		}
	}
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

		options := pypi.Options{
			PathPrefix:         "/" + idx.Path,
			AdapterID:          "extra:" + idx.Name,
			UpstreamSimplePath: idx.SimplePath,
			ArtifactSigningKey: extraIndexArtifactKey,
			ArtifactSelector:   upstream.NewEgressSelector(idxPool),
		}
		if idx.Kind == config.ExtraIndexKindPyTorch {
			idxHandler, err := pypi.NewChannelFamily(
				cacheMgr,
				upstream.NewPassiveRecoverySelector(idxPool),
				cfg.Cache,
				database,
				options,
			)
			if err != nil {
				return nil, fmt.Errorf("create extra index %s channel adapter: %w", idx.Name, err)
			}
			idxHandler.Register(r.Group("/" + idx.Path))
			idxHandler.Register(projectGroup.Group("/" + idx.Path))
		} else {
			idxHandler, err := pypi.NewWithOptions(
				cacheMgr,
				upstream.NewPassiveRecoverySelector(idxPool),
				cfg.Cache,
				database,
				options,
			)
			if err != nil {
				return nil, fmt.Errorf("create extra index %s adapter: %w", idx.Name, err)
			}
			idxHandler.Register(r.Group("/" + idx.Path))
			idxHandler.Register(projectGroup.Group("/" + idx.Path))
		}

		zap.L().Info("extra index registered",
			zap.String("name", idx.Name),
			zap.String("kind", idx.Kind),
			zap.String("path", "/"+idx.Path),
			zap.Int("upstreams", len(idx.Upstreams)),
		)
	}

	// Serve embedded frontend (SPA fallback).
	extraProxyPrefixes := make([]string, 0, len(cfg.ExtraIndexes))
	for _, idx := range cfg.ExtraIndexes {
		extraProxyPrefixes = append(extraProxyPrefixes, idx.Path)
	}
	if err := registerFrontend(r, extraProxyPrefixes...); err != nil {
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

	go serveHTTP(srv, listener)
	started = true
	if shutdownTrigger.Done() != nil {
		go shutdownWhenContextEnds(shutdownTrigger, serverCtx, srv)
	}

	summary := newStartupSummary(
		version.Version,
		cfg.Server.Host,
		listenerPort(listener.Addr(), cfg.Server.Port),
		cfg.IsDefault,
		cfg.BootstrapToken,
		cfg.BootstrapTokenGenerated,
	)
	zap.L().Info("server ready",
		zap.String("addr", listener.Addr().String()),
		zap.String("portal_url", summary.PortalURL),
		zap.Bool("setup_required", summary.SetupRequired),
	)
	if err := writeStartupSummary(os.Stderr, summary); err != nil {
		zap.L().Warn("failed to write startup summary", zap.Error(err))
	}

	return srv, nil
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
