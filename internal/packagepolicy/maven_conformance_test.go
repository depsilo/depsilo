// Licensed to the Apache Software Foundation (ASF) under one or more
// contributor license agreements. See the NOTICE file distributed with this
// work for additional information regarding copyright ownership. The ASF
// licenses this file to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.
//
// Translated and adapted to Go for Depsilo by Depsilo Contributors in 2026.
// The comparison and equality vectors come from Apache Maven 3.9.16's
// ComparableVersionTest.java, pinned at tag commit
// 2bdd9fddda4b155ebf8000e807eb73fd829a51d5:
// https://github.com/apache/maven/blob/2bdd9fddda4b155ebf8000e807eb73fd829a51d5/maven-artifact/src/test/java/org/apache/maven/artifact/versioning/ComparableVersionTest.java
// See the repository NOTICE and LICENSES/Apache-2.0.txt files.

package packagepolicy_test

import (
	"testing"

	"depsilo/internal/packagepolicy"
)

// These tests preserve every comparison/equality vector in Maven 3.9.16's
// ComparableVersionTest. Java-only API checks for getCanonical(), hashCode(),
// mutable parseVersion() reuse, and mutation of the process default Locale do
// not have equivalents on the immutable Go dialect interface. Every literal is
// nevertheless validated, compared in both directions, and compared with
// itself through the public PackagePolicyDialect seam.

func TestMavenComparableVersion3916OfficialStandaloneInput(t *testing.T) {
	dialect := mustMavenDialect(t)
	// Maven's testVersionsEqual invokes newComparable for this input solely to
	// exercise parsing/canonicalization before it starts its equality vectors.
	assertMavenVersionValidAndReflexive(t, dialect, "1.0-alpha")
}

func TestMavenComparableVersion3916OfficialQualifierOrder(t *testing.T) {
	dialect := mustMavenDialect(t)
	versions := []string{
		"1-alpha2snapshot",
		"1-alpha2",
		"1-alpha-123",
		"1-beta-2",
		"1-beta123",
		"1-m2",
		"1-m11",
		"1-rc",
		"1-cr2",
		"1-rc123",
		"1-SNAPSHOT",
		"1",
		"1-sp",
		"1-sp2",
		"1-sp123",
		"1-abc",
		"1-def",
		"1-pom-1",
		"1-1-snapshot",
		"1-1",
		"1-2",
		"1-123",
	}
	assertMavenOrderedSequence(t, dialect, versions)
}

func TestMavenComparableVersion3916OfficialNumberOrder(t *testing.T) {
	dialect := mustMavenDialect(t)
	versions := []string{
		"2.0", "2.0.a", "2-1", "2.0.2", "2.0.123", "2.1.0",
		"2.1-a", "2.1b", "2.1-c", "2.1-1", "2.1.0.1", "2.2",
		"2.123", "11.a2", "11.a11", "11.b2", "11.b11", "11.m2",
		"11.m11", "11", "11.a", "11b", "11c", "11m",
	}
	assertMavenOrderedSequence(t, dialect, versions)
}

func TestMavenComparableVersion3916OfficialEqualities(t *testing.T) {
	dialect := mustMavenDialect(t)
	pairs := [][2]string{
		{"1", "1"},
		{"1", "1.0"},
		{"1", "1.0.0"},
		{"1.0", "1.0.0"},
		{"1", "1-0"},
		{"1", "1.0-0"},
		{"1.0", "1.0-0"},
		{"1a", "1-a"},
		{"1a", "1.0-a"},
		{"1a", "1.0.0-a"},
		{"1.0a", "1-a"},
		{"1.0.0a", "1-a"},
		{"1x", "1-x"},
		{"1x", "1.0-x"},
		{"1x", "1.0.0-x"},
		{"1.0x", "1-x"},
		{"1.0.0x", "1-x"},
		{"1ga", "1"},
		{"1release", "1"},
		{"1final", "1"},
		{"1cr", "1rc"},
		{"1a1", "1-alpha-1"},
		{"1b2", "1-beta-2"},
		{"1m3", "1-milestone-3"},
		{"1X", "1x"},
		{"1A", "1a"},
		{"1B", "1b"},
		{"1M", "1m"},
		{"1Ga", "1"},
		{"1GA", "1"},
		{"1RELEASE", "1"},
		{"1release", "1"},
		{"1RELeaSE", "1"},
		{"1Final", "1"},
		{"1FinaL", "1"},
		{"1FINAL", "1"},
		{"1Cr", "1Rc"},
		{"1cR", "1rC"},
		{"1m3", "1Milestone3"},
		{"1m3", "1MileStone3"},
		{"1m3", "1MILESTONE3"},
	}
	for _, pair := range pairs {
		assertMavenEqual(t, dialect, pair[0], pair[1])
	}
}

func TestMavenComparableVersion3916OfficialComparisons(t *testing.T) {
	dialect := mustMavenDialect(t)
	pairs := [][2]string{
		{"1", "2"},
		{"1.5", "2"},
		{"1", "2.5"},
		{"1.0", "1.1"},
		{"1.1", "1.2"},
		{"1.0.0", "1.1"},
		{"1.0.1", "1.1"},
		{"1.1", "1.2.0"},
		{"1.0-alpha-1", "1.0"},
		{"1.0-alpha-1", "1.0-alpha-2"},
		{"1.0-alpha-1", "1.0-beta-1"},
		{"1.0-beta-1", "1.0-SNAPSHOT"},
		{"1.0-SNAPSHOT", "1.0"},
		{"1.0-alpha-1-SNAPSHOT", "1.0-alpha-1"},
		{"1.0", "1.0-1"},
		{"1.0-1", "1.0-2"},
		{"1.0.0", "1.0-1"},
		{"2.0-1", "2.0.1"},
		{"2.0.1-klm", "2.0.1-lmn"},
		{"2.0.1", "2.0.1-xyz"},
		{"2.0.1", "2.0.1-123"},
		{"2.0.1-xyz", "2.0.1-123"},
	}
	for _, pair := range pairs {
		assertMavenLess(t, dialect, pair[0], pair[1])
	}
}

func TestMavenComparableVersion3916OfficialRegressions(t *testing.T) {
	dialect := mustMavenDialect(t)

	t.Run("MNG-5568", func(t *testing.T) {
		a, b, c := "6.1.0", "6.1.0rc3", "6.1H.5-beta"
		assertMavenLess(t, dialect, b, a)
		assertMavenLess(t, dialect, b, c)
		assertMavenLess(t, dialect, a, c)
	})

	t.Run("MNG-6572", func(t *testing.T) {
		a := "20190126.230843"
		b := "1234567890.12345"
		c := "123456789012345.1H.5-beta"
		d := "12345678901234567890.1H.5-beta"
		for _, pair := range [][2]string{{a, b}, {b, c}, {a, c}, {c, d}, {b, d}, {a, d}} {
			assertMavenLess(t, dialect, pair[0], pair[1])
		}
	})

	t.Run("MNG-6964", func(t *testing.T) {
		a, b, c := "1-0.alpha", "1-0.beta", "1"
		assertMavenLess(t, dialect, a, c)
		assertMavenLess(t, dialect, b, c)
		assertMavenLess(t, dialect, a, b)
	})

	t.Run("MNG-7644", func(t *testing.T) {
		for _, qualifier := range []string{"abc", "alpha", "a", "beta", "b", "def", "milestone", "m", "RC"} {
			assertMavenLess(t, dialect, "1.0.0."+qualifier+"1", "1.0.0-"+qualifier+"2")

			aliases := []string{"2-" + qualifier, "2.0." + qualifier, "2.0.0." + qualifier}
			for left := 0; left < len(aliases); left++ {
				for right := left + 1; right < len(aliases); right++ {
					assertMavenEqual(t, dialect, aliases[left], aliases[right])
				}
			}
		}
	})
}

func TestMavenComparableVersion3916OfficialLeadingZeroEqualities(t *testing.T) {
	dialect := mustMavenDialect(t)

	oneVersions := []string{
		"0000000000000000001", "000000000000000001", "00000000000000001",
		"0000000000000001", "000000000000001", "00000000000001",
		"0000000000001", "000000000001", "00000000001", "0000000001",
		"000000001", "00000001", "0000001", "000001", "00001", "0001",
		"001", "01", "1",
	}
	zeroVersions := []string{
		"0000000000000000000", "000000000000000000", "00000000000000000",
		"0000000000000000", "000000000000000", "00000000000000",
		"0000000000000", "000000000000", "00000000000", "0000000000",
		"000000000", "00000000", "0000000", "000000", "00000", "0000",
		"000", "00", "0",
	}
	assertMavenEqualSequence(t, dialect, oneVersions)
	assertMavenEqualSequence(t, dialect, zeroVersions)
}

func TestMavenComparableVersion3916OfficialLocaleIndependentEquality(t *testing.T) {
	dialect := mustMavenDialect(t)
	assertMavenEqual(t, dialect, "1-abcdefghijklmnopqrstuvwxyz", "1-ABCDEFGHIJKLMNOPQRSTUVWXYZ")
}

func mustMavenDialect(t *testing.T) packagepolicy.PackagePolicyDialect {
	t.Helper()
	dialect, err := packagepolicy.DialectFor("maven")
	if err != nil {
		t.Fatalf("DialectFor(maven): %v", err)
	}
	if dialect.SupportsRanges() {
		t.Fatal("Maven ComparableVersion must remain exact-only")
	}
	return dialect
}

func assertMavenOrderedSequence(t *testing.T, dialect packagepolicy.PackagePolicyDialect, versions []string) {
	t.Helper()
	for index := 0; index < len(versions)-1; index++ {
		for higher := index + 1; higher < len(versions); higher++ {
			assertMavenLess(t, dialect, versions[index], versions[higher])
		}
	}
}

func assertMavenEqualSequence(t *testing.T, dialect packagepolicy.PackagePolicyDialect, versions []string) {
	t.Helper()
	for left := range versions {
		for right := left; right < len(versions); right++ {
			assertMavenEqual(t, dialect, versions[left], versions[right])
		}
	}
}

func assertMavenLess(t *testing.T, dialect packagepolicy.PackagePolicyDialect, lower, higher string) {
	t.Helper()
	assertMavenVersionValidAndReflexive(t, dialect, lower)
	assertMavenVersionValidAndReflexive(t, dialect, higher)

	forward, err := dialect.CompareVersions(lower, higher)
	if err != nil {
		t.Fatalf("CompareVersions(%q, %q): %v", lower, higher, err)
	}
	reverse, err := dialect.CompareVersions(higher, lower)
	if err != nil {
		t.Fatalf("CompareVersions(%q, %q): %v", higher, lower, err)
	}
	if forward >= 0 || reverse <= 0 {
		t.Errorf("ComparableVersion order %q < %q failed: forward=%d reverse=%d", lower, higher, forward, reverse)
	}
}

func assertMavenEqual(t *testing.T, dialect packagepolicy.PackagePolicyDialect, left, right string) {
	t.Helper()
	assertMavenVersionValidAndReflexive(t, dialect, left)
	assertMavenVersionValidAndReflexive(t, dialect, right)

	forward, err := dialect.CompareVersions(left, right)
	if err != nil {
		t.Fatalf("CompareVersions(%q, %q): %v", left, right, err)
	}
	reverse, err := dialect.CompareVersions(right, left)
	if err != nil {
		t.Fatalf("CompareVersions(%q, %q): %v", right, left, err)
	}
	if forward != 0 || reverse != 0 {
		t.Errorf("ComparableVersion equality %q == %q failed: forward=%d reverse=%d", left, right, forward, reverse)
	}
}

func assertMavenVersionValidAndReflexive(t *testing.T, dialect packagepolicy.PackagePolicyDialect, version string) {
	t.Helper()
	if err := dialect.ValidateVersion(version); err != nil {
		t.Fatalf("ValidateVersion(%q): %v", version, err)
	}
	comparison, err := dialect.CompareVersions(version, version)
	if err != nil {
		t.Fatalf("CompareVersions(%q, %q): %v", version, version, err)
	}
	if comparison != 0 {
		t.Fatalf("CompareVersions(%q, itself) = %d, want 0", version, comparison)
	}
	if normalized, err := packagepolicy.NormalizeVersion("maven", version); err != nil {
		t.Fatalf("NormalizeVersion(%q): %v", version, err)
	} else if normalized == "" {
		t.Fatalf("NormalizeVersion(%q) returned an empty value", version)
	}
}
