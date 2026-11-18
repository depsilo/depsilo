package admin

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"time"

	"depsilo/internal/api/contract"
	"depsilo/internal/audit"
	"depsilo/internal/db"
)

func toAuditLogResponse(item db.AuditLog, canViewCredentials bool) contract.AuditLog {
	upstreamURL := item.UpstreamURL
	if !canViewCredentials {
		upstreamURL = maskCredentialURL(upstreamURL)
	}
	return contract.AuditLog{
		ID: item.ID, Ecosystem: item.Ecosystem, PackageName: item.PackageName,
		Version: item.Version, Action: item.Action, CacheResult: contract.AuditLogCacheResult(item.CacheResult),
		ClientIP: item.ClientIP, UserAgent: item.UserAgent, UpstreamURL: upstreamURL,
		LatencyMs: item.LatencyMs, BytesSent: item.BytesSent, StatusCode: item.StatusCode,
		CreatedAt: item.CreatedAt,
	}
}

func maskAuditCredentialURL(raw string) string {
	return maskCredentialURL(raw)
}

func toAuditLogResponses(rows []db.AuditLog, canViewCredentials bool) []contract.AuditLog {
	items := make([]contract.AuditLog, len(rows))
	for i, row := range rows {
		items[i] = toAuditLogResponse(row, canViewCredentials)
	}
	return items
}

func toAuditListResponse(result *audit.QueryResult, ecosystems []audit.EcosystemCount, canViewCredentials bool) contract.AuditLogListResponse {
	counts := make([]contract.EcosystemCount, len(ecosystems))
	for index, count := range ecosystems {
		counts[index] = contract.EcosystemCount{ID: count.ID, Count: count.Count}
	}
	return contract.AuditLogListResponse{
		Items: toAuditLogResponses(result.Items, canViewCredentials), Total: result.Total, Page: result.Page, Ecosystems: counts,
	}
}

func encodeAuditCSV(items []contract.AuditLog) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"Time", "Ecosystem", "Package", "Version", "Action", "Result", "Client IP", "Latency(ms)", "Bytes", "Upstream URL"}); err != nil {
		return nil, err
	}
	for _, item := range items {
		record := []string{
			item.CreatedAt.Format(time.RFC3339), item.Ecosystem, item.PackageName,
			item.Version, item.Action, string(item.CacheResult), item.ClientIP,
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
