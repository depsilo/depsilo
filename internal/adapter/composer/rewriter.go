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

	return json.Marshal(doc)
}
