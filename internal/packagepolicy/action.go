package packagepolicy

import (
	"fmt"
	"strings"
)

// NormalizeRuleAction validates the decision carried by a package rule and
// returns its persisted canonical form.
func NormalizeRuleAction(action string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(action))
	if normalized != "allow" && normalized != "deny" {
		return "", fmt.Errorf("%w: action %q must be allow or deny", ErrInvalidRule, action)
	}
	return normalized, nil
}
