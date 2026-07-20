package public

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDiscoverUsesCanonicalEcosystemRoutes(t *testing.T) {
	router := gin.New()
	router.GET("/api/v1/discover", NewDiscoverHandler([]string{"cargo", "docker", "custom"}).Discover)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/discover", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Ecosystems []ecosystemInfo `json:"ecosystems"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	want := []ecosystemInfo{
		{Name: "cargo", Path: "/crates/", Purpose: ecosystemPurposes["cargo"]},
		{Name: "docker", Path: "/v2/", Purpose: ecosystemPurposes["docker"]},
		{Name: "custom", Path: "/custom/", Purpose: "custom"},
	}
	if len(response.Ecosystems) != len(want) {
		t.Fatalf("ecosystems = %#v", response.Ecosystems)
	}
	for index := range want {
		if response.Ecosystems[index] != want[index] {
			t.Fatalf("ecosystem[%d] = %#v, want %#v", index, response.Ecosystems[index], want[index])
		}
	}
}
