package upstream

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"depsilo/internal/config"
	dbmodel "depsilo/internal/db"
	"gorm.io/gorm"
)

func bootstrapDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := dbmodel.Open("sqlite", filepath.Join(t.TempDir(), "depsilo.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := dbmodel.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	return database
}

func source(name string, upstreams ...config.UpstreamConfig) SeedSource {
	return SeedSource{Ecosystem: name, Upstreams: upstreams}
}

func TestReconcileBootstrap_FirstSeedMergesLegacyRowsAndWritesBothStates(t *testing.T) {
	database := bootstrapDB(t)
	legacy := dbmodel.UpstreamRecord{AdapterType: "pypi", Name: "legacy", URL: "https://legacy.example", Priority: 1}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}

	got, err := ReconcileBootstrap(database, []SeedSource{
		source("pypi", config.UpstreamConfig{Name: "legacy", URL: "https://config-must-not-overwrite.example", Priority: 9}, config.UpstreamConfig{Name: "fallback", URL: "https://fallback.example", Priority: 2}),
		source("npm", config.UpstreamConfig{Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pypi", "npm"}; !reflect.DeepEqual(got.ActiveEcosystems, want) {
		t.Fatalf("active=%v want=%v", got.ActiveEcosystems, want)
	}

	var rows []dbmodel.UpstreamRecord
	if err := database.Order("adapter_type, name").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d want=3", len(rows))
	}
	var persistedLegacy dbmodel.UpstreamRecord
	if err := database.Where("adapter_type = ? AND name = ?", "pypi", "legacy").First(&persistedLegacy).Error; err != nil {
		t.Fatal(err)
	}
	if persistedLegacy.URL != "https://legacy.example" || persistedLegacy.Priority != 1 {
		t.Fatalf("legacy row overwritten: %#v", persistedLegacy)
	}

	var marker, activeState dbmodel.ControlPlaneState
	if err := database.First(&marker, "key = ?", SeedMarkerKey).Error; err != nil {
		t.Fatal(err)
	}
	if marker.Value != "true" {
		t.Fatalf("marker=%q", marker.Value)
	}
	if err := database.First(&activeState, "key = ?", ActiveEcosystemsKey).Error; err != nil {
		t.Fatal(err)
	}
	var stored []string
	if err := json.Unmarshal([]byte(activeState.Value), &stored); err != nil {
		t.Fatal(err)
	}
	if want := []string{"pypi", "npm"}; !reflect.DeepEqual(stored, want) {
		t.Fatalf("stored=%v want=%v", stored, want)
	}
}

func TestReconcileBootstrap_SeededRestartDoesNotRestoreDeletedActiveRow(t *testing.T) {
	database := bootstrapDB(t)
	sources := []SeedSource{source("pypi",
		config.UpstreamConfig{Name: "primary", URL: "https://one.example", Priority: 1},
		config.UpstreamConfig{Name: "secondary", URL: "https://two.example", Priority: 2},
	)}
	if _, err := ReconcileBootstrap(database, sources); err != nil {
		t.Fatal(err)
	}
	if err := database.Where("adapter_type = ? AND name = ?", "pypi", "secondary").Delete(&dbmodel.UpstreamRecord{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileBootstrap(database, sources); err != nil {
		t.Fatal(err)
	}
	var count int64
	database.Model(&dbmodel.UpstreamRecord{}).Where("adapter_type = ?", "pypi").Count(&count)
	if count != 1 {
		t.Fatalf("deleted config row was restored; count=%d", count)
	}
}

func TestReconcileBootstrap_SeededConfigActivatesOnlyNewEcosystem(t *testing.T) {
	database := bootstrapDB(t)
	if _, err := ReconcileBootstrap(database, []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "https://one.example", Priority: 1})}); err != nil {
		t.Fatal(err)
	}

	got, err := ReconcileBootstrap(database, []SeedSource{
		source("pypi", config.UpstreamConfig{Name: "must-not-import", URL: "https://must-not-import.example", Priority: 2}),
		source("npm", config.UpstreamConfig{Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pypi", "npm"}; !reflect.DeepEqual(got.ActiveEcosystems, want) {
		t.Fatalf("active=%v want=%v", got.ActiveEcosystems, want)
	}
	var npmCount, pypiCount int64
	database.Model(&dbmodel.UpstreamRecord{}).Where("adapter_type = ?", "npm").Count(&npmCount)
	database.Model(&dbmodel.UpstreamRecord{}).Where("adapter_type = ?", "pypi").Count(&pypiCount)
	if npmCount != 1 || pypiCount != 1 {
		t.Fatalf("npm=%d pypi=%d", npmCount, pypiCount)
	}
}

func TestReconcileBootstrap_MigratesExactLegacyBuiltInUpstreamURLs(t *testing.T) {
	database := bootstrapDB(t)
	tests := []struct {
		adapter  string
		name     string
		wantName string
		oldURL   string
		wantURL  string
	}{
		{adapter: "cargo", name: "rsproxy", wantName: "rsproxy", oldURL: "https://rsproxy.cn/index", wantURL: "https://rsproxy.cn/index/"},
		{adapter: "maven", name: "central", wantName: "central", oldURL: "https://repo1.maven.org/maven2", wantURL: "https://repo.maven.apache.org/maven2/"},
		{adapter: "rubygems", name: "ruby-china", wantName: "tuna", oldURL: "https://gems.ruby-china.com", wantURL: "https://mirrors.tuna.tsinghua.edu.cn/rubygems/"},
		{adapter: "composer", name: "aliyun", wantName: "aliyun", oldURL: "https://mirrors.aliyun.com/composer", wantURL: "https://mirrors.aliyun.com/composer/"},
	}

	checkedAt := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	ids := make([]uint, 0, len(tests))
	for index, test := range tests {
		record := dbmodel.UpstreamRecord{
			AdapterType:   test.adapter,
			Name:          test.name,
			URL:           test.oldURL,
			Proxy:         "http://proxy.example",
			Priority:      index + 3,
			ProbeMode:     "passive",
			ProbeInterval: "17m",
		}
		if err := database.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Model(&record).UpdateColumns(map[string]any{
			"healthy":         false,
			"avg_latency_ms":  912,
			"success_rate":    0.2,
			"last_checked_at": checkedAt,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Create(&dbmodel.UpstreamLatencyLog{
			UpstreamID: record.ID, Name: record.Name, LatencyMs: 912, Healthy: false, CreatedAt: checkedAt,
		}).Error; err != nil {
			t.Fatal(err)
		}
		ids = append(ids, record.ID)
	}
	for _, state := range []dbmodel.ControlPlaneState{
		{Key: SeedMarkerKey, Value: "true"},
		{Key: ActiveEcosystemsKey, Value: `["cargo","maven","rubygems","composer"]`},
	} {
		if err := database.Create(&state).Error; err != nil {
			t.Fatal(err)
		}
	}

	bootstrap, err := ReconcileBootstrap(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index, test := range tests {
		var record dbmodel.UpstreamRecord
		if err := database.First(&record, ids[index]).Error; err != nil {
			t.Fatal(err)
		}
		if record.Name != test.wantName {
			t.Errorf("%s/%s name=%q want=%q", test.adapter, test.name, record.Name, test.wantName)
		}
		if record.URL != test.wantURL {
			t.Errorf("%s/%s URL=%q want=%q", test.adapter, test.name, record.URL, test.wantURL)
		}
		if record.Proxy != "http://proxy.example" || record.Priority != index+3 || record.ProbeMode != "passive" || record.ProbeInterval != "17m" {
			t.Errorf("%s/%s operator fields changed: %#v", test.adapter, test.name, record)
		}
		if !record.Healthy || record.AvgLatencyMs != 0 || record.SuccessRate != 1 || !record.LastCheckedAt.IsZero() {
			t.Errorf("%s/%s retained health from the old target: %#v", test.adapter, test.name, record)
		}
	}
	var migrationState dbmodel.ControlPlaneState
	if err := database.First(&migrationState, "key = ?", "upstreams_builtin_defaults_version").Error; err != nil {
		t.Fatal(err)
	}
	if migrationState.Value != "1" {
		t.Fatalf("built-in defaults version=%q want=1", migrationState.Value)
	}
	registry, err := NewRegistry(database, bootstrap.ActiveEcosystems)
	if err != nil {
		t.Fatal(err)
	}
	pools := registry.Pools()
	for _, test := range tests {
		snapshot := pools[test.adapter].Snapshot()
		if len(snapshot) != 1 || snapshot[0].Name != test.wantName || snapshot[0].URL != test.wantURL {
			t.Errorf("%s first registry snapshot=%#v want name=%q URL=%q", test.adapter, snapshot, test.wantName, test.wantURL)
		}
	}
	var retainedHistory int64
	if err := database.Model(&dbmodel.UpstreamLatencyLog{}).Count(&retainedHistory).Error; err != nil {
		t.Fatal(err)
	}
	if retainedHistory != 0 {
		t.Fatalf("migrated target retained %d stale latency log(s)", retainedHistory)
	}

	var afterFirst []dbmodel.UpstreamRecord
	if err := database.Order("id").Find(&afterFirst).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileBootstrap(database, nil); err != nil {
		t.Fatal(err)
	}
	var afterSecond []dbmodel.UpstreamRecord
	if err := database.Order("id").Find(&afterSecond).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterSecond, afterFirst) {
		t.Fatalf("second reconciliation was not idempotent:\nfirst:  %#v\nsecond: %#v", afterFirst, afterSecond)
	}
}

func TestReconcileBootstrap_DoesNotMigrateCustomizedUpstreamURLs(t *testing.T) {
	tests := []struct {
		name     string
		adapter  string
		upstream string
		url      string
	}{
		{name: "custom cargo URL", adapter: "cargo", upstream: "rsproxy", url: "https://cargo.example/index"},
		{name: "custom cargo name", adapter: "cargo", upstream: "private", url: "https://rsproxy.cn/index"},
		{name: "different adapter", adapter: "pypi", upstream: "rsproxy", url: "https://rsproxy.cn/index"},
		{name: "rubygems trailing slash differs", adapter: "rubygems", upstream: "ruby-china", url: "https://gems.ruby-china.com/"},
		{name: "composer already canonical", adapter: "composer", upstream: "aliyun", url: "https://mirrors.aliyun.com/composer/"},
		{name: "maven trailing slash differs", adapter: "maven", upstream: "central", url: "https://repo1.maven.org/maven2/"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := bootstrapDB(t)
			record := dbmodel.UpstreamRecord{
				AdapterType:   test.adapter,
				Name:          test.upstream,
				URL:           test.url,
				Proxy:         "http://operator-proxy.example",
				Priority:      9,
				ProbeMode:     "passive",
				ProbeInterval: "11m",
			}
			if err := database.Create(&record).Error; err != nil {
				t.Fatal(err)
			}
			for _, state := range []dbmodel.ControlPlaneState{
				{Key: SeedMarkerKey, Value: "true"},
				{Key: ActiveEcosystemsKey, Value: `["` + test.adapter + `"]`},
			} {
				if err := database.Create(&state).Error; err != nil {
					t.Fatal(err)
				}
			}

			var before dbmodel.UpstreamRecord
			if err := database.First(&before, record.ID).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := ReconcileBootstrap(database, nil); err != nil {
				t.Fatal(err)
			}
			var after dbmodel.UpstreamRecord
			if err := database.First(&after, record.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("customized upstream changed:\nbefore: %#v\nafter:  %#v", before, after)
			}
		})
	}
}

func TestReconcileBootstrap_LegacyRubyGemsRenameConflictFallsBackToURLOnly(t *testing.T) {
	database := bootstrapDB(t)
	legacy := dbmodel.UpstreamRecord{
		AdapterType: "rubygems", Name: "ruby-china", URL: "https://gems.ruby-china.com",
		Proxy: "http://legacy-proxy.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "13m",
	}
	operator := dbmodel.UpstreamRecord{
		AdapterType: "rubygems", Name: "tuna", URL: "https://operator-tuna.example/rubygems/",
		Proxy: "http://operator-proxy.example", Priority: 2, ProbeMode: "active", ProbeInterval: "19m",
	}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&operator).Error; err != nil {
		t.Fatal(err)
	}
	for _, log := range []dbmodel.UpstreamLatencyLog{
		{UpstreamID: legacy.ID, Name: legacy.Name, LatencyMs: 100, Healthy: false},
		{UpstreamID: operator.ID, Name: operator.Name, LatencyMs: 20, Healthy: true},
	} {
		if err := database.Create(&log).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []dbmodel.ControlPlaneState{
		{Key: SeedMarkerKey, Value: "true"},
		{Key: ActiveEcosystemsKey, Value: `["rubygems"]`},
	} {
		if err := database.Create(&state).Error; err != nil {
			t.Fatal(err)
		}
	}
	var legacyBefore, operatorBefore dbmodel.UpstreamRecord
	if err := database.First(&legacyBefore, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.First(&operatorBefore, operator.ID).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := ReconcileBootstrap(database, nil); err != nil {
		t.Fatal(err)
	}
	var legacyAfter, operatorAfter dbmodel.UpstreamRecord
	if err := database.First(&legacyAfter, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.First(&operatorAfter, operator.ID).Error; err != nil {
		t.Fatal(err)
	}
	if legacyAfter.Name != legacyBefore.Name || legacyAfter.URL != "https://mirrors.tuna.tsinghua.edu.cn/rubygems/" ||
		legacyAfter.Proxy != legacyBefore.Proxy || legacyAfter.Priority != legacyBefore.Priority ||
		legacyAfter.ProbeMode != legacyBefore.ProbeMode || legacyAfter.ProbeInterval != legacyBefore.ProbeInterval {
		t.Fatalf("conflicting legacy row was not safely migrated: %#v", legacyAfter)
	}
	if !reflect.DeepEqual(operatorAfter, operatorBefore) {
		t.Fatalf("operator tuna row changed:\nbefore: %#v\nafter:  %#v", operatorBefore, operatorAfter)
	}
	var legacyHistory, operatorHistory int64
	if err := database.Model(&dbmodel.UpstreamLatencyLog{}).Where("upstream_id = ?", legacy.ID).Count(&legacyHistory).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&dbmodel.UpstreamLatencyLog{}).Where("upstream_id = ?", operator.ID).Count(&operatorHistory).Error; err != nil {
		t.Fatal(err)
	}
	if legacyHistory != 0 || operatorHistory != 1 {
		t.Fatalf("history counts legacy=%d operator=%d want 0/1", legacyHistory, operatorHistory)
	}
}

func TestReconcileBootstrap_BuiltInURLMigrationDoesNotOverrideLaterAdminEdit(t *testing.T) {
	database := bootstrapDB(t)
	record := dbmodel.UpstreamRecord{AdapterType: "cargo", Name: "rsproxy", URL: "https://rsproxy.cn/index", Priority: 1}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	for _, state := range []dbmodel.ControlPlaneState{
		{Key: SeedMarkerKey, Value: "true"},
		{Key: ActiveEcosystemsKey, Value: `["cargo"]`},
	} {
		if err := database.Create(&state).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ReconcileBootstrap(database, nil); err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&record).UpdateColumns(map[string]any{
		"url":             "https://rsproxy.cn/index",
		"healthy":         false,
		"avg_latency_ms":  88,
		"success_rate":    0.5,
		"last_checked_at": time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := ReconcileBootstrap(database, nil); err != nil {
		t.Fatal(err)
	}
	var after dbmodel.UpstreamRecord
	if err := database.First(&after, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.URL != "https://rsproxy.cn/index" || after.Healthy || after.AvgLatencyMs != 88 || after.SuccessRate != 0.5 {
		t.Fatalf("later Admin edit was overwritten on restart: %#v", after)
	}
}

func TestReconcileBootstrap_FirstSeedPreservesExplicitOldConfigDefaults(t *testing.T) {
	database := bootstrapDB(t)
	got, err := ReconcileBootstrap(database, []SeedSource{source("cargo", config.UpstreamConfig{
		Name: "rsproxy", URL: "https://rsproxy.cn/index", Priority: 1,
	})})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.ActiveEcosystems, []string{"cargo"}) {
		t.Fatalf("active=%v want=[cargo]", got.ActiveEcosystems)
	}
	var record dbmodel.UpstreamRecord
	if err := database.Where("adapter_type = ? AND name = ?", "cargo", "rsproxy").First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if record.URL != "https://rsproxy.cn/index" {
		t.Fatalf("first seed rewrote explicit config URL %q", record.URL)
	}
	var migrationState dbmodel.ControlPlaneState
	if err := database.First(&migrationState, "key = ?", "upstreams_builtin_defaults_version").Error; err != nil {
		t.Fatal(err)
	}
	if migrationState.Value != "1" {
		t.Fatalf("built-in defaults version=%q want=1", migrationState.Value)
	}
}

func TestReconcileBootstrap_FirstSeedMigratesPreexistingLegacyRow(t *testing.T) {
	database := bootstrapDB(t)
	record := dbmodel.UpstreamRecord{
		AdapterType: "cargo", Name: "rsproxy", URL: "https://rsproxy.cn/index", Priority: 1,
	}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}

	got, err := ReconcileBootstrap(database, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.ActiveEcosystems, []string{"cargo"}) {
		t.Fatalf("active=%v want=[cargo]", got.ActiveEcosystems)
	}
	var after dbmodel.UpstreamRecord
	if err := database.First(&after, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.URL != "https://rsproxy.cn/index/" {
		t.Fatalf("preexisting legacy URL=%q want canonical URL", after.URL)
	}
}

func TestReconcileBootstrap_ActiveEcosystemWithoutRowsFailsAndDoesNotReimport(t *testing.T) {
	database := bootstrapDB(t)
	sources := []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "https://one.example", Priority: 1})}
	if _, err := ReconcileBootstrap(database, sources); err != nil {
		t.Fatal(err)
	}
	if err := database.Where("adapter_type = ?", "pypi").Delete(&dbmodel.UpstreamRecord{}).Error; err != nil {
		t.Fatal(err)
	}
	_, err := ReconcileBootstrap(database, sources)
	if err == nil || err.Error() != "active ecosystem pypi has no upstreams" {
		t.Fatalf("err=%v", err)
	}
	var count int64
	database.Model(&dbmodel.UpstreamRecord{}).Where("adapter_type = ?", "pypi").Count(&count)
	if count != 0 {
		t.Fatalf("active config was reimported; count=%d", count)
	}
}

func TestReconcileBootstrap_IgnoresDockerAndExtraRecords(t *testing.T) {
	database := bootstrapDB(t)
	for _, row := range []dbmodel.UpstreamRecord{
		{AdapterType: "docker", Name: "hub", URL: "https://registry-1.docker.io", Priority: 1},
		{AdapterType: "extra:private", Name: "private", URL: "https://private.example", Priority: 1},
	} {
		if err := database.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReconcileBootstrap(database, []SeedSource{source("pypi")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ActiveEcosystems) != 0 {
		t.Fatalf("active=%v", got.ActiveEcosystems)
	}
	var state dbmodel.ControlPlaneState
	if err := database.First(&state, "key = ?", ActiveEcosystemsKey).Error; err != nil {
		t.Fatal(err)
	}
	var stored []string
	if err := json.Unmarshal([]byte(state.Value), &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 {
		t.Fatalf("stored=%v", stored)
	}
}

func TestReconcileBootstrap_ValidatesAllSourcesBeforeWriting(t *testing.T) {
	tests := []struct {
		name    string
		sources []SeedSource
	}{
		{name: "blank name", sources: []SeedSource{source("pypi", config.UpstreamConfig{Name: " ", URL: "https://one.example", Priority: 1})}},
		{name: "long name", sources: []SeedSource{source("pypi", config.UpstreamConfig{Name: strings.Repeat("a", 129), URL: "https://one.example", Priority: 1})}},
		{name: "duplicate trimmed name", sources: []SeedSource{source("pypi",
			config.UpstreamConfig{Name: "one", URL: "https://one.example", Priority: 1},
			config.UpstreamConfig{Name: " one ", URL: "https://two.example", Priority: 2},
		)}},
		{name: "zero priority", sources: []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "https://one.example"})}},
		{name: "invalid url", sources: []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "file:///tmp/one", Priority: 1})}},
		{name: "missing url host", sources: []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "https:///one", Priority: 1})}},
		{name: "invalid proxy", sources: []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "https://one.example", Proxy: "socks5://proxy.example", Priority: 1})}},
		{name: "invalid probe mode", sources: []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "https://one.example", Priority: 1, ProbeMode: "sometimes"})}},
		{name: "invalid probe interval", sources: []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "https://one.example", Priority: 1, ProbeInterval: "later"})}},
		{name: "non-positive probe interval", sources: []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "https://one.example", Priority: 1, ProbeInterval: "0s"})}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := bootstrapDB(t)
			legacy := dbmodel.UpstreamRecord{AdapterType: "npm", Name: "legacy", URL: "https://legacy.example", Priority: 1}
			if err := database.Create(&legacy).Error; err != nil {
				t.Fatal(err)
			}
			for _, state := range []dbmodel.ControlPlaneState{
				{Key: SeedMarkerKey, Value: "true"},
				{Key: ActiveEcosystemsKey, Value: `["npm"]`},
			} {
				if err := database.Create(&state).Error; err != nil {
					t.Fatal(err)
				}
			}

			got, err := ReconcileBootstrap(database, tt.sources)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if len(got.ActiveEcosystems) != 0 {
				t.Fatalf("result=%v", got.ActiveEcosystems)
			}
			var rows, states int64
			if err := database.Model(&dbmodel.UpstreamRecord{}).Count(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Model(&dbmodel.ControlPlaneState{}).Count(&states).Error; err != nil {
				t.Fatal(err)
			}
			if rows != 1 || states != 2 {
				t.Fatalf("invalid input changed database: rows=%d states=%d", rows, states)
			}
			var marker, active dbmodel.ControlPlaneState
			if err := database.First(&marker, "key = ?", SeedMarkerKey).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.First(&active, "key = ?", ActiveEcosystemsKey).Error; err != nil {
				t.Fatal(err)
			}
			if marker.Value != "true" || active.Value != `["npm"]` {
				t.Fatalf("invalid input changed state: marker=%q active=%q", marker.Value, active.Value)
			}
		})
	}
}

func TestReconcileBootstrap_NormalizesSeedValues(t *testing.T) {
	database := bootstrapDB(t)
	got, err := ReconcileBootstrap(database, []SeedSource{source("pypi", config.UpstreamConfig{
		Name: " primary ", URL: " https://pypi.example/simple ", Proxy: " http://proxy.example ", Priority: 1, ProbeInterval: "60m",
	})})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pypi"}; !reflect.DeepEqual(got.ActiveEcosystems, want) {
		t.Fatalf("active=%v want=%v", got.ActiveEcosystems, want)
	}
	var row dbmodel.UpstreamRecord
	if err := database.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Name != "primary" || row.URL != "https://pypi.example/simple" || row.Proxy != "http://proxy.example" || row.ProbeMode != "active" || row.ProbeInterval != "1h0m0s" {
		t.Fatalf("row was not normalized: %#v", row)
	}
}

func TestReconcileBootstrap_RejectsCorruptPersistedState(t *testing.T) {
	tests := []struct {
		name        string
		markerValue string
		activeValue *string
	}{
		{name: "invalid marker", markerValue: "false"},
		{name: "missing active state", markerValue: "true"},
		{name: "null active state", markerValue: "true", activeValue: stringPtr("null")},
		{name: "malformed active state", markerValue: "true", activeValue: stringPtr("[")},
		{name: "non-string active state", markerValue: "true", activeValue: stringPtr(`[1]`)},
		{name: "duplicate active state", markerValue: "true", activeValue: stringPtr(`["pypi","pypi"]`)},
		{name: "unsupported active state", markerValue: "true", activeValue: stringPtr(`["docker"]`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := bootstrapDB(t)
			if err := database.Create(&dbmodel.ControlPlaneState{Key: SeedMarkerKey, Value: tt.markerValue}).Error; err != nil {
				t.Fatal(err)
			}
			if tt.activeValue != nil {
				if err := database.Create(&dbmodel.ControlPlaneState{Key: ActiveEcosystemsKey, Value: *tt.activeValue}).Error; err != nil {
					t.Fatal(err)
				}
			}
			got, err := ReconcileBootstrap(database, nil)
			if err == nil {
				t.Fatal("expected persisted state error")
			}
			if len(got.ActiveEcosystems) != 0 {
				t.Fatalf("result=%v", got.ActiveEcosystems)
			}
			var marker dbmodel.ControlPlaneState
			if err := database.First(&marker, "key = ?", SeedMarkerKey).Error; err != nil {
				t.Fatal(err)
			}
			if marker.Value != tt.markerValue {
				t.Fatalf("marker changed to %q", marker.Value)
			}
		})
	}
}

func TestReconcileBootstrap_RejectsInvalidBuiltInDefaultsVersionWithoutWriting(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "non-numeric", value: "latest", wantErr: `invalid built-in upstream defaults version "latest"`},
		{name: "negative", value: "-1", wantErr: `invalid built-in upstream defaults version "-1"`},
		{name: "future", value: "2", wantErr: "unsupported built-in upstream defaults version 2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := bootstrapDB(t)
			record := dbmodel.UpstreamRecord{
				AdapterType: "cargo", Name: "rsproxy", URL: "https://rsproxy.cn/index", Priority: 1,
			}
			if err := database.Create(&record).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Create(&dbmodel.ControlPlaneState{Key: builtInDefaultsVersionKey, Value: test.value}).Error; err != nil {
				t.Fatal(err)
			}

			if _, err := ReconcileBootstrap(database, nil); err == nil || err.Error() != test.wantErr {
				t.Fatalf("err=%v want=%q", err, test.wantErr)
			}
			var after dbmodel.UpstreamRecord
			if err := database.First(&after, record.ID).Error; err != nil {
				t.Fatal(err)
			}
			if after.URL != record.URL {
				t.Fatalf("invalid version changed URL to %q", after.URL)
			}
			var version dbmodel.ControlPlaneState
			if err := database.First(&version, "key = ?", builtInDefaultsVersionKey).Error; err != nil {
				t.Fatal(err)
			}
			if version.Value != test.value {
				t.Fatalf("version=%q want=%q", version.Value, test.value)
			}
		})
	}
}

func TestReconcileBootstrap_CanonicalizesPersistedActiveOrderAndAcceptsEmpty(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stored string
		want   []string
	}{
		{name: "canonical order", stored: `["npm","pypi"]`, want: []string{"pypi", "npm"}},
		{name: "empty", stored: `[]`, want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := bootstrapDB(t)
			for _, ecosystem := range tc.want {
				if err := database.Create(&dbmodel.UpstreamRecord{AdapterType: ecosystem, Name: ecosystem, URL: "https://" + ecosystem + ".example", Priority: 1}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := database.Create(&dbmodel.ControlPlaneState{Key: SeedMarkerKey, Value: "true"}).Error; err != nil {
				t.Fatal(err)
			}
			if err := database.Create(&dbmodel.ControlPlaneState{Key: ActiveEcosystemsKey, Value: tc.stored}).Error; err != nil {
				t.Fatal(err)
			}
			got, err := ReconcileBootstrap(database, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.ActiveEcosystems, tc.want) {
				t.Fatalf("active=%v want=%v", got.ActiveEcosystems, tc.want)
			}
		})
	}
}

func TestReconcileBootstrap_StateSaveFailureRollsBackRowsAndMarker(t *testing.T) {
	database := bootstrapDB(t)
	trigger := `CREATE TRIGGER fail_active_state BEFORE INSERT ON control_plane_states
		WHEN NEW.key = '` + ActiveEcosystemsKey + `'
		BEGIN SELECT RAISE(ABORT, 'injected active state failure'); END`
	if err := database.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}

	got, err := ReconcileBootstrap(database, []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "https://one.example", Priority: 1})})
	if err == nil {
		t.Fatal("expected injected state-save error")
	}
	if len(got.ActiveEcosystems) != 0 {
		t.Fatalf("result=%v", got.ActiveEcosystems)
	}
	var rows, states int64
	if err := database.Model(&dbmodel.UpstreamRecord{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&dbmodel.ControlPlaneState{}).Count(&states).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 0 || states != 0 {
		t.Fatalf("transaction was not rolled back: rows=%d states=%d", rows, states)
	}
}

func TestReconcileBootstrap_LaterStateSaveFailureRollsBackBuiltInMigrationAndVersion(t *testing.T) {
	database := bootstrapDB(t)
	record := dbmodel.UpstreamRecord{
		AdapterType:   "cargo",
		Name:          "rsproxy",
		URL:           "https://rsproxy.cn/index",
		Proxy:         "http://operator-proxy.example",
		Priority:      7,
		ProbeMode:     "passive",
		ProbeInterval: "23m",
	}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	checkedAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	if err := database.Model(&record).UpdateColumns(map[string]any{
		"healthy":         false,
		"avg_latency_ms":  144,
		"success_rate":    0.25,
		"last_checked_at": checkedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&dbmodel.UpstreamLatencyLog{
		UpstreamID: record.ID, Name: record.Name, LatencyMs: 144, Healthy: false, CreatedAt: checkedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, state := range []dbmodel.ControlPlaneState{
		{Key: SeedMarkerKey, Value: "true"},
		{Key: ActiveEcosystemsKey, Value: `["cargo"]`},
	} {
		if err := database.Create(&state).Error; err != nil {
			t.Fatal(err)
		}
	}
	var before dbmodel.UpstreamRecord
	if err := database.First(&before, record.ID).Error; err != nil {
		t.Fatal(err)
	}

	trigger := `CREATE TRIGGER fail_active_state_update BEFORE UPDATE ON control_plane_states
		WHEN OLD.key = '` + ActiveEcosystemsKey + `'
		BEGIN SELECT RAISE(ABORT, 'injected active state update failure'); END`
	if err := database.Exec(trigger).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileBootstrap(database, nil); err == nil {
		t.Fatal("expected injected state-save error")
	}

	var after dbmodel.UpstreamRecord
	if err := database.First(&after, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("migration was not rolled back:\nbefore: %#v\nafter:  %#v", before, after)
	}
	var migrationVersions int64
	if err := database.Model(&dbmodel.ControlPlaneState{}).Where("key = ?", builtInDefaultsVersionKey).Count(&migrationVersions).Error; err != nil {
		t.Fatal(err)
	}
	if migrationVersions != 0 {
		t.Fatalf("built-in defaults version survived rollback: count=%d", migrationVersions)
	}
	var history int64
	if err := database.Model(&dbmodel.UpstreamLatencyLog{}).Where("upstream_id = ?", record.ID).Count(&history).Error; err != nil {
		t.Fatal(err)
	}
	if history != 1 {
		t.Fatalf("latency history deletion was not rolled back: count=%d", history)
	}
}

func TestReconcileBootstrap_ConcurrentCallsAreIdempotent(t *testing.T) {
	database := bootstrapDB(t)
	sources := []SeedSource{source("pypi", config.UpstreamConfig{Name: "one", URL: "https://one.example", Priority: 1})}
	results := make(chan BootstrapResult, 2)
	errs := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < 2; i++ {
		go func() {
			start.Wait()
			got, err := ReconcileBootstrap(database, sources)
			results <- got
			errs <- err
		}()
	}
	start.Done()
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		if got := <-results; !reflect.DeepEqual(got.ActiveEcosystems, []string{"pypi"}) {
			t.Fatalf("active=%v", got.ActiveEcosystems)
		}
	}
	var rows, marker, active int64
	database.Model(&dbmodel.UpstreamRecord{}).Count(&rows)
	database.Model(&dbmodel.ControlPlaneState{}).Where("key = ?", SeedMarkerKey).Count(&marker)
	database.Model(&dbmodel.ControlPlaneState{}).Where("key = ?", ActiveEcosystemsKey).Count(&active)
	if rows != 1 || marker != 1 || active != 1 {
		t.Fatalf("rows=%d marker=%d active=%d", rows, marker, active)
	}
}

func stringPtr(value string) *string { return &value }
