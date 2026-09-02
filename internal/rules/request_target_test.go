package rules

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/adapter"
	"depsilo/internal/db"
	ecosystemcatalog "depsilo/internal/ecosystem"
)

func TestExtractRequestTargetCoversRuleEcosystems(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		path    string
		name    string
		version string
	}{
		"pypi": {
			path:    "/pypi/files/requests-2.32.3-py3-none-any.whl",
			name:    "requests",
			version: "2.32.3",
		},
		"apt": {
			path:    "/apt/jammy/pool/main/c/curl/curl_8.1.0-1ubuntu1_amd64.deb",
			name:    "curl",
			version: "",
		},
		"npm": {
			path:    "/npm/@scope/internal-utils/-/internal-utils-1.4.2.tgz",
			name:    "@scope/internal-utils",
			version: "",
		},
		"go": {
			path:    "/go/github.com/!azure/azure-sdk-for-go/@v/v68.0.0.zip",
			name:    "github.com/Azure/azure-sdk-for-go",
			version: "v68.0.0",
		},
		"cargo": {
			path:    "/crates/api/v1/crates/serde/1.0.219/download",
			name:    "serde",
			version: "1.0.219",
		},
		"maven": {
			path:    "/maven/org/apache/commons/commons-lang3/3.17.0/commons-lang3-3.17.0.jar",
			name:    "org.apache.commons:commons-lang3",
			version: "3.17.0",
		},
		"composer": {
			path:    "/composer/dist/monolog/monolog/3.8.1.0/0123456789abcdef.zip",
			name:    "monolog/monolog",
			version: "3.8.1.0",
		},
		"nuget": {
			path:    "/nuget/v3-flatcontainer/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg",
			name:    "newtonsoft.json",
			version: "13.0.3",
		},
		"conda": {
			path:    "/conda/conda-forge/linux-64/numpy-base-1.26.4-py311h8a23956_0.conda",
			name:    "conda-forge/numpy-base",
			version: "1.26.4",
		},
		"cran": {
			path:    "/cran/src/contrib/dplyr_1.1.4.tar.gz",
			name:    "dplyr",
			version: "1.1.4",
		},
		"alpine": {
			path:    "/alpine/v3.21/main/x86_64/py3-requests-2.32.3-r0.apk",
			name:    "v3.21/main/x86_64/py3-requests",
			version: "2.32.3-r0",
		},
	}

	// RuleDefinitions is also the package-rules UI's ecosystem set. Basing
	// this assertion on the catalog catches a newly exposed ecosystem whose
	// route has not yet gained an accurate package/version extractor.
	for _, definition := range ecosystemcatalog.RuleDefinitions() {
		if definition.Name == "npm" {
			for _, requestPath := range []string{
				"/npm/@scope/internal-utils",
				"/npm/@scope/internal-utils/-/internal-utils-1.4.2.tgz",
				"/npm/@scope/internal-utils/-/__depsilo_tarball_v1/token/internal-utils.tgz",
			} {
				if target, ok := extractRequestTarget(requestPath); ok {
					t.Errorf("authenticated adapter-owned npm path %q reached middleware as %#v", requestPath, target)
				}
			}
			continue
		}
		test, exists := tests[definition.Name]
		if !exists {
			t.Fatalf("missing request-target fixture for catalog ecosystem %q", definition.Name)
		}
		t.Run(definition.Name, func(t *testing.T) {
			target, ok := extractRequestTarget(test.path)
			if !ok {
				t.Fatalf("extractRequestTarget(%q) did not recognize request", test.path)
			}
			want := requestTarget{Ecosystem: definition.Name, PackageName: test.name, Version: test.version}
			if target != want {
				t.Fatalf("extractRequestTarget(%q) = %#v, want %#v", test.path, target, want)
			}
		})
	}
}

func TestExtractRequestTargetProjectAndMetadataPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   requestTarget
		wantOK bool
	}{
		{
			name:   "project prefix uses canonical cargo route",
			path:   "/p/payments/crates/api/v1/crates/tokio/1.44.1/download",
			want:   requestTarget{Ecosystem: "cargo", PackageName: "tokio", Version: "1.44.1"},
			wantOK: true,
		},
		{
			name:   "pypi package metadata has unknown version",
			path:   "/pypi/simple/requests/",
			want:   requestTarget{Ecosystem: "pypi", PackageName: "requests"},
			wantOK: true,
		},
		{
			name:   "go list metadata decodes canonical module identity",
			path:   "/go/github.com/!azure/azure-sdk/@v/list",
			want:   requestTarget{Ecosystem: "go", PackageName: "github.com/Azure/azure-sdk"},
			wantOK: true,
		},
		{
			name:   "go version metadata decodes canonical module identity",
			path:   "/go/github.com/!azure/azure-sdk/@v/v1.2.3.info",
			want:   requestTarget{Ecosystem: "go", PackageName: "github.com/Azure/azure-sdk", Version: "v1.2.3"},
			wantOK: true,
		},
		{
			name:   "go version metadata decodes canonical version identity",
			path:   "/go/github.com/user/repo/@v/v1.0.0-!r!c1.mod",
			want:   requestTarget{Ecosystem: "go", PackageName: "github.com/user/repo", Version: "v1.0.0-RC1"},
			wantOK: true,
		},
		{
			name:   "go version metadata rejects unescaped uppercase version",
			path:   "/go/github.com/user/repo/@v/v1.0.0-RC1.info",
			wantOK: false,
		},
		{
			name:   "go latest metadata accepts semantic import version suffix",
			path:   "/go/example.com/module/v2/@latest",
			want:   requestTarget{Ecosystem: "go", PackageName: "example.com/module/v2"},
			wantOK: true,
		},
		{
			name:   "cargo sparse index path does not guess case-sensitive identity",
			path:   "/crates/my/cr/mycrate",
			wantOK: false,
		},
		{
			name:   "RubyGems remains outside Package Rules until identity provenance exists",
			path:   "/rubygems/gems/nokogiri-1.16.5-x86_64-linux.gem",
			wantOK: false,
		},
		{
			name:   "APT installer micro-package remains package-wide enforceable",
			path:   "/apt/debian/dists/stable/main/debian-installer/binary-amd64/anna_1.0_amd64.udeb",
			want:   requestTarget{Ecosystem: "apt", PackageName: "anna"},
			wantOK: true,
		},
		{
			name:   "cran Windows binary artifact carries exact version",
			path:   "/cran/bin/windows/contrib/4.5/dplyr_1.1.4.zip",
			want:   requestTarget{Ecosystem: "cran", PackageName: "dplyr", Version: "1.1.4"},
			wantOK: true,
		},
		{
			name:   "cran macOS binary artifact carries exact version",
			path:   "/cran/bin/macosx/big-sur-arm64/contrib/4.5/dplyr_1.1.4.tgz",
			want:   requestTarget{Ecosystem: "cran", PackageName: "dplyr", Version: "1.1.4"},
			wantOK: true,
		},
		{
			name:   "cran artifact needs an unambiguous name version separator",
			path:   "/cran/src/contrib/not_a_package_1.0.tar.gz",
			wantOK: false,
		},
		{
			name:   "maven artifact uses coordinate and directory version",
			path:   "/maven/io/netty/netty-all/4.1.118.Final/netty-all-4.1.118.Final.jar",
			want:   requestTarget{Ecosystem: "maven", PackageName: "io.netty:netty-all", Version: "4.1.118.Final"},
			wantOK: true,
		},
		{
			name:   "maven package metadata is skipped because path depth is ambiguous",
			path:   "/maven/io/netty/netty-all/maven-metadata.xml",
			wantOK: false,
		},
		{
			name:   "maven version-level metadata is skipped",
			path:   "/maven/io/netty/netty-all/4.1.118.Final/maven-metadata.xml",
			wantOK: false,
		},
		{
			name:   "maven numeric artifact metadata is not mistaken for version metadata",
			path:   "/maven/com/acme/123/maven-metadata.xml",
			wantOK: false,
		},
		{
			name:   "maven numeric artifact download remains enforceable",
			path:   "/maven/com/acme/123/1.0/123-1.0.jar",
			want:   requestTarget{Ecosystem: "maven", PackageName: "com.acme:123", Version: "1.0"},
			wantOK: true,
		},
		{
			name:   "maven pom remains enforceable",
			path:   "/maven/com/acme/123/1.0/123-1.0.pom",
			want:   requestTarget{Ecosystem: "maven", PackageName: "com.acme:123", Version: "1.0"},
			wantOK: true,
		},
		{
			name:   "maven Android archive remains enforceable",
			path:   "/maven/com/android/library/1.0/library-1.0.aar",
			want:   requestTarget{Ecosystem: "maven", PackageName: "com.android:library", Version: "1.0"},
			wantOK: true,
		},
		{
			name:   "maven arbitrary packaging remains enforceable",
			path:   "/maven/com/acme/app/1.0/app-1.0.war",
			want:   requestTarget{Ecosystem: "maven", PackageName: "com.acme:app", Version: "1.0"},
			wantOK: true,
		},
		{
			name:   "maven timestamped snapshot uses directory base version",
			path:   "/maven/com/acme/app/1.0-SNAPSHOT/app-1.0-20260901.010203-7.jar",
			want:   requestTarget{Ecosystem: "maven", PackageName: "com.acme:app", Version: "1.0-SNAPSHOT"},
			wantOK: true,
		},
		{
			name:   "alpine artifact keeps resolver package key",
			path:   "/p/platform/alpine/v3.20/community/aarch64/nodejs-20.15.1-r0.apk",
			want:   requestTarget{Ecosystem: "alpine", PackageName: "v3.20/community/aarch64/nodejs", Version: "20.15.1-r0"},
			wantOK: true,
		},
		{
			name:   "malformed PyPI artifact is explicit rather than skipped",
			path:   "/pypi/files/not-a-valid-wheel.whl",
			want:   requestTarget{Ecosystem: "pypi", AmbiguousArtifact: true},
			wantOK: true,
		},
		{
			name:   "route prefix needs a path boundary",
			path:   "/mavenish/org/example/pkg/1/pkg-1.jar",
			wantOK: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, ok := extractRequestTarget(test.path)
			if ok != test.wantOK || target != test.want {
				t.Fatalf("extractRequestTarget(%q) = (%#v, %v), want (%#v, %v)", test.path, target, ok, test.want, test.wantOK)
			}
		})
	}
}

func TestExtractRequestTargetMarksUnreliablePyPIArtifactIdentities(t *testing.T) {
	t.Parallel()

	for _, requestPath := range []string{
		"/pypi/files/packages/aa/my-package-1.0.tar.gz",
		"/pypi/files/packages/aa/package-1.0-1.tar.gz",
		"/pypi/files/packages/aa/package-1.0.zip",
		"/pypi/files/packages/aa/package-1.0.tar.bz2",
		"/pypi/files/packages/aa/package-1.0.tar.xz",
		"/pypi/files/packages/aa/package-1.0.tar.zst",
		"/pypi/files/packages/aa/package-1.0.tgz",
		"/pypi/files/packages/aa/package-1.0.egg",
		"/p/backend/pypi/files/packages/aa/not-a-valid-wheel.whl",
		"/pypi/files/packages/aa/bad$name-1.0-py3-none-any.whl",
		"/pypi/files/packages/aa/demo-1.0bogus-py3-none-any.whl",
		"/pypi/files/packages/aa/package-1.0.tar",
		"/pypi/files/packages/aa/package-1.0.tbz",
		"/pypi/files/packages/aa/package-1.0.txz",
		"/pypi/files/packages/aa/package-1.0.tlz",
		"/pypi/files/packages/aa/package-1.0.tar.lz",
		"/pypi/files/packages/aa/package-1.0.tar.lzma",
		"/pypi/files/packages/aa/package-1.0.unknown",
	} {
		target, ok := extractRequestTarget(requestPath)
		if !ok || target != (requestTarget{Ecosystem: "pypi", AmbiguousArtifact: true}) {
			t.Errorf("extractRequestTarget(%q) = (%#v, %v), want explicit ambiguous PyPI artifact", requestPath, target, ok)
		}
	}
}

func TestExtractRequestTargetLeavesPyPIMetadataSidecarsOutsideArtifactRules(t *testing.T) {
	t.Parallel()

	for _, requestPath := range []string{
		"/pypi/files/packages/aa/requests-2.32.3-py3-none-any.whl.metadata",
		"/p/backend/pypi/files/packages/aa/requests-2.32.3.tar.gz.metadata",
	} {
		if target, ok := extractRequestTarget(requestPath); ok {
			t.Errorf("extractRequestTarget(%q) = %#v, want no package-rule target for PEP 658 sidecar", requestPath, target)
		}
	}
}

func TestExtractRequestTargetUsesConfiguredExtraPyPIRoutes(t *testing.T) {
	t.Parallel()

	private, err := NewPyPIRouteDescriptor("company/python/private", false)
	if err != nil {
		t.Fatal(err)
	}
	torch, err := NewPyPIRouteDescriptor("company/python/torch", true)
	if err != nil {
		t.Fatal(err)
	}
	routes := []PyPIRouteDescriptor{private, torch}

	tests := []struct {
		name   string
		path   string
		want   requestTarget
		wantOK bool
	}{
		{
			name:   "ordinary multi-segment package index",
			path:   "/company/python/private/simple/requests/",
			want:   requestTarget{Ecosystem: "pypi", PackageName: "requests"},
			wantOK: true,
		},
		{
			name:   "ordinary project-scoped artifact",
			path:   "/p/payments/company/python/private/files/requests-2.32.3-py3-none-any.whl",
			want:   requestTarget{Ecosystem: "pypi", PackageName: "requests", Version: "2.32.3"},
			wantOK: true,
		},
		{
			name:   "channel package index",
			path:   "/company/python/torch/cu128/simple/torch/",
			want:   requestTarget{Ecosystem: "pypi", PackageName: "torch"},
			wantOK: true,
		},
		{
			name:   "project-scoped channel artifact",
			path:   "/p/ml/company/python/torch/cpu/files/torch-2.7.1-py3-none-any.whl",
			want:   requestTarget{Ecosystem: "pypi", PackageName: "torch", Version: "2.7.1"},
			wantOK: true,
		},
		{
			name:   "ambiguous ordinary artifact remains explicit",
			path:   "/company/python/private/files/my-package-1.0.tar.gz",
			want:   requestTarget{Ecosystem: "pypi", AmbiguousArtifact: true},
			wantOK: true,
		},
		{
			name:   "uppercase channel is not a registered route",
			path:   "/company/python/torch/CU128/simple/torch/",
			wantOK: false,
		},
		{
			name:   "unconfigured lookalike is not inferred",
			path:   "/company/python/other/simple/requests/",
			wantOK: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, ok := extractRequestTarget(test.path, routes...)
			if ok != test.wantOK || target != test.want {
				t.Fatalf("extractRequestTarget(%q) = (%#v, %v), want (%#v, %v)", test.path, target, ok, test.want, test.wantOK)
			}
		})
	}

	if target, ok := extractRequestTarget("/simple/requests/", PyPIRouteDescriptor{}); ok {
		t.Fatalf("zero-value route descriptor inferred an unconfigured target %#v", target)
	}
}

func TestMiddlewareAllowsAmbiguousPyPIIdentityWhenNoRulesExist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewStore(newRulesTestDB(t))
	router := gin.New()
	router.Use(Middleware(NewEngine(store, nil)))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, requestPath := range []string{
		"/pypi/files/packages/aa/bad$name-1.0-py3-none-any.whl",
		"/pypi/files/packages/aa/demo-1.0bogus-py3-none-any.whl",
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, requestPath, nil))
		if response.Code != http.StatusNoContent {
			t.Errorf("GET %s status = %d, want %d; body = %s", requestPath, response.Code, http.StatusNoContent, response.Body.String())
		}
	}
}

func TestMiddlewareEnforcesRulesOnConfiguredExtraPyPIRoutes(t *testing.T) {
	private, err := NewPyPIRouteDescriptor("company/python/private", false)
	if err != nil {
		t.Fatal(err)
	}
	torch, err := NewPyPIRouteDescriptor("company/python/torch", true)
	if err != nil {
		t.Fatal(err)
	}
	routes := []PyPIRouteDescriptor{private, torch}

	tests := []struct {
		name          string
		rule          db.PackageRule
		wantAmbiguous int
	}{
		{
			name:          "PyPI rule",
			rule:          db.PackageRule{Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny"},
			wantAmbiguous: http.StatusServiceUnavailable,
		},
		{
			name:          "global rule",
			rule:          db.PackageRule{Ecosystem: "*", PackageName: "*", Version: "*", Action: "deny"},
			wantAmbiguous: http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			store := NewStore(newRulesTestDB(t))
			if err := store.Create(&test.rule); err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.Use(Middleware(NewEngine(store, nil), routes...))
			router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			for _, requestPath := range []string{
				"/company/python/private/simple/requests/",
				"/p/payments/company/python/private/files/requests-2.32.3-py3-none-any.whl",
				"/company/python/torch/cu128/simple/requests/",
				"/p/ml/company/python/torch/cpu/files/requests-2.32.3-py3-none-any.whl",
			} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, requestPath, nil))
				if response.Code != http.StatusForbidden {
					t.Errorf("GET %s status = %d, want %d; body = %s", requestPath, response.Code, http.StatusForbidden, response.Body.String())
				}
			}

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(
				http.MethodGet,
				"/company/python/private/files/my-package-1.0.tar.gz",
				nil,
			))
			if response.Code != test.wantAmbiguous {
				t.Fatalf("ambiguous artifact status = %d, want %d; body = %s", response.Code, test.wantAmbiguous, response.Body.String())
			}
		})
	}
}

func TestMiddlewareFailsClosedForUnidentifiedPyPIFilesAcrossRoutes(t *testing.T) {
	private, err := NewPyPIRouteDescriptor("company/python/private", false)
	if err != nil {
		t.Fatal(err)
	}

	store := NewStore(newRulesTestDB(t))
	if err := store.Create(&db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny",
	}); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(Middleware(NewEngine(store, nil), private))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, test := range []struct {
		name string
		path string
	}{
		{name: "built-in route", path: "/pypi/files/packages/aa/package-1.0.tar"},
		{name: "project-scoped route", path: "/p/payments/pypi/files/packages/aa/package-1.0.tbz"},
		{name: "configured extra route", path: "/company/python/private/files/packages/aa/package-1.0.tar.lzma"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("GET %s status = %d, want %d; body = %s", test.path, response.Code, http.StatusServiceUnavailable, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), `"code":"PACKAGE_POLICY_UNEVALUABLE"`) {
				t.Fatalf("GET %s body = %s, want PACKAGE_POLICY_UNEVALUABLE", test.path, response.Body.String())
			}
		})
	}
}

func TestGlobalRuleOnlyAppliesToPackageRuleRoutesAndConfiguredExtraPyPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewStore(newRulesTestDB(t))
	if err := store.Create(&db.PackageRule{
		Ecosystem: "*", PackageName: "*", Version: "*", Action: "deny",
	}); err != nil {
		t.Fatal(err)
	}
	extra, err := NewPyPIRouteDescriptor("company/python/private", false)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(Middleware(NewEngine(store, nil), extra))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, test := range []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "configured extra PyPI remains in Package Rules scope",
			path:       "/company/python/private/simple/requests/",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "disabled RubyGems enforcement is outside global rule scope",
			path:       "/rubygems/gems/rake-13.2.1.gem",
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "disabled Helm enforcement is outside global rule scope",
			path:       "/p/platform/helm/ingress-nginx-4.12.1.tgz",
			wantStatus: http.StatusNoContent,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != test.wantStatus {
				t.Fatalf("GET %s status = %d, want %d; body = %s", test.path, response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func TestMiddlewareFailsClosedForAmbiguousPyPIArtifactsOnlyWhenRelevantRulesExist(t *testing.T) {
	tests := []struct {
		name       string
		rule       *db.PackageRule
		wantStatus int
	}{
		{name: "no rules preserves artifact compatibility", wantStatus: http.StatusNoContent},
		{
			name:       "unrelated ecosystem rule preserves artifact compatibility",
			rule:       &db.PackageRule{Ecosystem: "cargo", PackageName: "serde", Version: "*", Action: "deny"},
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "PyPI rule makes an ambiguous artifact unevaluable",
			rule:       &db.PackageRule{Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny"},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "global deny deterministically blocks an ambiguous artifact",
			rule:       &db.PackageRule{Ecosystem: "*", PackageName: "*", Version: "*", Action: "deny"},
			wantStatus: http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			database := newRulesTestDB(t)
			store := NewStore(database)
			if test.rule != nil {
				if err := store.Create(test.rule); err != nil {
					t.Fatal(err)
				}
			}

			router := gin.New()
			router.Use(Middleware(NewEngine(store, nil)))
			router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/pypi/files/packages/aa/my-package-1.0.tar.gz", nil)
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantStatus == http.StatusServiceUnavailable &&
				!strings.Contains(response.Body.String(), `"code":"PACKAGE_POLICY_UNEVALUABLE"`) {
				t.Fatalf("body = %s, want PACKAGE_POLICY_UNEVALUABLE", response.Body.String())
			}
		})
	}
}

func TestMiddlewareEvaluatesOnlyDeterministicRulesForIncompleteArtifactIdentity(t *testing.T) {
	tests := []struct {
		name       string
		rules      []db.PackageRule
		wantStatus int
	}{
		{
			name:       "global allow is deterministic",
			rules:      []db.PackageRule{{Ecosystem: "*", PackageName: "*", Version: "*", Action: "allow"}},
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "ecosystem wildcard deny is deterministic",
			rules:      []db.PackageRule{{Ecosystem: "pypi", PackageName: "*", Version: "*", Action: "deny"}},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "same-action specific rule cannot change wildcard deny",
			rules: []db.PackageRule{
				{Ecosystem: "pypi", PackageName: "*", Version: "*", Action: "deny"},
				{Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny"},
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "specific allow may override wildcard deny",
			rules: []db.PackageRule{
				{Ecosystem: "pypi", PackageName: "*", Version: "*", Action: "deny"},
				{Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "allow"},
			},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "specific allow equals default allow",
			rules:      []db.PackageRule{{Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "allow"}},
			wantStatus: http.StatusNoContent,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewStore(newRulesTestDB(t))
			for index := range test.rules {
				if err := store.Create(&test.rules[index]); err != nil {
					t.Fatal(err)
				}
			}
			router := gin.New()
			router.Use(Middleware(NewEngine(store, nil)))
			router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(
				http.MethodGet, "/pypi/files/packages/aa/my-package-1.0.tar.gz", nil,
			))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func TestExtractRequestTargetRejectsMalformedGoModuleIdentities(t *testing.T) {
	t.Parallel()

	for _, escapedPath := range []string{
		"github.com/!1zure/sdk",
		"github.com/!Azure/sdk",
		"github.com/!!azure/sdk",
		"github.com/azure!/sdk",
		"github.com/!\u00e9zure/sdk",
		"github.com/Azure/sdk",
		"example.com/module/v1",
	} {
		for _, suffix := range []string{"/@v/list", "/@latest", "/@v/v1.0.0.info", "/@v/v1.0.0.mod", "/@v/v1.0.0.zip"} {
			requestPath := "/go/" + escapedPath + suffix
			if target, ok := extractRequestTarget(requestPath); ok {
				t.Errorf("extractRequestTarget(%q) = %#v, want no policy target", requestPath, target)
			}
		}
	}
}

func TestMiddlewareEnforcesArtifactVersionsButNotUnknownMetadataVersions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRulesTestDB(t)

	seed := []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "requests", Version: "< 2.0.0", Action: "deny", Reason: "upgrade requests"},
		{Ecosystem: "pypi", PackageName: "flask", Version: "*", Action: "deny"},
		{Ecosystem: "cargo", PackageName: "serde", Version: "1.0.219", Action: "deny"},
		{Ecosystem: "maven", PackageName: "org.apache.commons:commons-lang3", Version: "3.17.0", Action: "deny"},
		{Ecosystem: "maven", PackageName: "com.acme:123", Version: "*", Action: "deny"},
		{Ecosystem: "maven", PackageName: "com:acme", Version: "123", Action: "deny"},
		{Ecosystem: "maven", PackageName: "com.acme:app", Version: "1.0", Action: "deny"},
		{Ecosystem: "cran", PackageName: "dplyr", Version: "1.1.4", Action: "deny"},
		{Ecosystem: "cran", PackageName: "ggplot2", Version: "*", Action: "deny"},
		{Ecosystem: "alpine", PackageName: "v3.21/main/x86_64/py3-requests", Version: "2.32.3-r0", Action: "deny"},
		{Ecosystem: "go", PackageName: "github.com/user/repo", Version: "v1.0.0-RC1", Action: "deny"},
		{Ecosystem: "nuget", PackageName: "Newtonsoft.Json", Version: "1.0", Action: "deny"},
		{Ecosystem: "npm", PackageName: "demo", Version: "*", Action: "deny"},
		{Ecosystem: "apt", PackageName: "anna", Version: "*", Action: "deny"},
	}
	store := NewStore(database)
	for index := range seed {
		if err := store.Create(&seed[index]); err != nil {
			t.Fatalf("seed rule %d: %v", index, err)
		}
	}

	engine := NewEngine(store, nil)
	router := gin.New()
	router.Use(Middleware(engine))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{name: "version range blocks matching artifact", path: "/pypi/files/requests-1.9.0.tar.gz", wantStatus: http.StatusForbidden},
		{name: "version range allows newer artifact", path: "/pypi/files/requests-2.1.0.tar.gz", wantStatus: http.StatusNoContent},
		{name: "version range does not block package metadata", path: "/pypi/simple/requests/", wantStatus: http.StatusNoContent},
		{name: "package-wide rule blocks package metadata", path: "/pypi/simple/flask/", wantStatus: http.StatusForbidden},
		{name: "project route cannot bypass version rule", path: "/p/backend/pypi/files/requests-1.9.0.tar.gz", wantStatus: http.StatusForbidden},
		{name: "cargo download path carries version", path: "/crates/api/v1/crates/serde/1.0.219/download", wantStatus: http.StatusForbidden},
		{name: "maven coordinate carries version", path: "/maven/org/apache/commons/commons-lang3/3.17.0/commons-lang3-3.17.0.jar", wantStatus: http.StatusForbidden},
		{name: "maven numeric artifact is blocked without guessing path role", path: "/maven/com/acme/123/1.0/123-1.0.jar", wantStatus: http.StatusForbidden},
		{name: "maven numeric artifact metadata is skipped rather than misidentified", path: "/maven/com/acme/123/maven-metadata.xml", wantStatus: http.StatusNoContent},
		{name: "maven arbitrary packaging cannot bypass exact rule", path: "/maven/com/acme/app/1.0/app-1.0.war", wantStatus: http.StatusForbidden},
		{name: "cran exact rule blocks Windows binary artifact", path: "/cran/bin/windows/contrib/4.5/dplyr_1.1.4.zip", wantStatus: http.StatusForbidden},
		{name: "cran exact rule allows another macOS binary version", path: "/cran/bin/macosx/big-sur-arm64/contrib/4.5/dplyr_1.1.5.tgz", wantStatus: http.StatusNoContent},
		{name: "cran package-wide rule blocks source artifact", path: "/cran/src/contrib/ggplot2_3.5.2.tar.gz", wantStatus: http.StatusForbidden},
		{name: "alpine route is governed", path: "/alpine/v3.21/main/x86_64/py3-requests-2.32.3-r0.apk", wantStatus: http.StatusForbidden},
		{name: "go proxy escaped version matches canonical exact rule", path: "/go/github.com/user/repo/@v/v1.0.0-!r!c1.zip", wantStatus: http.StatusForbidden},
		{name: "nuget URL version matches normalized exact rule", path: "/nuget/v3-flatcontainer/newtonsoft.json/1.0.0/newtonsoft.json.1.0.0.nupkg", wantStatus: http.StatusForbidden},
		{name: "npm metadata remains discoverable before authenticated adapter evaluation", path: "/npm/demo", wantStatus: http.StatusNoContent},
		{name: "legacy npm route is left to adapter rejection", path: "/npm/demo/-/archive.tgz", wantStatus: http.StatusNoContent},
		{name: "signed npm route is left to authenticated adapter evaluation", path: "/npm/demo/-/__depsilo_tarball_v1/opaque-token/archive.tgz", wantStatus: http.StatusNoContent},
		{name: "APT udeb cannot bypass package-wide rule", path: "/apt/debian/pool/main/a/anna/anna_1.0_amd64.udeb", wantStatus: http.StatusForbidden},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("GET %s status = %d, want %d; body = %s", test.path, recorder.Code, test.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestMiddlewareKeepsMultiSegmentCondaChannelsDistinct(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRulesTestDB(t)
	store := NewStore(database)
	if err := store.Create(&db.PackageRule{
		Ecosystem: "conda", PackageName: "pkgs/main/numpy", Version: "*", Action: "deny",
	}); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(Middleware(NewEngine(store, nil)))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, test := range []struct {
		path       string
		wantStatus int
	}{
		{path: "/conda/pkgs/main/linux-64/numpy-2.0.0-py312_0.conda", wantStatus: http.StatusForbidden},
		{path: "/conda/pkgs/r/linux-64/numpy-2.0.0-py312_0.conda", wantStatus: http.StatusNoContent},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != test.wantStatus {
			t.Errorf("GET %s status = %d, want %d; body=%s", test.path, recorder.Code, test.wantStatus, recorder.Body.String())
		}
	}
}

type ruleAuditCapture struct {
	entries []db.AuditLog
}

func (capture *ruleAuditCapture) Log(entry db.AuditLog) {
	capture.entries = append(capture.entries, entry)
}

func TestMiddlewareRecordsPolicyBlockAsHandledRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRulesTestDB(t)
	store := NewStore(database)
	if err := store.Create(&db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "< 2.0.0", Action: "deny",
	}); err != nil {
		t.Fatal(err)
	}
	audit := &ruleAuditCapture{}
	release := adapter.InstallAccessHooks(nil, audit)
	t.Cleanup(release)

	router := gin.New()
	router.Use(Middleware(NewEngine(store, nil)))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/pypi/files/requests-1.9.0.tar.gz", nil)
	request.RemoteAddr = "192.0.2.20:4321"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if len(audit.entries) != 1 {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
	entry := audit.entries[0]
	if entry.Ecosystem != "pypi" || entry.PackageName != "requests" || entry.Version != "1.9.0" ||
		entry.Action != "download" || entry.CacheResult != "blocked" || entry.StatusCode != http.StatusForbidden {
		t.Fatalf("audit entry = %#v", entry)
	}
}

func TestMiddlewareFailsOpenWhenRuleStoreIsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRulesTestDB(t)
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	router := gin.New()
	router.Use(Middleware(NewEngine(NewStore(database), nil)))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pypi/files/requests-1.9.0.tar.gz", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("closed rules DB status = %d, want %d; body = %s", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}
}

func TestMiddlewareUsesLastKnownGoodSnapshotWhenRefreshFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRulesTestDB(t)
	store := NewStore(database)
	if err := store.Create(&db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny",
	}); err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(store, nil)
	router := gin.New()
	router.Use(Middleware(engine))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pypi/files/requests-1.9.0.tar.gz", nil))
		return recorder
	}

	if first := request(); first.Code != http.StatusForbidden {
		t.Fatalf("initial policy status = %d, want %d; body = %s", first.Code, http.StatusForbidden, first.Body.String())
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	engine.InvalidateCache()

	// The database is unavailable after the cache expires, but the last
	// successful snapshot still contains the deny rule and must remain active.
	if second := request(); second.Code != http.StatusForbidden {
		t.Fatalf("stale policy status = %d, want %d; body = %s", second.Code, http.StatusForbidden, second.Body.String())
	}
}

func TestMiddlewareFailsClosedForUnpreparedPersistedRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRulesTestDB(t)
	if err := database.Create(&db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "< 2.0.0", Action: "deny",
	}).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(Middleware(NewEngine(NewStore(database), nil)))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pypi/files/requests-1.9.0.tar.gz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unprepared policy status = %d, want %d; body = %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
}

func TestMiddlewareFailsClosedForTypeCorruptPersistedRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRulesTestDB(t)
	store := NewStore(database)
	rule := db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "< 2.0", Action: "deny",
	}
	if err := store.Create(&rule); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("UPDATE package_rules SET dialect_revision = 'corrupt' WHERE id = ?", rule.ID).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(Middleware(NewEngine(store, nil)))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pypi/files/requests-1.9.0.tar.gz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("type-corrupt policy status = %d, want %d; body = %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
}

func TestMiddlewareFailsClosedWhenAnyPersistedRuleColumnCannotBeScanned(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRulesTestDB(t)
	store := NewStore(database)
	rule := db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "< 2.0", Action: "deny",
	}
	if err := store.Create(&rule); err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("UPDATE package_rules SET updated_at = 7 WHERE id = ?", rule.ID).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(Middleware(NewEngine(store, nil)))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pypi/files/requests-1.9.0.tar.gz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unscannable policy status = %d, want %d; body = %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
}

func TestMiddlewareFailsClosedForNegativePersistedUnsignedFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *gorm.DB, db.PackageRule)
	}{
		{
			name: "negative dialect revision",
			mutate: func(t *testing.T, database *gorm.DB, rule db.PackageRule) {
				t.Helper()
				if err := database.Exec("UPDATE package_rules SET dialect_revision = -1 WHERE id = ?", rule.ID).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "negative primary key",
			mutate: func(t *testing.T, database *gorm.DB, _ db.PackageRule) {
				t.Helper()
				if err := database.Exec(`
					INSERT INTO package_rules
						(id, ecosystem, package_name, version, action, reason, created_by,
						 created_at, updated_at, normalized_package_name, normalized_version, dialect_revision)
					VALUES
						(-1, 'pypi', 'requests', '< 2.0', 'deny', '', '',
						 CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'requests', '< 2.0', 1)
				`).Error; err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			database := newRulesTestDB(t)
			store := NewStore(database)
			rule := db.PackageRule{
				Ecosystem: "pypi", PackageName: "requests", Version: "< 2.0", Action: "deny",
			}
			if err := store.Create(&rule); err != nil {
				t.Fatal(err)
			}
			test.mutate(t, database, rule)

			router := gin.New()
			router.Use(Middleware(NewEngine(store, nil)))
			router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pypi/files/requests-1.9.0.tar.gz", nil))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("corrupt unsigned field status = %d, want %d; body = %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
			}
		})
	}
}

func TestMiddlewareFailsClosedForTextTimestampSQLiteAcceptsButGoCannotScan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRulesTestDB(t)
	store := NewStore(database)
	rule := db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "< 2.0", Action: "deny",
	}
	if err := store.Create(&rule); err != nil {
		t.Fatal(err)
	}
	// SQLite accepts this as a julianday modifier, but it is not a concrete
	// timestamp representation that the Go driver can scan into time.Time.
	if err := database.Exec("UPDATE package_rules SET updated_at = 'now' WHERE id = ?", rule.ID).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(Middleware(NewEngine(store, nil)))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pypi/files/requests-1.9.0.tar.gz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unscannable timestamp status = %d, want %d; body = %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
}

func TestMiddlewareFailsClosedWhenPackageRuleSchemaIsMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRulesTestDB(t)
	if err := database.Migrator().DropTable(&db.PackageRule{}); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(Middleware(NewEngine(NewStore(database), nil)))
	router.GET("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pypi/files/requests-1.9.0.tar.gz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing package-rule schema status = %d, want %d; body = %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
}

func newRulesTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "rules.db")),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatalf("open rules database: %v", err)
	}
	if err := database.AutoMigrate(&db.PackageRule{}); err != nil {
		t.Fatalf("migrate package rules: %v", err)
	}
	return database
}
