package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"

	"depsilo/internal/api/contract"
	"depsilo/internal/db"
)

type updateSecurityPolicyRequest struct {
	AutoBlockEnabled bool               `json:"auto_block_enabled"`
	MinCVSSScore     securityJSONNumber `json:"min_cvss_score"`
}

// securityJSONNumber accepts a bare JSON number and rejects every other
// literal (string, boolean, null, object). The json.Number field must stay
// named: embedding it promotes json.Number.UnmarshalJSONFrom into this type,
// which json/v2 (the default encoding/json implementation since Go 1.27)
// prefers over the method below — that promoted decoder accepts a quoted
// number, so the guard would silently stop running.
type securityJSONNumber struct {
	number json.Number
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
	n.number = number
	return nil
}

func (r updateSecurityPolicyRequest) validatedMinCVSSScore() (float32, bool) {
	if r.MinCVSSScore.number == "" {
		return 0, true
	}
	score, ok := new(big.Rat).SetString(r.MinCVSSScore.number.String())
	if !ok || score.Sign() < 0 || score.Cmp(big.NewRat(10, 1)) > 0 {
		return 0, false
	}
	value, _ := score.Float32()
	return value, true
}

func toVulnerabilityResponse(v db.Vulnerability) contract.SecurityVulnerability {
	return contract.SecurityVulnerability{
		ID: v.ID, OSVID: v.OSVID, Ecosystem: v.Ecosystem, PackageName: v.PackageName,
		AffectedRanges: v.AffectedRanges, Severity: contract.SecuritySeverity(v.Severity), CVSSScore: v.CVSSScore,
		Summary: v.Summary, Details: v.Details, Aliases: v.Aliases, References: v.References,
		PublishedAt: v.PublishedAt, ModifiedAt: v.ModifiedAt, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func toVulnerabilityResponses(items []db.Vulnerability) []contract.SecurityVulnerability {
	responses := make([]contract.SecurityVulnerability, len(items))
	for i, item := range items {
		responses[i] = toVulnerabilityResponse(item)
	}
	return responses
}

func toVulnerabilityCheckResponses(items []db.VulnerabilityCheck) []contract.SecurityVulnerabilityCheck {
	responses := make([]contract.SecurityVulnerabilityCheck, len(items))
	for i, item := range items {
		responses[i] = contract.SecurityVulnerabilityCheck{
			ID: item.ID, Ecosystem: item.Ecosystem, PackageName: item.PackageName,
			HasVulnerabilities: item.HasVulnerabilities, VulnerabilityCount: item.VulnerabilityCount,
			LastFetchedAt: item.LastFetchedAt, NextFetchAt: item.NextFetchAt,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		}
	}
	return responses
}

func toSecurityPolicyResponse(policy db.SecurityPolicy) contract.SecurityPolicy {
	return contract.SecurityPolicy{
		ID: policy.ID, Ecosystem: policy.Ecosystem, AutoBlockEnabled: policy.AutoBlockEnabled,
		MinCVSSScore: policy.MinCVSSScore, CreatedBy: policy.CreatedBy,
		CreatedAt: policy.CreatedAt, UpdatedAt: policy.UpdatedAt,
	}
}

func toSecurityPolicyResponses(items []db.SecurityPolicy) []contract.SecurityPolicy {
	responses := make([]contract.SecurityPolicy, len(items))
	for i, item := range items {
		responses[i] = toSecurityPolicyResponse(item)
	}
	return responses
}
