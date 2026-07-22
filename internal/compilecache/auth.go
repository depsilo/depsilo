package compilecache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

var (
	ErrUnauthorized    = errors.New("invalid or expired compile-cache credential")
	ErrForbidden       = errors.New("compile-cache credential does not grant this access")
	ErrAuthUnavailable = errors.New("compile-cache credential store is unavailable")
)

// CredentialPrincipal is the authenticated build-machine identity.
type CredentialPrincipal struct {
	ID        uint
	Namespace string
	CanWrite  bool
}

// Authorizer validates namespace-scoped compiler-cache bearer credentials.
type Authorizer struct {
	db  *gorm.DB
	now func() time.Time

	touchMu   sync.Mutex
	touched   map[uint]time.Time
	lastSweep time.Time
}

// NewAuthorizer constructs a credential Authorizer backed by the metadata DB.
func NewAuthorizer(database *gorm.DB) *Authorizer {
	return &Authorizer{db: database, now: time.Now, touched: make(map[uint]time.Time)}
}

// Authenticate validates a machine credential and binds it to the namespace
// in the URL. Credential last-use writes are coalesced to at most once per
// minute, avoiding a SQLite write for every compiler-cache request.
func (a *Authorizer) Authenticate(ctx context.Context, authorization, namespace string, write bool) (CredentialPrincipal, error) {
	if a == nil || a.db == nil || !strings.HasPrefix(authorization, "Bearer ") {
		return CredentialPrincipal{}, ErrUnauthorized
	}
	rawToken := strings.TrimPrefix(authorization, "Bearer ")
	if rawToken == "" || strings.ContainsAny(rawToken, " \t\r\n") {
		return CredentialPrincipal{}, ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(rawToken))
	var credential db.CompileCacheCredential
	lookup := a.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL", hex.EncodeToString(digest[:])).
		Limit(1).Find(&credential)
	if lookup.Error != nil {
		return CredentialPrincipal{}, fmt.Errorf("%w: %v", ErrAuthUnavailable, lookup.Error)
	}
	if lookup.RowsAffected == 0 {
		return CredentialPrincipal{}, ErrUnauthorized
	}
	now := a.now().UTC()
	if credential.ExpiresAt != nil && !now.Before(*credential.ExpiresAt) {
		return CredentialPrincipal{}, ErrUnauthorized
	}
	if credential.Namespace != namespace {
		return CredentialPrincipal{}, ErrForbidden
	}
	if credential.Permissions != "readonly" && credential.Permissions != "readwrite" {
		return CredentialPrincipal{}, ErrUnauthorized
	}
	if write && credential.Permissions != "readwrite" {
		return CredentialPrincipal{}, ErrForbidden
	}
	shouldTouch := credential.LastUsedAt == nil || now.Sub(credential.LastUsedAt.UTC()) >= time.Minute
	a.touchMu.Lock()
	if a.lastSweep.IsZero() || now.Before(a.lastSweep) || now.Sub(a.lastSweep) >= time.Minute {
		for credentialID, touchedAt := range a.touched {
			if !now.Before(touchedAt.Add(time.Minute)) {
				delete(a.touched, credentialID)
			}
		}
		a.lastSweep = now
	}
	if last, ok := a.touched[credential.ID]; ok && now.Sub(last) < time.Minute {
		shouldTouch = false
	}
	if shouldTouch {
		a.touched[credential.ID] = now
	}
	a.touchMu.Unlock()
	if shouldTouch {
		// LastUsedAt is observability, not authority. A failed touch must not
		// turn an otherwise valid cache operation into a build failure.
		if err := a.db.WithContext(ctx).Model(&credential).
			Where("revoked_at IS NULL").Update("last_used_at", &now).Error; err != nil {
			zap.L().Warn("update compiler-cache credential last use", zap.Uint("credential_id", credential.ID), zap.Error(err))
			a.touchMu.Lock()
			if a.touched[credential.ID].Equal(now) {
				delete(a.touched, credential.ID)
			}
			a.touchMu.Unlock()
		}
	}
	return CredentialPrincipal{
		ID: credential.ID, Namespace: credential.Namespace,
		CanWrite: credential.Permissions == "readwrite",
	}, nil
}
