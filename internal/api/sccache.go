package api

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/compilecache"
)

const (
	methodPropfind          = "PROPFIND"
	methodMkcol             = "MKCOL"
	sccacheCheckPath        = ".sccache_check"
	sccacheProbeBody        = "Hello, World!"
	maxWebDAVRequestBody    = int64(16 << 10)
	webDAVCollectionModTime = "Thu, 01 Jan 1970 00:00:00 GMT"
)

// SCCacheHandler implements only the WebDAV operations used by sccache's
// OpenDAL backend. It deliberately is not a general-purpose WebDAV server:
// virtual directories and the startup probe stay in this Adapter, while only
// validated 64-byte artifact identities enter the shared cache Module.
type SCCacheHandler struct {
	enabled    bool
	service    *compilecache.Service
	authorizer *compilecache.Authorizer
}

// NewSCCacheHandler constructs the narrow WebDAV Adapter used by sccache.
func NewSCCacheHandler(enabled bool, service *compilecache.Service, authorizer *compilecache.Authorizer) *SCCacheHandler {
	return &SCCacheHandler{enabled: enabled, service: service, authorizer: authorizer}
}

// Handle serves one sccache WebDAV data-plane request.
func (h *SCCacheHandler) Handle(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	operation := strings.ToLower(c.Request.Method)
	switch c.Request.Method {
	case http.MethodHead, http.MethodGet, http.MethodPut, methodPropfind, methodMkcol:
	default:
		operation = "other"
	}
	started := time.Now()
	defer func() {
		M.CompileCacheDuration.WithLabelValues(string(compilecache.ProtocolSCCache), operation).Observe(time.Since(started).Seconds())
	}()

	if (c.Request.Method == http.MethodPut || c.Request.Method == methodPropfind) && h.enabled && h.service != nil {
		finishDeadline := h.installUploadDeadline(c)
		defer finishDeadline()
	}
	if (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) && h.enabled && h.service != nil {
		finishDeadline := h.installDownloadDeadline(c)
		defer finishDeadline()
	}

	if !h.enabled || h.service == nil || h.authorizer == nil {
		h.record(operation, "disabled")
		preventRequestBodyDrain(c)
		c.Status(http.StatusNotFound)
		return
	}
	namespace, err := compilecache.NormalizeNamespace(c.Param("namespace"))
	if err != nil {
		h.protocolError(c, operation, http.StatusBadRequest, "invalid namespace")
		return
	}
	write := c.Request.Method == http.MethodPut || c.Request.Method == methodMkcol
	if _, err := h.authorizer.Authenticate(c.Request.Context(), c.GetHeader("Authorization"), namespace, write); err != nil {
		switch {
		case errors.Is(c.Request.Context().Err(), context.DeadlineExceeded):
			h.protocolError(c, operation, http.StatusRequestTimeout, "compiler-cache request timed out")
		case errors.Is(err, compilecache.ErrAuthUnavailable):
			h.protocolError(c, operation, http.StatusServiceUnavailable, "credential service is unavailable")
		case errors.Is(err, compilecache.ErrForbidden):
			h.protocolError(c, operation, http.StatusForbidden, "credential does not grant this access")
		default:
			c.Header("WWW-Authenticate", `Bearer realm="depsilo-compile-cache"`)
			h.protocolError(c, operation, http.StatusUnauthorized, "invalid or expired credential")
		}
		return
	}

	rawPath := strings.TrimPrefix(c.Param("path"), "/")
	if rawPath == sccacheCheckPath {
		h.handleCheck(c, operation)
		return
	}
	if c.Request.Method == methodPropfind || c.Request.Method == methodMkcol {
		h.handleCollection(c, rawPath, operation)
		return
	}
	id, err := compilecache.ParseSCCacheArtifact(namespace, rawPath)
	if err != nil {
		h.protocolError(c, operation, http.StatusBadRequest, "invalid sccache artifact path")
		return
	}
	if (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) && c.Request.ContentLength != 0 {
		h.protocolError(c, operation, http.StatusBadRequest, "request body is not allowed")
		return
	}
	switch c.Request.Method {
	case http.MethodHead:
		h.head(c, id)
	case http.MethodGet:
		h.get(c, id)
	case http.MethodPut:
		h.put(c, id)
	default:
		c.Header("Allow", "HEAD, GET, PUT, PROPFIND, MKCOL")
		h.protocolError(c, operation, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *SCCacheHandler) handleCheck(c *gin.Context, operation string) {
	switch c.Request.Method {
	case http.MethodHead:
		if c.Request.ContentLength != 0 {
			h.protocolError(c, operation, http.StatusBadRequest, "request body is not allowed")
			return
		}
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Length", strconv.Itoa(len(sccacheProbeBody)))
		h.record(operation, "hit")
		c.Status(http.StatusOK)
	case http.MethodGet:
		if c.Request.ContentLength != 0 {
			h.protocolError(c, operation, http.StatusBadRequest, "request body is not allowed")
			return
		}
		h.record(operation, "hit")
		M.CompileCacheBytes.WithLabelValues(string(compilecache.ProtocolSCCache), "served").Add(float64(len(sccacheProbeBody)))
		c.Data(http.StatusOK, "application/octet-stream", []byte(sccacheProbeBody))
	case http.MethodPut:
		if c.Request.ContentLength < 0 {
			h.protocolError(c, operation, http.StatusLengthRequired, "Content-Length is required")
			return
		}
		if c.Request.ContentLength > int64(len(sccacheProbeBody)) {
			h.protocolError(c, operation, http.StatusRequestEntityTooLarge, "invalid sccache check body")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(len(sccacheProbeBody))+1)
		body, err := io.ReadAll(c.Request.Body)
		if err != nil || string(body) != sccacheProbeBody || int64(len(body)) != c.Request.ContentLength {
			h.protocolError(c, operation, http.StatusBadRequest, "invalid sccache check body")
			return
		}
		if err := h.service.CheckWritable(c.Request.Context()); err != nil {
			h.serviceError(c, operation, err)
			return
		}
		h.record(operation, "accepted")
		c.Status(http.StatusNoContent)
	default:
		c.Header("Allow", "HEAD, GET, PUT")
		h.protocolError(c, operation, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *SCCacheHandler) handleCollection(c *gin.Context, rawPath, operation string) {
	canonical, ok := canonicalSCCacheDirectory(rawPath)
	if !ok {
		h.protocolError(c, operation, http.StatusBadRequest, "invalid sccache collection path")
		return
	}
	switch c.Request.Method {
	case methodPropfind:
		if c.GetHeader("Depth") != "0" {
			h.protocolError(c, operation, http.StatusBadRequest, "only WebDAV Depth: 0 is supported")
			return
		}
		if c.Request.ContentLength > maxWebDAVRequestBody {
			h.protocolError(c, operation, http.StatusRequestEntityTooLarge, "WebDAV request body is too large")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWebDAVRequestBody)
		if _, err := io.Copy(io.Discard, c.Request.Body); err != nil {
			h.protocolError(c, operation, http.StatusBadRequest, "invalid WebDAV request body")
			return
		}
		h.record(operation, "collection")
		c.Data(http.StatusMultiStatus, "application/xml; charset=utf-8", webDAVCollectionResponse(c.Request.URL.EscapedPath(), canonical))
	case methodMkcol:
		if c.Request.ContentLength != 0 {
			h.protocolError(c, operation, http.StatusBadRequest, "MKCOL request body is not supported")
			return
		}
		h.record(operation, "created")
		c.Status(http.StatusCreated)
	}
}

func canonicalSCCacheDirectory(raw string) (string, bool) {
	path := strings.Trim(raw, "/")
	if path == "" {
		return "", true
	}
	parts := strings.Split(path, "/")
	if len(parts) > 3 {
		return "", false
	}
	for _, part := range parts {
		if len(part) != 1 || !strings.ContainsRune("0123456789abcdef", rune(part[0])) {
			return "", false
		}
	}
	return strings.Join(parts, "/") + "/", true
}

func webDAVCollectionResponse(requestPath, canonical string) []byte {
	href := requestPath
	if href == "" {
		href = "/"
	}
	if canonical != "" && !strings.HasSuffix(href, "/") {
		href += "/"
	}
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(href))
	return []byte(`<?xml version="1.0" encoding="utf-8"?>` +
		`<D:multistatus xmlns:D="DAV:"><D:response><D:href>` + escaped.String() + `</D:href>` +
		`<D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype>` +
		`<D:getlastmodified>` + webDAVCollectionModTime + `</D:getlastmodified><D:getcontentlength>0</D:getcontentlength>` +
		`</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response></D:multistatus>`)
}

func (h *SCCacheHandler) head(c *gin.Context, id compilecache.ArtifactID) {
	size, err := h.service.Stat(c.Request.Context(), id)
	if err != nil {
		h.serviceError(c, "head", err)
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	h.record("head", "hit")
	c.Status(http.StatusOK)
}

func (h *SCCacheHandler) get(c *gin.Context, id compilecache.ArtifactID) {
	entry, err := h.service.Open(c.Request.Context(), id)
	if err != nil {
		h.serviceError(c, "get", err)
		return
	}
	closed := make(chan struct{})
	stopClose := context.AfterFunc(c.Request.Context(), func() {
		_ = entry.Body.Close()
		close(closed)
	})
	defer func() {
		if !stopClose() {
			<-closed
		}
		if err := entry.Body.Close(); err != nil {
			zap.L().Debug("close sccache response", zap.Error(err))
		}
	}()
	h.record("get", "hit")
	M.CompileCacheBytes.WithLabelValues(string(compilecache.ProtocolSCCache), "served").Add(float64(entry.Size))
	c.DataFromReader(http.StatusOK, entry.Size, "application/octet-stream", entry.Body, nil)
}

func (h *SCCacheHandler) put(c *gin.Context, id compilecache.ArtifactID) {
	size := c.Request.ContentLength
	if size < 0 {
		h.protocolError(c, "put", http.StatusLengthRequired, "Content-Length is required")
		return
	}
	if size > h.service.MaxEntryBytes() {
		h.protocolError(c, "put", http.StatusRequestEntityTooLarge, "entry exceeds max_entry_size_mb")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.service.MaxEntryBytes()+1)
	result, err := h.service.Put(c.Request.Context(), id, c.Request.Body, size)
	if err != nil {
		h.serviceError(c, "put", err)
		return
	}
	M.CompileCacheBytes.WithLabelValues(string(compilecache.ProtocolSCCache), "stored").Add(float64(result.Size))
	if result.Created {
		h.record("put", "created")
		c.Status(http.StatusCreated)
		return
	}
	h.record("put", "overwritten")
	c.Status(http.StatusNoContent)
}

func (h *SCCacheHandler) installUploadDeadline(c *gin.Context) func() {
	uploadCtx, cancel := context.WithTimeout(c.Request.Context(), h.service.UploadTimeout())
	c.Request = c.Request.WithContext(uploadCtx)
	controller := http.NewResponseController(c.Writer)
	body := c.Request.Body
	finished := make(chan struct{})
	stop := context.AfterFunc(uploadCtx, func() {
		_ = controller.SetReadDeadline(time.Now())
		_ = body.Close()
		close(finished)
	})
	return func() {
		if !stop() {
			<-finished
		}
		cancel()
	}
}

func (h *SCCacheHandler) installDownloadDeadline(c *gin.Context) func() {
	downloadCtx, cancel := context.WithTimeout(c.Request.Context(), h.service.DownloadTimeout())
	c.Request = c.Request.WithContext(downloadCtx)
	controller := http.NewResponseController(c.Writer)
	finished := make(chan struct{})
	stop := context.AfterFunc(downloadCtx, func() {
		_ = controller.SetWriteDeadline(time.Now())
		close(finished)
	})
	return func() {
		if !stop() {
			<-finished
		}
		cancel()
	}
}

func (h *SCCacheHandler) serviceError(c *gin.Context, operation string, err error) {
	switch {
	case errors.Is(err, compilecache.ErrNotFound):
		h.protocolError(c, operation, http.StatusNotFound, "cache miss")
	case errors.Is(err, compilecache.ErrInvalidKey), errors.Is(err, compilecache.ErrInvalidNamespace),
		errors.Is(err, compilecache.ErrInvalidProtocol), errors.Is(err, compilecache.ErrInvalidSize),
		errors.Is(err, compilecache.ErrSizeMismatch):
		h.protocolError(c, operation, http.StatusBadRequest, "invalid cache entry")
	case errors.Is(err, compilecache.ErrTooLarge):
		h.protocolError(c, operation, http.StatusRequestEntityTooLarge, "entry is too large")
	case errors.Is(err, compilecache.ErrInsufficientStorage):
		h.protocolError(c, operation, http.StatusInsufficientStorage, "compiler cache is full")
	case errors.Is(err, compilecache.ErrUploadBusy):
		c.Header("Retry-After", "1")
		h.protocolError(c, operation, http.StatusServiceUnavailable, "compiler-cache upload queue is full")
	case errors.Is(err, compilecache.ErrDownloadBusy):
		c.Header("Retry-After", "1")
		h.protocolError(c, operation, http.StatusServiceUnavailable, "compiler-cache download concurrency is full")
	case errors.Is(err, context.DeadlineExceeded):
		h.protocolError(c, operation, http.StatusRequestTimeout, "compiler-cache request timed out")
	case errors.Is(err, context.Canceled):
		h.protocolError(c, operation, http.StatusServiceUnavailable, "compiler-cache request was canceled")
	default:
		zap.L().Warn("sccache operation failed", zap.String("operation", operation), zap.Error(err))
		h.protocolError(c, operation, http.StatusServiceUnavailable, "compiler-cache storage is unavailable")
	}
}

func (h *SCCacheHandler) protocolError(c *gin.Context, operation string, status int, message string) {
	preventRequestBodyDrain(c)
	h.record(operation, resultForStatus(status))
	c.Data(status, "text/plain; charset=utf-8", []byte(message+"\n"))
}

func (h *SCCacheHandler) record(operation, result string) {
	M.CompileCacheRequests.WithLabelValues(string(compilecache.ProtocolSCCache), operation, result).Inc()
}
