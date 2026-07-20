package upstream

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"depsilo/internal/db"
	"gorm.io/gorm"
)

var (
	ErrNotFound           = errors.New("upstream not found")
	ErrEcosystemNotActive = errors.New("ecosystem not active")
	ErrInvalidUpstream    = errors.New("invalid upstream")
	ErrImmutableEcosystem = errors.New("immutable ecosystem")
	ErrConflict           = errors.New("upstream conflict")
	ErrLastUpstream       = errors.New("last upstream")
	ErrReconcileFailed    = errors.New("registry reconcile failed")
)

type RuntimeUpstream struct {
	ID                            uint
	AdapterType, Name, URL, Proxy string
	Priority                      int
	ProbeMode, ProbeInterval      string
	Healthy                       bool
	AvgLatencyMS                  int64
	SuccessRate                   float64
	LastCheckedAt                 time.Time
	WorkerRunning                 bool
	CreatedAt, UpdatedAt          time.Time
}

type workerHandle struct {
	generation uint64
	cancel     context.CancelFunc
	done       chan struct{}
}

type Registry struct {
	db            *gorm.DB
	commit        func(*gorm.DB) error
	active        []string
	pools         map[string]*Pool
	mutationLocks map[string]*sync.Mutex

	lifecycleMu    sync.Mutex
	workersMu      sync.Mutex
	workers        map[uint]workerHandle
	nextGeneration uint64
	ctx            context.Context
	cancel         context.CancelFunc
	started        bool
	closeDone      chan struct{}
	closingWorkers []workerHandle

	degradedMu sync.RWMutex
	degraded   map[string]error
}

func NewRegistry(database *gorm.DB, active []string) (*Registry, error) {
	ordered, err := canonicalActive(active)
	if err != nil {
		return nil, err
	}
	r := &Registry{
		db:            database,
		commit:        func(tx *gorm.DB) error { return tx.Commit().Error },
		active:        ordered,
		pools:         make(map[string]*Pool),
		mutationLocks: make(map[string]*sync.Mutex),
		workers:       make(map[uint]workerHandle),
		degraded:      make(map[string]error),
	}
	for _, ecosystem := range ordered {
		var records []db.UpstreamRecord
		if err := database.Where("adapter_type = ?", ecosystem).Order("priority, id").Find(&records).Error; err != nil {
			return nil, err
		}
		if len(records) == 0 {
			return nil, fmt.Errorf("active ecosystem %s has no upstreams", ecosystem)
		}
		pool, err := NewPoolFromRecords(records)
		if err != nil {
			return nil, fmt.Errorf("build %s pool: %w", ecosystem, err)
		}
		r.pools[ecosystem] = pool
		r.mutationLocks[ecosystem] = &sync.Mutex{}
	}
	return r, nil
}

func (r *Registry) Start(parent context.Context) {
	for {
		r.lifecycleMu.Lock()
		if r.closeDone != nil {
			closeDone := r.closeDone
			r.lifecycleMu.Unlock()
			<-closeDone
			continue
		}

		r.workersMu.Lock()
		if r.started {
			r.workersMu.Unlock()
			r.lifecycleMu.Unlock()
			return
		}
		r.ctx, r.cancel = context.WithCancel(parent)
		r.started = true
		for _, pool := range r.pools {
			for _, u := range pool.Snapshot() {
				r.startWorkerLocked(u)
			}
		}
		r.workersMu.Unlock()
		r.lifecycleMu.Unlock()
		return
	}
}

// Close cancels all health workers and waits without a deadline.
func (r *Registry) Close() {
	_ = r.CloseContext(context.Background())
}

// CloseContext cancels all health workers and waits for the current generation.
// A caller timeout only stops that caller's wait; a later call can join the same
// close transition and finish waiting for the workers.
func (r *Registry) CloseContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.lifecycleMu.Lock()
	if r.closeDone != nil {
		handles := append([]workerHandle(nil), r.closingWorkers...)
		r.lifecycleMu.Unlock()
		return waitWorkerHandles(ctx, handles)
	}

	r.workersMu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	r.ctx = nil
	r.cancel = nil
	r.started = false
	handles := make([]workerHandle, 0, len(r.workers))
	for _, handle := range r.workers {
		handle.cancel()
		handles = append(handles, handle)
	}
	r.workersMu.Unlock()
	if len(handles) == 0 {
		r.lifecycleMu.Unlock()
		return nil
	}

	closeDone := make(chan struct{})
	r.closeDone = closeDone
	r.closingWorkers = append([]workerHandle(nil), handles...)
	r.lifecycleMu.Unlock()

	go r.completeClose(handles, closeDone)
	return waitWorkerHandles(ctx, handles)
}

func waitWorkerHandles(ctx context.Context, handles []workerHandle) error {
	for _, handle := range handles {
		select {
		case <-handle.done:
			continue
		default:
		}
		select {
		case <-handle.done:
		case <-ctx.Done():
			select {
			case <-handle.done:
				continue
			default:
				return ctx.Err()
			}
		}
	}
	return nil
}

func (r *Registry) completeClose(handles []workerHandle, closeDone chan struct{}) {
	_ = waitWorkerHandles(context.Background(), handles)

	r.lifecycleMu.Lock()
	if r.closeDone == closeDone {
		r.closeDone = nil
		r.closingWorkers = nil
		close(closeDone)
	}
	r.lifecycleMu.Unlock()
}

func (r *Registry) startWorkerLocked(u *Upstream) {
	if !r.started || u.ID == 0 || u.ProbeMode != "active" {
		return
	}
	ctx, cancel := context.WithCancel(r.ctx)
	done := make(chan struct{})
	r.nextGeneration++
	generation := r.nextGeneration
	r.workers[u.ID] = workerHandle{generation: generation, cancel: cancel, done: done}
	go func() {
		defer r.finishWorker(u.ID, generation, done)
		runUpstreamHealthCheck(ctx, u, r.db, u.ProbeInterval)
	}()
}

func (r *Registry) finishWorker(id uint, generation uint64, done chan struct{}) {
	r.workersMu.Lock()
	handle, ok := r.workers[id]
	if ok && handle.generation == generation && handle.done == done {
		delete(r.workers, id)
	}
	r.workersMu.Unlock()
	close(done)
}

func (r *Registry) Pools() map[string]*Pool {
	out := make(map[string]*Pool, len(r.pools))
	for ecosystem, pool := range r.pools {
		out[ecosystem] = pool
	}
	return out
}

func (r *Registry) ActiveEcosystems() []string {
	return append([]string(nil), r.active...)
}

func (r *Registry) WorkerRunning(id uint) bool {
	r.workersMu.Lock()
	defer r.workersMu.Unlock()
	_, ok := r.workers[id]
	return ok
}

func (r *Registry) runtimeUpstream(u *Upstream) RuntimeUpstream {
	health := u.HealthSnapshot()
	return RuntimeUpstream{
		ID: u.ID, AdapterType: u.AdapterType, Name: u.Name, URL: u.URL, Proxy: u.Proxy,
		Priority: u.Priority, ProbeMode: u.ProbeMode, ProbeInterval: u.ProbeInterval.String(),
		Healthy: health.Healthy, AvgLatencyMS: health.AvgLatency.Milliseconds(),
		SuccessRate: health.SuccessRate, LastCheckedAt: health.LastCheckedAt,
		WorkerRunning: r.WorkerRunning(u.ID), CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

func (r *Registry) List() []RuntimeUpstream {
	out := make([]RuntimeUpstream, 0)
	for _, ecosystem := range r.active {
		for _, u := range r.pools[ecosystem].Snapshot() {
			out = append(out, r.runtimeUpstream(u))
		}
	}
	return out
}

func (r *Registry) Get(id uint) (RuntimeUpstream, error) {
	for _, ecosystem := range r.active {
		if u, ok := r.pools[ecosystem].Find(id); ok {
			return r.runtimeUpstream(u), nil
		}
	}
	return RuntimeUpstream{}, ErrNotFound
}
