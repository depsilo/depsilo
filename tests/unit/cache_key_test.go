package unit

import (
	"testing"

	aptAdapter "depsilo/internal/adapter/apt"
	cargoAdapter "depsilo/internal/adapter/cargo"
	composerAdapter "depsilo/internal/adapter/composer"
	condaAdapter "depsilo/internal/adapter/conda"
	cranAdapter "depsilo/internal/adapter/cran"
	dockerAdapter "depsilo/internal/adapter/docker"
	goProxy "depsilo/internal/adapter/goproxy"
	helmAdapter "depsilo/internal/adapter/helm"
	mavenAdapter "depsilo/internal/adapter/maven"
	"depsilo/internal/adapter/npm"
	nugetAdapter "depsilo/internal/adapter/nuget"
	"depsilo/internal/adapter/pypi"
	rubygemsAdapter "depsilo/internal/adapter/rubygems"
)

// ---------- PyPI ----------

func TestCacheKey_PyPI_Index(t *testing.T) {
	key := pypi.IndexCacheKey("pypi", "requests")
	if key != "pypi/simple/requests/index.html" {
		t.Errorf("expected pypi/simple/requests/index.html, got %s", key)
	}
}

func TestCacheKey_PyPI_IndexNormalizesCase(t *testing.T) {
	if pypi.IndexCacheKey("pypi", "Requests") != "pypi/simple/requests/index.html" {
		t.Error("IndexCacheKey should lowercase package name")
	}
}

func TestCacheKey_PyPI_File(t *testing.T) {
	fkey := pypi.FileCacheKey("pypi", "/packages/ab/cd/requests-2.31.0.tar.gz")
	if fkey != "pypi/files/packages/ab/cd/requests-2.31.0.tar.gz" {
		t.Errorf("got %s", fkey)
	}
}

func TestCacheKey_PyPI_FileNoLeadingSlash(t *testing.T) {
	fkey := pypi.FileCacheKey("pypi", "packages/ab/cd/foo-1.0.whl")
	if fkey != "pypi/files/packages/ab/cd/foo-1.0.whl" {
		t.Errorf("got %s", fkey)
	}
}

// ---------- npm ----------

func TestCacheKey_Npm_Metadata(t *testing.T) {
	key := npm.MetadataCacheKey("lodash")
	if key != "npm/lodash/metadata.json" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Npm_ScopedMetadata(t *testing.T) {
	key := npm.ScopedMetadataCacheKey("babel", "core")
	if key != "npm/@babel/core/metadata.json" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Npm_Tarball(t *testing.T) {
	key := npm.TarballCacheKey("lodash", "lodash-4.17.21.tgz")
	if key != "npm/lodash/-/lodash-4.17.21.tgz" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Npm_ScopedTarball(t *testing.T) {
	key := npm.ScopedTarballCacheKey("babel", "core", "core-7.24.0.tgz")
	if key != "npm/@babel/core/-/core-7.24.0.tgz" {
		t.Errorf("got %s", key)
	}
}

// ---------- APT ----------

func TestCacheKey_APT(t *testing.T) {
	key := aptAdapter.CacheKey("ubuntu", "dists/jammy/InRelease")
	if key != "apt/ubuntu/dists/jammy/InRelease" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_APT_LeadingSlash(t *testing.T) {
	key := aptAdapter.CacheKey("ubuntu", "/dists/jammy/Release")
	if key != "apt/ubuntu/dists/jammy/Release" {
		t.Errorf("got %s", key)
	}
}

// ---------- Go Proxy ----------

func TestCacheKey_Go_List(t *testing.T) {
	key := goProxy.ListCacheKey("github.com/gin-gonic/gin")
	if key != "go/github.com/gin-gonic/gin/@v/list" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Go_Latest(t *testing.T) {
	key := goProxy.LatestCacheKey("github.com/gin-gonic/gin")
	if key != "go/github.com/gin-gonic/gin/@latest" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Go_Info(t *testing.T) {
	key := goProxy.InfoCacheKey("github.com/gin-gonic/gin", "v1.9.1")
	if key != "go/github.com/gin-gonic/gin/@v/v1.9.1.info" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Go_Mod(t *testing.T) {
	key := goProxy.ModCacheKey("github.com/gin-gonic/gin", "v1.9.1")
	if key != "go/github.com/gin-gonic/gin/@v/v1.9.1.mod" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Go_Zip(t *testing.T) {
	key := goProxy.ZipCacheKey("github.com/gin-gonic/gin", "v1.9.1")
	if key != "go/github.com/gin-gonic/gin/@v/v1.9.1.zip" {
		t.Errorf("got %s", key)
	}
}

// ---------- Cargo ----------

func TestCacheKey_Cargo_Config(t *testing.T) {
	key := cargoAdapter.ConfigCacheKey()
	if key != "cargo/config.json" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Cargo_Index(t *testing.T) {
	key := cargoAdapter.IndexCacheKey("se/rd", "serde")
	if key != "cargo/index/se/rd/serde" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Cargo_Crate(t *testing.T) {
	key := cargoAdapter.CrateCacheKey("serde", "1.0.0")
	if key != "cargo/crates/serde/1.0.0.crate" {
		t.Errorf("got %s", key)
	}
}

// ---------- Maven ----------

func TestCacheKey_Maven(t *testing.T) {
	key := mavenAdapter.CacheKey("org/apache/commons/commons-lang3/3.14.0/commons-lang3-3.14.0.jar")
	expected := "maven/org/apache/commons/commons-lang3/3.14.0/commons-lang3-3.14.0.jar"
	if key != expected {
		t.Errorf("got %s", key)
	}
}

// ---------- RubyGems ----------

func TestCacheKey_RubyGems(t *testing.T) {
	key := rubygemsAdapter.CacheKey("gems/rails-7.0.0.gem")
	if key != "rubygems/gems/rails-7.0.0.gem" {
		t.Errorf("got %s", key)
	}
}

// ---------- Composer ----------

func TestCacheKey_Composer_Packages(t *testing.T) {
	key := composerAdapter.PackagesCacheKey()
	if key != "composer/packages.json" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Composer_Metadata(t *testing.T) {
	key := composerAdapter.MetadataCacheKey("monolog", "monolog")
	if key != "composer/p2/monolog/monolog.json" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Composer_DevMetadata(t *testing.T) {
	key := composerAdapter.DevMetadataCacheKey("monolog", "monolog")
	if key != "composer/p2/monolog/monolog~dev.json" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Composer_Dist(t *testing.T) {
	key := composerAdapter.DistCacheKey("monolog", "monolog", "abc123", "zip")
	if key != "composer/dist/monolog/monolog/abc123.zip" {
		t.Errorf("got %s", key)
	}
}

// ---------- NuGet ----------

func TestCacheKey_NuGet(t *testing.T) {
	key := nugetAdapter.CacheKey("v3/package/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg")
	expected := "nuget/v3/package/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg"
	if key != expected {
		t.Errorf("got %s", key)
	}
}

// ---------- Conda ----------

func TestCacheKey_Conda(t *testing.T) {
	key := condaAdapter.CacheKey("pkgs/main/linux-64/numpy-1.24.0.tar.bz2")
	expected := "conda/pkgs/main/linux-64/numpy-1.24.0.tar.bz2"
	if key != expected {
		t.Errorf("got %s", key)
	}
}

// ---------- CRAN ----------

func TestCacheKey_CRAN(t *testing.T) {
	key := cranAdapter.CacheKey("src/contrib/ggplot2_3.4.0.tar.gz")
	expected := "cran/src/contrib/ggplot2_3.4.0.tar.gz"
	if key != expected {
		t.Errorf("got %s", key)
	}
}

// ---------- Helm ----------

func TestCacheKey_Helm(t *testing.T) {
	key := helmAdapter.CacheKey("nginx-15.0.0.tgz")
	expected := "helm/nginx-15.0.0.tgz"
	if key != expected {
		t.Errorf("got %s", key)
	}
}

// ---------- Docker ----------

func TestCacheKey_Docker_Manifest(t *testing.T) {
	key := dockerAdapter.ManifestCacheKey("dockerhub", "library/nginx", "latest")
	if key != "docker/dockerhub/manifests/library/nginx/latest" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Docker_ManifestDigest(t *testing.T) {
	key := dockerAdapter.ManifestCacheKey("dockerhub", "library/nginx", "sha256:abc123")
	if key != "docker/dockerhub/manifests/library/nginx/sha256__abc123" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Docker_Blob(t *testing.T) {
	key := dockerAdapter.BlobCacheKey("dockerhub", "sha256:abc123")
	if key != "docker/dockerhub/blobs/sha256__abc123" {
		t.Errorf("got %s", key)
	}
}

func TestCacheKey_Docker_TagList(t *testing.T) {
	key := dockerAdapter.TagListCacheKey("dockerhub", "library/nginx")
	if key != "docker/dockerhub/tags/library/nginx/list" {
		t.Errorf("got %s", key)
	}
}
