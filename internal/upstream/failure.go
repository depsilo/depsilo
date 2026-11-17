package upstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"syscall"

	"depsilo/internal/egress"
)

// Failure codes explain why an upstream check failed. They are stable API
// values that Admin maps to operator guidance. The egress_ codes other than
// FailureEgressProxy are refusals by the outbound guard; FailureEgressProxy
// is a failure at the proxy that carries the upstream.
const (
	// FailureFakeIP: DNS answered from a range that local proxies in fake-IP
	// mode answer public hosts from: a benchmarking range, 198.18.0.0/15
	// (RFC 2544) or 2001:2::/48 (RFC 5180 as corrected by RFC Errata 1752),
	// or a documented unique-local range of proxy software (see
	// egress.UniqueLocalProxyRange).
	FailureFakeIP = "egress_fake_ip"
	// FailureSharedAddress: the host resolved into 100.64.0.0/10, used by
	// carrier-grade NAT and overlay networks, and no range was declared.
	FailureSharedAddress = "egress_shared_address"
	// FailureReserved: another special-purpose address, or a DNS answer that
	// mixed network scopes.
	FailureReserved = "egress_reserved"
	// FailurePrivate: a private or loopback target this origin may not reach.
	FailurePrivate = "egress_private"
	// FailureEgressProxy: the request failed at the proxy that carries the
	// upstream, its own or [egress] upstream_proxy. Either Depsilo could not
	// connect to the proxy, or the proxy answered the CONNECT request for an
	// https target with a status other than 200, which Failure.Status holds.
	// Nothing else tells a failure at the proxy from one at the upstream, so
	// other failures keep their codes: a proxy that does not answer before
	// the check's deadline is FailureTimeout, and a tunnel that the proxy
	// closes, instead of answering or after a 200 that proxy software such
	// as mihomo sends before it connects, is FailureRefused or
	// FailureNetwork. A plain-http upstream needs no CONNECT, so the proxy's
	// answer counts as the upstream's own: a 5xx is FailureHTTPStatus, and a
	// 4xx passes the check.
	FailureEgressProxy = "egress_proxy"
	FailureDNS         = "dns"
	FailureTimeout     = "timeout"
	FailureTLS         = "tls"
	FailureRefused     = "connection_refused"
	FailureHTTPStatus  = "http_status"
	FailureNetwork     = "network"
	failureUnavailable = ""
)

// Failure is a bounded, displayable explanation of a failed upstream check.
// It never carries a URL, credential, header or response body.
type Failure struct {
	Code string
	// Address is the resolved address the outbound guard refused.
	Address string
	// Range is the special-purpose range containing Address.
	Range string
	// Status is the HTTP status of a FailureHTTPStatus result, or the status
	// with which the egress proxy answered CONNECT in a FailureEgressProxy
	// result. It is zero when Depsilo could not connect to the proxy.
	Status int
}

// EgressDeniedError reports a connection the outbound guard refused. Its
// message stays the guard's historical wording; Address and Range explain it.
type EgressDeniedError struct {
	Address netip.Addr
	Range   netip.Prefix
	unsafe  bool
	message string
}

func (e *EgressDeniedError) Error() string { return e.message }

// StatusError reports an upstream response status that marks it unhealthy.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return "upstream returned status " + strconv.Itoa(e.Code) }

// proxyConnectError reports an egress proxy that answered the CONNECT request
// for an https target with a status other than 200. It holds only the status:
// the status text and body are the proxy's own and stay out of messages.
type proxyConnectError struct{ status int }

func (e *proxyConnectError) Error() string {
	return "egress proxy refused CONNECT (" + strconv.Itoa(e.status) + ")"
}

// refuseProxyConnect is the http.Transport.OnProxyConnectResponse of every
// proxied upstream client. Without it, Go fails the request with the status
// text of the proxy's answer, which ClassifyFailure cannot tell apart from
// any other network failure.
func refuseProxyConnect(_ context.Context, _ *url.URL, _ *http.Request, response *http.Response) error {
	if response.StatusCode != http.StatusOK {
		return &proxyConnectError{status: response.StatusCode}
	}
	return nil
}

// unsafeDenial names the first refused address among a resolver answer, and
// the range that makes it unsafe when one does.
func unsafeDenial(policy egress.Policy, addresses []netip.Addr, message string) *EgressDeniedError {
	denial := &EgressDeniedError{unsafe: true, message: message}
	for _, address := range addresses {
		if addrNetworkScope(policy, address) == networkUnsafe {
			denial.Address = address
			denial.Range = refusedRange(address)
			break
		}
	}
	return denial
}

// refusedRange is the range that makes address unsafe on its own: a
// documented unique-local range of proxy software or a special-purpose range.
// It is invalid for an address that only its kind makes unsafe, such as a
// link-local or multicast address.
func refusedRange(address netip.Addr) netip.Prefix {
	if prefix, ok := egress.UniqueLocalProxyRange(address); ok {
		return prefix
	}
	prefix, _ := egress.SpecialPurposePrefix(address)
	return prefix
}

// isFakeIPRange reports whether a refused range is one that local proxies in
// fake-IP mode answer from; see FailureFakeIP.
func isFakeIPRange(prefix netip.Prefix) bool {
	if fake, ok := egress.FakeIPRange(prefix.Addr()); ok && fake == prefix {
		return true
	}
	proxySoftware, ok := egress.UniqueLocalProxyRange(prefix.Addr())
	return ok && proxySoftware == prefix
}

func scopeDenial(addresses []netip.Addr, message string) *EgressDeniedError {
	denial := &EgressDeniedError{message: message}
	if len(addresses) > 0 {
		denial.Address = addresses[0]
	}
	return denial
}

func ipAddrs(addresses []net.IPAddr) []netip.Addr {
	out := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		if ip, ok := netip.AddrFromSlice(address.IP); ok {
			out = append(out, ip.Unmap())
		}
	}
	return out
}

// ClassifyFailure explains a failed check. It returns nil for nil.
func ClassifyFailure(err error) *Failure {
	if err == nil {
		return nil
	}
	var denied *EgressDeniedError
	if errors.As(err, &denied) {
		failure := &Failure{Code: FailurePrivate}
		if denied.Address.IsValid() {
			failure.Address = denied.Address.String()
		}
		if denied.unsafe {
			failure.Code = FailureReserved
			if denied.Range.IsValid() {
				failure.Range = denied.Range.String()
				switch {
				case isFakeIPRange(denied.Range):
					failure.Code = FailureFakeIP
				case denied.Range == egress.SharedAddressSpace:
					failure.Code = FailureSharedAddress
				}
			}
		}
		return failure
	}
	var status *StatusError
	if errors.As(err, &status) {
		return &Failure{Code: FailureHTTPStatus, Status: status.Code}
	}
	// Both egress proxy failures come first. Go wraps the error of a failed
	// connection to the proxy, whether resolving or dialing it or the TLS
	// handshake with an https proxy failed, in a *net.OpError whose Op is
	// "proxyconnect", and the rules below would read that cause as the
	// upstream's own.
	var connect *proxyConnectError
	if errors.As(err, &connect) {
		return &Failure{Code: FailureEgressProxy, Status: connect.status}
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "proxyconnect" {
		return &Failure{Code: FailureEgressProxy}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return &Failure{Code: FailureDNS}
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return &Failure{Code: FailureTimeout}
	}
	if isTLSFailure(err) {
		return &Failure{Code: FailureTLS}
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) {
		return &Failure{Code: FailureRefused}
	}
	return &Failure{Code: FailureNetwork}
}

func isTLSFailure(err error) bool {
	var verification *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	var alert tls.AlertError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.As(err, &verification) || errors.As(err, &record) || errors.As(err, &alert) ||
		errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalid)
}
