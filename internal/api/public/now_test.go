package public

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

func TestNowQueryFailuresDoNotBecomeIdleStatistics(t *testing.T) {
	for _, query := range []string{"strftime", "created_at, adapter_type", "SUM(bytes_sent)"} {
		t.Run(query, func(t *testing.T) {
			database := newStatsTestDB(t)
			if err := database.AutoMigrate(&db.AccessLog{}); err != nil {
				t.Fatal(err)
			}
			injected := false
			if err := database.Callback().Row().Before("gorm:row").Register("test:query_failure", func(tx *gorm.DB) {
				selectSQL := strings.Join(tx.Statement.Selects, " ") + fmt.Sprint(tx.Statement.Clauses["SELECT"].Expression)
				if strings.Contains(selectSQL, query) {
					injected = true
					tx.AddError(errors.New("private database failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}

			response := requestNow(NewNowHandler(database, nil, time.Now(), nil))
			if !injected {
				t.Fatal("test did not reach the target query")
			}
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("failed query returned %d: %s", response.Code, response.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["code"] != "DB_ERROR" || body["rate"] != nil || body["sparkline"] != nil {
				t.Fatalf("failure must be explicit, without fabricated samples: %#v", body)
			}
			if strings.Contains(response.Body.String(), "private database failure") {
				t.Fatal("response exposed database details")
			}
		})
	}
}

func TestNowSuccessfulEmptyQueryRemainsMeasuredZero(t *testing.T) {
	database := newStatsTestDB(t)
	if err := database.AutoMigrate(&db.AccessLog{}); err != nil {
		t.Fatal(err)
	}
	response := requestNow(NewNowHandler(database, nil, time.Now(), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("empty database returned %d: %s", response.Code, response.Body.String())
	}
	var body nowResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.LastActivity != nil || body.Rate.HasData || body.Rate.RequestsPerMin != 0 || len(body.Sparkline) != 30 {
		t.Fatalf("unexpected empty statistics: %#v", body)
	}
}

func requestNow(handler *NowHandler) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/now", handler.Get)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/now", nil))
	return response
}
