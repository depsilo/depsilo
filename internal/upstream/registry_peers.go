package upstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/cascade"
	"depsilo/internal/db"
)

func (r *Registry) options() PoolOptions {
	r.optionsMu.RLock()
	defer r.optionsMu.RUnlock()
	return r.poolOptions
}

// Peers returns the cascade parents upstreams can currently egress through.
func (r *Registry) Peers() *PeerSet {
	return r.options().Peers
}

// SetPeers replaces the cascade parents while the server runs. Every upstream
// bound to a parent gets a new client for the new set; other upstreams keep
// their client, health, and probe worker. A nil set turns cascade egress off:
// bound upstreams then egress directly until a set names their parent again.
func (r *Registry) SetPeers(peers *PeerSet) error {
	for _, ecosystem := range r.active {
		r.mutationLocks[ecosystem].Lock()
		defer r.mutationLocks[ecosystem].Unlock()
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.optionsMu.Lock()
	r.poolOptions.Peers = peers
	r.optionsMu.Unlock()

	for _, ecosystem := range r.active {
		if err := r.rebuildBoundLocked(ecosystem); err != nil {
			return fmt.Errorf("rebuild %s upstreams for the new cascade parents: %w", ecosystem, err)
		}
	}
	return nil
}

func (r *Registry) rebuildBoundLocked(ecosystem string) error {
	var records []db.UpstreamRecord
	if err := r.db.Where("adapter_type = ?", ecosystem).Order("priority, id").Find(&records).Error; err != nil {
		return err
	}
	pool := r.pools[ecosystem]
	previous := pool.load()
	// Drop bound upstreams from the reuse candidates so they are rebuilt.
	reusable := &poolSnapshot{byID: make(map[uint]*Upstream)}
	if previous != nil {
		for id, current := range previous.byID {
			if current.Via == "" {
				reusable.byID[id] = current
			}
		}
	}
	next, err := buildPoolSnapshotWithOptions(records, reusable, r.options())
	if err != nil {
		return err
	}
	workers := planWorkers(previous, next)
	pool.Replace(next)
	r.applyWorkerPlanLocked(workers)
	return r.verify(ecosystem)
}

// RouteResult reports what RouteAllVia changed.
type RouteResult struct {
	// Bound counts the upstreams that now egress through the parent.
	Bound int
	// Skipped names upstreams left direct because their origin is on a
	// private or local network, which a parent cannot reach for this node.
	Skipped []string
}

// RouteAllVia binds every direct upstream of every active ecosystem to the
// named parent. Upstreams already bound to a parent keep theirs, and
// upstreams whose origin is local or private stay direct.
func (r *Registry) RouteAllVia(ctx context.Context, via string) (RouteResult, error) {
	if err := r.validatePeerReference(via); err != nil {
		return RouteResult{}, err
	}
	if via == "" {
		return RouteResult{}, fmt.Errorf("%w: cascade peer name is empty", ErrInvalidUpstream)
	}
	var result RouteResult
	for _, ecosystem := range r.active {
		lock := r.mutationLocks[ecosystem]
		lock.Lock()
		bound := 0
		prepared, err := r.prepareAndCommit(ctx, ecosystem, func(tx *gorm.DB) (uint, error) {
			var records []db.UpstreamRecord
			if err := tx.Where("adapter_type = ? AND via = ''", ecosystem).Order("priority, id").Find(&records).Error; err != nil {
				return 0, err
			}
			for _, record := range records {
				if localOrigin(record.URL) {
					result.Skipped = append(result.Skipped, ecosystem+"/"+record.Name)
					continue
				}
				if err := tx.Model(&db.UpstreamRecord{}).Where("id = ?", record.ID).Update("via", via).Error; err != nil {
					return 0, err
				}
				bound++
			}
			return 0, nil
		})
		if err == nil && bound > 0 {
			err = r.publish(ecosystem, prepared)
		}
		lock.Unlock()
		if err != nil {
			return result, fmt.Errorf("bind %s upstreams to %s: %w", ecosystem, via, err)
		}
		result.Bound += bound
	}
	return result, nil
}

// localOrigin reports whether rawURL names a host on a local or private
// network: a loopback, private, or link-local address, localhost, a single
// label, or a name under a local-only suffix.
func localOrigin(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}
	if host == "localhost" || !strings.Contains(host, ".") {
		return true
	}
	for _, suffix := range []string{".localhost", ".local", ".lan", ".internal", ".home.arpa", ".corp", ".intranet"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// Errors ProbePeer returns, wrapped with the detail.
var (
	ErrPeerUnreachable  = errors.New("cascade parent is unreachable")
	ErrPeerNotRelay     = errors.New("address does not serve the cascade relay")
	ErrPeerUnauthorized = errors.New("cascade parent rejected the token")
)

// ProbePeer checks that peer serves the cascade relay and accepts its token,
// through the same guarded egress a bound upstream would use. It returns the
// parent's instance ID.
func (r *Registry) ProbePeer(ctx context.Context, peer PeerEgress) (string, error) {
	if peer.URL == nil {
		return "", fmt.Errorf("%w: no URL", ErrPeerUnreachable)
	}
	client, err := buildClient("", peer.URL.String(), r.options())
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPeerUnreachable, err)
	}
	client.Timeout = 10 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	relay := *peer.URL
	relay.Path = strings.TrimRight(relay.Path, "/") + cascade.RelayPath
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, relay.String(), nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPeerUnreachable, err)
	}
	request.Header.Set(cascade.HeaderToken, peer.Token)
	request.Header.Set(cascade.HeaderProbe, "1")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPeerUnreachable, err)
	}
	defer response.Body.Close()
	_, _ = io.CopyN(io.Discard, response.Body, 64<<10)
	instance := response.Header.Get(cascade.HeaderInstance)
	switch {
	case instance == "":
		return "", fmt.Errorf("%w: it answered %d; enable cascade on the parent and use the address Depsilo listens on", ErrPeerNotRelay, response.StatusCode)
	case response.StatusCode == http.StatusUnauthorized:
		return "", ErrPeerUnauthorized
	case response.StatusCode != http.StatusNoContent:
		return "", fmt.Errorf("%w: the relay answered %d; upgrade the parent to a version that supports pairing", ErrPeerNotRelay, response.StatusCode)
	}
	return instance, nil
}
