package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/distribution"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func distributionAPIFixture(t *testing.T) (*gin.Engine, *distribution.Store, *gorm.DB, []byte, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewPrivateLocalStorage(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := distribution.NewStore(database, storage, distribution.Limits{MaxObjectBytes: 1024, MaxStorageBytes: 2048})
	if err != nil {
		t.Fatal(err)
	}
	h := NewDistributionHandler(store, database)
	router := gin.New()
	router.GET("/sets", h.List)
	router.GET("/sets/:id", h.Get)
	router.POST("/sets", h.Stage)
	router.PUT("/sets/:id/objects/:digest", h.Put)
	router.POST("/sets/:id/verify", h.Verify)
	router.DELETE("/sets/:id", h.Delete)
	router.POST("/prune", h.Prune)
	sum := sha256.Sum256([]byte("wheel"))
	digest := hex.EncodeToString(sum[:])
	raw, err := json.Marshal(distribution.Manifest{Format: distribution.ManifestFormat, Name: "fixture", Revision: "v1", Ecosystem: "pypi",
		Target:  distribution.Target{OS: "linux", Architecture: "amd64", PythonVersion: "3.12.7", PythonABI: "cp312", PipVersion: "24.2", ClientImage: "python@sha256:" + strings.Repeat("a", 64)},
		Objects: []distribution.Object{{SHA256: digest, Size: 5, Kind: "wheel", Filename: "example-1.0-py3-none-any.whl", Package: "example", Version: "1.0"}}})
	if err != nil {
		t.Fatal(err)
	}
	return router, store, database, raw, digest
}

func distributionAPIRequest(router *gin.Engine, method, path string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	// Exercise streamed uploads without a trustworthy Content-Length.
	if method == http.MethodPut {
		r.ContentLength = -1
		r.TransferEncoding = []string{"chunked"}
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

func TestDistributionAPICatalogLifecycle(t *testing.T) {
	router, store, database, raw, digest := distributionAPIFixture(t)
	created := distributionAPIRequest(router, http.MethodPost, "/sets", raw)
	if created.Code != 201 {
		t.Fatalf("stage %d: %s", created.Code, created.Body)
	}
	var set distribution.Set
	if err := json.Unmarshal(created.Body.Bytes(), &set); err != nil {
		t.Fatal(err)
	}
	base := "/sets/" + set.ID
	for i := 0; i < 2; i++ {
		r := distributionAPIRequest(router, http.MethodPost, "/sets", raw)
		if r.Code != 201 || !strings.Contains(r.Body.String(), set.ID) {
			t.Fatalf("repeat stage: %d %s", r.Code, r.Body)
		}
	}
	missing := distributionAPIRequest(router, http.MethodPost, base+"/verify", nil)
	if missing.Code != 200 || !strings.Contains(missing.Body.String(), `"content_status":"incomplete"`) || !strings.Contains(missing.Body.String(), digest) {
		t.Fatalf("missing content: %d %s", missing.Code, missing.Body)
	}
	bad := distributionAPIRequest(router, http.MethodPut, base+"/objects/"+digest, []byte("wheel-extra"))
	if bad.Code != 422 {
		t.Fatalf("oversized object: %d %s", bad.Code, bad.Body)
	}
	for _, tc := range []struct {
		body   string
		reused bool
	}{{"wheel", false}, {"wheel", true}} {
		r := distributionAPIRequest(router, http.MethodPut, base+"/objects/"+digest, []byte(tc.body))
		if r.Code != 200 || !strings.Contains(r.Body.String(), fmt.Sprintf(`"reused":%t`, tc.reused)) {
			t.Fatalf("upload %d %s", r.Code, r.Body)
		}
	}
	verified := distributionAPIRequest(router, http.MethodPost, base+"/verify", nil)
	if verified.Code != 200 || !strings.Contains(verified.Body.String(), `"content_status":"content_verified"`) || !strings.Contains(verified.Body.String(), `"serving":false`) {
		t.Fatalf("verify %d %s", verified.Code, verified.Body)
	}
	listed := distributionAPIRequest(router, http.MethodGet, "/sets?per_page=10000", nil)
	if listed.Code != 200 || strings.Contains(listed.Body.String(), `"manifest"`) || !strings.Contains(listed.Body.String(), `"per_page":20`) || !strings.Contains(listed.Body.String(), `"scope":"catalog_only"`) {
		t.Fatalf("list %d %s", listed.Code, listed.Body)
	}
	detail := distributionAPIRequest(router, http.MethodGet, base, nil)
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), digest) || detail.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("detail %d %s", detail.Code, detail.Body)
	}
	if _, err := store.Activate(context.Background(), set.ID); err != nil {
		t.Fatal(err)
	}
	blocked := distributionAPIRequest(router, http.MethodDelete, base, nil)
	if blocked.Code != 409 || !strings.Contains(blocked.Body.String(), "SET_ACTIVE") {
		t.Fatalf("delete active %d %s", blocked.Code, blocked.Body)
	}
	// Internal activation is deliberately absent from the HTTP operator surface.
	if r := distributionAPIRequest(router, http.MethodPost, base+"/activate", nil); r.Code != 404 {
		t.Fatalf("unexpected activation route: %d", r.Code)
	}
	if err := database.Where("set_id = ?", set.ID).Delete(&db.DistributionActiveSet{}).Error; err != nil {
		t.Fatal(err)
	}
	if r := distributionAPIRequest(router, http.MethodDelete, base, nil); r.Code != 204 {
		t.Fatalf("delete %d %s", r.Code, r.Body)
	}
	pruned := distributionAPIRequest(router, http.MethodPost, "/prune", nil)
	if pruned.Code != 200 || !strings.Contains(pruned.Body.String(), `"removed_bytes":5`) {
		t.Fatalf("prune %d %s", pruned.Code, pruned.Body)
	}
	var actions []string
	if err := database.Model(&db.AuditLog{}).Order("id").Pluck("action", &actions).Error; err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"dist_stage", "dist_upload", "dist_verify", "dist_delete", "dist_prune"} {
		if !strings.Contains(strings.Join(actions, ","), wanted) {
			t.Fatalf("missing audit %s in %v", wanted, actions)
		}
	}
}

func TestDistributionAPIBoundedInputAndSanitizedFailures(t *testing.T) {
	router, _, database, _, _ := distributionAPIFixture(t)
	if r := distributionAPIRequest(router, http.MethodPost, "/sets", bytes.Repeat([]byte("x"), distribution.MaxManifestBytes+1)); r.Code != 413 {
		t.Fatalf("oversize manifest %d", r.Code)
	}
	for _, path := range []string{"/sets/not-a-digest", "/sets?page=1000001"} {
		if r := distributionAPIRequest(router, http.MethodGet, path, nil); r.Code != 400 {
			t.Fatalf("invalid path %s: %d", path, r.Code)
		}
	}
	sqlDB, _ := database.DB()
	_ = sqlDB.Close()
	r := distributionAPIRequest(router, http.MethodGet, "/sets", nil)
	if r.Code != 500 || strings.Contains(r.Body.String(), "sql:") || strings.Contains(r.Body.String(), "catalog.db") {
		t.Fatalf("unsafe backend error %d %s", r.Code, r.Body)
	}
}
