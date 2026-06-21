package notify

import (
	"encoding/json"
	"testing"
	"time"
)

func testEvent() Event {
	return Event{
		Type:      EventUpstreamDown,
		Severity:  "critical",
		Title:     "All PyPI upstreams are down",
		Message:   "3 of 3 upstreams unhealthy. Packages will be served from stale cache.",
		Detail:    "tuna: timeout | official: timeout | aliyun: timeout",
		Timestamp: time.Date(2026, 6, 21, 14, 30, 0, 0, time.UTC),
	}
}

func mustJSON(t *testing.T, payload []byte) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v\npayload: %s", err, string(payload))
	}
	return m
}

func TestFormatSlack(t *testing.T) {
	payload, err := formatSlack(testEvent())
	if err != nil {
		t.Fatal(err)
	}
	m := mustJSON(t, payload)
	attachments, ok := m["attachments"].([]interface{})
	if !ok || len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %v", m["attachments"])
	}
}

func TestFormatDingTalk(t *testing.T) {
	payload, err := formatDingTalk(testEvent())
	if err != nil {
		t.Fatal(err)
	}
	m := mustJSON(t, payload)
	if m["msgtype"] != "markdown" {
		t.Fatalf("expected msgtype=markdown, got %v", m["msgtype"])
	}
	md, ok := m["markdown"].(map[string]interface{})
	if !ok {
		t.Fatal("markdown field missing")
	}
	if md["title"] != testEvent().Title {
		t.Fatalf("title mismatch: %v", md["title"])
	}
}

func TestFormatWeCom(t *testing.T) {
	payload, err := formatWeCom(testEvent())
	if err != nil {
		t.Fatal(err)
	}
	m := mustJSON(t, payload)
	if m["msgtype"] != "markdown" {
		t.Fatalf("expected msgtype=markdown, got %v", m["msgtype"])
	}
}

func TestFormatFeishu(t *testing.T) {
	payload, err := formatFeishu(testEvent())
	if err != nil {
		t.Fatal(err)
	}
	m := mustJSON(t, payload)
	if m["msg_type"] != "interactive" {
		t.Fatalf("expected msg_type=interactive, got %v", m["msg_type"])
	}
}

func TestFormatGeneric(t *testing.T) {
	payload, err := formatGeneric(testEvent())
	if err != nil {
		t.Fatal(err)
	}
	m := mustJSON(t, payload)
	if m["type"] != EventUpstreamDown {
		t.Fatalf("type mismatch: %v", m["type"])
	}
}

func TestMatchEvent(t *testing.T) {
	tests := []struct {
		filter string
		event  string
		want   bool
	}{
		{"*", "upstream_down", true},
		{"", "upstream_down", true},
		{"upstream_down", "upstream_down", true},
		{"upstream_down,disk_high", "disk_high", true},
		{"upstream_down", "disk_high", false},
		{"upstream_down,disk_high", "vuln_critical", false},
	}
	for _, tt := range tests {
		got := matchEvent(tt.filter, tt.event)
		if got != tt.want {
			t.Errorf("matchEvent(%q, %q) = %v, want %v", tt.filter, tt.event, got, tt.want)
		}
	}
}
