//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/mod/sumdb"

	"depsilo/tests/mock"
)

func TestGo_ModuleList(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/go/github.com/test/mod/@v/list")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, "v1.0.0") {
		t.Error("version list missing v1.0.0")
	}
}

func TestGo_ModuleInfo(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/go/github.com/test/mod/@v/v1.0.0.info")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, `"Version":"v1.0.0"`) {
		t.Error("version info missing")
	}
}

func TestGo_ModuleMod(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/go/github.com/test/mod/@v/v1.0.0.mod")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, "module github.com/test/mod") {
		t.Error("go.mod content missing")
	}
}

func TestGo_ModuleZipMissThenHit(t *testing.T) {
	assertMissThenHit(t, depsiloURL+"/go/github.com/test/cachehit/@v/v1.0.0.zip", "FAKE_CACHEHIT_GO_ZIP", true)
}

// sumDBClient is the checksum database client of the go command, reading the
// database only through Depsilo's Go route. It keeps no state between tests,
// like a go command with an empty GOPATH.
type sumDBClient struct {
	t      *testing.T
	config map[string][]byte
	remote atomic.Int64 // requests sent to Depsilo
}

func newSumDBClient(t *testing.T) *sumdb.Client {
	return sumdb.NewClient(&sumDBClient{t: t, config: map[string][]byte{
		"key": []byte(mockServer.GoSumDBKey()),
	}})
}

func (c *sumDBClient) ReadRemote(path string) ([]byte, error) {
	c.remote.Add(1)
	resp, err := http.Get(depsiloURL + "/go/sumdb/" + mock.GoSumDBName + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s: %s", path, resp.Status, body)
	}
	return body, nil
}

func (c *sumDBClient) ReadConfig(file string) ([]byte, error) { return c.config[file], nil }

func (c *sumDBClient) WriteConfig(file string, old, new []byte) error {
	if !bytes.Equal(c.config[file], old) {
		return sumdb.ErrWriteConflict
	}
	c.config[file] = new
	return nil
}

func (c *sumDBClient) ReadCache(string) ([]byte, error) { return nil, os.ErrNotExist }
func (c *sumDBClient) WriteCache(string, []byte)        {}
func (c *sumDBClient) Log(msg string)                   { c.t.Log(msg) }
func (c *sumDBClient) SecurityError(msg string) {
	c.t.Errorf("checksum database security error: %s", msg)
}

func sumDBUpstreamRequests() []string {
	var paths []string
	for _, request := range mockServer.Requests() {
		if strings.HasPrefix(request.Path, "/sumdb/") {
			paths = append(paths, request.Path)
		}
	}
	return paths
}

// A go command pointed at Depsilo asks it whether it relays the checksum
// database, then verifies module hashes against records and tiles it reads
// through Depsilo.
func TestGo_ChecksumDatabaseIsRelayedAndVerifies(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/go/sumdb/"+mock.GoSumDBName+"/supported")
	assertStatus(t, resp, 200)
	resp.Body.Close()

	// Grow the log first so that proving a record needs hash tiles.
	for _, module := range []string{"github.com/test/a", "github.com/test/b", "github.com/test/c"} {
		if _, err := newSumDBClient(t).Lookup(module, "v1.0.0"); err != nil {
			t.Fatalf("lookup %s: %v", module, err)
		}
	}

	want := strings.Split(strings.TrimSpace(mock.GoSumLines("github.com/test/mod", "v1.0.0")), "\n")
	lookup := func() {
		t.Helper()
		lines, err := newSumDBClient(t).Lookup("github.com/test/mod", "v1.0.0")
		if err != nil {
			t.Fatalf("verified lookup through Depsilo: %v", err)
		}
		if len(lines) != 1 || lines[0] != want[0] {
			t.Fatalf("go.sum lines = %q, want %q", lines, want[:1])
		}
	}
	lookup()
	served := sumDBUpstreamRequests()
	var sawLookup, sawTile bool
	for _, path := range served {
		sawLookup = sawLookup || path == "/sumdb/"+mock.GoSumDBName+"/lookup/github.com/test/mod@v1.0.0"
		sawTile = sawTile || strings.HasPrefix(path, "/sumdb/"+mock.GoSumDBName+"/tile/")
	}
	if !sawLookup || !sawTile {
		t.Fatalf("upstream requests = %v, want the lookup and at least one tile", served)
	}

	// A second client with nothing cached is answered without the upstream.
	lookup()
	if again := sumDBUpstreamRequests(); len(again) != len(served) {
		t.Errorf("second verification asked the upstream: %v", again[len(served):])
	}

	resp = httpGet(t, depsiloURL+"/go/sumdb/"+mock.GoSumDBName+"/latest")
	assertStatus(t, resp, 200)
	if body := readBody(t, resp); !strings.HasPrefix(body, "go.sum database tree\n") {
		t.Errorf("latest = %q, want a signed tree head", body)
	}
}

type goAuditRow struct {
	ID          uint      `json:"id"`
	Ecosystem   string    `json:"ecosystem"`
	PackageName string    `json:"package_name"`
	Version     string    `json:"version"`
	Action      string    `json:"action"`
	StatusCode  int       `json:"status_code"`
	CreatedAt   time.Time `json:"created_at"`
}

func adminItems(t *testing.T, path string) []goAuditRow {
	t.Helper()
	resp := adminGet(t, depsiloURL+path)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s = %d: %s", path, resp.StatusCode, body)
	}
	var page struct {
		Items []goAuditRow `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return page.Items
}

// Reading the checksum database downloads no dependency. What a go command
// asks it is audited as metadata and stays out of the Admin recent-downloads
// feed, which shows the module the go command then fetched.
func TestGo_ChecksumDatabaseRequestsAreAuditedAsMetadataNotDownloads(t *testing.T) {
	const module = "github.com/test/audited"
	started := time.Now().UTC()

	resp := httpGet(t, depsiloURL+"/go/sumdb/"+mock.GoSumDBName+"/supported")
	assertStatus(t, resp, 200)
	resp.Body.Close()
	client := &sumDBClient{t: t, config: map[string][]byte{"key": []byte(mockServer.GoSumDBKey())}}
	if _, err := sumdb.NewClient(client).Lookup(module, "v1.0.0"); err != nil {
		t.Fatalf("verified lookup through Depsilo: %v", err)
	}
	checksumRequests := 1 + int(client.remote.Load())
	resp = httpGet(t, depsiloURL+"/go/"+module+"/@v/v1.0.0.zip")
	assertStatus(t, resp, 200)
	resp.Body.Close()

	// Audit rows are persisted in request order shortly after the response:
	// once the module download shows, the checksum requests made before it
	// are stored too, and would be the rows right below it.
	var downloads []goAuditRow
	deadline := time.Now().Add(10 * time.Second)
	for {
		downloads = adminItems(t, "/api/v1/admin/dashboard/recent-downloads?limit=20")
		if len(downloads) > 0 && downloads[0].Ecosystem == "go" && downloads[0].PackageName == module && downloads[0].Version == "v1.0.0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the module download never became the newest recent download; feed = %+v", downloads)
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, row := range downloads {
		if row.Ecosystem == "go" && row.PackageName == "" {
			t.Errorf("recent downloads show a Go download of no package: %+v", row)
		}
	}

	// Module requests are audited under their module path, so Go rows without
	// a package name are checksum database requests. A row is dated at the
	// start of its request; the filter takes whole seconds, so the requests
	// of a test that ended within this second are told apart here.
	query := url.Values{"ecosystem": {"go"}, "start": {started.Truncate(time.Second).Format(time.RFC3339)}, "page_size": {"200"}}
	var audited int
	for _, row := range adminItems(t, "/api/v1/admin/audit-logs?"+query.Encode()) {
		if row.PackageName != "" || row.CreatedAt.Before(started) {
			continue
		}
		audited++
		if row.Action != "metadata" {
			t.Errorf("checksum database request audited as %+v, want action metadata", row)
		}
	}
	if audited != checksumRequests {
		t.Errorf("audit log holds %d checksum database requests since %s, want the %d this test made",
			audited, started.Format(time.RFC3339Nano), checksumRequests)
	}
}

// A 404 tells the go command to contact the checksum database itself.
func TestGo_ChecksumDatabaseTheUpstreamLacksIsNotFound(t *testing.T) {
	for _, path := range []string{"/supported", "/latest", "/lookup/github.com/test/mod@v1.0.0", "/tile/8/0/000"} {
		resp := httpGet(t, depsiloURL+"/go/sumdb/sum.golang.org"+path)
		assertStatus(t, resp, 404)
		resp.Body.Close()
	}
}

func TestGo_ChecksumDatabaseRejectsMalformedPaths(t *testing.T) {
	before := mockServer.RequestCount()
	for _, path := range []string{
		"/go/sumdb/" + mock.GoSumDBName,
		"/go/sumdb/" + mock.GoSumDBName + "/unknown",
		"/go/sumdb/not_a_host/latest",
		"/go/sumdb/" + mock.GoSumDBName + "/lookup/github.com/test/mod",
		"/go/sumdb/" + mock.GoSumDBName + "/lookup/github.com/test/mod@v1.0.0-a%23b",
		"/go/sumdb/" + mock.GoSumDBName + "/tile/8/0/0",
		"/go/sumdb/" + mock.GoSumDBName + "/tile/8/0/%2e%2e/000",
	} {
		resp := httpGet(t, depsiloURL+path)
		assertStatus(t, resp, 404)
		resp.Body.Close()
	}
	if after := mockServer.RequestCount(); after != before {
		t.Errorf("malformed checksum database requests reached the upstream (%d requests)", after-before)
	}
}
