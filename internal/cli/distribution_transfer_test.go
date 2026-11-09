package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"depsilo/internal/api/admin"
	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/distribution"
	"github.com/gin-gonic/gin"
)

func transferCLIStore(t *testing.T) *distribution.Store {
	t.Helper()
	root := t.TempDir()
	database, err := db.Open("sqlite", filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewPrivateLocalStorage(filepath.Join(root, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := distribution.NewStore(database, storage, distribution.Limits{MaxObjectBytes: 1024, MaxStorageBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func transferCLIServer(t *testing.T, store *distribution.Store) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := admin.NewDistributionHandler(store, nil)
	router := gin.New()
	router.GET("/api/v1/admin/distribution/sets/:id/bundle", h.Export)
	router.POST("/api/v1/admin/distribution/import", h.Import)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

func TestDistributionCLITransfersBundleBetweenInstances(t *testing.T) {
	t.Setenv("DEPSILO_TOKEN", "token")
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "transfer.pem")
	out, code := captureCLIStdout(t, func() int { return Run("distribution", []string{"keygen", "--out", keyPath, "--json"}) })
	var generated map[string]string
	if code != 0 || json.Unmarshal([]byte(out), &generated) != nil || !strings.HasPrefix(generated["public_key"], "ed25519:") {
		t.Fatalf("keygen code=%d output=%s", code, out)
	}
	if info, err := os.Stat(keyPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode: %v %v", info, err)
	}
	if _, code := captureCLIStdout(t, func() int { return Run("distribution", []string{"keygen", "--out", keyPath, "--json"}) }); code == 0 {
		t.Fatal("keygen replaced an existing key file")
	}
	signer, err := distribution.LoadSigningKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	_, _, id, digest, raw := distributionCLIFixture(t)
	source := transferCLIStore(t)
	source.ConfigureTransfer(distribution.TransferKeys{Signer: signer})
	if _, err := source.Stage(context.Background(), raw, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Put(context.Background(), id, digest, strings.NewReader("wheel")); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Verify(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	sourceServer := transferCLIServer(t, source)

	bundlePath := filepath.Join(dir, "set.tar")
	out, code = captureCLIStdout(t, func() int {
		return Run("distribution", []string{"export", id, "--out", bundlePath, "--url", sourceServer.URL, "--json"})
	})
	if code != 0 || !strings.Contains(out, `"content_verified": true`) || !strings.Contains(out, generated["key_id"]) {
		t.Fatalf("export code=%d output=%s", code, out)
	}
	saved, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, code := captureCLIStdout(t, func() int {
		return Run("distribution", []string{"export", id, "--out", bundlePath, "--url", sourceServer.URL, "--json"})
	}); code == 0 {
		t.Fatal("export replaced an existing bundle")
	}
	if again, _ := os.ReadFile(bundlePath); !bytes.Equal(again, saved) {
		t.Fatal("failed export modified the existing bundle")
	}

	target := transferCLIStore(t)
	targetServer := transferCLIServer(t, target)
	out, code = captureCLIStdout(t, func() int {
		return Run("distribution", []string{"import", "--bundle", bundlePath, "--url", targetServer.URL, "--json"})
	})
	if code == 0 || !strings.Contains(out, "trusted transfer key") {
		t.Fatalf("untrusted import code=%d output=%s", code, out)
	}
	trusted, err := distribution.ParseTrustedKeys([]string{generated["public_key"]})
	if err != nil {
		t.Fatal(err)
	}
	target.ConfigureTransfer(distribution.TransferKeys{Trusted: trusted})
	out, code = captureCLIStdout(t, func() int {
		return Run("distribution", []string{"import", "--bundle", bundlePath, "--url", targetServer.URL, "--json"})
	})
	if code != 0 || !strings.Contains(out, `"provenance_status": "preparer_attested"`) {
		t.Fatalf("import code=%d output=%s", code, out)
	}
	if set, err := target.Get(context.Background(), id); err != nil || set.ContentStatus != "content_verified" {
		t.Fatalf("imported set: %+v %v", set, err)
	}
}

func TestDistributionCLIExportDiscardsShortDownloads(t *testing.T) {
	t.Setenv("DEPSILO_TOKEN", "token")
	_, signer, _ := ed25519.GenerateKey(nil)
	_, _, id, digest, raw := distributionCLIFixture(t)
	source := transferCLIStore(t)
	source.ConfigureTransfer(distribution.TransferKeys{Signer: signer})
	_, _ = source.Stage(context.Background(), raw, "operator")
	_, _ = source.Put(context.Background(), id, digest, strings.NewReader("wheel"))
	_, _ = source.Verify(context.Background(), id)
	export, err := source.PrepareExport(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var full bytes.Buffer
	if err := export.Write(context.Background(), &full); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Declare the full length but stop after the object: the client must see a short body.
		w.Header().Set("Content-Length", strconv.Itoa(full.Len()))
		w.WriteHeader(200)
		_, _ = w.Write(full.Bytes()[:full.Len()-1024])
	}))
	defer server.Close()
	dir := t.TempDir()
	out, code := captureCLIStdout(t, func() int {
		return Run("distribution", []string{"export", id, "--out", filepath.Join(dir, "set.tar"), "--url", server.URL, "--json"})
	})
	if code == 0 || !strings.Contains(out, "nothing was saved") {
		t.Fatalf("short export code=%d output=%s", code, out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("short export left files: %v", entries)
	}
}

func transferCLIBundle(t *testing.T) (bundle []byte, id string, trusted []ed25519.PublicKey) {
	t.Helper()
	public, signer, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, id, digest, raw := distributionCLIFixture(t)
	source := transferCLIStore(t)
	source.ConfigureTransfer(distribution.TransferKeys{Signer: signer})
	if _, err := source.Stage(context.Background(), raw, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Put(context.Background(), id, digest, strings.NewReader("wheel")); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Verify(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	export, err := source.PrepareExport(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := export.Write(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Bytes(), id, []ed25519.PublicKey{public}
}

func TestDistributionCLIExportFallsBackWithoutHardLinks(t *testing.T) {
	t.Setenv("DEPSILO_TOKEN", "token")
	bundle, id, _ := transferCLIBundle(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(bundle)))
		_, _ = w.Write(bundle)
	}))
	defer server.Close()
	original := linkFile
	t.Cleanup(func() { linkFile = original })
	// exFAT, FAT32 and many network shares report an unsupported operation.
	linkFile = func(string, string) error { return &os.LinkError{Op: "link", Err: errors.ErrUnsupported} }
	dir := t.TempDir()
	out := filepath.Join(dir, "set.tar")
	if output, code := captureCLIStdout(t, func() int {
		return Run("distribution", []string{"export", id, "--out", out, "--url", server.URL, "--json"})
	}); code != 0 {
		t.Fatalf("export without hard links code=%d output=%s", code, output)
	}
	if saved, err := os.ReadFile(out); err != nil || !bytes.Equal(saved, bundle) {
		t.Fatalf("fallback did not publish the verified bundle: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("fallback left temporary files: %v", entries)
	}
	// The fallback still refuses to replace a destination.
	if err := publishBundle(filepath.Join(dir, "missing-temp"), out); err == nil || !strings.Contains(err.Error(), "nothing was replaced") {
		t.Fatalf("fallback replaced an existing destination: %v", err)
	}
}

func TestDistributionCLIImportFailureKeepsServerDiagnostics(t *testing.T) {
	t.Setenv("DEPSILO_TOKEN", "token")
	bundle, id, trusted := transferCLIBundle(t)
	target := transferCLIStore(t)
	target.ConfigureTransfer(distribution.TransferKeys{Trusted: trusted})
	server := transferCLIServer(t, target)
	path := filepath.Join(t.TempDir(), "truncated.tar")
	if err := os.WriteFile(path, bundle[:len(bundle)-1024], 0o600); err != nil {
		t.Fatal(err)
	}
	output, code := captureCLIStdout(t, func() int {
		return Run("distribution", []string{"import", "--bundle", path, "--url", server.URL, "--json"})
	})
	var report struct {
		Error  string `json:"error"`
		Import struct {
			SetID          string `json:"set_id"`
			WrittenObjects int    `json:"written_objects"`
		} `json:"import"`
	}
	if code == 0 || json.Unmarshal([]byte(output), &report) != nil || !strings.Contains(report.Error, "HTTP 400") ||
		report.Import.SetID != id || report.Import.WrittenObjects != 1 {
		t.Fatalf("failed import diagnostics code=%d output=%s", code, output)
	}
}
