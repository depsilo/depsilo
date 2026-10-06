package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/config"
)

func TestCascadeHandlerReturnsTopologyWithoutSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewCascadeHandler(config.CascadeConfig{
		Enabled:           true,
		Token:             "mesh-secret-token-0123456789",
		MaxHops:           3,
		MaxTTL:            12 * time.Hour,
		AllowInsecureHTTP: true,
		Peers: []config.CascadePeerConfig{{
			Name:               "home",
			URL:                "http://192.168.1.10:23333",
			Token:              "peer-secret-token-0123456789",
			ForwardCredentials: true,
		}},
	}, "node-1")

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	handler.Info(context)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "secret") {
		t.Fatalf("cascade info leaked a token: %s", body)
	}
	var response cascadeInfoResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Enabled || response.InstanceID != "node-1" || response.MaxHops != 3 {
		t.Fatalf("response = %#v", response)
	}
	if response.RelayPath != "/_depsilo/relay/v1" || !response.AllowInsecureHTTP {
		t.Fatalf("response = %#v", response)
	}
	if response.MaxTTLSeconds != int64((12 * time.Hour).Seconds()) {
		t.Fatalf("max ttl = %d", response.MaxTTLSeconds)
	}
	if len(response.Peers) != 1 || response.Peers[0].Name != "home" ||
		response.Peers[0].URL != "http://192.168.1.10:23333" || !response.Peers[0].ForwardCredentials {
		t.Fatalf("peers = %#v", response.Peers)
	}
}
