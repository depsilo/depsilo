package nuget

import (
	"encoding/json"
	"strings"
)

// RewriteServiceIndex rewrites the NuGet V3 service index JSON,
// replacing upstream @id field values with proxy URLs.
func RewriteServiceIndex(data []byte, baseURL string) ([]byte, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	resources, ok := doc["resources"].([]interface{})
	if !ok {
		return data, nil
	}

	filtered := make([]interface{}, 0, len(resources))
	for _, res := range resources {
		rMap, ok := res.(map[string]interface{})
		if !ok {
			filtered = append(filtered, res)
			continue
		}
		// Drop RepositorySignatures resources entirely. NuGet clients
		// enforce HTTPS on these (NU1301) which conflicts with Depsilo
		// serving plain HTTP. The resource is optional — when absent the
		// client simply skips repository signature verification, which is
		// the correct behaviour for a caching proxy that doesn't sign.
		if t, ok := rMap["@type"].(string); ok && strings.HasPrefix(t, "RepositorySignatures") {
			continue
		}
		if id, ok := rMap["@id"].(string); ok {
			// Replace upstream base with proxy base
			// e.g. "https://api.nuget.org/v3/..." -> "/nuget/v3/..."
			for _, prefix := range []string{
				"https://api.nuget.org",
				"https://azuresearch-usnc.nuget.org",
				"https://azuresearch-ussc.nuget.org",
			} {
				if strings.HasPrefix(id, prefix) {
					rMap["@id"] = baseURL + "/nuget" + strings.TrimPrefix(id, prefix)
					break
				}
			}
		}
		filtered = append(filtered, res)
	}
	doc["resources"] = filtered

	return json.Marshal(doc)
}
