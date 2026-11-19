package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"depsilo/internal/db"
	"depsilo/internal/rules"
)

// TestRulesTestStructuredSpecificityContract keeps the wire representation of
// the explain endpoint independent from the implementation's internal scalar
// score.  In particular, the selector dimensions are lexicographic and the
// response must expose the action/ID tie-break dimensions as well.
func TestRulesTestStructuredSpecificityContract(t *testing.T) {
	router, database := newRulesAdminTestRouter(t)
	store := rules.NewStore(database)
	seed := []db.PackageRule{
		{Ecosystem: "pypi", PackageName: "*", Version: "*", Action: "deny", Reason: "baseline"},
		{Ecosystem: "pypi", PackageName: "requests", Version: ">= 1.0.0", Action: "allow", Reason: "range"},
		{Ecosystem: "pypi", PackageName: "requests", Version: "1.0.0", Action: "deny", Reason: "exact"},
	}
	for index := range seed {
		if err := store.Create(&seed[index]); err != nil {
			t.Fatalf("create seed rule %d: %v", index, err)
		}
	}

	response := rulesAdminRequest(router, http.MethodPost, "/rules/test", `{"ecosystem":"pypi","package":"requests","version":"1.0.0"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("test status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Allowed      bool            `json:"allowed"`
		Precedence   string          `json:"precedence_reason"`
		MatchedRule  *db.PackageRule `json:"matched_rule"`
		WinningRule  *db.PackageRule `json:"winning_rule"`
		Reason       string          `json:"reason"`
		WinnerReason string          `json:"winner_reason"`
		Winner       *struct {
			Rule db.PackageRule `json:"rule"`
		} `json:"winner"`
		Candidates []struct {
			Rule        db.PackageRule        `json:"rule"`
			Specificity rules.RuleSpecificity `json:"specificity"`
			MatchLevels rules.RuleMatchLevels `json:"match_levels"`
			Matched     bool                  `json:"matched"`
			Selected    bool                  `json:"selected"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode explain response: %v", err)
	}
	if payload.Allowed {
		t.Fatal("exact deny winner unexpectedly allowed")
	}
	if payload.Winner == nil || payload.Winner.Rule.ID != seed[2].ID {
		t.Fatalf("winner payload = %+v, want rule %d", payload.Winner, seed[2].ID)
	}
	if payload.MatchedRule == nil || payload.WinningRule == nil ||
		payload.MatchedRule.ID != seed[2].ID || payload.WinningRule.ID != seed[2].ID {
		t.Fatalf("matched=%+v winning=%+v, want rule %d", payload.MatchedRule, payload.WinningRule, seed[2].ID)
	}
	if payload.Reason != "exact" || payload.WinnerReason != "exact" {
		t.Fatalf("decision reasons = reason %q winner_reason %q", payload.Reason, payload.WinnerReason)
	}
	if payload.Precedence != "version_specificity" {
		t.Fatalf("precedence_reason = %q, want version_specificity", payload.Precedence)
	}
	if len(payload.Candidates) != len(seed) {
		t.Fatalf("candidate count = %d, want %d", len(payload.Candidates), len(seed))
	}
	winner := payload.Candidates[0]
	if !winner.Selected || !winner.Matched || winner.Rule.ID != seed[2].ID {
		t.Fatalf("winner candidate = %+v, want selected rule %d", winner, seed[2].ID)
	}
	if winner.MatchLevels.Package != "exact" || winner.MatchLevels.Version != "exact" {
		t.Fatalf("winner match levels = %+v, want exact package/version", winner.MatchLevels)
	}
	for index := 1; index < len(payload.Candidates); index++ {
		if payload.Candidates[index].Selected {
			t.Fatalf("candidate %d is unexpectedly selected", index)
		}
		if payload.Candidates[index-1].Specificity.Compare(payload.Candidates[index].Specificity) < 0 {
			t.Fatalf("candidate order is not descending at %d", index)
		}
	}
	wantWinner := rules.RuleSpecificity{
		Priority: 0, Ecosystem: 2, Package: 2, Version: 2, Action: 1, ID: seed[2].ID,
	}
	if winner.Specificity != wantWinner {
		t.Fatalf("winner specificity = %+v, want %+v", winner.Specificity, wantWinner)
	}
	// The range and package-wide candidates make sure each lower selector
	// dimension survives JSON encoding rather than being collapsed to a sum.
	if got := payload.Candidates[1].Specificity; got.Ecosystem != 2 || got.Package != 2 || got.Version != 1 || got.Action != 0 {
		t.Fatalf("range specificity = %+v, want E2/K2/V1/A0", got)
	}
	if got := payload.Candidates[2].Specificity; got.Ecosystem != 2 || got.Package != 0 || got.Version != 0 || got.Action != 1 {
		t.Fatalf("wildcard specificity = %+v, want E2/K0/V0/A1", got)
	}
}
