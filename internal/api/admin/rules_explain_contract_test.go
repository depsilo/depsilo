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
		Allowed    bool   `json:"allowed"`
		Precedence string `json:"precedence_reason"`
		Winner     *struct {
			Rule db.PackageRule `json:"rule"`
		} `json:"winner"`
		Candidates []struct {
			Rule        db.PackageRule        `json:"rule"`
			Specificity rules.RuleSpecificity `json:"specificity"`
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
	if payload.Precedence != "version_specificity" {
		t.Fatalf("precedence_reason = %q, want version_specificity", payload.Precedence)
	}
	if len(payload.Candidates) != len(seed) {
		t.Fatalf("candidate count = %d, want %d", len(payload.Candidates), len(seed))
	}
	winner := payload.Candidates[0]
	if !winner.Selected || winner.Rule.ID != seed[2].ID {
		t.Fatalf("winner candidate = %+v, want selected rule %d", winner, seed[2].ID)
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

func TestRulesTestExplainDenyTiePrecedesID(t *testing.T) {
	router, database := newRulesAdminTestRouter(t)
	store := rules.NewStore(database)
	deny := db.PackageRule{Ecosystem: "npm", PackageName: "fixture", Version: "1.0.0", Action: "deny", Reason: "deny tie"}
	allow := db.PackageRule{Ecosystem: "npm", PackageName: "fixture", Version: "1.0.0", Action: "allow", Reason: "newer allow"}
	if err := store.Create(&deny); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&allow); err != nil {
		t.Fatal(err)
	}
	if allow.ID <= deny.ID {
		t.Fatalf("fixture IDs = deny %d allow %d, want allow newer", deny.ID, allow.ID)
	}

	response := rulesAdminRequest(router, http.MethodPost, "/rules/test", `{"ecosystem":"npm","package":"fixture","version":"1.0.0"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("test status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Allowed     bool            `json:"allowed"`
		Precedence  string          `json:"precedence_reason"`
		WinningRule *db.PackageRule `json:"winning_rule"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode explain response: %v", err)
	}
	if payload.Allowed || payload.WinningRule == nil || payload.WinningRule.ID != deny.ID {
		t.Fatalf("deny tie result = %+v, want deny rule %d", payload, deny.ID)
	}
	if payload.Precedence != "deny_tie_break" {
		t.Fatalf("precedence_reason = %q, want deny_tie_break", payload.Precedence)
	}
}

func TestRulesTestExplainIDTieIsStableForSameAction(t *testing.T) {
	router, database := newRulesAdminTestRouter(t)
	store := rules.NewStore(database)
	older := db.PackageRule{Ecosystem: "npm", PackageName: "fixture", Version: "1.0.0", Action: "allow", Reason: "older"}
	newer := db.PackageRule{Ecosystem: "npm", PackageName: "fixture", Version: "1.0.0", Action: "allow", Reason: "newer"}
	if err := store.Create(&older); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&newer); err != nil {
		t.Fatal(err)
	}

	response := rulesAdminRequest(router, http.MethodPost, "/rules/test", `{"ecosystem":"npm","package":"fixture","version":"1.0.0"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("test status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Allowed     bool            `json:"allowed"`
		Precedence  string          `json:"precedence_reason"`
		WinningRule *db.PackageRule `json:"winning_rule"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode explain response: %v", err)
	}
	if !payload.Allowed || payload.WinningRule == nil || payload.WinningRule.ID != newer.ID {
		t.Fatalf("ID tie result = %+v, want newer rule %d", payload, newer.ID)
	}
	if payload.Precedence != "id_tie_break" {
		t.Fatalf("precedence_reason = %q, want id_tie_break", payload.Precedence)
	}
}
