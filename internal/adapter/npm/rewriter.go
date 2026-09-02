package npm

import (
	"encoding/json"
	"strings"
)

// RewriteTarballURLs preserves the legacy pure URL-transformation interface
// used by export/tooling callers. The npm proxy itself uses PreparePackument so
// its cache retains the exact source-bound artifact reference; this unsigned
// output is never a fetchable Depsilo artifact route.
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
