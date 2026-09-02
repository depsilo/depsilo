package npm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/rules"
	"depsilo/internal/upstream"
)

var testNPMTarballSigningKey = bytes.Repeat([]byte{0x7a}, tarballSigningKeySize)

type npmQuarantineCall struct {
	ecosystem string
	packageID string
	version   string
}

type npmRecordingQuarantineChecker struct {
	mu    sync.Mutex
	calls []npmQuarantineCall
}

type npmRecordingPackageRuleChecker struct {
	mu       sync.Mutex
	calls    []npmQuarantineCall
	decision adapter.PackageRuleDecision
}

type failingRandomReader struct{}

func (failingRandomReader) Read([]byte) (int, error) {
	return 0, errors.New("test random source unavailable")
}

func (checker *npmRecordingQuarantineChecker) Check(
	_ context.Context,
	ecosystem, packageID, version, _ string,
) adapter.QuarantineDecision {
	checker.mu.Lock()
	defer checker.mu.Unlock()
	checker.calls = append(checker.calls, npmQuarantineCall{
		ecosystem: ecosystem,
		packageID: packageID,
		version:   version,
	})
	return adapter.QuarantineDecision{Allowed: true}
}

func (checker *npmRecordingQuarantineChecker) snapshot() []npmQuarantineCall {
	checker.mu.Lock()
	defer checker.mu.Unlock()
	return append([]npmQuarantineCall(nil), checker.calls...)
}

func (checker *npmRecordingPackageRuleChecker) EvaluatePackageRule(
	_ context.Context,
	ecosystem, packageID, version string,
) adapter.PackageRuleDecision {
	checker.mu.Lock()
	defer checker.mu.Unlock()
	checker.calls = append(checker.calls, npmQuarantineCall{
		ecosystem: ecosystem,
		packageID: packageID,
		version:   version,
	})
	return checker.decision
}

func (checker *npmRecordingPackageRuleChecker) snapshot() []npmQuarantineCall {
	checker.mu.Lock()
	defer checker.mu.Unlock()
	return append([]npmQuarantineCall(nil), checker.calls...)
}

func newNPMProvenanceFixture(
	t *testing.T,
	signer *tarballSigner,
) (*gin.Engine, *atomic.Int64, *atomic.Int64, func(*tarballSigner) *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	metadataRequests := &atomic.Int64{}
	tarballRequests := &atomic.Int64{}
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/fixture":
			metadataRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"fixture","versions":{`+
				`"1.0.0-alpha":{"dist":{"tarball":"`+upstreamServerURL(request)+`/fixture/-/pkg-alpha.tgz"}},`+
				`"1.0.0":{"dist":{"tarball":"`+upstreamServerURL(request)+`/fixture/-/pkg-custom.tgz"}},`+
				`"1.1.0":{"dist":{"tarball":"`+upstreamServerURL(request)+`/fixture/-/pkg-next.tgz"}}}}`)
		case "/fixture/-/pkg-alpha.tgz", "/fixture/-/pkg-custom.tgz", "/fixture/-/pkg-next.tgz":
			tarballRequests.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "tarball")
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(upstreamServer.Close)

	newRouter := newNPMProvenanceRouterFactory(t, []config.UpstreamConfig{{
		Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})

	return newRouter(signer), metadataRequests, tarballRequests, newRouter
}

func newNPMRulesEngine(t *testing.T, models ...db.PackageRule) (*rules.Engine, *gorm.DB) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "npm-rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	store := rules.NewStore(database)
	for index := range models {
		if err := store.Create(&models[index]); err != nil {
			t.Fatalf("create npm package rule %d: %v", index, err)
		}
	}
	return rules.NewEngine(store, nil), database
}

func newNPMProvenanceRouterFactory(
	t *testing.T,
	upstreamConfigs []config.UpstreamConfig,
) func(*tarballSigner) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "npm-provenance.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), 72*time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
	pool, err := upstream.NewPool(upstreamConfigs)
	if err != nil {
		t.Fatal(err)
	}
	selector := upstream.NewPrioritySelector(pool)
	newRouter := func(activeSigner *tarballSigner) *gin.Engine {
		handler := newHandlerWithSigner(
			manager,
			selector,
			config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour},
			database,
			activeSigner,
		)
		router := gin.New()
		handler.Register(router.Group("/npm"))
		handler.Register(router.Group("/p/:slug/npm"))
		return router
	}

	return newRouter
}

func upstreamServerURL(request *http.Request) string {
	return "http://" + request.Host
}

func deterministicTarballSigner(t *testing.T, keyByte byte) *tarballSigner {
	t.Helper()
	signer, err := newTarballSigner(bytes.Repeat([]byte{keyByte}, tarballSigningKeySize))
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func requestNPMMetadataTarballURL(t *testing.T, router http.Handler) string {
	return requestNPMMetadataTarballURLAt(t, router, "/npm/fixture", "1.0.0")
}

func requestNPMMetadataTarballURLAt(
	t *testing.T,
	router http.Handler,
	metadataPath, version string,
) string {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, metadataPath, nil)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("metadata status = %d, body=%s", response.Code, response.Body.String())
	}
	var document struct {
		Versions map[string]struct {
			Dist struct {
				Tarball string `json:"tarball"`
			} `json:"dist"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	return document.Versions[version].Dist.Tarball
}

func requestURLPath(t *testing.T, router http.Handler, rawURL string) *httptest.ResponseRecorder {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, parsed.RequestURI(), nil)
	router.ServeHTTP(response, request)
	return response
}

func TestSignedTarballUsesPackumentVersionAndRejectsTampering(t *testing.T) {
	signer := deterministicTarballSigner(t, 0x11)
	router, _, tarballRequests, _ := newNPMProvenanceFixture(t, signer)
	checker := &npmRecordingQuarantineChecker{}
	ruleChecker := &npmRecordingPackageRuleChecker{
		decision: adapter.PackageRuleDecision{Outcome: adapter.PackageRuleAllow},
	}
	scoped := adapter.NewRequestScope(nil, nil, checker, ruleChecker).Wrap(router)

	signedURL := requestNPMMetadataTarballURL(t, router)
	if !strings.Contains(signedURL, "/-/"+SignedTarballRouteSegment+"/") {
		t.Fatalf("metadata tarball URL is not signed: %q", signedURL)
	}
	response := requestURLPath(t, scoped, signedURL)
	if response.Code != http.StatusOK || response.Body.String() != "tarball" {
		t.Fatalf("signed tarball status = %d, body=%q", response.Code, response.Body.String())
	}
	if got := checker.snapshot(); len(got) != 1 || got[0] != (npmQuarantineCall{
		ecosystem: "npm",
		packageID: "fixture",
		version:   "1.0.0",
	}) {
		t.Fatalf("checker calls = %#v", got)
	}

	parsed, err := url.Parse(signedURL)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(parsed.Path, "/")
	markerAt := -1
	for index, part := range parts {
		if part == SignedTarballRouteSegment {
			markerAt = index
			break
		}
	}
	if markerAt < 0 || markerAt+2 >= len(parts) {
		t.Fatalf("unexpected signed URL path %q", parsed.Path)
	}
	token := parts[markerAt+1]

	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{
		[]byte(`"version":"1.0.0"`),
		[]byte("pkg-custom.tgz"),
		[]byte("/fixture/-/pkg-custom.tgz"),
	} {
		if bytes.Contains(decoded, secret) {
			t.Fatalf("signed route disclosed claim %q in its token", secret)
		}
	}
	claims, valid := signer.verify(token, "global", "fixture", "pkg-custom.tgz")
	if !valid || claims.Version != "1.0.0" {
		t.Fatalf("legitimate encrypted claims = %#v, valid=%v", claims, valid)
	}
	if alternate := nonCanonicalRawBase64URL(token); alternate != "" {
		if _, valid := signer.verify(alternate, "global", "fixture", "pkg-custom.tgz"); valid {
			t.Fatal("non-canonical base64url token was accepted")
		}
	} else {
		t.Fatal("test token unexpectedly had no base64url trailing pad bits")
	}
	claims.Version = "9.0.0"
	forgedVersionToken, err := deterministicTarballSigner(t, 0x12).sign(claims)
	if err != nil {
		t.Fatal(err)
	}

	mutatedEnvelope := append([]byte(nil), decoded...)
	mutatedEnvelope[0] ^= 0x01
	mutatedEnvelopeToken := base64.RawURLEncoding.EncodeToString(mutatedEnvelope)

	mutatedToken := []byte(token)
	if mutatedToken[0] == 'A' {
		mutatedToken[0] = 'B'
	} else {
		mutatedToken[0] = 'A'
	}

	tamperedPaths := []string{
		strings.Replace(parsed.Path, "/"+token+"/", "/"+forgedVersionToken+"/", 1),
		strings.Replace(parsed.Path, "/"+token+"/", "/"+mutatedEnvelopeToken+"/", 1),
		strings.Replace(parsed.Path, "/"+token+"/", "/"+string(mutatedToken)+"/", 1),
		strings.Replace(parsed.Path, "/npm/fixture/", "/npm/other/", 1),
		strings.TrimSuffix(parsed.Path, "/pkg-custom.tgz") + "/other.tgz",
	}
	for _, path := range tamperedPaths {
		response := requestURLPath(t, scoped, path)
		if response.Code != http.StatusNotFound {
			t.Errorf("tampered path %q status = %d, body=%s", path, response.Code, response.Body.String())
		}
	}
	if got := checker.snapshot(); len(got) != 1 {
		t.Fatalf("tampered routes called checker: %#v", got)
	}
	if calls := ruleChecker.snapshot(); len(calls) != 1 {
		t.Fatalf("tampered routes called package rules: %#v", calls)
	}
	if got := tarballRequests.Load(); got != 1 {
		t.Fatalf("tampered routes contacted upstream: %d requests", got)
	}
}

func TestSignedTarballPackageRuleDenyPrecedesQuarantineAndNetwork(t *testing.T) {
	router, _, tarballRequests, _ := newNPMProvenanceFixture(t, deterministicTarballSigner(t, 0x13))
	signedURL := requestNPMMetadataTarballURL(t, router)
	ruleChecker := &npmRecordingPackageRuleChecker{
		decision: adapter.PackageRuleDecision{
			Outcome: adapter.PackageRuleDeny,
			Reason:  "blocked by exact npm policy",
		},
	}
	quarantineChecker := &npmRecordingQuarantineChecker{}
	scoped := adapter.NewRequestScope(nil, nil, quarantineChecker, ruleChecker).Wrap(router)

	response := requestURLPath(t, scoped, signedURL)
	if response.Code != http.StatusForbidden ||
		!strings.Contains(response.Body.String(), `"code":"PACKAGE_DENIED"`) {
		t.Fatalf("signed deny status=%d body=%s", response.Code, response.Body.String())
	}
	want := npmQuarantineCall{ecosystem: "npm", packageID: "fixture", version: "1.0.0"}
	if calls := ruleChecker.snapshot(); len(calls) != 1 || calls[0] != want {
		t.Fatalf("rule checker calls = %#v, want %#v", calls, want)
	}
	if calls := quarantineChecker.snapshot(); len(calls) != 0 {
		t.Fatalf("quarantine ran after package-rule deny: %#v", calls)
	}
	if got := tarballRequests.Load(); got != 0 {
		t.Fatalf("package-rule deny contacted upstream: requests=%d", got)
	}
}

func TestAuthenticatedNPMRulesUsePackumentVersionAndRuleSpecificity(t *testing.T) {
	router, metadataRequests, tarballRequests, _ := newNPMProvenanceFixture(
		t, deterministicTarballSigner(t, 0x14),
	)
	engine, _ := newNPMRulesEngine(t,
		db.PackageRule{
			Ecosystem: "npm", PackageName: "fixture", Version: "*",
			Action: "deny", Reason: "baseline package deny",
		},
		db.PackageRule{
			Ecosystem: "npm", PackageName: "fixture", Version: ">= 1.0.0",
			Action: "allow", Reason: "allow final releases",
		},
		db.PackageRule{
			Ecosystem: "npm", PackageName: "fixture", Version: "1.1.0",
			Action: "deny", Reason: "exact version deny wins",
		},
	)

	// Match the production order: global rules middleware first, then the npm
	// Adapter, with the authenticated checker carried by immutable RequestScope.
	policyRouter := gin.New()
	policyRouter.Use(rules.Middleware(engine))
	policyRouter.Any("/*path", gin.WrapH(router))
	scoped := adapter.NewRequestScope(nil, nil, nil, rules.Wrap(engine)).Wrap(policyRouter)

	metadata := requestURLPath(t, scoped, "/npm/fixture")
	if metadata.Code != http.StatusOK {
		t.Fatalf("metadata under package deny status=%d body=%s", metadata.Code, metadata.Body.String())
	}

	alphaURL := requestNPMMetadataTarballURLAt(t, scoped, "/npm/fixture", "1.0.0-alpha")
	finalURL := requestNPMMetadataTarballURLAt(t, scoped, "/npm/fixture", "1.0.0")
	nextURL := requestNPMMetadataTarballURLAt(t, scoped, "/npm/fixture", "1.1.0")

	if response := requestURLPath(t, scoped, alphaURL); response.Code != http.StatusForbidden ||
		!strings.Contains(response.Body.String(), "baseline package deny") {
		t.Fatalf("prerelease decision status=%d body=%s", response.Code, response.Body.String())
	}
	if response := requestURLPath(t, scoped, finalURL); response.Code != http.StatusOK {
		t.Fatalf("final release decision status=%d body=%s", response.Code, response.Body.String())
	}
	if response := requestURLPath(t, scoped, nextURL); response.Code != http.StatusForbidden ||
		!strings.Contains(response.Body.String(), "exact version deny wins") {
		t.Fatalf("exact-rule decision status=%d body=%s", response.Code, response.Body.String())
	}
	if got := metadataRequests.Load(); got != 1 {
		t.Fatalf("metadata requests=%d, want one cached fetch", got)
	}
	if got := tarballRequests.Load(); got != 1 {
		t.Fatalf("artifact requests=%d, want only allowed final release", got)
	}
}

func TestSignedTarballUnsafePackageRuleDecisionFailsClosed(t *testing.T) {
	router, _, tarballRequests, _ := newNPMProvenanceFixture(t, deterministicTarballSigner(t, 0x15))
	signedURL := requestNPMMetadataTarballURL(t, router)
	ruleChecker := &npmRecordingPackageRuleChecker{
		decision: adapter.PackageRuleDecision{Outcome: adapter.PackageRuleUnevaluable},
	}
	quarantineChecker := &npmRecordingQuarantineChecker{}
	scoped := adapter.NewRequestScope(nil, nil, quarantineChecker, ruleChecker).Wrap(router)

	response := requestURLPath(t, scoped, signedURL)
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), `"code":"PACKAGE_POLICY_UNEVALUABLE"`) {
		t.Fatalf("unsafe decision status=%d body=%s", response.Code, response.Body.String())
	}
	if calls := quarantineChecker.snapshot(); len(calls) != 0 {
		t.Fatalf("unsafe package policy reached quarantine: %#v", calls)
	}
	if got := tarballRequests.Load(); got != 0 {
		t.Fatalf("unsafe package policy contacted upstream: requests=%d", got)
	}
}

func TestSignedTarballTransientRuleStoreFailureFailsOpen(t *testing.T) {
	router, _, tarballRequests, _ := newNPMProvenanceFixture(t, deterministicTarballSigner(t, 0x16))
	signedURL := requestNPMMetadataTarballURL(t, router)
	engine, database := newNPMRulesEngine(t)
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDatabase.Close(); err != nil {
		t.Fatal(err)
	}
	scoped := adapter.NewRequestScope(nil, nil, nil, rules.Wrap(engine)).Wrap(router)

	response := requestURLPath(t, scoped, signedURL)
	if response.Code != http.StatusOK {
		t.Fatalf("transient rule-store failure status=%d body=%s", response.Code, response.Body.String())
	}
	if got := tarballRequests.Load(); got != 1 {
		t.Fatalf("fail-open request did not reach exact upstream: requests=%d", got)
	}
}

func nonCanonicalRawBase64URL(encoded string) string {
	if encoded == "" || len(encoded)%4 == 0 {
		return ""
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	index := strings.IndexByte(alphabet, encoded[len(encoded)-1])
	if index < 0 {
		return ""
	}
	unusedBits := 2
	if len(encoded)%4 == 2 {
		unusedBits = 4
	}
	alternateIndex := index ^ 1
	if index>>unusedBits != alternateIndex>>unusedBits {
		return ""
	}
	return encoded[:len(encoded)-1] + string(alphabet[alternateIndex])
}

func TestLegacyDirectTarballIsRejectedWithoutMakingVersionDecision(t *testing.T) {
	router, _, tarballRequests, _ := newNPMProvenanceFixture(t, deterministicTarballSigner(t, 0x22))
	checker := &npmRecordingQuarantineChecker{}
	ruleChecker := &npmRecordingPackageRuleChecker{
		decision: adapter.PackageRuleDecision{Outcome: adapter.PackageRuleAllow},
	}
	scoped := adapter.NewRequestScope(nil, nil, checker, ruleChecker).Wrap(router)

	for _, path := range []string{
		"/npm/fixture/-/pkg-custom.tgz",
		"/npm/@scope/fixture/-/pkg-custom.tgz",
	} {
		response := requestURLPath(t, scoped, path)
		if response.Code != http.StatusNotFound {
			t.Errorf("legacy tarball %q status = %d, want 404; body=%s", path, response.Code, response.Body.String())
		}
	}
	if calls := checker.snapshot(); len(calls) != 0 {
		t.Fatalf("legacy filename caused a version decision: %#v", calls)
	}
	if calls := ruleChecker.snapshot(); len(calls) != 0 {
		t.Fatalf("legacy filename reached package rules: %#v", calls)
	}
	if got := tarballRequests.Load(); got != 0 {
		t.Fatalf("legacy tarball contacted upstream: requests = %d", got)
	}
}

func TestCachedPackumentUsesStableDeploymentKeyAcrossRestart(t *testing.T) {
	firstSigner := deterministicTarballSigner(t, 0x33)
	firstRouter, metadataRequests, _, newRouter := newNPMProvenanceFixture(t, firstSigner)
	firstURL := requestNPMMetadataTarballURL(t, firstRouter)

	// A separately constructed signer models a new process using the same
	// persisted deployment secret.
	secondRouter := newRouter(deterministicTarballSigner(t, 0x33))
	secondURL := requestNPMMetadataTarballURL(t, secondRouter)
	if got := metadataRequests.Load(); got != 1 {
		t.Fatalf("second signer missed stable metadata cache: upstream requests = %d", got)
	}
	if response := requestURLPath(t, secondRouter, firstURL); response.Code != http.StatusOK {
		t.Fatalf("previous-process URL with same secret status = %d, body=%s", response.Code, response.Body.String())
	}
	if response := requestURLPath(t, secondRouter, secondURL); response.Code != http.StatusOK {
		t.Fatalf("new-process URL with same secret status = %d, body=%s", response.Code, response.Body.String())
	}

	rotatedRouter := newRouter(deterministicTarballSigner(t, 0x44))
	rotatedURL := requestNPMMetadataTarballURL(t, rotatedRouter)
	if response := requestURLPath(t, rotatedRouter, firstURL); response.Code != http.StatusNotFound {
		t.Fatalf("old URL after secret rotation status = %d, want 404", response.Code)
	}
	if response := requestURLPath(t, rotatedRouter, rotatedURL); response.Code != http.StatusOK {
		t.Fatalf("rotated-key URL status = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestTarballSignerFailsClosedWhenRandomSourceFails(t *testing.T) {
	signer, err := newTarballSignerWithRandom(
		bytes.Repeat([]byte{0x54}, tarballSigningKeySize),
		failingRandomReader{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = signer.sign(tarballClaims{
		Format: tarballClaimFormat, Audience: "global", Package: "fixture", Version: "1.0.0",
		Source: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x53}, sha256.Size)),
		Target: "https://registry.example/fixture/-/fixture.tgz", Filename: "fixture.tgz",
	})
	if err == nil || !strings.Contains(err.Error(), "random") {
		t.Fatalf("sign with failed random source error = %v", err)
	}
}

func TestHandlerCopiesInjectedSigningKey(t *testing.T) {
	key := bytes.Repeat([]byte{0x55}, tarballSigningKeySize)
	handler := New(nil, nil, config.CacheConfig{}, nil, key)
	claims := tarballClaims{
		Format:   tarballClaimFormat,
		Audience: "global",
		Package:  "fixture",
		Version:  "1.0.0",
		Source:   base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x66}, sha256.Size)),
		Target:   "https://registry.example/fixture/-/pkg-custom.tgz",
		Filename: "pkg-custom.tgz",
	}
	token, err := handler.signer.sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	key[0] ^= 0xff
	if verified, valid := handler.signer.verify(token, "global", "fixture", "pkg-custom.tgz"); !valid || verified.Version != "1.0.0" {
		t.Fatal("Handler retained caller-owned signing key memory")
	}
}

func TestHandlerRejectsInvalidSigningKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New accepted a short signing key")
		}
	}()
	New(nil, nil, config.CacheConfig{}, nil, []byte("short"))
}

func TestTarballTargetPreservesEncodedFilenameAndQuery(t *testing.T) {
	base, err := url.Parse("https://registry.example/fixture")
	if err != nil {
		t.Fatal(err)
	}
	target, filename, err := resolveTarballTarget(
		base,
		"https://cdn.example/fixture/-/pkg%3Fvariant%231.tgz?download=1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if target != "https://cdn.example/fixture/-/pkg%3Fvariant%231.tgz?download=1" ||
		filename != "pkg?variant#1.tgz" {
		t.Fatalf("target=%q filename=%q", target, filename)
	}
}

func TestTarballTargetRejectsEncodedFilenameDelimiter(t *testing.T) {
	base, err := url.Parse("https://registry.example/fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveTarballTarget(base, "/fixture/-/pkg%2F-%2Fevil.tgz"); err == nil {
		t.Fatal("encoded filename path delimiter was accepted")
	}
}

func TestPreparePackumentBindsExactTargetSourceAndDeclaredDigests(t *testing.T) {
	sourceID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x77}, sha256.Size))
	prepared, err := PreparePackument(
		[]byte(`{"name":"fixture","versions":{"1.0.0":{"dist":{"tarball":"../fixture/-/pkg%3Fvariant%231.tgz?download=1&path=%2F","integrity":"sha512-declared","shasum":"0123456789abcdef"}}}}`),
		"fixture",
		"https://registry.example/npm/fixture",
		sourceID,
	)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Versions map[string]struct {
			Dist struct {
				Tarball string `json:"tarball"`
			} `json:"dist"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(prepared, &document); err != nil {
		t.Fatal(err)
	}
	reference, err := decodePreparedTarballReference(document.Versions["1.0.0"].Dist.Tarball)
	if err != nil {
		t.Fatal(err)
	}
	if reference.Source != sourceID ||
		reference.Target != "https://registry.example/fixture/-/pkg%3Fvariant%231.tgz?download=1&path=%2F" ||
		reference.Filename != "pkg?variant#1.tgz" ||
		reference.Integrity != "sha512-declared" || reference.Shasum != "0123456789abcdef" {
		t.Fatalf("prepared reference = %#v", reference)
	}

	signer := deterministicTarballSigner(t, 0x78)
	runtime, err := signRuntimeTarballURLs(
		prepared,
		"https://depsilo.example",
		"/p/acme/npm",
		"project:acme",
		"fixture",
		signer,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(runtime, &document); err != nil {
		t.Fatal(err)
	}
	signed, err := url.Parse(document.Versions["1.0.0"].Dist.Tarball)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(signed.EscapedPath(), "/")
	if len(parts) < 3 {
		t.Fatalf("signed path = %q", signed.EscapedPath())
	}
	claims, valid := signer.verify(
		parts[len(parts)-2],
		"project:acme",
		"fixture",
		"pkg?variant#1.tgz",
	)
	if !valid || claims.Version != "1.0.0" || claims.Source != sourceID ||
		claims.Target != reference.Target || claims.Integrity != reference.Integrity ||
		claims.Shasum != reference.Shasum {
		t.Fatalf("verified claims = %#v, valid=%v", claims, valid)
	}
}

func TestPreparePackumentRejectsClaimsThatCannotFitSignedRoute(t *testing.T) {
	sourceID := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x75}, sha256.Size))
	query := strings.Repeat("&", 1800)
	metadata := []byte(`{"name":"fixture","versions":{"1.0.0":{"dist":{"tarball":"https://registry.example/fixture/-/fixture.tgz?x=1` + query + `"}}}}`)
	if _, err := PreparePackument(metadata, "fixture", "https://registry.example/fixture", sourceID); err == nil ||
		!strings.Contains(err.Error(), "signed route limit") {
		t.Fatalf("oversized signed claims error = %v", err)
	}
}

func TestSignedTarballPathBudgetIncludesPrefixPackageAndFilename(t *testing.T) {
	claims := tarballClaims{Package: "@scope/fixture", Filename: strings.Repeat("x", 512)}
	prefix := "/p/" + strings.Repeat("a", maxProjectAudienceSlugLength) + "/npm"
	if err := validateSignedTarballPathSize(prefix, claims, maxSignedTarballPathLength); err == nil {
		t.Fatal("token-only limit accepted an oversized complete signed route")
	}
}

func TestInvalidPackumentVersionFailsBeforeCachePersistence(t *testing.T) {
	requests := &atomic.Int64{}
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"fixture","versions":{"1.0":{"dist":{"tarball":"/fixture/-/fixture-1.0.tgz"}}}}`)
	}))
	t.Cleanup(upstreamServer.Close)
	newRouter := newNPMProvenanceRouterFactory(t, []config.UpstreamConfig{{
		Name: "invalid", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	router := newRouter(deterministicTarballSigner(t, 0x79))

	for attempt := 0; attempt < 2; attempt++ {
		response := requestURLPath(t, router, "/npm/fixture")
		if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "UPSTREAM_INVALID_METADATA") {
			t.Fatalf("attempt %d status=%d body=%s", attempt+1, response.Code, response.Body.String())
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("invalid packument was cached: upstream requests=%d, want 2", got)
	}
}

func TestSignedTarballDoesNotFailOverToAnotherUpstream(t *testing.T) {
	var sourceA *httptest.Server
	sourceA = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/fixture":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"fixture","versions":{"1.0.0":{"dist":{"tarball":"`+sourceA.URL+`/fixture/-/same.tgz"}}}}`)
		case "/fixture/-/same.tgz":
			_, _ = io.WriteString(w, "source-a")
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(sourceA.Close)
	sourceBRequests := &atomic.Int64{}
	sourceB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		sourceBRequests.Add(1)
		_, _ = io.WriteString(w, "source-b")
	}))
	t.Cleanup(sourceB.Close)

	newRouter := newNPMProvenanceRouterFactory(t, []config.UpstreamConfig{
		{Name: "source-a", URL: sourceA.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "source-b", URL: sourceB.URL, Priority: 2, ProbeMode: "passive"},
	})
	router := newRouter(deterministicTarballSigner(t, 0x7a))
	signedURL := requestNPMMetadataTarballURL(t, router)
	sourceA.Close()

	response := requestURLPath(t, router, signedURL)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("artifact with unavailable provenance source status=%d body=%s", response.Code, response.Body.String())
	}
	if got := sourceBRequests.Load(); got != 0 {
		t.Fatalf("artifact request failed over to source B: requests=%d", got)
	}
}

func TestAuthenticatedTarballCacheKeySeparatesSourcesTargetsAndDigests(t *testing.T) {
	sourceA := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x81}, sha256.Size))
	sourceB := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x82}, sha256.Size))
	base := tarballClaims{
		Package: "fixture", Filename: "same.tgz", Source: sourceA,
		Target:    "https://a.example/identical/path/same.tgz?download=1",
		Integrity: "sha512-a", Shasum: "sha1-a",
	}
	keys := []string{authenticatedTarballCacheKey(base)}
	otherSource := base
	otherSource.Source = sourceB
	otherSource.Target = "https://b.example/identical/path/same.tgz?download=1"
	keys = append(keys, authenticatedTarballCacheKey(otherSource))
	otherQuery := base
	otherQuery.Target = "https://a.example/identical/path/same.tgz?download=2"
	keys = append(keys, authenticatedTarballCacheKey(otherQuery))
	otherDigest := base
	otherDigest.Integrity = "sha512-b"
	keys = append(keys, authenticatedTarballCacheKey(otherDigest))

	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if _, duplicate := seen[key]; duplicate {
			t.Fatalf("provenance-distinct artifacts collided at cache key %q", key)
		}
		seen[key] = struct{}{}
	}
}

func TestSignedTarballAudiencePreventsProjectAndGlobalReplay(t *testing.T) {
	router, _, tarballRequests, _ := newNPMProvenanceFixture(t, deterministicTarballSigner(t, 0x83))
	checker := &npmRecordingQuarantineChecker{}
	ruleChecker := &npmRecordingPackageRuleChecker{
		decision: adapter.PackageRuleDecision{Outcome: adapter.PackageRuleAllow},
	}
	scoped := adapter.NewRequestScope(nil, nil, checker, ruleChecker).Wrap(router)

	signedURL := requestNPMMetadataTarballURLAt(t, router, "/p/acme/npm/fixture", "1.0.0")
	if !strings.Contains(signedURL, "/p/acme/npm/fixture/-/"+SignedTarballRouteSegment+"/") {
		t.Fatalf("project metadata emitted the wrong route: %q", signedURL)
	}
	if response := requestURLPath(t, scoped, signedURL); response.Code != http.StatusOK {
		t.Fatalf("project artifact status=%d body=%s", response.Code, response.Body.String())
	}
	parsed, err := url.Parse(signedURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, replay := range []string{
		strings.Replace(parsed.Path, "/p/acme/npm/", "/npm/", 1),
		strings.Replace(parsed.Path, "/p/acme/npm/", "/p/other/npm/", 1),
	} {
		if response := requestURLPath(t, scoped, replay); response.Code != http.StatusNotFound {
			t.Errorf("cross-audience replay %q status=%d body=%s", replay, response.Code, response.Body.String())
		}
	}
	if got := checker.snapshot(); len(got) != 1 {
		t.Fatalf("cross-audience replay called checker: %#v", got)
	}
	if calls := ruleChecker.snapshot(); len(calls) != 1 || calls[0].version != "1.0.0" {
		t.Fatalf("cross-audience replay called package rules: %#v", calls)
	}
	if got := tarballRequests.Load(); got != 1 {
		t.Fatalf("cross-audience replay contacted upstream: requests=%d", got)
	}
}

func TestSignedTarballFetchPreservesEncodedTargetAndQuery(t *testing.T) {
	artifactRequests := &atomic.Int64{}
	var upstreamServer *httptest.Server
	upstreamServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/fixture":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"fixture","versions":{"1.0.0":{"dist":{"tarball":"`+upstreamServer.URL+`/fixture/-/pkg%3Fvariant%231.tgz?download=1&path=%2F"}}}}`)
		case "/fixture/-/pkg?variant#1.tgz":
			artifactRequests.Add(1)
			if request.URL.EscapedPath() != "/fixture/-/pkg%3Fvariant%231.tgz" ||
				request.URL.RawQuery != "download=1&path=%2F" {
				t.Errorf("artifact request URI changed: escapedPath=%q query=%q", request.URL.EscapedPath(), request.URL.RawQuery)
			}
			_, _ = io.WriteString(w, "encoded-target")
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(upstreamServer.Close)
	newRouter := newNPMProvenanceRouterFactory(t, []config.UpstreamConfig{{
		Name: "encoded", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	router := newRouter(deterministicTarballSigner(t, 0x84))

	signedURL := requestNPMMetadataTarballURL(t, router)
	response := requestURLPath(t, router, signedURL)
	if response.Code != http.StatusOK || response.Body.String() != "encoded-target" {
		t.Fatalf("encoded target status=%d body=%q url=%q", response.Code, response.Body.String(), signedURL)
	}
	if got := artifactRequests.Load(); got != 1 {
		t.Fatalf("artifact requests=%d, want 1", got)
	}
}

func TestSignedTarballFetchUsesConfiguredBasicAuthOnOrigin(t *testing.T) {
	const username = "registry-user"
	const password = "registry-password"
	metadataRequests := &atomic.Int64{}
	artifactRequests := &atomic.Int64{}
	var upstreamServer *httptest.Server
	upstreamServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotUsername, gotPassword, authenticated := request.BasicAuth()
		if !authenticated || gotUsername != username || gotPassword != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="private npm"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/fixture":
			metadataRequests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"fixture","versions":{"1.0.0":{"dist":{"tarball":"`+upstreamServer.URL+`/fixture/-/fixture-1.0.0.tgz"}}}}`)
		case "/fixture/-/fixture-1.0.0.tgz":
			artifactRequests.Add(1)
			_, _ = io.WriteString(w, "private-artifact")
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(upstreamServer.Close)

	configuredURL, err := url.Parse(upstreamServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	configuredURL.User = url.UserPassword(username, password)
	newRouter := newNPMProvenanceRouterFactory(t, []config.UpstreamConfig{{
		Name: "private", URL: configuredURL.String(), Priority: 1, ProbeMode: "passive",
	}})
	router := newRouter(deterministicTarballSigner(t, 0x85))

	signedURL := requestNPMMetadataTarballURL(t, router)
	response := requestURLPath(t, router, signedURL)
	if response.Code != http.StatusOK || response.Body.String() != "private-artifact" {
		t.Fatalf("private artifact status=%d body=%q", response.Code, response.Body.String())
	}
	if got := metadataRequests.Load(); got != 1 {
		t.Fatalf("authenticated metadata requests=%d, want 1", got)
	}
	if got := artifactRequests.Load(); got != 1 {
		t.Fatalf("authenticated artifact requests=%d, want 1", got)
	}
}

func TestSignedTarballFetchDoesNotSendOriginBasicAuthToDifferentOrigin(t *testing.T) {
	const username = "registry-user"
	const password = "registry-password"
	var artifactAuthorization string
	var artifactAuthorizationMu sync.Mutex
	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		artifactAuthorizationMu.Lock()
		artifactAuthorization = request.Header.Get("Authorization")
		artifactAuthorizationMu.Unlock()
		_, _ = io.WriteString(w, "external-artifact")
	}))
	t.Cleanup(artifactServer.Close)

	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotUsername, gotPassword, authenticated := request.BasicAuth()
		if !authenticated || gotUsername != username || gotPassword != password {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if request.URL.Path != "/fixture" {
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"fixture","versions":{"1.0.0":{"dist":{"tarball":"`+artifactServer.URL+`/fixture-1.0.0.tgz"}}}}`)
	}))
	t.Cleanup(registryServer.Close)

	configuredURL, err := url.Parse(registryServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	configuredURL.User = url.UserPassword(username, password)
	newRouter := newNPMProvenanceRouterFactory(t, []config.UpstreamConfig{{
		Name: "private", URL: configuredURL.String(), Priority: 1, ProbeMode: "passive",
	}})
	router := newRouter(deterministicTarballSigner(t, 0x86))

	signedURL := requestNPMMetadataTarballURL(t, router)
	response := requestURLPath(t, router, signedURL)
	if response.Code != http.StatusOK || response.Body.String() != "external-artifact" {
		t.Fatalf("external artifact status=%d body=%q", response.Code, response.Body.String())
	}
	artifactAuthorizationMu.Lock()
	gotAuthorization := artifactAuthorization
	artifactAuthorizationMu.Unlock()
	if gotAuthorization != "" {
		t.Fatalf("configured-origin Authorization leaked to different origin: %q", gotAuthorization)
	}
}

func TestSignedTarballFetchDropsOriginBasicAuthOnCrossOriginRedirect(t *testing.T) {
	const username = "registry-user"
	const password = "registry-password"
	var redirectedAuthorization string
	var redirectedAuthorizationMu sync.Mutex
	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		redirectedAuthorizationMu.Lock()
		redirectedAuthorization = request.Header.Get("Authorization")
		redirectedAuthorizationMu.Unlock()
		_, _ = io.WriteString(w, "redirected-artifact")
	}))
	t.Cleanup(artifactServer.Close)

	var registryServer *httptest.Server
	registryServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotUsername, gotPassword, authenticated := request.BasicAuth()
		if !authenticated || gotUsername != username || gotPassword != password {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/fixture":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"name":"fixture","versions":{"1.0.0":{"dist":{"tarball":"`+registryServer.URL+`/fixture/-/fixture-1.0.0.tgz"}}}}`)
		case "/fixture/-/fixture-1.0.0.tgz":
			http.Redirect(w, request, artifactServer.URL+"/redirected.tgz", http.StatusTemporaryRedirect)
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(registryServer.Close)

	configuredURL, err := url.Parse(registryServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	configuredURL.User = url.UserPassword(username, password)
	newRouter := newNPMProvenanceRouterFactory(t, []config.UpstreamConfig{{
		Name: "private", URL: configuredURL.String(), Priority: 1, ProbeMode: "passive",
	}})
	router := newRouter(deterministicTarballSigner(t, 0x87))

	signedURL := requestNPMMetadataTarballURL(t, router)
	response := requestURLPath(t, router, signedURL)
	if response.Code != http.StatusOK || response.Body.String() != "redirected-artifact" {
		t.Fatalf("redirected artifact status=%d body=%q", response.Code, response.Body.String())
	}
	redirectedAuthorizationMu.Lock()
	gotAuthorization := redirectedAuthorization
	redirectedAuthorizationMu.Unlock()
	if gotAuthorization != "" {
		t.Fatalf("configured-origin Authorization leaked across redirect: %q", gotAuthorization)
	}
}
