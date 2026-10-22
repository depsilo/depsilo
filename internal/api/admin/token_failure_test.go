package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"depsilo/internal/db"
)

func TestTokenStorageFailuresAreNotEmptyOrMissing(t *testing.T) {
	database := openCredentialTestDB(t, "token-errors.db", &db.APIToken{})
	handler := NewTokenHandler(database)
	router := principalTestRouter(true)
	router.GET("/tokens", handler.List)
	router.DELETE("/tokens/:id", handler.Delete)
	if err := database.Migrator().DropTable(&db.APIToken{}); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct{ method, path string }{{http.MethodGet, "/tokens"}, {http.MethodDelete, "/tokens/1"}} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(request.method, request.path, nil))
		if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"DB_ERROR"`) {
			t.Fatalf("%s = %d %s", request.method, response.Code, response.Body.String())
		}
	}
}
