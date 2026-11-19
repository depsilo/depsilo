package nuget

import (
	"encoding/json"
	"testing"
)

func rewrittenIDs(t *testing.T, input, upstreamURL string) map[string]string {
	t.Helper()
	result, err := RewriteServiceIndex([]byte(input), "http://localhost:8080", upstreamURL)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Version   string `json:"version"`
		Resources []struct {
			ID   string `json:"@id"`
			Type string `json:"@type"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(result, &document); err != nil {
		t.Fatalf("decode rewritten index %s: %v", result, err)
	}
	if document.Version != "3.0.0" {
		t.Fatalf("version = %q, want it preserved", document.Version)
	}
	ids := make(map[string]string, len(document.Resources))
	for _, resource := range document.Resources {
		ids[resource.Type] = resource.ID
	}
	return ids
}

func TestRewriteServiceIndexMapsResourcesUnderTheUpstreamBase(t *testing.T) {
	t.Parallel()
	const nugetOrg = `{"version":"3.0.0","resources":[
		{"@id":"https://api.nuget.org/v3-flatcontainer/","@type":"PackageBaseAddress/3.0.0"},
		{"@id":"https://api.nuget.org/v3/registration5-gz-semver2/","@type":"RegistrationsBaseUrl/3.6.0"},
		{"@id":"https://azuresearch-usnc.nuget.org/query","@type":"SearchQueryService"},
		{"@id":"https://azuresearch-ussc.nuget.org/autocomplete","@type":"SearchAutocompleteService"},
		{"@id":"https://www.nuget.org/packages/{id}/{version}/ReportAbuse","@type":"ReportAbuseUriTemplate/3.0.0"},
		{"@id":"https://api.nuget.org/v3-index/repository-signatures/5.0.0/index.json","@type":"RepositorySignatures/5.0.0"}
	]}`
	ids := rewrittenIDs(t, nugetOrg, "https://api.nuget.org")
	for resourceType, want := range map[string]string{
		"PackageBaseAddress/3.0.0":   "http://localhost:8080/nuget/v3-flatcontainer/",
		"RegistrationsBaseUrl/3.6.0": "http://localhost:8080/nuget/v3/registration5-gz-semver2/",
		// Search is on another origin that the Upstream cannot reach as
		// api.nuget.org/query, so it stays direct and keeps working.
		"SearchQueryService":           "https://azuresearch-usnc.nuget.org/query",
		"SearchAutocompleteService":    "https://azuresearch-ussc.nuget.org/autocomplete",
		"ReportAbuseUriTemplate/3.0.0": "https://www.nuget.org/packages/{id}/{version}/ReportAbuse",
	} {
		if got := ids[resourceType]; got != want {
			t.Errorf("%s @id = %q, want %q", resourceType, got, want)
		}
	}
	if _, kept := ids["RepositorySignatures/5.0.0"]; kept || len(ids) != 5 {
		t.Errorf("resources = %v, want RepositorySignatures dropped", ids)
	}
}

func TestRewriteServiceIndexFollowsACustomFeedBase(t *testing.T) {
	t.Parallel()
	const feed = `{"version":"3.0.0","resources":[
		{"@id":"https://Nexus.example.com:443/repository/nuget-group/v3/flat/","@type":"PackageBaseAddress/3.0.0"},
		{"@id":"https://nexus.example.com/repository/nuget-group/v3/registration/","@type":"RegistrationsBaseUrl/3.6.0"},
		{"@id":"https://nexus.example.com/repository/nuget-group/v3/search?semVerLevel=2.0.0","@type":"SearchQueryService"},
		{"@id":"https://nexus.example.com/repository/nuget-groupx/v3/flat/","@type":"SiblingFeed"},
		{"@id":"https://nexus.example.com/repository/other/v3/flat/","@type":"OtherFeed"},
		{"@id":"http://nexus.example.com/repository/nuget-group/v3/flat/","@type":"OtherScheme"},
		{"@id":"https://nexus.example.com/repository/nuget-group/../other/","@type":"DotSegments"},
		{"@id":"https://api.nuget.org/v3-flatcontainer/","@type":"NugetOrg"},
		{"@id":"v3/relative/","@type":"Relative"}
	]}`
	ids := rewrittenIDs(t, feed, "https://nexus.example.com/repository/nuget-group/")
	for resourceType, want := range map[string]string{
		"PackageBaseAddress/3.0.0":   "http://localhost:8080/nuget/v3/flat/",
		"RegistrationsBaseUrl/3.6.0": "http://localhost:8080/nuget/v3/registration/",
		"SearchQueryService":         "http://localhost:8080/nuget/v3/search?semVerLevel=2.0.0",
		"SiblingFeed":                "https://nexus.example.com/repository/nuget-groupx/v3/flat/",
		"OtherFeed":                  "https://nexus.example.com/repository/other/v3/flat/",
		"OtherScheme":                "http://nexus.example.com/repository/nuget-group/v3/flat/",
		"DotSegments":                "https://nexus.example.com/repository/nuget-group/../other/",
		// Without the hard-coded host list, nuget.org is just another origin.
		"NugetOrg": "https://api.nuget.org/v3-flatcontainer/",
		"Relative": "v3/relative/",
	} {
		if got := ids[resourceType]; got != want {
			t.Errorf("%s @id = %q, want %q", resourceType, got, want)
		}
	}
}

func TestRewriteServiceIndexRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	if _, err := RewriteServiceIndex([]byte("not json"), "http://localhost:8080", "https://api.nuget.org"); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}
