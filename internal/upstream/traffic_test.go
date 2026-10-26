package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"depsilo/internal/cascade"
	"depsilo/internal/traffic"
)

func TestFetchMetersConfiguredTransportAndActualReads(t *testing.T) {
	for _, route := range []traffic.Route{traffic.RouteDirect, traffic.RoutePeer} {
		t.Run(string(route), func(t *testing.T) {
			meter := traffic.NewMeter()
			SetTrafficMeter(meter)
			t.Cleanup(func() { SetTrafficMeter(nil) })
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if route == traffic.RoutePeer && r.URL.Path != cascade.RelayPath {
					t.Errorf("peer received non-relay request: %s", r.URL.Path)
				}
				// A plain origin cannot claim to be a peer using response headers.
				w.Header().Set(cascade.HeaderInstance, "claimed-parent")
				w.Header().Set(cascade.HeaderCache, cascade.CacheHit)
				_, _ = io.WriteString(w, "abcdefghij")
			}))
			defer server.Close()
			u := newTestUpstream(t, server.URL)
			if route == traffic.RoutePeer {
				u.client, _ = newPeerForServer(t, server, nil)
				u.URL = "https://registry.npmjs.org"
			}
			attr := traffic.NewAttribution()
			result, err := u.Fetch(traffic.WithAttribution(context.Background(), attr), "/pkg")
			if err != nil {
				t.Fatal(err)
			}
			if meter.Totals().Direct.Bytes != 0 || meter.Totals().Peer.Bytes != 0 {
				t.Fatal("declared response length was counted before any read")
			}
			if n, err := io.CopyN(io.Discard, result.Body, 3); n != 3 || err != nil {
				t.Fatalf("partial read = %d, %v", n, err)
			}
			if err := result.Body.Close(); err != nil {
				t.Fatal(err)
			}
			totals := meter.Totals()
			got, other := totals.Direct, totals.Peer
			if route == traffic.RoutePeer {
				got, other = totals.Peer, totals.Direct
			}
			if got != (traffic.UpstreamTotals{Exchanges: 1, Bytes: 3}) || other != (traffic.UpstreamTotals{}) {
				t.Fatalf("transport totals = %+v", totals)
			}
			if requests, bytes := attr.OriginTotals(); requests != 1 || bytes != 3 {
				t.Fatalf("legacy origin attribution = (%d,%d), want (1,3)", requests, bytes)
			}
			if rate := meter.Snapshot(60, time.Now()); rate.OriginRequests != 1 || rate.OriginBytes != 3 {
				t.Fatalf("legacy rolling aggregate = %+v", rate)
			}
		})
	}
}

func TestUpstreamMeterExcludesProbesAndErrors(t *testing.T) {
	meter := traffic.NewMeter()
	SetTrafficMeter(meter)
	t.Cleanup(func() { SetTrafficMeter(nil) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/error" {
			w.WriteHeader(http.StatusNotFound)
		}
		_, _ = io.WriteString(w, "body")
	}))
	defer server.Close()
	u := newTestUpstream(t, server.URL)
	if got := probe(context.Background(), u); got.Err != nil || !got.Healthy {
		t.Fatalf("health probe = %+v", got)
	}
	if _, err := u.Fetch(context.Background(), "/error"); err == nil {
		t.Fatal("error response unexpectedly succeeded")
	}
	for _, item := range []struct{ method, path string }{
		{http.MethodHead, "/pkg"}, {http.MethodGet, "/error"},
	} {
		response, err := u.Request(context.Background(), item.path, RequestOptions{Method: item.method})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	if got := meter.Totals(); got != (traffic.Totals{}) {
		t.Fatalf("probes/errors changed traffic totals: %+v", got)
	}
}

func TestRawRequestDistinguishesRedirectsFromNotModified(t *testing.T) {
	meter := traffic.NewMeter()
	SetTrafficMeter(meter)
	t.Cleanup(func() { SetTrafficMeter(nil) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/revalidate" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Location", "/destination")
		w.WriteHeader(http.StatusMovedPermanently)
		_, _ = io.WriteString(w, "redirect body")
	}))
	defer server.Close()
	u := newTestUpstream(t, server.URL)
	for _, path := range []string{"/redirect", "/revalidate"} {
		response, err := u.Request(context.Background(), path, RequestOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if path == "/redirect" {
			// Only the portion consumed belongs to the redirect-byte subset.
			if _, err := io.CopyN(io.Discard, response.Body, 3); err != nil {
				t.Fatal(err)
			}
		}
		_ = response.Body.Close()
	}
	want := traffic.UpstreamTotals{Exchanges: 2, Bytes: 3, RedirectExchanges: 1, RedirectBytes: 3}
	if got := meter.Totals().Direct; got != want {
		t.Fatalf("redirect/revalidation totals = %+v, want %+v", got, want)
	}
}

type trafficReadCloser struct {
	io.Reader
	closed bool
}

func (b *trafficReadCloser) Close() error {
	b.closed = true
	return nil
}

func TestRawRequestMeterPreservesReadErrorsAndClose(t *testing.T) {
	meter := traffic.NewMeter()
	SetTrafficMeter(meter)
	t.Cleanup(func() { SetTrafficMeter(nil) })
	body := &trafficReadCloser{Reader: &dataThenErrorBody{data: []byte("partial")}}
	u := &Upstream{
		Name: "raw", URL: "https://example.com", health: healthState{healthy: true},
		client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body, ContentLength: 1000}, nil
		})},
	}
	response, err := u.Request(context.Background(), "/pkg", RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	if string(data) != "partial" || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read = %q, %v", data, err)
	}
	if err := response.Body.Close(); err != nil || !body.closed {
		t.Fatalf("close was not forwarded: closed=%v err=%v", body.closed, err)
	}
	if got := meter.Totals().Direct; got != (traffic.UpstreamTotals{Exchanges: 1, Bytes: 7}) {
		t.Fatalf("read-error totals = %+v", got)
	}
}

func TestLegacyOriginOnlyMeterStillWorks(t *testing.T) {
	meter := &originOnlyTestMeter{}
	SetTrafficMeter(meter)
	t.Cleanup(func() { SetTrafficMeter(nil) })
	request := httptest.NewRequest(http.MethodGet, "https://example.com/pkg", nil)
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("payload"))}
	MeterResponse(nil, request, response)
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if meter.requests != 1 || meter.bytes != 7 {
		t.Fatalf("legacy observer = %+v", meter)
	}
}

type originOnlyTestMeter struct{ requests, bytes int64 }

func (m *originOnlyTestMeter) ObserveOriginExchange()     { m.requests++ }
func (m *originOnlyTestMeter) ObserveOriginBytes(n int64) { m.bytes += n }
