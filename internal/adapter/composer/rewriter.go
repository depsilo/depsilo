package composer

import (
	"encoding/json"
	"strings"
)

// RewritePackagesJSON rewrites the metadata-url in packages.json to point to our proxy.
func RewritePackagesJSON(data []byte, baseURL string) ([]byte, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	// Rewrite metadata-url to point to our proxy
	doc["metadata-url"] = baseURL + "/composer/p2/%package%.json"

	// Inject a preferred dist mirror so composer downloads dist
	// archives through the proxy instead of going straight to the
	// host the p2 metadata points at (GitHub for packagist). This is
	// the standard Composer mirror mechanism (packagist.jp does the
	// same); composer tries mirrors in order and falls back to the
	// original dist URL when the mirror fails, so injecting it never
	// makes an install less available. Without it, dist traffic
	// bypasses the proxy entirely — no caching, no quarantine.
	//
	// ENFORCEMENT CAVEAT: the fallback cuts both ways. Composer
	// treats ANY mirror error — including the quarantine gate's 451
	// — as "mirror failed, trying the next URL" and downloads the
	// original dist directly (FileDownloader keeps the origin URL
	// after preferred mirrors). Unlike npm/pypi/cargo, where the URL
	// rewrite leaves the client no origin to fall back to, the
	// composer gate therefore provides caching, audit events and
	// best-effort blocking, but NOT hard enforcement on its own.
	// Hard enforcement needs egress control (proxy-only network) or
	// filtering quarantined versions out of the p2 metadata — a
	// product decision tracked in CLAUDE.md §11.4.
	doc["mirrors"] = []map[string]interface{}{
		{
			"dist-url":  baseURL + "/composer/dist/%package%/%version%/%reference%.%type%",
			"preferred": true,
		},
	}

	return json.Marshal(doc)
}
