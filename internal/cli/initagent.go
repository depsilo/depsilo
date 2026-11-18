package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"depsilo/internal/prompts"
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

	prompt, source := fetchAgentPrompt(endpoint)

	if source == promptSourceEmbedded {
		// Still a usable file, but not this instance's own text: say so.
		fmt.Fprintf(os.Stderr, "Warning: could not fetch the prompt from %s; using the text built into this binary, which lists every ecosystem instead of the ones that instance serves.\n", endpoint)
	}

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
		Action  string `json:"action"` // "created" | "updated" | "skipped" | "would-create" | "would-update" | "error"
		Message string `json:"message,omitempty"`
	}
	results := make([]result, 0, len(targets))

	failed := 0
	for _, t := range targets {
		action, msg, err := applyTarget(t, prompt, endpoint, dryRun)
		if err != nil {
			failed++
			results = append(results, result{Path: t.path, Action: "error", Message: err.Error()})
			continue
		}
		results = append(results, result{Path: t.path, Action: action, Message: msg})
	}
	// One file that could not be handled fails the run, even when the others
	// were written.
	exitCode := 0
	failure := ""
	if failed > 0 {
		exitCode = 1
		failure = fmt.Sprintf("%d of %d files could not be updated", failed, len(targets))
	}

	if jsonMode {
		output := map[string]any{"ok": failed == 0, "endpoint": endpoint, "source": source, "dry_run": dryRun, "results": results}
		if failed > 0 {
			output["error"] = failure
		}
		printJSON(output)
		return exitCode
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

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "Error: %s\n", failure)
		return exitCode
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

The prompt comes from the Depsilo instance and lists the ecosystems it
serves. If the instance cannot be reached, the text built into this binary
is written instead, with a warning. Exits 1 when any file could not be
updated, including one whose depsilo:start marker has lost its end marker.

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
	if i >= 0 {
		j := strings.Index(src[i:], endMark)
		if j < 0 {
			// Appending here would leave two start markers, and the next
			// run would replace everything between the first and the new
			// end marker. The file needs a person.
			return "", "", fmt.Errorf("found %q without a closing %q; restore the closing marker or remove the block, then run again", startMark, endMark)
		}
		j += i
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

// Where the prompt init-agent wrote came from; the "source" field of the
// JSON output.
const (
	promptSourceServer   = "server"
	promptSourceEmbedded = "embedded"
)

// fetchAgentPrompt asks the live server for its bootstrap prompt, which lists
// the ecosystems that instance serves. When the server cannot be reached it
// falls back to the same text rendered in this binary for every ecosystem.
// source reports which of the two the caller got.
func fetchAgentPrompt(endpoint string) (prompt, source string) {
	url := endpoint + "/api/v1/agent-prompt"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err == nil {
		resp, err := httpClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				body, _ := io.ReadAll(resp.Body)
				if len(body) > 0 {
					return strings.TrimSpace(string(body)), promptSourceServer
				}
			}
		}
	}
	return strings.TrimSpace(prompts.BootstrapOffline(endpoint)), promptSourceEmbedded
}
