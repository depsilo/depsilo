package pypi

import (
	"regexp"
	"strings"
)

var hrefRe = regexp.MustCompile(`href="([^"]+)"`)

// RewriteURLs rewrites download URLs in PyPI simple index HTML.
// pathPrefix is the route prefix (e.g., "/pypi" or "/pypi-torch-cu130").
func RewriteURLs(html string, baseURL string, pathPrefix string) string {
	baseURL = strings.TrimRight(baseURL, "/")

	return hrefRe.ReplaceAllStringFunc(html, func(match string) string {
		sub := hrefRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		url := sub[1]

		idx := strings.Index(url, "/packages/")
		if idx < 0 {
			idx = strings.Index(url, "packages/")
			if idx < 0 {
				return match
			}
			filePath := "/" + url[idx:]
			return `href="` + baseURL + pathPrefix + `/files` + filePath + `"`
		}

		filePath := url[idx:]
		return `href="` + baseURL + pathPrefix + `/files` + filePath + `"`
	})
}
