package security

import "testing"

func TestCVSSV3BaseScoreMatchesReferenceScores(t *testing.T) {
	// Expected values are the FIRST calculator's base scores; several vectors
	// are copied from real OSV records (PYSEC-2024-60, GHSA-jjg7-2v4v-x38h).
	for vector, want := range map[string]float64{
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H": 9.8,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H": 7.5,
		"CVSS:3.1/AV:L/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H": 6.2,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:L": 5.3,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N": 6.1,
		"CVSS:3.1/AV:N/AC:L/PR:L/UI:N/S:C/C:H/I:H/A:H": 9.9,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H": 10.0,
		"CVSS:3.1/AV:P/AC:H/PR:H/UI:R/S:U/C:L/I:N/A:N": 1.6,
		"CVSS:3.0/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:N/A:N": 5.9,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N": 0,
		// Temporal and environmental metrics do not change the base score.
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H/E:U/RL:O/RC:C/CR:H": 7.5,
		// Metric order is not significant.
		"CVSS:3.1/S:U/AV:N/AC:L/PR:N/UI:N/A:H/I:N/C:N": 7.5,
	} {
		got, ok := cvssV3BaseScore(vector)
		if !ok || got != want {
			t.Errorf("cvssV3BaseScore(%q) = %v, %v; want %v", vector, got, ok, want)
		}
	}
}

func TestCVSSV3BaseScoreRejectsIncompleteOrUnknownVectors(t *testing.T) {
	for _, vector := range []string{
		"",
		"9.8",
		"CVSS:3.1",
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H",
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H/A:L",
		"CVSS:3.1/AV:X/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:X/C:H/I:H/A:H",
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A",
		"CVSS:2.0/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		"AV:N/AC:L/Au:N/C:P/I:P/A:P",
		"CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:N/VI:N/VA:H/SC:N/SI:N/SA:N",
	} {
		if got, ok := cvssV3BaseScore(vector); ok {
			t.Errorf("cvssV3BaseScore(%q) = %v, accepted", vector, got)
		}
	}
}

func TestCVSSRoundUpAvoidsFloatingPointArtifacts(t *testing.T) {
	for input, want := range map[float64]float64{
		4.000000000000001: 4.0,
		4.02:              4.1,
		4.0:               4.0,
		9.760161495:       9.8,
	} {
		if got := cvssRoundUp(input); got != want {
			t.Errorf("cvssRoundUp(%v) = %v, want %v", input, got, want)
		}
	}
}
