package unit

import (
	"testing"

	"depsilo/internal/adapter/packagekey"
)

func TestExtractPackageName_PyPI(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"pypi/simple/requests/index.html", "requests"},
		{"pypi/files/packages/ab/cd/requests-2.31.0.tar.gz", "requests"},
		{"pypi/files/packages/ab/cd/Flask-2.0.0-py3-none-any.whl", "Flask"},
		{"pypi/files/packages/aa/foo-bar-1.0.zip", ""},
		{"pypi/files/packages/aa/foo-bar-1.0.tar.gz", ""},
		{"pypi/files/packages/aa/Flask-2.0.0-py3-none-any.whl.metadata", ""},
	}
	for _, tt := range tests {
		got := packagekey.ExtractName("pypi", tt.key)
		if got != tt.expected {
			t.Errorf("ExtractPackageName(pypi, %q) = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestExtractPackageName_Npm(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"npm/lodash/metadata.json", "lodash"},
		{"npm/lodash/-/lodash-4.17.21.tgz", "lodash"},
		{"npm/@babel/core/metadata.json", "@babel/core"},
		{"npm/@babel/core/-/core-7.24.0.tgz", "@babel/core"},
		{"npm-exact-v1/Express/metadata.json", "Express"},
		{"npm-exact-v1/@LegacyScope/Name/-/Name-1.0.0.tgz", "@LegacyScope/Name"},
	}
	for _, tt := range tests {
		got := packagekey.ExtractName("npm", tt.key)
		if got != tt.expected {
			t.Errorf("ExtractPackageName(npm, %q) = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestExtractPackageName_Go(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"go/github.com/gin-gonic/gin/@v/v1.9.1.info", "github.com/gin-gonic/gin"},
		{"go/github.com/gin-gonic/gin/@v/list", "github.com/gin-gonic/gin"},
		{"go/github.com/test/mod/@latest", "github.com/test/mod"},
	}
	for _, tt := range tests {
		got := packagekey.ExtractName("go", tt.key)
		if got != tt.expected {
			t.Errorf("ExtractPackageName(go, %q) = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestExtractPackageName_Cargo(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"cargo/crates/serde/1.0.0.crate", "serde"},
		{"cargo/index/se/rd/serde", ""},
	}
	for _, tt := range tests {
		got := packagekey.ExtractName("cargo", tt.key)
		if got != tt.expected {
			t.Errorf("ExtractPackageName(cargo, %q) = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestExtractPackageName_APT(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"apt/ubuntu/pool/main/c/curl/curl_7.68.0-1ubuntu2_amd64.deb", "curl"},
		{"apt/debian/pool/main/a/anna/anna_1.0_amd64.udeb", "anna"},
		{"apt/ubuntu/dists/jammy/InRelease", ""},
	}
	for _, tt := range tests {
		got := packagekey.ExtractName("apt", tt.key)
		if got != tt.expected {
			t.Errorf("ExtractPackageName(apt, %q) = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestExtractPackageName_Maven(t *testing.T) {
	key := "maven/org/apache/commons/commons-lang3/3.14.0/commons-lang3-3.14.0.jar"
	got := packagekey.ExtractName("maven", key)
	if got != "org.apache.commons:commons-lang3" {
		t.Errorf("ExtractPackageName(maven, ...) = %q, want complete coordinate", got)
	}
	if got := packagekey.ExtractName("maven", "maven/org/apache/commons/commons-lang3/maven-metadata.xml"); got != "" {
		t.Errorf("ExtractPackageName(maven metadata) = %q, want empty", got)
	}
}

func TestExtractPackageName_RubyGems(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"rubygems/gems/rails-7.0.0.gem", "rails"},
		{"rubygems/info/rails", "rails"},
	}
	for _, tt := range tests {
		got := packagekey.ExtractName("rubygems", tt.key)
		if got != tt.expected {
			t.Errorf("ExtractPackageName(rubygems, %q) = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestExtractPackageName_Composer(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"composer/p2/monolog/monolog.json", "monolog/monolog"},
		{"composer/p2/monolog/monolog~dev.json", "monolog/monolog"},
		{"composer/dist/monolog/monolog/abc123.zip", "monolog/monolog"},
	}
	for _, tt := range tests {
		got := packagekey.ExtractName("composer", tt.key)
		if got != tt.expected {
			t.Errorf("ExtractPackageName(composer, %q) = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestExtractPackageName_NuGet(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"nuget/v3/package/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg", "newtonsoft.json"},
		{"nuget/v3/registration/newtonsoft.json/index.json", "newtonsoft.json"},
	}
	for _, tt := range tests {
		got := packagekey.ExtractName("nuget", tt.key)
		if got != tt.expected {
			t.Errorf("ExtractPackageName(nuget, %q) = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestExtractPackageName_Conda(t *testing.T) {
	key := "conda/pkgs/main/linux-64/numpy-1.24.0-py39h.tar.bz2"
	got := packagekey.ExtractName("conda", key)
	if got != "numpy" {
		t.Errorf("ExtractPackageName(conda, ...) = %q, want numpy", got)
	}
}

func TestExtractPackageName_CRAN(t *testing.T) {
	key := "cran/src/contrib/ggplot2_3.4.0.tar.gz"
	got := packagekey.ExtractName("cran", key)
	if got != "ggplot2" {
		t.Errorf("ExtractPackageName(cran, ...) = %q, want ggplot2", got)
	}
}

func TestExtractPackageName_Helm(t *testing.T) {
	key := "helm/nginx-15.0.0.tgz"
	got := packagekey.ExtractName("helm", key)
	if got != "nginx" {
		t.Errorf("ExtractPackageName(helm, ...) = %q, want nginx", got)
	}
}

func TestExtractPackageName_Docker(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"docker/dockerhub/manifests/library/nginx/latest", "library/nginx"},
		{"docker/ghcr/manifests/owner/repo/sha256__abc", "owner/repo"},
		{"docker/dockerhub/manifests/team/service/api/v1.0", "team/service/api"},
		{"docker/dockerhub/blobs/sha256__abc", ""},
		{"docker/dockerhub/tags/library/nginx/list", "library/nginx"},
	}
	for _, tt := range tests {
		got := packagekey.ExtractName("docker", tt.key)
		if got != tt.expected {
			t.Errorf("ExtractPackageName(docker, %q) = %q, want %q", tt.key, got, tt.expected)
		}
	}
}

func TestExtractPackageName_Unknown(t *testing.T) {
	got := packagekey.ExtractName("unknown", "some/key")
	if got != "" {
		t.Errorf("ExtractPackageName(unknown, ...) = %q, want empty", got)
	}
}
