package security

import (
	"math"
	"strings"
)

// cvssV3Weights are the CVSS v3.0/v3.1 base metric weights. Privileges
// Required has scope-dependent weights and is handled separately.
var cvssV3Weights = map[string]map[string]float64{
	"AV": {"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2},
	"AC": {"L": 0.77, "H": 0.44},
	"UI": {"N": 0.85, "R": 0.62},
	"C":  {"H": 0.56, "L": 0.22, "N": 0},
	"I":  {"H": 0.56, "L": 0.22, "N": 0},
	"A":  {"H": 0.56, "L": 0.22, "N": 0},
}

var cvssV3PrivilegeWeights = map[string]struct{ unchanged, changed float64 }{
	"N": {0.85, 0.85},
	"L": {0.62, 0.68},
	"H": {0.27, 0.5},
}

// cvssV3BaseScore computes the base score of a CVSS v3.0 or v3.1 vector such
// as "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H". OSV records carry the
// vector rather than a number. Temporal and environmental metrics may follow
// the base metrics and do not change the base score. A vector with a missing,
// repeated or unknown base metric is rejected rather than guessed.
func cvssV3BaseScore(vector string) (float64, bool) {
	parts := strings.Split(strings.TrimSpace(vector), "/")
	if len(parts) < 9 || (parts[0] != "CVSS:3.0" && parts[0] != "CVSS:3.1") {
		return 0, false
	}
	base := make(map[string]string, 8)
	seen := make(map[string]bool, len(parts))
	for _, part := range parts[1:] {
		name, value, ok := strings.Cut(part, ":")
		if !ok || name == "" || value == "" || seen[name] {
			return 0, false
		}
		seen[name] = true
		switch name {
		case "AV", "AC", "PR", "UI", "S", "C", "I", "A":
			base[name] = value
		}
	}
	if len(base) != 8 {
		return 0, false
	}

	changed := false
	switch base["S"] {
	case "U":
	case "C":
		changed = true
	default:
		return 0, false
	}
	weight := func(metric string) (float64, bool) {
		value, ok := cvssV3Weights[metric][base[metric]]
		return value, ok
	}
	attackVector, okAV := weight("AV")
	attackComplexity, okAC := weight("AC")
	userInteraction, okUI := weight("UI")
	confidentiality, okC := weight("C")
	integrity, okI := weight("I")
	availability, okA := weight("A")
	privilege, okPR := cvssV3PrivilegeWeights[base["PR"]]
	if !okAV || !okAC || !okUI || !okC || !okI || !okA || !okPR {
		return 0, false
	}
	privileges := privilege.unchanged
	if changed {
		privileges = privilege.changed
	}

	impactSubscore := 1 - (1-confidentiality)*(1-integrity)*(1-availability)
	impact := 6.42 * impactSubscore
	if changed {
		impact = 7.52*(impactSubscore-0.029) - 3.25*math.Pow(impactSubscore-0.02, 15)
	}
	if impact <= 0 {
		return 0, true
	}
	exploitability := 8.22 * attackVector * attackComplexity * privileges * userInteraction
	if changed {
		return cvssRoundUp(math.Min(1.08*(impact+exploitability), 10)), true
	}
	return cvssRoundUp(math.Min(impact+exploitability, 10)), true
}

// cvssRoundUp is the CVSS v3.1 Roundup function: the smallest one-decimal
// value not below its input, computed on integers to avoid floating-point
// artifacts such as 4.000000000000001 rounding to 4.1.
func cvssRoundUp(value float64) float64 {
	scaled := int64(math.Round(value * 100000))
	if scaled%10000 == 0 {
		return float64(scaled) / 100000
	}
	return float64(scaled/10000+1) / 10
}
