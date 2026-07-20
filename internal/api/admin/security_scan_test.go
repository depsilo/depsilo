package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
	"depsilo/internal/security"
)

func TestTriggerScanAtomicallyReservesServerOwnedJob(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "manual-scan.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(
		&db.CacheEntry{},
		&db.Vulnerability{},
		&db.VulnerabilityCheck{},
		&db.SecurityPolicy{},
		&db.PackageRule{},
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.CacheEntry{
		Key:         "pypi/manual-scan/index",
		AdapterType: "pypi",
		PackageName: "manual-scan",
	}).Error; err != nil {
		t.Fatal(err)
	}

	releaseOSV := make(chan struct{})
	defer close(releaseOSV)
	osvServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-releaseOSV
	}))
	t.Cleanup(osvServer.Close)
	fetcher := security.NewFetcher(osvServer.URL, "")
	defer fetcher.Close()
	scanner := security.NewScanner(database, fetcher, nil)
	serverContext, cancelServer := context.WithCancel(context.Background())
	handler := NewSecurityHandlerWithContext(serverContext, database, scanner, nil, nil)
	handler.scanTimeout = time.Minute
	router := gin.New()
	router.POST("/security/scan", handler.TriggerScan)

	requestContext, cancelRequest := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/security/scan", nil).WithContext(requestContext)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("first trigger status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	cancelRequest()
	time.Sleep(20 * time.Millisecond)
	if !scanner.IsScanning() {
		t.Fatal("manual scan inherited the completed request context")
	}

	const contenders = 16
	start := make(chan struct{})
	statuses := make(chan int, contenders)
	var requests sync.WaitGroup
	for range contenders {
		requests.Add(1)
		go func() {
			defer requests.Done()
			<-start
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/security/scan", nil))
			statuses <- recorder.Code
		}()
	}
	close(start)
	requests.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusConflict {
			t.Errorf("contending trigger status = %d, want 409", status)
		}
	}

	cancelServer()
	closeContext, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelClose()
	if err := scanner.Close(closeContext); err != nil {
		t.Fatalf("close scanner: %v", err)
	}
	if scanner.IsScanning() {
		t.Fatal("server cancellation left the manual scan running")
	}
}
