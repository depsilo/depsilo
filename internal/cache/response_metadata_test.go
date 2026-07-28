package cache

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestResponseMetadataDropsOversizedWholeValueAndKeepsHuggingFaceHeaders(t *testing.T) {
	const (
		link   = `</huggingface/api/models/acme/model/tree/main?cursor=next>; rel="next"`
		etag   = `"model-v1"`
		commit = "0123456789abcdef0123456789abcdef01234567"
	)
	body := WithResponseMetadata(io.NopCloser(strings.NewReader("body")), http.Header{
		"Link":          {link},
		"ETag":          {etag},
		"X-Repo-Commit": {commit},
		"X-Linked-Etag": {strings.Repeat("x", 9<<10)},
	})
	defer body.Close()

	headers := responseMetadataFrom(body)
	if got := headers.Get("Link"); got != link {
		t.Fatalf("legal Hugging Face Link = %q, want %q", got, link)
	}
	if got := headers.Get("ETag"); got != etag {
		t.Fatalf("ETag = %q, want %q", got, etag)
	}
	if got := headers.Get("X-Repo-Commit"); got != commit {
		t.Fatalf("X-Repo-Commit = %q, want %q", got, commit)
	}
	if values := headers.Values("X-Linked-Etag"); len(values) != 0 {
		t.Fatalf("oversized header value was retained or truncated: lengths=%v", valueLengths(values))
	}
}

func TestResponseMetadataDropsCacheControlInsteadOfRenewingFreshness(t *testing.T) {
	const cacheControl = "public, max-age=259200"
	headers := sanitizeResponseMetadata(http.Header{
		"Cache-Control": {cacheControl},
		"Vary":          {"User-Agent"},
	})

	if got := headers.Get("Cache-Control"); got != "" {
		t.Fatalf("Cache-Control crossed generic cache metadata boundary: %q", got)
	}
	if got := headers.Get("Vary"); got != "" {
		t.Fatalf("Vary crossed generic cache metadata boundary: %q", got)
	}
}

func TestResponseMetadataLimitsValuesPerHeader(t *testing.T) {
	values := make([]string, 24)
	for index := range values {
		values[index] = strings.Repeat("a", index+1)
	}

	headers := sanitizeResponseMetadata(http.Header{
		"X-Checksum-Sha256": values,
	})

	got := headers.Values("X-Checksum-Sha256")
	if len(got) != 16 {
		t.Fatalf("value count = %d, want 16", len(got))
	}
	for index, value := range got {
		if value != values[index] {
			t.Fatalf("value %d = %q, want whole value %q", index, value, values[index])
		}
	}
}

func TestResponseMetadataLimitsEncodedSizeAndKeepsHuggingFaceHeaders(t *testing.T) {
	const (
		link     = `</huggingface/api/models/acme/model/tree/main?cursor=next>; rel="next"`
		etag     = `"model-v1"`
		commit   = "0123456789abcdef0123456789abcdef01234567"
		checksum = "3a6eb0790f39ac87c94f3856b2dd2c5d110e6811602261a9a923d3bb23adc8b7"
	)
	filler := strings.Repeat("f", 8<<10)
	headers := http.Header{
		"Link":                {link},
		"ETag":                {etag},
		"X-Repo-Commit":       {commit},
		"X-Checksum-Sha256":   {checksum},
		"Content-Disposition": {filler, filler, filler, filler, filler},
		"Content-Language":    {filler, filler, filler, filler, filler},
	}

	encoded := encodeResponseMetadata(headers)
	if len(encoded) > 32<<10 {
		t.Fatalf("encoded metadata size = %d, want at most %d", len(encoded), 32<<10)
	}
	decoded := decodeResponseMetadata(encoded)
	if got := decoded.Get("Link"); got != link {
		t.Fatalf("legal Hugging Face Link = %q, want %q", got, link)
	}
	if got := decoded.Get("ETag"); got != etag {
		t.Fatalf("ETag = %q, want %q", got, etag)
	}
	if got := decoded.Get("X-Repo-Commit"); got != commit {
		t.Fatalf("X-Repo-Commit = %q, want %q", got, commit)
	}
	if got := decoded.Get("X-Checksum-Sha256"); got != checksum {
		t.Fatalf("X-Checksum-Sha256 = %q, want %q", got, checksum)
	}
	for _, key := range []string{"Content-Disposition", "Content-Language"} {
		for index, value := range decoded.Values(key) {
			if value != filler {
				t.Fatalf("%s value %d was truncated: length=%d", key, index, len(value))
			}
		}
	}
}

func TestWithResponseMetadataLimitsMergedMetadata(t *testing.T) {
	const link = `</huggingface/api/models/acme/model/tree/main?cursor=next>; rel="next"`
	filler := strings.Repeat(`\`, 8<<10)
	body := WithResponseMetadata(io.NopCloser(strings.NewReader("body")), http.Header{
		"Content-Disposition": {filler, filler, filler},
	})
	body = WithResponseMetadata(body, http.Header{
		"Link":             {link},
		"Content-Language": {filler, filler, filler},
	})
	defer body.Close()

	headers := responseMetadataFrom(body)
	encoded, err := json.Marshal(headers)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 32<<10 {
		t.Fatalf("merged encoded metadata size = %d, want at most %d", len(encoded), 32<<10)
	}
	if got := headers.Get("Link"); got != link {
		t.Fatalf("legal Hugging Face Link = %q, want %q", got, link)
	}
	for _, key := range []string{"Content-Disposition", "Content-Language"} {
		for index, value := range headers.Values(key) {
			if value != filler {
				t.Fatalf("%s value %d was truncated: length=%d", key, index, len(value))
			}
		}
	}
}

func TestResponseMetadataBudgetKeepsRepresentationCriticalHeaders(t *testing.T) {
	filler := strings.Repeat("f", 8<<10)
	headers := sanitizeResponseMetadata(http.Header{
		"Content-Encoding":    {"gzip"},
		"Content-Language":    {"en"},
		"Content-Range":       {"bytes 0-1023/4096"},
		"Content-Disposition": {filler, filler, filler, filler, filler},
		"X-Checksum-Sha512":   {filler, filler, filler, filler, filler},
	})

	for key, want := range map[string]string{
		"Content-Encoding": "gzip",
		"Content-Language": "en",
		"Content-Range":    "bytes 0-1023/4096",
	} {
		if got := headers.Get(key); got != want {
			t.Fatalf("%s = %q, want %q under aggregate budget pressure", key, got, want)
		}
	}
	encoded, err := json.Marshal(headers)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 32<<10 {
		t.Fatalf("encoded metadata size = %d, want at most %d", len(encoded), 32<<10)
	}
}

func TestDecodeResponseMetadataRejectsOversizedLegacyPayload(t *testing.T) {
	encoded, err := json.Marshal(http.Header{
		"ETag":             {`"legacy-model"`},
		"Content-Language": {strings.Repeat("x", 33<<10)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if headers := decodeResponseMetadata(string(encoded)); len(headers) != 0 {
		t.Fatalf("oversized legacy payload decoded as %#v, want empty metadata", headers)
	}
}

func TestDecodeStoredResponseMetadataSanitizesLegacyValidatorColumns(t *testing.T) {
	const lastModified = "Wed, 21 Oct 2015 07:28:00 GMT"
	headers := decodeStoredResponseMetadata(
		"",
		strings.Repeat("e", 9<<10),
		lastModified,
	)

	if values := headers.Values("ETag"); len(values) != 0 {
		t.Fatalf("oversized legacy ETag was retained or truncated: lengths=%v", valueLengths(values))
	}
	if got := headers.Get("Last-Modified"); got != lastModified {
		t.Fatalf("legal legacy Last-Modified = %q, want %q", got, lastModified)
	}
}

func valueLengths(values []string) []int {
	lengths := make([]int, len(values))
	for index, value := range values {
		lengths[index] = len(value)
	}
	return lengths
}
