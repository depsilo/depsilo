package entitlement

import (
	"math"
	"time"

	"depsilo/internal/license"
	"depsilo/internal/trial"
)

// Source identifies which entitlement grant is currently active.
type Source string

const (
	SourceNone  Source = "none"
	SourceTrial Source = "trial"
	SourcePaid  Source = "paid"
)

// Status is the unified view used by frontend code, RequirePro 402 bodies,
// and the GetStatus admin endpoint. See spec §7.2 for field semantics and
// §16.2 for the deprecated-alias compatibility window.
type Status struct {
	IsPro            bool       `json:"is_pro"`
	Source           Source     `json:"source"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	DaysLeft         int        `json:"days_left"`
	TrialUsed        bool       `json:"trial_used"`
	TrialAvailable   bool       `json:"trial_available"`
	LicenseKeyMasked string     `json:"license_key_masked,omitempty"`
	LicenseError     string     `json:"license_error,omitempty"`
	LastChecked      time.Time  `json:"last_checked"`

	// Deprecated aliases — to be removed in 0.5.0 (spec §16.2).
	KeyMasked   string     `json:"key_masked,omitempty"`
	ActivatedAt *time.Time `json:"activated_at,omitempty"`
}

// Checker is the only abstraction the rest of the codebase reaches for
// entitlement decisions. RequirePro middleware, audit.Logger, and rules.Engine
// all depend on *Checker rather than directly on license.Manager or trial.Manager.
type Checker struct {
	lic   *license.Manager
	trial *trial.Manager
}

// NewChecker composes a license manager and a trial manager into one façade.
func NewChecker(lic *license.Manager, t *trial.Manager) *Checker {
	return &Checker{lic: lic, trial: t}
}

// IsPro reports whether ANY entitlement source currently grants Pro access.
// Hot path — called by RequirePro middleware on every Pro request.
func (c *Checker) IsPro() bool {
	if c.lic != nil && c.lic.IsPro() {
		return true
	}
	if c.trial != nil && c.trial.IsActive() {
		return true
	}
	return false
}

// TrialAvailable reports whether the user can still start a trial.
// False either because no trial manager is configured, or because a trial
// record already exists (active or expired — one trial per install).
func (c *Checker) TrialAvailable() bool {
	if c.trial == nil {
		return false
	}
	return !c.trial.IsUsed()
}

// Status assembles a unified view across the underlying sources.
// Precedence when multiple sources are active: paid > trial > none.
func (c *Checker) Status() Status {
	var licStatus license.LicenseStatus
	licPro := false
	if c.lic != nil {
		licStatus = c.lic.Status()
		licPro = licStatus.IsPro
	}
	trialUsed := c.trial != nil && c.trial.IsUsed()
	trialActive := c.trial != nil && c.trial.IsActive()

	s := Status{
		IsPro:            licPro || trialActive,
		TrialUsed:        trialUsed,
		TrialAvailable:   !trialUsed,
		LicenseKeyMasked: licStatus.KeyMasked,
		KeyMasked:        licStatus.KeyMasked, // deprecated alias
		LicenseError:     licStatus.Error,
		LastChecked:      licStatus.LastChecked,
	}

	now := time.Now().UTC()

	switch {
	case licPro:
		s.Source = SourcePaid
		s.ExpiresAt = licStatus.ExpiresAt
		s.ActivatedAt = licStatus.ActivatedAt
		if licStatus.ExpiresAt != nil {
			s.DaysLeft = daysBetween(now, *licStatus.ExpiresAt)
		}
	case trialActive:
		s.Source = SourceTrial
		if rec := c.trial.Record(); rec != nil {
			s.ExpiresAt = &rec.ExpiresAt
			s.ActivatedAt = &rec.ActivatedAt
			s.DaysLeft = daysBetween(now, rec.ExpiresAt)
		}
	default:
		s.Source = SourceNone
	}

	return s
}

func daysBetween(from, to time.Time) int {
	delta := to.Sub(from).Hours() / 24
	if delta <= 0 {
		return 0
	}
	return int(math.Ceil(delta))
}
