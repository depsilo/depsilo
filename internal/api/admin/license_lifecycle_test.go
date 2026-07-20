package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"depsilo/internal/asyncruntime"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/license"
)

type licenseTaskSubmitter struct {
	task asyncruntime.Task
	err  error
}

func (submitter *licenseTaskSubmitter) Submit(task asyncruntime.Task) error {
	if submitter.err != nil {
		return submitter.err
	}
	submitter.task = task
	return nil
}

func TestLicenseRevalidateReportsAdmissionState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("DEPSILO_DEV_PRO", "0")
	database := openCredentialTestDB(t, "license-revalidate.db", &db.LicenseStorage{})
	submitter := &licenseTaskSubmitter{err: asyncruntime.ErrClosed}
	manager := license.NewManagerWithSubmitter(
		submitter,
		config.LicenseConfig{Key: "depsilo-test-key"},
		database,
	)
	handler := NewLicenseHandler(manager, nil, nil)
	request := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		ginContext, _ := gin.CreateTestContext(recorder)
		ginContext.Request = httptest.NewRequest(http.MethodPost, "/license/revalidate", nil)
		handler.Revalidate(ginContext)
		return recorder
	}

	if recorder := request(); recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "SERVER_SHUTTING_DOWN") {
		t.Fatalf("rejected status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	submitter.err = nil
	if recorder := request(); recorder.Code != http.StatusAccepted {
		t.Fatalf("accepted status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder := request(); recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "REVALIDATION_RUNNING") {
		t.Fatalf("duplicate status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	submitter.task(context.Background())
}

func TestLicenseSetKeyDoesNotHidePersistenceFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("DEPSILO_DEV_PRO", "0")
	database := openCredentialTestDB(t, "license-set-key.db", &db.LicenseStorage{})
	manager := license.NewManager(config.LicenseConfig{}, database)
	handler := NewLicenseHandler(manager, nil, nil)

	requestContext, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(
		http.MethodPut,
		"/license/key",
		strings.NewReader(`{"key":"depsilo-test-key"}`),
	).WithContext(requestContext)
	ginContext.Request.Header.Set("Content-Type", "application/json")

	handler.SetKey(ginContext)

	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "DB_ERROR") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if manager.Status().KeyMasked != "" {
		t.Fatalf("manager key changed after failed persistence: %#v", manager.Status())
	}
}
