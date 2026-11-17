package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"depsilo/internal/db"
	"depsilo/internal/middleware"
)

func TestCreateTokenRejectsUnknownPermissionsWithAllowedValues(t *testing.T) {
	database := openCredentialTestDB(t, "token-create.db", &db.APIToken{})
	handler := NewTokenHandler(database)
	router := principalTestRouter(true)
	router.POST("/tokens", func(c *gin.Context) { c.Set(middleware.ContextKeyUserID, uint(1)) }, handler.Create)
	create := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/tokens", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	for _, body := range []string{
		`{"name":"ci","permissions":"read"}`,
		`{"name":"ci","permissions":"ReadOnly"}`,
		`{"name":"ci","permissions":""}`,
		`{"name":"ci"}`,
	} {
		response := create(body)
		var problem struct{ Code, Message string }
		if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
			t.Fatalf("%s: body %q is not a JSON error: %v", body, response.Body.String(), err)
		}
		if response.Code != http.StatusBadRequest || problem.Code != "BAD_REQUEST" {
			t.Fatalf("%s = %d %s, want 400 BAD_REQUEST", body, response.Code, response.Body.String())
		}
		if problem.Message != `permissions must be "readonly" or "readwrite"` {
			t.Fatalf("%s message = %q, want the allowed values", body, problem.Message)
		}
	}
	var stored int64
	if err := database.Model(&db.APIToken{}).Count(&stored).Error; err != nil || stored != 0 {
		t.Fatalf("rejected requests stored %d tokens (err=%v)", stored, err)
	}

	// A missing name keeps its own 400.
	if response := create(`{"permissions":"readonly"}`); response.Code != http.StatusBadRequest ||
		!strings.Contains(response.Body.String(), `"code":"BAD_REQUEST"`) {
		t.Fatalf("missing name = %d %s", response.Code, response.Body.String())
	}

	for _, permissions := range []string{"readonly", "readwrite"} {
		response := create(`{"name":"ci","permissions":"` + permissions + `"}`)
		var created struct{ Token, Permissions string }
		if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusCreated || created.Permissions != permissions || !strings.HasPrefix(created.Token, "rc_") {
			t.Fatalf("create %s token = %d %s", permissions, response.Code, response.Body.String())
		}
	}
}
