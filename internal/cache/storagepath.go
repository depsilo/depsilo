package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// maxStorageSegment bounds a derived path segment. Most filesystems cap a
// single component at 255 bytes and escaping can triple a segment, so longer
// segments fall back to a digest instead of failing the write.
const maxStorageSegment = 200

// storagePath maps a logical cache key onto the path the storage backend uses
// for it.
//
// Cache keys are case-sensitive identities, but a case-folding root (macOS and
// Windows mounts) resolves `npm-exact-v1/Express/metadata.json` and
// `npm-exact-v1/express/metadata.json` to a single file, so the second writer
// silently replaces the first object. On such a root every byte outside
// [a-z0-9._-] is percent-escaped — uppercase hex inside the escape is the only
// place case survives, and it always occupies the same position — which keeps
// the mapping injective after case folding. Over-long segments collapse to a
// digest, `~`-prefixed so it can never collide with an escaped segment.
//
// Lowercase keys, which dominate the cache, are returned unchanged, and a
// case-sensitive root keeps the historical layout for every key: the bare key
// is already unique there and rewriting it would orphan existing objects.
func storagePath(key string, foldsCase bool) string {
	if !foldsCase || key == "" {
		return key
	}
	segments := strings.Split(key, "/")
	for index, segment := range segments {
		segments[index] = storagePathSegment(segment)
	}
	return strings.Join(segments, "/")
}

func storagePathSegment(segment string) string {
	if storageSegmentSafe(segment) {
		return segment
	}
	escaped := escapeStorageSegment(segment)
	if len(escaped) <= maxStorageSegment {
		return escaped
	}
	digest := sha256.Sum256([]byte(segment))
	return "~" + hex.EncodeToString(digest[:])
}

// storageKeyFromPath recovers the logical key behind a listed object path. It
// reports false when the path cannot be reversed — a storage path segment
// replaced by a digest, which only happens for keys whose escaped form
// overflows the component limit — so callers can fall back to the storage
// path instead of inventing a key.
func storageKeyFromPath(path string, foldsCase bool) (string, bool) {
	if !foldsCase || path == "" {
		return path, true
	}
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		decoded, ok := unescapeStorageSegment(segment)
		if !ok {
			return "", false
		}
		segments[index] = decoded
	}
	return strings.Join(segments, "/"), true
}

func unescapeStorageSegment(segment string) (string, bool) {
	if !strings.Contains(segment, "%") {
		// An unescaped segment is either safe as-is or a digest placeholder.
		return segment, !strings.HasPrefix(segment, "~")
	}
	var builder strings.Builder
	builder.Grow(len(segment))
	for index := 0; index < len(segment); index++ {
		char := segment[index]
		if char != '%' {
			if !storageSegmentByteSafe(char) {
				return "", false
			}
			builder.WriteByte(char)
			continue
		}
		if index+2 >= len(segment) {
			return "", false
		}
		high, highOK := storageEscapeValue(segment[index+1])
		low, lowOK := storageEscapeValue(segment[index+2])
		if !highOK || !lowOK {
			return "", false
		}
		builder.WriteByte(high<<4 | low)
		index += 2
	}
	return builder.String(), true
}

func storageEscapeValue(char byte) (byte, bool) {
	switch {
	case char >= '0' && char <= '9':
		return char - '0', true
	case char >= 'A' && char <= 'F':
		return char - 'A' + 10, true
	default:
		return 0, false
	}
}

// storageSegmentSafe reports whether a segment survives case folding: it is
// non-empty and every byte is identical in the folded form.
func storageSegmentSafe(segment string) bool {
	if segment == "" {
		return false
	}
	for index := 0; index < len(segment); index++ {
		if !storageSegmentByteSafe(segment[index]) {
			return false
		}
	}
	return true
}

func storageSegmentByteSafe(char byte) bool {
	switch {
	case char >= 'a' && char <= 'z':
		return true
	case char >= '0' && char <= '9':
		return true
	default:
		return char == '.' || char == '-' || char == '_'
	}
}

const storageEscapeHex = "0123456789ABCDEF"

func escapeStorageSegment(segment string) string {
	var builder strings.Builder
	builder.Grow(len(segment))
	for index := 0; index < len(segment); index++ {
		char := segment[index]
		if storageSegmentByteSafe(char) {
			builder.WriteByte(char)
			continue
		}
		builder.WriteByte('%')
		builder.WriteByte(storageEscapeHex[char>>4])
		builder.WriteByte(storageEscapeHex[char&0x0f])
	}
	return builder.String()
}
