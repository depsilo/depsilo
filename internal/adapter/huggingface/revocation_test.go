package huggingface

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/db"
)

func TestRepositoryRevocationIdentityCanonicalizesCaseAliases(t *testing.T) {
	alias, aliasOK := repositoryForParsed(ParseRequestPath(
		"/OpenAI/Whisper-Tiny/resolve/main/config.json",
	))
	canonical, canonicalOK := repositoryForParsed(ParseRequestPath(
		"/openai/whisper-tiny/resolve/main/config.json",
	))
	if !aliasOK || !canonicalOK {
		t.Fatal("recognized repository path did not produce a revocation identity")
	}
	if alias != canonical {
		t.Fatalf("case aliases have different revocation identities: %+v != %+v", alias, canonical)
	}
}

func TestRepositoryRevocationStatusClassification(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		code       string
		provenance responseProvenance
		want       bool
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, want: true},
		{name: "forbidden", status: http.StatusForbidden, want: true},
		{
			name:   "repository not found",
			status: http.StatusNotFound,
			code:   "RepositoryNotFound",
			want:   true,
		},
		{
			name:   "entry not found",
			status: http.StatusNotFound,
			code:   "EntryNotFound",
		},
		{name: "plain file not found", status: http.StatusNotFound},
		{
			name:       "signed artifact spoofed repository error",
			status:     http.StatusForbidden,
			code:       "RepositoryNotFound",
			provenance: responseFromSignedArtifact,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := make(http.Header)
			header.Set("X-Error-Code", test.code)
			if got := repositoryRevocationStatus(
				test.status,
				header,
				test.provenance,
			); got != test.want {
				t.Fatalf(
					"repositoryRevocationStatus(%d, %q) = %v, want %v",
					test.status,
					test.code,
					got,
					test.want,
				)
			}
		})
	}
}

func TestHandlerRepositoryRevocationInvalidatesSiblingCommitEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		bodyA  = "public-artifact-a"
		bodyB  = "public-artifact-b"
	)
	var repositoryPrivate atomic.Bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if repositoryPrivate.Load() {
			http.Error(w, "repository became private", http.StatusForbidden)
			return
		}

		body := bodyA
		if strings.HasSuffix(r.URL.Path, "/b.bin") {
			body = bodyB
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Repo-Commit", commit)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	for name, body := range map[string]string{"a.bin": bodyA, "b.bin": bodyB} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodGet,
				"/huggingface/Acme/Model/resolve/main/"+name,
				nil,
			),
		)
		if recorder.Code != http.StatusOK || recorder.Body.String() != body {
			t.Fatalf("warm %s response = (%d, %q)", name, recorder.Code, recorder.Body.String())
		}
		waitForCacheEntry(
			t,
			database,
			"huggingface/acme/model/resolve/"+commit+"/"+name,
		)
	}
	metadata := httptest.NewRecorder()
	router.ServeHTTP(
		metadata,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/api/models/ACME/Model?blobs=true",
			nil,
		),
	)
	if metadata.Code != http.StatusOK {
		t.Fatalf("warm query metadata response = (%d, %q)", metadata.Code, metadata.Body.String())
	}
	metadataKey, err := CacheKeyForRawQuery(
		ParseRequestPath("/api/models/acme/model"),
		"blobs=true",
	)
	if err != nil {
		t.Fatal(err)
	}
	waitForCacheEntry(t, database, metadataKey)
	var metadataPackage string
	if err := database.Model(&db.CacheEntry{}).
		Where("key = ?", metadataKey).
		Pluck("package_name", &metadataPackage).Error; err != nil {
		t.Fatal(err)
	}
	if metadataPackage != "acme/model" {
		t.Fatalf("query metadata package = %q, want acme/model", metadataPackage)
	}
	if err := database.Model(&db.HuggingFaceRefPin{}).
		Where("key = ?", "huggingface/acme/model/ref/main").
		Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}

	repositoryPrivate.Store(true)
	revoked := httptest.NewRecorder()
	router.ServeHTTP(
		revoked,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/main/a.bin",
			nil,
		),
	)
	if revoked.Code != http.StatusForbidden {
		t.Fatalf("revoked response = (%d, %q)", revoked.Code, revoked.Body.String())
	}

	sibling := httptest.NewRecorder()
	router.ServeHTTP(
		sibling,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/ACME/MODEL/resolve/"+commit+"/b.bin",
			nil,
		),
	)
	if sibling.Code != http.StatusForbidden || sibling.Body.String() == bodyB {
		t.Fatalf(
			"revoked sibling response = (%d, %q), want authoritative private response",
			sibling.Code,
			sibling.Body.String(),
		)
	}

	var cached, pins int64
	if err := database.Model(&db.CacheEntry{}).
		Where("adapter_type = ? AND package_name = ?", "huggingface", "acme/model").
		Count(&cached).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&db.HuggingFaceRefPin{}).
		Where("key LIKE ?", "huggingface/acme/model/ref/%").
		Count(&pins).Error; err != nil {
		t.Fatal(err)
	}
	if cached != 0 || pins != 0 {
		t.Fatalf("repository cleanup = cached:%d pins:%d, want 0/0", cached, pins)
	}
}

func TestHandlerEntryNotFoundInvalidatesOnlyExactCommitEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		bodyA  = "public-artifact-a"
		bodyB  = "public-artifact-b"
	)
	var artifactAMissing atomic.Bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isArtifactA := strings.HasSuffix(r.URL.Path, "/a.bin")
		if artifactAMissing.Load() && isArtifactA {
			w.Header().Set("X-Error-Code", "EntryNotFound")
			http.Error(w, "artifact does not exist", http.StatusNotFound)
			return
		}

		body := bodyA
		if !isArtifactA {
			body = bodyB
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Repo-Commit", commit)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	for name, body := range map[string]string{"a.bin": bodyA, "b.bin": bodyB} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodGet,
				"/huggingface/acme/model/resolve/main/"+name,
				nil,
			),
		)
		if recorder.Code != http.StatusOK || recorder.Body.String() != body {
			t.Fatalf("warm %s response = (%d, %q)", name, recorder.Code, recorder.Body.String())
		}
		waitForCacheEntry(
			t,
			database,
			"huggingface/acme/model/resolve/"+commit+"/"+name,
		)
	}
	if err := database.Model(&db.HuggingFaceRefPin{}).
		Where("key = ?", "huggingface/acme/model/ref/main").
		Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}

	artifactAMissing.Store(true)
	missing := httptest.NewRecorder()
	router.ServeHTTP(
		missing,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/main/a.bin",
			nil,
		),
	)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing response = (%d, %q)", missing.Code, missing.Body.String())
	}

	sibling := httptest.NewRecorder()
	router.ServeHTTP(
		sibling,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/"+commit+"/b.bin",
			nil,
		),
	)
	if sibling.Code != http.StatusOK || sibling.Body.String() != bodyB {
		t.Fatalf(
			"unrelated sibling response = (%d, %q), want cached body",
			sibling.Code,
			sibling.Body.String(),
		)
	}

	var cachedA, cachedB int64
	if err := database.Model(&db.CacheEntry{}).
		Where("key = ?", "huggingface/acme/model/resolve/"+commit+"/a.bin").
		Count(&cachedA).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&db.CacheEntry{}).
		Where("key = ?", "huggingface/acme/model/resolve/"+commit+"/b.bin").
		Count(&cachedB).Error; err != nil {
		t.Fatal(err)
	}
	if cachedA != 0 || cachedB != 1 {
		t.Fatalf("exact cleanup = cached A:%d B:%d, want 0/1", cachedA, cachedB)
	}
}

func TestHandlerRevocationWaitsForAdmittedCacheRequestAndFencesItsFill(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "cccccccccccccccccccccccccccccccccccccccc"
		bodyA  = "public-artifact-a"
		bodyC  = "inflight-public-artifact-c"
	)
	var repositoryPrivate atomic.Bool
	artifactCStarted := make(chan struct{}, 1)
	releaseArtifactC := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/c.bin") && !repositoryPrivate.Load() {
			artifactCStarted <- struct{}{}
			<-releaseArtifactC
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", strconv.Itoa(len(bodyC)))
			w.Header().Set("X-Repo-Commit", commit)
			if r.Method != http.MethodHead {
				_, _ = io.WriteString(w, bodyC)
			}
			return
		}
		if repositoryPrivate.Load() {
			http.Error(w, "repository became private", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(bodyA)))
		w.Header().Set("X-Repo-Commit", commit)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, bodyA)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	warm := httptest.NewRecorder()
	router.ServeHTTP(
		warm,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/main/a.bin",
			nil,
		),
	)
	if warm.Code != http.StatusOK || warm.Body.String() != bodyA {
		t.Fatalf("warm response = (%d, %q)", warm.Code, warm.Body.String())
	}
	waitForCacheEntry(
		t,
		database,
		"huggingface/acme/model/resolve/"+commit+"/a.bin",
	)
	if err := database.Model(&db.HuggingFaceRefPin{}).
		Where("key = ?", "huggingface/acme/model/ref/main").
		Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}

	inflightDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodGet,
				"/huggingface/acme/model/resolve/"+commit+"/c.bin",
				nil,
			),
		)
		inflightDone <- recorder
	}()
	select {
	case <-artifactCStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for admitted cache request")
	}

	repositoryPrivate.Store(true)
	revocationDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodGet,
				"/huggingface/acme/model/resolve/main/a.bin",
				nil,
			),
		)
		revocationDone <- recorder
	}()

	repository, ok := repositoryForParsed(ParseRequestPath(
		"/acme/model/resolve/main/a.bin",
	))
	if !ok {
		t.Fatal("failed to derive test repository identity")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !handler.repositoryRevoked(repository) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !handler.repositoryRevoked(repository) {
		t.Fatal("repository revocation gate was not closed")
	}
	select {
	case response := <-revocationDone:
		t.Fatalf(
			"revocation completed before admitted cache request drained: (%d, %q)",
			response.Code,
			response.Body.String(),
		)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseArtifactC)
	var inflight *httptest.ResponseRecorder
	select {
	case inflight = <-inflightDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for admitted cache response")
	}
	if inflight.Code != http.StatusOK || inflight.Body.String() != bodyC {
		t.Fatalf("admitted response = (%d, %q)", inflight.Code, inflight.Body.String())
	}

	var revoked *httptest.ResponseRecorder
	select {
	case revoked = <-revocationDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for repository revocation")
	}
	if revoked.Code != http.StatusForbidden {
		t.Fatalf("revoked response = (%d, %q)", revoked.Code, revoked.Body.String())
	}

	replayed := httptest.NewRecorder()
	router.ServeHTTP(
		replayed,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/"+commit+"/c.bin",
			nil,
		),
	)
	if replayed.Code != http.StatusForbidden || replayed.Body.String() == bodyC {
		t.Fatalf("fenced inflight fill replayed = (%d, %q)", replayed.Code, replayed.Body.String())
	}

	var cached int64
	if err := database.Model(&db.CacheEntry{}).
		Where("adapter_type = ? AND package_name = ?", "huggingface", "acme/model").
		Count(&cached).Error; err != nil {
		t.Fatal(err)
	}
	if cached != 0 {
		t.Fatalf("repository cache rows after inflight fencing = %d, want 0", cached)
	}
}

func TestHandlerAnonymousSuccessRetriesUnsafeCleanupBeforeRestore(t *testing.T) {
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.HuggingFaceRefPin{
		Key:       "huggingface/acme/model/ref/main",
		Commit:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`
		CREATE TRIGGER fail_repository_retry_pin_delete
		BEFORE DELETE ON hugging_face_ref_pins
		BEGIN
			SELECT RAISE(FAIL, 'pin database unavailable');
		END;
	`).Error; err != nil {
		t.Fatal(err)
	}

	handler := &Handler{
		db:          database,
		revocations: newRepositoryRevocationGate(),
	}
	repository, ok := repositoryForParsed(ParseRequestPath(
		"/acme/model/resolve/main/model.bin",
	))
	if !ok {
		t.Fatal("failed to derive test repository identity")
	}
	generation, _ := handler.revocations.begin(repository.packageName)
	handler.revocations.finish(repository.packageName, generation, "", false)

	request := httptest.NewRequest(
		http.MethodGet,
		"/huggingface/acme/model/resolve/main/model.bin",
		nil,
	)
	success := &resolvedResponse{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
	}
	handler.observeDirectRepositoryResponse(
		request,
		"/acme/model/resolve/main/model.bin",
		success,
		handler.directRepositoryRevocationTicket(
			request,
			"/acme/model/resolve/main/model.bin",
		),
	)
	if !handler.repositoryRevoked(repository) {
		t.Fatal("anonymous 2xx restored repository after cleanup retry failed")
	}

	if err := database.Exec("DROP TRIGGER fail_repository_retry_pin_delete").Error; err != nil {
		t.Fatal(err)
	}
	handler.observeDirectRepositoryResponse(
		request,
		"/acme/model/resolve/main/model.bin",
		success,
		handler.directRepositoryRevocationTicket(
			request,
			"/acme/model/resolve/main/model.bin",
		),
	)
	if handler.repositoryRevoked(repository) {
		t.Fatal("anonymous 2xx did not restore repository after successful cleanup retry")
	}
}

func TestHandlerBackgroundRefreshRevokesWholeRepository(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "dddddddddddddddddddddddddddddddddddddddd"
		bodyA  = "public-artifact-a"
		bodyB  = "public-artifact-b"
	)
	var repositoryPrivate atomic.Bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if repositoryPrivate.Load() {
			http.Error(w, "repository became private", http.StatusForbidden)
			return
		}
		body := bodyA
		if strings.HasSuffix(r.URL.Path, "/b.bin") {
			body = bodyB
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Repo-Commit", commit)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	for name, body := range map[string]string{"a.bin": bodyA, "b.bin": bodyB} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodGet,
				"/huggingface/acme/model/resolve/main/"+name,
				nil,
			),
		)
		if recorder.Code != http.StatusOK || recorder.Body.String() != body {
			t.Fatalf("warm %s response = (%d, %q)", name, recorder.Code, recorder.Body.String())
		}
		waitForCacheEntry(
			t,
			database,
			"huggingface/acme/model/resolve/"+commit+"/"+name,
		)
	}
	if err := database.Model(&db.CacheEntry{}).
		Where("key = ?", "huggingface/acme/model/resolve/"+commit+"/a.bin").
		Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}

	repositoryPrivate.Store(true)
	stale := httptest.NewRecorder()
	router.ServeHTTP(
		stale,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/"+commit+"/a.bin",
			nil,
		),
	)
	if stale.Code != http.StatusOK || stale.Body.String() != bodyA {
		t.Fatalf("stale response = (%d, %q)", stale.Code, stale.Body.String())
	}

	repository, ok := repositoryForParsed(ParseRequestPath(
		"/acme/model/resolve/" + commit + "/a.bin",
	))
	if !ok {
		t.Fatal("failed to derive test repository identity")
	}
	deadline := time.Now().Add(3 * time.Second)
	for !handler.repositoryRevoked(repository) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !handler.repositoryRevoked(repository) {
		t.Fatal("background 403 did not close repository revocation gate")
	}

	sibling := httptest.NewRecorder()
	router.ServeHTTP(
		sibling,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/"+commit+"/b.bin",
			nil,
		),
	)
	if sibling.Code != http.StatusForbidden || sibling.Body.String() == bodyB {
		t.Fatalf(
			"background-revoked sibling response = (%d, %q)",
			sibling.Code,
			sibling.Body.String(),
		)
	}
}

func TestHandlerDirectEntryNotFoundInvalidatesExactCommitEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		bodyA  = "public-artifact-a"
		bodyB  = "public-artifact-b"
	)
	var mode atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isArtifactA := strings.HasSuffix(r.URL.Path, "/a.bin")
		switch mode.Load() {
		case 1:
			if isArtifactA {
				w.Header().Set("X-Error-Code", "EntryNotFound")
				http.Error(w, "artifact does not exist", http.StatusNotFound)
				return
			}
		case 2:
			http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
			return
		}
		body := bodyA
		if !isArtifactA {
			body = bodyB
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Repo-Commit", commit)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	for name, body := range map[string]string{"a.bin": bodyA, "b.bin": bodyB} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodGet,
				"/huggingface/acme/model/resolve/main/"+name,
				nil,
			),
		)
		if recorder.Code != http.StatusOK || recorder.Body.String() != body {
			t.Fatalf("warm %s response = (%d, %q)", name, recorder.Code, recorder.Body.String())
		}
		waitForCacheEntry(
			t,
			database,
			"huggingface/acme/model/resolve/"+commit+"/"+name,
		)
	}

	mode.Store(1)
	rangeRequest := httptest.NewRequest(
		http.MethodGet,
		"/huggingface/acme/model/resolve/main/a.bin",
		nil,
	)
	rangeRequest.Header.Set("Range", "bytes=0-3")
	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, rangeRequest)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("direct missing response = (%d, %q)", missing.Code, missing.Body.String())
	}

	mode.Store(2)
	removed := httptest.NewRecorder()
	router.ServeHTTP(
		removed,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/"+commit+"/a.bin",
			nil,
		),
	)
	if removed.Code == http.StatusOK || removed.Body.String() == bodyA {
		t.Fatalf("directly invalidated entry revived = (%d, %q)", removed.Code, removed.Body.String())
	}
	sibling := httptest.NewRecorder()
	router.ServeHTTP(
		sibling,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/"+commit+"/b.bin",
			nil,
		),
	)
	if sibling.Code != http.StatusOK || sibling.Body.String() != bodyB {
		t.Fatalf("unrelated sibling response = (%d, %q)", sibling.Code, sibling.Body.String())
	}
}

func TestHandlerRepositoryNotFoundCodeDisablesTransientStalePinFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "ffffffffffffffffffffffffffffffffffffffff"
		bodyA  = "public-artifact-a"
		bodyB  = "public-artifact-b"
	)
	var repositoryMissing atomic.Bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if repositoryMissing.Load() {
			w.Header().Set("X-Error-Code", "RepositoryNotFound")
			http.Error(w, "repository does not exist", http.StatusServiceUnavailable)
			return
		}
		body := bodyA
		if strings.HasSuffix(r.URL.Path, "/b.bin") {
			body = bodyB
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Repo-Commit", commit)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	for name, body := range map[string]string{"a.bin": bodyA, "b.bin": bodyB} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodGet,
				"/huggingface/acme/model/resolve/main/"+name,
				nil,
			),
		)
		if recorder.Code != http.StatusOK || recorder.Body.String() != body {
			t.Fatalf("warm %s response = (%d, %q)", name, recorder.Code, recorder.Body.String())
		}
		waitForCacheEntry(
			t,
			database,
			"huggingface/acme/model/resolve/"+commit+"/"+name,
		)
	}
	if err := database.Model(&db.HuggingFaceRefPin{}).
		Where("key = ?", "huggingface/acme/model/ref/main").
		Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}

	repositoryMissing.Store(true)
	missing := httptest.NewRecorder()
	router.ServeHTTP(
		missing,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/main/a.bin",
			nil,
		),
	)
	if missing.Code == http.StatusOK || missing.Body.String() == bodyA {
		t.Fatalf("repository error used stale pin = (%d, %q)", missing.Code, missing.Body.String())
	}

	sibling := httptest.NewRecorder()
	router.ServeHTTP(
		sibling,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/"+commit+"/b.bin",
			nil,
		),
	)
	if sibling.Code == http.StatusOK || sibling.Body.String() == bodyB {
		t.Fatalf("repository-error sibling revived = (%d, %q)", sibling.Code, sibling.Body.String())
	}
}

func TestHandlerSignedCDNForbiddenDoesNotRevokeSibling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "abababababababababababababababababababab"
		bodyA  = "public-artifact-a"
		bodyB  = "public-artifact-b"
	)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Error-Code", "RepositoryNotFound")
		http.Error(w, "signed URL expired", http.StatusForbidden)
	}))
	t.Cleanup(cdn.Close)

	var redirectToCDN atomic.Bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redirectToCDN.Load() {
			w.Header().Set("Location", cdn.URL+"/artifact")
			w.Header().Set("X-Repo-Commit", commit)
			w.WriteHeader(http.StatusFound)
			return
		}
		body := bodyA
		if strings.HasSuffix(r.URL.Path, "/b.bin") {
			body = bodyB
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Repo-Commit", commit)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	siblingPath := "/huggingface/acme/model/resolve/" + commit + "/b.bin"
	warmSibling := httptest.NewRecorder()
	router.ServeHTTP(
		warmSibling,
		httptest.NewRequest(http.MethodGet, siblingPath, nil),
	)
	if warmSibling.Code != http.StatusOK || warmSibling.Body.String() != bodyB {
		t.Fatalf("warm sibling response = (%d, %q)", warmSibling.Code, warmSibling.Body.String())
	}
	waitForCacheEntry(
		t,
		database,
		"huggingface/acme/model/resolve/"+commit+"/b.bin",
	)

	redirectToCDN.Store(true)
	cdnFailure := httptest.NewRecorder()
	router.ServeHTTP(
		cdnFailure,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/"+commit+"/a.bin",
			nil,
		),
	)
	if cdnFailure.Code != http.StatusForbidden {
		t.Fatalf("CDN failure response = (%d, %q)", cdnFailure.Code, cdnFailure.Body.String())
	}

	sibling := httptest.NewRecorder()
	router.ServeHTTP(
		sibling,
		httptest.NewRequest(http.MethodGet, siblingPath, nil),
	)
	if sibling.Code != http.StatusOK || sibling.Body.String() != bodyB {
		t.Fatalf("CDN error revoked sibling = (%d, %q)", sibling.Code, sibling.Body.String())
	}
}
