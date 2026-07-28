package huggingface

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/db"
)

const (
	maxTrackedRepositoryRevocations = 4096
	repositoryCleanupTimeout        = 10 * time.Second
)

type huggingFaceRepository struct {
	packageName string
	escapedRepo string
}

func repositoryForParsed(parsed Parsed) (huggingFaceRepository, bool) {
	repo, ok := escapedPath(parsed.Repo, parsed.repoSegments)
	if !ok || parsed.Repo == "" {
		return huggingFaceRepository{}, false
	}

	switch parsed.Kind {
	case PathResolve, PathRaw,
		PathAPIModelInfo, PathAPIModelRevision, PathAPIModelTree:
		return huggingFaceRepository{
			packageName: parsed.Repo,
			escapedRepo: repo,
		}, true
	case PathAPIDatasetInfo, PathAPIDatasetRevision, PathAPIDatasetTree:
		return huggingFaceRepository{
			packageName: "datasets/" + parsed.Repo,
			escapedRepo: "datasets/" + repo,
		}, true
	default:
		return huggingFaceRepository{}, false
	}
}

type repositoryRevocationState struct {
	generation   uint64
	durableToken string
	cleanupSafe  bool
	revoked      bool
	active       int
	drained      chan struct{}
}

type repositoryRevocationTicket struct {
	repository string
	generation uint64
	revoked    bool
}

// repositoryRevocationGate is the adapter-level fail-closed boundary. Cache
// deletion can involve many keys and two backends; once a repository is marked
// revoked, every public request bypasses cache until cleanup is known safe and
// an anonymous upstream response proves the repository is public again.
type repositoryRevocationGate struct {
	mu           sync.Mutex
	states       map[string]*repositoryRevocationState
	generation   uint64
	revokedCount int
	failClosed   bool
	beginOrder   repositoryRevocationBeginOrder
}

type repositoryRevocationBeginOrder struct {
	mu      sync.Mutex
	entries map[string]*repositoryRevocationBeginEntry
}

type repositoryRevocationBeginEntry struct {
	mu   sync.Mutex
	refs int
}

func newRepositoryRevocationGate() *repositoryRevocationGate {
	return &repositoryRevocationGate{
		states: make(map[string]*repositoryRevocationState),
	}
}

func (g *repositoryRevocationGate) lockBegin(repository string) func() {
	if g == nil || repository == "" {
		return func() {}
	}
	order := &g.beginOrder
	order.mu.Lock()
	if order.entries == nil {
		order.entries = make(map[string]*repositoryRevocationBeginEntry)
	}
	entry := order.entries[repository]
	if entry == nil {
		entry = &repositoryRevocationBeginEntry{}
		order.entries[repository] = entry
	}
	entry.refs++
	order.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		order.mu.Lock()
		entry.refs--
		if entry.refs == 0 && order.entries[repository] == entry {
			delete(order.entries, repository)
		}
		order.mu.Unlock()
	}
}

func (g *repositoryRevocationGate) failClose() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.failClosed = true
	g.states = make(map[string]*repositoryRevocationState)
	g.revokedCount = 0
	g.mu.Unlock()
}

func (g *repositoryRevocationGate) seed(
	repository string,
	durableToken string,
	cleanupSafe bool,
) bool {
	if g == nil || repository == "" || durableToken == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failClosed {
		return false
	}
	if _, found := g.states[repository]; found {
		g.failClosed = true
		g.states = make(map[string]*repositoryRevocationState)
		g.revokedCount = 0
		return false
	}
	if g.revokedCount >= maxTrackedRepositoryRevocations {
		g.failClosed = true
		g.states = make(map[string]*repositoryRevocationState)
		g.revokedCount = 0
		return false
	}
	g.generation++
	g.states[repository] = &repositoryRevocationState{
		generation:   g.generation,
		durableToken: durableToken,
		cleanupSafe:  cleanupSafe,
		revoked:      true,
	}
	g.revokedCount++
	return true
}

func (g *repositoryRevocationGate) enterCache(repository string) (func(), bool) {
	if g == nil || repository == "" {
		return func() {}, true
	}
	g.mu.Lock()
	if g.failClosed {
		g.mu.Unlock()
		return nil, false
	}
	state := g.states[repository]
	if state != nil && state.revoked {
		g.mu.Unlock()
		return nil, false
	}
	if state == nil {
		state = &repositoryRevocationState{}
		g.states[repository] = state
	}
	state.active++
	g.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			current := g.states[repository]
			if current == state {
				state.active--
				if state.active == 0 {
					if state.drained != nil {
						close(state.drained)
					}
					if !state.revoked {
						delete(g.states, repository)
					}
				}
			}
			g.mu.Unlock()
		})
	}, true
}

func (g *repositoryRevocationGate) begin(repository string) (uint64, <-chan struct{}) {
	if g == nil || repository == "" {
		return 0, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	generation, drained, _ := g.beginLocked(repository, 0, false)
	return generation, drained
}

func (g *repositoryRevocationGate) beginIfGeneration(
	repository string,
	expectedGeneration uint64,
) (uint64, <-chan struct{}, bool) {
	if g == nil || repository == "" || expectedGeneration == 0 {
		return 0, nil, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.beginLocked(repository, expectedGeneration, true)
}

func (g *repositoryRevocationGate) beginLocked(
	repository string,
	expectedGeneration uint64,
	requireExpected bool,
) (uint64, <-chan struct{}, bool) {
	if g.failClosed {
		return 0, nil, false
	}
	state := g.states[repository]
	if requireExpected &&
		(state == nil ||
			!state.revoked ||
			state.generation != expectedGeneration) {
		return 0, nil, false
	}
	if (state == nil || !state.revoked) &&
		g.revokedCount >= maxTrackedRepositoryRevocations {
		// Never evict a revocation marker merely to bound memory. A process-wide
		// cache bypass is safer than making the evicted private repository
		// readable again.
		g.failClosed = true
		g.states = make(map[string]*repositoryRevocationState)
		g.revokedCount = 0
		return 0, nil, false
	}
	if state == nil {
		state = &repositoryRevocationState{}
		g.states[repository] = state
	}
	if !state.revoked {
		g.revokedCount++
	}
	g.generation++
	state.generation = g.generation
	state.durableToken = ""
	state.cleanupSafe = false
	state.revoked = true
	if state.active > 0 && state.drained == nil {
		state.drained = make(chan struct{})
	}
	return g.generation, state.drained, true
}

func (g *repositoryRevocationGate) finish(
	repository string,
	generation uint64,
	durableToken string,
	cleanupSafe bool,
) {
	if g == nil || repository == "" || generation == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failClosed {
		return
	}
	state, found := g.states[repository]
	if !found || !state.revoked || state.generation != generation {
		return
	}
	state.durableToken = durableToken
	state.cleanupSafe = cleanupSafe
}

func (g *repositoryRevocationGate) revoked(repository string) bool {
	if g == nil || repository == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failClosed {
		return true
	}
	state := g.states[repository]
	return state != nil && state.revoked
}

func (g *repositoryRevocationGate) ticket(
	repository string,
) repositoryRevocationTicket {
	ticket := repositoryRevocationTicket{repository: repository}
	if g == nil || repository == "" {
		return ticket
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failClosed {
		ticket.revoked = true
		return ticket
	}
	state := g.states[repository]
	if state != nil && state.revoked {
		ticket.generation = state.generation
		ticket.revoked = true
	}
	return ticket
}

func (g *repositoryRevocationGate) restorationCandidate(
	repository string,
	expectedGeneration uint64,
) (uint64, string, bool) {
	if g == nil || repository == "" || expectedGeneration == 0 {
		return 0, "", false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failClosed {
		return 0, "", false
	}
	state, found := g.states[repository]
	if !found ||
		!state.revoked ||
		!state.cleanupSafe ||
		state.active != 0 ||
		state.generation != expectedGeneration {
		return 0, "", false
	}
	return state.generation, state.durableToken, true
}

func (g *repositoryRevocationGate) restoreIfSafe(
	repository string,
	generation uint64,
	durableToken string,
) bool {
	if g == nil || repository == "" || generation == 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failClosed {
		return false
	}
	state, found := g.states[repository]
	if !found ||
		!state.revoked ||
		!state.cleanupSafe ||
		state.active != 0 ||
		state.generation != generation ||
		state.durableToken != durableToken {
		return false
	}
	delete(g.states, repository)
	g.revokedCount--
	return true
}

func (g *repositoryRevocationGate) cleanupRequired(
	repository string,
	expectedGeneration uint64,
) bool {
	if g == nil || repository == "" || expectedGeneration == 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failClosed {
		return false
	}
	state := g.states[repository]
	return state != nil &&
		state.revoked &&
		state.generation == expectedGeneration &&
		!state.cleanupSafe
}

func publicRepositoryRequest(request *http.Request) bool {
	return request != nil &&
		request.URL != nil &&
		upstreamAuthorization(request) == "" &&
		!hasCredentialQuery(request.URL.Query())
}

func repositoryRevocationStatus(
	status int,
	header http.Header,
	provenance responseProvenance,
) bool {
	if provenance != responseFromHubOrigin {
		return false
	}
	errorCode := strings.ToLower(strings.TrimSpace(header.Get("X-Error-Code")))
	errorCode = strings.NewReplacer("-", "", "_", "", " ", "").Replace(errorCode)
	switch errorCode {
	case "repositorynotfound", "reponotfound", "gatedrepo", "privaterepo":
		return true
	}
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

func (h *Handler) repositoryRevoked(repository huggingFaceRepository) bool {
	return h != nil &&
		h.revocations != nil &&
		h.revocations.revoked(repository.packageName)
}

func (h *Handler) loadRepositoryRevocations() {
	if h == nil || h.revocations == nil || h.revocationStore == nil {
		return
	}
	loadCtx, cancel := context.WithTimeout(
		context.Background(),
		repositoryCleanupTimeout,
	)
	markers, err := h.revocationStore.Load(loadCtx)
	cancel()
	if err != nil {
		h.revocations.failClose()
		zap.L().Error(
			"Hugging Face repository revocation state could not be restored; cache disabled",
			zap.Error(err),
		)
		return
	}
	for _, marker := range markers {
		if h.revocations.seed(
			marker.Repository,
			marker.Token,
			marker.CleanupSafe,
		) {
			continue
		}
		h.revocations.failClose()
		zap.L().Error(
			"Hugging Face repository revocation state is invalid; cache disabled",
			zap.String("repository", marker.Repository),
		)
		return
	}
	if len(markers) != 0 {
		zap.L().Warn(
			"restored Hugging Face repository revocation markers",
			zap.Int("repositories", len(markers)),
		)
	}
}

type repositoryRevocationAttempt struct {
	repository   huggingFaceRepository
	generation   uint64
	durableToken string
	drained      <-chan struct{}
	cleanupSafe  bool
	cleanupErr   error
}

func (h *Handler) revokeRepository(ctx context.Context, parsed Parsed) {
	attempt, started := h.beginRepositoryRevocation(ctx, parsed, 0, false)
	if !started {
		return
	}
	h.finishRepositoryRevocation(ctx, attempt)
}

func (h *Handler) revokeRepositoryAsync(ctx context.Context, parsed Parsed) {
	attempt, started := h.beginRepositoryRevocation(ctx, parsed, 0, false)
	if !started {
		return
	}
	go h.finishRepositoryRevocation(
		context.WithoutCancel(contextOrBackground(ctx)),
		attempt,
	)
}

func (h *Handler) retryRepositoryCleanup(
	ctx context.Context,
	parsed Parsed,
	expectedGeneration uint64,
) (uint64, bool) {
	attempt, started := h.beginRepositoryRevocation(
		ctx,
		parsed,
		expectedGeneration,
		true,
	)
	if !started {
		return 0, false
	}
	h.finishRepositoryRevocation(ctx, attempt)
	return attempt.generation, true
}

func (h *Handler) beginRepositoryRevocation(
	ctx context.Context,
	parsed Parsed,
	expectedGeneration uint64,
	requireExpected bool,
) (*repositoryRevocationAttempt, bool) {
	if h == nil {
		return nil, false
	}
	repository, ok := repositoryForParsed(parsed)
	if !ok {
		return nil, false
	}
	unlockBegin := func() {}
	if h.revocations != nil {
		// Keep the in-memory generation and its durable token in one order.
		// Cleanup itself stays outside this short per-repository critical section.
		unlockBegin = h.revocations.lockBegin(repository.packageName)
	}
	defer unlockBegin()

	generation := uint64(0)
	var drained <-chan struct{}
	if requireExpected && h.revocations == nil {
		return nil, false
	}
	if h.revocations != nil {
		// This is the linearization point: public requests stop consulting the
		// cache before the first sibling key or ref pin is deleted.
		if requireExpected {
			var started bool
			generation, drained, started = h.revocations.beginIfGeneration(
				repository.packageName,
				expectedGeneration,
			)
			if !started {
				return nil, false
			}
		} else {
			generation, drained = h.revocations.begin(repository.packageName)
		}
	}

	attempt := &repositoryRevocationAttempt{
		repository:  repository,
		generation:  generation,
		drained:     drained,
		cleanupSafe: true,
	}
	if h.revocationStore != nil {
		persistCtx, cancel := repositoryRevocationContext(ctx)
		var persistErr error
		attempt.durableToken, persistErr = h.revocationStore.Begin(
			persistCtx,
			repository.packageName,
			repository.escapedRepo,
		)
		cancel()
		if persistErr != nil {
			attempt.cleanupSafe = false
			attempt.cleanupErr = errors.Join(
				attempt.cleanupErr,
				fmt.Errorf("persist repository revocation marker: %w", persistErr),
			)
		}
	}
	return attempt, true
}

func (h *Handler) finishRepositoryRevocation(
	ctx context.Context,
	attempt *repositoryRevocationAttempt,
) {
	if h == nil || attempt == nil {
		return
	}
	repository := attempt.repository
	if attempt.drained != nil {
		waitCtx, cancel := repositoryRevocationContext(ctx)
		select {
		case <-attempt.drained:
		case <-waitCtx.Done():
			attempt.cleanupSafe = false
			attempt.cleanupErr = errors.Join(
				attempt.cleanupErr,
				fmt.Errorf("wait for admitted repository cache requests: %w", waitCtx.Err()),
			)
		}
		cancel()
	}
	if h.cacheMgr != nil {
		result, err := h.cacheMgr.InvalidatePackage(
			ctx,
			"huggingface",
			repository.packageName,
		)
		attempt.cleanupSafe = attempt.cleanupSafe && result.SafeToRestore
		attempt.cleanupErr = errors.Join(attempt.cleanupErr, err)
	}
	if h.db != nil {
		cleanupCtx, cancel := repositoryRevocationContext(ctx)
		pinErr := h.db.WithContext(cleanupCtx).
			Where("key LIKE ? ESCAPE '!'", escapeSQLLike(
				"huggingface/"+repository.escapedRepo+"/ref/",
			)+"%").
			Delete(&db.HuggingFaceRefPin{}).Error
		cancel()
		if pinErr != nil {
			attempt.cleanupSafe = false
			attempt.cleanupErr = errors.Join(
				attempt.cleanupErr,
				fmt.Errorf("delete repository ref pins: %w", pinErr),
			)
		}
	}
	if h.revocationStore != nil &&
		attempt.durableToken != "" &&
		attempt.cleanupSafe {
		persistCtx, cancel := repositoryRevocationContext(ctx)
		markedSafe, persistErr := h.revocationStore.MarkCleanupSafe(
			persistCtx,
			repository.packageName,
			attempt.durableToken,
		)
		cancel()
		if persistErr != nil {
			attempt.cleanupSafe = false
			attempt.cleanupErr = errors.Join(
				attempt.cleanupErr,
				fmt.Errorf("mark repository revocation cleanup safe: %w", persistErr),
			)
		} else if !markedSafe {
			// Another revocation generation superseded this cleanup. Its own
			// generation remains responsible for proving that cleanup is safe.
			attempt.cleanupSafe = false
		}
	}
	if h.revocations != nil {
		h.revocations.finish(
			repository.packageName,
			attempt.generation,
			attempt.durableToken,
			attempt.cleanupSafe,
		)
	}

	if attempt.cleanupErr != nil {
		zap.L().Error("Hugging Face repository revocation cleanup failed closed",
			zap.String("repository", repository.packageName),
			zap.Bool("safe_to_restore", attempt.cleanupSafe),
			zap.Error(attempt.cleanupErr),
		)
	}
}

func (h *Handler) observeDirectRepositoryResponse(
	request *http.Request,
	target string,
	response *resolvedResponse,
	ticket repositoryRevocationTicket,
) {
	if h == nil || response == nil || !publicRepositoryRequest(request) {
		return
	}
	path, rawQuery, _ := strings.Cut(target, "?")
	parsed := ParseRequestPath(path)
	repository, ok := repositoryForParsed(parsed)
	if !ok {
		return
	}

	if repositoryRevocationStatus(
		response.StatusCode,
		response.Header,
		response.provenance,
	) {
		h.revokeRepository(request.Context(), parsed)
		return
	}
	if response.StatusCode == http.StatusNotFound && h.cacheMgr != nil {
		cacheKey, err := CacheKeyForRawQuery(parsed, rawQuery)
		if err == nil && cacheKey != "" {
			if err := h.cacheMgr.Invalidate(request.Context(), cacheKey); err != nil {
				zap.L().Error("Hugging Face direct not-found cleanup failed closed",
					zap.String("key", cacheKey),
					zap.Error(err),
				)
			}
		}
	}
	if response.StatusCode >= http.StatusOK &&
		response.StatusCode < http.StatusMultipleChoices &&
		h.revocations != nil &&
		ticket.revoked &&
		ticket.generation != 0 &&
		ticket.repository == repository.packageName {
		if h.revocations.cleanupRequired(
			repository.packageName,
			ticket.generation,
		) {
			generation, retried := h.retryRepositoryCleanup(
				request.Context(),
				parsed,
				ticket.generation,
			)
			if !retried {
				return
			}
			ticket.generation = generation
		}
		generation, durableToken, safe := h.revocations.restorationCandidate(
			repository.packageName,
			ticket.generation,
		)
		if safe {
			durableCleared := true
			if h.revocationStore != nil {
				durableCleared = false
				persistCtx, cancel := repositoryRevocationContext(request.Context())
				cleared, err := h.revocationStore.DeleteCleanupSafe(
					persistCtx,
					repository.packageName,
					durableToken,
				)
				cancel()
				if err != nil {
					zap.L().Error(
						"Hugging Face repository revocation marker removal failed closed",
						zap.String("repository", repository.packageName),
						zap.Error(err),
					)
				} else {
					durableCleared = cleared
				}
			}
			if durableCleared {
				h.revocations.restoreIfSafe(
					repository.packageName,
					generation,
					durableToken,
				)
			}
		}
	}
}

func (h *Handler) directRepositoryRevocationTicket(
	request *http.Request,
	target string,
) repositoryRevocationTicket {
	if h == nil ||
		h.revocations == nil ||
		!publicRepositoryRequest(request) {
		return repositoryRevocationTicket{}
	}
	path, _, _ := strings.Cut(target, "?")
	repository, ok := repositoryForParsed(ParseRequestPath(path))
	if !ok {
		return repositoryRevocationTicket{}
	}
	return h.revocations.ticket(repository.packageName)
}

func repositoryRevocationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.WithoutCancel(contextOrBackground(ctx)),
		repositoryCleanupTimeout,
	)
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func escapeSQLLike(value string) string {
	value = strings.ReplaceAll(value, "!", "!!")
	value = strings.ReplaceAll(value, "%", "!%")
	return strings.ReplaceAll(value, "_", "!_")
}
