package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"depsilo/internal/distribution"
)

// A valid 1 MiB manifest can expand up to sixfold when HTTP JSON encoding
// escapes HTML characters in source URLs. Leave room for the Set envelope
// while still bounding responses from a misconfigured or untrusted endpoint.
const distributionResponseLimit = 8 << 20

func runDistribution(args []string) int {
	jsonMode, args := stripJSONFlag(args)
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Println("Usage: depsilo distribution stage --manifest FILE --objects DIR [--url URL] [--json]\n" +
			"       depsilo distribution list [--page N] [--per-page N] [--url URL] [--json]\n" +
			"       depsilo distribution prune [--url URL] [--json]\n" +
			"       depsilo distribution inspect|verify|delete ID [--url URL] [--json]\n\n" +
			"Objects are regular files named by their SHA-256 in DIR. Stage is repeatable.\n" +
			"Delete removes the catalog entry; prune reclaims unreferenced object bytes.\n" +
			"This prepares a content-verified catalog only; it does not provide offline package serving.")
		if len(args) == 0 {
			return 1
		}
		return 0
	}
	command := args[0]
	serverURL, manifestPath, objectsPath, id := getServerURL(), "", "", ""
	page, perPage := 1, 20
	paginationSet := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--url", "--manifest", "--objects", "--page", "--per-page":
			option := args[i]
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return distributionCLIError(jsonMode, errors.New("option requires a value"))
			}
			switch option {
			case "--url":
				serverURL = args[i]
			case "--manifest":
				manifestPath = args[i]
			case "--objects":
				objectsPath = args[i]
			case "--page", "--per-page":
				value, err := strconv.Atoi(args[i])
				if err != nil || value < 1 || (option == "--page" && value > 1_000_000) || (option == "--per-page" && value > 100) {
					return distributionCLIError(jsonMode, errors.New("page must be 1–1000000 and per-page must be 1–100"))
				}
				paginationSet = true
				if option == "--page" {
					page = value
				} else {
					perPage = value
				}
			}
		default:
			if strings.HasPrefix(args[i], "-") || id != "" {
				return distributionCLIError(jsonMode, errors.New("unexpected argument; use distribution --help"))
			}
			id = args[i]
		}
	}
	if command != "list" && paginationSet {
		return distributionCLIError(jsonMode, errors.New("pagination options are only available for list"))
	}
	if command == "stage" {
		if id != "" || manifestPath == "" || objectsPath == "" {
			return distributionCLIError(jsonMode, errors.New("stage requires --manifest and --objects"))
		}
	} else if command == "list" || command == "prune" {
		if id != "" || manifestPath != "" || objectsPath != "" {
			return distributionCLIError(jsonMode, errors.New("list and prune do not accept a manifest, objects, or identifier"))
		}
	} else if command == "inspect" || command == "verify" || command == "delete" {
		if !distributionSHA256(id) || manifestPath != "" || objectsPath != "" {
			return distributionCLIError(jsonMode, errors.New("expected one lowercase SHA-256 dependency-set identifier"))
		}
	} else {
		return distributionCLIError(jsonMode, errors.New("unknown distribution command; use distribution --help"))
	}
	base, err := distributionServerURL(serverURL)
	if err != nil {
		return distributionCLIError(jsonMode, err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	// Large object uploads need a separate client from the short JSON helpers.
	// No redirects are followed, so bearer credentials and local file contents
	// cannot be replayed to another endpoint by a 30x response.
	client := &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	request := func(method, path string, body io.Reader) ([]byte, error) {
		return distributionRequest(ctx, client, method, base+path, body)
	}
	var result []byte
	switch command {
	case "stage":
		result, err = stageDistribution(request, manifestPath, objectsPath)
	case "list":
		result, err = request(http.MethodGet, fmt.Sprintf("?page=%d&per_page=%d", page, perPage), nil)
	case "inspect":
		result, err = request(http.MethodGet, "/"+id, nil)
	case "verify":
		result, err = request(http.MethodPost, "/"+id+"/verify", nil)
	case "delete":
		result, err = request(http.MethodDelete, "/"+id, nil)
	case "prune":
		result, err = distributionRequest(ctx, client, http.MethodPost, strings.TrimSuffix(base, "/sets")+"/prune", nil)
	}
	if err != nil {
		return distributionCLIError(jsonMode, err)
	}
	if len(result) == 0 {
		result = []byte(`{"deleted":true}`)
	}
	if !jsonMode {
		fmt.Println("Dependency-set preparation catalog (package serving and installation validation are not provided):")
	}
	var formatted bytes.Buffer
	if json.Indent(&formatted, result, "", "  ") != nil {
		return distributionCLIError(jsonMode, errors.New("server returned an invalid JSON response"))
	}
	fmt.Println(formatted.String())
	if command == "stage" || command == "verify" {
		var report struct {
			ContentStatus string `json:"content_status"`
			Verification  struct {
				ContentStatus string `json:"content_status"`
			} `json:"verification"`
		}
		_ = json.Unmarshal(result, &report)
		if report.ContentStatus != "content_verified" && report.Verification.ContentStatus != "content_verified" {
			return 1
		}
	}
	return 0
}

func distributionCLIError(jsonMode bool, err error) int {
	if jsonMode {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()})
	} else {
		fmt.Fprintln(os.Stderr, "Error:", err)
	}
	return 1
}

func distributionServerURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" ||
		strings.ContainsAny(raw, "\r\n\t ") {
		return "", errors.New("server URL must be HTTP(S) without credentials, query, fragment, or whitespace")
	}
	return strings.TrimRight(u.String(), "/") + "/api/v1/admin/distribution/sets", nil
}

func distributionRequest(ctx context.Context, client *http.Client, method, endpoint string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, errors.New("cannot create dependency-set request")
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(getToken()))
	if method == http.MethodPut {
		req.Header.Set("Content-Type", "application/octet-stream")
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("dependency-set request failed or timed out; retry stage to resume the same manifest")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, distributionResponseLimit+1))
	if err != nil || len(data) > distributionResponseLimit {
		return nil, errors.New("server response could not be read within the response limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure apiError
		if json.Unmarshal(data, &failure) == nil && failure.Message != "" {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, failure.Message)
		}
		return nil, fmt.Errorf("HTTP %d; redirects are not followed", resp.StatusCode)
	}
	return data, nil
}

func stageDistribution(request func(string, string, io.Reader) ([]byte, error), manifestPath, objectsPath string) ([]byte, error) {
	f, err := os.Open(manifestPath)
	if err != nil {
		return nil, errors.New("cannot open manifest file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, distribution.MaxManifestBytes+1))
	_ = f.Close()
	if err != nil || len(raw) > distribution.MaxManifestBytes {
		return nil, errors.New("manifest exceeds the limit or could not be read")
	}
	manifest, err := distribution.ParseManifest(raw)
	if err != nil {
		return nil, errors.New("invalid dependency-set manifest")
	}
	directoryInfo, err := os.Lstat(objectsPath)
	if err != nil || !directoryInfo.IsDir() {
		return nil, errors.New("objects must be a directory, not a symlink")
	}
	root, err := os.OpenRoot(objectsPath)
	if err != nil {
		return nil, errors.New("cannot open objects directory")
	}
	defer root.Close()
	// Preflight all files before mutating the remote catalog. Reopen and check
	// identity again immediately before upload to detect replacement by a link.
	for _, object := range manifest.Objects {
		file, err := distributionObject(root, object.SHA256, object.Size)
		if err != nil {
			return nil, err
		}
		_ = file.Close()
	}
	data, err := request(http.MethodPost, "", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var staged distribution.Set
	sum := sha256.Sum256(raw)
	if json.Unmarshal(data, &staged) != nil || staged.ID != hex.EncodeToString(sum[:]) {
		return nil, errors.New("server returned an invalid dependency-set identifier")
	}
	for _, object := range manifest.Objects {
		file, err := distributionObject(root, object.SHA256, object.Size)
		if err != nil {
			return nil, err
		}
		_, uploadErr := request(http.MethodPut, "/"+staged.ID+"/objects/"+object.SHA256, file)
		_ = file.Close()
		if uploadErr != nil {
			return nil, uploadErr
		}
	}
	verified, err := request(http.MethodPost, "/"+staged.ID+"/verify", nil)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"id": staged.ID, "scope": "catalog_only", "serving": false,
		"provenance_status": "operator_declared", "install_validation": "not_run", "verification": json.RawMessage(verified)})
}

func distributionObject(root *os.Root, digest string, size int64) (*os.File, error) {
	if !distributionSHA256(digest) || size < 0 {
		return nil, errors.New("invalid declared object")
	}
	info, err := root.Lstat(digest)
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return nil, errors.New("a declared object is missing, is not a regular file, or has the wrong size")
	}
	file, err := root.Open(digest)
	if err != nil {
		return nil, errors.New("cannot open declared object")
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) || opened.Size() != size {
		_ = file.Close()
		return nil, errors.New("declared object changed during upload preparation")
	}
	return file, nil
}

func distributionSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
