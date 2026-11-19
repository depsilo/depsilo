package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
)

// An MCP read builds its answer in process. It must never fetch the URL the
// request's Host header names, and nothing such a host serves may reach the
// response. Each answer still addresses the client by its own origin.
func TestMCPReadsDoNotFetchRequestHost(t *testing.T) {
	type mcpContents struct {
		Result struct {
			Contents []struct {
				Text string `json:"text"`
			} `json:"contents"`
		} `json:"result"`
	}
	for _, test := range []struct {
		name     string
		body     string
		sentinel string
		check    func(t *testing.T, body []byte, externalURL string)
	}{
		{
			name:     "discover resource",
			body:     `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"depsilo://discover"}}`,
			sentinel: "external-body-sentinel",
			check: func(t *testing.T, body []byte, externalURL string) {
				var envelope mcpContents
				if err := json.Unmarshal(body, &envelope); err != nil {
					t.Fatalf("decode MCP response: %v", err)
				}
				if len(envelope.Result.Contents) != 1 {
					t.Fatalf("resource contents = %#v", envelope.Result.Contents)
				}
				var catalog struct {
					Service string `json:"service"`
					MCP     struct {
						URL string `json:"url"`
					} `json:"mcp"`
				}
				if err := json.Unmarshal([]byte(envelope.Result.Contents[0].Text), &catalog); err != nil {
					t.Fatalf("decode discover resource: %v; text=%q", err, envelope.Result.Contents[0].Text)
				}
				if catalog.Service != "depsilo" {
					t.Fatalf("discover service = %q, want depsilo", catalog.Service)
				}
				if want := externalURL + "/mcp"; catalog.MCP.URL != want {
					t.Fatalf("discover MCP URL = %q, want client-visible %q", catalog.MCP.URL, want)
				}
			},
		},
		{
			name:     "setup prompt",
			body:     `{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"setup"}}`,
			sentinel: "external-prompt-sentinel",
			check: func(t *testing.T, body []byte, externalURL string) {
				var envelope struct {
					Result struct {
						Messages []struct {
							Content struct {
								Text string `json:"text"`
							} `json:"content"`
						} `json:"messages"`
					} `json:"result"`
				}
				if err := json.Unmarshal(body, &envelope); err != nil {
					t.Fatalf("decode MCP response: %v", err)
				}
				if len(envelope.Result.Messages) != 1 {
					t.Fatalf("prompt messages = %#v", envelope.Result.Messages)
				}
				if prompt := envelope.Result.Messages[0].Content.Text; !strings.Contains(prompt, "Depsilo at "+externalURL) {
					t.Fatalf("prompt omitted client-visible base URL %q: %q", externalURL, prompt)
				}
			},
		},
		{
			name:     "stats resource",
			body:     `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"depsilo://stats"}}`,
			sentinel: "external-stats-sentinel",
			check: func(t *testing.T, body []byte, _ string) {
				var envelope mcpContents
				if err := json.Unmarshal(body, &envelope); err != nil {
					t.Fatalf("decode MCP response: %v", err)
				}
				if len(envelope.Result.Contents) != 1 || !strings.Contains(envelope.Result.Contents[0].Text, `"today"`) {
					t.Fatalf("stats resource contents = %#v", envelope.Result.Contents)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, _, readToken := newReadAuthRouter(t)

			var externalCalls atomic.Int32
			external := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				externalCalls.Add(1)
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(`{"today":{"leaked":"` + test.sentinel + `"}}`))
			}))
			defer external.Close()

			request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(test.body))
			request.Host = strings.TrimPrefix(external.URL, "http://")
			request.Header.Set("Authorization", "Bearer "+readToken)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, http.StatusOK, response.Body.String())
			}
			if calls := externalCalls.Load(); calls != 0 {
				t.Fatalf("made %d outbound request(s) to the request Host", calls)
			}
			if strings.Contains(response.Body.String(), test.sentinel) {
				t.Fatalf("returned the attacker-controlled response body: %q", response.Body.String())
			}
			test.check(t, response.Body.Bytes(), external.URL)
		})
	}
}

func TestMCPStatsResourceMatchesPublicStatsContract(t *testing.T) {
	router, _, readToken := newReadAuthRouter(t)

	statsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	statsRequest.Host = "depsilo.example"
	statsResponse := httptest.NewRecorder()
	router.ServeHTTP(statsResponse, statsRequest)
	if statsResponse.Code != http.StatusOK {
		t.Fatalf("public stats status = %d, want %d; body=%q", statsResponse.Code, http.StatusOK, statsResponse.Body.String())
	}
	var publicStats map[string]any
	if err := json.Unmarshal(statsResponse.Body.Bytes(), &publicStats); err != nil {
		t.Fatalf("decode public stats: %v", err)
	}

	requestBody := `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"depsilo://stats"}}`
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(requestBody))
	request.Host = "depsilo.example"
	request.Header.Set("Authorization", "Bearer "+readToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("MCP stats status = %d, want %d; body=%q", response.Code, http.StatusOK, response.Body.String())
	}

	var envelope struct {
		Result struct {
			Contents []struct {
				Text string `json:"text"`
			} `json:"contents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode MCP response: %v", err)
	}
	if len(envelope.Result.Contents) != 1 {
		t.Fatalf("resource contents = %#v", envelope.Result.Contents)
	}
	var mcpStats map[string]any
	if err := json.Unmarshal([]byte(envelope.Result.Contents[0].Text), &mcpStats); err != nil {
		t.Fatalf("decode MCP stats: %v; text=%q", err, envelope.Result.Contents[0].Text)
	}

	if got, want := sortedJSONKeys(mcpStats), sortedJSONKeys(publicStats); !reflect.DeepEqual(got, want) {
		t.Fatalf("MCP stats top-level fields = %v, want public stats fields %v", got, want)
	}
	if _, exposed := mcpStats["top_packages"]; exposed {
		t.Fatal("MCP stats restored privacy-sensitive top_packages")
	}
	for _, field := range []string{"today", "week", "cache", "upstreams", "extra_indexes"} {
		if !reflect.DeepEqual(mcpStats[field], publicStats[field]) {
			t.Fatalf("MCP stats %s = %#v, want public stats value %#v", field, mcpStats[field], publicStats[field])
		}
	}
	for _, field := range []string{"service", "series"} {
		mcpObject, ok := mcpStats[field].(map[string]any)
		if !ok {
			t.Fatalf("MCP stats %s = %#v, want object", field, mcpStats[field])
		}
		publicObject, ok := publicStats[field].(map[string]any)
		if !ok {
			t.Fatalf("public stats %s = %#v, want object", field, publicStats[field])
		}
		if got, want := sortedJSONKeys(mcpObject), sortedJSONKeys(publicObject); !reflect.DeepEqual(got, want) {
			t.Fatalf("MCP stats %s fields = %v, want %v", field, got, want)
		}
	}
}

func sortedJSONKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
