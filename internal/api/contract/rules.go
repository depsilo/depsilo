package contract

import "time"

// RuleAction is what a Package Rule does to a matching request.
type RuleAction string

const (
	RuleActionAllow RuleAction = "allow"
	RuleActionDeny  RuleAction = "deny"
)

// RuleRecord is one persisted Package Rule. Ecosystem, PackageName, and
// Version are selectors: "*" matches anything, a trailing "*" on the package
// is a prefix, and the version may be exact or a single comparison.
type RuleRecord struct {
	ID          uint       `json:"id"`
	Ecosystem   string     `json:"ecosystem"`
	PackageName string     `json:"package_name"`
	Version     string     `json:"version"`
	Action      RuleAction `json:"action"`
	Reason      string     `json:"reason"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// RuleListResponse answers GET /admin/rules, newest rule first.
type RuleListResponse struct {
	Items []RuleRecord `json:"items"`
	Total int          `json:"total"`
}

// RuleTestSpecificity is a candidate's precedence tuple, compared
// lexicographically in field order; the larger tuple wins.
type RuleTestSpecificity struct {
	Priority  int `json:"priority"`
	Ecosystem int `json:"ecosystem"`
	Package   int `json:"package"`
	Version   int `json:"version"`
	// Action is the tie-breaker: deny is 1, allow is 0.
	Action int  `json:"action"`
	ID     uint `json:"id"`
}

// RuleTestEcosystemMatchLevel is how a rule's ecosystem selector matched.
type RuleTestEcosystemMatchLevel string

const (
	RuleTestEcosystemMatchLevelExact    RuleTestEcosystemMatchLevel = "exact"
	RuleTestEcosystemMatchLevelWildcard RuleTestEcosystemMatchLevel = "wildcard"
)

// RuleTestPackageMatchLevel is how a rule's package selector matched.
type RuleTestPackageMatchLevel string

const (
	RuleTestPackageMatchLevelExact    RuleTestPackageMatchLevel = "exact"
	RuleTestPackageMatchLevelPrefix   RuleTestPackageMatchLevel = "prefix"
	RuleTestPackageMatchLevelWildcard RuleTestPackageMatchLevel = "wildcard"
)

// RuleTestVersionMatchLevel is how a rule's version selector matched.
type RuleTestVersionMatchLevel string

const (
	RuleTestVersionMatchLevelExact    RuleTestVersionMatchLevel = "exact"
	RuleTestVersionMatchLevelRange    RuleTestVersionMatchLevel = "range"
	RuleTestVersionMatchLevelWildcard RuleTestVersionMatchLevel = "wildcard"
)

// RuleTestMatchLevels names the selector shape behind a candidate's
// specificity tuple.
type RuleTestMatchLevels struct {
	Ecosystem RuleTestEcosystemMatchLevel `json:"ecosystem"`
	Package   RuleTestPackageMatchLevel   `json:"package"`
	Version   RuleTestVersionMatchLevel   `json:"version"`
}

// RuleTestCandidate is a persisted rule that matched the test coordinate,
// with its ranking. Explanation is a language-neutral diagnostic; the Admin
// localizes from MatchLevels and Specificity instead.
type RuleTestCandidate struct {
	Rule        RuleRecord          `json:"rule"`
	Specificity RuleTestSpecificity `json:"specificity"`
	MatchLevels RuleTestMatchLevels `json:"match_levels"`
	Matched     bool                `json:"matched"`
	Selected    bool                `json:"selected"`
	Explanation string              `json:"explanation,omitempty"`
}

// RuleTestResponse answers POST /admin/rules/test: the decision the request
// path would make, explained. MatchedRule and WinningRule are the same rule
// (null when none matched); Reason is that rule's operator-authored reason or
// the default decision's. PrecedenceReason names the tuple dimension that
// selected the winner. Candidates are in winning order and empty, never null,
// when no rule matched. PolicyStatus describes the rule snapshot evaluated.
type RuleTestResponse struct {
	Allowed          bool                `json:"allowed"`
	MatchedRule      *RuleRecord         `json:"matched_rule" tstype:"RuleRecord | null,required"`
	WinningRule      *RuleRecord         `json:"winning_rule" tstype:"RuleRecord | null,required"`
	Winner           *RuleTestCandidate  `json:"winner" tstype:"RuleTestCandidate | null,required"`
	Reason           string              `json:"reason"`
	WinnerReason     string              `json:"winner_reason"`
	PrecedenceReason string              `json:"precedence_reason"`
	Candidates       []RuleTestCandidate `json:"candidates"`
	PolicyStatus     *PolicyStatus       `json:"policy_status,omitempty"`
}
