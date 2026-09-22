package public

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"depsilo/internal/db"
	"github.com/gin-gonic/gin"
)

func TestNowRateDistinguishesSamplingFromMeasuredZero(t *testing.T) {
	database := newStatsTestDB(t)
	if err := database.AutoMigrate(&db.AccessLog{}); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	for _, test := range []struct {
		name       string
		startedAgo time.Duration
		state      string
		hasData    bool
	}{
		{name: "sampling", startedAgo: 10 * time.Second, state: "sampling", hasData: false},
		{name: "complete zero", startedAgo: 61 * time.Second, state: "ready", hasData: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := time.Now().UTC().Add(-test.startedAgo)
			handler := NewNowHandler(database, nil, started)
			router := gin.New()
			router.GET("/now", handler.Get)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/now", nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var body nowResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Rate.State != test.state || body.Rate.HasData != test.hasData {
				t.Fatalf("rate=%+v, want state=%s hasData=%t", body.Rate, test.state, test.hasData)
			}
			if body.Rate.RequestsPerSecond != 0 || body.Rate.EgressBps != 0 {
				t.Fatalf("zero window returned non-zero rates: %+v", body.Rate)
			}
		})
	}
}
