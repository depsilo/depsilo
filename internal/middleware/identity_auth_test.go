package middleware

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

func newIdentityAuthFixture(t *testing.T, source, localRole, mappedRole string) (*gorm.DB, db.User, db.ExternalIdentity, db.IdentitySession, string) {
	t.Helper()
	database := newAuthTestDB(t)
	if err := database.AutoMigrate(&db.ExternalIdentity{}, &db.IdentitySession{}); err != nil {
		t.Fatal(err)
	}
	user := createAuthTestUser(t, database, "operator", localRole, true)
	user.PermissionSource = source
	if err := database.Model(&user).Update("permission_source", source).Error; err != nil {
		t.Fatal(err)
	}
	identity := db.ExternalIdentity{UserID: user.ID, Provider: "oidc", Issuer: "https://identity.example", Subject: "stable-subject"}
	if err := database.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}
	session := db.IdentitySession{ID: "test-session", UserID: user.ID, IdentityID: identity.ID, Role: mappedRole,
		ConfigFingerprint: "test-configuration", ExpiresAt: time.Now().Add(30 * time.Minute)}
	if err := database.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	token, err := GenerateIdentityJWT(authTestSecret, user, session, 168*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return database, user, identity, session, token
}

func TestIdentitySessionsRespectBothRoleAuthorities(t *testing.T) {
	for _, test := range []struct{ source, local, mapped, effective, provider, protocol string }{
		{"local", "admin", "readonly", "readonly", "oidc", "oidc"},
		{"local", "readonly", "admin", "readonly", "company", "oidc"},
		{"local", "admin", "admin", "admin", "community", "oidc"},
		{"oidc", "admin", "admin", "admin", "oidc", "oidc"},
		{"oidc", "readonly", "readonly", "readonly", "company", "oidc"},
		{"local", "admin", "readonly", "readonly", "directory", "ldap"},
		{"local", "readonly", "admin", "readonly", "directory", "ldap"},
		{"ldap", "admin", "admin", "admin", "directory", "ldap"},
		{"ldap", "readonly", "readonly", "readonly", "directory", "ldap"},
	} {
		t.Run(test.source+"/"+test.local+"/"+test.mapped, func(t *testing.T) {
			database, _, linked, session, token := newIdentityAuthFixture(t, test.source, test.local, test.mapped)
			if err := database.Model(&linked).Updates(map[string]any{"provider": test.provider, "issuer": map[string]string{"oidc": "https://identity.example", "ldap": db.LDAPIdentityIssuerPrefix + "test-directory"}[test.protocol]}).Error; err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.Use(Authenticate(authTestSecret, database))
			router.GET("/me", func(c *gin.Context) { p, _ := PrincipalFromContext(c); c.JSON(http.StatusOK, p) })
			router.POST("/business", WriteRequired(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			router.POST("/credentials", CredentialManagementRequired(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			response := authRequest(router, http.MethodGet, "/me", token)
			var principal Principal
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &principal) != nil {
				t.Fatalf("principal: %d %s", response.Code, response.Body.String())
			}
			if principal.Role != test.effective || principal.PermissionSource != test.source || principal.LoginMethod != test.protocol || principal.CanManageCredentials {
				t.Fatalf("unexpected principal: %+v", principal)
			}
			if principal.CurrentIdentityID != linked.ID || principal.CurrentProviderID != test.provider {
				t.Fatal("principal lost actual login identity")
			}
			wantWrite := http.StatusForbidden
			if test.effective == "admin" {
				wantWrite = http.StatusNoContent
			}
			if got := authRequest(router, http.MethodPost, "/business", token).Code; got != wantWrite {
				t.Fatalf("business status %d want %d", got, wantWrite)
			}
			if got := authRequest(router, http.MethodPost, "/credentials", token).Code; got != http.StatusForbidden {
				t.Fatalf("credential status %d", got)
			}
			claims := &Claims{}
			if _, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return []byte(authTestSecret), nil }); err != nil {
				t.Fatal(err)
			}
			if claims.SessionID != session.ID || claims.ExpiresAt.Time.After(session.ExpiresAt) {
				t.Fatal("JWT lost absolute session binding")
			}
		})
	}
}

func TestIdentitySessionsRejectRevocationAndMissingBindings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*gorm.DB, db.User, db.ExternalIdentity, db.IdentitySession) error
	}{
		{"revoked", func(d *gorm.DB, u db.User, i db.ExternalIdentity, s db.IdentitySession) error {
			return d.Model(&s).Update("revoked_at", time.Now()).Error
		}},
		{"expired", func(d *gorm.DB, u db.User, i db.ExternalIdentity, s db.IdentitySession) error {
			return d.Model(&s).Update("expires_at", time.Now().Add(-time.Second)).Error
		}},
		{"identity unlinked", func(d *gorm.DB, u db.User, i db.ExternalIdentity, s db.IdentitySession) error {
			return d.Delete(&i).Error
		}},
		{"identity reassigned", func(d *gorm.DB, u db.User, i db.ExternalIdentity, s db.IdentitySession) error {
			return d.Model(&i).Update("user_id", u.ID+1).Error
		}},
		{"identity source missing", func(d *gorm.DB, u db.User, i db.ExternalIdentity, s db.IdentitySession) error {
			return d.Model(&i).Update("provider", "").Error
		}},
		{"wrong protocol authority", func(d *gorm.DB, u db.User, i db.ExternalIdentity, s db.IdentitySession) error {
			return d.Model(&i).Update("issuer", db.LDAPIdentityIssuerPrefix+"another-authority").Error
		}},
		{"owner disabled", func(d *gorm.DB, u db.User, i db.ExternalIdentity, s db.IdentitySession) error {
			return d.Model(&u).Update("enabled", false).Error
		}},
		{"credential changed", func(d *gorm.DB, u db.User, i db.ExternalIdentity, s db.IdentitySession) error {
			return d.Model(&u).Update("credential_version", u.CredentialVersion+1).Error
		}},
		{"session missing", func(d *gorm.DB, u db.User, i db.ExternalIdentity, s db.IdentitySession) error {
			return d.Delete(&s).Error
		}},
		{"unsupported session role", func(d *gorm.DB, u db.User, i db.ExternalIdentity, s db.IdentitySession) error {
			return d.Model(&s).Update("role", "owner").Error
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, user, identity, session, token := newIdentityAuthFixture(t, "oidc", "admin", "admin")
			if err := test.mutate(database, user, identity, session); err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.GET("/protected", Authenticate(authTestSecret, database), func(c *gin.Context) { c.Status(http.StatusOK) })
			assertGenericUnauthorized(t, authRequest(router, http.MethodGet, "/protected", token), token)
		})
	}
}

func TestManagedIdentityCannotUseLegacyJWTOrAPIToken(t *testing.T) {
	database, user, _, _, _ := newIdentityAuthFixture(t, "oidc", "admin", "admin")
	if _, err := GenerateJWT(authTestSecret, user, time.Hour); err == nil {
		t.Fatal("managed identity received an unbound JWT")
	}
	// Simulate a pre-existing/signed token missing the session, as a refresh regression would.
	legacyUser := user
	legacyUser.PermissionSource = "local"
	legacy, err := GenerateJWT(authTestSecret, legacyUser, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	createAuthTestAPIToken(t, database, user.ID, "old-managed-token", "readwrite")
	router := gin.New()
	router.GET("/protected", Authenticate(authTestSecret, database), func(c *gin.Context) { c.Status(http.StatusOK) })
	for _, token := range []string{legacy, "old-managed-token"} {
		assertGenericUnauthorized(t, authRequest(router, http.MethodGet, "/protected", token), token)
	}
}
