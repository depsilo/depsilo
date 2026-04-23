package unit

import (
	"encoding/json"
	"strings"
	"testing"

	composerAdapter "depsilo/internal/adapter/composer"
	"depsilo/internal/adapter/npm"
	nugetAdapter "depsilo/internal/adapter/nuget"
	"depsilo/internal/adapter/pypi"
)

// ---------- PyPI URL Rewrite ----------

func TestURLRewrite_PyPI_AbsoluteURL(t *testing.T) {
	html := `<a href="https://files.pythonhosted.org/packages/ab/cd/requests-2.31.0.tar.gz#sha256=abcd1234">requests-2.31.0.tar.gz</a>`
	result := pypi.RewriteURLs(html, "http://localhost:8080", "/pypi")
	if strings.Contains(result, "pythonhosted.org") {
		t.Error("external URL not rewritten")
	}
	if !strings.Contains(result, "http://localhost:8080/pypi/files/packages/ab/cd/requests-2.31.0.tar.gz") {
		t.Errorf("local URL not found in result: %s", result)
	}
	if !strings.Contains(result, "#sha256=abcd1234") {
		t.Error("hash fragment not preserved")
	}
}

func TestURLRewrite_PyPI_RelativeURL(t *testing.T) {
	html := `<a href="../../packages/ab/cd/requests-2.31.0.tar.gz#sha256=abcd">requests</a>`
	result := pypi.RewriteURLs(html, "http://localhost:8080", "/pypi")
	if !strings.Contains(result, "http://localhost:8080/pypi/files/packages/ab/cd/requests-2.31.0.tar.gz") {
		t.Errorf("relative URL not properly rewritten: %s", result)
	}
}

func TestURLRewrite_PyPI_MultipleURLs(t *testing.T) {
	html := `<a href="https://files.pythonhosted.org/packages/a/b/pkg-1.0.whl#sha256=aaa">pkg-1.0.whl</a>
<a href="https://files.pythonhosted.org/packages/c/d/pkg-1.0.tar.gz#sha256=bbb">pkg-1.0.tar.gz</a>`
	result := pypi.RewriteURLs(html, "http://localhost:8080", "/pypi")
	if strings.Contains(result, "pythonhosted.org") {
		t.Error("some external URLs not rewritten")
	}
	if !strings.Contains(result, "/pypi/files/packages/a/b/pkg-1.0.whl") {
		t.Error("first URL not rewritten")
	}
	if !strings.Contains(result, "/pypi/files/packages/c/d/pkg-1.0.tar.gz") {
		t.Error("second URL not rewritten")
	}
}

func TestURLRewrite_PyPI_NonPackageURL(t *testing.T) {
	html := `<a href="https://example.com/some-other-page">link</a>`
	result := pypi.RewriteURLs(html, "http://localhost:8080", "/pypi")
	// Non-package URLs should not be rewritten
	if !strings.Contains(result, "https://example.com/some-other-page") {
		t.Error("non-package URL should not be rewritten")
	}
}

func TestURLRewrite_PyPI_TrailingSlashBaseURL(t *testing.T) {
	html := `<a href="https://files.pythonhosted.org/packages/a/b/pkg-1.0.whl">pkg</a>`
	result := pypi.RewriteURLs(html, "http://localhost:8080/", "/pypi")
	// Should not have double slash
	if strings.Contains(result, "8080//pypi") {
		t.Error("double slash in rewritten URL")
	}
}

// ---------- npm URL Rewrite ----------

func TestURLRewrite_Npm_TarballURL(t *testing.T) {
	input := `{"name":"lodash","versions":{"4.17.21":{"name":"lodash","version":"4.17.21","dist":{"tarball":"https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz","shasum":"abc123"}}}}`
	result, err := npm.RewriteTarballURLs([]byte(input), "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)
	if strings.Contains(resultStr, "registry.npmjs.org") {
		t.Error("external tarball URL not rewritten")
	}
	if !strings.Contains(resultStr, "http://localhost:8080/npm/lodash/-/lodash-4.17.21.tgz") {
		t.Errorf("local tarball URL not found in result: %s", resultStr)
	}
}

func TestURLRewrite_Npm_MultipleVersions(t *testing.T) {
	input := `{"name":"lodash","versions":{
		"4.17.20":{"dist":{"tarball":"https://registry.npmjs.org/lodash/-/lodash-4.17.20.tgz"}},
		"4.17.21":{"dist":{"tarball":"https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz"}}
	}}`
	result, err := npm.RewriteTarballURLs([]byte(input), "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)
	if strings.Contains(resultStr, "registry.npmjs.org") {
		t.Error("not all tarball URLs rewritten")
	}
}

func TestURLRewrite_Npm_PreservesOtherFields(t *testing.T) {
	input := `{"name":"lodash","versions":{"4.17.21":{"dist":{"tarball":"https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz","shasum":"abc123","integrity":"sha512-test"}}}}`
	result, err := npm.RewriteTarballURLs([]byte(input), "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(result, &doc); err != nil {
		t.Fatal(err)
	}
	versions := doc["versions"].(map[string]interface{})
	v := versions["4.17.21"].(map[string]interface{})
	dist := v["dist"].(map[string]interface{})
	if dist["shasum"] != "abc123" {
		t.Error("shasum not preserved")
	}
	if dist["integrity"] != "sha512-test" {
		t.Error("integrity not preserved")
	}
}

func TestURLRewrite_Npm_NoName(t *testing.T) {
	input := `{"versions":{"1.0.0":{"dist":{"tarball":"https://registry.npmjs.org/test/-/test-1.0.0.tgz"}}}}`
	result, err := npm.RewriteTarballURLs([]byte(input), "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	// With no name field, should return unchanged
	if !strings.Contains(string(result), "registry.npmjs.org") {
		t.Error("should not rewrite when name is missing")
	}
}

func TestURLRewrite_Npm_InvalidJSON(t *testing.T) {
	_, err := npm.RewriteTarballURLs([]byte("not json"), "http://localhost:8080")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------- Composer URL Rewrite ----------

func TestURLRewrite_Composer_PackagesJSON(t *testing.T) {
	input := `{"packages":{},"metadata-url":"/p2/%package%.json"}`
	result, err := composerAdapter.RewritePackagesJSON([]byte(input), "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)
	if !strings.Contains(resultStr, "http://localhost:8080/composer/p2/%package%.json") {
		t.Errorf("metadata-url not rewritten: %s", resultStr)
	}
}

func TestURLRewrite_Composer_PreservesPackages(t *testing.T) {
	input := `{"packages":{"test/pkg":[]},"metadata-url":"/p2/%package%.json"}`
	result, err := composerAdapter.RewritePackagesJSON([]byte(input), "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(result, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["packages"]; !ok {
		t.Error("packages field not preserved")
	}
}

func TestURLRewrite_Composer_InvalidJSON(t *testing.T) {
	_, err := composerAdapter.RewritePackagesJSON([]byte("{bad"), "http://localhost:8080")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------- NuGet URL Rewrite ----------

func TestURLRewrite_NuGet_ServiceIndex(t *testing.T) {
	input := `{"version":"3.0.0","resources":[
		{"@id":"https://api.nuget.org/v3/search","@type":"SearchQueryService"},
		{"@id":"https://api.nuget.org/v3/registration","@type":"RegistrationsBaseUrl"}
	]}`
	result, err := nugetAdapter.RewriteServiceIndex([]byte(input), "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)
	if strings.Contains(resultStr, "api.nuget.org") {
		t.Error("nuget.org URLs not rewritten")
	}
	if !strings.Contains(resultStr, "http://localhost:8080/nuget/v3/search") {
		t.Errorf("search URL not rewritten: %s", resultStr)
	}
	if !strings.Contains(resultStr, "http://localhost:8080/nuget/v3/registration") {
		t.Errorf("registration URL not rewritten: %s", resultStr)
	}
}

func TestURLRewrite_NuGet_AzureSearchURLs(t *testing.T) {
	input := `{"version":"3.0.0","resources":[
		{"@id":"https://azuresearch-usnc.nuget.org/query","@type":"SearchQueryService"}
	]}`
	result, err := nugetAdapter.RewriteServiceIndex([]byte(input), "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	resultStr := string(result)
	if strings.Contains(resultStr, "azuresearch") {
		t.Error("azuresearch URL not rewritten")
	}
}

func TestURLRewrite_NuGet_PreservesVersion(t *testing.T) {
	input := `{"version":"3.0.0","resources":[]}`
	result, err := nugetAdapter.RewriteServiceIndex([]byte(input), "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(result, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["version"] != "3.0.0" {
		t.Error("version field not preserved")
	}
}

func TestURLRewrite_NuGet_UnknownURLNotRewritten(t *testing.T) {
	input := `{"version":"3.0.0","resources":[
		{"@id":"https://custom.nuget.example.com/v3/index","@type":"SearchQueryService"}
	]}`
	result, err := nugetAdapter.RewriteServiceIndex([]byte(input), "http://localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), "custom.nuget.example.com") {
		t.Error("unknown URL should not be rewritten")
	}
}

func TestURLRewrite_NuGet_InvalidJSON(t *testing.T) {
	_, err := nugetAdapter.RewriteServiceIndex([]byte("not json"), "http://localhost:8080")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
