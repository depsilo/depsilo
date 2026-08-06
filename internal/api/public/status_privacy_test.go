package public

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/db"
)

func TestAnonymousStatsDoesNotExposeRequestIdentity(t *testing.T) {
	database := newStatsTestDB(t)
	if err := database.AutoMigrate(&db.AccessLog{}, &db.CacheEntry{}); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.AccessLog{
		AdapterType: "pypi",
		PackageName: "private-customer-package",
		Hit:         true,
		StatusCode:  http.StatusOK,
		CreatedAt:   time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	stats := NewStatsHandler(database, nil, nil, nil, nil, false)
	router.GET("/stats", stats.GetStats)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/stats", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "private-customer-package") {
		t.Fatalf("anonymous stats exposed a package name: %s", recorder.Body.String())
	}

	var body any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"package_name", "top_packages"} {
		if containsJSONField(body, field) {
			t.Fatalf("anonymous stats exposed request field %q: %s", field, recorder.Body.String())
		}
	}
}

func containsJSONField(value any, field string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == field || containsJSONField(child, field) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsJSONField(child, field) {
				return true
			}
		}
	}
	return false
}
