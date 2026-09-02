//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

const (
	onboardingStatusPath   = "/api/v1/admin/onboarding/status"
	onboardingPollTimeout  = 10 * time.Second
	onboardingPollInterval = 50 * time.Millisecond
)

var onboardingArtifactSequence atomic.Uint64

type onboardingStatus struct {
	Status      string            `json:"status"`
	StartedAt   string            `json:"started_at"`
	Events      []onboardingEvent `json:"events"`
	NextAfterID uint              `json:"next_after_id"`
	HasMore     bool              `json:"has_more"`
}

type onboardingEvent struct {
	ID          uint      `json:"id"`
	Ecosystem   string    `json:"ecosystem"`
	PackageName string    `json:"package_name"`
	Version     string    `json:"version"`
	Outcome     string    `json:"outcome"`
	StatusCode  int       `json:"status_code"`
	CreatedAt   time.Time `json:"created_at"`
}

func TestOnboardingStatusTracksRealMissThenHit(t *testing.T) {
	baseline := getOnboardingBaseline(t)
	version := fmt.Sprintf("1.0.%d", onboardingArtifactSequence.Add(1))
	artifactPath := fmt.Sprintf("/onboarding-fixture/-/onboarding-fixture-%s.tgz", version)
	proxyURL := npmVersionTarballURL(t, "onboarding-fixture", version)

	before := mockRequestCountForPath(artifactPath)
	first := httpGet(t, proxyURL)
	assertStatus(t, first, http.StatusOK)
	if body := readBody(t, first); body != "FAKE_ONBOARDING_NPM_TARBALL" {
		t.Fatalf("first onboarding artifact body = %q", body)
	}

	// Make the second request before polling the eventually persisted audit log.
	// This keeps the cache read inside the short integration blob TTL even on a
	// slow runner while the status endpoint remains the only observation seam.
	second := httpGet(t, proxyURL)
	assertStatus(t, second, http.StatusOK)
	if body := readBody(t, second); body != "FAKE_ONBOARDING_NPM_TARBALL" {
		t.Fatalf("cached onboarding artifact body = %q", body)
	}

	events, _ := waitForOnboardingOutcomes(t, baseline, []string{"miss", "hit"}, func(event onboardingEvent) bool {
		return event.Ecosystem == "npm" &&
			event.PackageName == "onboarding-fixture" &&
			event.Version == version
	})
	miss, hit := events[0], events[1]
	if miss.StatusCode != http.StatusOK || miss.ID <= baseline.NextAfterID {
		t.Fatalf("miss event = %+v, baseline after_id = %d", miss, baseline.NextAfterID)
	}
	if hit.StatusCode != http.StatusOK || hit.ID <= miss.ID {
		t.Fatalf("ordered outcomes = %+v", events)
	}
	if got := mockRequestCountForPath(artifactPath); got != before+1 {
		t.Fatalf("cache hit contacted upstream: requests = %d, want %d", got, before+1)
	}
}

func TestOnboardingStatusTracksRealPolicyBlock(t *testing.T) {
	baseline := getOnboardingBaseline(t)
	response, _ := fetchBlocked(t)
	if response.StatusCode != http.StatusUnavailableForLegalReasons {
		t.Fatalf("blocked request status = %d", response.StatusCode)
	}

	blocked, _ := waitForOnboardingOutcome(t, baseline, "blocked", func(event onboardingEvent) bool {
		return event.Ecosystem == "npm" && event.PackageName == "malicious-pkg" && event.Version == "1.0.0"
	})
	if blocked.StatusCode != http.StatusUnavailableForLegalReasons {
		t.Fatalf("blocked event = %+v", blocked)
	}
}

func TestOnboardingStatusTracksRealUpstreamFailure(t *testing.T) {
	baseline := getOnboardingBaseline(t)
	artifactPath := "/onboarding-error/-/onboarding-error-1.0.0.tgz"
	before := mockRequestCountForPath(artifactPath)

	response := httpGet(t, npmVersionTarballURL(t, "onboarding-error", "1.0.0"))
	assertStatus(t, response, http.StatusBadGateway)
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()

	failure, _ := waitForOnboardingOutcome(t, baseline, "error", func(event onboardingEvent) bool {
		return event.Ecosystem == "npm" && event.PackageName == "onboarding-error" && event.Version == "1.0.0"
	})
	if failure.StatusCode != http.StatusBadGateway {
		t.Fatalf("upstream failure event = %+v", failure)
	}
	if got := mockRequestCountForPath(artifactPath); got != before+1 {
		t.Fatalf("upstream failure requests = %d, want %d", got, before+1)
	}
}

func getOnboardingBaseline(t *testing.T) onboardingStatus {
	t.Helper()
	status := getOnboardingStatus(t, "")
	if status.StartedAt == "" {
		t.Fatal("onboarding baseline omitted started_at")
	}
	if _, err := time.Parse(time.RFC3339Nano, status.StartedAt); err != nil {
		t.Fatalf("onboarding baseline started_at = %q: %v", status.StartedAt, err)
	}
	if len(status.Events) != 0 {
		t.Fatalf("onboarding baseline returned historical events: %+v", status.Events)
	}
	return status
}

func waitForOnboardingOutcome(
	t *testing.T,
	cursor onboardingStatus,
	wantOutcome string,
	matches func(onboardingEvent) bool,
) (onboardingEvent, onboardingStatus) {
	t.Helper()
	events, status := waitForOnboardingOutcomes(t, cursor, []string{wantOutcome}, matches)
	return events[0], status
}

func waitForOnboardingOutcomes(
	t *testing.T,
	cursor onboardingStatus,
	wantOutcomes []string,
	matches func(onboardingEvent) bool,
) ([]onboardingEvent, onboardingStatus) {
	t.Helper()
	deadline := time.Now().Add(onboardingPollTimeout)
	last := cursor
	matched := make([]onboardingEvent, 0, len(wantOutcomes))
	for {
		last = getOnboardingStatus(t, onboardingStatusQuery(cursor))
		for _, event := range last.Events {
			if matches(event) {
				matched = append(matched, event)
			}
		}
		if last.NextAfterID > cursor.NextAfterID {
			cursor.NextAfterID = last.NextAfterID
		}
		if len(matched) >= len(wantOutcomes) {
			for index, want := range wantOutcomes {
				if matched[index].Outcome != want {
					t.Fatalf("onboarding outcomes = %+v, want ordered outcomes %v", matched, wantOutcomes)
				}
			}
			return matched[:len(wantOutcomes)], last
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for onboarding outcomes %v; matched = %+v; last status = %+v", wantOutcomes, matched, last)
		}
		timer := time.NewTimer(onboardingPollInterval)
		<-timer.C
	}
}

func onboardingStatusQuery(cursor onboardingStatus) string {
	values := url.Values{}
	values.Set("after_id", fmt.Sprintf("%d", cursor.NextAfterID))
	values.Set("started_at", cursor.StartedAt)
	return "?" + values.Encode()
}

func getOnboardingStatus(t *testing.T, query string) onboardingStatus {
	t.Helper()
	response := adminGet(t, depsiloURL+onboardingStatusPath+query)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET onboarding status = %d: %s", response.StatusCode, body)
	}
	var status onboardingStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatalf("decode onboarding status: %v", err)
	}
	return status
}

func mockRequestCountForPath(path string) int {
	count := 0
	for _, request := range mockServer.Requests() {
		if request.Path == path {
			count++
		}
	}
	return count
}
