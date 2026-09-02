package security

import (
	"encoding/json"
	"strconv"
	"strings"

	"depsilo/internal/db"
	"depsilo/internal/ecosystem"
)

// ParseVulnerability converts an OSV vulnerability into a DB model.
func ParseVulnerability(v OSVVulnerability, ecosystemHint string) *db.Vulnerability {
	severity, cvss := extractSeverity(v.Severity)
	ranges := extractAffectedRanges(v.Affected)
	refs := extractReferences(v.References)
	aliases := strings.Join(v.Aliases, ",")
	ecosystem := ecosystemHint
	packageName := ""

	if len(v.Affected) > 0 && v.Affected[0].Package != nil {
		packageName = v.Affected[0].Package.Name
		if eco := reverseEcosystem(v.Affected[0].Package.Ecosystem); eco != "" {
			ecosystem = eco
		}
	}

	return &db.Vulnerability{
		OSVID:          v.ID,
		Ecosystem:      ecosystem,
		PackageName:    packageName,
		AffectedRanges: ranges,
		Severity:       severity,
		CVSSScore:      cvss,
		Summary:        v.Summary,
		Details:        v.Details,
		Aliases:        aliases,
		References:     refs,
		PublishedAt:    v.Published,
		ModifiedAt:     v.Modified,
	}
}

func extractSeverity(severities []osvSeverity) (string, float32) {
	for _, s := range severities {
		if s.Type == "CVSS_V3" {
			score := parseCVSSScore(s.Score)
			return classifyCVSS(score), score
		}
	}
	return "unknown", 0
}

func parseCVSSScore(scoreStr string) float32 {
	if f, err := strconv.ParseFloat(scoreStr, 32); err == nil {
		return float32(f)
	}
	return 0
}

func classifyCVSS(score float32) string {
	switch {
	case score >= 9.0:
		return "critical"
	case score >= 7.0:
		return "high"
	case score >= 4.0:
		return "medium"
	case score > 0:
		return "low"
	default:
		return "unknown"
	}
}

func extractAffectedRanges(affected []osvAffected) string {
	type rangeEntry struct {
		Type   string     `json:"type"`
		Events []osvEvent `json:"events"`
	}
	var allRanges []rangeEntry
	for _, a := range affected {
		for _, r := range a.Ranges {
			allRanges = append(allRanges, rangeEntry{Type: r.Type, Events: r.Events})
		}
	}
	data, _ := json.Marshal(allRanges)
	return string(data)
}

func extractReferences(refs []osvRef) string {
	urls := make([]string, 0, len(refs))
	for _, r := range refs {
		urls = append(urls, r.URL)
	}
	data, _ := json.Marshal(urls)
	return string(data)
}

func reverseEcosystem(osvEco string) string {
	return ecosystem.NameForOSV(osvEco)
}
