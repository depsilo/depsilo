package upstream

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"depsilo/internal/db"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type MutationInput struct {
	AdapterType, Name, URL, Proxy string
	Priority                      int
	ProbeMode, ProbeInterval      string
}

type preparedMutation struct {
	next     *poolSnapshot
	workers  workerPlan
	resultID uint
}

type workerPlan struct {
	stop  []uint
	start []*Upstream
}

func validateMutation(input MutationInput) (MutationInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.URL = strings.TrimSpace(input.URL)
	input.Proxy = strings.TrimSpace(input.Proxy)
	if input.Name == "" || len(input.Name) > 128 || input.Priority <= 0 {
		return input, fmt.Errorf("%w: name and positive priority are required", ErrInvalidUpstream)
	}
	if !validOriginURL(input.URL) {
		return input, fmt.Errorf("%w: invalid url", ErrInvalidUpstream)
	}
	if input.Proxy != "" && !validHTTPURL(input.Proxy) {
		return input, fmt.Errorf("%w: invalid proxy", ErrInvalidUpstream)
	}
	if input.ProbeMode != "active" && input.ProbeMode != "passive" {
		return input, fmt.Errorf("%w: probe_mode must be active or passive", ErrInvalidUpstream)
	}
	interval, err := time.ParseDuration(input.ProbeInterval)
	if err != nil || interval <= 0 {
		return input, fmt.Errorf("%w: probe_interval must be a positive Go duration", ErrInvalidUpstream)
	}
	input.ProbeInterval = interval.String()
	return input, nil
}

func (r *Registry) runTransaction(ctx context.Context, apply func(*gorm.DB) error) (err error) {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback().Error
		}
	}()
	if err := apply(tx); err != nil {
		return err
	}
	if err := r.commit(tx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (r *Registry) prepareAndCommit(ctx context.Context, ecosystem string, apply func(*gorm.DB) (uint, error)) (preparedMutation, error) {
	pool := r.pools[ecosystem]
	previous := pool.load()
	var prepared preparedMutation
	err := r.runTransaction(ctx, func(tx *gorm.DB) error {
		id, err := apply(tx)
		if err != nil {
			return err
		}
		var records []db.UpstreamRecord
		if err := tx.Where("adapter_type = ?", ecosystem).Order("priority, id").Find(&records).Error; err != nil {
			return err
		}
		next, err := buildPoolSnapshot(records, previous)
		if err != nil {
			return err
		}
		prepared = preparedMutation{next: next, workers: planWorkers(previous, next), resultID: id}
		return nil
	})
	return prepared, err
}

func (r *Registry) Create(ctx context.Context, input MutationInput) (RuntimeUpstream, error) {
	lock := r.mutationLocks[input.AdapterType]
	if lock == nil {
		return RuntimeUpstream{}, ErrEcosystemNotActive
	}
	input, err := validateMutation(input)
	if err != nil {
		return RuntimeUpstream{}, err
	}
	lock.Lock()
	defer lock.Unlock()
	prepared, err := r.prepareAndCommit(ctx, input.AdapterType, func(tx *gorm.DB) (uint, error) {
		var count int64
		if err := tx.Model(&db.UpstreamRecord{}).Where("adapter_type = ? AND name = ?", input.AdapterType, input.Name).Count(&count).Error; err != nil {
			return 0, err
		}
		if count != 0 {
			return 0, ErrConflict
		}
		record := db.UpstreamRecord{
			AdapterType:   input.AdapterType,
			Name:          input.Name,
			URL:           input.URL,
			Proxy:         input.Proxy,
			Priority:      input.Priority,
			ProbeMode:     input.ProbeMode,
			ProbeInterval: input.ProbeInterval,
			Healthy:       true,
			SuccessRate:   1,
		}
		if err := tx.Create(&record).Error; err != nil {
			return 0, err
		}
		return record.ID, nil
	})
	if err != nil {
		return RuntimeUpstream{}, err
	}
	if err := r.publish(input.AdapterType, prepared); err != nil {
		return RuntimeUpstream{}, err
	}
	return r.Get(prepared.resultID)
}

func (r *Registry) Update(ctx context.Context, id uint, input MutationInput) (RuntimeUpstream, error) {
	var current db.UpstreamRecord
	if err := r.db.WithContext(ctx).First(&current, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return RuntimeUpstream{}, ErrNotFound
	} else if err != nil {
		return RuntimeUpstream{}, err
	}
	if input.AdapterType != current.AdapterType {
		return RuntimeUpstream{}, ErrImmutableEcosystem
	}
	lock := r.mutationLocks[current.AdapterType]
	if lock == nil {
		return RuntimeUpstream{}, ErrEcosystemNotActive
	}
	input, err := validateMutation(input)
	if err != nil {
		return RuntimeUpstream{}, err
	}
	lock.Lock()
	defer lock.Unlock()
	prepared, err := r.prepareAndCommit(ctx, current.AdapterType, func(tx *gorm.DB) (uint, error) {
		var record db.UpstreamRecord
		if err := tx.First(&record, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrNotFound
		} else if err != nil {
			return 0, err
		}
		var count int64
		if err := tx.Model(&db.UpstreamRecord{}).Where("adapter_type = ? AND name = ? AND id <> ?", current.AdapterType, input.Name, id).Count(&count).Error; err != nil {
			return 0, err
		}
		if count != 0 {
			return 0, ErrConflict
		}
		err := tx.Model(&record).Updates(map[string]any{
			"name":           input.Name,
			"url":            input.URL,
			"proxy":          input.Proxy,
			"priority":       input.Priority,
			"probe_mode":     input.ProbeMode,
			"probe_interval": input.ProbeInterval,
		}).Error
		if err != nil {
			return 0, err
		}
		return id, nil
	})
	if err != nil {
		return RuntimeUpstream{}, err
	}
	if err := r.publish(current.AdapterType, prepared); err != nil {
		return RuntimeUpstream{}, err
	}
	return r.Get(id)
}

func (r *Registry) Delete(ctx context.Context, id uint) (RuntimeUpstream, error) {
	before, err := r.Get(id)
	if err != nil {
		return RuntimeUpstream{}, err
	}
	lock := r.mutationLocks[before.AdapterType]
	if lock == nil {
		return RuntimeUpstream{}, ErrEcosystemNotActive
	}
	lock.Lock()
	defer lock.Unlock()
	current, err := r.Get(id)
	if err != nil {
		return RuntimeUpstream{}, err
	}
	prepared, err := r.prepareAndCommit(ctx, current.AdapterType, func(tx *gorm.DB) (uint, error) {
		var record db.UpstreamRecord
		if err := tx.First(&record, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrNotFound
		} else if err != nil {
			return 0, err
		}
		var count int64
		if err := tx.Model(&db.UpstreamRecord{}).Where("adapter_type = ?", record.AdapterType).Count(&count).Error; err != nil {
			return 0, err
		}
		if count <= 1 {
			return 0, ErrLastUpstream
		}
		result := tx.Delete(&db.UpstreamRecord{}, id)
		if result.Error != nil {
			return 0, result.Error
		}
		if result.RowsAffected == 0 {
			return 0, ErrNotFound
		}
		return id, nil
	})
	if err != nil {
		return RuntimeUpstream{}, err
	}
	if err := r.publish(current.AdapterType, prepared); err != nil {
		return RuntimeUpstream{}, err
	}
	return current, nil
}

func (r *Registry) Check(ctx context.Context, id uint) (RuntimeUpstream, ProbeResult, error) {
	resource, err := r.Get(id)
	if err != nil {
		return RuntimeUpstream{}, ProbeResult{}, err
	}
	lock := r.mutationLocks[resource.AdapterType]
	if lock == nil {
		return RuntimeUpstream{}, ProbeResult{}, ErrEcosystemNotActive
	}
	lock.Lock()
	defer lock.Unlock()
	u, ok := r.pools[resource.AdapterType].Find(id)
	if !ok {
		return RuntimeUpstream{}, ProbeResult{}, ErrNotFound
	}
	result := probe(ctx, u)
	if err := persistCheckedProbe(r.db.WithContext(ctx), u, result); err != nil {
		return RuntimeUpstream{}, ProbeResult{}, err
	}
	updated, err := r.Get(id)
	return updated, result, err
}

func persistCheckedProbe(database *gorm.DB, u *Upstream, result ProbeResult) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	next := u.health
	next.applyProbe(result)

	err := database.Transaction(func(tx *gorm.DB) error {
		if err := persistHealthSnapshot(tx, u, HealthSnapshot{
			Healthy:       next.healthy,
			AvgLatency:    next.avgLatency,
			SuccessRate:   next.successRate,
			LastCheckedAt: next.lastCheckedAt,
		}); err != nil {
			return err
		}
		return tx.Create(&db.UpstreamLatencyLog{
			UpstreamID: u.ID,
			Name:       u.Name,
			LatencyMs:  result.Latency.Milliseconds(),
			Healthy:    result.Healthy,
			CreatedAt:  result.CheckedAt,
		}).Error
	})
	if err != nil {
		return err
	}
	u.health = next
	return nil
}

func planWorkers(previous, next *poolSnapshot) workerPlan {
	plan := workerPlan{}
	if previous != nil {
		for id, old := range previous.byID {
			current := next.byID[id]
			if current != old {
				plan.stop = append(plan.stop, id)
			}
		}
	}
	for id, current := range next.byID {
		var old *Upstream
		if previous != nil {
			old = previous.byID[id]
		}
		if current.ProbeMode == "active" && current != old {
			plan.start = append(plan.start, current)
		}
	}
	sort.Slice(plan.stop, func(i, j int) bool { return plan.stop[i] < plan.stop[j] })
	sort.Slice(plan.start, func(i, j int) bool { return plan.start[i].ID < plan.start[j].ID })
	return plan
}

// publish serializes snapshot and worker publication with Start and Close.
func (r *Registry) publish(ecosystem string, prepared preparedMutation) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.pools[ecosystem].Replace(prepared.next)
	r.applyWorkerPlanLocked(prepared.workers)
	if err := r.verify(ecosystem); err == nil {
		r.clearDegraded(ecosystem)
		return nil
	}
	if err := r.reloadEcosystemLocked(ecosystem); err != nil {
		r.markDegraded(ecosystem, err)
		return fmt.Errorf("%w: %v", ErrReconcileFailed, err)
	}
	r.clearDegraded(ecosystem)
	return nil
}

func (r *Registry) applyWorkerPlanLocked(plan workerPlan) {
	r.workersMu.Lock()
	stopped := make([]workerHandle, 0, len(plan.stop))
	for _, id := range plan.stop {
		if handle, ok := r.workers[id]; ok {
			handle.cancel()
			stopped = append(stopped, handle)
			delete(r.workers, id)
		}
	}
	r.workersMu.Unlock()
	for _, handle := range stopped {
		<-handle.done
	}
	r.workersMu.Lock()
	defer r.workersMu.Unlock()
	for _, current := range plan.start {
		r.startWorkerLocked(current)
	}
}

func snapshotMatches(snapshot *poolSnapshot, records []db.UpstreamRecord) bool {
	if snapshot == nil || len(snapshot.upstreams) != len(records) {
		return false
	}
	for i, record := range records {
		mode := record.ProbeMode
		if mode == "" {
			mode = "active"
		}
		intervalText := record.ProbeInterval
		if intervalText == "" {
			intervalText = DefaultProbeInterval.String()
		}
		interval, err := time.ParseDuration(intervalText)
		if err != nil || interval <= 0 {
			return false
		}
		current := snapshot.upstreams[i]
		if current.ID != record.ID || current.AdapterType != record.AdapterType ||
			current.Name != record.Name || current.URL != record.URL || current.Proxy != record.Proxy ||
			current.Priority != record.Priority || current.ProbeMode != mode || current.ProbeInterval != interval {
			return false
		}
	}
	return true
}

func (r *Registry) verify(ecosystem string) error {
	var records []db.UpstreamRecord
	if err := r.db.Where("adapter_type = ?", ecosystem).Order("priority, id").Find(&records).Error; err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("active ecosystem %s has no upstreams", ecosystem)
	}
	if !snapshotMatches(r.pools[ecosystem].load(), records) {
		return fmt.Errorf("runtime snapshot differs from committed %s records", ecosystem)
	}
	return nil
}

func (r *Registry) reloadEcosystemLocked(ecosystem string) error {
	var records []db.UpstreamRecord
	if err := r.db.Where("adapter_type = ?", ecosystem).Order("priority, id").Find(&records).Error; err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("active ecosystem %s has no upstreams", ecosystem)
	}
	pool := r.pools[ecosystem]
	previous := pool.load()
	next, err := buildPoolSnapshot(records, previous)
	if err != nil {
		return err
	}
	workers := planWorkers(previous, next)
	pool.Replace(next)
	r.applyWorkerPlanLocked(workers)
	return r.verify(ecosystem)
}

func (r *Registry) markDegraded(ecosystem string, err error) {
	r.degradedMu.Lock()
	r.degraded[ecosystem] = err
	r.degradedMu.Unlock()
	zap.L().Error("upstream registry ecosystem degraded", zap.String("ecosystem", ecosystem), zap.Error(err))
}

func (r *Registry) clearDegraded(ecosystem string) {
	r.degradedMu.Lock()
	defer r.degradedMu.Unlock()
	delete(r.degraded, ecosystem)
}

func (r *Registry) degradedError(ecosystem string) error {
	r.degradedMu.RLock()
	defer r.degradedMu.RUnlock()
	return r.degraded[ecosystem]
}
