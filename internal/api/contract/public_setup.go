package contract

import "time"

// SetupStatusResponse answers GET /setup/status. ListenPort, TokenCommand, and
// ConfigExists are present exactly while setup is pending. RestartPending is
// present, and true, only between setup completion and the restart that
// applies it.
type SetupStatusResponse struct {
	NeedsSetup     bool    `json:"needs_setup"`
	TokenRequired  bool    `json:"token_required"`
	ListenPort     *int    `json:"listen_port,omitempty"`
	TokenCommand   *string `json:"token_command,omitempty"`
	ConfigExists   *bool   `json:"config_exists,omitempty"`
	RestartPending bool    `json:"restart_pending,omitempty"`
}

// SetupRestartStrategy is how the new configuration takes effect: the process
// replaces itself, or an external supervisor must restart it.
type SetupRestartStrategy string

const (
	SetupRestartStrategyExec               SetupRestartStrategy = "exec"
	SetupRestartStrategySupervisorRequired SetupRestartStrategy = "supervisor_required"
)

// SetupCompleteResponse answers POST /setup/complete. ReconnectURL is where
// the browser finds the service once it listens on the configured port.
type SetupCompleteResponse struct {
	Status          string               `json:"status" tstype:"'ok'"`
	Message         string               `json:"message"`
	ReconnectURL    string               `json:"reconnect_url"`
	RestartStrategy SetupRestartStrategy `json:"restart_strategy"`
}

// OnboardingStatus is where the first-project onboarding flow stands.
type OnboardingStatus string

const (
	OnboardingStatusNotStarted OnboardingStatus = "not_started"
	OnboardingStatusCompleted  OnboardingStatus = "completed"
	OnboardingStatusSkipped    OnboardingStatus = "skipped"
)

// OnboardingEvent is one policy-blocked package request observed since
// onboarding began, read from the audit log; Outcome is "blocked". Every other
// request outcome is an OnboardingDeliveryEvent.
type OnboardingEvent struct {
	ID          uint      `json:"id"`
	RequestID   string    `json:"request_id"`
	Ecosystem   string    `json:"ecosystem"`
	PackageName string    `json:"package_name"`
	Version     string    `json:"version"`
	Action      string    `json:"action"`
	Outcome     string    `json:"outcome"`
	StatusCode  int       `json:"status_code"`
	CreatedAt   time.Time `json:"created_at"`
}

// OnboardingDeliveryEvent is one package request the access log recorded,
// with whether it counts as a successful artifact download. CreatedAt is when
// the request started; Version is derived from the cache key and is empty for
// a refused or failed response.
type OnboardingDeliveryEvent struct {
	ID                         uint      `json:"id"`
	RequestID                  string    `json:"request_id"`
	Ecosystem                  string    `json:"ecosystem"`
	PackageName                string    `json:"package_name"`
	Version                    string    `json:"version"`
	CacheKey                   string    `json:"cache_key"`
	Method                     string    `json:"method"`
	DependencyKind             string    `json:"dependency_kind"`
	DeliveryResult             string    `json:"delivery_result"`
	BytesSent                  int64     `json:"bytes_sent"`
	StatusCode                 int       `json:"status_code"`
	Outcome                    string    `json:"outcome"`
	CreatedAt                  time.Time `json:"created_at"`
	SuccessfulArtifactDownload bool      `json:"successful_artifact_download"`
}

// OnboardingStatusResponse answers GET /admin/onboarding/status. The first
// request returns no events, only the cursors later polls continue from.
// NextDeliveryAfterID is empty when a poll sent no delivery cursor.
type OnboardingStatusResponse struct {
	Status              OnboardingStatus          `json:"status"`
	StartedAt           time.Time                 `json:"started_at"`
	Events              []OnboardingEvent         `json:"events"`
	NextAfterID         uint                      `json:"next_after_id"`
	HasMore             bool                      `json:"has_more"`
	DeliveryEvents      []OnboardingDeliveryEvent `json:"delivery_events"`
	NextDeliveryAfterID string                    `json:"next_delivery_after_id"`
	DeliveriesHasMore   bool                      `json:"deliveries_has_more"`
}

// OnboardingUpdateResponse answers PUT /admin/onboarding with the stored
// status, which stays completed once completed.
type OnboardingUpdateResponse struct {
	Status OnboardingStatus `json:"status"`
}
