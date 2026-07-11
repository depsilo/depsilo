package admin

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/url"
	"time"

	"depsilo/internal/audit"
	"depsilo/internal/db"
)

type auditLogResponse struct {
	ID          uint      `json:"id"`
	Ecosystem   string    `json:"ecosystem"`
	PackageName string    `json:"package_name"`
	Version     string    `json:"version"`
	Action      string    `json:"action"`
	CacheResult string    `json:"cache_result"`
	ClientIP    string    `json:"client_ip"`
	UserAgent   string    `json:"user_agent"`
	UpstreamURL string    `json:"upstream_url"`
	LatencyMs   int64     `json:"latency_ms"`
	BytesSent   int64     `json:"bytes_sent"`
	StatusCode  int       `json:"status_code"`
	CreatedAt   time.Time `json:"created_at"`
}

type auditListResponse struct {
	Items []auditLogResponse `json:"items"`
	Total int64              `json:"total"`
	Page  int                `json:"page"`
}

func toAuditLogResponse(item db.AuditLog) auditLogResponse {
	return auditLogResponse{
		ID: item.ID, Ecosystem: item.Ecosystem, PackageName: item.PackageName,
		Version: item.Version, Action: item.Action, CacheResult: item.CacheResult,
		ClientIP: item.ClientIP, UserAgent: item.UserAgent, UpstreamURL: maskAuditURLUserInfo(item.UpstreamURL),
		LatencyMs: item.LatencyMs, BytesSent: item.BytesSent, StatusCode: item.StatusCode,
		CreatedAt: item.CreatedAt,
	}
}

func maskAuditURLUserInfo(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "***"
	}
	if parsed.User == nil {
		return raw
	}
	parsed.User = url.UserPassword("***", "***")
	return parsed.String()
}

func toAuditLogResponses(rows []db.AuditLog) []auditLogResponse {
	items := make([]auditLogResponse, len(rows))
	for i, row := range rows {
		items[i] = toAuditLogResponse(row)
	}
	return items
}

func toAuditListResponse(result *audit.QueryResult) auditListResponse {
	return auditListResponse{Items: toAuditLogResponses(result.Items), Total: result.Total, Page: result.Page}
}

func encodeAuditCSV(items []auditLogResponse) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"Time", "Ecosystem", "Package", "Version", "Action", "Result", "Client IP", "Latency(ms)", "Bytes", "Upstream URL"}); err != nil {
		return nil, err
	}
	for _, item := range items {
		record := []string{
			item.CreatedAt.Format(time.RFC3339), item.Ecosystem, item.PackageName,
			item.Version, item.Action, item.CacheResult, item.ClientIP,
			fmt.Sprintf("%d", item.LatencyMs), fmt.Sprintf("%d", item.BytesSent), item.UpstreamURL,
		}
		for i := range record {
			record[i] = neutralizeCSVCell(record[i])
		}
		if err := w.Write(record); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func neutralizeCSVCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r', '\n':
		return "'" + value
	default:
		return value
	}
}
