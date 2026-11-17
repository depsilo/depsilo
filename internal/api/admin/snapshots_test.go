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
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/snapshot"
)

func newSnapshotsTestRouter(t *testing.T) (*gin.Engine, *snapshot.Store) {
	t.Helper()
	router, store, _ := newSnapshotsTestRouterWithDatabase(t)
	return router, store
}

func newSnapshotsTestRouterWithDatabase(t *testing.T) (*gin.Engine, *snapshot.Store, *gorm.DB) {
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
	return router, store, database
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

// A body that is not a manifest, or a manifest that breaks one of the store's
// rules, is the client's error; a failure while a valid manifest is being
// stored is the server's. Both keep the code and message shape of
// SNAPSHOT_FAILED.
func TestSnapshotsImportAnswers400ForARejectedBodyAnd500ForAFailedApply(t *testing.T) {
	const validItem = `{"ecosystem":"npm","package":"left-pad","version":"1.3.0",` +
		`"cache_key":"npm/files/left-pad-1.3.0.tgz",` +
		`"sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","size":25}`
	const validManifest = `{"format":"depsilo/snapshot/v1","name":"golden","items":[` + validItem + `]}`
	manifestWithItem := func(item string) string {
		return `{"format":"depsilo/snapshot/v1","name":"golden","items":[` + item + `]}`
	}

	importManifest := func(t *testing.T, router *gin.Engine, body string) (int, string, string) {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/snapshots/import", strings.NewReader(body)))
		var payload struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if response.Code != http.StatusCreated {
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("error body is not JSON: %v: %s", err, response.Body.String())
			}
		}
		return response.Code, payload.Code, payload.Message
	}

	for _, test := range []struct {
		name        string
		body        string
		wantMessage string // the message, or its beginning when it ends in "..."
	}{
		// The body cannot be decoded.
		{"not JSON", "not a manifest", "decode snapshot manifest: ..."},
		{"empty body", "", "decode snapshot manifest: ..."},
		{"truncated JSON", validManifest[:len(validManifest)/2], "decode snapshot manifest: ..."},
		{"items of another type", `{"format":"depsilo/snapshot/v1","name":"golden","items":"all"}`, "decode snapshot manifest: ..."},
		// The body is JSON and fails one of the manifest's rules.
		{"empty object", `{}`, `unsupported snapshot manifest format ""`},
		{"null", `null`, `unsupported snapshot manifest format ""`},
		{"another object", `{"hello":"world"}`, `unsupported snapshot manifest format ""`},
		{"another format", `{"format":"something/else","name":"golden","items":[` + validItem + `]}`,
			`unsupported snapshot manifest format "something/else"`},
		{"no name", `{"format":"depsilo/snapshot/v1","items":[` + validItem + `]}`, "snapshot name is required"},
		{"more items than the limit",
			`{"format":"depsilo/snapshot/v1","name":"golden","items":[` + strings.Repeat("{},", snapshot.ImportItemLimit) + `{}]}`,
			"snapshot manifest has 200001 items; the import limit is 200000"},
		{"item without an identity field", manifestWithItem(`{"ecosystem":"npm"}`), "snapshot item is missing an identity field"},
		{"item with a short SHA-256",
			manifestWithItem(`{"ecosystem":"npm","package":"a","version":"1","cache_key":"npm/files/a-1.tgz","sha256":"zz","size":1}`),
			"snapshot item a@1 has an invalid SHA-256"},
		{"item with a SHA-256 that is not hexadecimal",
			manifestWithItem(`{"ecosystem":"npm","package":"a","version":"1","cache_key":"npm/files/a-1.tgz","sha256":"` + strings.Repeat("z", 64) + `","size":1}`),
			"snapshot item a@1 has an invalid SHA-256"},
		{"item with a negative size",
			manifestWithItem(`{"ecosystem":"npm","package":"a","version":"1","cache_key":"npm/files/a-1.tgz","sha256":"` + strings.Repeat("b", 64) + `","size":-1}`),
			"snapshot item a@1 has a negative size"},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, store := newSnapshotsTestRouter(t)
			status, code, message := importManifest(t, router, test.body)
			messageMatches := message == test.wantMessage
			if prefix, isPrefix := strings.CutSuffix(test.wantMessage, "..."); isPrefix {
				messageMatches = strings.HasPrefix(message, prefix)
			}
			if status != http.StatusBadRequest || code != "SNAPSHOT_FAILED" || !messageMatches {
				t.Fatalf("status=%d code=%q message=%q, want 400 SNAPSHOT_FAILED %q", status, code, message, test.wantMessage)
			}
			if records, err := store.List(t.Context()); err != nil || len(records) != 0 {
				t.Fatalf("a rejected upload stored a snapshot: %v, %v", records, err)
			}
		})
	}

	// A manifest with no items keeps the code it always had.
	t.Run("no items", func(t *testing.T) {
		router, _ := newSnapshotsTestRouter(t)
		status, code, _ := importManifest(t, router, `{"format":"depsilo/snapshot/v1","name":"golden","items":[]}`)
		if status != http.StatusBadRequest || code != "EMPTY_SNAPSHOT" {
			t.Fatalf("status=%d code=%q, want 400 EMPTY_SNAPSHOT", status, code)
		}
	})

	// So does a name that is taken: the manifest is valid, the conflict is
	// found while it is applied.
	t.Run("name already taken", func(t *testing.T) {
		router, _ := newSnapshotsTestRouter(t)
		if status, code, message := importManifest(t, router, validManifest); status != http.StatusCreated {
			t.Fatalf("first import: status=%d code=%q message=%q, want 201", status, code, message)
		}
		status, code, _ := importManifest(t, router, validManifest)
		if status != http.StatusConflict || code != "NAME_TAKEN" {
			t.Fatalf("status=%d code=%q, want 409 NAME_TAKEN", status, code)
		}
	})

	t.Run("valid manifest", func(t *testing.T) {
		router, _ := newSnapshotsTestRouter(t)
		if status, code, message := importManifest(t, router, validManifest); status != http.StatusCreated {
			t.Fatalf("status=%d code=%q message=%q, want 201", status, code, message)
		}
	})

	t.Run("valid manifest that cannot be stored", func(t *testing.T) {
		router, _, database := newSnapshotsTestRouterWithDatabase(t)
		if err := database.Migrator().DropTable(&db.SnapshotItem{}); err != nil {
			t.Fatal(err)
		}
		status, code, message := importManifest(t, router, validManifest)
		if status != http.StatusInternalServerError || code != "SNAPSHOT_FAILED" || message == "" {
			t.Fatalf("status=%d code=%q message=%q, want 500 SNAPSHOT_FAILED", status, code, message)
		}
	})
}
