package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"depsilo/internal/api/contract"
	"depsilo/internal/blocklist"
	"depsilo/internal/config"
	"depsilo/internal/db"
)

func TestBlocklistStatusAndCapabilitiesReportEachEcosystemsOwnSync(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "blocklist-status.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := blocklist.NewStore(database)
	ctx := context.Background()
	refreshed := make(map[string]int64)
	for _, eco := range blocklist.SyncedEcosystems() {
		refreshed[eco] = 0
	}
	if err := store.RecordSync(ctx, blocklist.SyncResult{Refreshed: refreshed}, 0); err != nil {
		t.Fatal(err)
	}
	delete(refreshed, "maven")
	partial := blocklist.SyncResult{Refreshed: refreshed, Failed: map[string]error{"maven": errors.New("fetch Maven/all.zip: HTTP 404")}}
	if err := store.RecordSync(ctx, partial, 0); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.GET("/status", NewBlocklistHandler(nil, store, nil, "block").Status)
	r.GET("/capabilities", NewCapabilityHandler(database, &config.Config{}, nil, []string{"npm", "maven"}, nil, store, "block", nil).Summary)

	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest("GET", "/status", nil))
	var status contract.BlocklistStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || response.Code != 200 {
		t.Fatalf("status = %d %s (%v)", response.Code, response.Body.String(), err)
	}
	if status.LastError != "maven: fetch Maven/all.zip: HTTP 404" || status.LastSuccessAt == nil || !status.LastSuccessAt.Before(*status.LastSyncAt) {
		t.Fatalf("run-level status = %+v, want the maven failure and the earlier full success", status)
	}
	maven, npm := status.EcosystemSync["maven"], status.EcosystemSync["npm"]
	if maven.LastError == "" || maven.LastSuccessAt == nil || !maven.LastSuccessAt.Equal(*status.LastSuccessAt) {
		t.Fatalf("maven sync = %+v, want its failure and its last good data time", maven)
	}
	if npm.LastError != "" || npm.LastSuccessAt == nil || !npm.LastSuccessAt.Equal(*status.LastSyncAt) {
		t.Fatalf("npm sync = %+v, want refreshed by the latest run", npm)
	}

	response = httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest("GET", "/capabilities", nil))
	var summary contract.CapabilitySummary
	if err := json.Unmarshal(response.Body.Bytes(), &summary); err != nil || response.Code != 200 {
		t.Fatalf("capabilities = %d %s (%v)", response.Code, response.Body.String(), err)
	}
	facts := make(map[string]contract.CapabilityFact)
	for _, fact := range summary.Capabilities {
		if fact.Name == "malicious_blocklist" {
			facts[fact.Ecosystem] = fact
		}
	}
	if facts["npm"].DataStatus != contract.CapabilityDataStatusFresh || facts["npm"].RecentFailure != "" {
		t.Errorf("npm blocklist fact = %+v, want fresh", facts["npm"])
	}
	if facts["maven"].DataStatus != contract.CapabilityDataStatusStale || facts["maven"].RecentFailure == "" {
		t.Errorf("maven blocklist fact = %+v, want stale with its failure", facts["maven"])
	}
}
