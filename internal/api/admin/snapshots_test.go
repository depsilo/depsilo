package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"depsilo/internal/db"
	"depsilo/internal/snapshot"
)

func newSnapshotsTestRouter(t *testing.T) (*gin.Engine, *snapshot.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "snapshots-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	records := []db.TamperRecord{
		{Key: "pypi/files/requests-2.31.0.whl", Ecosystem: "pypi", Package: "requests", Version: "2.31.0", SHA256: strings.Repeat("a", 64), Size: 100},
		{Key: "npm/files/left-pad-1.3.0.tgz", Ecosystem: "npm", Package: "left-pad", Version: "1.3.0", SHA256: strings.Repeat("b", 64), Size: 25},
	}
	if err := database.Create(&records).Error; err != nil {
		t.Fatal(err)
	}
	store := snapshot.NewStore(database)
	handler := NewSnapshotsHandler(store, database)
	router := gin.New()
	router.GET("/snapshots", handler.List)
	router.GET("/snapshots/:id", handler.Get)
	router.GET("/snapshots/:id/export", handler.Export)
	router.POST("/snapshots", handler.Create)
	router.POST("/snapshots/import", handler.Import)
	router.PUT("/snapshots/active", handler.Activate)
	router.DELETE("/snapshots/:id", handler.Delete)
	return router, store
}

func TestSnapshotsAPILifecycle(t *testing.T) {
	router, _ := newSnapshotsTestRouter(t)

	create := httptest.NewRecorder()
	router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/snapshots",
		bytes.NewBufferString(`{"name":"golden-1","note":"release candidate"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var created db.Snapshot
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ArtifactCount != 2 || created.TotalBytes != 125 {
		t.Fatalf("created snapshot = %+v", created)
	}

	duplicate := httptest.NewRecorder()
	router.ServeHTTP(duplicate, httptest.NewRequest(http.MethodPost, "/snapshots",
		bytes.NewBufferString(`{"name":"golden-1"}`)))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}

	detail := httptest.NewRecorder()
	router.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/snapshots/1", nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"total":2`) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}

	activate := httptest.NewRecorder()
	router.ServeHTTP(activate, httptest.NewRequest(http.MethodPut, "/snapshots/active",
		bytes.NewBufferString(`{"snapshot_id":1}`)))
	if activate.Code != http.StatusOK || !strings.Contains(activate.Body.String(), `"active_snapshot":"golden-1"`) {
		t.Fatalf("activate status=%d body=%s", activate.Code, activate.Body.String())
	}

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/snapshots", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"active_snapshot_id":1`) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}

	deleteActive := httptest.NewRecorder()
	router.ServeHTTP(deleteActive, httptest.NewRequest(http.MethodDelete, "/snapshots/1", nil))
	if deleteActive.Code != http.StatusConflict {
		t.Fatalf("delete active status=%d body=%s", deleteActive.Code, deleteActive.Body.String())
	}

	export := httptest.NewRecorder()
	router.ServeHTTP(export, httptest.NewRequest(http.MethodGet, "/snapshots/1/export", nil))
	if export.Code != http.StatusOK ||
		!strings.Contains(export.Header().Get("Content-Disposition"), "snapshot-golden-1.json") ||
		!strings.Contains(export.Body.String(), `"format":"depsilo/snapshot/v1"`) {
		t.Fatalf("export status=%d headers=%v body=%s", export.Code, export.Header(), export.Body.String())
	}

	imported := httptest.NewRecorder()
	router.ServeHTTP(imported, httptest.NewRequest(http.MethodPost, "/snapshots/import?name=golden-copy",
		bytes.NewReader(export.Body.Bytes())))
	if imported.Code != http.StatusCreated || !strings.Contains(imported.Body.String(), `"name":"golden-copy"`) {
		t.Fatalf("import status=%d body=%s", imported.Code, imported.Body.String())
	}

	deactivate := httptest.NewRecorder()
	router.ServeHTTP(deactivate, httptest.NewRequest(http.MethodPut, "/snapshots/active",
		bytes.NewBufferString(`{"snapshot_id":0}`)))
	if deactivate.Code != http.StatusOK || !strings.Contains(deactivate.Body.String(), `"active_snapshot_id":0`) {
		t.Fatalf("deactivate status=%d body=%s", deactivate.Code, deactivate.Body.String())
	}

	remove := httptest.NewRecorder()
	router.ServeHTTP(remove, httptest.NewRequest(http.MethodDelete, "/snapshots/1", nil))
	if remove.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", remove.Code, remove.Body.String())
	}
}

func TestSnapshotsAPIRejectsEmptyCacheAndUnknownSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "empty-snapshots-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	handler := NewSnapshotsHandler(snapshot.NewStore(database), database)
	router := gin.New()
	router.POST("/snapshots", handler.Create)
	router.PUT("/snapshots/active", handler.Activate)

	create := httptest.NewRecorder()
	router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/snapshots",
		bytes.NewBufferString(`{"name":"empty"}`)))
	if create.Code != http.StatusBadRequest || !strings.Contains(create.Body.String(), "EMPTY_SNAPSHOT") {
		t.Fatalf("empty create status=%d body=%s", create.Code, create.Body.String())
	}

	activate := httptest.NewRecorder()
	router.ServeHTTP(activate, httptest.NewRequest(http.MethodPut, "/snapshots/active",
		bytes.NewBufferString(`{"snapshot_id":42}`)))
	if activate.Code != http.StatusNotFound {
		t.Fatalf("unknown activate status=%d body=%s", activate.Code, activate.Body.String())
	}
}
