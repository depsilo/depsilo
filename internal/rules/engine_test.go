package rules

import (
	"testing"

	"depsilo/internal/db"
)

func TestMatchScoreVersionSpecificity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		ruleVersion   string
		actualVersion string
		want          int
	}{
		{name: "unknown actual matches wildcard", ruleVersion: "*", actualVersion: "", want: 4},
		{name: "unknown actual matches empty rule version", ruleVersion: "", actualVersion: "", want: 4},
		{name: "unknown actual does not match exact", ruleVersion: "1.2.3", actualVersion: "", want: -1},
		{name: "unknown actual does not match less-than range", ruleVersion: "< 2.0.0", actualVersion: "", want: -1},
		{name: "unknown actual does not match greater-than range", ruleVersion: ">= 1.0.0", actualVersion: "", want: -1},
		{name: "exact version has highest specificity", ruleVersion: "1.2.3", actualVersion: "1.2.3", want: 6},
		{name: "matching range has lower specificity", ruleVersion: "< 2.0.0", actualVersion: "1.2.3", want: 5},
		{name: "package-wide version has no version specificity", ruleVersion: "*", actualVersion: "1.2.3", want: 4},
		{name: "nonmatching exact is rejected", ruleVersion: "1.2.4", actualVersion: "1.2.3", want: -1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := &db.PackageRule{
				Ecosystem:   "pypi",
				PackageName: "requests",
				Version:     test.ruleVersion,
			}
			if got := matchScore(rule, "pypi", "requests", test.actualVersion); got != test.want {
				t.Fatalf("matchScore(version=%q, actual=%q) = %d, want %d", test.ruleVersion, test.actualVersion, got, test.want)
			}
		})
	}
}
