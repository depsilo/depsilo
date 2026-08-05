package pypi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

const maxIndexChannelBytes = 64

// ChannelFamily exposes one PyPI-compatible upstream family through a channel
// segment, for example /pypi-torch/cpu/simple/ and
// /pypi-torch/rocm6.4/simple/. The public adapter identity stays stable while
// cache keys and signed artifact audiences are isolated per channel.
type ChannelFamily struct {
	prototype    *Handler
	pathPrefix   string
	upstreamRoot string
}

// NewChannelFamily builds a channel-aware extra-index adapter. The upstream
// selector must point at a fixed family root; request data can only contribute
// one validated path segment and can never choose a host, query, or fragment.
func NewChannelFamily(
	cacheMgr *cache.Manager,
	selector upstream.Selector,
	cfg config.CacheConfig,
	database *gorm.DB,
	options Options,
) (*ChannelFamily, error) {
	if !strings.HasPrefix(options.AdapterID, "extra:") {
		return nil, errors.New("channel index family requires a stable extra-index adapter ID")
	}
	prototype, err := newWithOptions(cacheMgr, selector, cfg, database, options)
	if err != nil {
		return nil, err
	}
	return &ChannelFamily{
		prototype:    prototype,
		pathPrefix:   prototype.pathPrefix,
		upstreamRoot: prototype.upstreamSimplePath,
	}, nil
}

// Register mounts the channel-aware form of the normal PyPI route contract.
func (f *ChannelFamily) Register(rg *gin.RouterGroup) {
	rg.GET("/:channel/simple/", f.dispatch((*Handler).handleSimpleIndex))
	rg.GET("/:channel/simple/:package/", f.dispatch((*Handler).handlePackageIndex))
	rg.GET("/:channel/simple/:package", f.dispatch((*Handler).handlePackageRedirect))
	rg.GET("/:channel/files/*filepath", f.dispatch((*Handler).handleFileDownload))
}

func (f *ChannelFamily) dispatch(next func(*Handler, *gin.Context)) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := f.channelFromRequest(c)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    "NOT_FOUND",
				"message": "unknown package index channel",
			})
			return
		}
		handler, ok := f.handlerForChannel(channel)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    "NOT_FOUND",
				"message": "unknown package index channel",
			})
			return
		}
		next(handler, c)
	}
}

func (f *ChannelFamily) channelFromRequest(c *gin.Context) (string, bool) {
	escapedPath := c.Request.URL.EscapedPath()
	marker := f.pathPrefix + "/"
	markerIndex := strings.LastIndex(escapedPath, marker)
	if markerIndex < 0 {
		return "", false
	}
	remainder := escapedPath[markerIndex+len(marker):]
	rawChannel, _, ok := strings.Cut(remainder, "/")
	if !ok || rawChannel == "" || rawChannel != c.Param("channel") {
		return "", false
	}
	return rawChannel, true
}

func (f *ChannelFamily) handlerForChannel(raw string) (*Handler, bool) {
	if !validIndexChannel(raw) {
		return nil, false
	}
	channel := strings.ToLower(raw)
	handler := *f.prototype
	handler.pathPrefix = f.pathPrefix + "/" + channel
	handler.upstreamSimplePath = appendChannelPath(f.upstreamRoot, channel)
	handler.cacheNamespace = channelCacheNamespace(handler.adapterID, channel)
	handler.artifactAudience = channelArtifactAudience(handler.adapterID, channel)
	return &handler, true
}

func validIndexChannel(value string) bool {
	if value == "" || len(value) > maxIndexChannelBytes || value != strings.ToLower(value) {
		return false
	}
	if !asciiChannelAlphaNumeric(value[0]) || !asciiChannelAlphaNumeric(value[len(value)-1]) {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if asciiChannelAlphaNumeric(character) || character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func asciiChannelAlphaNumeric(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
}

func appendChannelPath(root, channel string) string {
	if root == "/" {
		return "/" + channel
	}
	return strings.TrimRight(root, "/") + "/" + channel
}

func channelCacheNamespace(adapterID, channel string) string {
	return adapterID + "/channels/" + channel
}

func channelArtifactAudience(adapterID, channel string) string {
	return adapterID + ":channel:" + channel
}

// ChannelIndexFromCacheKey returns the channel and package encoded by a
// channel-family metadata key. It is used by maintenance code to rebuild the
// corresponding public refresh route without trusting arbitrary DB paths.
func ChannelIndexFromCacheKey(adapterID, key string) (string, string, bool) {
	prefix := adapterID + "/channels/"
	rest, ok := strings.CutPrefix(key, prefix)
	if !ok {
		return "", "", false
	}
	channel, _, ok := strings.Cut(rest, "/")
	if !ok || !validIndexChannel(channel) {
		return "", "", false
	}
	packageName, ok := IndexPackageFromCacheKey(channelCacheNamespace(adapterID, channel), key)
	if !ok {
		return "", "", false
	}
	return channel, packageName, true
}
