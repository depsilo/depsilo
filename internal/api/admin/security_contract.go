package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"depsilo/internal/db"
)

type securityDashboardResponse struct {
	TotalVulnerabilities int64            `json:"total_vulnerabilities"`
	AffectedPackages     int64            `json:"affected_packages"`
	BySeverity           map[string]int64 `json:"by_severity"`
	AutoBlockedCount     int64            `json:"auto_blocked_count"`
	LastScanAt           *string          `json:"last_scan_at"`
	ScanInProgress       bool             `json:"scan_in_progress"`
}

type vulnerabilityResponse struct {
	ID             uint      `json:"id"`
	OSVID          string    `json:"osv_id"`
	Ecosystem      string    `json:"ecosystem"`
	PackageName    string    `json:"package_name"`
	AffectedRanges string    `json:"affected_ranges"`
	Severity       string    `json:"severity"`
	CVSSScore      float32   `json:"cvss_score"`
	Summary        string    `json:"summary"`
	Details        string    `json:"details"`
	Aliases        string    `json:"aliases"`
	References     string    `json:"references"`
	PublishedAt    time.Time `json:"published_at"`
	ModifiedAt     time.Time `json:"modified_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type vulnerabilityCheckResponse struct {
	ID                 uint      `json:"id"`
	Ecosystem          string    `json:"ecosystem"`
	PackageName        string    `json:"package_name"`
	HasVulnerabilities bool      `json:"has_vulnerabilities"`
	VulnerabilityCount int       `json:"vulnerability_count"`
	LastFetchedAt      time.Time `json:"last_fetched_at"`
	NextFetchAt        time.Time `json:"next_fetch_at"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type securityPolicyResponse struct {
	ID               uint      `json:"id"`
	Ecosystem        string    `json:"ecosystem"`
	AutoBlockEnabled bool      `json:"auto_block_enabled"`
	MinCVSSScore     float32   `json:"min_cvss_score"`
	CreatedBy        string    `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type updateSecurityPolicyRequest struct {
	AutoBlockEnabled bool               `json:"auto_block_enabled"`
	MinCVSSScore     securityJSONNumber `json:"min_cvss_score"`
}

type securityJSONNumber struct {
	json.Number
}

func (n *securityJSONNumber) UnmarshalJSON(data []byte) error {
	literal := bytes.TrimSpace(data)
	if len(literal) == 0 || (literal[0] != '-' && (literal[0] < '0' || literal[0] > '9')) {
		return errors.New("expected JSON number")
	}
	var number json.Number
	if err := json.Unmarshal(literal, &number); err != nil {
		return err
	}
	n.Number = number
	return nil
}

func (r updateSecurityPolicyRequest) validatedMinCVSSScore() (float32, bool) {
	if r.MinCVSSScore.Number == "" {
		return 0, true
	}
	score, ok := new(big.Rat).SetString(r.MinCVSSScore.String())
	if !ok || score.Sign() < 0 || score.Cmp(big.NewRat(10, 1)) > 0 {
		return 0, false
	}
	value, _ := score.Float32()
	return value, true
}

type securityPage[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
}

func toVulnerabilityResponse(v db.Vulnerability) vulnerabilityResponse {
	return vulnerabilityResponse{
		ID: v.ID, OSVID: v.OSVID, Ecosystem: v.Ecosystem, PackageName: v.PackageName,
		AffectedRanges: v.AffectedRanges, Severity: v.Severity, CVSSScore: v.CVSSScore,
		Summary: v.Summary, Details: v.Details, Aliases: v.Aliases, References: v.References,
		PublishedAt: v.PublishedAt, ModifiedAt: v.ModifiedAt, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func toVulnerabilityResponses(items []db.Vulnerability) []vulnerabilityResponse {
	responses := make([]vulnerabilityResponse, len(items))
	for i, item := range items {
		responses[i] = toVulnerabilityResponse(item)
	}
	return responses
}

func toVulnerabilityCheckResponses(items []db.VulnerabilityCheck) []vulnerabilityCheckResponse {
	responses := make([]vulnerabilityCheckResponse, len(items))
	for i, item := range items {
		responses[i] = vulnerabilityCheckResponse{
			ID: item.ID, Ecosystem: item.Ecosystem, PackageName: item.PackageName,
			HasVulnerabilities: item.HasVulnerabilities, VulnerabilityCount: item.VulnerabilityCount,
			LastFetchedAt: item.LastFetchedAt, NextFetchAt: item.NextFetchAt,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		}
	}
	return responses
}

func toSecurityPolicyResponse(policy db.SecurityPolicy) securityPolicyResponse {
	return securityPolicyResponse{
		ID: policy.ID, Ecosystem: policy.Ecosystem, AutoBlockEnabled: policy.AutoBlockEnabled,
		MinCVSSScore: policy.MinCVSSScore, CreatedBy: policy.CreatedBy,
		CreatedAt: policy.CreatedAt, UpdatedAt: policy.UpdatedAt,
	}
}

func toSecurityPolicyResponses(items []db.SecurityPolicy) []securityPolicyResponse {
	responses := make([]securityPolicyResponse, len(items))
	for i, item := range items {
		responses[i] = toSecurityPolicyResponse(item)
	}
	return responses
}
