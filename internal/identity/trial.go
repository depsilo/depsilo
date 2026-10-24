package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"depsilo/internal/config"
)

var errAuthenticationNotCompleted = errors.New("AUTHENTICATION_NOT_COMPLETED")

type TrialResult struct {
	Outcome       string    `json:"outcome"`
	Authenticated bool      `json:"authenticated"`
	MappedRole    *string   `json:"mapped_role"`
	DisplayName   string    `json:"display_name,omitempty"`
	Code          string    `json:"code,omitempty"`
	CheckedAt     time.Time `json:"checked_at"`
}

// NewTrial never opens a durable account/session write path. It owns only one
// administrator's short-lived browser verification flow and provider metadata.
func NewTrial(id string, cfg config.OIDCConfig) (*Manager, error) {
	cfg.Enabled = true
	m, err := newManager(nil, id, cfg, &sync.Mutex{})
	if err != nil {
		return nil, err
	}
	m.trial = true
	return m, nil
}

func trialResult(auth authenticatedIdentity, err error, returnTo string) Result {
	trial := &TrialResult{Outcome: "success", Authenticated: auth.Verified, DisplayName: auth.Name, CheckedAt: time.Now().UTC()}
	if auth.Role != "" {
		role := auth.Role
		trial.MappedRole = &role
	}
	if errors.Is(err, errAuthenticationNotCompleted) {
		trial.Outcome, trial.Code = "failed", "AUTHENTICATION_NOT_COMPLETED"
	} else if errors.Is(err, ErrLDAPCredentials) || errors.Is(err, ErrLDAPUnavailable) {
		trial.Outcome, trial.Code = "failed", err.Error()
	} else if errors.Is(err, ErrDenied) && auth.Verified {
		trial.Outcome, trial.Code = "denied", "ACCESS_DENIED"
	} else if err != nil {
		trial.Outcome, trial.Code = "failed", "OIDC_UNAVAILABLE"
	}
	return Result{Mode: "test", ReturnTo: returnTo, Trial: trial}
}

// Check verifies discovery's issuer/endpoint origin and that its JWKS document
// is readable. Client credentials and actual login require a browser trial.
func (m *Manager) Check(ctx context.Context) error {
	if !m.Enabled() {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	p, _, err := m.provider(ctx)
	if err != nil {
		return err
	}
	var metadata struct {
		JWKS string `json:"jwks_uri"`
	}
	if p.Claims(&metadata) != nil {
		return ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadata.JWKS, nil)
	if err != nil {
		return ErrUnavailable
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	var keys struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&keys) != nil || len(keys.Keys) == 0 {
		return ErrUnavailable
	}
	return nil
}
