package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/upstream"
)

const maxUpstreamRequestBodyBytes int64 = 1 << 20

var upstreamMutationFields = map[string]bool{
	"adapter_type":   true,
	"name":           true,
	"url":            true,
	"proxy":          false,
	"priority":       true,
	"probe_mode":     true,
	"probe_interval": true,
}

type upstreamMutationRequest struct {
	AdapterType   string `json:"adapter_type" binding:"required"`
	Name          string `json:"name" binding:"required"`
	URL           string `json:"url" binding:"required"`
	Proxy         string `json:"proxy"`
	Priority      int    `json:"priority" binding:"required"`
	ProbeMode     string `json:"probe_mode" binding:"required"`
	ProbeInterval string `json:"probe_interval" binding:"required"`
}

func (request upstreamMutationRequest) toMutation() upstream.MutationInput {
	return upstream.MutationInput{
		AdapterType:   request.AdapterType,
		Name:          request.Name,
		URL:           request.URL,
		Proxy:         request.Proxy,
		Priority:      request.Priority,
		ProbeMode:     request.ProbeMode,
		ProbeInterval: request.ProbeInterval,
	}
}

func decodeUpstreamMutationRequest(c *gin.Context) (upstreamMutationRequest, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUpstreamRequestBodyBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return upstreamMutationRequest{}, errors.New("request body too large")
		}
		return upstreamMutationRequest{}, err
	}
	return parseUpstreamMutationRequest(body)
}

func parseUpstreamMutationRequest(body []byte) (upstreamMutationRequest, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil {
		return upstreamMutationRequest{}, err
	}
	if opening != json.Delim('{') {
		return upstreamMutationRequest{}, errors.New("request body must be one JSON object")
	}

	values := make(map[string]json.RawMessage, len(upstreamMutationFields))
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return upstreamMutationRequest{}, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return upstreamMutationRequest{}, errors.New("JSON object key must be a string")
		}
		if _, allowed := upstreamMutationFields[key]; !allowed {
			return upstreamMutationRequest{}, fmt.Errorf("unknown upstream field %q", key)
		}
		if _, duplicate := values[key]; duplicate {
			return upstreamMutationRequest{}, fmt.Errorf("duplicate upstream field %q", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return upstreamMutationRequest{}, err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return upstreamMutationRequest{}, fmt.Errorf("upstream field %q cannot be null", key)
		}
		values[key] = value
	}
	closing, err := decoder.Token()
	if err != nil {
		return upstreamMutationRequest{}, err
	}
	if closing != json.Delim('}') {
		return upstreamMutationRequest{}, errors.New("malformed JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return upstreamMutationRequest{}, errors.New("request body must contain one JSON object")
		}
		return upstreamMutationRequest{}, err
	}

	for field, required := range upstreamMutationFields {
		if _, exists := values[field]; required && !exists {
			return upstreamMutationRequest{}, fmt.Errorf("upstream field %q is required", field)
		}
	}

	var request upstreamMutationRequest
	stringFields := []struct {
		name     string
		target   *string
		required bool
	}{
		{name: "adapter_type", target: &request.AdapterType, required: true},
		{name: "name", target: &request.Name, required: true},
		{name: "url", target: &request.URL, required: true},
		{name: "proxy", target: &request.Proxy},
		{name: "probe_mode", target: &request.ProbeMode, required: true},
		{name: "probe_interval", target: &request.ProbeInterval, required: true},
	}
	for _, field := range stringFields {
		value, exists := values[field.name]
		if !exists {
			continue
		}
		if err := json.Unmarshal(value, field.target); err != nil {
			return upstreamMutationRequest{}, fmt.Errorf("upstream field %q must be a string", field.name)
		}
		if field.required && *field.target == "" {
			return upstreamMutationRequest{}, fmt.Errorf("upstream field %q is required", field.name)
		}
	}
	if err := json.Unmarshal(values["priority"], &request.Priority); err != nil {
		return upstreamMutationRequest{}, errors.New("upstream field \"priority\" must be an integer")
	}
	if request.Priority == 0 {
		return upstreamMutationRequest{}, errors.New("upstream field \"priority\" is required")
	}
	return request, nil
}

func writeBadUpstreamRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
}

type adminUpstreamResponse struct {
	ID            uint       `json:"id"`
	AdapterType   string     `json:"adapter_type"`
	Name          string     `json:"name"`
	URL           string     `json:"url"`
	Proxy         string     `json:"proxy"`
	Priority      int        `json:"priority"`
	ProbeMode     string     `json:"probe_mode"`
	ProbeInterval string     `json:"probe_interval"`
	Healthy       bool       `json:"healthy"`
	AvgLatencyMS  int64      `json:"avg_latency_ms"`
	SuccessRate   float64    `json:"success_rate"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
	WorkerRunning bool       `json:"worker_running"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type upstreamListResponse struct {
	Items []adminUpstreamResponse `json:"items"`
	Total int                     `json:"total"`
}

type deleteUpstreamResponse struct {
	DeletedID   uint   `json:"deleted_id"`
	AdapterType string `json:"adapter_type"`
}

type checkResultResponse struct {
	Healthy   bool      `json:"healthy"`
	LatencyMS int64     `json:"latency_ms"`
	CheckedAt time.Time `json:"checked_at"`
	Error     *string   `json:"error"`
}

type checkUpstreamResponse struct {
	Upstream adminUpstreamResponse `json:"upstream"`
	Check    checkResultResponse   `json:"check"`
}

func mapAdminUpstream(item upstream.RuntimeUpstream, canViewCredentials bool) adminUpstreamResponse {
	urlValue, proxyValue := item.URL, item.Proxy
	if !canViewCredentials {
		urlValue = readableMaskedURL(urlValue)
		proxyValue = readableMaskedURL(proxyValue)
	}

	var checkedAt *time.Time
	if !item.LastCheckedAt.IsZero() {
		value := item.LastCheckedAt
		checkedAt = &value
	}

	return adminUpstreamResponse{
		ID:            item.ID,
		AdapterType:   item.AdapterType,
		Name:          item.Name,
		URL:           urlValue,
		Proxy:         proxyValue,
		Priority:      item.Priority,
		ProbeMode:     item.ProbeMode,
		ProbeInterval: item.ProbeInterval,
		Healthy:       item.Healthy,
		AvgLatencyMS:  item.AvgLatencyMS,
		SuccessRate:   item.SuccessRate,
		LastCheckedAt: checkedAt,
		WorkerRunning: item.WorkerRunning,
		CreatedAt:     item.CreatedAt,
		UpdatedAt:     item.UpdatedAt,
	}
}

func readableMaskedURL(value string) string {
	return maskCredentialURL(value)
}

func writeUpstreamError(c *gin.Context, err error) {
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"
	switch {
	case errors.Is(err, upstream.ErrNotFound):
		status, code, message = http.StatusNotFound, "NOT_FOUND", upstream.ErrNotFound.Error()
	case errors.Is(err, upstream.ErrConflict):
		status, code, message = http.StatusConflict, "CONFLICT", upstream.ErrConflict.Error()
	case errors.Is(err, upstream.ErrLastUpstream):
		status, code, message = http.StatusConflict, "LAST_UPSTREAM", upstream.ErrLastUpstream.Error()
	case errors.Is(err, upstream.ErrEcosystemNotActive):
		status, code, message = http.StatusConflict, "ECOSYSTEM_NOT_ACTIVE", upstream.ErrEcosystemNotActive.Error()
	case errors.Is(err, upstream.ErrImmutableEcosystem):
		status, code, message = http.StatusUnprocessableEntity, "IMMUTABLE_ECOSYSTEM", upstream.ErrImmutableEcosystem.Error()
	case errors.Is(err, upstream.ErrInvalidUpstream):
		status, code, message = http.StatusUnprocessableEntity, "INVALID_UPSTREAM", upstream.ErrInvalidUpstream.Error()
	case errors.Is(err, upstream.ErrReconcileFailed):
		code, message = "REGISTRY_RECONCILE_FAILED", "registry reconciliation failed"
	}
	if status == http.StatusInternalServerError {
		zap.L().Error("upstream request failed", zap.Error(err))
	}
	c.JSON(status, gin.H{"code": code, "message": message})
}
