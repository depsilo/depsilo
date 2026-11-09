package upstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"syscall"

	"depsilo/internal/egress"
)

// Failure codes explain why an upstream check failed. They are stable API
// values that Admin maps to operator guidance.
const (
	// FailureFakeIP: DNS answered with an RFC 2544 benchmarking address,
	// which is how local proxies in fake-IP mode answer for public hosts.
	FailureFakeIP = "egress_fake_ip"
	// FailureSharedAddress: the host resolved into 100.64.0.0/10, used by
	// carrier-grade NAT and overlay networks, and no range was declared.
	FailureSharedAddress = "egress_shared_address"
	// FailureReserved: another special-purpose address, or a DNS answer that
	// mixed network scopes.
	FailureReserved = "egress_reserved"
	// FailurePrivate: a private or loopback target this origin may not reach.
	FailurePrivate     = "egress_private"
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
	// Status is the HTTP status of a FailureHTTPStatus result.
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

// unsafeDenial names the first refused address among a resolver answer.
func unsafeDenial(policy egress.Policy, addresses []netip.Addr, message string) *EgressDeniedError {
	denial := &EgressDeniedError{unsafe: true, message: message}
	for _, address := range addresses {
		if addrNetworkScope(policy, address) == networkUnsafe {
			denial.Address = address
			denial.Range, _ = egress.SpecialPurposePrefix(address)
			break
		}
	}
	return denial
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
				switch denied.Range {
				case egress.BenchmarkingRange:
					failure.Code = FailureFakeIP
				case egress.SharedAddressSpace:
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
