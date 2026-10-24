package identity

import (
	"context"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"depsilo/internal/db"
)

// ChoiceInfo contains only information the verified browser needs to make an
// explicit account choice. Provider subject and authorization proof stay here.
type ChoiceInfo struct {
	DisplayName, Role, ReturnTo string
	ExpiresAt                   time.Time
}

type choiceTransaction struct {
	Auth authenticatedIdentity
	Info ChoiceInfo
	Busy bool
}

func (m *Manager) prepareChoice(result Result) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	if len(m.choices) >= maxPending {
		return Result{}, ErrRateLimited
	}
	choice := ChoiceInfo{DisplayName: result.candidate.Name, Role: result.candidate.Role, ReturnTo: result.ReturnTo, ExpiresAt: time.Now().Add(flowTTL)}
	key := randomToken()
	m.choices[key] = choiceTransaction{Auth: *result.candidate, Info: choice}
	return Result{Mode: "choice", ReturnTo: choice.ReturnTo, Choice: &choice, ChoiceKey: key}, nil
}

func (m *Manager) Choice(key string) (ChoiceInfo, error) {
	if !m.Enabled() {
		return ChoiceInfo{}, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	choice, exists := m.choices[key]
	if !exists {
		return ChoiceInfo{}, ErrExpired
	}
	return choice.Info, nil
}

func (m *Manager) CancelChoice(key string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.choices, key)
}

func (m *Manager) ResolveChoice(ctx context.Context, key, action, username, password string) (Result, error) {
	if !m.Enabled() {
		return Result{}, ErrUnavailable
	}
	if action != "create" && action != "link" {
		return Result{}, ErrDenied
	}
	m.mu.Lock()
	m.pruneLocked()
	choice, exists := m.choices[key]
	if !exists || choice.Busy {
		m.mu.Unlock()
		return Result{}, ErrExpired
	}
	choice.Busy = true
	m.choices[key] = choice
	m.mu.Unlock()
	txn := transaction{Mode: "create", ReturnTo: choice.Info.ReturnTo}
	if action == "link" {
		var user db.User
		username = strings.TrimSpace(username)
		err := m.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
		if err != nil || username == "" || len(username) > 255 || !user.Enabled || !localPassword(user) || len(password) > 1024 || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
			m.mu.Lock()
			if retained, ok := m.choices[key]; ok {
				retained.Busy = false
				m.choices[key] = retained
			}
			m.mu.Unlock()
			m.recordDenial(ctx, 0, ErrProof)
			return Result{}, ErrProof
		}
		txn.Mode, txn.UserID, txn.CredentialVersion = "link_login", user.ID, user.CredentialVersion
	}
	// Cancellation or expiry during the password proof wins before any durable
	// mutation. Otherwise this decision is consumed exactly once, including errors.
	m.mu.Lock()
	m.pruneLocked()
	_, exists = m.choices[key]
	delete(m.choices, key)
	m.mu.Unlock()
	if !exists {
		return Result{}, ErrExpired
	}
	result, err := m.apply(ctx, txn, choice.Auth)
	if err != nil {
		m.recordDenial(ctx, txn.UserID, err)
	}
	return result, err
}
