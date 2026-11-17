package goproxy

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/adapter"
	"depsilo/internal/gomoduleidentity"
)

// sumDBTile matches the tile coordinates of the checksum database protocol:
// <height>/<level>/<index> or <height>/data/<index>, where the index is
// written as three-digit elements ("x123/456") and a partial tile ends in
// ".p/<width>".
var sumDBTile = regexp.MustCompile(`^[0-9]+/(?:[0-9]+|data)/(?:x[0-9]{3}/)*[0-9]{3}(?:\.p/[0-9]+)?$`)

// proxySumDB relays one checksum database request to the module proxy pool,
// as the GOPROXY protocol lets a proxy do. path is what follows "sumdb" in
// the request: /<name>/supported, /latest, /lookup/<module>@<version> or
// /tile/<coordinates>.
//
// The go command verifies signed tree heads and inclusion proofs with the
// database's public key, so the relay validates only the shape of the path.
// A pool member that does not mirror the database answers 404 or 410 and the
// next one is asked; when none does, that answer tells the go command to
// contact the database directly.
func (h *Handler) proxySumDB(c *gin.Context, path string) {
	name, endpoint, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	if !validSumDBName(name) {
		c.Status(http.StatusNotFound)
		return
	}

	cacheKey, ttl, supported := "", h.cfg.TTLIndex, false
	lookup, isLookup := strings.CutPrefix(endpoint, "lookup/")
	tile, isTile := strings.CutPrefix(endpoint, "tile/")
	switch {
	case endpoint == "supported":
		// The go command reads 404 and 410 as "contact the database yourself"
		// and gives up on any other failure. A remembered "supported" must
		// therefore go once no member mirrors the database, and a pool that
		// cannot be asked must not turn into an error.
		cacheKey, supported = SumDBSupportedCacheKey(name), true
	case endpoint == "latest":
		cacheKey = SumDBLatestCacheKey(name)
	case isLookup && validSumDBLookup(lookup):
		// A record never changes, and the tree head its answer ends in stays
		// verifiable however old it gets -> long TTL. Two answers still
		// differ in bytes; the cache classifies every checksum database key
		// as metadata, so a refresh replaces the copy and no tamper baseline
		// is taken.
		cacheKey, ttl = SumDBLookupCacheKey(name, lookup), h.cfg.TTLBlob
	case isTile && sumDBTile.MatchString(tile):
		// Tiles never change once written -> long TTL
		cacheKey, ttl = SumDBTileCacheKey(name, tile), h.cfg.TTLBlob
	default:
		c.Status(http.StatusNotFound)
		return
	}

	h.proxy.Serve(c, adapter.TransparentPlan{
		Path:             "sumdb/" + name + "/" + endpoint,
		CacheKey:         cacheKey,
		TTL:              ttl,
		Optional:         true,
		PresenceIsAnswer: supported,
	})

	// The go command asks "supported" once per run and then expects every
	// tree head, record and tile from here. One that cannot be served shows
	// the remembered answer may no longer hold (the mirroring member is down
	// and the others lack the database), so the next run asks the pool again
	// instead of failing the same way until the copy expires.
	if !supported && c.Writer.Status() != http.StatusOK {
		if err := h.cache.Invalidate(c.Request.Context(), SumDBSupportedCacheKey(name)); err != nil {
			zap.L().Warn("drop remembered checksum database support failed",
				zap.String("sumdb", name),
				zap.Error(err),
			)
		}
	}
}

// validSumDBName reports whether name is a plain host name, which is what
// names a checksum database (sum.golang.org).
func validSumDBName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// sumDBVersion matches the versions a checksum database records, in the
// escaped spelling: canonical semantic versions, pseudo-versions included.
// Nothing in it needs URL escaping, so the forwarded path is the checked one.
var sumDBVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9a-z!.-]+)?(\+incompatible)?$`)

// validSumDBLookup reports whether lookup is <module>@<version> in the
// escaped spelling the go command sends.
func validSumDBLookup(lookup string) bool {
	module, version, ok := strings.Cut(lookup, "@")
	if !ok || !sumDBVersion.MatchString(version) {
		return false
	}
	if _, err := gomoduleidentity.DecodeProxyPath(module); err != nil {
		return false
	}
	_, err := gomoduleidentity.DecodeProxyVersion(version)
	return err == nil
}
