package security

import (
	"encoding/json"
	"strconv"
	"strings"

	"depsilo/internal/db"
	"depsilo/internal/ecosystem"
)

// AdvisoryDetailRevision identifies how stored advisory fields were derived
// from OSV records. Raise it when parsing changes; rows from an older revision
// are fetched and parsed again by the next scan of their package.
//
// Revision 1 scores CVSS v3 vectors and uses GitHub's qualitative rating when
// no v3 vector exists. Revision 0 read the vector as a number, so every
// OSV-sourced advisory was stored as "unknown" with score 0.
const AdvisoryDetailRevision = 1

// ParseVulnerability converts an OSV vulnerability into a DB model.
func ParseVulnerability(v OSVVulnerability, ecosystemHint string) *db.Vulnerability {
	severity, cvss := extractSeverity(v)
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
		DetailRevision: AdvisoryDetailRevision,
	}
}

// extractSeverity scores the record's CVSS v3 vector. OSV carries vectors,
// not numbers, in severity scores; a bare number is still accepted because
// operator imports from other tools sometimes contain one. Records scored only
// with CVSS v4 fall back to the database's qualitative rating, leaving the
// numeric score unknown instead of inventing one.
func extractSeverity(v OSVVulnerability) (string, float32) {
	for _, s := range v.Severity {
		if s.Type != "CVSS_V3" {
			continue
		}
		if score, ok := cvssV3BaseScore(s.Score); ok && score > 0 {
			return classifyCVSS(float32(score)), float32(score)
		}
		if score, err := strconv.ParseFloat(strings.TrimSpace(s.Score), 32); err == nil && score > 0 && score <= 10 {
			return classifyCVSS(float32(score)), float32(score)
		}
	}
	if label := databaseSpecificSeverity(v.DatabaseSpecific); label != "" {
		return label, 0
	}
	return "unknown", 0
}

// databaseSpecificSeverity reads the qualitative rating GitHub advisories
// publish as database_specific.severity. Other shapes are ignored.
func databaseSpecificSeverity(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	var label string
	if json.Unmarshal(fields["severity"], &label) != nil {
		return ""
	}
	switch strings.ToUpper(strings.TrimSpace(label)) {
	case "CRITICAL":
		return "critical"
	case "HIGH":
		return "high"
	case "MODERATE", "MEDIUM":
		return "medium"
	case "LOW":
		return "low"
	default:
		return ""
	}
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
