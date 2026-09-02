package npm

import (
	"encoding/json"
	"testing"
)

func TestRewriteTarballURLs(t *testing.T) {
	tests := []struct {
		name     string
		document string
		baseURL  string
		want     string
	}{
		{
			name:     "unscoped relative",
			document: `{"name":"lodash","versions":{"4.17.21":{"dist":{"tarball":"https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz"}}}}`,
			baseURL:  "",
			want:     "/npm/lodash/-/lodash-4.17.21.tgz",
		},
		{
			name:     "unscoped absolute",
			document: `{"name":"lodash","versions":{"4.17.21":{"dist":{"tarball":"https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz"}}}}`,
			baseURL:  "http://localhost:23333",
			want:     "http://localhost:23333/npm/lodash/-/lodash-4.17.21.tgz",
		},
		{
			name:     "scoped relative",
			document: `{"name":"@types/node","versions":{"20.0.0":{"dist":{"tarball":"https://registry.npmjs.org/@types/node/-/node-20.0.0.tgz"}}}}`,
			baseURL:  "",
			want:     "/npm/@types/node/-/node-20.0.0.tgz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RewriteTarballURLs([]byte(tt.document), tt.baseURL)
			if err != nil {
				t.Fatalf("RewriteTarballURLs() error = %v", err)
			}
			var doc struct {
				Name     string `json:"name"`
				Versions map[string]struct {
					Dist struct {
						Tarball string `json:"tarball"`
					} `json:"dist"`
				} `json:"versions"`
			}
			if err := json.Unmarshal(got, &doc); err != nil {
				t.Fatalf("unmarshal rewritten document: %v", err)
			}
			for _, version := range doc.Versions {
				if version.Dist.Tarball != tt.want {
					t.Fatalf("tarball = %q, want %q", version.Dist.Tarball, tt.want)
				}
			}
		})
	}
}

func TestRewriteTarballURLsTargetsOnlyTarballValues(t *testing.T) {
	in := []byte(`{"name":"lodash","description":"see /npm/ for details","versions":{"4.17.21":{"dist":{"tarball":"https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz"},"description":"still /npm/ here"}}}`)
	got, err := RewriteTarballURLs(in, "http://localhost:23333")
	if err != nil {
		t.Fatalf("RewriteTarballURLs() error = %v", err)
	}

	var doc struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Versions    map[string]struct {
			Dist struct {
				Tarball string `json:"tarball"`
			} `json:"dist"`
			Description string `json:"description"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("unmarshal rewritten document: %v", err)
	}
	if doc.Description != "see /npm/ for details" {
		t.Fatalf("top-level description = %q, want untouched", doc.Description)
	}
	version := doc.Versions["4.17.21"]
	if version.Description != "still /npm/ here" {
		t.Fatalf("version description = %q, want untouched", version.Description)
	}
	if version.Dist.Tarball != "http://localhost:23333/npm/lodash/-/lodash-4.17.21.tgz" {
		t.Fatalf("tarball = %q, want base URL applied", version.Dist.Tarball)
	}
}
