package pypi

import (
	"regexp"
	"strings"
)

// hrefRe matches all href attributes in HTML anchor tags.
var hrefRe = regexp.MustCompile(`href="([^"]+)"`)

// RewriteURLs rewrites all package download URLs in a PyPI simple index HTML page
// to point through our proxy. Handles both absolute URLs (https://...) and
// relative URLs (../../packages/...).
//
// Examples:
//
//	href="https://files.pythonhosted.org/packages/..." → href="/pypi/files/packages/..."
//	href="../../packages/ab/cd/file.whl#sha256=..." → href="/pypi/files/packages/ab/cd/file.whl#sha256=..."
func RewriteURLs(html string, baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")

	return hrefRe.ReplaceAllStringFunc(html, func(match string) string {
		sub := hrefRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		url := sub[1]

		// Find /packages/ in the URL — this is the key path component
		idx := strings.Index(url, "/packages/")
		if idx < 0 {
			// Also handle relative paths like "../../packages/..."
			idx = strings.Index(url, "packages/")
			if idx < 0 {
				return match
			}
			// Extract from "packages/" onward
			filePath := "/" + url[idx:]
			return `href="` + baseURL + `/pypi/files` + filePath + `"`
		}

		// Absolute or rooted path: extract from /packages/ onward
		filePath := url[idx:]
		return `href="` + baseURL + `/pypi/files` + filePath + `"`
	})
}
