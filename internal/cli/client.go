package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// apiError represents an error response from the Depsilo API.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

// getJSON performs a GET request and decodes the JSON response into target.
// Returns HTTP status code and any error.
func getJSON(url string, target any) (int, error) {
	status, body, err := getJSONBody(url)
	if err != nil {
		return status, err
	}

	if status >= 400 {
		var errResp apiError
		if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
			return status, &errResp
		}
		return status, fmt.Errorf("HTTP %d", status)
	}

	if target != nil {
		if err := json.Unmarshal(body, target); err != nil {
			return status, fmt.Errorf("parse response: %w", err)
		}
	}

	return status, nil
}

// getJSONAnyStatus decodes a JSON response even when the HTTP status is not
// successful. Readiness responses intentionally carry dependency details with
// HTTP 503, and callers need those details to produce an actionable failure.
func getJSONAnyStatus(url string, target any) (int, error) {
	status, body, err := getJSONBody(url)
	if err != nil {
		return status, err
	}
	if target != nil {
		if err := json.Unmarshal(body, target); err != nil {
			return status, fmt.Errorf("parse response: %w", err)
		}
	}
	return status, nil
}

func getJSONBody(url string) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("create request: %w", err)
	}

	token := getToken()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read response: %w", err)
	}
	return resp.StatusCode, body, nil
}

// postJSON performs a POST request with optional JSON body and decodes response.
func postJSON(url string, body any, target any) (int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(http.MethodPost, url, reqBody)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	token := getToken()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp apiError
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Message != "" {
			return resp.StatusCode, &errResp
		}
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if target != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, target); err != nil {
			return resp.StatusCode, fmt.Errorf("parse response: %w", err)
		}
	}

	return resp.StatusCode, nil
}
