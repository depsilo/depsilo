package resolvers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// All tests use a single canonical timestamp so equality assertions
// stay simple. UTC at second precision matches what every public
// registry returns.
var fixedTime = time.Date(2026, 6, 15, 12, 34, 56, 0, time.UTC)

func mustParseRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v.UTC()
}

func TestNpmResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Scoped packages: the wire format is "@scope%2Fname" but
		// Go's net/http decodes the path before exposing r.URL.Path,
		// so the actual incoming Path is "/@scope/name" — we assert
		// on the raw RequestURI to confirm the wire-format encoding
		// is correct.
		switch r.URL.Path {
		case "/lodash":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"time":{"4.17.21":"2026-06-15T12:34:56.000Z","modified":"2026-06-20T00:00:00.000Z"}}`))
		case "/@scope/internal":
			if r.RequestURI != "/@scope%2Finternal" {
				http.Error(w, "expected slash-encoded path on wire, got "+r.RequestURI, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"time":{"1.0.0":"2026-06-15T12:34:56.000Z"}}`))
		case "/missing":
			http.NotFound(w, r)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	r := &npmResolver{client: http.DefaultClient, base: srv.URL}

	t.Run("plain package", func(t *testing.T) {
		got, err := r.Lookup(context.Background(), "lodash", "4.17.21")
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		if !got.Equal(mustParseRFC3339(t, "2026-06-15T12:34:56Z")) {
			t.Errorf("got %v, want 2026-06-15T12:34:56Z", got)
		}
	})
	t.Run("scoped package URL-encoded", func(t *testing.T) {
		got, err := r.Lookup(context.Background(), "@scope/internal", "1.0.0")
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		if !got.Equal(mustParseRFC3339(t, "2026-06-15T12:34:56Z")) {
			t.Errorf("got %v, want 2026-06-15T12:34:56Z", got)
		}
	})
	t.Run("missing package", func(t *testing.T) {
		_, err := r.Lookup(context.Background(), "missing", "1.0.0")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("missing version", func(t *testing.T) {
		_, err := r.Lookup(context.Background(), "lodash", "99.99.99")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestPypiResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/requests/2.32.3/json":
			_, _ = w.Write([]byte(`{"urls":[
				{"upload_time_iso_8601":"2026-06-16T01:02:03.000Z"},
				{"upload_time_iso_8601":"2026-06-15T12:34:56.000Z"}
			]}`))
		case "/empty/1.0/json":
			_, _ = w.Write([]byte(`{"urls":[]}`))
		case "/gone/1.0/json":
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()
	r := &pypiResolver{client: http.DefaultClient, base: srv.URL}

	t.Run("earliest of multiple distributions", func(t *testing.T) {
		got, err := r.Lookup(context.Background(), "requests", "2.32.3")
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		if !got.Equal(mustParseRFC3339(t, "2026-06-15T12:34:56Z")) {
			t.Errorf("got %v, want earliest 2026-06-15T12:34:56Z", got)
		}
	})
	t.Run("missing upload_time", func(t *testing.T) {
		_, err := r.Lookup(context.Background(), "empty", "1.0")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("404", func(t *testing.T) {
		_, err := r.Lookup(context.Background(), "gone", "1.0")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestCargoResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/crates/serde" {
			_, _ = w.Write([]byte(`{"versions":[
				{"num":"1.0.197","created_at":"2026-06-15T12:34:56Z"},
				{"num":"1.0.196","created_at":"2026-06-01T00:00:00Z"}
			]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	r := &cargoResolver{client: http.DefaultClient, base: srv.URL}

	got, err := r.Lookup(context.Background(), "serde", "1.0.197")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !got.Equal(fixedTime) {
		t.Errorf("got %v, want %v", got, fixedTime)
	}

	if _, err := r.Lookup(context.Background(), "serde", "99.0.0"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing version err = %v, want ErrNotFound", err)
	}
}

func TestRubygemsResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/versions/rails.json" {
			_, _ = w.Write([]byte(`[{"number":"7.1.0","created_at":"2026-06-15T12:34:56Z"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	r := &rubygemsResolver{client: http.DefaultClient, base: srv.URL}

	got, err := r.Lookup(context.Background(), "rails", "7.1.0")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !got.Equal(fixedTime) {
		t.Errorf("got %v, want %v", got, fixedTime)
	}
}

func TestComposerResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/p2/symfony/console.json" {
			_, _ = w.Write([]byte(`{"packages":{"symfony/console":[
				{"version":"v7.0.0","time":"2026-06-15T12:34:56+00:00"}
			]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	r := &composerResolver{client: http.DefaultClient, base: srv.URL}

	got, err := r.Lookup(context.Background(), "symfony/console", "v7.0.0")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !got.Equal(fixedTime) {
		t.Errorf("got %v, want %v", got, fixedTime)
	}
}

func TestComposerResolver_DevVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Branch versions live ONLY in the ~dev metadata file; the
		// main file must not be consulted for them.
		if r.URL.Path == "/p2/symfony/console~dev.json" {
			_, _ = w.Write([]byte(`{"packages":{"symfony/console":[
				{"version":"dev-main","time":"2026-06-15T12:34:56+00:00"}
			]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	r := &composerResolver{client: http.DefaultClient, base: srv.URL}

	got, err := r.Lookup(context.Background(), "symfony/console", "dev-main")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !got.Equal(fixedTime) {
		t.Errorf("got %v, want %v", got, fixedTime)
	}
}

func TestNugetResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// IDs and versions must be lowercased on the wire.
		switch r.URL.Path {
		case "/newtonsoft.json/13.0.3.json":
			_, _ = w.Write([]byte(`{"catalogEntry":{"published":"2026-06-15T12:34:56Z"}}`))
		case "/yanked/1.0.0.json":
			_, _ = w.Write([]byte(`{"catalogEntry":{"published":"0001-01-01T00:00:00Z"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	r := &nugetResolver{client: http.DefaultClient, base: srv.URL}

	t.Run("normal version + lowercased path", func(t *testing.T) {
		got, err := r.Lookup(context.Background(), "Newtonsoft.Json", "13.0.3")
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		if !got.Equal(fixedTime) {
			t.Errorf("got %v, want %v", got, fixedTime)
		}
	})
	t.Run("yanked-sentinel year 0001 → ErrNotFound", func(t *testing.T) {
		_, err := r.Lookup(context.Background(), "yanked", "1.0.0")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestHFResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Both with-revision and without-revision endpoints should
		// resolve to the same lastModified for our test fixture.
		if r.URL.Path == "/models/bert-base-uncased" ||
			strings.HasPrefix(r.URL.Path, "/models/bert-base-uncased/revision/") ||
			r.URL.Path == "/datasets/org/corpus" ||
			strings.HasPrefix(r.URL.Path, "/datasets/org/corpus/revision/") {
			_, _ = w.Write([]byte(`{"lastModified":"2026-06-15T12:34:56.000Z"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	r := &hfResolver{client: http.DefaultClient, base: srv.URL}

	// "main" branch and empty version both route to the model doc.
	for _, ver := range []string{"", "main"} {
		got, err := r.Lookup(context.Background(), "bert-base-uncased", ver)
		if err != nil {
			t.Fatalf("Lookup ver=%q: %v", ver, err)
		}
		if !got.Equal(fixedTime) {
			t.Errorf("ver=%q got %v, want %v", ver, got, fixedTime)
		}
	}
	// Specific revision routes to /revision/<ref>.
	got, err := r.Lookup(context.Background(), "bert-base-uncased", "abcd1234")
	if err != nil {
		t.Fatalf("revision lookup: %v", err)
	}
	if !got.Equal(fixedTime) {
		t.Errorf("got %v, want %v", got, fixedTime)
	}

	got, err = r.Lookup(context.Background(), "datasets/org/corpus", "v2")
	if err != nil {
		t.Fatalf("dataset revision lookup: %v", err)
	}
	if !got.Equal(fixedTime) {
		t.Errorf("dataset got %v, want %v", got, fixedTime)
	}
}

func TestLastModifiedResolver(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Last-Modified", fixedTime.Format(http.TimeFormat))
			w.WriteHeader(http.StatusOK)
		case "/no-header":
			w.WriteHeader(http.StatusOK)
		case "/missing":
			http.NotFound(w, r)
		case "/forbidden-then-range-ok":
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			// Range fallback path.
			w.Header().Set("Last-Modified", fixedTime.Format(http.TimeFormat))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte{0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Run("HEAD success", func(t *testing.T) {
		r := &lastModifiedResolver{
			client:    http.DefaultClient,
			ecosystem: "test",
			urlFn:     func(_, _ string) (string, error) { return srv.URL + "/ok", nil },
		}
		got, err := r.Lookup(context.Background(), "pkg", "1.0")
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		if !got.Equal(fixedTime) {
			t.Errorf("got %v, want %v", got, fixedTime)
		}
	})
	t.Run("HEAD missing 404", func(t *testing.T) {
		r := &lastModifiedResolver{
			client:    http.DefaultClient,
			ecosystem: "test",
			urlFn:     func(_, _ string) (string, error) { return srv.URL + "/missing", nil },
		}
		_, err := r.Lookup(context.Background(), "pkg", "1.0")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("HEAD has no Last-Modified", func(t *testing.T) {
		r := &lastModifiedResolver{
			client:    http.DefaultClient,
			ecosystem: "test",
			urlFn:     func(_, _ string) (string, error) { return srv.URL + "/no-header", nil },
		}
		_, err := r.Lookup(context.Background(), "pkg", "1.0")
		if !errors.Is(err, ErrUpstreamUnavailable) {
			t.Errorf("err = %v, want ErrUpstreamUnavailable", err)
		}
	})
	t.Run("HEAD 403 → range GET fallback", func(t *testing.T) {
		r := &lastModifiedResolver{
			client:    http.DefaultClient,
			ecosystem: "test",
			urlFn:     func(_, _ string) (string, error) { return srv.URL + "/forbidden-then-range-ok", nil },
		}
		got, err := r.Lookup(context.Background(), "pkg", "1.0")
		if err != nil {
			t.Fatalf("Lookup: %v", err)
		}
		if !got.Equal(fixedTime) {
			t.Errorf("got %v, want %v", got, fixedTime)
		}
	})
	t.Run("urlFn error → ErrUnsupported", func(t *testing.T) {
		r := &lastModifiedResolver{
			client:    http.DefaultClient,
			ecosystem: "test",
			urlFn:     func(_, _ string) (string, error) { return "", errBuildURL },
		}
		_, err := r.Lookup(context.Background(), "pkg", "1.0")
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("err = %v, want ErrUnsupported", err)
		}
	})
}

var errBuildURL = stubError("synthetic urlFn failure")

type stubError string

func (e stubError) Error() string { return string(e) }

func TestMavenArtifactURL(t *testing.T) {
	cases := []struct {
		pkg, ver, want string
		isErr          bool
	}{
		{"org.apache.commons:commons-lang3", "3.14.0", "https://repo1.maven.org/maven2/org/apache/commons/commons-lang3/3.14.0/commons-lang3-3.14.0.jar", false},
		{"junit:junit", "4.13.2", "https://repo1.maven.org/maven2/junit/junit/4.13.2/junit-4.13.2.jar", false},
		{"nogroup", "1.0", "", true},
		{"group:", "1.0", "", true},
	}
	for _, c := range cases {
		got, err := mavenArtifactURL(c.pkg, c.ver)
		if c.isErr {
			if err == nil {
				t.Errorf("%q: expected error", c.pkg)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.pkg, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q → %q\nwant %q", c.pkg, got, c.want)
		}
	}
}

func TestAlpineArtifactURL(t *testing.T) {
	got, err := alpineArtifactURL("v3.19/main/x86_64/curl", "8.5.0-r0")
	if err != nil {
		t.Fatalf("alpineArtifactURL: %v", err)
	}
	want := "https://dl-cdn.alpinelinux.org/alpine/v3.19/main/x86_64/curl-8.5.0-r0.apk"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := alpineArtifactURL("notenoughparts", "1.0"); err == nil {
		t.Errorf("expected error for incomplete pkg id")
	}
}

func TestCranResolver_LatestVersionFromDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/web/packages/dplyr/DESCRIPTION" {
			_, _ = w.Write([]byte(`Package: dplyr
Version: 1.1.4
Title: A Grammar of Data Manipulation
Date/Publication: 2026-06-15 12:34:56 UTC
`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	r := &cranResolver{client: http.DefaultClient, base: srv.URL}

	got, err := r.Lookup(context.Background(), "dplyr", "1.1.4")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !got.Equal(fixedTime) {
		t.Errorf("got %v, want %v", got, fixedTime)
	}
}

func TestCranResolver_DateOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/web/packages/p/DESCRIPTION" {
			_, _ = w.Write([]byte("Package: p\nVersion: 1.0\nDate/Publication: 2026-06-15\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	r := &cranResolver{client: http.DefaultClient, base: srv.URL}
	got, err := r.Lookup(context.Background(), "p", "1.0")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	want := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRegistryHasAllExpected(t *testing.T) {
	reg := NewRegistry()
	expected := []string{
		"npm", "pypi", "cargo", "rubygems", "composer", "nuget", "huggingface",
		"cran", "maven", "helm", "conda", "alpine", "docker",
	}
	for _, eco := range expected {
		if _, ok := reg[eco]; !ok {
			t.Errorf("registry missing resolver for %q", eco)
		}
	}
}
