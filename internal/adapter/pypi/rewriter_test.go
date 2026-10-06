package pypi

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestExternalArtifactTokenV2CarriesSourceAndUploadTime(t *testing.T) {
	t.Parallel()
	claims := externalArtifactClaims{
		Target:     "https://cdn.example/demo-1.0-py3-none-any.whl",
		Source:     "c291cmNlLWJvdW5k",
		UploadTime: "2026-09-01T12:30:45Z",
	}
	token, err := encodeExternalArtifactToken(testArtifactSigningKey, "pypi", claims)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, externalArtifactTokenVersionV2+".") {
		t.Fatalf("token = %q, want v2 prefix", token)
	}
	decoded, err := decodeExternalArtifactToken(testArtifactSigningKey, "pypi", token)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != claims {
		t.Fatalf("decoded = %#v, want %#v", decoded, claims)
	}
	if _, err := encodeExternalArtifactToken(testArtifactSigningKey, "pypi", externalArtifactClaims{
		Target: claims.Target, UploadTime: "not-a-time",
	}); err == nil {
		t.Fatal("encoded an unparseable upload time")
	}
}

func TestExternalArtifactTokenStillDecodesLegacyV1(t *testing.T) {
	t.Parallel()
	target := "https://cdn.example/legacy-1.0-py3-none-any.whl"
	payload := base64.RawURLEncoding.EncodeToString([]byte(target))
	mac := externalArtifactMAC(testArtifactSigningKey, "pypi", externalArtifactTokenVersion, payload)
	token := externalArtifactTokenVersion + "." + payload + "." + base64.RawURLEncoding.EncodeToString(mac)
	decoded, err := decodeExternalArtifactToken(testArtifactSigningKey, "pypi", token)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Target != target || decoded.Source != "" || decoded.UploadTime != "" {
		t.Fatalf("legacy decoded = %#v", decoded)
	}
}

func TestSignedRewriteResolvesRealPyTorchArtifactShapes(t *testing.T) {
	t.Parallel()
	const pageURL = "https://download.pytorch.org/whl/cu128/torch/"
	tests := []struct {
		name     string
		href     string
		expected string
	}{
		{
			name:     "PyTorch absolute CDN",
			href:     "https://download-r2.pytorch.org/whl/cu128/torch-2.7.1%2Bcu128-cp313-cp313-manylinux_2_28_x86_64.whl#sha256=abcd",
			expected: "https://download-r2.pytorch.org/whl/cu128/torch-2.7.1%2Bcu128-cp313-cp313-manylinux_2_28_x86_64.whl",
		},
		{
			name:     "root relative typing extensions",
			href:     "/whl/typing_extensions-4.15.0-py3-none-any.whl#sha256=0123",
			expected: "https://download.pytorch.org/whl/typing_extensions-4.15.0-py3-none-any.whl",
		},
		{
			name:     "relative artifact",
			href:     "../torch-2.7.0%2Bcu128-py3-none-any.whl",
			expected: "https://download.pytorch.org/whl/cu128/torch-2.7.0%2Bcu128-py3-none-any.whl",
		},
		{
			name:     "Python Hosted",
			href:     "https://files.pythonhosted.org/packages/aa/filelock-3.18.0-py3-none-any.whl",
			expected: "https://files.pythonhosted.org/packages/aa/filelock-3.18.0-py3-none-any.whl",
		},
		{
			name:     "NVIDIA package CDN",
			href:     "https://pypi.nvidia.com/nvidia-cublas-cu12/nvidia_cublas_cu12-12.8.4.1-py3-none-manylinux_2_27_x86_64.whl",
			expected: "https://pypi.nvidia.com/nvidia-cublas-cu12/nvidia_cublas_cu12-12.8.4.1-py3-none-manylinux_2_27_x86_64.whl",
		},
		{
			name:     "index declared unknown public CDN",
			href:     "https://objects.vendor.example/releases/dependency-1.0.0.tar.gz",
			expected: "https://objects.vendor.example/releases/dependency-1.0.0.tar.gz",
		},
		{
			name:     "HTML escaped signed query",
			href:     "https://objects.vendor.example/releases/dependency-1.0.0.whl?part=1&amp;signature=abc",
			expected: "https://objects.vendor.example/releases/dependency-1.0.0.whl?part=1&signature=abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html := `<a href="` + tt.href + `">artifact</a>`
			got, err := rewriteSignedArtifactURLs(
				html, "", "/pypi-torch-cu128", pageURL,
				"extra:pytorch-cu128", "c291cmNl", testArtifactSigningKey,
			)
			if err != nil {
				t.Fatal(err)
			}
			href := rewrittenHref(t, got)
			parsed, err := url.Parse(href)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(href, "://") {
				t.Fatalf("artifact bypasses Depsilo: %s", href)
			}
			prefix := "/pypi-torch-cu128/files/_external/"
			if !strings.HasPrefix(parsed.Path, prefix) {
				t.Fatalf("rewritten path = %q", parsed.Path)
			}
			rest := strings.TrimPrefix(parsed.Path, prefix)
			token, filename, ok := strings.Cut(rest, "/")
			if !ok || token == "" || filename == "" {
				t.Fatalf("rewritten path does not retain filename: %q", parsed.Path)
			}
			claims, err := decodeExternalArtifactToken(testArtifactSigningKey, "extra:pytorch-cu128", token)
			if err != nil {
				t.Fatal(err)
			}
			if claims.Target != tt.expected {
				t.Fatalf("signed target = %q, want %q", claims.Target, tt.expected)
			}
			targetURL, _ := url.Parse(claims.Target)
			wantFilename, _ := artifactFilename(targetURL)
			if filename != wantFilename {
				t.Fatalf("local filename = %q, want %q", filename, wantFilename)
			}
			if !strings.HasSuffix(strings.ToLower(filename), ".whl") &&
				!strings.HasSuffix(strings.ToLower(filename), ".tar.gz") {
				t.Fatalf("local filename does not expose an artifact extension: %q", filename)
			}
			original, _ := url.Parse(tt.href)
			if parsed.Fragment != original.Fragment {
				t.Fatalf("fragment = %q, want %q", parsed.Fragment, original.Fragment)
			}
		})
	}
}

func TestSignedRewriteHandlesHrefQuotingAndLeavesNonArtifactsAlone(t *testing.T) {
	t.Parallel()
	html := `<a HREF='wheel-1.0-py3-none-any.whl'>wheel</a><a href=../>parent</a>`
	got, err := rewriteSignedArtifactURLs(
		html, "", "/extra", "https://index.example/simple/wheel/",
		"extra:wheel", "c291cmNl", testArtifactSigningKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `/extra/files/_external/`) {
		t.Fatalf("single quoted relative artifact was not rewritten: %s", got)
	}
	if !strings.Contains(got, `href=../>`) {
		t.Fatalf("non-artifact link changed: %s", got)
	}
}

func TestSignedRewriteDecodesHTMLAttributeEntities(t *testing.T) {
	t.Parallel()
	got, err := rewriteSignedArtifactURLs(
		`<a href="https://cdn.example/pkg-1.0.whl?download=1&amp;mirror=primary">pkg</a>`,
		"", "/extra", "https://index.example/simple/pkg/",
		"extra:pkg", "c291cmNl", testArtifactSigningKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(rewrittenHref(t, got))
	if err != nil {
		t.Fatal(err)
	}
	rest := strings.TrimPrefix(parsed.Path, "/extra/files/_external/")
	token, _, ok := strings.Cut(rest, "/")
	if !ok {
		t.Fatalf("signed path is malformed: %q", parsed.Path)
	}
	claims, err := decodeExternalArtifactToken(testArtifactSigningKey, "extra:pkg", token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Target != "https://cdn.example/pkg-1.0.whl?download=1&mirror=primary" {
		t.Fatalf("decoded target = %q", claims.Target)
	}
}

func TestSignedTokenRejectsTamperingCrossRouteReplayAndLengthAbuse(t *testing.T) {
	t.Parallel()
	target := "https://cdn.example/torch-2.7.1-py3-none-any.whl?download=1"
	token, err := encodeExternalArtifactToken(testArtifactSigningKey, "extra:pytorch-cu128", externalArtifactClaims{Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := decodeExternalArtifactToken(testArtifactSigningKey, "extra:pytorch-cu128", token); err != nil || decoded.Target != target {
		t.Fatalf("valid token decode = %q, %v", decoded, err)
	}

	last := token[len(token)-1]
	replacement := byte('A')
	if last == replacement {
		replacement = 'B'
	}
	tampered := token[:len(token)-1] + string(replacement)
	if _, err := decodeExternalArtifactToken(testArtifactSigningKey, "extra:pytorch-cu128", tampered); err == nil {
		t.Fatal("tampered token was accepted")
	}
	if _, err := decodeExternalArtifactToken(testArtifactSigningKey, "extra:pytorch-cu129", token); err == nil {
		t.Fatal("token replayed across adapter routes")
	}
	if _, err := decodeExternalArtifactToken(testArtifactSigningKey, "extra:pytorch-cu128", strings.Repeat("a", maxExternalArtifactTokenLen+1)); err == nil {
		t.Fatal("oversized token was accepted")
	}
}

func TestSignedRewriteRejectsUnsafeTarget(t *testing.T) {
	t.Parallel()
	_, err := rewriteSignedArtifactURLs(
		`<a href="https://user:secret@cdn.example/pkg-1.0.whl">pkg</a>`,
		"", "/extra", "https://index.example/simple/pkg/",
		"extra:pkg", "c291cmNl", testArtifactSigningKey,
	)
	if err == nil {
		t.Fatal("credential-bearing target was accepted")
	}
}

func TestSignedRewriteRejectsHTTPSArtifactDowngrade(t *testing.T) {
	t.Parallel()
	_, err := rewriteSignedArtifactURLs(
		`<a href="http://cdn.example/pkg-1.0-py3-none-any.whl">pkg</a>`,
		"", "/extra", "https://index.example/simple/pkg/",
		"extra:pkg", "c291cmNl", testArtifactSigningKey,
	)
	if !errors.Is(err, errArtifactSchemeDowngrade) {
		t.Fatalf("HTTPS-to-HTTP downgrade error = %v", err)
	}

	got, err := rewriteSignedArtifactURLs(
		`<a href="http://cdn.example/pkg-1.0-py3-none-any.whl">pkg</a>`,
		"", "/extra", "http://index.internal/simple/pkg/",
		"extra:pkg", "c291cmNl", testArtifactSigningKey,
	)
	if err != nil || !strings.Contains(got, "/extra/files/_external/") {
		t.Fatalf("HTTP index with HTTP artifact should remain supported: output=%q error=%v", got, err)
	}
}

func TestUnsignedRewriteNeverCreatesExternalToken(t *testing.T) {
	t.Parallel()
	html := `<a href="https://files.pythonhosted.org/packages/aa/pkg-1.0.whl">pkg</a>`
	got := RewriteURLs(html, "", "/pypi")
	if strings.Contains(got, "/_external/") {
		t.Fatalf("unsigned external token created: %s", got)
	}
	if !strings.Contains(got, `/pypi/files/packages/aa/pkg-1.0.whl`) {
		t.Fatalf("legacy /packages rewrite lost: %s", got)
	}
}

func rewrittenHref(t *testing.T, html string) string {
	t.Helper()
	match := hrefRe.FindStringSubmatch(html)
	if len(match) == 0 {
		t.Fatalf("rewritten href missing: %s", html)
	}
	return hrefFromMatch(match)
}
