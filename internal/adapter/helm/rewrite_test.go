package helm

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func rewriteHelmIndexString(t *testing.T, repoURL, index string) string {
	t.Helper()
	// One-byte reads on both sides exercise the line buffering seams.
	source := io.NopCloser(iotest.OneByteReader(strings.NewReader(index)))
	rewritten := rewriteHelmIndex(source, repoURL)
	defer rewritten.Close()
	out, err := io.ReadAll(iotest.OneByteReader(rewritten))
	if err != nil {
		t.Fatalf("read rewritten index: %v", err)
	}
	return string(out)
}

func TestRewriteHelmIndexMakesRepositoryChartURLsRelative(t *testing.T) {
	t.Parallel()
	const repo = "https://charts.example.com/stable"
	cases := []struct {
		name  string
		index string
		want  string
	}{
		{
			name: "block list under the repository",
			index: "entries:\n  nginx:\n  - name: nginx\n    urls:\n" +
				"    - https://charts.example.com/stable/nginx-1.2.3.tgz\n" +
				"    - https://charts.example.com/stable/charts/nginx-1.2.3.tgz\n    version: 1.2.3\n",
			want: "entries:\n  nginx:\n  - name: nginx\n    urls:\n" +
				"    - nginx-1.2.3.tgz\n" +
				"    - charts/nginx-1.2.3.tgz\n    version: 1.2.3\n",
		},
		{
			name: "foreign host, other directory, query and scheme stay absolute",
			index: "entries:\n  nginx:\n  - urls:\n" +
				"    - https://cdn.example.net/stable/nginx-1.2.3.tgz\n" +
				"    - https://charts.example.com/other/nginx-1.2.3.tgz\n" +
				"    - https://charts.example.com/stable/nginx-1.2.3.tgz?token=abc\n" +
				"    - http://charts.example.com/stable/nginx-1.2.3.tgz\n" +
				"    - https://charts.example.com/stable/../nginx-1.2.3.tgz\n" +
				"    - https://charts.example.com/stablex/nginx-1.2.3.tgz\n",
			want: "entries:\n  nginx:\n  - urls:\n" +
				"    - https://cdn.example.net/stable/nginx-1.2.3.tgz\n" +
				"    - https://charts.example.com/other/nginx-1.2.3.tgz\n" +
				"    - https://charts.example.com/stable/nginx-1.2.3.tgz?token=abc\n" +
				"    - http://charts.example.com/stable/nginx-1.2.3.tgz\n" +
				"    - https://charts.example.com/stable/../nginx-1.2.3.tgz\n" +
				"    - https://charts.example.com/stablex/nginx-1.2.3.tgz\n",
		},
		{
			name:  "relative URL stays as is",
			index: "entries:\n  nginx:\n  - urls:\n    - nginx-1.2.3.tgz\n",
			want:  "entries:\n  nginx:\n  - urls:\n    - nginx-1.2.3.tgz\n",
		},
		{
			name:  "host case and default port",
			index: "entries:\n  a:\n  - urls:\n    - https://CHARTS.example.com:443/stable/a-1.tgz\n",
			want:  "entries:\n  a:\n  - urls:\n    - a-1.tgz\n",
		},
		{
			name: "inline flow list with quotes",
			index: "entries:\n  a:\n  - name: a\n" +
				"    urls: [https://charts.example.com/stable/a-1.tgz, \"https://charts.example.com/stable/a-1b.tgz\", 'https://cdn.example.net/a-1.tgz']\n",
			want: "entries:\n  a:\n  - name: a\n" +
				"    urls: [a-1.tgz, \"a-1b.tgz\", 'https://cdn.example.net/a-1.tgz']\n",
		},
		{
			name: "quoted block items and trailing comment",
			index: "entries:\n  a:\n  - urls:\n" +
				"    - \"https://charts.example.com/stable/a-1.tgz\"\n" +
				"    - 'https://charts.example.com/stable/a-2.tgz'   # mirror\n" +
				"    - \"https://charts.example.com/stable/a\\u002d3.tgz\"\n",
			want: "entries:\n  a:\n  - urls:\n" +
				"    - \"a-1.tgz\"\n" +
				"    - 'a-2.tgz'   # mirror\n" +
				"    - \"https://charts.example.com/stable/a\\u002d3.tgz\"\n",
		},
		{
			name: "CRLF line endings are kept",
			index: "apiVersion: v1\r\nentries:\r\n  a:\r\n  - urls:\r\n" +
				"    - https://charts.example.com/stable/a-1.tgz\r\n    version: \"1\"\r\n",
			want: "apiVersion: v1\r\nentries:\r\n  a:\r\n  - urls:\r\n" +
				"    - a-1.tgz\r\n    version: \"1\"\r\n",
		},
		{
			name: "non-compact indentation and no final newline",
			index: "entries:\n    a:\n        -   name: a\n            urls:\n" +
				"                - https://charts.example.com/stable/a-1.tgz\n            version: 1",
			want: "entries:\n    a:\n        -   name: a\n            urls:\n" +
				"                - a-1.tgz\n            version: 1",
		},
		{
			name: "only entries urls are touched; text and digests are kept",
			index: "apiVersion: v1\n" +
				"urls:\n- https://charts.example.com/stable/top.tgz\n" +
				"entries:\n  a:\n  - description: |-\n" +
				"      urls:\n      - https://charts.example.com/stable/text.tgz\n" +
				"    digest: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08\n" +
				"    home: https://charts.example.com/stable/a-1.tgz\n" +
				"    sources:\n    - https://charts.example.com/stable/src.tgz\n" +
				"    urls:\n\n    # first mirror\n    - https://charts.example.com/stable/a-1.tgz\n" +
				"generated: \"2026-11-19T00:00:00Z\"\n" +
				"serverInfo:\n  urls:\n  - https://charts.example.com/stable/info.tgz\n",
			want: "apiVersion: v1\n" +
				"urls:\n- https://charts.example.com/stable/top.tgz\n" +
				"entries:\n  a:\n  - description: |-\n" +
				"      urls:\n      - https://charts.example.com/stable/text.tgz\n" +
				"    digest: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08\n" +
				"    home: https://charts.example.com/stable/a-1.tgz\n" +
				"    sources:\n    - https://charts.example.com/stable/src.tgz\n" +
				"    urls:\n\n    # first mirror\n    - a-1.tgz\n" +
				"generated: \"2026-11-19T00:00:00Z\"\n" +
				"serverInfo:\n  urls:\n  - https://charts.example.com/stable/info.tgz\n",
		},
		{
			name: "a segment that would read as a scheme or YAML indicator gets ./",
			index: "entries:\n  a:\n  - urls:\n" +
				"    - https://charts.example.com/stable/a:1.tgz\n" +
				"    - https://charts.example.com/stable/@a-1.tgz\n",
			want: "entries:\n  a:\n  - urls:\n" +
				"    - ./a:1.tgz\n" +
				"    - ./@a-1.tgz\n",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := rewriteHelmIndexString(t, repo, testCase.index); got != testCase.want {
				t.Fatalf("rewritten index:\n%q\nwant:\n%q", got, testCase.want)
			}
		})
	}
}

func TestRewriteHelmIndexAcceptsARepositoryRoot(t *testing.T) {
	t.Parallel()
	index := "entries:\n  a:\n  - urls:\n    - http://127.0.0.1:8080/a-1.tgz\n"
	for _, repo := range []string{"http://127.0.0.1:8080", "http://127.0.0.1:8080/"} {
		if got, want := rewriteHelmIndexString(t, repo, index), "entries:\n  a:\n  - urls:\n    - a-1.tgz\n"; got != want {
			t.Fatalf("repo %q: rewritten index %q, want %q", repo, got, want)
		}
	}
}

func TestRewriteHelmIndexPassesOverlongLinesThrough(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 3*helmRewriteBufferSize)
	index := "entries:\n  a:\n  - description: " + long + "\n    urls:\n" +
		"    - https://charts.example.com/stable/a-1.tgz\n" +
		"    - https://charts.example.com/stable/" + long + ".tgz\n" +
		"    - https://charts.example.com/stable/a-2.tgz\n"
	want := "entries:\n  a:\n  - description: " + long + "\n    urls:\n" +
		"    - a-1.tgz\n" +
		"    - https://charts.example.com/stable/" + long + ".tgz\n" +
		"    - a-2.tgz\n"
	if got := rewriteHelmIndexString(t, "https://charts.example.com/stable", index); got != want {
		t.Fatalf("overlong lines were not passed through unchanged")
	}
}

// The rewritten index must still resolve chart identities: the gate matches a
// relative URL against the requested path exactly as it matched the absolute
// one by path.
func TestScanHelmIndexResolvesRewrittenURLs(t *testing.T) {
	t.Parallel()
	index := strings.ReplaceAll(helmTestIndex, "https://charts.example.com/", "https://charts.example.com/stable/")
	rewritten := rewriteHelmIndexString(t, "https://charts.example.com/stable", index)
	if strings.Contains(rewritten, "https://charts.example.com/stable/") {
		t.Fatalf("index was not rewritten:\n%s", rewritten)
	}
	matches, err := scanHelmIndex(strings.NewReader(rewritten), "acme-demo-1.0.0.tgz", "acme-demo-1.0.0.tgz")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].chart != "acme-demo" || matches[0].version != "1.0.0" {
		t.Fatalf("matches = %+v, want acme-demo@1.0.0", matches)
	}
}

// The cache fill pipeline does a client write, a storage enqueue and a timer
// re-arm per Read, so one Read must carry as many already buffered lines as
// the caller's buffer holds rather than one line at a time.
func TestRewriteHelmIndexFillsTheReadBuffer(t *testing.T) {
	t.Parallel()
	var index strings.Builder
	index.WriteString("entries:\n  a:\n")
	for range 200 {
		index.WriteString("  - urls:\n    - https://charts.example.com/stable/a-1.tgz\n    version: 1\n")
	}
	rewritten := rewriteHelmIndex(io.NopCloser(strings.NewReader(index.String())), "https://charts.example.com/stable")
	defer rewritten.Close()
	buffer := make([]byte, 32<<10)
	n, err := rewritten.Read(buffer)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if lines := strings.Count(string(buffer[:n]), "\n"); lines < 100 {
		t.Fatalf("first read returned %d lines (%d bytes), want the buffered index in one read", lines, n)
	}
	if !strings.Contains(string(buffer[:n]), "    - a-1.tgz\n") {
		t.Fatalf("first read was not rewritten: %q", buffer[:n])
	}
}

// A line that is not yet complete must not hold back lines already rewritten:
// the reader returns what it has instead of waiting on the source.
func TestRewriteHelmIndexDoesNotWaitForAnIncompleteLine(t *testing.T) {
	t.Parallel()
	source, writer := io.Pipe()
	rewritten := rewriteHelmIndex(source, "https://charts.example.com/stable")
	defer rewritten.Close()
	go func() {
		_, _ = writer.Write([]byte("entries:\n  a:\n  - urls:\n    - https://charts.example.com/stable/a-1.tgz\n    vers"))
	}()
	var got strings.Builder
	buffer := make([]byte, 4096)
	for !strings.Contains(got.String(), "a-1.tgz\n") {
		n, err := rewritten.Read(buffer)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		got.Write(buffer[:n])
	}
	_ = writer.Close()
	if got.String() != "entries:\n  a:\n  - urls:\n    - a-1.tgz\n" {
		t.Fatalf("read %q before the incomplete line", got.String())
	}
}
