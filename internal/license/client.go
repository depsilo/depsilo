package license

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const validateURL = "https://api.lemonsqueezy.com/v1/licenses/validate"

type validateRequest struct {
	LicenseKey   string `json:"license_key"`
	InstanceName string `json:"instance_name"`
}

type validateResponse struct {
	Valid      bool `json:"valid"`
	LicenseKey struct {
		Status           string  `json:"status"`
		ExpiresAt        *string `json:"expires_at"`
		ActivationLimit  int     `json:"activation_limit"`
		ActivationsCount int     `json:"activations_count"`
	} `json:"license_key"`
}

func validate(key string) (*validateResponse, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "depsilo"
	}

	body, _ := json.Marshal(validateRequest{
		LicenseKey:   key,
		InstanceName: hostname,
	})

	client := &http.Client{Timeout: 10 * time.Second}

	// Try with one retry
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(5 * time.Second)
		}

		req, err := http.NewRequest("POST", validateURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnprocessableEntity {
			return &validateResponse{Valid: false}, nil
		}

		var result validateResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			lastErr = fmt.Errorf("decode response: %w", err)
			continue
		}
		return &result, nil
	}

	return nil, fmt.Errorf("validation failed after retries: %w", lastErr)
}
