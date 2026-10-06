package db

import "time"

// Supported collector formats.
const (
	// AuditExporterKindNDJSON posts one JSON object per audit row, for
	// collectors with a generic HTTP/NDJSON intake (Elastic, Datadog, …).
	AuditExporterKindNDJSON = "ndjson"
	// AuditExporterKindSplunkHEC posts Splunk HTTP Event Collector envelopes.
	AuditExporterKindSplunkHEC = "splunk_hec"
)

// AuditExporter forwards audit_log rows to an external collector (SIEM).
//
// Cursor is the highest audit_logs.id already delivered by this exporter. The
// log table is the durable buffer: delivery never blocks a request, a restart
// resumes from the cursor, and a failing collector simply stops the cursor
// from advancing.
type AuditExporter struct {
	ID    uint   `gorm:"primarykey" json:"id"`
	Name  string `gorm:"size:128" json:"name"`
	Kind  string `gorm:"size:16" json:"kind"` // ndjson | splunk_hec
	URL   string `gorm:"size:512" json:"url"`
	Token string `gorm:"size:512" json:"-"` // bearer / HEC token, never serialized
	// Events is "*" or a comma-separated list of action/cache_result values
	// (download, metadata, hit, miss, error, blocked, plus governance actions).
	Events  string `gorm:"size:256;default:'*'" json:"events"`
	Enabled bool   `gorm:"default:true" json:"enabled"`

	Cursor         uint       `gorm:"default:0" json:"cursor"`
	DeliveredCount int64      `gorm:"default:0" json:"delivered_count"`
	LastError      string     `gorm:"size:512" json:"last_error"`
	LastAttemptAt  *time.Time `json:"last_attempt_at"`
	LastSuccessAt  *time.Time `json:"last_success_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (AuditExporter) TableName() string { return "audit_exporters" }
