package adapter

import "time"

// AccessObservation is the transport-neutral outcome of one proxy request.
// It deliberately mirrors only observable request facts; Prometheus and other
// telemetry adapters remain outside the package-proxy domain.
type AccessObservation struct {
	AdapterType string
	Method      string
	Hit         bool
	Upstream    string
	Latency     time.Duration
	StatusCode  int
	BytesSent   int64
}

// RequestObserver receives completed proxy request outcomes. Implementations
// must return quickly and must not mutate the observation.
type RequestObserver interface {
	ObserveAccess(AccessObservation)
}
