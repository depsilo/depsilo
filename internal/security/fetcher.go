package security

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"depsilo/internal/ecosystem"
	"go.uber.org/zap"
)

var ErrFetcherClosed = errors.New("OSV fetcher is closed")

// OSVEcosystem returns the OSV ecosystem name for a Depsilo adapter type.
func OSVEcosystem(depsiloType string) string {
	return ecosystem.OSVNameFor(depsiloType)
}

// Fetcher queries the OSV.dev API for vulnerability data.
type Fetcher struct {
	client  *http.Client
	baseURL string
	limiter *time.Ticker
	closed  chan struct{}
	close   sync.Once
}

// NewFetcher creates a new OSV.dev API client.
func NewFetcher(baseURL, proxy string) *Fetcher {
	transport := &http.Transport{}
	if proxy != "" {
		if proxyURL, err := url.Parse(proxy); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	return &Fetcher{
		client:  &http.Client{Timeout: 30 * time.Second, Transport: transport},
		baseURL: baseURL,
		limiter: time.NewTicker(time.Second),
		closed:  make(chan struct{}),
	}
}

// Close releases resources.
func (f *Fetcher) Close() {
	f.close.Do(func() {
		if f.closed != nil {
			close(f.closed)
		}
		if f.limiter != nil {
			f.limiter.Stop()
		}
		if f.client != nil {
			f.client.CloseIdleConnections()
		}
	})
}

func (f *Fetcher) waitForRateLimit(ctx context.Context) error {
	if f.limiter == nil {
		return nil
	}
	if f.closed == nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.limiter.C:
			return nil
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.closed:
		return ErrFetcherClosed
	case <-f.limiter.C:
		return nil
	}
}

type osvQueryRequest struct {
	Package *osvPackage `json:"package"`
}

type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type osvBatchRequest struct {
	Queries []osvQueryRequest `json:"queries"`
}

// OSVVulnerability represents a single vulnerability from OSV.dev.
type OSVVulnerability struct {
	ID         string        `json:"id"`
	Summary    string        `json:"summary"`
	Details    string        `json:"details"`
	Aliases    []string      `json:"aliases"`
	Severity   []osvSeverity `json:"severity"`
	Affected   []osvAffected `json:"affected"`
	References []osvRef      `json:"references"`
	Published  time.Time     `json:"published"`
	Modified   time.Time     `json:"modified"`
}

type osvSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type osvAffected struct {
	Package *osvPackage `json:"package"`
	Ranges  []osvRange  `json:"ranges"`
}

type osvRange struct {
	Type   string     `json:"type"`
	Events []osvEvent `json:"events"`
}

type osvEvent struct {
	Introduced   string `json:"introduced,omitempty"`
	Fixed        string `json:"fixed,omitempty"`
	LastAffected string `json:"last_affected,omitempty"`
}

type osvRef struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type osvQueryResponse struct {
	Vulns []OSVVulnerability `json:"vulns"`
}

type osvBatchResponse struct {
	Results []osvQueryResponse `json:"results"`
}

// PackageRef identifies a package by ecosystem and name.
type PackageRef struct {
	Ecosystem string
	Name      string
}

// Query fetches vulnerabilities for a single package.
func (f *Fetcher) Query(ctx context.Context, ecosystem, packageName string) ([]OSVVulnerability, error) {
	osvEco := OSVEcosystem(ecosystem)
	if osvEco == "" {
		return nil, nil
	}
	if err := f.waitForRateLimit(ctx); err != nil {
		return nil, err
	}
	body := osvQueryRequest{Package: &osvPackage{Name: packageName, Ecosystem: osvEco}}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}
	var result osvQueryResponse
	if err := f.doPost(ctx, "/v1/query", data, &result); err != nil {
		return nil, err
	}
	return result.Vulns, nil
}

// QueryBatch fetches vulnerabilities for multiple packages in one request.
func (f *Fetcher) QueryBatch(ctx context.Context, packages []PackageRef) ([][]OSVVulnerability, error) {
	if len(packages) == 0 {
		return nil, nil
	}
	if err := f.waitForRateLimit(ctx); err != nil {
		return nil, err
	}
	queries := make([]osvQueryRequest, len(packages))
	for i, pkg := range packages {
		osvEco := OSVEcosystem(pkg.Ecosystem)
		if osvEco == "" {
			continue
		}
		queries[i] = osvQueryRequest{Package: &osvPackage{Name: pkg.Name, Ecosystem: osvEco}}
	}
	body := osvBatchRequest{Queries: queries}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal batch query: %w", err)
	}
	var result osvBatchResponse
	if err := f.doPost(ctx, "/v1/querybatch", data, &result); err != nil {
		return nil, err
	}
	out := make([][]OSVVulnerability, len(result.Results))
	for i, r := range result.Results {
		out[i] = r.Vulns
	}
	return out, nil
}

// doPost sends a POST request with retry on 5xx/429.
func (f *Fetcher) doPost(ctx context.Context, path string, body []byte, result interface{}) error {
	reqURL := f.baseURL + path
	backoff := time.Second
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := f.client.Do(req)
		if err != nil {
			return fmt.Errorf("OSV request failed: %w", err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return json.Unmarshal(respBody, result)
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			zap.L().Warn("OSV request failed, retrying",
				zap.Int("status", resp.StatusCode),
				zap.Int("attempt", attempt+1),
				zap.Duration("backoff", backoff),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
				continue
			}
		}
		return fmt.Errorf("OSV returned %d: %s", resp.StatusCode, string(respBody))
	}
	return fmt.Errorf("OSV request failed after 3 attempts")
}
