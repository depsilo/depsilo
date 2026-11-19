package nuget

import (
	"encoding/json"
	"net"
	"net/url"
	"strings"
)

// RewriteServiceIndex rewrites the NuGet V3 service index JSON so every
// resource the serving Upstream hosts is reached through Depsilo. An @id
// under upstreamURL, the Upstream's configured base URL including its path,
// becomes baseURL + "/nuget" + the rest of its path, which handlePassthrough
// maps back onto the same Upstream URL.
//
// An @id on any other origin is left as it is: Depsilo fetches only paths
// under the Upstream's base, so pointing such a resource at Depsilo would
// break it. nuget.org's search and autocomplete resources live on
// azuresearch-*.nuget.org, and api.nuget.org has no /query or /autocomplete,
// so they stay direct and search keeps working for a client that can reach
// them; package downloads and registrations are on api.nuget.org and go
// through Depsilo.
func RewriteServiceIndex(data []byte, baseURL, upstreamURL string) ([]byte, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	upstreamBase, err := url.Parse(upstreamURL)
	if err != nil {
		upstreamBase = nil
	}

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
			// e.g. "https://api.nuget.org/v3-flatcontainer/" -> "<base>/nuget/v3-flatcontainer/"
			if suffix, under := upstreamResourceSuffix(upstreamBase, id); under {
				rMap["@id"] = baseURL + "/nuget" + suffix
			}
		}
		filtered = append(filtered, res)
	}
	doc["resources"] = filtered

	return json.Marshal(doc)
}

// upstreamResourceSuffix returns the part of id after the Upstream base URL,
// starting with "/" or empty, when id is on the same origin and under the
// base path. A suffix with dot segments is refused, since a client would
// resolve it out of the /nuget route.
func upstreamResourceSuffix(base *url.URL, id string) (string, bool) {
	if base == nil || base.Host == "" {
		return "", false
	}
	parsed, err := url.Parse(id)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.User != nil ||
		!strings.EqualFold(parsed.Scheme, base.Scheme) ||
		!strings.EqualFold(nugetHostPort(parsed), nugetHostPort(base)) {
		return "", false
	}
	basePath := strings.TrimSuffix(base.EscapedPath(), "/")
	idPath := parsed.EscapedPath()
	var suffix string
	switch {
	case idPath == basePath:
	case strings.HasPrefix(idPath, basePath+"/"):
		suffix = idPath[len(basePath):]
	default:
		return "", false
	}
	for _, segment := range strings.Split(suffix, "/") {
		if decoded, err := url.PathUnescape(segment); err != nil || decoded == "." || decoded == ".." {
			return "", false
		}
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		suffix += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		suffix += "#" + parsed.EscapedFragment()
	}
	return suffix, true
}

func nugetHostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}
