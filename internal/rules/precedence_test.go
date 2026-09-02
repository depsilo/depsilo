package rules

import (
	"context"
	"testing"

	"depsilo/internal/db"
)

func TestRuleSpecificityCompareUsesLexicographicTuple(t *testing.T) {
	t.Parallel()

	base := RuleSpecificity{Priority: 0, Ecosystem: 2, Package: 2, Version: 2, Action: 0, ID: 10}
	tests := []struct {
		name  string
		other RuleSpecificity
		want  int
	}{
		{
			name:  "priority dominates all selector dimensions",
			other: RuleSpecificity{Priority: 1, Ecosystem: 1, Package: 1, Version: 1, Action: 1, ID: 1},
			want:  -1,
		},
		{
			name:  "ecosystem dominates package and version",
			other: RuleSpecificity{Priority: 0, Ecosystem: 1, Package: 2, Version: 2, Action: 1, ID: 99},
			want:  1,
		},
		{
			name:  "package dominates version",
			other: RuleSpecificity{Priority: 0, Ecosystem: 2, Package: 1, Version: 2, Action: 1, ID: 99},
			want:  1,
		},
		{
			name:  "version dominates action",
			other: RuleSpecificity{Priority: 0, Ecosystem: 2, Package: 2, Version: 1, Action: 1, ID: 99},
			want:  1,
		},
		{
			name:  "deny action tie break",
			other: RuleSpecificity{Priority: 0, Ecosystem: 2, Package: 2, Version: 2, Action: 1, ID: 1},
			want:  -1,
		},
		{
			name:  "higher id is final tie break",
			other: RuleSpecificity{Priority: 0, Ecosystem: 2, Package: 2, Version: 2, Action: 0, ID: 11},
			want:  -1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := base.Compare(test.other); got != test.want {
				t.Fatalf("Compare(%+v, %+v) = %d, want %d", base, test.other, got, test.want)
			}
			if got := test.other.Compare(base); got != -test.want {
				t.Fatalf("reverse Compare(%+v, %+v) = %d, want %d", test.other, base, got, -test.want)
			}
		})
	}
	if got := base.Compare(base); got != 0 {
		t.Fatalf("self Compare = %d, want 0", got)
	}
}

func TestEngineUsesStructuredSpecificityInsteadOfAdditiveScore(t *testing.T) {
	database := newRulesTestDB(t)
	store := NewStore(database)

	// Both rules have the historical scalar score 5:
	//   exact ecosystem + exact package + range version = 2+2+1
	//   exact ecosystem + prefix package + exact version = 2+1+2
	// The explicit tuple compares package specificity before version
	// specificity, so the exact-package rule must win regardless of insertion
	// order. Give the less-specific rule the deny action to ensure the action
	// tie-break cannot mask the selector ordering.
	exactPackage := db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: ">= 1.0.0",
		Action: "allow", Reason: "exact package range",
	}
	prefixPackage := db.PackageRule{
		Ecosystem: "pypi", PackageName: "req*", Version: "1.0.0",
		Action: "deny", Reason: "prefix exact",
	}
	if err := store.Create(&prefixPackage); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&exactPackage); err != nil {
		t.Fatal(err)
	}

	allowed, matched, err := NewEngine(store, nil).Check(context.Background(), "pypi", "requests", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed || matched == nil || matched.ID != exactPackage.ID {
		t.Fatalf("structured winner = allowed %v matched %+v, want exact-package allow %d", allowed, matched, exactPackage.ID)
	}
}

func TestEngineDenyWinsEqualSpecificityBeforeID(t *testing.T) {
	database := newRulesTestDB(t)
	store := NewStore(database)
	olderDeny := db.PackageRule{
		Ecosystem: "npm", PackageName: "fixture", Version: "1.0.0", Action: "deny", Reason: "deny tie-break",
	}
	newerAllow := db.PackageRule{
		Ecosystem: "npm", PackageName: "fixture", Version: "1.0.0", Action: "allow", Reason: "newer allow",
	}
	if err := store.Create(&olderDeny); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&newerAllow); err != nil {
		t.Fatal(err)
	}
	if newerAllow.ID <= olderDeny.ID {
		t.Fatalf("fixture IDs = deny %d allow %d, want newer allow ID", olderDeny.ID, newerAllow.ID)
	}

	allowed, matched, err := NewEngine(store, nil).Check(context.Background(), "npm", "fixture", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if allowed || matched == nil || matched.ID != olderDeny.ID {
		t.Fatalf("equal-specificity winner = allowed %v matched %+v, want older deny %d", allowed, matched, olderDeny.ID)
	}
}

func TestEngineIDTieBreakIsIndependentOfSnapshotOrder(t *testing.T) {
	database := newRulesTestDB(t)
	store := NewStore(database)
	older := db.PackageRule{
		Ecosystem: "npm", PackageName: "fixture", Version: "1.0.0", Action: "allow", Reason: "older",
	}
	newer := db.PackageRule{
		Ecosystem: "npm", PackageName: "fixture", Version: "1.0.0", Action: "allow", Reason: "newer",
	}
	if err := store.Create(&older); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&newer); err != nil {
		t.Fatal(err)
	}
	first, err := NewEngine(store, nil).Explain(context.Background(), "npm", "fixture", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if first.MatchedRule == nil || first.MatchedRule.ID != newer.ID || first.PrecedenceReason != "id_tie_break" {
		t.Fatalf("normal-order winner = %+v, precedence=%q; want ID %d/id_tie_break", first.MatchedRule, first.PrecedenceReason, newer.ID)
	}

	// Recompile the same persisted rows in the opposite order. The winner must
	// remain the higher-ID rule rather than whichever row happens to be first.
	compiledOlder, err := compilePersistedRule(older)
	if err != nil {
		t.Fatal(err)
	}
	compiledNewer, err := compilePersistedRule(newer)
	if err != nil {
		t.Fatal(err)
	}
	second, err := evaluateCompiledRules([]compiledRule{compiledNewer, compiledOlder}, "npm", "fixture", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if second.MatchedRule == nil || second.MatchedRule.ID != newer.ID {
		t.Fatalf("reversed-order winner = %+v, want ID %d", second.MatchedRule, newer.ID)
	}
}

func TestEngineEcosystemSpecificityPrecedesVersionSpecificity(t *testing.T) {
	database := newRulesTestDB(t)
	store := NewStore(database)
	global := db.PackageRule{
		Ecosystem: "*", PackageName: "*", Version: "*", Action: "deny", Reason: "global fallback",
	}
	concrete := db.PackageRule{
		Ecosystem: "npm", PackageName: "*", Version: "*", Action: "allow", Reason: "npm policy",
	}
	// Insert the concrete rule first so this assertion does not rely on the
	// Store's created_at ordering. Exact ecosystem specificity must win over a
	// global rule before action or ID are considered.
	if err := store.Create(&concrete); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&global); err != nil {
		t.Fatal(err)
	}
	allowed, matched, err := NewEngine(store, nil).Check(context.Background(), "npm", "fixture", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed || matched == nil || matched.ID != concrete.ID {
		t.Fatalf("ecosystem winner = allowed %v matched %+v, want concrete allow %d", allowed, matched, concrete.ID)
	}
}

func TestEngineExplainReturnsStableWinningCandidates(t *testing.T) {
	database := newRulesTestDB(t)
	store := NewStore(database)
	rules := []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "*", Version: "*", Action: "deny", Reason: "global package fallback"},
		{Ecosystem: "pypi", PackageName: "requests", Version: ">= 1.0.0", Action: "allow", Reason: "approved releases"},
		{Ecosystem: "pypi", PackageName: "requests", Version: "1.0.0", Action: "deny", Reason: "pinned incident"},
	}
	for index := range rules {
		if err := store.Create(&rules[index]); err != nil {
			t.Fatal(err)
		}
	}

	evaluation, err := NewEngine(store, nil).Explain(context.Background(), "pypi", "requests", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Allowed || evaluation.MatchedRule == nil || evaluation.WinningRule == nil {
		t.Fatalf("evaluation = %+v, want deny with winner", evaluation)
	}
	if evaluation.MatchedRule.ID != rules[2].ID || evaluation.WinningRule.ID != rules[2].ID {
		t.Fatalf("winner IDs = matched %d winning %d, want %d", evaluation.MatchedRule.ID, evaluation.WinningRule.ID, rules[2].ID)
	}
	if evaluation.PrecedenceReason != "version_specificity" {
		t.Fatalf("precedence reason = %q, want version_specificity", evaluation.PrecedenceReason)
	}
	if len(evaluation.Candidates) != 3 {
		t.Fatalf("candidate count = %d, want 3", len(evaluation.Candidates))
	}
	if !evaluation.Candidates[0].Selected || !evaluation.Candidates[0].Matched {
		t.Fatalf("first candidate = %+v, want selected matched", evaluation.Candidates[0])
	}
	if evaluation.Candidates[0].Rule.ID != rules[2].ID {
		t.Fatalf("first candidate ID = %d, want %d", evaluation.Candidates[0].Rule.ID, rules[2].ID)
	}
	if evaluation.Candidates[0].MatchLevels.Package != "exact" || evaluation.Candidates[0].MatchLevels.Version != "exact" {
		t.Fatalf("winner match levels = %+v, want exact/exact", evaluation.Candidates[0].MatchLevels)
	}
	for index := 1; index < len(evaluation.Candidates); index++ {
		if evaluation.Candidates[index].Selected {
			t.Fatalf("candidate %d unexpectedly selected", index)
		}
		if evaluation.Candidates[index-1].Specificity.Compare(evaluation.Candidates[index].Specificity) < 0 {
			t.Fatalf("candidate order is not descending at %d: %+v then %+v", index, evaluation.Candidates[index-1], evaluation.Candidates[index])
		}
	}
}

func TestEngineExplainNoMatchHasNoCandidates(t *testing.T) {
	database := newRulesTestDB(t)
	store := NewStore(database)
	if err := store.Create(&db.PackageRule{
		Ecosystem: "pypi", PackageName: "requests", Version: "*", Action: "deny",
	}); err != nil {
		t.Fatal(err)
	}
	evaluation, err := NewEngine(store, nil).Explain(context.Background(), "pypi", "flask", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !evaluation.Allowed || evaluation.MatchedRule != nil || len(evaluation.Candidates) != 0 || evaluation.PrecedenceReason != "default_allow" {
		t.Fatalf("no-match evaluation = %+v, want allow and no candidates", evaluation)
	}
}

func TestIncompleteArtifactFallbackUsesDenyTieBreak(t *testing.T) {
	database := newRulesTestDB(t)
	store := NewStore(database)
	// Both rules are package/version wildcards for the same ecosystem. The
	// older deny must remain the deterministic fallback even though the newer
	// allow has the larger ID: action is compared before ID in the tuple.
	deny := db.PackageRule{Ecosystem: "pypi", PackageName: "*", Version: "*", Action: "deny"}
	allow := db.PackageRule{Ecosystem: "pypi", PackageName: "*", Version: "*", Action: "allow"}
	if err := store.Create(&deny); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&allow); err != nil {
		t.Fatal(err)
	}
	allowed, matched, err := NewEngine(store, nil).CheckIncompleteArtifact(context.Background(), "pypi")
	if err != nil {
		t.Fatal(err)
	}
	if allowed || matched == nil || matched.ID != deny.ID {
		t.Fatalf("incomplete fallback = allowed %v matched %+v, want deny rule %d", allowed, matched, deny.ID)
	}
}

func TestEvaluateCompiledRulesIsIndependentOfSnapshotOrder(t *testing.T) {
	database := newRulesTestDB(t)
	store := NewStore(database)
	ruleModels := []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "requests", Version: ">= 1.0.0", Action: "allow"},
		{Ecosystem: "pypi", PackageName: "req*", Version: "1.0.0", Action: "deny"},
	}
	compiled := make([]compiledRule, 0, len(ruleModels))
	for index := range ruleModels {
		if err := store.Create(&ruleModels[index]); err != nil {
			t.Fatal(err)
		}
		candidate, err := compilePersistedRule(ruleModels[index])
		if err != nil {
			t.Fatal(err)
		}
		compiled = append(compiled, candidate)
	}

	first, err := evaluateCompiledRules(compiled, "pypi", "requests", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	compiled[0], compiled[1] = compiled[1], compiled[0]
	second, err := evaluateCompiledRules(compiled, "pypi", "requests", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if first.MatchedRule == nil || second.MatchedRule == nil || first.MatchedRule.ID != second.MatchedRule.ID {
		t.Fatalf("snapshot-order winners = %+v / %+v, want same rule", first.MatchedRule, second.MatchedRule)
	}
	if first.MatchedRule.ID != ruleModels[0].ID || !first.Allowed {
		t.Fatalf("snapshot-order winner = allowed %v rule %+v, want exact-package allow %d", first.Allowed, first.MatchedRule, ruleModels[0].ID)
	}
}

func FuzzRuleSpecificityComparatorProperties(f *testing.F) {
	seeds := []struct {
		a RuleSpecificity
		b RuleSpecificity
		c RuleSpecificity
	}{
		{
			a: RuleSpecificity{Priority: 0, Ecosystem: 1, Package: 0, Version: 0, Action: 0, ID: 1},
			b: RuleSpecificity{Priority: 0, Ecosystem: 2, Package: 1, Version: 0, Action: 0, ID: 2},
			c: RuleSpecificity{Priority: 0, Ecosystem: 2, Package: 2, Version: 2, Action: 1, ID: 3},
		},
		{
			a: RuleSpecificity{Priority: -1, Ecosystem: 2, Package: 2, Version: 2, Action: 1, ID: 10},
			b: RuleSpecificity{Priority: 0, Ecosystem: 0, Package: 0, Version: 0, Action: 0, ID: 0},
			c: RuleSpecificity{Priority: 1, Ecosystem: 0, Package: 0, Version: 0, Action: 0, ID: 0},
		},
	}
	for _, seed := range seeds {
		f.Add(seed.a.Priority, seed.a.Ecosystem, seed.a.Package, seed.a.Version, seed.a.Action, seed.a.ID,
			seed.b.Priority, seed.b.Ecosystem, seed.b.Package, seed.b.Version, seed.b.Action, seed.b.ID,
			seed.c.Priority, seed.c.Ecosystem, seed.c.Package, seed.c.Version, seed.c.Action, seed.c.ID)
	}
	f.Fuzz(func(t *testing.T,
		ap, ae, ak, av, aa int, aid uint,
		bp, be, bk, bv, ba int, bid uint,
		cp, ce, ck, cv, ca int, cid uint,
	) {
		a := RuleSpecificity{Priority: ap, Ecosystem: ae, Package: ak, Version: av, Action: aa, ID: aid}
		b := RuleSpecificity{Priority: bp, Ecosystem: be, Package: bk, Version: bv, Action: ba, ID: bid}
		c := RuleSpecificity{Priority: cp, Ecosystem: ce, Package: ck, Version: cv, Action: ca, ID: cid}
		ab := ruleCompareSign(a.Compare(b))
		baSign := ruleCompareSign(b.Compare(a))
		if ab != -baSign {
			t.Fatalf("antisymmetry violated: a=%+v b=%+v ab=%d ba=%d", a, b, ab, baSign)
		}
		if a.Compare(a) != 0 || b.Compare(b) != 0 || c.Compare(c) != 0 {
			t.Fatalf("reflexivity violated: a=%+v b=%+v c=%+v", a, b, c)
		}
		if a.Compare(b) > 0 && b.Compare(c) > 0 && a.Compare(c) <= 0 {
			t.Fatalf("transitivity violated: a=%+v b=%+v c=%+v", a, b, c)
		}
	})
}

func ruleCompareSign(value int) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}
