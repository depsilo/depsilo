package composer

import "testing"

func TestParseDistPath(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		wantOK      bool
		vendor      string
		pkg         string
		versionNorm string
		reference   string
		ext         string
	}{
		{
			name:   "tagged release",
			path:   "dist/monolog/monolog/3.9.0.0/aef6ee73a77a66e404dd6540934a9ef1b3c855b4.zip",
			wantOK: true, vendor: "monolog", pkg: "monolog",
			versionNorm: "3.9.0.0", reference: "aef6ee73a77a66e404dd6540934a9ef1b3c855b4", ext: "zip",
		},
		{
			name:   "dev branch version",
			path:   "dist/acme/lib/dev-main/abc123.zip",
			wantOK: true, vendor: "acme", pkg: "lib",
			versionNorm: "dev-main", reference: "abc123", ext: "zip",
		},
		{
			name:   "slashed branch version",
			path:   "dist/acme/lib/dev-feature/x/abc123.tar",
			wantOK: true, vendor: "acme", pkg: "lib",
			versionNorm: "dev-feature/x", reference: "abc123", ext: "tar",
		},
		{name: "wrong prefix", path: "p2/acme/lib.json", wantOK: false},
		{name: "too few segments", path: "dist/acme/lib/abc.zip", wantOK: false},
		{name: "empty reference", path: "dist/acme/lib/1.0.0.0/.zip", wantOK: false},
		{name: "no extension", path: "dist/acme/lib/1.0.0.0/abc123", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vendor, pkg, versionNorm, reference, ext, ok := ParseDistPath(tt.path)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if vendor != tt.vendor || pkg != tt.pkg || versionNorm != tt.versionNorm ||
				reference != tt.reference || ext != tt.ext {
				t.Errorf("got (%q, %q, %q, %q, %q), want (%q, %q, %q, %q, %q)",
					vendor, pkg, versionNorm, reference, ext,
					tt.vendor, tt.pkg, tt.versionNorm, tt.reference, tt.ext)
			}
		})
	}
}

func TestFindDistEntry(t *testing.T) {
	// Minified p2 shape: first entry complete, later entries only
	// carry the keys that changed; a disappeared key is emitted with
	// the literal string value "__unset" (MetadataMinifier format).
	doc := []byte(`{
		"packages": {
			"acme/lib": [
				{
					"name": "acme/lib",
					"version": "2.0.0",
					"version_normalized": "2.0.0.0",
					"license": ["MIT"],
					"dist": {"url": "https://example.com/2.0.0.zip", "type": "zip", "reference": "ref200"}
				},
				{
					"version": "1.5.0",
					"version_normalized": "1.5.0.0",
					"dist": {"url": "https://example.com/1.5.0.zip", "type": "zip", "reference": "ref150"}
				},
				{
					"version": "1.0.0",
					"version_normalized": "1.0.0.0",
					"dist": {"url": "https://example.com/1.0.0.zip", "type": "zip", "reference": "ref100"},
					"license": "__unset"
				}
			]
		}
	}`)

	t.Run("match requires version AND reference", func(t *testing.T) {
		ent := findDistEntry(doc, "acme/lib", "1.5.0.0", "ref150")
		if ent == nil {
			t.Fatal("expected a match")
		}
		if ent.Version != "1.5.0" || ent.Dist.URL != "https://example.com/1.5.0.zip" {
			t.Errorf("wrong entry: %+v", ent)
		}
	})

	t.Run("version match with stale reference must NOT serve", func(t *testing.T) {
		// Metadata moved (re-tag / branch push): the entry for the
		// version now points at a different commit. Serving it would
		// hand the client different bytes than its lockfile pinned —
		// the request must 404 so composer falls back to the
		// original dist URL.
		if ent := findDistEntry(doc, "acme/lib", "1.5.0.0", "oldref"); ent != nil {
			t.Errorf("expected nil for reference mismatch, got %+v", ent)
		}
	})

	t.Run("fallback match by reference alone", func(t *testing.T) {
		ent := findDistEntry(doc, "acme/lib", "9.9.9.9", "ref100")
		if ent == nil {
			t.Fatal("expected reference fallback match")
		}
		if ent.Version != "1.0.0" {
			t.Errorf("wrong entry: %+v", ent)
		}
	})

	t.Run("no match", func(t *testing.T) {
		if ent := findDistEntry(doc, "acme/lib", "9.9.9.9", "nope"); ent != nil {
			t.Errorf("expected nil, got %+v", ent)
		}
	})

	t.Run("unknown package", func(t *testing.T) {
		if ent := findDistEntry(doc, "other/pkg", "2.0.0.0", "ref200"); ent != nil {
			t.Errorf("expected nil, got %+v", ent)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		if ent := findDistEntry([]byte("{nope"), "acme/lib", "2.0.0.0", "ref200"); ent != nil {
			t.Errorf("expected nil, got %+v", ent)
		}
	})

	t.Run("unset sentinel removes carried field", func(t *testing.T) {
		// "dist": "__unset" wipes the inherited dist for entry 1.9.0.
		// A buggy plain carry-forward would match 1.9.0 (version +
		// inherited reference); correct unset handling leaves 1.9.0
		// without a dist, so the reference-alone fallback resolves to
		// the 2.0.0 entry that really owns the reference.
		minified := []byte(`{
			"packages": {
				"acme/lib": [
					{
						"version": "2.0.0",
						"version_normalized": "2.0.0.0",
						"dist": {"url": "https://example.com/2.0.0.zip", "type": "zip", "reference": "shared"}
					},
					{
						"version": "1.9.0",
						"version_normalized": "1.9.0.0",
						"dist": "__unset"
					}
				]
			}
		}`)
		ent := findDistEntry(minified, "acme/lib", "1.9.0.0", "shared")
		if ent == nil {
			t.Fatal("expected reference fallback to the entry owning the reference")
		}
		if ent.Version != "2.0.0" {
			t.Errorf("dist survived __unset: got version %s", ent.Version)
		}
	})

	t.Run("minified inheritance carries dist forward", func(t *testing.T) {
		// dist omitted in the second entry — inherited from the
		// first (aliased version pointing at the same commit).
		minified := []byte(`{
			"packages": {
				"acme/lib": [
					{
						"version": "2.0.0",
						"version_normalized": "2.0.0.0",
						"dist": {"url": "https://example.com/2.0.0.zip", "type": "zip", "reference": "shared"}
					},
					{
						"version": "1.9.0",
						"version_normalized": "1.9.0.0"
					}
				]
			}
		}`)
		ent := findDistEntry(minified, "acme/lib", "1.9.0.0", "shared")
		if ent == nil {
			t.Fatal("expected a match")
		}
		if ent.Version != "1.9.0" || ent.Dist.URL != "https://example.com/2.0.0.zip" {
			t.Errorf("inheritance broken: %+v", ent)
		}
	})
}
