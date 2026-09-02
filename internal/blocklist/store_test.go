package blocklist

import (
	"context"
	"testing"
	"time"

	"depsilo/internal/db"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	return NewStore(database)
}

func TestNormalizeName(t *testing.T) {
	cases := []struct{ eco, in, want string }{
		{"npm", "EvIl-Pkg", "EvIl-Pkg"},
		{"NPM", "@Scope/Name", "@Scope/Name"},
		{"pypi", "Requests", "requests"},
		{"pypi", "typo__squat.pkg", "typo-squat-pkg"},
		{"maven", "com.Evil:Artifact", "com.Evil:Artifact"}, // case-significant
		{"go", "github.com/Evil/mod", "github.com/Evil/mod"},
	}
	for _, c := range cases {
		if got := NormalizeName(c.eco, c.in); got != c.want {
			t.Errorf("NormalizeName(%s, %s) = %s, want %s", c.eco, c.in, got, c.want)
		}
	}
}

func TestStore_NPMPackageIdentityIsCaseSensitive(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	rows := []db.MaliciousPackage{
		{SourceID: "MAL-EXPRESS-UPPER", Ecosystem: "npm", Package: "Express", Versions: ""},
		{SourceID: "MAL-EXPRESS-LOWER", Ecosystem: "npm", Package: "express", Versions: ""},
		{SourceID: "MAL-JSONSTREAM", Ecosystem: "npm", Package: "JSONStream", Versions: ""},
	}
	if err := s.ReplaceEcosystem(ctx, "npm", rows); err != nil {
		t.Fatal(err)
	}

	upper, upperOverridden, err := s.Check(ctx, "NPM", "Express", "1.0.0")
	if err != nil || upper == nil || upper.SourceID != "MAL-EXPRESS-UPPER" || upperOverridden {
		t.Fatalf("uppercase identity: match=%+v overridden=%t err=%v", upper, upperOverridden, err)
	}
	lower, lowerOverridden, err := s.Check(ctx, "npm", "express", "1.0.0")
	if err != nil || lower == nil || lower.SourceID != "MAL-EXPRESS-LOWER" || lowerOverridden {
		t.Fatalf("lowercase identity: match=%+v overridden=%t err=%v", lower, lowerOverridden, err)
	}
	if match, _, err := s.Check(ctx, "npm", "jsonstream", "1.0.0"); err != nil || match != nil {
		t.Fatalf("lowercase request matched JSONStream advisory: match=%+v err=%v", match, err)
	}
	if match, _, err := s.Check(ctx, "npm", "JSONStream", "1.0.0"); err != nil || match == nil {
		t.Fatalf("exact JSONStream request did not match: match=%+v err=%v", match, err)
	}

	if _, err := s.CreateOverride(ctx, "npm", "Express", "", "case-specific false positive", 1); err != nil {
		t.Fatal(err)
	}
	if _, overridden, err := s.Check(ctx, "npm", "Express", "1.0.0"); err != nil || !overridden {
		t.Fatalf("Express override not honored: overridden=%t err=%v", overridden, err)
	}
	if _, overridden, err := s.Check(ctx, "npm", "express", "1.0.0"); err != nil || overridden {
		t.Fatalf("Express override leaked to express: overridden=%t err=%v", overridden, err)
	}
}

func TestStore_Check(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	rows := []db.MaliciousPackage{
		{SourceID: "MAL-2026-0001", Ecosystem: "npm", Package: "evil-pkg", Versions: ""}, // all versions
		{SourceID: "MAL-2026-0002", Ecosystem: "npm", Package: "sometimes-evil", Versions: `["1.2.3","1.2.4"]`},
	}
	if err := s.ReplaceEcosystem(ctx, "npm", rows); err != nil {
		t.Fatal(err)
	}

	t.Run("all-versions advisory blocks any version", func(t *testing.T) {
		m, ov, err := s.Check(ctx, "npm", "evil-pkg", "9.9.9")
		if err != nil || m == nil || ov {
			t.Fatalf("m=%v ov=%v err=%v", m, ov, err)
		}
		if m.SourceID != "MAL-2026-0001" {
			t.Errorf("SourceID = %s", m.SourceID)
		}
	})

	t.Run("name requires exact case at query time", func(t *testing.T) {
		if m, _, _ := s.Check(ctx, "npm", "EVIL-PKG", "1.0.0"); m != nil {
			t.Error("uppercase query matched lowercase npm package")
		}
	})

	t.Run("version-list advisory matches exactly", func(t *testing.T) {
		if m, _, _ := s.Check(ctx, "npm", "sometimes-evil", "1.2.3"); m == nil {
			t.Error("listed version should match")
		}
		if m, _, _ := s.Check(ctx, "npm", "sometimes-evil", "1.2.5"); m != nil {
			t.Errorf("unlisted version matched: %+v", m)
		}
	})

	t.Run("clean package passes", func(t *testing.T) {
		if m, _, _ := s.Check(ctx, "npm", "lodash", "4.17.21"); m != nil {
			t.Errorf("clean package matched: %+v", m)
		}
	})

	t.Run("replace removes retracted advisories", func(t *testing.T) {
		if err := s.ReplaceEcosystem(ctx, "npm", rows[1:]); err != nil {
			t.Fatal(err)
		}
		if m, _, _ := s.Check(ctx, "npm", "evil-pkg", "1.0.0"); m != nil {
			t.Error("retracted advisory still matches after replace")
		}
	})
}

func TestStore_Overrides(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.ReplaceEcosystem(ctx, "pypi", []db.MaliciousPackage{
		{SourceID: "MAL-2026-0003", Ecosystem: "pypi", Package: "false-positive", Versions: ""},
	}); err != nil {
		t.Fatal(err)
	}

	t.Run("reason is mandatory", func(t *testing.T) {
		if _, err := s.CreateOverride(ctx, "pypi", "false-positive", "", "", 1); err == nil {
			t.Error("empty reason accepted")
		}
	})

	ov, err := s.CreateOverride(ctx, "pypi", "False_Positive", "1.0.0", "vetted internally, MAL entry is a typosquat collision", 7)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Package != "false-positive" {
		t.Errorf("override package not normalized: %s", ov.Package)
	}
	if ttl := time.Until(ov.ExpiresAt); ttl > OverrideTTL || ttl < OverrideTTL-time.Minute {
		t.Errorf("unexpected TTL: %v", ttl)
	}

	t.Run("override exempts the exact version only", func(t *testing.T) {
		if _, overridden, _ := s.Check(ctx, "pypi", "false-positive", "1.0.0"); !overridden {
			t.Error("override not honored")
		}
		if _, overridden, _ := s.Check(ctx, "pypi", "false-positive", "2.0.0"); overridden {
			t.Error("override leaked to another version")
		}
	})

	t.Run("expired override stops exempting", func(t *testing.T) {
		s.now = func() time.Time { return time.Now().UTC().Add(25 * time.Hour) }
		defer func() { s.now = func() time.Time { return time.Now().UTC() } }()
		if _, overridden, _ := s.Check(ctx, "pypi", "false-positive", "1.0.0"); overridden {
			t.Error("expired override still honored")
		}
	})

	t.Run("create + revoke both leave audit events", func(t *testing.T) {
		if err := s.RevokeOverride(ctx, ov.ID, "expiring test override", 7); err != nil {
			t.Fatal(err)
		}
		var actions []string
		s.db.Model(&db.QuarantineEvent{}).Order("id").Pluck("action", &actions)
		want := map[string]bool{ActionOverrideCreated: false, ActionOverrideRevoked: false}
		for _, a := range actions {
			if _, ok := want[a]; ok {
				want[a] = true
			}
		}
		for a, seen := range want {
			if !seen {
				t.Errorf("missing audit event %s (got %v)", a, actions)
			}
		}
	})
}

func TestStore_PackageWideOverride(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.ReplaceEcosystem(ctx, "npm", []db.MaliciousPackage{
		{SourceID: "MAL-2026-0004", Ecosystem: "npm", Package: "wide", Versions: ""},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOverride(ctx, "npm", "wide", "", "package-wide exemption", 1); err != nil {
		t.Fatal(err)
	}
	if _, overridden, _ := s.Check(ctx, "npm", "wide", "3.1.4"); !overridden {
		t.Error("package-wide override (empty version) should cover any version")
	}
}

func TestStore_CanonicalizationRegressions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.ReplaceEcosystem(ctx, "nuget", []db.MaliciousPackage{
		{SourceID: "MAL-2026-0005", Ecosystem: "nuget", Package: "evil.client", Versions: `["1.0","2.0.0-Alpha"]`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceEcosystem(ctx, "go", []db.MaliciousPackage{
		{SourceID: "MAL-2026-0006", Ecosystem: "go", Package: "github.com/evil/module", Versions: `["1.2.3"]`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceEcosystem(ctx, "pypi", []db.MaliciousPackage{
		{SourceID: "MAL-2026-0007", Ecosystem: "pypi", Package: "evil-py", Versions: ""},
	}); err != nil {
		t.Fatal(err)
	}

	t.Run("nuget is case-insensitive", func(t *testing.T) {
		if m, _, _ := s.Check(ctx, "nuget", "evil.client", "1.0.0"); m == nil {
			t.Error("NuGet 1.0 request should match equivalent flat-container 1.0.0")
		}
		if m, _, _ := s.Check(ctx, "nuget", "EVIL.CLIENT", "2.0.0-alpha"); m == nil {
			t.Error("NuGet package and prerelease identity should be case-insensitive")
		}
	})
	t.Run("go versions match with the v prefix stripped", func(t *testing.T) {
		if m, _, _ := s.Check(ctx, "go", "github.com/evil/module", "v1.2.3"); m == nil {
			t.Error("GOPROXY v-prefixed version should match the dataset's bare semver")
		}
		if m, _, _ := s.Check(ctx, "go", "github.com/evil/module", "v1.2.4"); m != nil {
			t.Error("unlisted version matched")
		}
	})
	t.Run("extra PyPI indexes canonicalize to pypi", func(t *testing.T) {
		if m, _, _ := s.Check(ctx, "extra:corp-mirror", "Evil_Py", "1.0.0"); m == nil {
			t.Error("extra-index ecosystem should hit the pypi rows")
		}
	})
}

func TestStore_ExplicitVersionsUseDialectEquality(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	fixtures := []struct {
		ecosystem string
		pkg       string
		stored    string
		request   string
	}{
		{ecosystem: "npm", pkg: "signed-release", stored: "1.0.0+advisory", request: "1.0.0+mirror"},
		{ecosystem: "cargo", pkg: "signed_release", stored: "1.0.0+advisory", request: "1.0.0+mirror"},
		{ecosystem: "pypi", pkg: "friendly-kit", stored: "1.0RC1", request: "1.0rc1"},
		{ecosystem: "maven", pkg: "org.example:artifact", stored: "1.0-final", request: "1.0"},
	}
	for index, fixture := range fixtures {
		row := db.MaliciousPackage{
			SourceID:  "MAL-DIALECT",
			Ecosystem: fixture.ecosystem,
			Package:   fixture.pkg,
			Versions:  `["` + fixture.stored + `"]`,
		}
		if err := s.ReplaceEcosystem(ctx, fixture.ecosystem, []db.MaliciousPackage{row}); err != nil {
			t.Fatal(err)
		}
		t.Run(fixture.ecosystem, func(t *testing.T) {
			match, _, err := s.Check(ctx, fixture.ecosystem, fixture.pkg, fixture.request)
			if err != nil || match == nil {
				t.Fatalf("dialect-equal version did not match: match=%+v err=%v fixture=%+v index=%d", match, err, fixture, index)
			}
		})
	}
}

func TestStore_OverrideUsesDialectVersionIdentity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.ReplaceEcosystem(ctx, "nuget", []db.MaliciousPackage{{
		SourceID: "MAL-NUGET", Ecosystem: "nuget", Package: "example.client", Versions: `["1.0"]`,
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateOverride(ctx, "nuget", "Example.Client", "1.0", "verified false positive", 7); err != nil {
		t.Fatal(err)
	}
	match, overridden, err := s.Check(ctx, "nuget", "example.client", "1.0.0")
	if err != nil || match == nil || !overridden {
		t.Fatalf("NuGet-equivalent override not honored: match=%+v overridden=%t err=%v", match, overridden, err)
	}
}

func TestStore_InvalidExplicitVersionIsObservable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.ReplaceEcosystem(ctx, "nuget", []db.MaliciousPackage{{
		SourceID: "MAL-CORRUPT", Ecosystem: "nuget", Package: "bad.client", Versions: `["1..0"]`,
	}}); err != nil {
		t.Fatal(err)
	}
	match, _, err := s.Check(ctx, "nuget", "bad.client", "1.0.0")
	if match != nil || err == nil {
		t.Fatalf("corrupt explicit version = match %+v err %v, want nil match and observable error", match, err)
	}
}
