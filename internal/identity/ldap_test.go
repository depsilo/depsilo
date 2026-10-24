package identity

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/config"
	ber "github.com/go-asn1-ber/asn1-ber"
	"github.com/go-ldap/ldap/v3"
)

// A wire-level directory fixture exercises real LDAP bind/search and TLS. No
// production authentication decision is replaced by a test hook.
type fakeLDAP struct {
	url, ca                 string
	tlsConfig               *tls.Config
	mu                      sync.Mutex
	groups                  []string
	userDN, subject, defect string
	binds, clearBinds       atomic.Int64
}

const ldapFixtureBase = "ou=people,dc=example"
const ldapFixtureGroup = "cn=admins,ou=groups,dc=example"

func newFakeLDAP(t *testing.T, startTLS bool) *fakeLDAP {
	t.Helper()
	certServer := httptest.NewTLSServer(nil)
	cert := certServer.TLS.Certificates[0]
	certServer.Close()
	f := &fakeLDAP{ca: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})), tlsConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}, groups: []string{ldapFixtureGroup}, userDN: "uid=alice," + ldapFixtureBase, subject: "75dcb3fe-40f8-4bf5-a714-5551a135b839"}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if startTLS {
		f.url = "ldap://" + listener.Addr().String()
	} else {
		f.url = "ldaps://" + listener.Addr().String()
		listener = tls.NewListener(listener, f.tlsConfig)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go f.serve(connection, !startTLS)
		}
	}()
	return f
}

func ldapEnvelope(id int64, response *ber.Packet) []byte {
	p := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
	p.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, id, ""))
	p.AppendChild(response)
	return p.Bytes()
}

func ldapResult(id int64, tag ber.Tag, code uint16) []byte {
	p := ber.Encode(ber.ClassApplication, ber.TypeConstructed, tag, nil, "")
	p.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, int64(code), ""))
	p.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
	p.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
	return ldapEnvelope(id, p)
}

func ldapEntryPacket(id int64, dn string, attrs map[string][]string) []byte {
	p := ber.Encode(ber.ClassApplication, ber.TypeConstructed, ldap.ApplicationSearchResultEntry, nil, "")
	p.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, dn, ""))
	attributes := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
	for name, values := range attrs {
		attribute := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
		attribute.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, name, ""))
		set := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "")
		for _, value := range values {
			set.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, value, ""))
		}
		attribute.AppendChild(set)
		attributes.AppendChild(attribute)
	}
	p.AppendChild(attributes)
	return ldapEnvelope(id, p)
}

func (f *fakeLDAP) serve(connection net.Conn, encrypted bool) {
	defer func() { _ = connection.Close() }()
	_ = connection.SetDeadline(time.Now().Add(20 * time.Second))
	userBound := false
	for {
		packet, err := ber.ReadPacket(connection)
		if err != nil || len(packet.Children) < 2 {
			return
		}
		id, ok := packet.Children[0].Value.(int64)
		if !ok {
			return
		}
		request := packet.Children[1]
		switch request.Tag {
		case ldap.ApplicationExtendedRequest:
			if encrypted {
				return
			}
			if _, err := connection.Write(ldapResult(id, ldap.ApplicationExtendedResponse, ldap.LDAPResultSuccess)); err != nil {
				return
			}
			secure := tls.Server(connection, f.tlsConfig)
			if secure.Handshake() != nil {
				return
			}
			connection = secure
			encrypted = true
		case ldap.ApplicationBindRequest:
			f.binds.Add(1)
			if !encrypted {
				f.clearBinds.Add(1)
			}
			if len(request.Children) != 3 {
				return
			}
			dn, password := request.Children[1].Data.String(), request.Children[2].Data.String()
			f.mu.Lock()
			userDN := f.userDN
			f.mu.Unlock()
			code := uint16(ldap.LDAPResultInvalidCredentials)
			if dn == "cn=reader,dc=example" && password == "service-secret" {
				code = ldap.LDAPResultSuccess
			}
			if dn == userDN && password == "user-secret" {
				code = ldap.LDAPResultSuccess
				userBound = true
			}
			if _, err := connection.Write(ldapResult(id, ldap.ApplicationBindResponse, code)); err != nil {
				return
			}
		case ldap.ApplicationSearchRequest:
			if len(request.Children) < 8 {
				return
			}
			base := request.Children[0].Data.String()
			scope, _ := request.Children[1].Value.(int64)
			filter, _ := ldap.DecompileFilter(request.Children[6])
			f.mu.Lock()
			userDN, subject, defect := f.userDN, f.subject, f.defect
			groups := append([]string{}, f.groups...)
			f.mu.Unlock()
			if base == ldapFixtureBase && scope == ldap.ScopeBaseObject {
				if _, err := connection.Write(ldapEntryPacket(id, base, map[string][]string{"objectClass": {"organizationalUnit"}})); err != nil {
					return
				}
			} else if base == ldapFixtureBase && filter == "(uid=alice)" || base == userDN && scope == ldap.ScopeBaseObject {
				attrs := map[string][]string{"entryUUID": {subject}, "displayName": {"Directory Alice"}, "memberOf": groups}
				switch defect {
				case "missing subject":
					delete(attrs, "entryUUID")
				case "replaced subject":
					if userBound {
						attrs["entryUUID"] = []string{"52d8424c-a408-42cb-8a8e-1c67db52f404"}
					}
				case "large response":
					attrs["displayName"] = []string{strings.Repeat("x", maxLDAPResponseBytes+100)}
				}
				if _, err := connection.Write(ldapEntryPacket(id, userDN, attrs)); err != nil {
					return
				}
				if defect == "ambiguous" {
					if _, err := connection.Write(ldapEntryPacket(id, "uid=other,"+ldapFixtureBase, attrs)); err != nil {
						return
					}
				}
			}
			if _, err := connection.Write(ldapResult(id, ldap.ApplicationSearchResultDone, ldap.LDAPResultSuccess)); err != nil {
				return
			}
		case ldap.ApplicationUnbindRequest:
			return
		default:
			return
		}
	}
}

func (f *fakeLDAP) source() config.AuthenticationSourceConfig {
	return config.AuthenticationSourceConfig{Protocol: "ldap", OIDCConfig: config.OIDCConfig{Enabled: true, DisplayName: "Directory", AdminGroups: []string{ldapFixtureGroup}, ReadonlyGroups: []string{"cn=readers,ou=groups,dc=example"}, SessionTTL: 30 * time.Minute}, LDAP: &config.LDAPConfig{URL: f.url, BindDN: "cn=reader,dc=example", BindPassword: "service-secret", UserBaseDN: ldapFixtureBase, UserFilter: "(uid={username})", SubjectAttribute: "entryUUID", DisplayNameAttribute: "displayName", GroupsAttribute: "memberOf", CACertPEM: f.ca}}
}

func TestLDAPTLSAuthenticationChoiceAndStableIdentity(t *testing.T) {
	for _, startTLS := range []bool{false, true} {
		t.Run(map[bool]string{false: "LDAPS", true: "StartTLS"}[startTLS], func(t *testing.T) {
			_, _, database := testManager(t)
			directory := newFakeLDAP(t, startTLS)
			auth := config.AuthConfig{Sources: map[string]config.AuthenticationSourceConfig{"directory": directory.source()}}
			registry, err := NewRegistry(database, auth)
			if err != nil {
				t.Fatal(err)
			}
			m := registry.Manager("directory")
			if m.Protocol() != "ldap" || !strings.HasPrefix(m.Config().Issuer, "urn:depsilo:ldap:") {
				t.Fatal("directory authority lost")
			}
			if err := m.CheckLDAP(context.Background()); err != nil {
				t.Fatal(err)
			}
			result, err := m.LDAPAuthenticate(context.Background(), "login", "/admin/cache", "alice", "user-secret", 0, 0)
			if err != nil || result.Mode != "choice" || result.Choice == nil {
				t.Fatalf("new identity skipped explicit choice: %v %+v", err, result)
			}
			assertAccountCounts(t, database, 0, 0, 0)
			login, err := m.ResolveChoice(context.Background(), result.ChoiceKey, "create", "", "")
			if err != nil || login.User.PermissionSource != "ldap" || login.Session.Role != "admin" {
				t.Fatalf("directory managed login: %v %+v", err, login)
			}
			directory.mu.Lock()
			directory.userDN = "uid=renamed," + ldapFixtureBase
			directory.mu.Unlock()
			next, err := m.LDAPAuthenticate(context.Background(), "login", "/admin", "alice", "user-secret", 0, 0)
			if err != nil || next.User.ID != login.User.ID {
				t.Fatalf("DN rename changed UUID identity: %v", err)
			}
			cfg := auth.Sources["directory"]
			cfg.DisplayName = "Renamed directory"
			auth.Sources["directory"] = cfg
			blank := next.Session
			blank.ID, blank.ConfigFingerprint = randomToken(), ""
			if err := database.Create(&blank).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := NewRegistry(database, auth); err != nil {
				t.Fatal(err)
			}
			assertSessionRevoked(t, database, next.Session.ID, false)
			assertSessionRevoked(t, database, blank.ID, true)
			cfg.LDAP.BindPassword = "rotated"
			auth.Sources["directory"] = cfg
			if _, err := NewRegistry(database, auth); err != nil {
				t.Fatal(err)
			}
			assertSessionRevoked(t, database, next.Session.ID, true)
			if directory.clearBinds.Load() != 0 {
				t.Fatal("password sent before TLS")
			}
		})
	}
}

func TestLDAPRejectsInvalidProofAndUnsafeDirectoryResults(t *testing.T) {
	for _, scenario := range []struct {
		name, username, password, defect string
		expected                         error
	}{
		{"empty password", "alice", "", "", ErrLDAPCredentials},
		{"wrong password", "alice", "wrong", "", ErrLDAPCredentials},
		{"unknown user", "unknown", "user-secret", "", ErrLDAPCredentials},
		{"filter injection", "*)(uid=*)", "user-secret", "", ErrLDAPCredentials},
		{"ambiguous user", "alice", "user-secret", "ambiguous", ErrLDAPCredentials},
		{"missing UUID", "alice", "user-secret", "missing subject", ErrLDAPUnavailable},
		{"replaced entry", "alice", "user-secret", "replaced subject", ErrLDAPUnavailable},
		{"response budget", "alice", "user-secret", "large response", ErrLDAPUnavailable},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, _, database := testManager(t)
			directory := newFakeLDAP(t, false)
			directory.defect = scenario.defect
			registry, err := NewRegistry(database, config.AuthConfig{Sources: map[string]config.AuthenticationSourceConfig{"directory": directory.source()}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = registry.Manager("directory").LDAPAuthenticate(context.Background(), "login", "/admin", scenario.username, scenario.password, 0, 0)
			if !errors.Is(err, scenario.expected) {
				t.Fatalf("got %v want %v", err, scenario.expected)
			}
			assertAccountCounts(t, database, 0, 0, 0)
			if scenario.password == "" && directory.binds.Load() != 0 {
				t.Fatal("empty user password reached directory")
			}
		})
	}
}

func TestLDAPLocalBindingTrialAndObservedDemotion(t *testing.T) {
	_, _, database := testManager(t)
	directory := newFakeLDAP(t, false)
	source := directory.source()
	registry, err := NewRegistry(database, config.AuthConfig{Sources: map[string]config.AuthenticationSourceConfig{"directory": source}})
	if err != nil {
		t.Fatal(err)
	}
	m := registry.Manager("directory")
	local := createLocal(t, database, "local-admin", "admin")
	linked, err := m.LDAPAuthenticate(context.Background(), "link", "/admin/account", "alice", "user-secret", local.ID, local.CredentialVersion)
	if err != nil || linked.Mode != "link" || linked.User.ID != local.ID {
		t.Fatalf("local directory binding: %v", err)
	}
	login, err := m.LDAPAuthenticate(context.Background(), "login", "/admin", "alice", "user-secret", 0, 0)
	if err != nil || login.User.PermissionSource != "local" || login.User.ID != local.ID {
		t.Fatalf("binding changed local authority: %v", err)
	}
	trial, err := NewLDAPTrial("directory", source)
	if err != nil {
		t.Fatal(err)
	}
	directory.mu.Lock()
	directory.groups = []string{"cn=unmapped,ou=groups,dc=example"}
	directory.mu.Unlock()
	result, err := trial.LDAPAuthenticate(context.Background(), "test", "/admin/authentication?source=directory", "alice", "user-secret", 0, 0)
	if err != nil || result.Trial == nil || result.Trial.Outcome != "denied" || !result.Trial.Authenticated {
		t.Fatalf("trial mapping result: %v %+v", err, result)
	}
	assertSessionRevoked(t, database, login.Session.ID, false)
	_, err = m.LDAPAuthenticate(context.Background(), "login", "/admin", "alice", "user-secret", 0, 0)
	if !errors.Is(err, ErrDenied) {
		t.Fatal("unmapped directory admission allowed")
	}
	assertSessionRevoked(t, database, login.Session.ID, true)
	assertAccountCounts(t, database, 1, 1, 1)
	if err := registry.Unlink(context.Background(), local.ID, local.CredentialVersion, login.Session.IdentityID, "test-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.ProvePassword(context.Background(), local.ID, local.CredentialVersion, "test-password"); err != nil {
		t.Fatal("local recovery was lost")
	}
}

func TestLDAPCertificateValidationCannotBeDisabled(t *testing.T) {
	for _, startTLS := range []bool{false, true} {
		directory := newFakeLDAP(t, startTLS)
		source := directory.source()
		source.LDAP.CACertPEM = ""
		trial, err := NewLDAPTrial("directory", source)
		if err != nil {
			t.Fatal(err)
		}
		if err := trial.CheckLDAP(context.Background()); !errors.Is(err, ErrLDAPUnavailable) {
			t.Fatal("untrusted directory certificate accepted")
		}
		if directory.binds.Load() != 0 {
			t.Fatal("credentials sent to an unverified directory")
		}
	}
}

func TestLDAPObjectGUIDUsesStableBinaryIdentity(t *testing.T) {
	entry := ldap.NewEntry("cn=Person,dc=example", map[string][]string{"objectGUID": {string([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})}})
	subject, err := ldapSubject(entry, "objectGUID")
	if err != nil || subject != "guid:0102030405060708090a0b0c0d0e0f10" {
		t.Fatalf("binary identity lost: %v %s", err, subject)
	}
	entry = ldap.NewEntry("cn=Person,dc=example", map[string][]string{"objectGUID": {"short"}})
	if _, err := ldapSubject(entry, "objectGUID"); err == nil {
		t.Fatal("invalid binary identifier accepted")
	}
	entry = ldap.NewEntry("cn=Person,dc=example", map[string][]string{"objectGUID": {string(make([]byte, 16))}})
	if _, err := ldapSubject(entry, "objectGUID"); err == nil {
		t.Fatal("zero binary identifier accepted")
	}
}
