package contract

import "time"

// WarmupJobStatus is the lifecycle stage of a cache warmup job.
type WarmupJobStatus string

const (
	WarmupJobStatusQueued      WarmupJobStatus = "queued"
	WarmupJobStatusRunning     WarmupJobStatus = "running"
	WarmupJobStatusCancelling  WarmupJobStatus = "cancelling"
	WarmupJobStatusSucceeded   WarmupJobStatus = "succeeded"
	WarmupJobStatusPartial     WarmupJobStatus = "partial"
	WarmupJobStatusFailed      WarmupJobStatus = "failed"
	WarmupJobStatusInterrupted WarmupJobStatus = "interrupted"
	WarmupJobStatusCancelled   WarmupJobStatus = "cancelled"
)

// WarmupItemStatus is the stage of one package inside a warmup job.
type WarmupItemStatus string

const (
	WarmupItemStatusQueued      WarmupItemStatus = "queued"
	WarmupItemStatusRunning     WarmupItemStatus = "running"
	WarmupItemStatusSucceeded   WarmupItemStatus = "succeeded"
	WarmupItemStatusFailed      WarmupItemStatus = "failed"
	WarmupItemStatusCancelled   WarmupItemStatus = "cancelled"
	WarmupItemStatusInterrupted WarmupItemStatus = "interrupted"
)

// WarmupJobItem is one package of a warmup job.
type WarmupJobItem struct {
	Package string           `json:"package"`
	Status  WarmupItemStatus `json:"status"`
	Detail  string           `json:"detail,omitempty"`
}

// WarmupStartResponse answers POST /admin/cache/warmup and its retry: the
// job was queued with Packages normalized package names.
type WarmupStartResponse struct {
	Message  string          `json:"message"`
	Packages int             `json:"packages"`
	JobID    string          `json:"job_id"`
	Status   WarmupJobStatus `json:"status"`
}

// WarmupJobResponse answers GET /admin/cache/warmup/:id.
type WarmupJobResponse struct {
	JobID     string          `json:"job_id"`
	Ecosystem string          `json:"ecosystem"`
	Status    WarmupJobStatus `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Packages  int             `json:"packages"`
	Items     []WarmupJobItem `json:"items"`
}

// WarmupCancelResponse answers DELETE /admin/cache/warmup/:id. Status is
// cancelling while the job stops, or the terminal status it already had.
type WarmupCancelResponse struct {
	JobID  string          `json:"job_id"`
	Status WarmupJobStatus `json:"status"`
}
