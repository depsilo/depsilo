package contract

import "time"

// QuarantineEvent is one auditable supply-chain decision: a minimum release
// age block, serve, or bypass; a blocklist or tamper outcome; or an approval
// or override change. ActorID is 0 for system events.
type QuarantineEvent struct {
	ID        uint   `json:"id"`
	Ecosystem string `json:"ecosystem"`
	Package   string `json:"package"`
	Version   string `json:"version"`
	Action    string `json:"action"`
	Reason    string `json:"reason"`
	// ThresholdSeconds is the minimum release age in effect at the time.
	ThresholdSeconds int64 `json:"threshold_seconds"`
	// AgeAtCallSeconds is the observed release age when the call was made.
	AgeAtCallSeconds int64     `json:"age_at_call_seconds"`
	ActorID          uint      `json:"actor_id"`
	ClientIP         string    `json:"client_ip"`
	CreatedAt        time.Time `json:"created_at"`
}

// QuarantineEventListResponse answers GET /admin/quarantine/events.
type QuarantineEventListResponse struct {
	Items []QuarantineEvent `json:"items"`
	Total int64             `json:"total"`
	// Ecosystems counts the events every other filter matches, per ecosystem.
	Ecosystems []EcosystemCount `json:"ecosystems"`
}

// QuarantineApproval is an operator approval that lets one version through
// the minimum release age. ApprovedBy is the approving user's id.
type QuarantineApproval struct {
	ID         uint      `json:"id"`
	Ecosystem  string    `json:"ecosystem"`
	Package    string    `json:"package"`
	Version    string    `json:"version"`
	Reason     string    `json:"reason"`
	ApprovedBy uint      `json:"approved_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// QuarantineApprovalListResponse answers GET /admin/quarantine/approvals.
type QuarantineApprovalListResponse struct {
	Items []QuarantineApproval `json:"items"`
	Total int64                `json:"total"`
}

// QuarantineApproveResponse answers POST /admin/quarantine/approve with the
// normalized coordinate that was approved. It carries no row id.
type QuarantineApproveResponse struct {
	Ecosystem  string    `json:"ecosystem"`
	Package    string    `json:"package"`
	Version    string    `json:"version"`
	Reason     string    `json:"reason"`
	ApprovedBy uint      `json:"approved_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// QuarantineRevokeResponse answers DELETE /admin/quarantine/approvals/:id
// with the coordinate whose approval was removed.
type QuarantineRevokeResponse struct {
	Ecosystem string `json:"ecosystem"`
	Package   string `json:"package"`
	Version   string `json:"version"`
	RevokedBy uint   `json:"revoked_by"`
	Reason    string `json:"reason"`
}
