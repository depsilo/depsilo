package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/distribution"
)

func distributionCLIFixture(t *testing.T) (manifestPath, objectDir, setID, digest string, raw []byte) {
	t.Helper()
	root := t.TempDir()
	objectDir = filepath.Join(root, "objects")
	if err := os.Mkdir(objectDir, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("wheel"))
	digest = hex.EncodeToString(sum[:])
	manifest := distribution.Manifest{Format: distribution.ManifestFormat, Name: "cli-example", Revision: "v1", Ecosystem: "pypi",
		Target:  distribution.Target{OS: "linux", Architecture: "amd64", PythonVersion: "3.12.7", PythonABI: "cp312", PipVersion: "24.2", ClientImage: "python@sha256:" + strings.Repeat("a", 64)},
		Objects: []distribution.Object{{SHA256: digest, Size: 5, Kind: "wheel", Filename: "example-1.0-py3-none-any.whl", Package: "example", Version: "1.0"}}}
	var err error
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath = filepath.Join(root, "manifest.json")
	if err = os.WriteFile(manifestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(objectDir, digest), []byte("wheel"), 0600); err != nil {
		t.Fatal(err)
	}
	sum = sha256.Sum256(raw)
	setID = hex.EncodeToString(sum[:])
	return
}

func TestDistributionCLIStagesStreamsAndResumesExactManifest(t *testing.T) {
	manifestPath, objectDir, id, digest, raw := distributionCLIFixture(t)
	t.Setenv("DEPSILO_TOKEN", "operator-token")
	var stages, uploads, verifies atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer operator-token" {
			t.Errorf("missing bearer credential")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/admin/distribution/sets":
			body, _ := io.ReadAll(r.Body)
			if string(body) != string(raw) {
				t.Errorf("manifest bytes changed")
			}
			stages.Add(1)
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
		case "PUT /api/v1/admin/distribution/sets/" + id + "/objects/" + digest:
			body, _ := io.ReadAll(r.Body)
			if string(body) != "wheel" || r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Errorf("wrong streamed object")
			}
			uploads.Add(1)
			_, _ = io.WriteString(w, `{"reused":true}`)
		case "POST /api/v1/admin/distribution/sets/" + id + "/verify":
			verifies.Add(1)
			_, _ = io.WriteString(w, `{"content_status":"content_verified","serving":false}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for i := 0; i < 2; i++ {
		output, code := captureCLIStdout(t, func() int {
			return Run("distribution", []string{"stage", "--objects", objectDir, "--url", server.URL, "--manifest", manifestPath, "--json"})
		})
		if code != 0 || !strings.Contains(output, `"scope": "catalog_only"`) || !strings.Contains(output, `"install_validation": "not_run"`) || !strings.Contains(output, id) {
			t.Fatalf("stage code=%d output=%s", code, output)
		}
	}
	if stages.Load() != 2 || uploads.Load() != 2 || verifies.Load() != 2 {
		t.Fatalf("stage/upload/verify counts=%d/%d/%d", stages.Load(), uploads.Load(), verifies.Load())
	}
}

func TestDistributionCLIRefusesUnsafeObjectFilesBeforeMutation(t *testing.T) {
	for _, kind := range []string{"missing", "symlink", "directory", "wrong size", "root symlink"} {
		t.Run(kind, func(t *testing.T) {
			manifestPath, objectDir, _, digest, _ := distributionCLIFixture(t)
			file := filepath.Join(objectDir, digest)
			if kind == "root symlink" {
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(objectDir, alias); err != nil {
					t.Fatal(err)
				}
				objectDir = alias
			} else {
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "symlink":
					outside := filepath.Join(t.TempDir(), "private")
					if err := os.WriteFile(outside, []byte("wheel"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(outside, file); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(file, 0700); err != nil {
						t.Fatal(err)
					}
				case "wrong size":
					if err := os.WriteFile(file, []byte("wrong-size"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			called := false
			_, err := stageDistribution(func(string, string, io.Reader) ([]byte, error) { called = true; return nil, nil }, manifestPath, objectDir)
			if err == nil || called || strings.Contains(err.Error(), objectDir) {
				t.Fatalf("preflight err=%v requested=%v", err, called)
			}
		})
	}
}

func TestDistributionCLIRefusesRedirectAndMismatchedManifestID(t *testing.T) {
	manifestPath, objectDir, _, _, _ := distributionCLIFixture(t)
	t.Setenv("DEPSILO_TOKEN", "never-forward-this")
	var forwarded atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1); w.WriteHeader(200) }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	output, code := captureCLIStdout(t, func() int {
		return Run("distribution", []string{"stage", "--manifest", manifestPath, "--objects", objectDir, "--url", origin.URL, "--json"})
	})
	if code != 1 || forwarded.Load() != 0 || strings.Contains(output, "never-forward-this") || !strings.Contains(output, "307") {
		t.Fatalf("redirect code=%d forwarded=%d output=%s", code, forwarded.Load(), output)
	}
	calls := 0
	_, err := stageDistribution(func(string, string, io.Reader) ([]byte, error) {
		calls++
		return []byte(`{"id":"` + strings.Repeat("b", 64) + `"}`), nil
	}, manifestPath, objectDir)
	if err == nil || calls != 1 {
		t.Fatalf("wrong manifest identity err=%v calls=%d", err, calls)
	}
}

func TestDistributionCLIOperatorCommandsAndIncompleteExit(t *testing.T) {
	id := strings.Repeat("a", 64)
	t.Setenv("DEPSILO_TOKEN", "token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/admin/distribution/sets":
			_, _ = io.WriteString(w, `{"items":[],"scope":"catalog_only"}`)
		case "GET /api/v1/admin/distribution/sets/" + id:
			_, _ = io.WriteString(w, `{"content_status":"staged"}`)
		case "POST /api/v1/admin/distribution/sets/" + id + "/verify":
			_, _ = io.WriteString(w, `{"content_status":"incomplete","issues":[{"code":"missing"}]}`)
		case "DELETE /api/v1/admin/distribution/sets/" + id:
			w.WriteHeader(204)
		case "POST /api/v1/admin/distribution/prune":
			_, _ = io.WriteString(w, `{"removed_bytes":5,"serving":false}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		command  string
		want     int
		contains string
	}{{"list", 0, "items"}, {"inspect", 0, "staged"}, {"verify", 1, "incomplete"}, {"delete", 0, "deleted"}, {"prune", 0, "removed_bytes"}} {
		args := []string{tc.command, "--url", server.URL, "--json"}
		if tc.command != "list" && tc.command != "prune" {
			args = append(args, id)
		}
		out, code := captureCLIStdout(t, func() int { return Run("distribution", args) })
		if code != tc.want || !strings.Contains(out, tc.contains) {
			t.Fatalf("%s code=%d output=%s", tc.command, code, out)
		}
	}
}

func TestDistributionCLIBoundsResponsesAndRejectsCredentialURLs(t *testing.T) {
	for _, url := range []string{"https://user:secret@example.test", "file:///secret", "https://example.test?token=secret", "https://example.test/#fragment"} {
		if _, err := distributionServerURL(url); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe URL error %v", err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", distributionResponseLimit+1))
	}))
	defer server.Close()
	client := &http.Client{Timeout: time.Second}
	if _, err := distributionRequest(context.Background(), client, http.MethodGet, server.URL, nil); err == nil {
		t.Fatal("unbounded response accepted")
	}
}

func TestDistributionCLIAcceptsHTMLEscapedManifestResponse(t *testing.T) {
	_, _, _, _, raw := distributionCLIFixture(t)
	var manifest distribution.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Objects = nil
	for i := 1; i <= 400; i++ {
		manifest.Objects = append(manifest.Objects, distribution.Object{
			SHA256: fmt.Sprintf("%064x", i), Size: 1, Kind: "wheel", Package: "example", Version: "1.0",
			Filename:  fmt.Sprintf("example%d-1.0-py3-none-any.whl", i),
			SourceURL: "https://packages.example/" + strings.Repeat("<", 1900),
		})
	}
	var submitted bytes.Buffer
	encoder := json.NewEncoder(&submitted)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(manifest); err != nil {
		t.Fatal(err)
	}
	if submitted.Len() > distribution.MaxManifestBytes {
		t.Fatal("fixture exceeds accepted manifest size")
	}
	if _, err := distribution.ParseManifest(submitted.Bytes()); err != nil {
		t.Fatalf("fixture is not a valid declaration: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(distribution.Set{Manifest: manifest})
	}))
	defer server.Close()
	response, err := distributionRequest(context.Background(), &http.Client{Timeout: time.Second}, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response) <= 4<<20 {
		t.Fatal("fixture did not exercise HTML expansion beyond the former limit")
	}
	var set distribution.Set
	if err := json.Unmarshal(response, &set); err != nil || len(set.Manifest.Objects) != 400 {
		t.Fatalf("expanded manifest response lost content: objects=%d err=%v", len(set.Manifest.Objects), err)
	}
}
