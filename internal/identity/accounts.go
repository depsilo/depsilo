package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"depsilo/internal/audit"
	"depsilo/internal/db"
)

func localPassword(user db.User) bool {
	return (user.PermissionSource == "local" || user.PermissionSource == "") && user.PasswordHash != ""
}

// ProvePassword verifies an enabled local account without treating a bound
// external login as authority to manage credentials. The handler rate-limits it.
func (m *Manager) ProvePassword(ctx context.Context, userID uint, version uint64, password string) (db.User, error) {
	if m == nil || m.db == nil {
		return db.User{}, ErrUnavailable
	}
	var user db.User
	if err := m.db.WithContext(ctx).First(&user, userID).Error; err != nil {
		m.recordDenial(ctx, userID, ErrProof)
		return db.User{}, ErrProof
	}
	if !user.Enabled || user.CredentialVersion != version || !localPassword(user) {
		m.recordDenial(ctx, userID, ErrDenied)
		return db.User{}, ErrDenied
	}
	if len(password) > 1024 || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		m.recordDenial(ctx, userID, ErrProof)
		return db.User{}, ErrProof
	}
	return user, nil
}

func (m *Manager) apply(ctx context.Context, txn transaction, auth authenticatedIdentity) (Result, error) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	result := Result{Mode: txn.Mode, ReturnTo: txn.ReturnTo}
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var linked db.ExternalIdentity
		findErr := tx.Where("issuer = ? AND subject = ?", m.cfg.Issuer, auth.Subject).First(&linked).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		// An issuer/subject previously bound under another source is never
		// silently reassigned, even after an operator deletes that source.
		if findErr == nil && linked.Provider != m.id {
			return ErrBound
		}
		if txn.Mode == "login" && errors.Is(findErr, gorm.ErrRecordNotFound) {
			result.Mode, result.candidate = "choice", &auth
			return nil
		}
		if txn.Mode == "create" && findErr == nil {
			return ErrBound
		}
		var user db.User
		linking := txn.Mode == "link" || txn.Mode == "link_login"
		if linking {
			if err := tx.First(&user, txn.UserID).Error; err != nil {
				return ErrDenied
			}
			if !user.Enabled || !localPassword(user) || user.CredentialVersion != txn.CredentialVersion {
				return ErrDenied
			}
			if findErr == nil {
				return ErrBound
			}
			var count int64
			if err := tx.Model(&db.ExternalIdentity{}).Where("user_id = ? AND provider = ?", user.ID, m.id).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return ErrBound
			}
			linked = db.ExternalIdentity{UserID: user.ID, Provider: m.id, Issuer: m.cfg.Issuer, Subject: auth.Subject, DisplayName: auth.Name, LastLoginAt: time.Now().UTC()}
			if err := tx.Create(&linked).Error; err != nil {
				return err
			}
			result.User = user
			if err := audit.RecordManagementEvent(ctx, tx, audit.ManagementEvent{ActorID: user.ID, Action: "identity.link", Target: fmt.Sprintf("user:%d", user.ID), Success: true, Detail: m.id}); err != nil {
				return err
			}
			if txn.Mode == "link" {
				return nil
			}
			findErr = nil
		}
		if findErr == nil {
			if err := tx.First(&user, linked.UserID).Error; err != nil {
				return ErrDenied
			}
			if !user.Enabled {
				return ErrDenied
			}
			if user.PermissionSource == m.Protocol() && user.Role != auth.Role {
				user.Role = auth.Role
				user.CredentialVersion++
				if err := tx.Model(&user).Updates(map[string]any{"role": user.Role, "credential_version": user.CredentialVersion}).Error; err != nil {
					return err
				}
				if err := tx.Model(&db.IdentitySession{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Update("revoked_at", time.Now().UTC()).Error; err != nil {
					return err
				}
			}
		} else {
			// Stable opaque names never match by email or provider-supplied name.
			hash := sha256.Sum256([]byte(m.cfg.Issuer + "\x00" + auth.Subject))
			user = db.User{Username: m.Protocol() + "_" + hex.EncodeToString(hash[:20]), Role: auth.Role, PermissionSource: m.Protocol(), Enabled: true, CredentialVersion: db.InitialCredentialVersion}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}
			linked = db.ExternalIdentity{UserID: user.ID, Provider: m.id, Issuer: m.cfg.Issuer, Subject: auth.Subject, DisplayName: auth.Name}
			if err := tx.Create(&linked).Error; err != nil {
				return err
			}
		}
		if user.PermissionSource != m.Protocol() && !localPassword(user) {
			return ErrDenied
		}
		now := time.Now().UTC()
		if err := tx.Model(&linked).Updates(map[string]any{"last_login_at": now, "display_name": auth.Name}).Error; err != nil {
			return err
		}
		if err := tx.Model(&user).Update("last_login_at", now).Error; err != nil {
			return err
		}
		// Every newly observed mapping invalidates older sessions for this
		// identity, including a locally owned account that was demoted at IdP.
		if err := tx.Model(&db.IdentitySession{}).Where("identity_id = ? AND role <> ? AND revoked_at IS NULL", linked.ID, auth.Role).Update("revoked_at", now).Error; err != nil {
			return err
		}
		session := db.IdentitySession{ID: randomToken(), UserID: user.ID, IdentityID: linked.ID, Role: auth.Role, ConfigFingerprint: m.fingerprint, CreatedAt: now, ExpiresAt: now.Add(m.cfg.SessionTTL)}
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		result.Mode = "login"
		result.User, result.Session = user, session
		return audit.RecordManagementEvent(ctx, tx, audit.ManagementEvent{ActorID: user.ID, Action: "identity.login", Target: fmt.Sprintf("user:%d", user.ID), Success: true, Detail: m.id})
	})
	if err != nil {
		if errors.Is(err, ErrDenied) || errors.Is(err, ErrBound) {
			return Result{}, err
		}
		return Result{}, ErrUnavailable
	}
	return result, nil
}

func (m *Manager) Unlink(ctx context.Context, userID uint, version uint64, identityID uint, password string) error {
	user, err := m.ProvePassword(ctx, userID, version, password)
	if err != nil {
		return err
	}
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current db.User
		if err := tx.First(&current, userID).Error; err != nil {
			return ErrDenied
		}
		if !current.Enabled || !localPassword(current) || current.CredentialVersion != user.CredentialVersion {
			return ErrDenied
		}
		var linked db.ExternalIdentity
		if err := tx.Where("id = ? AND user_id = ?", identityID, userID).First(&linked).Error; err != nil {
			return ErrDenied
		}
		if err := tx.Model(&db.IdentitySession{}).Where("identity_id = ? AND revoked_at IS NULL", identityID).Update("revoked_at", time.Now().UTC()).Error; err != nil {
			return err
		}
		if err := tx.Delete(&linked).Error; err != nil {
			return err
		}
		return audit.RecordManagementEvent(ctx, tx, audit.ManagementEvent{ActorID: userID, Action: "identity.unlink", Target: fmt.Sprintf("user:%d", userID), Success: true, Detail: linked.Provider})
	})
	if err != nil && !errors.Is(err, ErrDenied) {
		return ErrUnavailable
	}
	return err
}

func (m *Manager) revokeSubject(ctx context.Context, subject string) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return m.db.WithContext(ctx).Model(&db.IdentitySession{}).Where("identity_id IN (?) AND revoked_at IS NULL", m.db.Model(&db.ExternalIdentity{}).Select("id").Where("provider = ? AND issuer = ? AND subject = ?", m.id, m.cfg.Issuer, subject)).Update("revoked_at", time.Now().UTC()).Error
}

func (m *Manager) recordDenial(ctx context.Context, userID uint, err error) {
	if m == nil || m.db == nil {
		return
	}
	// All errors here are fixed public codes; never include provider payloads.
	code := "OIDC_UNAVAILABLE"
	for _, known := range []error{ErrDenied, ErrBound, ErrProof, ErrExpired, ErrRateLimited, ErrLDAPUnavailable, ErrLDAPCredentials} {
		if errors.Is(err, known) {
			code = known.Error()
			break
		}
	}
	_ = audit.RecordManagementEvent(ctx, m.db, audit.ManagementEvent{ActorID: userID, Action: "identity.denied", Target: m.id, Detail: code})
}
