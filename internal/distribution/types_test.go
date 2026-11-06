package distribution_test

import (
	"bytes"
	"errors"
	"testing"

	"depsilo/internal/distribution"
)

func TestParseManifestRejectsCaseAliasesAndUnknownFields(t *testing.T) {
	raw := manifestBytes(t, "python", "v1", contentObject([]byte("example"), "example"))
	if _, err := distribution.ParseManifest(raw); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, from, to string
	}{
		{"top-level case alias", `"name":`, `"Name":`},
		{"top-level mixed duplicate", `"name":`, `"Name":"shadow","name":`},
		{"target case alias", `"os":`, `"OS":`},
		{"object case alias", `"size":`, `"SIZE":`},
		{"object Unicode case alias", `"size":`, `"ſize":`},
		{"object mixed duplicate", `"size":`, `"Size":1,"size":`},
		{"exact duplicate", `"name":`, `"name":"shadow","name":`},
		{"unknown top-level", `"name":`, `"unrecognized":false,"name":`},
		{"unknown target", `"os":`, `"unknown_target":false,"os":`},
		{"unknown object", `"size":`, `"unknown_object":false,"size":`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := bytes.Replace(raw, []byte(tc.from), []byte(tc.to), 1)
			if bytes.Equal(raw, changed) {
				t.Fatal("fixture key was not replaced")
			}
			if _, err := distribution.ParseManifest(changed); !errors.Is(err, distribution.ErrInvalid) {
				t.Fatalf("ambiguous or unknown key accepted: %v", err)
			}
		})
	}
}
