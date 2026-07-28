package cache

import "errors"

// StaleFallbackPolicy lets an upstream distinguish a transient failure from an
// authoritative response. Errors are stale-eligible by default so existing
// adapters retain their behavior; implementations return false for results
// such as 401, 403, 404, and 410 where replaying old bytes would be incorrect.
type StaleFallbackPolicy interface {
	error
	AllowStaleFallback() bool
}

func allowStaleFallback(err error) bool {
	var policy StaleFallbackPolicy
	if errors.As(err, &policy) {
		return policy.AllowStaleFallback()
	}
	return true
}
