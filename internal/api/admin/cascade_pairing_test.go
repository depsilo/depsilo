package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/api/contract"
	"depsilo/internal/cascade"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/relay"
	"depsilo/internal/upstream"
)

// pairingNode is one Depsilo node for the pairing tests: its config file,
// store, registry, and the cascade settings its store last applied.
type pairingNode struct {
	id       string
	path     string
	store    *config.Store
	registry *upstream.Registry
	router   *gin.Engine

	mu      sync.Mutex
	applied config.CascadeConfig
}

func (n *pairingNode) Settings() config.CascadeConfig {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.applied
}
func (n *pairingNode) InstanceID() string { return n.id }

func newPairingNode(t *testing.T, id string, rows ...db.UpstreamRecord) *pairingNode {
	t.Helper()
	gin.SetMode(gin.TestMode)
	node := &pairingNode{id: id, path: filepath.Join(t.TempDir(), "config.toml")}
	if err := os.WriteFile(node.path, []byte("config_version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	effective := &config.Config{}
	effective.Cascade.MaxHops, effective.Cascade.MaxTTL, effective.Cascade.DirectFallback = 4, 168*time.Hour, true
	node.store = config.NewStore(node.path, effective, zap.NewAtomicLevel())
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	active := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		row.ProbeMode, row.ProbeInterval, row.Healthy = "passive", "30m", true
		if err := database.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if !seen[row.AdapterType] {
			seen[row.AdapterType] = true
			active = append(active, row.AdapterType)
		}
	}
	if node.registry, err = upstream.NewRegistry(database, active); err != nil {
		t.Fatal(err)
	}
	node.store.SetCascadeApplier(func(settings config.CascadeConfig) error {
		entries := make([]upstream.PeerEgress, 0, len(settings.Peers))
		for _, peer := range settings.Peers {
			parsed, _ := url.Parse(peer.URL)
			entries = append(entries, upstream.PeerEgress{Name: peer.Name, URL: parsed, Token: peer.Token, LocalInstanceID: id, MaxHops: settings.MaxHops, DirectFallback: settings.DirectFallback})
		}
		if err := node.registry.SetPeers(upstream.NewPeerSet(entries)); err != nil {
			return err
		}
		node.mu.Lock()
		node.applied = settings
		node.mu.Unlock()
		return nil
	})
	handler := NewCascadeConfigHandler(node.store, node.registry, node)
	node.router = gin.New()
	node.router.POST("/cascade/pairing", handler.Pairing)
	node.router.POST("/cascade/join", handler.Join)
	return node
}

func (n *pairingNode) post(t *testing.T, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, _ := json.Marshal(body)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	n.router.ServeHTTP(recorder, request)
	return recorder
}

// serveRelay serves the node's relay with the token its store last applied.
func (n *pairingNode) serveRelay(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		settings := n.Settings()
		if !settings.Enabled || request.URL.Path != cascade.RelayPath {
			http.NotFound(writer, request)
			return
		}
		(&relay.Handler{Identity: n.id, Token: settings.Token}).ServeHTTP(writer, request)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCascadePairingTurnsAParentOnAndAChildJoinsIt(t *testing.T) {
	parent := newPairingNode(t, "parent-node", db.UpstreamRecord{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1})
	relayServer := parent.serveRelay(t)

	recorder := parent.post(t, "/cascade/pairing", map[string]string{"url": relayServer.URL + "/"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("pairing: %d %s", recorder.Code, recorder.Body.String())
	}
	var pairing contract.AdminCascadePairingResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &pairing); err != nil {
		t.Fatal(err)
	}
	if !pairing.EnabledNow || pairing.RestartRequired || !pairing.Loopback || pairing.URL != relayServer.URL {
		t.Fatalf("pairing = %+v", pairing)
	}
	decoded, err := cascade.DecodePairingCode(pairing.Code)
	if err != nil || decoded.InstanceID != "parent-node" || decoded.URL != relayServer.URL || decoded.Token != parent.Settings().Token || len(decoded.Token) < 16 {
		t.Fatalf("code = %+v, %v", decoded, err)
	}
	// A second request reuses the token and changes nothing.
	again := parent.post(t, "/cascade/pairing", map[string]string{"url": relayServer.URL})
	if !strings.Contains(again.Body.String(), `"enabled_now":false`) || !strings.Contains(again.Body.String(), pairing.Code) {
		t.Fatalf("second pairing = %s", again.Body.String())
	}

	child := newPairingNode(t, "child-node",
		db.UpstreamRecord{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1},
		db.UpstreamRecord{AdapterType: "pypi", Name: "intranet", URL: "http://10.0.0.5:8080", Priority: 1},
	)
	recorder = child.post(t, "/cascade/join", map[string]string{"code": pairing.Code})
	if recorder.Code != http.StatusOK {
		t.Fatalf("join: %d %s", recorder.Code, recorder.Body.String())
	}
	var joined contract.AdminCascadeJoinResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &joined); err != nil {
		t.Fatal(err)
	}
	if joined.Peer != "parent" || joined.ParentInstanceID != "parent-node" || joined.Bound != 1 ||
		len(joined.Skipped) != 1 || joined.Skipped[0] != "pypi/intranet" || joined.RestartRequired || joined.InsecureHTTPAllowed {
		t.Fatalf("join = %+v", joined)
	}
	applied := child.Settings()
	if !applied.Enabled || applied.Token == "" || applied.Token == decoded.Token ||
		len(applied.Peers) != 1 || applied.Peers[0].Token != decoded.Token || applied.Peers[0].URL != relayServer.URL {
		t.Fatalf("child settings = %+v", applied)
	}
	for _, item := range child.registry.List() {
		if item.Name == "npmjs" && item.Via != "parent" {
			t.Fatalf("npmjs via = %q, want parent", item.Via)
		}
	}
	document, _ := os.ReadFile(child.path)
	if !strings.Contains(string(document), `name = "parent"`) {
		t.Fatalf("child config.toml lacks the parent:\n%s", document)
	}

	// Joining again with the same code keeps one parent.
	if recorder := child.post(t, "/cascade/join", map[string]string{"code": pairing.Code}); recorder.Code != http.StatusOK || len(child.Settings().Peers) != 1 {
		t.Fatalf("rejoin: %d %s peers=%d", recorder.Code, recorder.Body.String(), len(child.Settings().Peers))
	}
}

func TestCascadeJoinRefusesCodesItCannotUse(t *testing.T) {
	parent := newPairingNode(t, "parent-node", db.UpstreamRecord{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1})
	relayServer := parent.serveRelay(t)
	recorder := parent.post(t, "/cascade/pairing", map[string]string{"url": relayServer.URL})
	var pairing contract.AdminCascadePairingResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &pairing)
	decoded, _ := cascade.DecodePairingCode(pairing.Code)

	child := newPairingNode(t, "child-node", db.UpstreamRecord{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1})
	for name, test := range map[string]struct {
		code   string
		status int
		want   string
	}{
		"garbage":     {code: "hello", status: http.StatusBadRequest, want: "INVALID_PAIRING_CODE"},
		"own code":    {code: cascade.EncodePairingCode(cascade.Pairing{URL: relayServer.URL, Token: decoded.Token, InstanceID: "child-node"}), status: http.StatusUnprocessableEntity, want: "CASCADE_SELF_PAIRING"},
		"wrong token": {code: cascade.EncodePairingCode(cascade.Pairing{URL: relayServer.URL, Token: "wrong-token-0123456789", InstanceID: "parent-node"}), status: http.StatusUnprocessableEntity, want: "CASCADE_PARENT_UNAUTHORIZED"},
		"not a relay": {code: cascade.EncodePairingCode(cascade.Pairing{URL: relayServer.URL + "/elsewhere", Token: decoded.Token, InstanceID: "parent-node"}), status: http.StatusUnprocessableEntity, want: "CASCADE_PARENT_NOT_RELAY"},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := child.post(t, "/cascade/join", map[string]string{"code": test.code})
			if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), test.want) {
				t.Fatalf("join = %d %s, want %d %s", recorder.Code, recorder.Body.String(), test.status, test.want)
			}
			if child.Settings().Enabled {
				t.Fatal("a refused join must not change the configuration")
			}
		})
	}
}
