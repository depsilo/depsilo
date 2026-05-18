package rules

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter/packagekey"
)

// Middleware returns a Gin middleware that checks package rules before proxying.
func Middleware(engine *Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Determine ecosystem from path prefix
		path := c.Request.URL.Path
		ecosystem := ""
		switch {
		case strings.HasPrefix(path, "/pypi/"):
			ecosystem = "pypi"
		case strings.HasPrefix(path, "/apt/"):
			ecosystem = "apt"
		case strings.HasPrefix(path, "/npm/"):
			ecosystem = "npm"
		case strings.HasPrefix(path, "/go/"):
			ecosystem = "go"
		case strings.HasPrefix(path, "/crates/"):
			ecosystem = "cargo"
		case strings.HasPrefix(path, "/maven/"):
			ecosystem = "maven"
		case strings.HasPrefix(path, "/rubygems/"):
			ecosystem = "rubygems"
		case strings.HasPrefix(path, "/composer/"):
			ecosystem = "composer"
		case strings.HasPrefix(path, "/nuget/"):
			ecosystem = "nuget"
		case strings.HasPrefix(path, "/conda/"):
			ecosystem = "conda"
		case strings.HasPrefix(path, "/cran/"):
			ecosystem = "cran"
		case strings.HasPrefix(path, "/helm/"):
			ecosystem = "helm"
		default:
			c.Next()
			return
		}

		// Extract a rough package name from the path
		packageName := extractPackageFromPath(ecosystem, path)
		if packageName == "" {
			c.Next()
			return
		}

		allowed, rule, _ := engine.Check(c.Request.Context(), ecosystem, packageName, "")
		if !allowed {
			reason := "Package blocked by policy"
			if rule != nil && rule.Reason != "" {
				reason = rule.Reason
			}
			c.JSON(http.StatusForbidden, gin.H{
				"code":    "PACKAGE_DENIED",
				"message": reason,
				"package": packageName,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func extractPackageFromPath(ecosystem, path string) string {
	switch ecosystem {
	case "pypi":
		if strings.HasPrefix(path, "/pypi/simple/") {
			name := strings.TrimPrefix(path, "/pypi/simple/")
			name = strings.TrimSuffix(name, "/")
			if name != "" && !strings.Contains(name, "/") {
				return name
			}
		}
		if strings.HasPrefix(path, "/pypi/files/") {
			parts := strings.Split(path, "/")
			fname := parts[len(parts)-1]
			if idx := strings.Index(fname, "-"); idx > 0 {
				return fname[:idx]
			}
		}
	case "npm":
		trimmed := strings.TrimPrefix(path, "/npm/")
		if strings.HasPrefix(trimmed, "@") {
			parts := strings.SplitN(trimmed, "/", 3)
			if len(parts) >= 2 {
				return parts[0] + "/" + strings.Split(parts[1], "/")[0]
			}
		}
		parts := strings.SplitN(trimmed, "/", 2)
		if len(parts) >= 1 && parts[0] != "" {
			return parts[0]
		}
	case "maven":
		// Hard to extract from path alone, skip for now
		return ""
	default:
		// For others, use packagekey.ExtractName with a synthetic key
		key := strings.TrimPrefix(path, "/")
		return packagekey.ExtractName(ecosystem, key)
	}
	return ""
}
