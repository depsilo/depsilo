package composer

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRewritePackagesJSON(t *testing.T) {
	in := []byte(`{"metadata-url": "/p2/%package%.json", "search": "/search.json?q=%query%"}`)

	out, err := RewritePackagesJSON(in, "http://depsilo.local/")
	if err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}

	var doc struct {
		MetadataURL string `json:"metadata-url"`
		Search      string `json:"search"`
		Mirrors     []struct {
			DistURL   string `json:"dist-url"`
			Preferred bool   `json:"preferred"`
		} `json:"mirrors"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if doc.MetadataURL != "http://depsilo.local/composer/p2/%package%.json" {
		t.Errorf("metadata-url = %q", doc.MetadataURL)
	}
	if len(doc.Mirrors) != 1 {
		t.Fatalf("expected 1 mirror, got %d", len(doc.Mirrors))
	}
	if doc.Mirrors[0].DistURL != "http://depsilo.local/composer/dist/%package%/%version%/%reference%.%type%" {
		t.Errorf("dist-url = %q", doc.Mirrors[0].DistURL)
	}
	if !doc.Mirrors[0].Preferred {
		t.Error("mirror should be preferred")
	}
	// Unrelated fields pass through untouched.
	if !strings.Contains(doc.Search, "%query%") {
		t.Errorf("search field mangled: %q", doc.Search)
	}
}

func TestRewritePackagesJSON_InvalidJSON(t *testing.T) {
	if _, err := RewritePackagesJSON([]byte("{nope"), "http://x"); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
