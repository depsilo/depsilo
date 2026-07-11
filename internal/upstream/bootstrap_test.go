package upstream

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

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
