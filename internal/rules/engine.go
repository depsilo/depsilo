package rules

import (
	"context"
	"strings"
	"sync"
	"time"

	"depsilo/internal/db"
	"depsilo/internal/license"
)

// Engine evaluates package rules with an in-memory cache.
type Engine struct {
	store    *Store
	licMgr   *license.Manager
	mu       sync.RWMutex
	cache    []db.PackageRule
	lastLoad time.Time
	cacheTTL time.Duration
}

// NewEngine creates a new rules Engine.
func NewEngine(store *Store, licMgr *license.Manager) *Engine {
	return &Engine{
		store:    store,
		licMgr:   licMgr,
		cacheTTL: 30 * time.Second,
	}
}

// Check returns (allowed, matchedRule, error).
// Community edition always returns allowed=true.
func (e *Engine) Check(ctx context.Context, ecosystem, packageName, version string) (bool, *db.PackageRule, error) {
	if !e.licMgr.IsPro() {
		return true, nil, nil
	}

	rules, err := e.loadRules()
	if err != nil {
		return true, nil, err // fail open
	}

	// Find best matching rule
	var bestRule *db.PackageRule
	bestScore := -1

	for i := range rules {
		r := &rules[i]
		score := matchScore(r, ecosystem, packageName, version)
		if score > bestScore {
			bestScore = score
			bestRule = r
		}
	}

	if bestRule == nil {
		return true, nil, nil // no rule matches, default allow
	}

	allowed := bestRule.Action == "allow"
	return allowed, bestRule, nil
}

// InvalidateCache forces a reload on next Check.
func (e *Engine) InvalidateCache() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastLoad = time.Time{}
}

func (e *Engine) loadRules() ([]db.PackageRule, error) {
	e.mu.RLock()
	if time.Since(e.lastLoad) < e.cacheTTL && e.cache != nil {
		defer e.mu.RUnlock()
		return e.cache, nil
	}
	e.mu.RUnlock()

	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock
	if time.Since(e.lastLoad) < e.cacheTTL && e.cache != nil {
		return e.cache, nil
	}

	rules, err := e.store.List()
	if err != nil {
		return nil, err
	}
	e.cache = rules
	e.lastLoad = time.Now()
	return rules, nil
}

// matchScore returns -1 if rule doesn't match, higher score = better match.
// Scoring: ecosystem exact=2, *=1; package exact=2, prefix=1; version exact=2, range=1, *=0
func matchScore(r *db.PackageRule, ecosystem, packageName, version string) int {
	score := 0

	// Ecosystem match
	if r.Ecosystem == "*" {
		score += 1
	} else if strings.EqualFold(r.Ecosystem, ecosystem) {
		score += 2
	} else {
		return -1 // no match
	}

	// Package name match
	if r.PackageName == "*" {
		score += 0
	} else if strings.HasSuffix(r.PackageName, "*") {
		prefix := strings.TrimSuffix(r.PackageName, "*")
		if strings.HasPrefix(strings.ToLower(packageName), strings.ToLower(prefix)) {
			score += 1
		} else {
			return -1
		}
	} else if strings.EqualFold(r.PackageName, packageName) {
		score += 2
	} else {
		return -1
	}

	// Version match
	if r.Version == "*" || version == "" {
		score += 0
	} else if matchVersion(r.Version, version) {
		score += 1
	} else {
		return -1
	}

	return score
}

// matchVersion handles version comparison.
// Supports: "*", exact match, "< X", "<= X", "> X", ">= X"
func matchVersion(ruleVersion, actual string) bool {
	ruleVersion = strings.TrimSpace(ruleVersion)
	actual = strings.TrimSpace(actual)

	if ruleVersion == "*" || ruleVersion == "" {
		return true
	}

	// Check for comparison operators
	var op string
	var target string
	for _, prefix := range []string{"<=", ">=", "<", ">"} {
		if strings.HasPrefix(ruleVersion, prefix) {
			op = prefix
			target = strings.TrimSpace(ruleVersion[len(prefix):])
			break
		}
	}

	if op == "" {
		// Exact match
		return strings.EqualFold(ruleVersion, actual)
	}

	cmp := compareVersions(actual, target)
	switch op {
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	}
	return false
}

// compareVersions does a basic version comparison.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareVersions(a, b string) int {
	// Strip leading 'v' if present
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")

	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}

	for i := 0; i < maxLen; i++ {
		var numA, numB int
		if i < len(partsA) {
			numA = parseVersionPart(partsA[i])
		}
		if i < len(partsB) {
			numB = parseVersionPart(partsB[i])
		}
		if numA < numB {
			return -1
		}
		if numA > numB {
			return 1
		}
	}
	return 0
}

func parseVersionPart(s string) int {
	// Extract leading digits
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			break
		}
	}
	return n
}
