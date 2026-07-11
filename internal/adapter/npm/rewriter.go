package npm

import (
	"encoding/json"
	"strings"
)

// RewriteTarballURLs rewrites all dist.tarball URLs in npm packument JSON.
// baseURL examples: "" (for caching), "http://localhost:23333" (for runtime).
func RewriteTarballURLs(data []byte, baseURL string) ([]byte, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	name, _ := doc["name"].(string)
	if name == "" {
		return data, nil
	}

	versions, ok := doc["versions"].(map[string]interface{})
	if !ok {
		return data, nil
	}

	for _, versionData := range versions {
		vMap, ok := versionData.(map[string]interface{})
		if !ok {
			continue
		}
		dist, ok := vMap["dist"].(map[string]interface{})
		if !ok {
			continue
		}
		tarball, ok := dist["tarball"].(string)
		if !ok {
			continue
		}

		// Extract /-/filename.tgz suffix from tarball URL
		idx := strings.LastIndex(tarball, "/-/")
		if idx < 0 {
			continue
		}
		suffix := tarball[idx:] // "/-/express-4.18.2.tgz"

		dist["tarball"] = baseURL + "/npm/" + name + suffix
	}

	return json.Marshal(doc)
}
