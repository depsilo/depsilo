package rules

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
			version: "8.1.0-1ubuntu1",
		},
		"npm": {
			path:    "/npm/@scope/internal-utils/-/internal-utils-1.4.2.tgz",
			name:    "@scope/internal-utils",
			version: "1.4.2",
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
		"rubygems": {
			path:    "/rubygems/gems/rake-13.2.1.gem",
			name:    "rake",
			version: "13.2.1",
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
		"helm": {
			path:    "/helm/ingress-nginx-4.12.1.tgz",
			name:    "ingress-nginx",
			version: "4.12.1",
		},
	}

	// RuleDefinitions is also the package-rules UI's ecosystem set. Basing
	// this assertion on the catalog catches a newly exposed ecosystem whose
	// route has not yet gained an accurate package/version extractor.
	for _, definition := range ecosystemcatalog.RuleDefinitions() {
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
			name:   "maven artifact uses coordinate and directory version",
			path:   "/maven/io/netty/netty-all/4.1.118.Final/netty-all-4.1.118.Final.jar",
			want:   requestTarget{Ecosystem: "maven", PackageName: "io.netty:netty-all", Version: "4.1.118.Final"},
			wantOK: true,
		},
		{
			name:   "maven package metadata has unknown version",
			path:   "/maven/io/netty/netty-all/maven-metadata.xml",
			want:   requestTarget{Ecosystem: "maven", PackageName: "io.netty:netty-all"},
			wantOK: true,
		},
		{
			name:   "alpine artifact keeps resolver package key",
			path:   "/p/platform/alpine/v3.20/community/aarch64/nodejs-20.15.1-r0.apk",
			want:   requestTarget{Ecosystem: "alpine", PackageName: "v3.20/community/aarch64/nodejs", Version: "20.15.1-r0"},
			wantOK: true,
		},
		{
			name:   "malformed artifact without a version fails open",
			path:   "/pypi/files/not-a-valid-wheel.whl",
			wantOK: false,
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

func TestMiddlewareEnforcesArtifactVersionsButNotUnknownMetadataVersions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRulesTestDB(t)

	seed := []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "requests", Version: "< 2.0.0", Action: "deny", Reason: "upgrade requests"},
		{Ecosystem: "pypi", PackageName: "flask", Version: "*", Action: "deny"},
		{Ecosystem: "cargo", PackageName: "serde", Version: "1.0.219", Action: "deny"},
		{Ecosystem: "maven", PackageName: "org.apache.commons:commons-lang3", Version: "3.17.0", Action: "deny"},
		{Ecosystem: "alpine", PackageName: "v3.21/main/x86_64/py3-requests", Version: "2.32.3-r0", Action: "deny"},
	}
	if err := database.Create(&seed).Error; err != nil {
		t.Fatalf("seed rules: %v", err)
	}

	engine := NewEngine(NewStore(database), nil)
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
		{name: "alpine route is governed", path: "/alpine/v3.21/main/x86_64/py3-requests-2.32.3-r0.apk", wantStatus: http.StatusForbidden},
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

type ruleAuditCapture struct {
	entries []db.AuditLog
}

func (capture *ruleAuditCapture) Log(entry db.AuditLog) {
	capture.entries = append(capture.entries, entry)
}

func TestMiddlewareRecordsPolicyBlockAsHandledRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newRulesTestDB(t)
	if err := database.Create(&db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "< 2.0.0", Action: "deny",
	}).Error; err != nil {
		t.Fatal(err)
	}
	audit := &ruleAuditCapture{}
	release := adapter.InstallAccessHooks(nil, audit)
	t.Cleanup(release)

	router := gin.New()
	router.Use(Middleware(NewEngine(NewStore(database), nil)))
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
