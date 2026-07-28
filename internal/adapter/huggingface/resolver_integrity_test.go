package huggingface

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

func newIntegrityTestUpstream(t *testing.T, originURL string) *upstream.Upstream {
	t.Helper()
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name:      "integrity-test",
		URL:       originURL,
		Priority:  1,
		ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	return pool.Snapshot()[0]
}

func resolveIntegrityTest(
	t *testing.T,
	selected *upstream.Upstream,
	headers http.Header,
) (*resolvedResponse, error) {
	t.Helper()
	const target = "/repo/resolve/main/model.bin"
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header = headers.Clone()
	return newResolver().resolve(
		context.Background(),
		selected,
		request,
		target,
		true,
	)
}

func TestResolverRejectsCrossOriginContentLengthThatDisagreesWithHub(t *testing.T) {
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "3")
		_, _ = io.WriteString(w, "abc")
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", artifact.URL+"/blob")
		w.Header().Set("X-Linked-Size", "10")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	selected := newIntegrityTestUpstream(t, origin.URL)
	result, err := resolveIntegrityTest(t, selected, nil)
	if result != nil {
		_ = result.Body.Close()
		t.Fatalf("mismatched artifact returned a result: %+v", result)
	}
	if !errors.Is(err, cache.ErrBodySizeMismatch) {
		t.Fatalf("resolve error = %v, want ErrBodySizeMismatch", err)
	}
	if selected.IsHealthy() {
		t.Fatal("cross-origin size mismatch was not reported as a critical failure")
	}
}

func TestResolverRejectsContentCodingAfterForcingIdentity(t *testing.T) {
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept-Encoding"); got != "identity" {
			t.Errorf("Accept-Encoding = %q, want identity", got)
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", "3")
		_, _ = io.WriteString(w, "abc")
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", artifact.URL+"/blob")
		w.Header().Set("X-Linked-Size", "3")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	selected := newIntegrityTestUpstream(t, origin.URL)
	result, err := resolveIntegrityTest(t, selected, nil)
	if result != nil {
		_ = result.Body.Close()
		t.Fatalf("content-coded identity response returned a result: %+v", result)
	}
	if !errors.Is(err, cache.ErrBodySizeMismatch) {
		t.Fatalf("resolve error = %v, want ErrBodySizeMismatch", err)
	}
	if selected.IsHealthy() {
		t.Fatal("content-coded identity response was not critically latched")
	}
}

func TestResolverDetectsChunkedArtifactShorterThanHubDeclaration(t *testing.T) {
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "abc")
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", artifact.URL+"/blob")
		w.Header().Set("X-Linked-Size", "4")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	selected := newIntegrityTestUpstream(t, origin.URL)
	result, err := resolveIntegrityTest(t, selected, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	body, readErr := io.ReadAll(result.Body)
	if string(body) != "abc" {
		t.Fatalf("body = %q, want abc", body)
	}
	if !errors.Is(readErr, cache.ErrBodySizeMismatch) {
		t.Fatalf("body error = %v, want ErrBodySizeMismatch", readErr)
	}
	if selected.IsHealthy() {
		t.Fatal("streamed size mismatch was not reported as a critical failure")
	}
}

func TestResolverDoesNotTrustCrossOriginLinkedMetadata(t *testing.T) {
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "3")
		w.Header().Set("X-Linked-Etag", `"cdn-untrusted"`)
		w.Header().Set("X-Linked-Size", "999")
		w.Header().Set("X-Repo-Commit", "cdn-untrusted")
		_, _ = io.WriteString(w, "abc")
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", artifact.URL+"/blob")
		w.Header().Set("X-Linked-Etag", `"hub-trusted"`)
		w.Header().Set("X-Linked-Size", "3")
		w.Header().Set("X-Repo-Commit", "hub-trusted")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	selected := newIntegrityTestUpstream(t, origin.URL)
	result, err := resolveIntegrityTest(t, selected, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "abc" {
		t.Fatalf("body = %q, want abc", body)
	}
	for name, want := range map[string]string{
		"X-Linked-Etag": `"hub-trusted"`,
		"X-Linked-Size": "3",
		"X-Repo-Commit": "hub-trusted",
	} {
		if got := result.Header.Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestResolverUsesPartialLengthFor206(t *testing.T) {
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=0-3" {
			t.Errorf("artifact Range = %q, want bytes=0-3", got)
		}
		w.Header().Set("Content-Length", "4")
		w.Header().Set("Content-Range", "bytes 0-3/8")
		w.Header().Set("X-Linked-Size", "4")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "abcd")
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", artifact.URL+"/blob")
		w.Header().Set("X-Linked-Size", "8")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	selected := newIntegrityTestUpstream(t, origin.URL)
	result, err := resolveIntegrityTest(
		t,
		selected,
		http.Header{"Range": {"bytes=0-3"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "abcd" || result.Size != 4 {
		t.Fatalf("partial result = (%q, size=%d), want (abcd, 4)", body, result.Size)
	}
	if got := result.Header.Get("X-Linked-Size"); got != "8" {
		t.Fatalf("Hub X-Linked-Size = %q, want 8", got)
	}
}

func TestResolverAcceptsSubsetOfRequestedRange(t *testing.T) {
	body := strings.Repeat("x", 400)
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=0-999" {
			t.Errorf("artifact Range = %q, want bytes=0-999", got)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Content-Range", "bytes 100-499/2000")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", artifact.URL+"/blob")
		w.Header().Set("X-Linked-Size", "2000")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	selected := newIntegrityTestUpstream(t, origin.URL)
	result, err := resolveIntegrityTest(
		t,
		selected,
		http.Header{"Range": {"bytes=0-999"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(result.Body)
	_ = result.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body || result.Size != int64(len(body)) {
		t.Fatalf("subset result = (%d bytes, size=%d)", len(got), result.Size)
	}
	if !selected.IsHealthy() {
		t.Fatal("valid partial subset marked upstream unhealthy")
	}
}

func TestResolverRejectsPartialContentOutsideRequestedRange(t *testing.T) {
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "4")
		w.Header().Set("Content-Range", "bytes 4-7/8")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "efgh")
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", artifact.URL+"/blob")
		w.Header().Set("X-Linked-Size", "8")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	selected := newIntegrityTestUpstream(t, origin.URL)
	result, err := resolveIntegrityTest(
		t,
		selected,
		http.Header{"Range": {"bytes=0-3"}},
	)
	if result != nil {
		_ = result.Body.Close()
		t.Fatalf("mismatched range returned a result: %+v", result)
	}
	if !errors.Is(err, cache.ErrBodySizeMismatch) {
		t.Fatalf("resolve error = %v, want ErrBodySizeMismatch", err)
	}
	if selected.IsHealthy() {
		t.Fatal("mismatched partial range was not reported as a critical failure")
	}
}

func TestResolverRejectsCoalescedResponseAcrossLargeUnrequestedGap(t *testing.T) {
	const bodySize = 1001
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(bodySize))
		w.Header().Set("Content-Range", "bytes 0-1000/2000")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, strings.Repeat("x", bodySize))
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", artifact.URL+"/blob")
		w.Header().Set("X-Linked-Size", "2000")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	selected := newIntegrityTestUpstream(t, origin.URL)
	result, err := resolveIntegrityTest(
		t,
		selected,
		http.Header{"Range": {"bytes=0-0,1000-1000"}},
	)
	if result != nil {
		_ = result.Body.Close()
		t.Fatalf("large-gap response returned a result: %+v", result)
	}
	if !errors.Is(err, cache.ErrBodySizeMismatch) {
		t.Fatalf("resolve error = %v, want ErrBodySizeMismatch", err)
	}
	if selected.IsHealthy() {
		t.Fatal("large-gap response was not critically latched")
	}
}

func TestResolverRejectsPartialContentWithoutValidRangeContract(t *testing.T) {
	for _, test := range []struct {
		name        string
		rangeHeader string
		contentType string
	}{
		{name: "missing Range"},
		{name: "malformed Range", rangeHeader: "bytes=broken"},
		{
			name:        "multiple ranges without multipart response",
			rangeHeader: "bytes=0-1,4-5",
			contentType: "application/octet-stream",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", "4")
				w.Header().Set("Content-Range", "bytes 0-3/8")
				if test.contentType != "" {
					w.Header().Set("Content-Type", test.contentType)
				}
				w.WriteHeader(http.StatusPartialContent)
				_, _ = io.WriteString(w, "abcd")
			}))
			t.Cleanup(artifact.Close)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", artifact.URL+"/blob")
				w.Header().Set("X-Linked-Size", "8")
				w.WriteHeader(http.StatusFound)
			}))
			t.Cleanup(origin.Close)

			headers := make(http.Header)
			if test.rangeHeader != "" {
				headers.Set("Range", test.rangeHeader)
			}
			selected := newIntegrityTestUpstream(t, origin.URL)
			result, err := resolveIntegrityTest(t, selected, headers)
			if result != nil {
				_ = result.Body.Close()
				t.Fatalf("invalid partial contract returned a result: %+v", result)
			}
			if !errors.Is(err, cache.ErrBodySizeMismatch) {
				t.Fatalf("resolve error = %v, want ErrBodySizeMismatch", err)
			}
			if selected.IsHealthy() {
				t.Fatal("invalid partial contract was not reported as a critical failure")
			}
		})
	}
}

func TestResolverRejectsAmbiguousOrInvalidPartialHeaders(t *testing.T) {
	const multipartBody = "--first\r\n" +
		"Content-Range: bytes 0-1/8\r\n\r\nab\r\n" +
		"--first\r\n" +
		"Content-Range: bytes 4-5/8\r\n\r\nef\r\n" +
		"--first--\r\n"
	tests := []struct {
		name            string
		rangeHeader     string
		body            string
		artifactHeaders func(http.Header)
		originHeaders   func(http.Header)
	}{
		{
			name:        "duplicate Content-Range",
			rangeHeader: "bytes=0-3",
			body:        "abcd",
			artifactHeaders: func(headers http.Header) {
				headers.Add("Content-Range", "bytes 0-3/8")
				headers.Add("Content-Range", "bytes 4-7/8")
			},
		},
		{
			name:        "single part claims multipart without boundary",
			rangeHeader: "bytes=0-1",
			body:        "ab",
			artifactHeaders: func(headers http.Header) {
				headers.Set("Content-Range", "bytes 0-1/8")
				headers.Set("Content-Type", "multipart/byteranges")
			},
		},
		{
			name:        "single part claims multipart with invalid boundary",
			rangeHeader: "bytes=0-1",
			body:        "ab",
			artifactHeaders: func(headers http.Header) {
				headers.Set("Content-Range", "bytes 0-1/8")
				headers.Set(
					"Content-Type",
					`multipart/byteranges; boundary="invalid "`,
				)
			},
		},
		{
			name:        "single part has malformed Content-Type",
			rangeHeader: "bytes=0-1",
			body:        "ab",
			artifactHeaders: func(headers http.Header) {
				headers.Set("Content-Range", "bytes 0-1/8")
				headers.Set("Content-Type", "not a media type")
			},
		},
		{
			name:        "content coding despite forced identity",
			rangeHeader: "bytes=0-1",
			body:        "ab",
			artifactHeaders: func(headers http.Header) {
				headers.Set("Content-Range", "bytes 0-1/8")
				headers.Set("Content-Encoding", "gzip")
			},
		},
		{
			name:        "duplicate multipart Content-Type",
			rangeHeader: "bytes=0-1,4-5",
			body:        multipartBody,
			artifactHeaders: func(headers http.Header) {
				headers.Add("Content-Type", "multipart/byteranges; boundary=first")
				headers.Add("Content-Type", "multipart/byteranges; boundary=second")
			},
		},
		{
			name:        "duplicate Hub linked size",
			rangeHeader: "bytes=0-3",
			body:        "abcd",
			artifactHeaders: func(headers http.Header) {
				headers.Set("Content-Range", "bytes 0-3/8")
			},
			originHeaders: func(headers http.Header) {
				headers.Add("X-Linked-Size", "8")
				headers.Add("X-Linked-Size", "9")
			},
		},
		{
			name:        "combined malformed Hub linked size",
			rangeHeader: "bytes=0-3",
			body:        "abcd",
			artifactHeaders: func(headers http.Header) {
				headers.Set("Content-Range", "bytes 0-3/8")
			},
			originHeaders: func(headers http.Header) {
				headers.Set("X-Linked-Size", "8, 9")
			},
		},
		{
			name:        "oversized multipart boundary",
			rangeHeader: "bytes=0-1,4-5",
			body:        multipartBody,
			artifactHeaders: func(headers http.Header) {
				headers.Set(
					"Content-Type",
					"multipart/byteranges; boundary="+strings.Repeat("a", 71),
				)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", strconv.Itoa(len(test.body)))
				test.artifactHeaders(w.Header())
				w.WriteHeader(http.StatusPartialContent)
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(artifact.Close)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", artifact.URL+"/blob")
				if test.originHeaders != nil {
					test.originHeaders(w.Header())
				} else {
					w.Header().Set("X-Linked-Size", "8")
				}
				w.WriteHeader(http.StatusFound)
			}))
			t.Cleanup(origin.Close)

			selected := newIntegrityTestUpstream(t, origin.URL)
			result, err := resolveIntegrityTest(
				t,
				selected,
				http.Header{"Range": {test.rangeHeader}},
			)
			if result != nil {
				_ = result.Body.Close()
				t.Fatalf("ambiguous response returned a result: %+v", result)
			}
			if !errors.Is(err, cache.ErrBodySizeMismatch) {
				t.Fatalf("resolve error = %v, want ErrBodySizeMismatch", err)
			}
			if selected.IsHealthy() {
				t.Fatal("ambiguous response was not critically latched")
			}
		})
	}
}

func TestMultipartByteRangesBoundaryValidation(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "seventy characters",
			value: "multipart/byteranges; boundary=" + strings.Repeat("a", 70),
			want:  true,
		},
		{
			name:  "seventy one characters",
			value: "multipart/byteranges; boundary=" + strings.Repeat("a", 71),
		},
		{
			name:  "quoted interior space",
			value: `multipart/byteranges; boundary="valid boundary"`,
			want:  true,
		},
		{
			name:  "quoted trailing space",
			value: `multipart/byteranges; boundary="invalid "`,
		},
		{
			name:  "non ASCII",
			value: `multipart/byteranges; boundary="边界"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, got := multipartByteRangesBoundary(test.value)
			if got != test.want {
				t.Fatalf("valid = %v, want %v", got, test.want)
			}
		})
	}
}

func TestResolverAcceptsValidMultipartByteRanges(t *testing.T) {
	const (
		boundary = "depsilo-range-boundary"
		body     = "--depsilo-range-boundary\r\n" +
			"Content-Type: application/octet-stream\r\n" +
			"Content-Transfer-Encoding: binary\r\n" +
			"Content-Range: bytes 0-1/8\r\n\r\n" +
			"ab\r\n" +
			"--depsilo-range-boundary\r\n" +
			"Content-Type: application/octet-stream\r\n" +
			"Content-Range: bytes 4-5/8\r\n\r\n" +
			"ef\r\n" +
			"--depsilo-range-boundary--\r\n" +
			"legal epilogue"
	)
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=0-1,4-5" {
			t.Errorf("artifact Range = %q, want bytes=0-1,4-5", got)
		}
		if got := r.Header.Get("Accept-Encoding"); got != "identity" {
			t.Errorf("artifact Accept-Encoding = %q, want identity", got)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Content-Type", "multipart/byteranges; boundary="+boundary)
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept-Encoding"); got != "identity" {
			t.Errorf("origin Accept-Encoding = %q, want identity", got)
		}
		w.Header().Set("Location", artifact.URL+"/blob")
		w.Header().Set("X-Linked-Size", "8")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	selected := newIntegrityTestUpstream(t, origin.URL)
	result, err := resolveIntegrityTest(
		t,
		selected,
		http.Header{
			"Range":           {"bytes=0-1,4-5"},
			"Accept-Encoding": {"gzip"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	got, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body || result.Size != int64(len(body)) {
		t.Fatalf("multipart result = (%q, size=%d)", got, result.Size)
	}
	if !selected.IsHealthy() {
		t.Fatal("valid multipart response marked upstream unhealthy")
	}
}

func TestResolverRejectsInvalidMultipartByteRangePartsAfterRawStreaming(t *testing.T) {
	const boundary = "depsilo-invalid-boundary"
	tests := []struct {
		name string
		body string
	}{
		{
			name: "zero parts",
			body: "--depsilo-invalid-boundary--\r\n",
		},
		{
			name: "missing content range",
			body: "--depsilo-invalid-boundary\r\n\r\nab\r\n" +
				"--depsilo-invalid-boundary--\r\n",
		},
		{
			name: "range outside request",
			body: "--depsilo-invalid-boundary\r\n" +
				"Content-Range: bytes 6-7/8\r\n\r\ngh\r\n" +
				"--depsilo-invalid-boundary--\r\n",
		},
		{
			name: "total conflicts with Hub",
			body: "--depsilo-invalid-boundary\r\n" +
				"Content-Range: bytes 0-1/9\r\n\r\nab\r\n" +
				"--depsilo-invalid-boundary--\r\n",
		},
		{
			name: "part shorter than range",
			body: "--depsilo-invalid-boundary\r\n" +
				"Content-Range: bytes 0-2/8\r\n\r\nab\r\n" +
				"--depsilo-invalid-boundary--\r\n",
		},
		{
			name: "part longer than range",
			body: "--depsilo-invalid-boundary\r\n" +
				"Content-Range: bytes 0-1/8\r\n\r\nabc\r\n" +
				"--depsilo-invalid-boundary--\r\n",
		},
		{
			name: "part content length mismatch",
			body: "--depsilo-invalid-boundary\r\n" +
				"Content-Length: 3\r\n" +
				"Content-Range: bytes 0-1/8\r\n\r\nab\r\n" +
				"--depsilo-invalid-boundary--\r\n",
		},
		{
			name: "unsupported transfer encoding",
			body: "--depsilo-invalid-boundary\r\n" +
				"Content-Transfer-Encoding: quoted-printable\r\n" +
				"Content-Range: bytes 0-1/8\r\n\r\nab\r\n" +
				"--depsilo-invalid-boundary--\r\n",
		},
		{
			name: "missing closing boundary",
			body: "--depsilo-invalid-boundary\r\n" +
				"Content-Range: bytes 0-1/8\r\n\r\nab",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", strconv.Itoa(len(test.body)))
				w.Header().Set("Content-Type", "multipart/byteranges; boundary="+boundary)
				w.WriteHeader(http.StatusPartialContent)
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(artifact.Close)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", artifact.URL+"/blob")
				w.Header().Set("X-Linked-Size", "8")
				w.WriteHeader(http.StatusFound)
			}))
			t.Cleanup(origin.Close)

			selected := newIntegrityTestUpstream(t, origin.URL)
			result, err := resolveIntegrityTest(
				t,
				selected,
				http.Header{"Range": {"bytes=0-2,4-5"}},
			)
			if err != nil {
				t.Fatalf("header resolution failed before body validation: %v", err)
			}
			got, readErr := io.ReadAll(result.Body)
			_ = result.Body.Close()
			if string(got) != test.body {
				t.Fatalf("raw body changed:\n got %q\nwant %q", got, test.body)
			}
			if !errors.Is(readErr, cache.ErrBodySizeMismatch) {
				t.Fatalf("body error = %v, want ErrBodySizeMismatch", readErr)
			}
			if selected.IsHealthy() {
				t.Fatal("invalid multipart response was not critically latched")
			}
		})
	}
}

type terminalErrorReadCloser struct {
	data []byte
	err  error
}

func (r *terminalErrorReadCloser) Read(buffer []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(buffer, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.err
}

func (r *terminalErrorReadCloser) Close() error { return nil }

type closeReturnsEOFReadCloser struct {
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func (r *closeReturnsEOFReadCloser) Read([]byte) (int, error) {
	r.startOnce.Do(func() { close(r.started) })
	<-r.release
	return 0, io.EOF
}

func (r *closeReturnsEOFReadCloser) Close() error {
	r.closeOnce.Do(func() { close(r.release) })
	return nil
}

func TestMultipartValidatorPreservesTransportErrorClassification(t *testing.T) {
	sentinel := errors.New("transport interrupted")
	const body = "--boundary\r\n" +
		"Content-Range: bytes 0-1/8\r\n\r\nab\r\n" +
		"--boundary--\r\n"
	requested, ok := parseRequestedByteRanges([]string{"bytes=0-1,4-5"})
	if !ok {
		t.Fatal("test Range did not parse")
	}
	wrapped := newMultipartValidatingBody(
		&terminalErrorReadCloser{data: []byte(body), err: sentinel},
		"boundary",
		requested,
		8,
		true,
	)
	got, err := io.ReadAll(wrapped)
	_ = wrapped.Close()
	if string(got) != body {
		t.Fatalf("raw body = %q, want %q", got, body)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("body error = %v, want transport sentinel", err)
	}
	if errors.Is(err, cache.ErrBodySizeMismatch) {
		t.Fatalf("transport error was misclassified as body mismatch: %v", err)
	}
}

func TestMultipartValidatorCloseWinsBlockedEOFWithoutMismatch(t *testing.T) {
	requested, ok := parseRequestedByteRanges([]string{"bytes=0-1,4-5"})
	if !ok {
		t.Fatal("test Range did not parse")
	}
	source := &closeReturnsEOFReadCloser{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	wrapped := newMultipartValidatingBody(
		source,
		"boundary",
		requested,
		8,
		true,
	)
	readResult := make(chan error, 1)
	go func() {
		var buffer [1]byte
		_, err := wrapped.Read(buffer[:])
		readResult <- err
	}()
	<-source.started

	closeResult := make(chan error, 1)
	go func() { closeResult <- wrapped.Close() }()
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("Close error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close blocked behind multipart validation")
	}
	select {
	case err := <-readResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Read error = %v, want context.Canceled", err)
		}
		if errors.Is(err, cache.ErrBodySizeMismatch) {
			t.Fatalf("Close-induced EOF was classified as mismatch: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked Read did not exit after Close")
	}
}

func TestResolverAcceptsCoalescedSinglePartForMultipleRanges(t *testing.T) {
	const body = "abcdef"
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=0-1,4-5" {
			t.Errorf("artifact Range = %q, want bytes=0-1,4-5", got)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Content-Range", "bytes 0-5/8")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", artifact.URL+"/blob")
		w.Header().Set("X-Linked-Size", "8")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	selected := newIntegrityTestUpstream(t, origin.URL)
	result, err := resolveIntegrityTest(
		t,
		selected,
		http.Header{"Range": {"bytes=0-1,4-5"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	got, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body || result.Size != int64(len(body)) {
		t.Fatalf("coalesced result = (%q, size=%d)", got, result.Size)
	}
	if !selected.IsHealthy() {
		t.Fatal("valid coalesced response marked upstream unhealthy")
	}
}

func TestRequestedByteRangeMatchesContentRange(t *testing.T) {
	tests := []struct {
		name         string
		request      string
		content      byteContentRange
		linkedSize   int64
		hasLinked    bool
		wantParsed   bool
		wantMatches  bool
		extraHeaders []string
	}{
		{
			name:        "closed",
			request:     "bytes=0-3",
			content:     byteContentRange{first: 0, last: 3, total: 8, hasTotal: true},
			wantParsed:  true,
			wantMatches: true,
		},
		{
			name:        "closed prefix subset",
			request:     "bytes=0-999",
			content:     byteContentRange{first: 0, last: 499, total: 2000, hasTotal: true},
			wantParsed:  true,
			wantMatches: true,
		},
		{
			name:        "closed interior subset",
			request:     "bytes=0-999",
			content:     byteContentRange{first: 100, last: 499, total: 2000, hasTotal: true},
			wantParsed:  true,
			wantMatches: true,
		},
		{
			name:        "closed clipped at end",
			request:     "bytes=6-20",
			content:     byteContentRange{first: 6, last: 7, total: 8, hasTotal: true},
			wantParsed:  true,
			wantMatches: true,
		},
		{
			name:        "open interior subset without total",
			request:     "bytes=4-",
			content:     byteContentRange{first: 6, last: 7},
			wantParsed:  true,
			wantMatches: true,
		},
		{
			name:        "known total makes open range unsatisfiable",
			request:     "bytes=10-",
			content:     byteContentRange{first: 10, last: 10},
			linkedSize:  8,
			hasLinked:   true,
			wantParsed:  true,
			wantMatches: false,
		},
		{
			name:        "open",
			request:     "bytes=4-",
			content:     byteContentRange{first: 4, last: 7, total: 8, hasTotal: true},
			wantParsed:  true,
			wantMatches: true,
		},
		{
			name:        "suffix interior subset",
			request:     "bytes=-4",
			content:     byteContentRange{first: 5, last: 6, total: 8, hasTotal: true},
			wantParsed:  true,
			wantMatches: true,
		},
		{
			name:        "suffix without any total",
			request:     "bytes=-4",
			content:     byteContentRange{first: 4, last: 7},
			wantParsed:  true,
			wantMatches: false,
		},
		{
			name:        "suffix",
			request:     "bytes=-4",
			content:     byteContentRange{first: 4, last: 7, total: 8, hasTotal: true},
			wantParsed:  true,
			wantMatches: true,
		},
		{
			name:        "suffix uses trusted linked size",
			request:     "bytes=-4",
			content:     byteContentRange{first: 4, last: 7},
			linkedSize:  8,
			hasLinked:   true,
			wantParsed:  true,
			wantMatches: true,
		},
		{
			name:         "multiple range remains unsupported",
			request:      "bytes=0-1",
			extraHeaders: []string{"bytes=4-5"},
		},
		{
			name:        "wrong interval",
			request:     "bytes=0-3",
			content:     byteContentRange{first: 4, last: 7, total: 8, hasTotal: true},
			wantParsed:  true,
			wantMatches: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := append([]string{test.request}, test.extraHeaders...)
			requested, ok := parseSingleRequestedByteRange(values)
			if ok != test.wantParsed {
				t.Fatalf("parsed = %v, want %v", ok, test.wantParsed)
			}
			if !ok {
				return
			}
			if got := requested.matches(test.content, test.linkedSize, test.hasLinked); got != test.wantMatches {
				t.Fatalf("matches = %v, want %v", got, test.wantMatches)
			}
		})
	}
}

func TestRequestedRangesBoundCoalescedGaps(t *testing.T) {
	tests := []struct {
		name    string
		request string
		content byteContentRange
		want    bool
	}{
		{
			name:    "single member subset",
			request: "bytes=0-9,1000-1009",
			content: byteContentRange{first: 2, last: 4, total: 2000, hasTotal: true},
			want:    true,
		},
		{
			name:    "gap at policy limit",
			request: "bytes=0-0,257-257",
			content: byteContentRange{first: 0, last: 257, total: 1000, hasTotal: true},
			want:    true,
		},
		{
			name:    "gap over policy limit",
			request: "bytes=0-0,258-258",
			content: byteContentRange{first: 0, last: 258, total: 1000, hasTotal: true},
		},
		{
			name:    "attacker sized gap",
			request: "bytes=0-0,1000000000-1000000000",
			content: byteContentRange{
				first:    0,
				last:     1000000000,
				total:    1000000001,
				hasTotal: true,
			},
		},
		{
			name:    "response ends inside a gap",
			request: "bytes=0-0,10-10",
			content: byteContentRange{first: 0, last: 5, total: 20, hasTotal: true},
		},
		{
			name:    "unordered nearby members",
			request: "bytes=4-5,0-1",
			content: byteContentRange{first: 0, last: 5, total: 8, hasTotal: true},
			want:    true,
		},
		{
			name:    "unknown total closed and open coalescing",
			request: "bytes=0-0,2-",
			content: byteContentRange{first: 0, last: 5},
			want:    true,
		},
		{
			name:    "unsatisfiable member is ignored",
			request: "bytes=1000-1001,0-1",
			content: byteContentRange{first: 0, last: 1, total: 8, hasTotal: true},
			want:    true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requested, ok := parseRequestedByteRanges([]string{test.request})
			if !ok {
				t.Fatalf("Range %q did not parse", test.request)
			}
			got := requestedRangesMatchContentRange(requested, test.content, 0, false)
			if got != test.want {
				t.Fatalf("match = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRangeParserCapsRequestComplexity(t *testing.T) {
	specifications := make([]string, maxRequestedByteRanges+1)
	for index := range specifications {
		specifications[index] = strconv.Itoa(index*2) + "-" + strconv.Itoa(index*2)
	}
	if _, ok := parseRequestedByteRanges([]string{
		"bytes=" + strings.Join(specifications[:maxRequestedByteRanges], ","),
	}); !ok {
		t.Fatalf("%d ranges were rejected", maxRequestedByteRanges)
	}
	if _, ok := parseRequestedByteRanges([]string{
		"bytes=" + strings.Join(specifications, ","),
	}); ok {
		t.Fatalf("%d ranges exceeded the parser budget but were accepted", len(specifications))
	}
}
