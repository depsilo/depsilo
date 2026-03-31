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

	for _, res := range resources {
		rMap, ok := res.(map[string]interface{})
		if !ok {
			continue
		}
		id, ok := rMap["@id"].(string)
		if !ok {
			continue
		}
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

	return json.Marshal(doc)
}
