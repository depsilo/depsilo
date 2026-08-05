package pypi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	stdhtml "html"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	externalArtifactTokenVersion = "v1"
	maxArtifactURLLength         = 4096
	maxArtifactFilenameLength    = 512
	maxExternalArtifactTokenLen  = 5600
)

var hrefRe = regexp.MustCompile(`(?i)href\s*=\s*(?:"([^"]+)"|'([^']+)'|([^\s>]+))`)

var errArtifactSchemeDowngrade = errors.New("PyPI artifact URL downgrades an HTTPS index to HTTP")

// RewriteURLs preserves the legacy PyPI behavior: only paths containing
// /packages/ are mapped through the local file route. It deliberately does not
// create external artifact references because it has no signing key.
func RewriteURLs(html string, baseURL string, pathPrefix string) string {
	return rewriteLegacyURLs(html, baseURL, pathPrefix)
}

func rewriteLegacyURLs(html string, baseURL string, pathPrefix string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	return hrefRe.ReplaceAllStringFunc(html, func(match string) string {
		sub := hrefRe.FindStringSubmatch(match)
		href := hrefFromMatch(sub)
		if href == "" {
			return match
		}

		idx := strings.Index(href, "/packages/")
		if idx < 0 {
			idx = strings.Index(href, "packages/")
			if idx < 0 {
				return match
			}
			filePath := "/" + href[idx:]
			return `href="` + baseURL + pathPrefix + `/files` + filePath + `"`
		}

		filePath := href[idx:]
		return `href="` + baseURL + pathPrefix + `/files` + filePath + `"`
	})
}

// rewriteSignedArtifactURLs turns only recognizable Python artifacts declared
// by an upstream project page into authenticated local references. pageURL must
// be the final response URL so relative links retain upstream semantics after
// redirects.
func rewriteSignedArtifactURLs(
	html string,
	baseURL string,
	pathPrefix string,
	pageURL string,
	adapterID string,
	signingKey []byte,
) (string, error) {
	if len(signingKey) == 0 {
		return rewriteLegacyURLs(html, baseURL, pathPrefix), nil
	}
	page, err := parseFetchableArtifactURL(pageURL)
	if err != nil {
		return "", fmt.Errorf("invalid PyPI index response URL: %w", err)
	}
	baseURL = strings.TrimRight(baseURL, "/")
	var rewriteErr error

	rewritten := hrefRe.ReplaceAllStringFunc(html, func(match string) string {
		if rewriteErr != nil {
			return match
		}
		sub := hrefRe.FindStringSubmatch(match)
		href := hrefFromMatch(sub)
		if href == "" {
			return match
		}

		reference, parseErr := url.Parse(stdhtml.UnescapeString(href))
		if parseErr != nil {
			return match
		}
		target := page.ResolveReference(reference)
		if !obviousPythonArtifactURL(target) {
			return match
		}
		if page.Scheme == "https" && target.Scheme == "http" {
			rewriteErr = errArtifactSchemeDowngrade
			return match
		}

		fragment := target.EscapedFragment()
		target.Fragment = ""
		target.RawFragment = ""
		if err := validateArtifactTarget(target); err != nil {
			rewriteErr = err
			return match
		}
		filename, filenameErr := artifactFilename(target)
		if filenameErr != nil {
			rewriteErr = filenameErr
			return match
		}
		token, tokenErr := encodeExternalArtifactToken(signingKey, adapterID, target.String())
		if tokenErr != nil {
			rewriteErr = tokenErr
			return match
		}
		local := baseURL + pathPrefix + "/files/_external/" + token + "/" + url.PathEscape(filename)
		if fragment != "" {
			local += "#" + fragment
		}
		return `href="` + local + `"`
	})
	return rewritten, rewriteErr
}

func hrefFromMatch(groups []string) string {
	for index := 1; index < len(groups); index++ {
		if groups[index] != "" {
			return groups[index]
		}
	}
	return ""
}

func obviousPythonArtifactURL(target *url.URL) bool {
	if target == nil {
		return false
	}
	lower := strings.ToLower(target.Path)
	for _, suffix := range []string{
		".whl", ".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst", ".tgz", ".zip", ".egg",
		".whl.metadata", ".tar.gz.metadata", ".zip.metadata",
	} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func parseFetchableArtifactURL(raw string) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > maxArtifactURLLength {
		return nil, errors.New("artifact URL has invalid length")
	}
	target, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("artifact URL is invalid")
	}
	if err := validateArtifactTarget(target); err != nil {
		return nil, err
	}
	return target, nil
}

func validateArtifactTarget(target *url.URL) error {
	if target == nil || !target.IsAbs() ||
		(target.Scheme != "http" && target.Scheme != "https") || target.Host == "" ||
		target.User != nil || target.Fragment != "" || len(target.String()) > maxArtifactURLLength {
		return errors.New("artifact URL must be an absolute HTTP(S) URL without credentials or fragment")
	}
	return nil
}

func artifactFilename(target *url.URL) (string, error) {
	if target == nil {
		return "", errors.New("artifact filename is invalid")
	}
	escapedPath := target.EscapedPath()
	separator := strings.LastIndex(escapedPath, "/")
	escapedName := escapedPath[separator+1:]
	filename, err := url.PathUnescape(escapedName)
	if err != nil || filename == "" || filename == "." || filename == ".." ||
		len(filename) > maxArtifactFilenameLength || !utf8.ValidString(filename) ||
		strings.ContainsAny(filename, "/\\\x00\r\n") {
		return "", errors.New("artifact filename is invalid")
	}
	for _, character := range filename {
		if unicode.IsControl(character) {
			return "", errors.New("artifact filename is invalid")
		}
	}
	return filename, nil
}

func encodeExternalArtifactToken(signingKey []byte, adapterID, targetURL string) (string, error) {
	if len(signingKey) == 0 || adapterID == "" || len(targetURL) == 0 || len(targetURL) > maxArtifactURLLength {
		return "", errors.New("invalid external artifact token input")
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(targetURL))
	mac := externalArtifactMAC(signingKey, adapterID, payload)
	token := externalArtifactTokenVersion + "." + payload + "." + base64.RawURLEncoding.EncodeToString(mac)
	if len(token) > maxExternalArtifactTokenLen {
		return "", errors.New("external artifact token is too long")
	}
	return token, nil
}

func decodeExternalArtifactToken(signingKey []byte, adapterID, token string) (string, error) {
	if len(signingKey) == 0 || adapterID == "" || len(token) == 0 || len(token) > maxExternalArtifactTokenLen {
		return "", errors.New("invalid external artifact token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != externalArtifactTokenVersion || parts[1] == "" || parts[2] == "" {
		return "", errors.New("invalid external artifact token")
	}
	providedMAC, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(providedMAC) != sha256.Size || !hmac.Equal(providedMAC, externalArtifactMAC(signingKey, adapterID, parts[1])) {
		return "", errors.New("invalid external artifact token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(payload) == 0 || len(payload) > maxArtifactURLLength {
		return "", errors.New("invalid external artifact token")
	}
	return string(payload), nil
}

func externalArtifactMAC(signingKey []byte, adapterID, encodedPayload string) []byte {
	mac := hmac.New(sha256.New, signingKey)
	_, _ = mac.Write([]byte(externalArtifactTokenVersion))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(adapterID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(encodedPayload))
	return mac.Sum(nil)
}
