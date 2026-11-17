package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.uber.org/zap"

	"depsilo/internal/upstream"
)

// registryStage names what a failed registry request was doing. A 502 body
// names it, so the set stays small and fixed.
type registryStage string

const (
	stageAuthDiscovery registryStage = "auth discovery"
	stageToken         registryStage = "token fetch"
	stageTagResolution registryStage = "tag resolution"
	stageManifest      registryStage = "manifest"
	stageManifestHead  registryStage = "manifest HEAD"
	stageBlob          registryStage = "blob"
	stageTagList       registryStage = "tag list"
)

const (
	// genericRegistryFailure is the 502 message of a failure that typed
	// facts cannot explain.
	genericRegistryFailure = "Docker registry request failed"
	// proxyConnectFailure is the reason given for a failed connection to the
	// proxy that carries a registry. The transport's own text names the
	// proxy's host and port.
	proxyConnectFailure = "connection to the egress proxy failed"
	// maxClientMessageBytes caps a 502 message before its request suffix,
	// which is never cut, so the request ID always survives. Request IDs are
	// server-generated UUIDs, so the suffix adds 82 bytes and a whole message
	// stays within 594 bytes. The cap is a safety net, not a limit realistic
	// messages reach: a fake-IP refusal of Docker Hub's CDN host
	// (production.cloudfront.docker.com) with the no_proxy remedy is 516
	// bytes with its suffix, so a 512-byte cap on the whole message would cut
	// that remedy.
	maxClientMessageBytes = 512
	// maxSpecErrorCodes caps the error codes a 502 message repeats.
	maxSpecErrorCodes = 3
	// opaqueOrigin stands in for an origin that cannot be shown safely.
	opaqueOrigin = "***"
)

// registryFailure is a failed request to a registry, its Bearer realm, or a
// host either one redirects to. It is built where Depsilo calls the registry,
// so a 502 body can be written from typed facts alone: the stage, the origin
// of the hop that failed, and a reason from a fixed vocabulary. Error keeps
// the full diagnosis for the server log, minus URL credentials.
type registryFailure struct {
	stage registryStage
	// hop is the request that failed, after redirects.
	hop *url.URL
	// status is the hop's response status when cause is nil.
	status int
	// codes are the distribution-specification error codes of the response.
	codes []string
	// body is the bounded response body, for the server log only.
	body string
	// cause is why the request got no usable response.
	cause error
}

// transportFailure explains a request that got no usable response. The hop is
// the URL of Go's *url.Error: the request that failed after any redirects, or
// the redirect target that CheckRedirect refused, which is resolved against
// the response that named it.
func transportFailure(stage registryStage, request *http.Request, response *http.Response, err error) *registryFailure {
	hop := responseURL(response, request)
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.URL != "" {
		if target, parseErr := hop.Parse(urlErr.URL); parseErr == nil {
			hop = target
		}
	}
	return &registryFailure{stage: stage, hop: hop, cause: err}
}

// statusFailure explains a response with an unexpected status. The bounded
// body stays in the server log; a 502 body repeats only the specification
// error codes it contains.
func statusFailure(stage registryStage, request *http.Request, response *http.Response) *registryFailure {
	body, truncated := readErrorBody(response.Body)
	failure := &registryFailure{
		stage:  stage,
		hop:    responseURL(response, request),
		status: response.StatusCode,
		body:   string(body),
	}
	if truncated {
		failure.body += "…"
	} else {
		failure.codes = specErrorCodes(body)
	}
	return failure
}

// responseFailure explains a response that Depsilo could not use for a
// reason of its own, such as a missing challenge or an unreadable token.
func responseFailure(stage registryStage, request *http.Request, response *http.Response, cause error) *registryFailure {
	return &registryFailure{stage: stage, hop: responseURL(response, request), cause: cause}
}

// responseURL is the URL that answered: the last request of the redirect
// chain, or the request itself.
func responseURL(response *http.Response, request *http.Request) *url.URL {
	if response != nil && response.Request != nil && response.Request.URL != nil {
		return response.Request.URL
	}
	return request.URL
}

func (f *registryFailure) Error() string {
	var text strings.Builder
	text.WriteString(string(f.stage))
	text.WriteString(" failed: ")
	var urlErr *url.Error
	switch {
	case f.cause == nil:
		if f.hop != nil {
			text.WriteString(redactLogURL(f.hop.String()))
			text.WriteString(" ")
		}
		text.WriteString("returned ")
		text.WriteString(strconv.Itoa(f.status))
		if f.body != "" {
			text.WriteString(": ")
			text.WriteString(redactLogURLs(f.body))
		}
	case errors.As(f.cause, &urlErr) || f.hop == nil:
		text.WriteString(logText(f.cause))
	default:
		text.WriteString(redactLogURL(f.hop.String()))
		text.WriteString(": ")
		text.WriteString(logText(f.cause))
	}
	return text.String()
}

func (f *registryFailure) Unwrap() error { return f.cause }

// clientText is the failure as a 502 body states it:
// "<stage> failed at <origin>: <reason>".
func (f *registryFailure) clientText() string {
	return string(f.stage) + " failed at " + originOf(f.hop) + ": " + f.reason()
}

// reason states why the hop failed from the response status and its
// specification error codes, or from the type of the cause: a refusal that
// Depsilo wrote, a failed connection to the proxy, or otherwise the failure
// code that upstream.ClassifyFailure gives a package Upstream, which is
// network when nothing more specific applies. Every cause that is not
// Depsilo's own refusal is a transport error, because registryFailures are
// built only where Depsilo calls a registry. Text from the registry, the
// proxy, or Go's transport is never quoted: it can carry a response's bytes,
// such as a malformed status line, HTTP/2 GOAWAY data, or certificate names.
func (f *registryFailure) reason() string {
	if f.cause == nil {
		reason := "returned " + statusDigits(f.status)
		if len(f.codes) > 0 {
			reason += " (" + strings.Join(f.codes, ", ") + ")"
		}
		return reason
	}
	var explained clientReasoner
	if errors.As(f.cause, &explained) {
		return explained.clientReason()
	}
	var opErr *net.OpError
	if errors.As(f.cause, &opErr) && opErr.Op == "proxyconnect" {
		return proxyConnectFailure
	}
	return upstream.ClassifyFailure(f.cause).Code
}

// clientReasoner is an error that Depsilo writes from fixed phrases and typed
// facts only, so a 502 body may give it as the reason of a failure.
type clientReasoner interface {
	error
	clientReason() string
}

// refusal is an error that Depsilo raises with fixed text, such as a guard
// decision. The text carries no upstream bytes, so a 502 body may state it.
// cause adds detail for the server log; a 502 body repeats it only when it is
// a clientReasoner too.
type refusal struct {
	text  string
	cause error
}

func (r *refusal) Error() string {
	if r.cause == nil {
		return r.text
	}
	return r.text + ": " + r.cause.Error()
}

func (r *refusal) Unwrap() error { return r.cause }

func (r *refusal) clientReason() string {
	var inner clientReasoner
	if errors.As(r.cause, &inner) {
		return r.text + ": " + inner.clientReason()
	}
	return r.text
}

// fakeIPRefusal refuses a direct dial to a host name answered only with
// fake-IP addresses, the one answer a proxy would carry, and names the remedy.
// It holds the dialed host, one placeholder of its answer (an IPv4 one when
// the answer has one; see fakeIPDialError), and a fixed remedy.
type fakeIPRefusal struct {
	host    string
	address netip.Addr
	remedy  string
}

func (e *fakeIPRefusal) Error() string { return e.explain(e.host) }

// clientReason names the host only when it is a plain host name: a
// redirect can name any host that Go's URL parser accepts, and proxy
// software in fake-IP mode answers every name.
func (e *fakeIPRefusal) clientReason() string { return e.explain(displayHost(e.host)) }

func (e *fakeIPRefusal) explain(host string) string {
	return host + " resolved to " + e.address.String() +
		", a fake-IP address from proxy software in TUN/fake-IP mode; " + e.remedy
}

// uniqueLocalFakeIPRemedy is the advice of a uniqueLocalFakeIPRefusal. Only a
// placeholder range lets a proxy carry the answer, and without IPv6 fake-IP
// the answer keeps its IPv4 placeholders.
const uniqueLocalFakeIPRemedy = "set the proxy software's IPv6 fake-IP range (fake-ip-range6 or inet6_range) to 2001:2::/48 " +
	"or turn IPv6 fake-IP off, see docs/deployment.md#upstream-network-egress"

// uniqueLocalFakeIPRefusal refuses a host name whose DNS answer is IPv6
// fake-IP from a unique-local range (see uniqueLocalFakeIPError), with or
// without a proxy, and names the remedy. A 502 body names a documented range
// of proxy software that the answer lies in, or none: a real unique-local
// prefix would show anonymous clients how an internal network is numbered.
// The server log names the whole answer.
type uniqueLocalFakeIPRefusal struct {
	host   string
	answer []netip.Addr
	// proxyRange is the documented range of the first unique-local address
	// that lies in one; it is invalid when none does.
	proxyRange netip.Prefix
}

func (e *uniqueLocalFakeIPRefusal) Error() string {
	answer := make([]string, len(e.answer))
	for index, address := range e.answer {
		answer[index] = address.String()
	}
	return e.explain(e.host) + " (DNS answer: " + strings.Join(answer, ", ") + ")"
}

// clientReason names the host only when it is a plain host name, as
// fakeIPRefusal does, and never an address.
func (e *uniqueLocalFakeIPRefusal) clientReason() string { return e.explain(displayHost(e.host)) }

func (e *uniqueLocalFakeIPRefusal) explain(host string) string {
	if e.proxyRange.IsValid() {
		return host + " resolved into " + e.proxyRange.String() +
			", an IPv6 fake-IP range that Depsilo cannot tell from a private network; " + uniqueLocalFakeIPRemedy
	}
	return host + " resolved to fake-IP addresses and a unique-local address, " +
		"which Depsilo cannot tell from a private network; " + uniqueLocalFakeIPRemedy
}

// proxyConnectRefusal reports a proxy that answered CONNECT with a status
// other than 200. It holds only the status code: the status text is the
// proxy's own and stays out of messages.
type proxyConnectRefusal struct{ status int }

func (e *proxyConnectRefusal) Error() string { return e.clientReason() }

func (e *proxyConnectRefusal) clientReason() string {
	return "egress proxy refused CONNECT (" + statusDigits(e.status) + ")"
}

// refuseProxyConnect is the http.Transport.OnProxyConnectResponse of every
// proxied registry client. Without it, Go fails the request with the status
// text of the proxy's answer.
func refuseProxyConnect(_ context.Context, _ *url.URL, _ *http.Request, response *http.Response) error {
	if response.StatusCode != http.StatusOK {
		return &proxyConnectRefusal{status: response.StatusCode}
	}
	return nil
}

// statusDigits writes an HTTP status code as three digits.
func statusDigits(code int) string {
	return fmt.Sprintf("%03d", min(max(code, 0), 999))
}

// distributionErrorCodes is the closed list of error codes in the OCI
// distribution specification. A 502 body repeats only these, in this
// spelling, and never an error's message or detail.
var distributionErrorCodes = []string{
	"BLOB_UNKNOWN", "BLOB_UPLOAD_INVALID", "BLOB_UPLOAD_UNKNOWN", "DIGEST_INVALID",
	"MANIFEST_BLOB_UNKNOWN", "MANIFEST_INVALID", "MANIFEST_UNKNOWN", "NAME_INVALID",
	"NAME_UNKNOWN", "SIZE_INVALID", "UNAUTHORIZED", "DENIED", "UNSUPPORTED",
	"TOOMANYREQUESTS",
}

// specErrorCodes returns the distinct specification error codes of a
// registry error body, {"errors":[{"code":...}]}, at most maxSpecErrorCodes.
func specErrorCodes(body []byte) []string {
	var envelope struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return nil
	}
	var codes []string
	for _, entry := range envelope.Errors {
		for _, known := range distributionErrorCodes {
			if strings.EqualFold(entry.Code, known) && !slices.Contains(codes, known) {
				codes = append(codes, known)
				break
			}
		}
		if len(codes) == maxSpecErrorCodes {
			break
		}
	}
	return codes
}

// readErrorBody reads at most maxDockerErrorBodyBytes of an error response and
// reports whether the body was longer.
func readErrorBody(body io.Reader) ([]byte, bool) {
	data, _ := io.ReadAll(io.LimitReader(body, maxDockerErrorBodyBytes+1))
	if len(data) > maxDockerErrorBodyBytes {
		return data[:maxDockerErrorBodyBytes], true
	}
	return data, false
}

// clientErrorMessage is the message of a Docker 502 body, which anonymous
// clients read: "<stage> failed at <origin>: <reason> (request <id>; details
// in the Depsilo server log)". It is built from typed facts and fixed phrases
// only; any other error reads "Docker registry request failed". requestID
// belongs to the request being answered, never to err, so requests that share
// one inflight failure each name their own.
func clientErrorMessage(err error, requestID string) string {
	message := genericRegistryFailure
	var failure *registryFailure
	var explained clientReasoner
	switch {
	case errors.As(err, &failure):
		message = failure.clientText()
	case errors.As(err, &explained):
		message = explained.clientReason()
	}
	suffix := " (details in the Depsilo server log)"
	if requestID != "" {
		suffix = " (request " + requestID + "; details in the Depsilo server log)"
	}
	return truncateUTF8(message, maxClientMessageBytes) + suffix
}

// truncateUTF8 shortens text to at most limit bytes on a rune boundary and
// marks the cut with an ellipsis.
func truncateUTF8(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	const ellipsis = "…"
	cut := max(limit-len(ellipsis), 0)
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + ellipsis
}

// urlOrigin keeps the scheme and host of raw. Userinfo, path, query and
// fragment can all carry credentials or signed tokens.
func urlOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return opaqueOrigin
	}
	return originOf(parsed)
}

// originOf is scheme://host[:port] of an HTTP URL whose host is an address or
// a DNS-style name and whose port has at most five digits; anything else
// reads as opaqueOrigin.
func originOf(target *url.URL) string {
	if target == nil || (target.Scheme != "http" && target.Scheme != "https") ||
		!plainHost(target.Hostname()) || len(target.Port()) > 5 {
		return opaqueOrigin
	}
	return (&url.URL{Scheme: target.Scheme, Host: target.Host}).String()
}

// displayHost is host when a 502 body may name it, and opaqueOrigin
// otherwise.
func displayHost(host string) string {
	if plainHost(host) {
		return host
	}
	return opaqueOrigin
}

// plainHost reports whether host is an IP address without a zone, or a name
// of at most 253 letters, digits, dots, hyphens and underscores.
func plainHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return address.Zone() == ""
	}
	for index := 0; index < len(host); index++ {
		character := host[index]
		if !('a' <= character && character <= 'z' || 'A' <= character && character <= 'Z' ||
			'0' <= character && character <= '9' || character == '.' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}

// logError is the zap field of an error in a Docker log line.
func logError(err error) zap.Field {
	if err == nil {
		return zap.Skip()
	}
	return zap.String("error", logText(err))
}

// logText renders err for the server log with URL credentials removed. Go
// quotes the URL of a *url.Error, so that URL is redacted before quoting; a
// scan of the text catches the URLs elsewhere.
func logText(err error) string {
	if urlErr, ok := err.(*url.Error); ok && urlErr.Err != nil {
		return urlErr.Op + " " + strconv.Quote(redactLogURL(urlErr.URL)) + ": " + redactLogURLs(urlErr.Err.Error())
	}
	return redactLogURLs(err.Error())
}

// redactLogURL prepares a URL, or a reference such as a registry endpoint,
// for the server log, as containerd and go-containerregistry log registry
// URLs: userinfo and fragment are dropped, and every query value reads
// REDACTED while its key stays. Signed redirect targets carry their
// credentials in query values.
func redactLogURL(raw string) string {
	prefix, rest := "", raw
	if scheme, afterScheme, ok := strings.Cut(raw, "://"); ok && uriScheme(scheme) {
		prefix, rest = scheme+"://", afterScheme
	} else if afterSlashes, ok := strings.CutPrefix(raw, "//"); ok {
		prefix, rest = "//", afterSlashes
	}
	if prefix != "" {
		authorityEnd := strings.IndexAny(rest, "/?#")
		if authorityEnd < 0 {
			authorityEnd = len(rest)
		}
		authority := rest[:authorityEnd]
		if at := strings.LastIndex(authority, "@"); at >= 0 {
			authority = authority[at+1:]
		}
		prefix, rest = prefix+authority, rest[authorityEnd:]
	}
	rest, _, _ = strings.Cut(rest, "#")
	path, query, hasQuery := strings.Cut(rest, "?")
	if !hasQuery {
		return prefix + path
	}
	parameters := strings.Split(query, "&")
	for index, parameter := range parameters {
		if parameter == "" {
			continue
		}
		if key, _, hasValue := strings.Cut(parameter, "="); hasValue {
			parameters[index] = key + "=REDACTED"
		} else {
			parameters[index] = "REDACTED"
		}
	}
	return prefix + path + "?" + strings.Join(parameters, "&")
}

// uriScheme reports whether scheme is an RFC 3986 scheme name.
func uriScheme(scheme string) bool {
	for index := 0; index < len(scheme); index++ {
		character := scheme[index]
		letter := 'a' <= character && character <= 'z' || 'A' <= character && character <= 'Z'
		if !letter && (index == 0 || !('0' <= character && character <= '9' || strings.IndexByte("+-.", character) >= 0)) {
			return false
		}
	}
	return scheme != ""
}

// redactLogURLs applies redactLogURL to the URLs in text that is bound for
// the server log: every absolute HTTP URL, also one whose slashes JSON
// escaped as \/, and every double-quoted URL reference, such as the relative
// or scheme-relative Location that Go quotes when it cannot parse it.
//
// An absolute URL ends at whitespace, a control character, a double quote, a
// backtick or an angle bracket. A backslash escape stays inside it, so a URL
// with an escaped quote or ampersand in its query is redacted whole. A
// double-quoted string is a URL reference when it unquotes, as Go or JSON
// writes strings, to text without whitespace that redaction changes; it is
// written back in Go's quoting. A quote inside a string already read opens
// no string of its own, which keeps the scan linear in the length of text,
// such as a 64 KiB registry answer.
func redactLogURLs(text string) string {
	var redacted strings.Builder
	copied, found, quotedUntil := 0, false, 0
	for index := 0; index < len(text); {
		replacement, end, ok := "", 0, false
		if jsonEscaped, isURL := httpSchemeAt(text, index); isURL {
			end, ok = urlEnd(text, index), true
			if jsonEscaped {
				unescaped := strings.ReplaceAll(text[index:end], `\/`, "/")
				replacement = strings.ReplaceAll(redactLogURL(unescaped), "/", `\/`)
			} else {
				replacement = redactLogURL(text[index:end])
			}
		} else if text[index] == '"' && index >= quotedUntil {
			var reference string
			end, reference, ok = quotedURLReference(text, index)
			quotedUntil = end
			if ok {
				replacement = strconv.Quote(reference)
			}
		}
		if !ok {
			index++
			continue
		}
		redacted.WriteString(text[copied:index])
		redacted.WriteString(replacement)
		index, copied, found = end, end, true
	}
	if !found {
		return text
	}
	redacted.WriteString(text[copied:])
	return redacted.String()
}

// httpSchemeAt reports whether an "http://" or "https://" scheme, in any
// letter case, starts at text[index], and whether JSON escaped its slashes.
func httpSchemeAt(text string, index int) (jsonEscaped, ok bool) {
	for _, scheme := range [...]string{"http://", "https://", `http:\/\/`, `https:\/\/`} {
		if len(text)-index >= len(scheme) && strings.EqualFold(text[index:index+len(scheme)], scheme) {
			return strings.Contains(scheme, `\`), true
		}
	}
	return false, false
}

// urlEnd returns the end of the absolute URL that starts at text[start].
func urlEnd(text string, start int) int {
	end := start + 1
	for end < len(text) {
		if text[end] == '\\' && end+1 < len(text) && text[end+1] > ' ' && text[end+1] != 0x7f {
			end += 2
			continue
		}
		if urlDelimiter(text[end]) {
			break
		}
		end++
	}
	return end
}

func urlDelimiter(character byte) bool {
	return character <= ' ' || character == 0x7f || strings.IndexByte("\"`<>", character) >= 0
}

// quotedURLReference reads the double-quoted string at text[start]. It
// returns where the string ends, or where reading it failed; every quote in
// between is escaped within the string or closes it, so none opens a string
// of its own. When the string's content is a URL reference that redaction
// changes, it also returns the redacted reference.
func quotedURLReference(text string, start int) (end int, reference string, ok bool) {
	end = start + 1
	for end < len(text) && text[end] != '"' {
		if text[end] < ' ' || text[end] == 0x7f {
			return end, "", false
		}
		if text[end] == '\\' {
			end++
		}
		end++
	}
	if end >= len(text) {
		return len(text), "", false
	}
	end++
	literal := text[start:end]
	content, err := strconv.Unquote(literal)
	if err != nil && json.Unmarshal([]byte(literal), &content) != nil {
		return end, "", false
	}
	if strings.IndexFunc(content, func(character rune) bool { return character <= ' ' || character == 0x7f }) >= 0 {
		return end, "", false
	}
	if reference = redactLogURL(content); reference == content {
		return end, "", false
	}
	return end, reference, true
}
