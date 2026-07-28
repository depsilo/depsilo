package cache

import (
	"strings"
	"time"

	"depsilo/internal/adapter/packagekey"
)

// tamperEligible applies ecosystem semantics on top of the operator's generic
// TTL threshold. Hugging Face branch and tag files are downloadable artifacts,
// but they are intentionally mutable and must never establish an integrity
// baseline even when an unusually low threshold exceeds their short TTL.
func (m *Manager) tamperEligible(adapterType, key string, ttl time.Duration) bool {
	if !m.isImmutable(ttl) {
		return false
	}
	if !strings.EqualFold(adapterType, "huggingface") {
		return true
	}
	ref := packagekey.ExtractVersion("huggingface", key)
	if len(ref) != 40 {
		return false
	}
	for i := range ref {
		if (ref[i] < '0' || ref[i] > '9') && (ref[i] < 'a' || ref[i] > 'f') {
			return false
		}
	}
	return true
}
