package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// init-agent writes (or refreshes) the agent-instruction files most AI
// coding agents read on startup, so they know "this project uses Depsilo
// at <url>" without the user having to copy-paste the prompt every time.
//
// Files emitted (per target rules below):
//
//	CLAUDE.md     - Claude Code
//	AGENTS.md     - Codex CLI / OpenClaw / Hermes / generic
//	.cursorrules  - Cursor
//
// In all cases, the Depsilo block is wrapped in machine-readable markers
// so subsequent runs replace the block in-place instead of duplicating
// content. Surrounding user content is preserved untouched.
//
//	<!-- depsilo:start --> ... <!-- depsilo:end -->     (markdown)
//	# depsilo:start ... # depsilo:end                   (.cursorrules)

const (
	mdMarkerStart    = "<!-- depsilo:start -->"
	mdMarkerEnd      = "<!-- depsilo:end -->"
	plainMarkerStart = "# depsilo:start"
	plainMarkerEnd   = "# depsilo:end"
)

type initTarget struct {
	path  string
	style string // "markdown" | "cursorrules"
}

func runInitAgent(args []string) int {
	jsonMode, args := stripJSONFlag(args)

	format := "auto"
	outDir := "."
	endpoint := ""
	dryRun := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--dry-run":
			dryRun = true
		case strings.HasPrefix(a, "--format="):
			format = strings.TrimPrefix(a, "--format=")
		case a == "--format" && i+1 < len(args):
			format = args[i+1]
			i++
		case strings.HasPrefix(a, "--out="):
			outDir = strings.TrimPrefix(a, "--out=")
		case a == "--out" && i+1 < len(args):
			outDir = args[i+1]
			i++
		case strings.HasPrefix(a, "--endpoint="):
			endpoint = strings.TrimPrefix(a, "--endpoint=")
		case a == "--endpoint" && i+1 < len(args):
			endpoint = args[i+1]
			i++
		case a == "--help" || a == "-h":
			printInitAgentHelp()
			return 0
		default:
			fmt.Fprintf(os.Stderr, "unknown arg: %s\n", a)
			printInitAgentHelp()
			return 1
		}
	}

	if endpoint == "" {
		endpoint = getServerURL()
	}

	prompt := fetchAgentPrompt(endpoint)

	targets, err := resolveTargets(outDir, format)
	if err != nil {
		if jsonMode {
			printJSON(map[string]any{"ok": false, "error": err.Error()})
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		return 1
	}

	type result struct {
		Path    string `json:"path"`
		Action  string `json:"action"` // "created" | "updated" | "skipped" | "would-create" | "would-update"
		Message string `json:"message,omitempty"`
	}
	results := make([]result, 0, len(targets))

	for _, t := range targets {
		action, msg, err := applyTarget(t, prompt, endpoint, dryRun)
		if err != nil {
			results = append(results, result{Path: t.path, Action: "error", Message: err.Error()})
			continue
		}
		results = append(results, result{Path: t.path, Action: action, Message: msg})
	}

	if jsonMode {
		printJSON(map[string]any{"ok": true, "endpoint": endpoint, "dry_run": dryRun, "results": results})
		return 0
	}

	for _, r := range results {
		var tag string
		switch r.Action {
		case "created":
			tag = "✓ created"
		case "updated":
			tag = "✓ updated"
		case "skipped":
			tag = "· skipped"
		case "would-create", "would-update":
			tag = "→ " + r.Action
		case "error":
			tag = "✗ error"
		}
		fmt.Printf("%-14s  %s", tag, r.Path)
		if r.Message != "" {
			fmt.Printf("  (%s)", r.Message)
		}
		fmt.Println()
	}

	if !dryRun {
		fmt.Println()
		fmt.Println("AI agents opening this project will now see Depsilo's setup")
		fmt.Println("instructions automatically. Endpoint: " + endpoint)
	}
	return 0
}

func printInitAgentHelp() {
	fmt.Println(`Usage: depsilo init-agent [flags]

Writes agent-instruction files (CLAUDE.md / AGENTS.md / .cursorrules)
so AI coding agents auto-detect this project's Depsilo proxy.

Flags:
    --out <dir>           target directory (default ".")
    --format <mode>       auto | all | claudemd | agentsmd | cursorrules
                          (default "auto" — detects which files to write)
    --endpoint <url>      override Depsilo URL (default $DEPSILO_URL or
                          http://localhost:23333)
    --dry-run             show what would change without writing
    --json, -j            machine-readable JSON output

Examples:
    depsilo init-agent
    depsilo init-agent --format=all --dry-run
    depsilo init-agent --out ../my-project --endpoint http://depsilo.lan`)
}

// resolveTargets decides which files to write based on the format flag
// and the contents of outDir.
func resolveTargets(outDir, format string) ([]initTarget, error) {
	if _, err := os.Stat(outDir); err != nil {
		return nil, fmt.Errorf("output dir %q: %w", outDir, err)
	}

	tgt := func(name, style string) initTarget {
		return initTarget{path: filepath.Join(outDir, name), style: style}
	}

	switch format {
	case "all":
		return []initTarget{
			tgt("CLAUDE.md", "markdown"),
			tgt("AGENTS.md", "markdown"),
			tgt(".cursorrules", "cursorrules"),
		}, nil
	case "claudemd":
		return []initTarget{tgt("CLAUDE.md", "markdown")}, nil
	case "agentsmd":
		return []initTarget{tgt("AGENTS.md", "markdown")}, nil
	case "cursorrules":
		return []initTarget{tgt(".cursorrules", "cursorrules")}, nil
	case "auto":
		// Detect existing project conventions; only fall back to AGENTS.md
		// if nothing is detected (most generic, read by Codex / OpenClaw /
		// Hermes / Aider / etc.).
		var ts []initTarget
		if exists(filepath.Join(outDir, "CLAUDE.md")) || exists(filepath.Join(outDir, ".claude")) {
			ts = append(ts, tgt("CLAUDE.md", "markdown"))
		}
		if exists(filepath.Join(outDir, "AGENTS.md")) {
			ts = append(ts, tgt("AGENTS.md", "markdown"))
		}
		if exists(filepath.Join(outDir, ".cursor")) || exists(filepath.Join(outDir, ".cursorrules")) {
			ts = append(ts, tgt(".cursorrules", "cursorrules"))
		}
		if len(ts) == 0 {
			ts = append(ts, tgt("AGENTS.md", "markdown"))
		}
		return ts, nil
	default:
		return nil, fmt.Errorf("unknown --format %q (want auto|all|claudemd|agentsmd|cursorrules)", format)
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// applyTarget creates or updates a single target file. Returns the action
// taken and an optional short message.
func applyTarget(t initTarget, prompt, endpoint string, dryRun bool) (action, message string, err error) {
	block := buildBlock(t.style, prompt, endpoint)
	startMark, endMark := markers(t.style)

	existing, readErr := os.ReadFile(t.path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return "", "", readErr
	}

	if errors.Is(readErr, os.ErrNotExist) {
		// Create new file with just the block (plus tiny frame).
		content := []byte(initialFile(t.style) + block + "\n")
		if dryRun {
			return "would-create", fmt.Sprintf("%d bytes", len(content)), nil
		}
		if err := os.WriteFile(t.path, content, 0o644); err != nil {
			return "", "", err
		}
		return "created", fmt.Sprintf("%d bytes", len(content)), nil
	}

	// Existing file: in-place replace if marker block already present,
	// otherwise append.
	src := string(existing)
	i := strings.Index(src, startMark)
	j := strings.Index(src, endMark)
	if i >= 0 && j > i {
		// Replace existing block.
		before := src[:i]
		after := src[j+len(endMark):]
		out := before + block + after
		// Suppress writes when content is identical (idempotency).
		if out == src {
			return "skipped", "no changes", nil
		}
		if dryRun {
			return "would-update", "block in-place", nil
		}
		if err := os.WriteFile(t.path, []byte(out), 0o644); err != nil {
			return "", "", err
		}
		return "updated", "block in-place", nil
	}

	// No existing block: append with a one-line separator.
	out := src
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += "\n" + block + "\n"
	if dryRun {
		return "would-update", "block appended", nil
	}
	if err := os.WriteFile(t.path, []byte(out), 0o644); err != nil {
		return "", "", err
	}
	return "updated", "block appended", nil
}

func markers(style string) (start, end string) {
	if style == "cursorrules" {
		return plainMarkerStart, plainMarkerEnd
	}
	return mdMarkerStart, mdMarkerEnd
}

// initialFile returns the leading content to put before the Depsilo block
// when the file is being created from scratch. Kept minimal so the user
// can add their own context on top later.
func initialFile(style string) string {
	if style == "cursorrules" {
		return "# Project rules for Cursor\n\n"
	}
	return "# Project notes for AI agents\n\n"
}

// buildBlock produces the marker-wrapped Depsilo block.
func buildBlock(style, prompt, endpoint string) string {
	if style == "cursorrules" {
		// .cursorrules is plain text; we still bracket with comment-style
		// markers so re-runs can find the block.
		return strings.Join([]string{
			plainMarkerStart,
			"# Depsilo dependency cache at " + endpoint,
			"# Auto-generated by `depsilo init-agent` — edits inside this block",
			"# will be overwritten on the next run.",
			"",
			prompt,
			plainMarkerEnd,
		}, "\n")
	}
	// Markdown
	return strings.Join([]string{
		mdMarkerStart,
		"",
		"## Dependency cache (Depsilo)",
		"",
		"_Auto-generated by `depsilo init-agent`. Edits inside this block will_",
		"_be overwritten on the next run; add your own notes outside the_",
		"_`depsilo:start` / `depsilo:end` markers._",
		"",
		"```",
		prompt,
		"```",
		"",
		mdMarkerEnd,
	}, "\n")
}

// fetchAgentPrompt tries the live server first, falls back to a static
// template embedded here. Either way, the URLs end up template-substituted
// with the resolved endpoint.
func fetchAgentPrompt(endpoint string) string {
	url := endpoint + "/api/v1/agent-prompt"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err == nil {
		resp, err := httpClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				body, _ := io.ReadAll(resp.Body)
				if len(body) > 0 {
					return strings.TrimSpace(string(body))
				}
			}
		}
	}
	return embeddedAgentPrompt(endpoint)
}

func embeddedAgentPrompt(base string) string {
	// Mirrors internal/api/public/discover.go AgentPrompt() so users who
	// run init-agent against a non-running server still get a usable file.
	return fmt.Sprintf(`This workspace has a local dependency cache called Depsilo at %s.
It proxies 14 package ecosystems plus Docker OCI and serves cached artifacts at LAN speed.

Before running any package install in this workspace:

1. Verify it's reachable: curl -sf %s/health

2. Detect which package managers this project uses (requirements.txt,
   package.json, go.mod, Cargo.toml, pom.xml, Gemfile, composer.json,
   *.csproj, environment.yml, DESCRIPTION, Chart.yaml, or import transformers /
   import huggingface_hub in Python source, etc.).

3. Configure ONLY the detected tools. Pick from:

   pip:          pip config set global.index-url %s/pypi/simple/
   npm:          npm config set registry %s/npm/
   go:           go env -w GOPROXY=%s/go,direct
   cargo:        visit %s/ and copy the Cargo block to ~/.cargo/config.toml
   maven:        visit %s/ and copy the Maven mirror block to ~/.m2/settings.xml
   gem:          bundle config mirror.https://rubygems.org %s/rubygems/
   composer:     composer config -g repo.packagist composer %s/composer/
   nuget:        dotnet nuget add source %s/nuget/v3/index.json -n depsilo
   conda:        add channel %s/conda/ to ~/.condarc
   helm:         helm repo add depsilo %s/helm/
   huggingface:  export HF_ENDPOINT=%s/huggingface
   R/CRAN:       options(repos = c(CRAN = "%s/cran/")) in ~/.Rprofile

4. Run install commands normally — they auto-route through Depsilo.

These settings do not provide reliable outage failover. Even Go's ",direct"
suffix advances only after a 404/410 response, not when Depsilo is unreachable.
Keep the original registry settings as documented rollback instructions; do
not use GOPROXY "|direct", which would also bypass Depsilo's 451 enforcement.

For machine-readable access:
  GET %s/api/v1/discover     - service catalog
  GET %s/api/v1/stats        - cache metrics
  POST %s/mcp                - MCP server (initialize / tools/call)
`,
		base, base,
		base, base, base, base, base, base, base, base, base, base, base, base,
		base, base, base,
	)
}
