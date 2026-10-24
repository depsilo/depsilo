package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
)

var (
	ErrUnavailable = errors.New("OIDC_UNAVAILABLE")
	ErrExpired     = errors.New("LOGIN_EXPIRED")
	ErrDenied      = errors.New("ACCESS_DENIED")
	ErrBound       = errors.New("IDENTITY_ALREADY_BOUND")
	ErrRateLimited = errors.New("RATE_LIMITED")
	ErrProof       = errors.New("REAUTHENTICATION_FAILED")
	ErrSelection   = errors.New("PROVIDER_REQUIRED")
)

const ProviderID = "oidc"
const flowTTL = 5 * time.Minute
const completeTTL = time.Minute
const maxPending = 1024

type transaction struct {
	Browser, Verifier, Nonce, Mode, ReturnTo string
	UserID                                   uint
	CredentialVersion                        uint64
	ExpiresAt                                time.Time
}

type completion struct {
	Result    Result
	Error     error
	ExpiresAt time.Time
}

// Manager has no network-dependent startup or background goroutines. The
// bounded pending maps contain no access/refresh/ID tokens. Only opaque cookies
// carry flow references; the provider's tokens live for the callback request.
type Manager struct {
	db                *gorm.DB
	id                string
	protocol          string
	ldap              *config.LDAPConfig
	cfg               config.OIDCConfig
	fingerprint       string
	legacyFingerprint string
	client            *http.Client
	remote            providerState
	mu                sync.Mutex
	writeMu           *sync.Mutex
	pending           map[string]transaction
	completed         map[string]completion
	choices           map[string]choiceTransaction
	trial             bool
}

// New retains the legacy source's configuration and session compatibility.
// It only revokes legacy sessions; NewRegistry owns removal across all sources.
func New(database *gorm.DB, cfg config.OIDCConfig) (*Manager, error) {
	m, err := newManager(database, ProviderID, cfg, &sync.Mutex{})
	if err != nil {
		return nil, err
	}
	if database != nil {
		if err := database.Transaction(func(tx *gorm.DB) error {
			return m.reconcileSessions(tx, time.Now().UTC())
		}); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func newManager(database *gorm.DB, id string, cfg config.OIDCConfig, writes *sync.Mutex) (*Manager, error) {
	if err := config.ValidateSourceOIDCConfig(id, cfg); err != nil {
		return nil, err
	}
	m := &Manager{db: database, id: id, cfg: cfg, writeMu: writes, fingerprint: sessionFingerprint(id, cfg), legacyFingerprint: legacySessionFingerprint(id, cfg), pending: make(map[string]transaction), completed: make(map[string]completion), choices: make(map[string]choiceTransaction)}
	if cfg.Enabled {
		m.client = providerClient(cfg.Issuer)
	}
	return m, nil
}

// legacySessionFingerprint must remain byte-for-byte compatible with the old
// algorithm, including the legacy source's missing ID prefix and nil slices.
func legacySessionFingerprint(id string, cfg config.OIDCConfig) string {
	raw, _ := json.Marshal(struct {
		Config config.OIDCConfig
		Secret string
	}{cfg, cfg.ClientSecret})
	if id != ProviderID {
		raw = append([]byte(id+"\x00"), raw...)
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func sessionFingerprint(id string, cfg config.OIDCConfig) string {
	// Presentation changes are not authentication changes. Keep every other
	// field (including the secret) and the stable source ID in the security
	// boundary. Version the input, retaining the existing 64-character column.
	cfg.DisplayName = ""
	raw, _ := json.Marshal(struct {
		Config config.OIDCConfig
		Secret string
	}{cfg, cfg.ClientSecret})
	hash := sha256.Sum256(append([]byte("depsilo/oidc-session/v2\x00"+id+"\x00"), raw...))
	return hex.EncodeToString(hash[:])
}

// reconcileSessions runs inside the caller's startup transaction. Only an
// exact old fingerprint for the current full configuration proves continuity;
// upgrading while also renaming an old source therefore fails closed.
func (m *Manager) reconcileSessions(database *gorm.DB, now time.Time) error {
	identities := database.Model(&db.ExternalIdentity{}).Select("id").Where("provider = ?", m.id)
	q := database.Model(&db.IdentitySession{}).Where("revoked_at IS NULL AND identity_id IN (?)", identities)
	if m.Enabled() {
		matching := database.Model(&db.ExternalIdentity{}).Select("id").Where("provider = ? AND issuer = ?", m.id, m.cfg.Issuer)
		if m.legacyFingerprint != "" {
			if err := database.Model(&db.IdentitySession{}).
				Where("revoked_at IS NULL AND expires_at > ? AND config_fingerprint = ? AND identity_id IN (?)", now, m.legacyFingerprint, matching).
				Update("config_fingerprint", m.fingerprint).Error; err != nil {
				return err
			}
		}
		q = q.Where("config_fingerprint <> ? OR identity_id NOT IN (?)", m.fingerprint, matching)
	}
	return q.Update("revoked_at", now).Error
}

func (m *Manager) ID() string {
	if m == nil {
		return ""
	}
	return m.id
}

func (m *Manager) Protocol() string {
	if m != nil && m.protocol != "" {
		return m.protocol
	}
	return "oidc"
}

func (m *Manager) Config() config.OIDCConfig {
	if m == nil {
		return config.OIDCConfig{}
	}
	return m.cfg
}
func (m *Manager) Enabled() bool { return m != nil && m.cfg.Enabled && (m.db != nil || m.trial) }

func (m *Manager) ConfigFingerprint() string {
	if m == nil {
		return ""
	}
	return m.fingerprint
}

type StartResult struct{ AuthorizationURL, BrowserToken string }
type Result struct {
	Mode, ReturnTo string
	User           db.User
	Session        db.IdentitySession
	Choice         *ChoiceInfo
	ChoiceKey      string
	candidate      *authenticatedIdentity
	Trial          *TrialResult
}

// SafeReturnTo validates the navigation path separately from query/fragment
// data, so a legitimate escaped search survives the round trip unchanged.
// Browser path normalization must not escape Admin or create an auth loop.
func SafeReturnTo(target string) string {
	if len(target) > 2048 || strings.TrimSpace(target) != target || strings.IndexFunc(target, unicode.IsControl) >= 0 {
		return "/admin"
	}
	u, err := url.Parse(target)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Opaque != "" {
		return "/admin"
	}
	if u.Path != "/admin" && !strings.HasPrefix(u.Path, "/admin/") {
		return "/admin"
	}
	// Reject separators hidden in escapes, double encoding and dot segments.
	// Ordinary escaped characters (e.g. %63ache) cannot change this boundary.
	escapedPath := strings.ToLower(u.EscapedPath())
	if strings.Contains(escapedPath, "%2f") || strings.Contains(escapedPath, "%5c") || strings.ContainsAny(u.Path, "\\%") || strings.Contains(u.Path, "//") || strings.IndexFunc(u.Path, unicode.IsControl) >= 0 {
		return "/admin"
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return "/admin"
		}
	}
	navigationPath := strings.ToLower(u.Path)
	if navigationPath == "/admin/login" || strings.HasPrefix(navigationPath, "/admin/login/") || navigationPath == "/admin/auth" || strings.HasPrefix(navigationPath, "/admin/auth/") {
		return "/admin"
	}
	query, err := url.QueryUnescape(u.RawQuery)
	if err != nil || strings.IndexFunc(query, unicode.IsControl) >= 0 || strings.IndexFunc(u.Fragment, unicode.IsControl) >= 0 {
		return "/admin"
	}
	return target
}

func (m *Manager) Begin(ctx context.Context, mode, returnTo string, userID uint, version uint64) (StartResult, error) {
	if !m.Enabled() || m.Protocol() != "oidc" {
		return StartResult{}, ErrUnavailable
	}
	if (m.trial && mode != "test") || (!m.trial && mode != "login" && mode != "link") {
		return StartResult{}, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	_, oauth, err := m.provider(ctx)
	if err != nil {
		return StartResult{}, err
	}
	state, browser, nonce := randomToken(), randomToken(), randomToken()
	txn := transaction{Browser: browser, Verifier: oauth2.GenerateVerifier(), Nonce: nonce, Mode: mode, ReturnTo: SafeReturnTo(returnTo), UserID: userID, CredentialVersion: version, ExpiresAt: time.Now().Add(flowTTL)}
	if mode == "link" {
		txn.ReturnTo = "/admin/account"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	if len(m.pending) >= maxPending || len(m.completed) >= maxPending {
		return StartResult{}, ErrRateLimited
	}
	m.pending[state] = txn
	return StartResult{BrowserToken: browser, AuthorizationURL: oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(txn.Verifier), oauth2.SetAuthURLParam("prompt", "login"))}, nil
}

// Callback consumes the flow before performing exchange, so even concurrent
// browser retries cannot reuse a code or mutate an account twice. Invalid
// browser cookies cannot consume another browser's pending transaction.
func (m *Manager) Callback(ctx context.Context, state, browser, code, providerError string) (string, error) {
	if !m.Enabled() || m.Protocol() != "oidc" {
		return "", ErrUnavailable
	}
	m.mu.Lock()
	m.pruneLocked()
	txn, ok := m.pending[state]
	if !ok || browser == "" || subtle.ConstantTimeCompare([]byte(txn.Browser), []byte(browser)) != 1 {
		m.mu.Unlock()
		return "", ErrExpired
	}
	delete(m.pending, state)
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var result Result
	var err error
	if providerError != "" || code == "" || len(code) > 8192 {
		err = ErrDenied
		if m.trial && providerError == "access_denied" {
			// The provider may report either user cancellation or its own
			// admission policy here. Neither proves an authenticated identity
			// nor an unavailable service.
			err = errAuthenticationNotCompleted
		}
	} else {
		var auth authenticatedIdentity
		auth, err = m.authenticate(ctx, txn, code)
		if !m.trial && errors.Is(err, ErrDenied) && auth.Subject != "" {
			if revokeErr := m.revokeSubject(ctx, auth.Subject); revokeErr != nil {
				err = ErrUnavailable
			}
		}
		if m.trial {
			result = trialResult(auth, err, txn.ReturnTo)
			err = nil
		} else if err == nil {
			result, err = m.apply(ctx, txn, auth)
		}
	}
	if m.trial && result.Trial == nil {
		result = trialResult(authenticatedIdentity{}, err, txn.ReturnTo)
		err = nil
	}
	if err != nil {
		m.recordDenial(ctx, txn.UserID, err)
	}
	key := randomToken()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	if len(m.completed) >= maxPending {
		return "", ErrRateLimited
	}
	m.completed[key] = completion{Result: result, Error: err, ExpiresAt: time.Now().Add(completeTTL)}
	return key, nil
}

func (m *Manager) Complete(ctx context.Context, key string) (Result, error) {
	if !m.Enabled() {
		return Result{}, ErrUnavailable
	}
	m.mu.Lock()
	m.pruneLocked()
	c, ok := m.completed[key]
	delete(m.completed, key)
	m.mu.Unlock()
	if !ok {
		return Result{}, ErrExpired
	}
	if c.Error != nil {
		return Result{}, c.Error
	}
	if c.Result.Mode == "test" {
		return c.Result, nil
	}
	if c.Result.Mode == "choice" {
		return m.prepareChoice(c.Result)
	}
	var user db.User
	if err := m.db.WithContext(ctx).First(&user, c.Result.User.ID).Error; err != nil || !user.Enabled || user.CredentialVersion != c.Result.User.CredentialVersion {
		return Result{}, ErrDenied
	}
	if c.Result.Mode == "login" {
		var session db.IdentitySession
		if err := m.db.WithContext(ctx).Where("id = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ? AND config_fingerprint = ?", c.Result.Session.ID, user.ID, time.Now().UTC(), m.fingerprint).First(&session).Error; err != nil {
			return Result{}, ErrDenied
		}
		var linked db.ExternalIdentity
		if err := m.db.WithContext(ctx).Where("id = ? AND user_id = ? AND provider = ? AND issuer = ?", session.IdentityID, user.ID, m.id, m.cfg.Issuer).First(&linked).Error; err != nil {
			return Result{}, ErrDenied
		}
		c.Result.Session = session
	}
	c.Result.User = user
	return c.Result, nil
}

func (m *Manager) pruneLocked() {
	now := time.Now()
	for key, choice := range m.choices {
		if !now.Before(choice.Info.ExpiresAt) {
			delete(m.choices, key)
		}
	}
	for k, t := range m.pending {
		if !now.Before(t.ExpiresAt) {
			delete(m.pending, k)
		}
	}
	for k, c := range m.completed {
		if !now.Before(c.ExpiresAt) {
			delete(m.completed, k)
		}
	}
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
