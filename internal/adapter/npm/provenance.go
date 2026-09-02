package npm

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/packagepolicy"
)

const (
	// SignedTarballRouteSegment identifies Depsilo's authenticated internal npm
	// tarball route. It is deliberately not a valid legacy filename route: the
	// signed route has two additional path segments and is registered first.
	SignedTarballRouteSegment = "__depsilo_tarball_v1"

	preparedTarballReferencePrefix = "depsilo:npm-artifact-reference:v1:"
	tarballSigningKeySize          = 32
	tarballTokenMACSize            = sha256.Size
	tarballTokenNonceSize          = 12
	tarballTokenAEADOverhead       = 16
	maxTarballTokenLength          = 8192
	// Keep the complete escaped route below common 8 KiB request-line limits,
	// leaving room for the HTTP method, spaces, protocol, and proxy framing.
	maxSignedTarballPathLength   = 7168
	maxTarballTargetLength       = 2048
	maxTarballFilenameLength     = 512
	maxTarballDigestFieldLength  = 1024
	maxTarballVersionLength      = 256
	maxProjectAudienceSlugLength = 128
	tarballClaimFormat           = 3
	preparedTarballFormat        = 1
	tarballTokenMACDomain        = "depsilo/npm-tarball-token/mac/v1"
	tarballTokenEncryptionDomain = "depsilo/npm-tarball-token/encryption/v1"
)

type tarballSigner struct {
	macKey [tarballSigningKeySize]byte
	aead   cipher.AEAD
	random io.Reader
}

// preparedTarballReference is the cache-stable result of validating a
// packument tarball declaration. It is deliberately unsigned: the persistent
// cache contains no runtime token, and each metadata response signs a fresh
// route from this exact source/target reference and the versions map key.
type preparedTarballReference struct {
	Format    int    `json:"format"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Filename  string `json:"filename"`
	Integrity string `json:"integrity,omitempty"`
	Shasum    string `json:"shasum,omitempty"`
}

type tarballClaims struct {
	Format    int    `json:"format"`
	Audience  string `json:"audience"`
	Package   string `json:"package"`
	Version   string `json:"version"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Filename  string `json:"filename"`
	Integrity string `json:"integrity,omitempty"`
	Shasum    string `json:"shasum,omitempty"`
}

func newTarballSigner(key []byte) (*tarballSigner, error) {
	return newTarballSignerWithRandom(key, rand.Reader)
}

func newTarballSignerWithRandom(key []byte, random io.Reader) (*tarballSigner, error) {
	if len(key) != tarballSigningKeySize {
		return nil, fmt.Errorf(
			"npm tarball signer: signing key length is %d, want %d",
			len(key),
			tarballSigningKeySize,
		)
	}
	if random == nil {
		return nil, errors.New("npm tarball signer: random source is unavailable")
	}
	macKey := deriveTarballTokenSubkey(key, tarballTokenMACDomain)
	encryptionKey := deriveTarballTokenSubkey(key, tarballTokenEncryptionDomain)
	block, err := aes.NewCipher(encryptionKey[:])
	if err != nil {
		return nil, fmt.Errorf("npm tarball signer: initialize encryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("npm tarball signer: initialize authenticated encryption: %w", err)
	}
	if aead.NonceSize() != tarballTokenNonceSize || aead.Overhead() != tarballTokenAEADOverhead {
		return nil, errors.New("npm tarball signer: unexpected authenticated encryption parameters")
	}
	signer := &tarballSigner{aead: aead, random: random}
	copy(signer.macKey[:], macKey[:])
	return signer, nil
}

func deriveTarballTokenSubkey(root []byte, domain string) [tarballSigningKeySize]byte {
	mac := hmac.New(sha256.New, root)
	_, _ = mac.Write([]byte(domain))
	var key [tarballSigningKeySize]byte
	copy(key[:], mac.Sum(nil))
	return key
}

func (signer *tarballSigner) sign(claims tarballClaims) (string, error) {
	if signer == nil || signer.aead == nil || signer.random == nil {
		return "", errors.New("npm tarball signer is unavailable")
	}
	if err := validateTarballClaims(claims); err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal npm tarball claims: %w", err)
	}
	if base64.RawURLEncoding.EncodedLen(
		len(payload)+tarballTokenNonceSize+tarballTokenAEADOverhead+tarballTokenMACSize,
	) > maxTarballTokenLength {
		return "", errors.New("npm tarball claims exceed the signed route limit")
	}
	nonce := make([]byte, signer.aead.NonceSize())
	if _, err := io.ReadFull(signer.random, nonce); err != nil {
		return "", fmt.Errorf("read npm tarball token random nonce: %w", err)
	}
	ciphertext := signer.aead.Seal(nil, nonce, payload, nil)
	authenticated := make([]byte, 0, len(nonce)+len(ciphertext))
	authenticated = append(authenticated, nonce...)
	authenticated = append(authenticated, ciphertext...)
	mac := hmac.New(sha256.New, signer.macKey[:])
	_, _ = mac.Write(authenticated)
	signed := make([]byte, 0, len(authenticated)+tarballTokenMACSize)
	signed = append(signed, authenticated...)
	signed = append(signed, mac.Sum(nil)...)
	token := base64.RawURLEncoding.EncodeToString(signed)
	return token, nil
}

func (signer *tarballSigner) verify(
	token, audience, packageName, filename string,
) (tarballClaims, bool) {
	if signer == nil || signer.aead == nil || token == "" || len(token) > maxTarballTokenLength ||
		audience == "" || packageName == "" || !validTarballFilename(filename) {
		return tarballClaims{}, false
	}
	signed, err := base64.RawURLEncoding.Strict().DecodeString(token)
	minimumLength := signer.aead.NonceSize() + signer.aead.Overhead() + tarballTokenMACSize
	if err != nil || len(signed) <= minimumLength ||
		base64.RawURLEncoding.EncodeToString(signed) != token {
		return tarballClaims{}, false
	}
	authenticated := signed[:len(signed)-tarballTokenMACSize]
	providedMAC := signed[len(signed)-tarballTokenMACSize:]
	mac := hmac.New(sha256.New, signer.macKey[:])
	_, _ = mac.Write(authenticated)
	if !hmac.Equal(providedMAC, mac.Sum(nil)) {
		return tarballClaims{}, false
	}
	nonce := authenticated[:signer.aead.NonceSize()]
	ciphertext := authenticated[signer.aead.NonceSize():]
	payload, err := signer.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return tarballClaims{}, false
	}

	var claims tarballClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return tarballClaims{}, false
	}
	canonical, err := json.Marshal(claims)
	if err != nil || !bytes.Equal(canonical, payload) || validateTarballClaims(claims) != nil {
		return tarballClaims{}, false
	}
	if claims.Audience != audience || claims.Package != packageName || claims.Filename != filename {
		return tarballClaims{}, false
	}
	return claims, true
}

func validateTarballClaims(claims tarballClaims) error {
	if claims.Format != tarballClaimFormat || !validTarballAudience(claims.Audience) {
		return errors.New("npm tarball claims have an invalid format or audience")
	}
	if err := validateNPMCoordinate(claims.Package, claims.Version); err != nil {
		return err
	}
	reference := preparedTarballReference{
		Format:    preparedTarballFormat,
		Source:    claims.Source,
		Target:    claims.Target,
		Filename:  claims.Filename,
		Integrity: claims.Integrity,
		Shasum:    claims.Shasum,
	}
	return validatePreparedTarballReference(reference)
}

func validateNPMCoordinate(packageName, version string) error {
	if len(version) == 0 || len(version) > maxTarballVersionLength {
		return fmt.Errorf("invalid npm packument version length")
	}
	if err := validateNPMPackageName(packageName); err != nil {
		return err
	}
	dialect, err := packagepolicy.DialectFor("npm")
	if err != nil {
		return err
	}
	if err := dialect.ValidateVersion(version); err != nil {
		return fmt.Errorf("invalid npm packument version %q: %w", version, err)
	}
	return nil
}

func validTarballAudience(audience string) bool {
	if audience == "global" {
		return true
	}
	slug := strings.TrimPrefix(audience, "project:")
	if slug == audience || slug == "" || len(slug) > maxProjectAudienceSlugLength ||
		slug[0] == '-' || slug[len(slug)-1] == '-' {
		return false
	}
	for _, character := range slug {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validateNPMPackageName(packageName string) error {
	dialect, err := packagepolicy.DialectFor("npm")
	if err != nil {
		return err
	}
	normalized, err := dialect.NormalizePackageName(packageName)
	if err != nil || normalized != packageName {
		return fmt.Errorf("invalid npm package identity %q", packageName)
	}
	return nil
}

func validTarballFilename(filename string) bool {
	if filename == "" || filename == "." || filename == ".." ||
		len(filename) > maxTarballFilenameLength || !utf8.ValidString(filename) ||
		strings.ContainsAny(filename, "/\\\x00\r\n") {
		return false
	}
	for _, character := range filename {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

// PreparePackument validates every version identity and converts each exact
// dist.tarball reference into a cache-stable source-bound placeholder. Invalid
// metadata fails before cache persistence, so runtime never guesses a target.
func PreparePackument(
	data []byte,
	expectedPackage, metadataURL, sourceID string,
) ([]byte, error) {
	if !validProvenanceSourceID(sourceID) {
		return nil, errors.New("npm metadata source identity is invalid")
	}
	base, err := parseMetadataURL(metadataURL)
	if err != nil {
		return nil, err
	}

	var document map[string]interface{}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode npm packument: %w", err)
	}
	documentName, ok := document["name"].(string)
	if !ok || documentName != expectedPackage {
		return nil, fmt.Errorf(
			"npm packument name %q does not match requested package %q",
			documentName,
			expectedPackage,
		)
	}
	if err := validateNPMPackageName(documentName); err != nil {
		return nil, err
	}
	versions, ok := document["versions"].(map[string]interface{})
	if !ok {
		return nil, errors.New("npm packument versions map is missing or invalid")
	}

	for version, versionData := range versions {
		if err := validateNPMCoordinate(documentName, version); err != nil {
			return nil, err
		}
		versionMap, ok := versionData.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("npm packument version %q is not an object", version)
		}
		distValue, hasDist := versionMap["dist"]
		if !hasDist {
			continue
		}
		dist, ok := distValue.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("npm packument version %q has an invalid dist object", version)
		}
		tarballValue, hasTarball := dist["tarball"]
		if !hasTarball {
			continue
		}
		tarball, ok := tarballValue.(string)
		if !ok || tarball == "" {
			return nil, fmt.Errorf("npm packument version %q has an invalid tarball URL", version)
		}

		target, filename, err := resolveTarballTarget(base, tarball)
		if err != nil {
			return nil, fmt.Errorf("npm packument version %q: %w", version, err)
		}
		integrity, err := optionalDigestField(dist, "integrity")
		if err != nil {
			return nil, fmt.Errorf("npm packument version %q: %w", version, err)
		}
		shasum, err := optionalDigestField(dist, "shasum")
		if err != nil {
			return nil, fmt.Errorf("npm packument version %q: %w", version, err)
		}
		reference := preparedTarballReference{
			Format:    preparedTarballFormat,
			Source:    sourceID,
			Target:    target,
			Filename:  filename,
			Integrity: integrity,
			Shasum:    shasum,
		}
		// Reject metadata before cache persistence if its authenticated claims
		// could not fit in the internal route. The longest valid project
		// audience is used so the same cached placeholder works on every route.
		if err := validateTarballClaimEncodingSize(tarballClaims{
			Format:    tarballClaimFormat,
			Audience:  "project:" + strings.Repeat("a", maxProjectAudienceSlugLength),
			Package:   documentName,
			Version:   version,
			Source:    reference.Source,
			Target:    reference.Target,
			Filename:  reference.Filename,
			Integrity: reference.Integrity,
			Shasum:    reference.Shasum,
		}); err != nil {
			return nil, fmt.Errorf("npm packument version %q: %w", version, err)
		}
		placeholder, err := encodePreparedTarballReference(reference)
		if err != nil {
			return nil, fmt.Errorf("npm packument version %q: %w", version, err)
		}
		dist["tarball"] = placeholder
	}

	return json.Marshal(document)
}

func validateTarballClaimEncodingSize(claims tarballClaims) error {
	if err := validateTarballClaims(claims); err != nil {
		return err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return err
	}
	tokenLength := base64.RawURLEncoding.EncodedLen(
		len(payload) + tarballTokenNonceSize + tarballTokenAEADOverhead + tarballTokenMACSize,
	)
	if tokenLength > maxTarballTokenLength {
		return errors.New("npm tarball claims exceed the signed route limit")
	}
	maximumProjectPrefix := "/p/" + strings.Repeat("a", maxProjectAudienceSlugLength) + "/npm"
	if err := validateSignedTarballPathSize(maximumProjectPrefix, claims, tokenLength); err != nil {
		return err
	}
	return nil
}

func validateSignedTarballPathSize(routePrefix string, claims tarballClaims, tokenLength int) error {
	if tokenLength <= 0 || tokenLength > maxTarballTokenLength {
		return errors.New("npm tarball claims exceed the signed route limit")
	}
	path := signedTarballPath(
		routePrefix,
		claims.Package,
		strings.Repeat("A", tokenLength),
		claims.Filename,
	)
	if len(path) > maxSignedTarballPathLength {
		return errors.New("npm tarball claims exceed the signed route limit")
	}
	return nil
}

func parseMetadataURL(raw string) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > maxTarballTargetLength {
		return nil, errors.New("npm metadata response URL has an invalid length")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || !parsed.IsAbs() || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("npm metadata response URL is not a fetchable HTTP(S) URL")
	}
	return parsed, nil
}

func resolveTarballTarget(base *url.URL, raw string) (string, string, error) {
	if base == nil || len(raw) == 0 || len(raw) > maxTarballTargetLength {
		return "", "", errors.New("tarball URL has an invalid length")
	}
	reference, err := url.Parse(raw)
	if err != nil {
		return "", "", errors.New("tarball URL is invalid")
	}
	target := base.ResolveReference(reference)
	if target.Opaque != "" || !target.IsAbs() || target.Host == "" ||
		(target.Scheme != "http" && target.Scheme != "https") || target.User != nil ||
		target.Fragment != "" || len(target.String()) > maxTarballTargetLength {
		return "", "", errors.New("tarball URL must be an absolute HTTP(S) target without credentials or fragment")
	}
	if base.Scheme == "https" && target.Scheme == "http" {
		return "", "", errors.New("tarball URL downgrades an HTTPS packument to HTTP")
	}
	filename, err := tarballTargetFilename(target)
	if err != nil {
		return "", "", err
	}
	return target.String(), filename, nil
}

func tarballTargetFilename(target *url.URL) (string, error) {
	if target == nil {
		return "", errors.New("tarball filename is invalid")
	}
	escapedPath := target.EscapedPath()
	separator := strings.LastIndexByte(escapedPath, '/')
	if separator < 0 || separator == len(escapedPath)-1 {
		return "", errors.New("tarball filename is invalid")
	}
	escapedFilename := escapedPath[separator+1:]
	filename, err := url.PathUnescape(escapedFilename)
	if err != nil || !validTarballFilename(filename) {
		return "", errors.New("tarball filename is invalid")
	}
	return filename, nil
}

func optionalDigestField(dist map[string]interface{}, field string) (string, error) {
	value, exists := dist[field]
	if !exists {
		return "", nil
	}
	text, ok := value.(string)
	if !ok || !validOptionalDigestField(text, false) {
		return "", fmt.Errorf("dist.%s is invalid", field)
	}
	return text, nil
}

func validOptionalDigestField(value string, emptyAllowed bool) bool {
	if value == "" {
		return emptyAllowed
	}
	if len(value) > maxTarballDigestFieldLength || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func encodePreparedTarballReference(reference preparedTarballReference) (string, error) {
	if err := validatePreparedTarballReference(reference); err != nil {
		return "", err
	}
	payload, err := json.Marshal(reference)
	if err != nil {
		return "", err
	}
	return preparedTarballReferencePrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodePreparedTarballReference(value string) (preparedTarballReference, error) {
	if !strings.HasPrefix(value, preparedTarballReferencePrefix) {
		return preparedTarballReference{}, errors.New("npm tarball reference is not provenance prepared")
	}
	encoded := strings.TrimPrefix(value, preparedTarballReferencePrefix)
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(payload) == 0 || len(payload) > maxTarballTokenLength ||
		base64.RawURLEncoding.EncodeToString(payload) != encoded {
		return preparedTarballReference{}, errors.New("npm tarball reference is invalid")
	}
	var reference preparedTarballReference
	if err := json.Unmarshal(payload, &reference); err != nil {
		return preparedTarballReference{}, errors.New("npm tarball reference is invalid")
	}
	canonical, err := json.Marshal(reference)
	if err != nil || !bytes.Equal(canonical, payload) || validatePreparedTarballReference(reference) != nil {
		return preparedTarballReference{}, errors.New("npm tarball reference is invalid")
	}
	return reference, nil
}

func validatePreparedTarballReference(reference preparedTarballReference) error {
	if reference.Format != preparedTarballFormat || !validProvenanceSourceID(reference.Source) ||
		!validTarballFilename(reference.Filename) ||
		!validOptionalDigestField(reference.Integrity, true) ||
		!validOptionalDigestField(reference.Shasum, true) {
		return errors.New("npm tarball reference is invalid")
	}
	target, err := url.Parse(reference.Target)
	if err != nil || target.String() != reference.Target || target.Opaque != "" ||
		!target.IsAbs() || target.Host == "" ||
		(target.Scheme != "http" && target.Scheme != "https") || target.User != nil ||
		target.Fragment != "" || len(reference.Target) > maxTarballTargetLength {
		return errors.New("npm tarball reference target is invalid")
	}
	filename, err := tarballTargetFilename(target)
	if err != nil || filename != reference.Filename {
		return errors.New("npm tarball reference filename does not match its target")
	}
	return nil
}

func validProvenanceSourceID(sourceID string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(sourceID)
	return err == nil && len(decoded) == sha256.Size &&
		base64.RawURLEncoding.EncodeToString(decoded) == sourceID
}

// signRuntimeTarballURLs turns cache-stable prepared references into
// authenticated routes. The versions map key is the version authority; no
// version or upstream target is inferred from the archive filename.
func signRuntimeTarballURLs(
	data []byte,
	baseURL, routePrefix, audience, expectedPackage string,
	signer *tarballSigner,
) ([]byte, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	var document map[string]interface{}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode cached npm packument: %w", err)
	}
	documentName, ok := document["name"].(string)
	if !ok || documentName != expectedPackage {
		return nil, fmt.Errorf(
			"npm packument name %q does not match requested package %q",
			documentName,
			expectedPackage,
		)
	}
	if err := validateNPMPackageName(documentName); err != nil {
		return nil, err
	}
	versions, ok := document["versions"].(map[string]interface{})
	if !ok {
		return nil, errors.New("npm packument versions map is missing or invalid")
	}

	for version, versionData := range versions {
		if err := validateNPMCoordinate(documentName, version); err != nil {
			return nil, err
		}
		versionMap, ok := versionData.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("npm packument version %q is not an object", version)
		}
		distValue, hasDist := versionMap["dist"]
		if !hasDist {
			continue
		}
		dist, ok := distValue.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("npm packument version %q has an invalid dist object", version)
		}
		tarballValue, hasTarball := dist["tarball"]
		if !hasTarball {
			continue
		}
		placeholder, ok := tarballValue.(string)
		if !ok {
			return nil, fmt.Errorf("npm packument version %q has an invalid tarball reference", version)
		}
		reference, err := decodePreparedTarballReference(placeholder)
		if err != nil {
			return nil, fmt.Errorf("npm packument version %q: %w", version, err)
		}
		integrity, err := optionalDigestField(dist, "integrity")
		if err != nil || integrity != reference.Integrity {
			return nil, fmt.Errorf("npm packument version %q has inconsistent dist.integrity", version)
		}
		shasum, err := optionalDigestField(dist, "shasum")
		if err != nil || shasum != reference.Shasum {
			return nil, fmt.Errorf("npm packument version %q has inconsistent dist.shasum", version)
		}
		claims := tarballClaims{
			Format:    tarballClaimFormat,
			Audience:  audience,
			Package:   expectedPackage,
			Version:   version,
			Source:    reference.Source,
			Target:    reference.Target,
			Filename:  reference.Filename,
			Integrity: reference.Integrity,
			Shasum:    reference.Shasum,
		}
		payload, err := json.Marshal(claims)
		if err != nil {
			return nil, err
		}
		tokenLength := base64.RawURLEncoding.EncodedLen(
			len(payload) + tarballTokenNonceSize + tarballTokenAEADOverhead + tarballTokenMACSize,
		)
		if err := validateSignedTarballPathSize(routePrefix, claims, tokenLength); err != nil {
			return nil, err
		}
		token, err := signer.sign(claims)
		if err != nil {
			return nil, err
		}
		dist["tarball"] = baseURL + signedTarballPath(routePrefix, expectedPackage, token, reference.Filename)
	}

	return json.Marshal(document)
}

func signedTarballPath(routePrefix, packageName, token, filename string) string {
	return strings.TrimRight(routePrefix, "/") + "/" + escapedPackagePath(packageName) + "/-/" +
		SignedTarballRouteSegment + "/" + token + "/" + url.PathEscape(filename)
}

func escapedPackagePath(packageName string) string {
	segments := strings.Split(packageName, "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	return strings.Join(segments, "/")
}

func authenticatedTarballCacheKey(claims tarballClaims) string {
	identity, _ := json.Marshal(struct {
		Source    string `json:"source"`
		Target    string `json:"target"`
		Integrity string `json:"integrity,omitempty"`
		Shasum    string `json:"shasum,omitempty"`
	}{
		Source: claims.Source, Target: claims.Target,
		Integrity: claims.Integrity, Shasum: claims.Shasum,
	})
	digest := sha256.Sum256(identity)
	return packagekey.NPMExactIdentityCachePrefix + claims.Package + "/-/" +
		SignedTarballRouteSegment + "/objects/" + hex.EncodeToString(digest[:]) + "/" + claims.Filename
}
