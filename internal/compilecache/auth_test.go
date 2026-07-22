package compilecache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestAuthorizerEnforcesNamespaceExpiryAndPermission(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CompileCacheCredential{}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	createCredential := func(name, raw, namespace, permissions string, expiresAt *time.Time) {
		digest := sha256.Sum256([]byte(raw))
		if err := database.Create(&db.CompileCacheCredential{
			Name: name, Namespace: namespace, Permissions: permissions,
			TokenHash: hex.EncodeToString(digest[:]), ExpiresAt: expiresAt,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	createCredential("reader", "depsilo_cc_reader", "team-a", "readonly", nil)
	createCredential("writer", "depsilo_cc_writer", "team-a", "readwrite", nil)
	expired := now.Add(-time.Second)
	createCredential("expired", "depsilo_cc_expired", "team-a", "readwrite", &expired)
	revoked := now.Add(-time.Minute)
	createCredential("revoked", "depsilo_cc_revoked", "team-a", "readwrite", nil)
	if err := database.Model(&db.CompileCacheCredential{}).Where("name = ?", "revoked").Update("revoked_at", &revoked).Error; err != nil {
		t.Fatal(err)
	}

	authorizer := NewAuthorizer(database)
	authorizer.now = func() time.Time { return now }
	authorizer.touched[999] = now.Add(-2 * time.Minute)
	authorizer.lastSweep = now.Add(-2 * time.Minute)
	if _, err := authorizer.Authenticate(context.Background(), "Bearer depsilo_cc_reader", "team-a", false); err != nil {
		t.Fatalf("readonly read: %v", err)
	}
	if _, exists := authorizer.touched[999]; exists {
		t.Fatal("expired credential touch was not pruned")
	}
	if _, err := authorizer.Authenticate(context.Background(), "Bearer depsilo_cc_reader", "team-a", true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readonly write error = %v, want ErrForbidden", err)
	}
	if _, err := authorizer.Authenticate(context.Background(), "Bearer depsilo_cc_writer", "team-b", false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong namespace error = %v, want ErrForbidden", err)
	}
	if _, err := authorizer.Authenticate(context.Background(), "Bearer depsilo_cc_expired", "team-a", false); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired error = %v, want ErrUnauthorized", err)
	}
	if _, err := authorizer.Authenticate(context.Background(), "Bearer depsilo_cc_revoked", "team-a", false); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked error = %v, want ErrUnauthorized", err)
	}
	for _, header := range []string{"", "Basic depsilo_cc_writer", "Bearer ", "Bearer depsilo_cc_writer extra"} {
		if _, err := authorizer.Authenticate(context.Background(), header, "team-a", false); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("header %q error = %v, want ErrUnauthorized", header, err)
		}
	}
	var reader db.CompileCacheCredential
	if err := database.Where("name = ?", "reader").First(&reader).Error; err != nil {
		t.Fatal(err)
	}
	if reader.LastUsedAt == nil || !reader.LastUsedAt.Equal(now) {
		t.Fatalf("reader last_used_at = %v, want %v", reader.LastUsedAt, now)
	}
}

func TestAuthorizerDistinguishesCredentialStoreFailure(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "auth-unavailable.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CompileCacheCredential{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = NewAuthorizer(database).Authenticate(context.Background(), "Bearer depsilo_cc_any", "team-a", false)
	if !errors.Is(err, ErrAuthUnavailable) {
		t.Fatalf("Authenticate error = %v, want ErrAuthUnavailable", err)
	}
}
