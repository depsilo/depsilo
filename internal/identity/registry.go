package identity

import (
	"context"
	"sort"
	"sync"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
)

// Registry is the immutable, restart-applied set of authentication sources.
// All sources share account-write serialization; discovery remains lazy.
type Registry struct {
	sources  map[string]*Manager
	ordered  []*Manager
	accounts *Manager
}

func NewRegistry(database *gorm.DB, auth config.AuthConfig) (*Registry, error) {
	if err := config.ValidateAuthenticationSources(auth); err != nil {
		return nil, err
	}
	writes := &sync.Mutex{}
	r := &Registry{sources: make(map[string]*Manager), accounts: &Manager{db: database, id: ProviderID, writeMu: writes}}
	sources := config.AuthenticationSources(auth)
	ids := make([]string, 0, len(sources))
	for id := range sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m, err := newSourceManager(database, id, sources[id], writes)
		if err != nil {
			return nil, err
		}
		r.sources[id] = m
		r.ordered = append(r.ordered, m)
	}
	if database != nil {
		if err := database.Transaction(func(tx *gorm.DB) error {
			now := time.Now().UTC()
			for _, m := range r.ordered {
				if err := m.reconcileSessions(tx, now); err != nil {
					return err
				}
			}
			q := tx.Model(&db.IdentitySession{}).Where("revoked_at IS NULL")
			if len(ids) > 0 {
				retained := tx.Model(&db.ExternalIdentity{}).Select("id").Where("provider IN (?)", ids)
				q = q.Where("identity_id NOT IN (?)", retained)
			}
			return q.Update("revoked_at", now).Error
		}); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Registry) Manager(id string) *Manager {
	if r == nil {
		return nil
	}
	return r.sources[id]
}

func (r *Registry) Managers() []*Manager {
	if r == nil {
		return nil
	}
	return append([]*Manager(nil), r.ordered...)
}

// Select permits old clients only when their choice is unambiguous. A legacy
// source retains its identity even when disabled; it never falls through to a
// different provider without an explicit selection.
func (r *Registry) Select(id string) (*Manager, error) {
	if id != "" {
		m := r.Manager(id)
		if !m.Enabled() {
			return nil, ErrUnavailable
		}
		return m, nil
	}
	if legacy := r.Manager(ProviderID); legacy != nil {
		if !legacy.Enabled() {
			return nil, ErrUnavailable
		}
		return legacy, nil
	}
	var selected *Manager
	for _, m := range r.Managers() {
		if m.Enabled() {
			if selected != nil {
				return nil, ErrSelection
			}
			selected = m
		}
	}
	if selected == nil {
		return nil, ErrUnavailable
	}
	return selected, nil
}

func (r *Registry) ProvePassword(ctx context.Context, userID uint, version uint64, password string) (db.User, error) {
	if r == nil {
		return db.User{}, ErrUnavailable
	}
	return r.accounts.ProvePassword(ctx, userID, version, password)
}

func (r *Registry) Unlink(ctx context.Context, userID uint, version uint64, identityID uint, password string) error {
	if r == nil {
		return ErrUnavailable
	}
	return r.accounts.Unlink(ctx, userID, version, identityID, password)
}
