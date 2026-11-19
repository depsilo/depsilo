// Package adaptertest holds fixtures shared by package-adapter tests. Only
// tests import it; the depsilo binary does not link it.
//
// It imports no package adapter, so an adapter's in-package tests can use it.
// It does import the top-level adapter package, so tests of that package use
// it from the external adapter_test package.
package adaptertest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/quarantine"
	"depsilo/internal/upstream"
)

// NewManager returns a cache manager over a freshly migrated SQLite database
// and local storage under t's temporary directory, together with that
// database and storage. The manager is closed when t ends.
func NewManager(t testing.TB, immutableThreshold time.Duration) (*cache.Manager, *gorm.DB, *cache.LocalStorage) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "adapter.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), immutableThreshold)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
	return manager, database, storage
}

// UnavailableSelector is an upstream.Selector that never yields an Upstream.
type UnavailableSelector struct{}

// Select always fails.
func (UnavailableSelector) Select(context.Context) (*upstream.Upstream, error) {
	return nil, errors.New("test upstream unavailable")
}

// AccessRows returns the access log rows written to database, oldest first.
// A RequestScope without a recorder writes each package request's row to the
// database directly, so a handler test reads its request outcomes here.
func AccessRows(t testing.TB, database *gorm.DB) []db.AccessLog {
	t.Helper()
	var rows []db.AccessLog
	if err := database.Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("read access rows: %v", err)
	}
	return rows
}

// AuditCapture records every audit entry logged to it.
type AuditCapture struct {
	Entries []db.AuditLog
}

// Log records entry.
func (capture *AuditCapture) Log(entry db.AuditLog) {
	capture.Entries = append(capture.Entries, entry)
}

// Provenance says how a provenance environment treats an ecosystem's release
// timestamp.
type Provenance int

const (
	// BoundProvenance reports the ecosystem as having an end-to-end
	// artifact-source and timestamp binding.
	BoundProvenance Provenance = iota
	// ApproximateProvenance lists the ecosystem as an approximate source.
	ApproximateProvenance
)

// ProvenanceEnv is what a provenance test builds its adapter handler from:
// a cache manager, a passive selector over one Upstream, the database, and a
// checker enforcing a 168h minimum release age for one ecosystem.
type ProvenanceEnv struct {
	Manager  *cache.Manager
	Selector upstream.Selector
	DB       *gorm.DB
	Checker  *quarantine.Checker
}

// NewProvenanceEnv returns a ProvenanceEnv whose selector targets upstreamURL
// and whose checker gates ecosystem with the given provenance.
func NewProvenanceEnv(t testing.TB, upstreamURL, ecosystem string, provenance Provenance) ProvenanceEnv {
	t.Helper()
	manager, database, _ := NewManager(t, 72*time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: upstreamURL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}

	enabled := true
	policyConfig := quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{ecosystem: "168h"},
	}
	var bound func(string) bool
	switch provenance {
	case BoundProvenance:
		bound = func(candidate string) bool { return candidate == ecosystem }
	case ApproximateProvenance:
		policyConfig.ApproximateSources = []string{ecosystem}
	}
	policy, err := quarantine.NewPolicyWithProvenance(policyConfig, bound)
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance: %v", err)
	}
	store := quarantine.NewStore(database)
	checker, err := quarantine.NewChecker(policy, quarantine.NewLookup(store, nil), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	return ProvenanceEnv{
		Manager:  manager,
		Selector: upstream.NewPassiveRecoverySelector(pool),
		DB:       database,
		Checker:  checker,
	}
}

// Origin is an upstream origin that answers every request with Status, and
// with Body when Status is 200. It counts the requests it receives.
type Origin struct {
	Status   int
	Body     string
	requests atomic.Int32
}

// ServeHTTP answers the request.
func (origin *Origin) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	origin.requests.Add(1)
	if origin.Status != http.StatusOK {
		w.WriteHeader(origin.Status)
		return
	}
	_, _ = io.WriteString(w, origin.Body)
}

// Requests returns how many requests the origin received.
func (origin *Origin) Requests() int32 { return origin.requests.Load() }

// NewPoolSelector returns a passive selector over one Upstream per origin,
// named member-1, member-2, ... and preferred in the order given.
func NewPoolSelector(t testing.TB, origins ...http.Handler) *upstream.PassiveRecoverySelector {
	t.Helper()
	configs := make([]config.UpstreamConfig, 0, len(origins))
	for index, origin := range origins {
		server := httptest.NewServer(origin)
		t.Cleanup(server.Close)
		configs = append(configs, config.UpstreamConfig{
			Name: fmt.Sprintf("member-%d", index+1), URL: server.URL, Priority: index + 1, ProbeMode: "passive",
		})
	}
	pool, err := upstream.NewPool(configs)
	if err != nil {
		t.Fatal(err)
	}
	return upstream.NewPassiveRecoverySelector(pool)
}

// PoolAnswer is one way a two-member pool can answer a package route, and
// what the client and the access record must see for it.
type PoolAnswer struct {
	Name string
	// First and Second are the statuses the preferred and the other member
	// answer with.
	First, Second int
	// Status is what the client receives; Code is the error body's code,
	// empty for a success.
	Status int
	Code   string
	// SecondAsked says whether the request reaches the second member.
	SecondAsked bool
}

// MetadataPoolAnswers are the answers for content that never moves on a 404:
// one request must not mix snapshots of different mirrors, so a member's 404
// or 410 is the pool's answer, while an unavailable member is passed over.
var MetadataPoolAnswers = []PoolAnswer{
	{Name: "missing", First: http.StatusNotFound, Second: http.StatusOK, Status: http.StatusNotFound, Code: "NOT_FOUND"},
	{Name: "gone", First: http.StatusGone, Second: http.StatusOK, Status: http.StatusGone, Code: "GONE"},
	{Name: "failing member", First: http.StatusInternalServerError, Second: http.StatusOK, Status: http.StatusOK, SecondAsked: true},
	{Name: "rate-limited member", First: http.StatusTooManyRequests, Second: http.StatusOK, Status: http.StatusOK, SecondAsked: true},
	{Name: "failing pool", First: http.StatusInternalServerError, Second: http.StatusServiceUnavailable, Status: http.StatusBadGateway, Code: "UPSTREAM_UNAVAILABLE", SecondAsked: true},
	{Name: "forbidden", First: http.StatusForbidden, Second: http.StatusOK, Status: http.StatusBadGateway, Code: "UPSTREAM_UNAVAILABLE"},
}

// CheckPoolAnswers requests target from the router build returns once per
// answer, each time over a fresh two-member pool whose members serve body,
// and checks the response status and code, the status the access log row
// written to build's database carries, and whether the second member was
// asked.
func CheckPoolAnswers(
	t *testing.T,
	answers []PoolAnswer,
	target, body string,
	build func(t *testing.T, selector upstream.Selector) (http.Handler, *gorm.DB),
) {
	t.Helper()
	for _, answer := range answers {
		t.Run(answer.Name, func(t *testing.T) {
			first := &Origin{Status: answer.First, Body: body}
			second := &Origin{Status: answer.Second, Body: body}
			router, database := build(t, NewPoolSelector(t, first, second))
			handler := adapter.NewRequestScope(nil, nil, nil, nil).Wrap(router)

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))

			if response.Code != answer.Status {
				t.Fatalf("GET %s status = %d, want %d; body=%s", target, response.Code, answer.Status, response.Body.String())
			}
			if answer.Code != "" {
				var failure struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil || failure.Code != answer.Code {
					t.Fatalf("GET %s error body = %s, want code %s", target, response.Body.String(), answer.Code)
				}
			}
			if asked := second.Requests() > 0; asked != answer.SecondAsked {
				t.Fatalf("second member asked = %v, want %v", asked, answer.SecondAsked)
			}
			rows := AccessRows(t, database)
			if len(rows) == 0 {
				t.Fatalf("GET %s left no access record", target)
			}
			if recorded := rows[len(rows)-1].StatusCode; recorded != answer.Status {
				t.Fatalf("access record status = %d, want %d", recorded, answer.Status)
			}
		})
	}
}
