package rules

import (
	"testing"

	"depsilo/internal/db"
	"depsilo/internal/packagepolicy"
)

func TestMatchSpecificityVersionLevels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		ruleVersion   string
		actualVersion string
		wantMatched   bool
		wantVersion   int
	}{
		{name: "unknown actual matches wildcard", ruleVersion: "*", actualVersion: "", wantMatched: true, wantVersion: 0},
		{name: "unknown actual matches empty rule version", ruleVersion: "", actualVersion: "", wantMatched: true, wantVersion: 0},
		{name: "unknown actual does not match exact", ruleVersion: "1.2.3", actualVersion: "", wantMatched: false},
		{name: "unknown actual does not match less-than range", ruleVersion: "< 2.0.0", actualVersion: "", wantMatched: false},
		{name: "unknown actual does not match greater-than range", ruleVersion: ">= 1.0.0", actualVersion: "", wantMatched: false},
		{name: "exact version has highest specificity", ruleVersion: "1.2.3", actualVersion: "1.2.3", wantMatched: true, wantVersion: 2},
		{name: "matching range has lower specificity", ruleVersion: "< 2.0.0", actualVersion: "1.2.3", wantMatched: true, wantVersion: 1},
		{name: "package-wide version has no version specificity", ruleVersion: "*", actualVersion: "1.2.3", wantMatched: true, wantVersion: 0},
		{name: "nonmatching exact is rejected", ruleVersion: "1.2.4", actualVersion: "1.2.3", wantMatched: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := packagepolicy.PrepareRule(packagepolicy.RawRule{
				Ecosystem: "pypi", PackageName: "requests", Version: test.ruleVersion,
			})
			if err != nil {
				t.Fatalf("prepare rule: %v", err)
			}
			compiled, err := compilePersistedRule(db.PackageRule{
				Ecosystem:             prepared.Ecosystem,
				PackageName:           prepared.PackageName,
				Version:               prepared.Version,
				NormalizedPackageName: prepared.NormalizedPackageName,
				NormalizedVersion:     prepared.NormalizedVersion,
				DialectRevision:       prepared.DialectRevision,
				Action:                "deny",
			})
			if err != nil {
				t.Fatalf("compile rule: %v", err)
			}
			specificity, matched, err := compiled.matchSpecificity("pypi", "requests", test.actualVersion)
			if err != nil {
				t.Fatalf("matchSpecificity(version=%q, actual=%q): %v", test.ruleVersion, test.actualVersion, err)
			}
			if matched != test.wantMatched {
				t.Fatalf("matchSpecificity(version=%q, actual=%q) matched=%v, want %v", test.ruleVersion, test.actualVersion, matched, test.wantMatched)
			}
			if matched && specificity.Version != test.wantVersion {
				t.Fatalf("matchSpecificity(version=%q, actual=%q) specificity=%+v, want version=%d", test.ruleVersion, test.actualVersion, specificity, test.wantVersion)
			}
		})
	}
}
