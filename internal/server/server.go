package server

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/accesslog"
	"depsilo/internal/adapter"
	"depsilo/internal/adapter/alpine"
	"depsilo/internal/adapter/apt"
	"depsilo/internal/adapter/cargo"
	"depsilo/internal/adapter/composer"
	"depsilo/internal/adapter/conda"
	"depsilo/internal/adapter/cran"
	dockeradapter "depsilo/internal/adapter/docker"
	"depsilo/internal/adapter/goproxy"
	"depsilo/internal/adapter/helm"
	"depsilo/internal/adapter/huggingface"
	"depsilo/internal/adapter/maven"
	"depsilo/internal/adapter/npm"
	"depsilo/internal/adapter/nuget"
	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/adapter/pypi"
	"depsilo/internal/adapter/rubygems"
	"depsilo/internal/api"
	"depsilo/internal/audit"
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
	web "depsilo/web"
)

// StartServer initializes all components and starts the HTTP server.
// Returns the *http.Server for lifecycle control by the caller.
// The provided context controls background goroutine shutdown.
func StartServer(ctx context.Context) (*http.Server, error) {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	zap.L().Info("config loaded",
		zap.String("db_driver", cfg.Database.Driver),
		zap.String("storage_type", cfg.Storage.Type),
	)

	// Initialize database
	database, err := db.Open(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.AutoMigrate(database); err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	// Create default admin user if none exists
	api.EnsureDefaultAdmin(database)

	// Backfill PackageName for existing cache entries
	backfillPackageNames(database)

	// Access log rollup pipeline: one-shot backfill from access_logs into
	// the rollup tables (no-op if already populated), then start the
	// async batched recorder so future requests stream through it. The
	// adapter's LogAccess switches to recorder.Record once SetRecorder
	// runs; until then it writes raw rows synchronously.
	if cfg.AccessLog.BackfillOnStart {
		if err := accesslog.BackfillIfEmpty(ctx, database); err != nil {
			zap.L().Warn("access log rollup backfill failed", zap.Error(err))
		}
	}
	accessRecorder := accesslog.NewRecorder(database, accesslog.Config{
		Enabled:       cfg.AccessLog.RollupEnabled,
		BatchSize:     cfg.AccessLog.BatchSize,
		BatchInterval: cfg.AccessLog.BatchInterval,
	})
	adapter.SetRecorder(accessRecorder)
	go accesslog.StartCompactor(ctx, database)
	go accesslog.StartRetention(ctx, database, accesslog.RetentionConfig{
		RawDays:    cfg.AccessLog.RetentionDays,
		RollupDays: cfg.AccessLog.RollupRetentionDays,
	})

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
	// ttl >= ttl_blob selects exactly the blobs. Deriving from ttl_blob
	// (rather than a fixed 1h) keeps metadata OUT of tamper tracking for
	// any ttl_index < ttl_blob. An explicit config override wins.
	immutableThreshold := cfg.SupplyChain.TamperDetection.ImmutableThresholdOverride()
	if immutableThreshold <= 0 {
		immutableThreshold = cfg.Cache.TTLBlob
	}
	if cfg.SupplyChain.TamperDetection.IsEnabled() && cfg.Cache.TTLIndex >= immutableThreshold {
		zap.L().Warn("tamper detection: ttl_index >= immutable threshold — index metadata may be misclassified as immutable and false-alarm; lower ttl_index or raise the threshold",
			zap.Duration("ttl_index", cfg.Cache.TTLIndex),
			zap.Duration("immutable_threshold", immutableThreshold))
	}
	cacheMgr := cache.NewManager(storage, database, eventBus, immutableThreshold)

	// Ecosystem definitions: name, route path, and upstream config
	type ecosystemDef struct {
		name      string
		route     string
		upstreams []config.UpstreamConfig
	}
	ecosystems := []ecosystemDef{
		{"pypi", "/pypi", cfg.PyPI.Upstreams},
		{"apt", "/apt", cfg.APT.Upstreams},
		{"npm", "/npm", cfg.NPM.Upstreams},
		{"go", "/go", cfg.Go.Upstreams},
		{"cargo", "/crates", cfg.Cargo.Upstreams},
		{"maven", "/maven", cfg.Maven.Upstreams},
		{"rubygems", "/rubygems", cfg.RubyGems.Upstreams},
		{"composer", "/composer", cfg.Composer.Upstreams},
		{"nuget", "/nuget", cfg.NuGet.Upstreams},
		{"conda", "/conda", cfg.Conda.Upstreams},
		{"cran", "/cran", cfg.CRAN.Upstreams},
		{"alpine", "/alpine", cfg.Alpine.Upstreams},
		{"helm", "/helm", cfg.Helm.Upstreams},
		{"huggingface", "/huggingface", cfg.HuggingFace.Upstreams},
	}

	// Sync upstreams and create pools
	pools := make(map[string]*upstream.Pool, len(ecosystems))
	for _, eco := range ecosystems {
		syncUpstreams(database, eco.name, eco.upstreams)
		pool, err := upstream.NewPool(eco.upstreams)
		if err != nil {
			return nil, fmt.Errorf("create %s pool: %w", eco.name, err)
		}
		pools[eco.name] = pool
	}

	// Sync webhook configs from config.toml to DB
	syncWebhookConfigs(database, cfg.Webhooks)

	// Initialize license manager
	licenseManager := license.NewManager(cfg.License, database)
	go licenseManager.Start(ctx)

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
	go auditLogger.Start(ctx)
	adapter.SetAuditLogger(auditLogger)

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
	adapter.SetQuarantineChecker(quarantine.Wrap(quarantineChecker))

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
		go blocklistSyncer.Start(ctx)
	} else {
		zap.L().Info("malicious-package blocklist disabled by config")
	}

	// Tamper detection (DIRECTION T1): first-seen SHA-256 of immutable
	// artifacts; a re-fetch whose hash differs keeps the trusted bytes
	// and alerts. Enabled by default; nil recorder = fully off.
	var tamperRecorder *tamper.Recorder
	if cfg.SupplyChain.TamperDetection.IsEnabled() {
		tamperRecorder = tamper.NewRecorder(database)
		cacheMgr.SetTamperRecorder(tamperRecorder)
	} else {
		zap.L().Info("tamper detection disabled by config")
	}

	// Webhook notification engine
	webhookNotifier := notify.New(database)
	if err := webhookNotifier.LoadConfigs(); err != nil {
		zap.L().Warn("failed to load webhook configs", zap.Error(err))
	}
	go notify.StartScheduler(ctx, webhookNotifier, notify.SchedulerConfig{
		Pools:   pools,
		Checker: checker,
	})

	// Quarantine → webhook bridge. Installed AFTER the notifier exists
	// so the closure can capture it directly. Done via a setter on the
	// checker so the quarantine package never imports notify — kept
	// loosely coupled per the layering elsewhere (adapter.SetAuditLogger
	// follows the same pattern). Dispatch is fire-and-forget so the
	// gating decision returns without waiting on webhook delivery; a
	// panicking dispatcher is recovered inside the Checker so misbehaving
	// channels can't cascade into request failures.
	quarantineChecker.SetOnBlock(func(ev db.QuarantineEvent) {
		// Malware blocks page at critical severity — someone in the
		// org just tried to install known malware. Age quarantines
		// stay warnings: a too-young version is an inconvenience.
		if ev.Action == quarantine.ActionMalwareBlocked {
			webhookNotifier.Dispatch(ctx, notify.Event{
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
		webhookNotifier.Dispatch(ctx, notify.Event{
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
			webhookNotifier.Dispatch(ctx, notify.Event{
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

	osvFetcher := security.NewFetcher(secCfg.OSVURL, secCfg.Proxy)
	securityScanner := security.NewScanner(database, osvFetcher, secCfg.CheckTTL)
	securityImporter := security.NewImporter(database)

	if secCfg.Enabled {
		go security.StartBackgroundScan(ctx, securityScanner, secCfg.ScanInterval)
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

	// Restore latency metrics from DB before starting health checks
	allPools := make([]*upstream.Pool, 0, len(pools))
	for _, eco := range ecosystems {
		allPools = append(allPools, pools[eco.name])
	}
	for _, pool := range allPools {
		upstream.RestoreFromDB(pool, database)
	}
	for _, pool := range allPools {
		go upstream.StartHealthCheck(ctx, pool, database, upstream.DefaultProbeInterval)
	}
	go upstream.StartLatencyLogCleanup(ctx, database)
	go cache.StartLRUCleanup(ctx, storage, database, cfg.Cache.MaxSizeGB, cfg.Cache.LRUThreshold, 5*time.Minute)

	// Setup Gin
	r := gin.New()
	r.Use(middleware.Recovery())
	r.Use(middleware.Logger())
	r.Use(rules.Middleware(rulesEngine))
	r.Use(middleware.ProjectTokenMiddleware(database))

	// Build ordered ecosystem name list (defines UI iteration order)
	ecosystemNames := make([]string, 0, len(ecosystems))
	for _, eco := range ecosystems {
		ecosystemNames = append(ecosystemNames, eco.name)
	}

	// Register all API routes
	api.RegisterRoutes(r, api.Deps{
		DB:               database,
		Storage:          storage,
		Config:           cfg,
		Pools:            pools,
		Ecosystems:       ecosystemNames,
		CacheMgr:         cacheMgr,
		EventBus:         eventBus,
		LicenseManager:   licenseManager,
		TrialManager:     trialManager,
		Entitlement:      checker,
		AuditLogger:      auditLogger,
		RulesStore:       rulesStore,
		RulesEngine:      rulesEngine,
		SecurityScanner:  securityScanner,
		SecurityImporter: securityImporter,
		WebhookNotifier:  webhookNotifier,
		QuarantineStore:  quarantineStore,
		BlocklistStore:   blocklistStore,
		BlocklistSyncer:  blocklistSyncer,
	})

	// Register adapter handlers
	type adapterFactory func(*cache.Manager, upstream.Selector, config.CacheConfig, *gorm.DB) adapter.Adapter

	adapterFactories := map[string]adapterFactory{
		"pypi": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return pypi.New(cm, s, cc, db)
		},
		"apt": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return apt.New(cm, s, cc, db)
		},
		"npm": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return npm.New(cm, s, cc, db)
		},
		"go": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return goproxy.New(cm, s, cc, db)
		},
		"cargo": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return cargo.New(cm, s, cc, db)
		},
		"maven": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return maven.New(cm, s, cc, db)
		},
		"rubygems": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return rubygems.New(cm, s, cc, db)
		},
		"composer": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return composer.New(cm, s, cc, db)
		},
		"nuget": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return nuget.New(cm, s, cc, db)
		},
		"conda": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return conda.New(cm, s, cc, db)
		},
		"cran": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return cran.New(cm, s, cc, db)
		},
		"alpine": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return alpine.New(cm, s, cc, db)
		},
		"helm": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return helm.New(cm, s, cc, db)
		},
		"huggingface": func(cm *cache.Manager, s upstream.Selector, cc config.CacheConfig, db *gorm.DB) adapter.Adapter {
			return huggingface.New(cm, s, cc, db)
		},
	}

	handlers := make(map[string]adapter.Adapter, len(ecosystems))
	for _, eco := range ecosystems {
		factory := adapterFactories[eco.name]
		h := factory(cacheMgr, upstream.NewPrioritySelector(pools[eco.name]), cfg.Cache, database)
		h.Register(r.Group(eco.route))
		handlers[eco.name] = h
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

	// Project-scoped proxy routes (/p/:slug/...)
	projectGroup := r.Group("/p/:slug")
	projectGroup.Use(middleware.ProjectSlugMiddleware(database))
	// Re-register all adapter handlers under the project group
	for _, eco := range ecosystems {
		handlers[eco.name].Register(projectGroup.Group(eco.route))
	}

	// Register extra PyPI-compatible indexes
	for _, idx := range cfg.ExtraIndexes {
		idxPool, err := upstream.NewPool(idx.Upstreams)
		if err != nil {
			return nil, fmt.Errorf("create extra index %s pool: %w", idx.Name, err)
		}
		syncUpstreams(database, "extra:"+idx.Name, idx.Upstreams)
		upstream.RestoreFromDB(idxPool, database)
		go upstream.StartHealthCheck(ctx, idxPool, database, upstream.DefaultProbeInterval)

		idxHandler := pypi.NewWithPrefix(cacheMgr, upstream.NewPrioritySelector(idxPool), cfg.Cache, database, "/"+idx.Path, "extra:"+idx.Name)
		idxHandler.Register(r.Group("/" + idx.Path))
		idxHandler.Register(projectGroup.Group("/" + idx.Path))

		zap.L().Info("extra index registered",
			zap.String("name", idx.Name),
			zap.String("path", "/"+idx.Path),
			zap.Int("upstreams", len(idx.Upstreams)),
		)
	}

	// Serve embedded frontend (SPA fallback)
	distFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		return nil, fmt.Errorf("load embedded frontend: %w", err)
	}
	staticHandler := http.FileServer(http.FS(distFS))

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/" || strings.HasPrefix(path, "/assets") {
			staticHandler.ServeHTTP(c.Writer, c.Request)
			return
		}
		c.Request.URL.Path = "/"
		staticHandler.ServeHTTP(c.Writer, c.Request)
	})

	// Create and start HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// Flush the access-log recorder during graceful shutdown so the last
	// in-memory batch lands in SQLite instead of being lost. RegisterOnShutdown
	// callbacks run inside Server.Shutdown() before it returns, exactly the
	// hook we want. A 5s cap keeps a stuck flush from blocking shutdown
	// indefinitely.
	srv.RegisterOnShutdown(func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := accessRecorder.Close(flushCtx); err != nil {
			zap.L().Warn("access log recorder close failed", zap.Error(err))
		}
	})

	go func() {
		zap.L().Info("starting server", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zap.L().Fatal("server failed", zap.Error(err))
		}
	}()

	return srv, nil
}

// backfillPackageNames updates existing cache entries that have an empty PackageName.
func backfillPackageNames(database *gorm.DB) {
	var entries []db.CacheEntry
	database.Where("package_name = '' OR package_name IS NULL").Find(&entries)
	if len(entries) == 0 {
		return
	}
	zap.L().Info("backfilling package names", zap.Int("count", len(entries)))
	for _, e := range entries {
		name := packagekey.ExtractName(e.AdapterType, e.Key)
		if name != "" {
			database.Model(&e).Update("package_name", name)
		}
	}
	zap.L().Info("package name backfill complete")
}

// syncWebhookConfigs ensures configured webhooks from config.toml exist in the database.
func syncWebhookConfigs(database *gorm.DB, webhooks []config.WebhookConfig) {
	for _, w := range webhooks {
		var record db.WebhookConfig
		result := database.Where("url = ? AND platform = ?", w.URL, w.Platform).First(&record)
		if result.Error == gorm.ErrRecordNotFound {
			database.Create(&db.WebhookConfig{
				Name:     w.Name,
				Platform: w.Platform,
				URL:      w.URL,
				Events:   w.Events,
				Enabled:  w.Enabled,
			})
			zap.L().Info("synced webhook config from config.toml", zap.String("name", w.Name))
		}
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

// syncUpstreams ensures configured upstreams exist in the database.
func syncUpstreams(database *gorm.DB, adapterType string, upstreams []config.UpstreamConfig) {
	for _, u := range upstreams {
		var record db.UpstreamRecord
		result := database.Where("name = ? AND adapter_type = ?", u.Name, adapterType).First(&record)
		if result.Error == gorm.ErrRecordNotFound {
			record = db.UpstreamRecord{
				AdapterType: adapterType,
				Name:        u.Name,
				URL:         u.URL,
				Proxy:       u.Proxy,
				Priority:    u.Priority,
				Healthy:     true,
				SuccessRate: 1.0,
			}
			if err := database.Create(&record).Error; err != nil {
				zap.L().Warn("failed to sync upstream to db", zap.String("name", u.Name), zap.Error(err))
			} else {
				zap.L().Info("synced upstream to db", zap.String("name", u.Name), zap.String("type", adapterType))
			}
		} else if result.Error == nil {
			database.Model(&record).Updates(map[string]interface{}{
				"url":      u.URL,
				"proxy":    u.Proxy,
				"priority": u.Priority,
			})
		}
	}
}
