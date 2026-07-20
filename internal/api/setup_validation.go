package api

import (
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"depsilo/internal/config"
	"depsilo/internal/ecosystem"
)

const maxSetupUpstreamNameBytes = 128

type setupValidationIssue struct {
	code    string
	message string
}

// validateAndNormalizeSetupRequest validates data that would otherwise only
// fail after the freshly-written configuration is restarted. It also trims
// insignificant surrounding whitespace before config.WriteConfig persists the
// request. Messages never include URL or proxy values because those may embed
// private-registry credentials.
func validateAndNormalizeSetupRequest(req *config.SetupRequest) *setupValidationIssue {
	if req.Server.Port < 1 || req.Server.Port > 65535 {
		return setupIssue("INVALID_PORT", "Port must be between 1 and 65535")
	}

	storagePath := strings.TrimSpace(req.Storage.Path)
	if storagePath == "" {
		return setupIssue("INVALID_STORAGE_PATH", "Storage path is required")
	}
	if containsControlCharacter(storagePath) {
		return setupIssue("INVALID_STORAGE_PATH", "Storage path must not contain control characters")
	}
	req.Storage.Path = filepath.Clean(storagePath)

	keys := make([]string, 0, len(req.Ecosystems))
	for ecosystem := range req.Ecosystems {
		keys = append(keys, ecosystem)
	}
	sort.Strings(keys)
	setupDefinitions := make(map[string]ecosystem.Definition)
	for _, definition := range ecosystem.SetupDefinitions() {
		setupDefinitions[definition.Name] = definition
	}

	hasEnabled := false
	for _, ecosystemName := range keys {
		setup := req.Ecosystems[ecosystemName]
		definition, known := setupDefinitions[ecosystemName]
		// Initial setup currently persists only the shared upstream model.
		// Non-standard configuration (such as Docker registries) must not be
		// accepted as a no-op.
		if !known || !definition.StandardUpstreams {
			return setupIssue("INVALID_ECOSYSTEM", fmt.Sprintf("Ecosystem %q is not supported by initial setup", ecosystemName))
		}
		if !setup.Enabled {
			// Disabled entries are not written. Clear their values so ignored
			// client-side defaults cannot accidentally become active later.
			setup.Upstreams = nil
			req.Ecosystems[ecosystemName] = setup
			continue
		}

		hasEnabled = true
		if len(setup.Upstreams) == 0 {
			return setupIssue("INVALID_UPSTREAM", fmt.Sprintf("Enabled ecosystem %q requires at least one upstream", ecosystemName))
		}

		names := make(map[string]struct{}, len(setup.Upstreams))
		for index := range setup.Upstreams {
			upstream := &setup.Upstreams[index]
			upstream.Name = strings.TrimSpace(upstream.Name)
			upstream.URL = strings.TrimSpace(upstream.URL)
			upstream.Proxy = strings.TrimSpace(upstream.Proxy)

			if upstream.Name == "" || len(upstream.Name) > maxSetupUpstreamNameBytes || containsControlCharacter(upstream.Name) {
				return setupIssue("INVALID_UPSTREAM", fmt.Sprintf("Upstream %d for %q must have a name of at most %d bytes without control characters", index+1, ecosystemName, maxSetupUpstreamNameBytes))
			}
			if _, duplicate := names[upstream.Name]; duplicate {
				return setupIssue("INVALID_UPSTREAM", fmt.Sprintf("Ecosystem %q contains duplicate upstream names", ecosystemName))
			}
			names[upstream.Name] = struct{}{}

			if upstream.Priority <= 0 {
				return setupIssue("INVALID_UPSTREAM", fmt.Sprintf("Upstream %d for %q must have a positive priority", index+1, ecosystemName))
			}
			if !validSetupHTTPURL(upstream.URL) {
				return setupIssue("INVALID_UPSTREAM", fmt.Sprintf("Upstream %d for %q must use an HTTP or HTTPS URL with a host", index+1, ecosystemName))
			}
			if upstream.Proxy != "" && !validSetupHTTPURL(upstream.Proxy) {
				return setupIssue("INVALID_UPSTREAM", fmt.Sprintf("Proxy for upstream %d in %q must use an HTTP or HTTPS URL with a host", index+1, ecosystemName))
			}
		}
		req.Ecosystems[ecosystemName] = setup
	}

	if !hasEnabled {
		return setupIssue("NO_ECOSYSTEM", "At least one ecosystem must be enabled")
	}
	return nil
}

func setupIssue(code, message string) *setupValidationIssue {
	return &setupValidationIssue{code: code, message: message}
}

func validSetupHTTPURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func containsControlCharacter(value string) bool {
	return strings.ContainsFunc(value, unicode.IsControl)
}
