package upstream

import (
	"testing"

	"depsilo/internal/config"
)

func TestProvenanceSourceIdentityIsStableAndConfigurationBound(t *testing.T) {
	newSource := func(name, rawURL string) *Upstream {
		t.Helper()
		pool, err := NewPool([]config.UpstreamConfig{{
			Name: name, URL: rawURL, Priority: 1, ProbeMode: "passive",
		}})
		if err != nil {
			t.Fatal(err)
		}
		return pool.Snapshot()[0]
	}

	first := newSource("primary", "https://registry.example")
	restarted := newSource("primary", "https://registry.example")
	changedOrigin := newSource("primary", "https://other.example")
	if first.ProvenanceSourceID() == "" || first.ProvenanceSourceID() != restarted.ProvenanceSourceID() {
		t.Fatal("equivalent source configuration did not retain its provenance identity")
	}
	if first.ProvenanceSourceID() == changedOrigin.ProvenanceSourceID() {
		t.Fatal("origin change retained the old provenance identity")
	}
}

func TestProvenanceSourceIdentityDoesNotExposeCredentialVerifier(t *testing.T) {
	newSource := func(origin, proxy string) *Upstream {
		t.Helper()
		pool, err := NewPool([]config.UpstreamConfig{{
			Name: "private", URL: origin, Proxy: proxy, Priority: 1, ProbeMode: "passive",
		}})
		if err != nil {
			t.Fatal(err)
		}
		return pool.Snapshot()[0]
	}

	first := newSource(
		"https://registry-user:first-password@registry.example",
		"https://proxy-user:first-proxy-password@proxy.example",
	)
	rotated := newSource(
		"https://registry-user:second-password@registry.example",
		"https://proxy-user:second-proxy-password@proxy.example",
	)
	if first.ProvenanceSourceID() != rotated.ProvenanceSourceID() {
		t.Fatal("credential rotation changed the non-secret provenance endpoint identity")
	}
}

func TestPrioritySelectorResolvesOnlyExactProvenanceSource(t *testing.T) {
	pool, err := NewPool([]config.UpstreamConfig{
		{Name: "a", URL: "https://a.example", Priority: 1, ProbeMode: "passive"},
		{Name: "b", URL: "https://b.example", Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	selector := NewPrioritySelector(pool)
	sourceB := pool.Snapshot()[1]
	resolved, err := selector.ResolveProvenanceSource(sourceB.ProvenanceSourceID())
	if err != nil {
		t.Fatal(err)
	}
	if resolved != sourceB {
		t.Fatal("resolver reapplied priority selection instead of resolving source B")
	}
	if _, err := selector.ResolveProvenanceSource("unknown"); err == nil {
		t.Fatal("unknown provenance source was accepted")
	}
}
