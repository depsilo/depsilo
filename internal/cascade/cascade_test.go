package cascade

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewInstanceIDIsValidAndUnique(t *testing.T) {
	first, err := NewInstanceID()
	if err != nil {
		t.Fatalf("NewInstanceID: %v", err)
	}
	second, err := NewInstanceID()
	if err != nil {
		t.Fatalf("NewInstanceID: %v", err)
	}
	if !ValidInstanceID(first) || !ValidInstanceID(second) {
		t.Fatalf("generated IDs are invalid: %q %q", first, second)
	}
	if first == second {
		t.Fatal("generated instance IDs must be unique")
	}
}

func TestValidInstanceIDRejectsUnsafeValues(t *testing.T) {
	cases := []string{
		"",
		"UPPERCASE",
		"has space",
		"has/slash",
		strings.Repeat("a", MaxInstanceIDLength+1),
	}
	for _, value := range cases {
		if ValidInstanceID(value) {
			t.Fatalf("ValidInstanceID(%q) = true, want false", value)
		}
	}
	if !ValidInstanceID("node-01a") {
		t.Fatal("expected lowercase/digit/dash identifier to be valid")
	}
}

func TestChainParseRoundTripAndValidation(t *testing.T) {
	chain, err := ParseChain("alpha,beta-2")
	if err != nil {
		t.Fatalf("ParseChain: %v", err)
	}
	if chain.String() != "alpha,beta-2" {
		t.Fatalf("chain round trip = %q", chain.String())
	}
	if !chain.Contains("beta-2") || chain.Contains("gamma") {
		t.Fatal("chain membership is wrong")
	}
	if next := chain.With("gamma"); next.String() != "alpha,beta-2,gamma" {
		t.Fatalf("With = %q", next.String())
	}
	if _, err := ParseChain("bad id"); err == nil {
		t.Fatal("ParseChain accepted an invalid element")
	}
	tooLong := make([]string, MaxChainEntries+1)
	for index := range tooLong {
		tooLong[index] = "node"
	}
	if _, err := ParseChain(strings.Join(tooLong, ",")); err == nil {
		t.Fatal("ParseChain accepted an oversized chain")
	}
}

func TestChainContextIsCopied(t *testing.T) {
	original := Chain{"alpha"}
	ctx := WithChain(context.Background(), original)
	original[0] = "mutated"
	if got := ChainFrom(ctx); got.String() != "alpha" {
		t.Fatalf("context chain mutated to %q", got.String())
	}
	if ChainFrom(context.Background()) != nil {
		t.Fatal("empty context should not carry a chain")
	}
}

func TestFetchMetadataContext(t *testing.T) {
	ctx := WithFetchTTL(context.Background(), 90*time.Second)
	ctx = WithFetchKind(ctx, KindArtifact)
	ttl, ok := FetchTTLFrom(ctx)
	if !ok || ttl != 90*time.Second {
		t.Fatalf("FetchTTLFrom = %v, %v", ttl, ok)
	}
	if kind, ok := FetchKindFrom(ctx); !ok || kind != KindArtifact {
		t.Fatalf("FetchKindFrom = %q, %v", kind, ok)
	}
	if _, ok := FetchTTLFrom(context.Background()); ok {
		t.Fatal("empty context should not carry a TTL")
	}
	bogusCtx := WithFetchKind(context.Background(), "bogus")
	if _, ok := FetchKindFrom(bogusCtx); ok {
		t.Fatal("invalid kind must not be recorded")
	}
}

func TestTTLHeaderRoundTrip(t *testing.T) {
	if FormatTTLHeader(0) != "0" || FormatTTLHeader(1500*time.Millisecond) != "1" {
		t.Fatal("FormatTTLHeader truncated incorrectly")
	}
	ttl, present, err := ParseTTLHeader("3600")
	if err != nil || !present || ttl != time.Hour {
		t.Fatalf("ParseTTLHeader = %v, %v, %v", ttl, present, err)
	}
	if _, present, err := ParseTTLHeader("  "); err != nil || present {
		t.Fatalf("blank TTL should be absent: %v, %v", present, err)
	}
	if _, _, err := ParseTTLHeader("-5"); err == nil {
		t.Fatal("negative TTL must be rejected")
	}
	if _, _, err := ParseTTLHeader("soon"); err == nil {
		t.Fatal("non-numeric TTL must be rejected")
	}
}
