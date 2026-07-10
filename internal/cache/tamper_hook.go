package cache

import "context"

// TamperResult is what Verify returns to the manager. KnownMismatch is
// the only load-bearing bit: true means "a baseline existed and the
// re-fetched hash differs" — the manager must keep the first-seen bytes
// and NOT overwrite storage. All other cases (first sight, match) are
// KnownMismatch=false and the manager proceeds normally.
type TamperResult struct {
	KnownMismatch bool
}

// TamperRecorder is the optional contract the cache Manager consumes
// for content-integrity tracking. The concrete implementation lives in
// internal/tamper; defining the interface here keeps the cache package
// free of any import edge to it (same pattern as SecurityScanner).
//
// Record establishes the first-seen baseline for an immutable artifact
// (idempotent — a second call for a key that already has a baseline is
// a no-op). Verify compares a re-fetched artifact's hash to the
// baseline, records the audit event / fires the alert hook on mismatch,
// and returns whether the manager must protect the first-seen bytes.
type TamperRecorder interface {
	Record(ctx context.Context, key, ecosystem, pkg, version, sha256 string, size int64)
	Verify(ctx context.Context, key, ecosystem, pkg, version, sha256 string, size int64, clientIP string) TamperResult
}
