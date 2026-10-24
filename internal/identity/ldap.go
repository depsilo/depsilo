package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"depsilo/internal/config"
	"github.com/go-ldap/ldap/v3"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrLDAPUnavailable = errors.New("LDAP_UNAVAILABLE")
	ErrLDAPCredentials = errors.New("LDAP_AUTHENTICATION_FAILED")
)

const maxLDAPResponseBytes = 1 << 20

// Each connection performs only a few small bind/search exchanges. Bound the
// entire received stream before BER decoding; the library's default per-packet
// ceiling is 2 GiB. This also bounds indefinite-length constructed responses.
type boundedLDAPConn struct {
	net.Conn
	remaining int64
}

func (c *boundedLDAPConn) Read(p []byte) (int, error) {
	if c.remaining <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.Conn.Read(p)
	c.remaining -= int64(n)
	return n, err
}

func newSourceManager(database *gorm.DB, id string, source config.AuthenticationSourceConfig, writes *sync.Mutex) (*Manager, error) {
	if source.Protocol != "ldap" {
		return newManager(database, id, source.OIDCConfig, writes)
	}
	if err := config.ValidateSourceLDAPConfig(id, source); err != nil {
		return nil, err
	}
	directory := *source.LDAP
	cfg := source.OIDCConfig
	cfg.Issuer = config.LDAPAuthority(id, directory)
	security := cfg
	security.DisplayName = ""
	raw, _ := json.Marshal(struct {
		ID     string
		Config config.OIDCConfig
		LDAP   config.LDAPConfig
		Secret string
	}{id, security, directory, directory.BindPassword})
	hash := sha256.Sum256(append([]byte("depsilo/ldap-session/v1\x00"), raw...))
	return &Manager{db: database, id: id, protocol: "ldap", cfg: cfg, ldap: &directory, fingerprint: hex.EncodeToString(hash[:]), writeMu: writes, pending: make(map[string]transaction), completed: make(map[string]completion), choices: make(map[string]choiceTransaction)}, nil
}

func NewLDAPTrial(id string, source config.AuthenticationSourceConfig) (*Manager, error) {
	source.Enabled = true
	if source.Protocol != "ldap" {
		return nil, ErrLDAPUnavailable
	}
	m, err := newSourceManager(nil, id, source, &sync.Mutex{})
	if err != nil {
		return nil, err
	}
	m.trial = true
	return m, nil
}

// LDAPAuthenticate never retains either password. Unknown users, ambiguous
// searches and invalid credentials share one public failure. Account creation
// remains an explicit choice after directory authentication succeeds.
func (m *Manager) LDAPAuthenticate(ctx context.Context, mode, returnTo, username, password string, userID uint, version uint64) (Result, error) {
	if !m.Enabled() || m.Protocol() != "ldap" {
		return Result{}, ErrLDAPUnavailable
	}
	if m.trial && mode != "test" || !m.trial && mode != "login" && mode != "link" {
		return Result{}, ErrDenied
	}
	if username == "" || username != strings.TrimSpace(username) || len(username) > 255 || strings.ContainsFunc(username, unicode.IsControl) || password == "" || len(password) > 1024 || strings.ContainsRune(password, 0) {
		if m.trial {
			return trialResult(authenticatedIdentity{}, ErrLDAPCredentials, SafeReturnTo(returnTo)), nil
		}
		return Result{}, ErrLDAPCredentials
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	auth, err := m.authenticateLDAP(ctx, username, password)
	if m.trial {
		return trialResult(auth, err, SafeReturnTo(returnTo)), nil
	}
	if err != nil {
		if errors.Is(err, ErrDenied) && auth.Verified && auth.Subject != "" {
			if revokeErr := m.revokeSubject(ctx, auth.Subject); revokeErr != nil {
				return Result{}, ErrLDAPUnavailable
			}
		}
		m.recordDenial(ctx, userID, err)
		return Result{}, err
	}
	txn := transaction{Mode: mode, ReturnTo: SafeReturnTo(returnTo), UserID: userID, CredentialVersion: version}
	if mode == "link" {
		txn.ReturnTo = "/admin/account"
	}
	result, err := m.apply(ctx, txn, auth)
	if err == nil && result.Mode == "choice" {
		return m.prepareChoice(result)
	}
	return result, err
}

func (m *Manager) openLDAP(ctx context.Context) (*ldap.Conn, func(), error) {
	if m.ldap == nil {
		return nil, nil, ErrLDAPUnavailable
	}
	u, err := url.Parse(m.ldap.URL)
	if err != nil || u.Hostname() == "" {
		return nil, nil, ErrLDAPUnavailable
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if m.ldap.CACertPEM != "" && !pool.AppendCertsFromPEM([]byte(m.ldap.CACertPEM)) {
		return nil, nil, ErrLDAPUnavailable
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname(), RootCAs: pool}
	port := u.Port()
	if port == "" {
		if u.Scheme == "ldaps" {
			port = "636"
		} else {
			port = "389"
		}
	}
	address := net.JoinHostPort(u.Hostname(), port)
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	var raw net.Conn
	if u.Scheme == "ldaps" {
		raw, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", address)
	} else {
		raw, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, nil, ErrLDAPUnavailable
	}
	raw = &boundedLDAPConn{Conn: raw, remaining: maxLDAPResponseBytes}
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	}
	connection := ldap.NewConn(raw, u.Scheme == "ldaps")
	connection.SetTimeout(5 * time.Second)
	connection.Start()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	closeConnection := func() { stop(); _ = connection.Close() }
	if u.Scheme == "ldap" {
		if err := connection.StartTLS(tlsConfig); err != nil {
			closeConnection()
			return nil, nil, ErrLDAPUnavailable
		}
	}
	// Never use UnauthenticatedBind: empty-password simple bind can succeed as
	// an anonymous identity on directory servers.
	if m.ldap.BindPassword == "" || connection.Bind(m.ldap.BindDN, m.ldap.BindPassword) != nil {
		closeConnection()
		return nil, nil, ErrLDAPUnavailable
	}
	return connection, closeConnection, nil
}

func (m *Manager) CheckLDAP(ctx context.Context) error {
	if !m.Enabled() || m.Protocol() != "ldap" {
		return ErrLDAPUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	connection, closeConnection, err := m.openLDAP(ctx)
	if err != nil {
		return err
	}
	defer closeConnection()
	result, err := connection.Search(ldap.NewSearchRequest(m.ldap.UserBaseDN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 5, false, "(objectClass=*)", []string{"1.1"}, nil))
	if err != nil || len(result.Entries) != 1 || len(result.Referrals) != 0 {
		return ErrLDAPUnavailable
	}
	return nil
}

func (m *Manager) authenticateLDAP(ctx context.Context, username, password string) (authenticatedIdentity, error) {
	connection, closeConnection, err := m.openLDAP(ctx)
	if err != nil {
		return authenticatedIdentity{}, err
	}
	defer closeConnection()
	filter := strings.ReplaceAll(m.ldap.UserFilter, "{username}", ldap.EscapeFilter(username))
	attributes := []string{m.ldap.SubjectAttribute, m.ldap.DisplayNameAttribute, m.ldap.GroupsAttribute}
	result, err := connection.Search(ldap.NewSearchRequest(m.ldap.UserBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, 5, false, filter, attributes, nil))
	if ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
		return authenticatedIdentity{}, ErrLDAPCredentials
	}
	if err != nil || len(result.Referrals) != 0 {
		return authenticatedIdentity{}, ErrLDAPUnavailable
	}
	if len(result.Entries) != 1 {
		return authenticatedIdentity{}, ErrLDAPCredentials
	}
	entry := result.Entries[0]
	subject, err := ldapSubject(entry, m.ldap.SubjectAttribute)
	if err != nil {
		return authenticatedIdentity{}, err
	}
	if err := connection.Bind(entry.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return authenticatedIdentity{}, ErrLDAPCredentials
		}
		return authenticatedIdentity{}, ErrLDAPUnavailable
	}
	// Re-read as the service account after user proof, so group visibility is
	// consistent and a replaced entry between search and bind is rejected.
	if err := connection.Bind(m.ldap.BindDN, m.ldap.BindPassword); err != nil {
		return authenticatedIdentity{}, ErrLDAPUnavailable
	}
	current, err := connection.Search(ldap.NewSearchRequest(entry.DN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 5, false, "(objectClass=*)", attributes, nil))
	if err != nil || len(current.Entries) != 1 || len(current.Referrals) != 0 {
		return authenticatedIdentity{}, ErrLDAPUnavailable
	}
	entry = current.Entries[0]
	nowSubject, err := ldapSubject(entry, m.ldap.SubjectAttribute)
	if err != nil || subject != nowSubject {
		return authenticatedIdentity{}, ErrLDAPUnavailable
	}
	name := entry.GetEqualFoldAttributeValue(m.ldap.DisplayNameAttribute)
	if len(name) > 255 || strings.ContainsFunc(name, unicode.IsControl) || strings.TrimSpace(name) == "" {
		name = m.cfg.DisplayName
	}
	auth := authenticatedIdentity{Subject: subject, Name: name, Verified: true}
	groups := entry.GetEqualFoldAttributeValues(m.ldap.GroupsAttribute)
	if len(groups) > 256 {
		return auth, ErrDenied
	}
	for _, group := range groups {
		if len(group) > 4096 {
			return auth, ErrDenied
		}
	}
	auth.Role = m.ldapRole(groups)
	if auth.Role == "" {
		return auth, ErrDenied
	}
	return auth, nil
}

func ldapSubject(entry *ldap.Entry, attribute string) (string, error) {
	values := entry.GetEqualFoldRawAttributeValues(attribute)
	if len(values) != 1 {
		return "", ErrLDAPUnavailable
	}
	if strings.EqualFold(attribute, "objectGUID") {
		if len(values[0]) != 16 || bytes.Equal(values[0], make([]byte, 16)) {
			return "", ErrLDAPUnavailable
		}
		return "guid:" + hex.EncodeToString(values[0]), nil
	}
	parsed, err := uuid.Parse(string(values[0]))
	if err != nil || parsed == uuid.Nil {
		return "", ErrLDAPUnavailable
	}
	return "uuid:" + parsed.String(), nil
}

func (m *Manager) ldapRole(groups []string) string {
	matches := func(allowed []string) bool {
		for _, group := range groups {
			for _, want := range allowed {
				if group == want {
					return true
				}
				dn, err := ldap.ParseDN(group)
				other, otherErr := ldap.ParseDN(want)
				if err == nil && otherErr == nil && len(dn.RDNs) > 0 && dn.EqualFold(other) {
					return true
				}
			}
		}
		return false
	}
	if matches(m.cfg.AdminGroups) {
		return "admin"
	}
	if matches(m.cfg.ReadonlyGroups) {
		return "readonly"
	}
	return ""
}
