package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/identity"
	"depsilo/internal/middleware"
)

func authenticationImpactFixture(t *testing.T, principal middleware.Principal) (*gin.Engine, *gorm.DB, *identity.Registry) {
	t.Helper()
	_, database := newUserTestRouter(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	document := `[auth.sources.company]
protocol="oidc"
enabled=true
display_name="Company"
issuer="https://identity.example"
client_id="depsilo"
client_secret="saved-private-secret"
redirect_url="https://depsilo.example/api/v1/auth/oidc/company/callback"
admin_groups=["admins"]
`
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", strings.Repeat("x", 64))
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := identity.NewRegistry(database, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(path, cfg, zap.NewAtomicLevel())
	impact := NewAuthenticationSourceImpactHandler(store, database, registry)
	sources := NewAuthenticationSourceHandler(store)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(middleware.ContextKeyPrincipal, principal) })
	router.GET("/sources", AdminRoleRequired(), sources.List)
	localAdmin := router.Group("", AdminRoleRequired(), middleware.CredentialManagementRequired())
	localAdmin.GET("/sources/:id/impact", impact.Get)
	writes := localAdmin.Group("", middleware.WriteRequired())
	writes.DELETE("/sources/:id", sources.Delete)
	writes.POST("/sources/:id/restore", sources.Restore)
	return router, database, registry
}

func TestAuthenticationSourceImpactUsesRealCompleteCounts(t *testing.T) {
	router, database, registry := authenticationImpactFixture(t, middleware.Principal{Role: "admin", CanWrite: true, CanManageCredentials: true})
	now := time.Now().UTC()
	for i := 0; i < 24; i++ {
		source := "local"
		if i%2 == 0 {
			source = "oidc"
		}
		user := db.User{Username: fmt.Sprintf("operator-%02d", i), Role: "readonly", PermissionSource: source, Enabled: true, CredentialVersion: 1}
		if err := database.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
		binding := db.ExternalIdentity{UserID: user.ID, Provider: "company", Issuer: "https://identity.example", Subject: fmt.Sprintf("private-subject-%d", i), DisplayName: "private-identity-name"}
		if err := database.Create(&binding).Error; err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			// Although this invalid historical managed-account shape cannot be created
			// through Admin, it must not count as depending exclusively on this source.
			other := db.ExternalIdentity{UserID: user.ID, Provider: "other", Issuer: "https://other.example", Subject: "same-person"}
			if err := database.Create(&other).Error; err != nil {
				t.Fatal(err)
			}
		}
		session := db.IdentitySession{ID: fmt.Sprintf("session-%d", i), UserID: user.ID, IdentityID: binding.ID, Role: "readonly", ConfigFingerprint: registry.Manager("company").ConfigFingerprint(), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
		switch i {
		case 1:
			session.ExpiresAt = now.Add(-time.Minute)
		case 2:
			session.RevokedAt = &now
		case 3:
			session.ConfigFingerprint = "obsolete-fingerprint"
		case 4:
			if err := database.Model(&user).Update("enabled", false).Error; err != nil {
				t.Fatal(err)
			}
		case 5:
			if err := database.Model(&binding).Update("issuer", "https://old.example").Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := database.Create(&session).Error; err != nil {
			t.Fatal(err)
		}
	}
	response := sourceRequest(router, http.MethodGet, "/sources/company/impact", "")
	if response.Code != 200 {
		t.Fatalf("impact %d: %s", response.Code, response.Body)
	}
	var impact AuthenticationSourceImpact
	if err := json.Unmarshal(response.Body.Bytes(), &impact); err != nil {
		t.Fatal(err)
	}
	if impact.SourceID != "company" || impact.BoundUserCount != 24 || impact.DependentManagedUserCount != 11 || impact.ActiveSessionCount != 19 || len(impact.Users) != 20 || !impact.UsersTruncated || impact.ObservedAt.IsZero() {
		t.Fatalf("wrong impact counts: %+v", impact)
	}
	for _, private := range []string{"saved-private-secret", "private-subject", "private-identity-name", "session-", "fingerprint"} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatalf("impact leaks %s", private)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("impact must not cache account metadata")
	}
	// A source pending removal remains inspectable and recoverable until restart.
	if response := sourceRequest(router, http.MethodDelete, "/sources/company", ""); response.Code != 204 {
		t.Fatalf("delete %s", response.Body)
	}
	if response := sourceRequest(router, http.MethodGet, "/sources/company/impact", ""); response.Code != 200 {
		t.Fatal("pending source lost impact")
	}
	response = sourceRequest(router, http.MethodGet, "/sources", "")
	if !strings.Contains(response.Body.String(), `"can_restore":true`) {
		t.Fatal("missing restore capability")
	}
	if response := sourceRequest(router, http.MethodPost, "/sources/company/restore", `{}`); response.Code != 200 {
		t.Fatalf("restore %s", response.Body)
	}
	var count int64
	if err := database.Model(&db.ExternalIdentity{}).Where("provider = ?", "company").Count(&count).Error; err != nil || count != 24 {
		t.Fatal("remove/restore mutated bindings")
	}
}

func TestAuthenticationImpactFailureAndLocalAuthority(t *testing.T) {
	for _, principal := range []middleware.Principal{{Role: "admin", CanWrite: true, LoginMethod: "oidc"}, {Role: "readonly"}, {Role: "admin", AuthMethod: "api_token"}} {
		router, _, _ := authenticationImpactFixture(t, principal)
		if response := sourceRequest(router, http.MethodGet, "/sources/company/impact", ""); response.Code != 403 {
			t.Fatalf("unprivileged impact %d", response.Code)
		}
		if response := sourceRequest(router, http.MethodPost, "/sources/company/restore", `{}`); response.Code != 403 {
			t.Fatalf("unprivileged restore %d", response.Code)
		}
	}
	router, database, _ := authenticationImpactFixture(t, middleware.Principal{Role: "admin", CanWrite: true, CanManageCredentials: true})
	if response := sourceRequest(router, http.MethodGet, "/sources/missing/impact", ""); response.Code != 404 {
		t.Fatal("missing impact source accepted")
	}
	if err := database.Migrator().DropTable(&db.IdentitySession{}); err != nil {
		t.Fatal(err)
	}
	response := sourceRequest(router, http.MethodGet, "/sources/company/impact", "")
	if response.Code != 503 || strings.Contains(response.Body.String(), "no such table") || strings.Contains(response.Body.String(), `"active_session_count":0`) {
		t.Fatalf("failed query masquerades as empty count: %s", response.Body)
	}
}
