package server

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"depsilo/internal/httpnamespace"
	web "depsilo/web"
)

func registerFrontend(engine *gin.Engine, extraProxyPrefixes ...string) error {
	distFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		return fmt.Errorf("load embedded frontend: %w", err)
	}
	registerFrontendFS(engine, distFS, extraProxyPrefixes...)
	return nil
}

func registerFrontendFS(engine *gin.Engine, distFS fs.FS, extraProxyPrefixes ...string) {
	staticHandler := http.FileServer(http.FS(distFS))
	machinePrefixes := frontendMachinePrefixes(extraProxyPrefixes)

	engine.NoRoute(func(c *gin.Context) {
		requestPath := c.Request.URL.Path

		if unsafeFrontendPath(requestPath) {
			writeFrontendError(c, http.StatusNotFound)
			return
		}

		if requestPath == "/" || embeddedFrontendFileExists(distFS, requestPath) {
			if !frontendReadMethod(c.Request.Method) {
				c.Header("Allow", "GET, HEAD")
				writeFrontendError(c, http.StatusMethodNotAllowed)
				return
			}
			staticHandler.ServeHTTP(c.Writer, c.Request)
			return
		}

		if hasMachinePrefix(requestPath, machinePrefixes) ||
			!frontendBrowserNavigation(c.Request) ||
			frontendPathLooksLikeFile(requestPath) {
			writeFrontendError(c, http.StatusNotFound)
			return
		}

		c.Writer.Header().Add("Vary", "Accept")
		originalPath := c.Request.URL.Path
		originalRawPath := c.Request.URL.RawPath
		c.Request.URL.Path = "/"
		c.Request.URL.RawPath = ""
		defer func() {
			c.Request.URL.Path = originalPath
			c.Request.URL.RawPath = originalRawPath
		}()
		staticHandler.ServeHTTP(c.Writer, c.Request)
	})
}

func embeddedFrontendFileExists(distFS fs.FS, requestPath string) bool {
	filePath := strings.TrimPrefix(requestPath, "/")
	if filePath == "" || filePath == "." {
		return false
	}
	info, err := fs.Stat(distFS, filePath)
	return err == nil && !info.IsDir()
}

func frontendMachinePrefixes(extraProxyPrefixes []string) []string {
	prefixes := httpnamespace.MachineRoots()
	for _, prefix := range extraProxyPrefixes {
		normalized := "/" + strings.Trim(strings.TrimSpace(prefix), "/")
		if normalized != "/" {
			prefixes = append(prefixes, normalized)
		}
	}
	return prefixes
}

func hasMachinePrefix(requestPath string, prefixes []string) bool {
	for _, prefix := range prefixes {
		normalizedPrefix := "/" + strings.Trim(strings.TrimSpace(prefix), "/")
		if strings.EqualFold(requestPath, normalizedPrefix) ||
			len(requestPath) > len(normalizedPrefix) &&
				strings.EqualFold(requestPath[:len(normalizedPrefix)], normalizedPrefix) &&
				requestPath[len(normalizedPrefix)] == '/' {
			return true
		}
	}
	return false
}

func unsafeFrontendPath(requestPath string) bool {
	if requestPath == "" || requestPath[0] != '/' || strings.Contains(requestPath, `\`) {
		return true
	}
	segments := strings.Split(requestPath, "/")
	for index, segment := range segments {
		if segment == "." || segment == ".." {
			return true
		}
		if segment == "" && index > 0 && index < len(segments)-1 {
			return true
		}
	}
	return false
}

func frontendReadMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

func frontendBrowserNavigation(request *http.Request) bool {
	if !frontendReadMethod(request.Method) ||
		request.Header.Get("Range") != "" ||
		!acceptsFrontendHTML(strings.Join(request.Header.Values("Accept"), ",")) {
		return false
	}
	return true
}

func acceptsFrontendHTML(accept string) bool {
	for _, value := range strings.Split(accept, ",") {
		mediaType, parameters, err := mime.ParseMediaType(strings.TrimSpace(value))
		if err != nil || !strings.EqualFold(mediaType, "text/html") {
			continue
		}
		quality := 1.0
		if rawQuality, ok := parameters["q"]; ok {
			quality, err = strconv.ParseFloat(rawQuality, 64)
			if err != nil {
				continue
			}
		}
		if quality > 0 && quality <= 1 {
			return true
		}
	}
	return false
}

func frontendPathLooksLikeFile(requestPath string) bool {
	name := path.Base(requestPath)
	return name != "." && name != "/" && path.Ext(name) != ""
}

func writeFrontendError(c *gin.Context, status int) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(
		status,
		"text/plain; charset=utf-8",
		[]byte(fmt.Sprintf("%d %s\n", status, strings.ToLower(http.StatusText(status)))),
	)
}
