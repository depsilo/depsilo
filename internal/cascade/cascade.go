// Package cascade defines the wire contract shared by Depsilo instances that
// relay upstream fetches through a parent cache node.
//
// A cascade peer does not receive rewritten, client-facing protocol responses.
// The child sends the exact upstream request it would otherwise send itself
// (absolute URL plus safe request headers) to the parent's relay endpoint. The
// parent fetches that URL, caches the raw response, and streams it back. Every
// protocol adapter therefore keeps its normal rewriting, provenance, and
// policy behavior regardless of how many cache levels the bytes traversed.
package cascade

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// RelayPath is the authenticated parent endpoint that serves raw upstream
	// exchanges. It is deliberately outside every package-protocol namespace so
	// it can never be confused with a client-facing route.
	RelayPath = "/_depsilo/relay/v1"

	// HeaderTarget carries the absolute upstream URL the child wants fetched.
	HeaderTarget = "X-Depsilo-Relay-Target"
	// HeaderCache reports HIT, MISS, or BYPASS for the parent cache decision.
	HeaderCache = "X-Depsilo-Relay-Cache"
	// HeaderTTL carries the freshness budget the child uses for the response.
	// The parent clamps it to its configured maximum.
	HeaderTTL = "X-Depsilo-Relay-TTL"
	// HeaderSource names the child's upstream origin (base URL). A parent that
	// has the same origin configured can reuse that upstream's own egress
	// client, which is what keeps three-or-more-level chains connected. The
	// parent only honors it for targets on that exact origin.
	HeaderSource = "X-Depsilo-Relay-Source"
	// HeaderKind tells the parent whether the child treats this entry as
	// mutable index metadata or an immutable artifact. The parent uses the same
	// distinction for its own staleness policy.
	HeaderKind = "X-Depsilo-Relay-Kind"
	// HeaderToken authenticates the requesting instance.
	HeaderToken = "X-Depsilo-Cascade-Token"
	// HeaderChain lists the instance IDs already traversed, oldest first, and
	// always ends with the direct sender. It is both loop detection and the
	// bounded recursion budget.
	HeaderChain = "X-Depsilo-Cascade-Chain"
	// HeaderInstance identifies the relaying instance in relay responses.
	HeaderInstance = "X-Depsilo-Instance"

	CacheHit    = "HIT"
	CacheMiss   = "MISS"
	CacheBypass = "BYPASS"

	// KindIndex marks mutable protocol metadata; the parent refreshes it
	// synchronously on expiry.
	KindIndex = "index"
	// KindArtifact marks immutable content; the parent may serve it stale while
	// refreshing in the background.
	KindArtifact = "artifact"

	// DefaultMaxHops bounds relay recursion for deployments that leave the
	// setting unset. The value counts relay nodes, not client requests.
	DefaultMaxHops = 4
	// MaxChainEntries bounds the parsed chain independently of configuration so
	// a hostile peer cannot force unbounded work or response headers.
	MaxChainEntries = 16
	// MaxInstanceIDLength bounds one chain element.
	MaxInstanceIDLength = 64
	// MaxTargetURLLength leaves room for signed artifact URLs while staying
	// below common reverse-proxy header limits once the rest of the request is
	// accounted for.
	MaxTargetURLLength = 8 << 10
)

var (
	errEmptyInstanceID = errors.New("cascade instance ID is empty")
	errBadInstanceID   = errors.New("cascade instance ID has invalid characters")
	errChainTooLong    = errors.New("cascade chain is too long")
)

// NewInstanceID returns a fresh random instance identifier. The value is
// stored durably by the composition root; it is never derived from the
// hostname, database path, or another deployment attribute.
func NewInstanceID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate cascade instance ID: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// ValidInstanceID reports whether an operator-supplied or peer-supplied
// instance identifier is well formed.
func ValidInstanceID(id string) bool {
	if id == "" || len(id) > MaxInstanceIDLength {
		return false
	}
	for _, char := range id {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= '0' && char <= '9':
		case char == '-':
		default:
			return false
		}
	}
	return true
}

// Chain is an ordered list of instance IDs a relay request has traversed. The
// last element is the instance that sent the request.
type Chain []string

// ParseChain decodes the comma-separated chain header. An empty header is a
// valid chain for callers that dial a peer without an inbound chain.
func ParseChain(raw string) (Chain, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > MaxChainEntries {
		return nil, errChainTooLong
	}
	chain := make(Chain, 0, len(parts))
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" {
			return nil, errEmptyInstanceID
		}
		if !ValidInstanceID(id) {
			return nil, errBadInstanceID
		}
		chain = append(chain, id)
	}
	return chain, nil
}

// String encodes the chain for the wire header.
func (c Chain) String() string { return strings.Join(c, ",") }

// Contains reports whether id already appears in the chain.
func (c Chain) Contains(id string) bool {
	for _, entry := range c {
		if entry == id {
			return true
		}
	}
	return false
}

// With returns a copy of the chain with id appended. The caller must have
// already validated the resulting length against the configured hop budget.
func (c Chain) With(id string) Chain {
	next := make(Chain, 0, len(c)+1)
	next = append(next, c...)
	return append(next, id)
}

// Clone returns a caller-owned copy so context values cannot be mutated by a
// later request.
func (c Chain) Clone() Chain {
	if c == nil {
		return nil
	}
	return append(Chain(nil), c...)
}

type chainContextKey struct{}

// WithChain stores the chain that outbound peer requests must send. The value
// already includes the local instance because the relay endpoint appends it
// while handling an inbound request.
func WithChain(ctx context.Context, chain Chain) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if chain == nil {
		return ctx
	}
	return context.WithValue(ctx, chainContextKey{}, chain.Clone())
}

// ChainFrom returns the propagated chain, if any.
func ChainFrom(ctx context.Context) Chain {
	if ctx == nil {
		return nil
	}
	chain, _ := ctx.Value(chainContextKey{}).(Chain)
	return chain
}

type fetchTTLContextKey struct{}

// WithFetchTTL records the freshness budget the surrounding cache entry uses.
// Peer transports forward it so the parent cache expires at the same semantic
// boundary as the child instead of guessing from file extensions.
func WithFetchTTL(ctx context.Context, ttl time.Duration) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if ttl <= 0 {
		return ctx
	}
	return context.WithValue(ctx, fetchTTLContextKey{}, ttl)
}

// FetchTTLFrom returns the freshness budget recorded by the cache manager.
func FetchTTLFrom(ctx context.Context) (time.Duration, bool) {
	if ctx == nil {
		return 0, false
	}
	ttl, ok := ctx.Value(fetchTTLContextKey{}).(time.Duration)
	if !ok || ttl <= 0 {
		return 0, false
	}
	return ttl, true
}

type fetchKindContextKey struct{}

// WithFetchKind records whether the surrounding cache entry is mutable index
// metadata or immutable artifact content.
func WithFetchKind(ctx context.Context, kind string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if kind != KindIndex && kind != KindArtifact {
		return ctx
	}
	return context.WithValue(ctx, fetchKindContextKey{}, kind)
}

// FetchKindFrom returns the recorded cache kind.
func FetchKindFrom(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	kind, ok := ctx.Value(fetchKindContextKey{}).(string)
	return kind, ok
}

// ParseTTLHeader decodes the seconds value carried by HeaderTTL.
func ParseTTLHeader(raw string) (time.Duration, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seconds < 0 {
		return 0, false, errors.New("invalid relay TTL")
	}
	return time.Duration(seconds) * time.Second, true, nil
}

// FormatTTLHeader encodes a non-negative duration as whole seconds.
func FormatTTLHeader(ttl time.Duration) string {
	if ttl <= 0 {
		return "0"
	}
	return strconv.FormatInt(int64(ttl/time.Second), 10)
}
