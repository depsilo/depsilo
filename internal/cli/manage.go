package cli

import (
	"fmt"
	"os"
)

// ── runWarmup ───────────────────────────────────────────────────

func runWarmup(args []string) int {
	jsonMode, args := stripJSONFlag(args)
	if len(args) < 2 {
		if jsonMode {
			printJSON(map[string]any{"ok": false, "error": "missing_args", "usage": "depsilo warmup <ecosystem> <package> [package...]"})
			return 1
		}
		fmt.Fprintln(os.Stderr, "Usage: depsilo warmup <ecosystem> <package> [package...]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  depsilo warmup pypi requests")
		fmt.Fprintln(os.Stderr, "  depsilo warmup pypi requests numpy torch")
		fmt.Fprintln(os.Stderr, "  depsilo warmup npm express react")
		return 1
	}

	eco := args[0]
	pkgs := args[1:]

	body := map[string]any{
		"ecosystem": eco,
		"packages":  pkgs,
	}

	var result struct {
		Message  string `json:"message"`
		Packages int    `json:"packages"`
	}

	status, err := postJSON(getServerURL()+"/api/v1/admin/cache/warmup", body, &result)
	if err != nil {
		if jsonMode {
			printJSON(map[string]any{"ok": false, "error": err.Error()})
			return 1
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if status != 200 {
		if jsonMode {
			printJSON(map[string]any{"ok": false, "http_status": status})
			return 1
		}
		fmt.Fprintf(os.Stderr, "Error: server returned HTTP %d\n", status)
		return 1
	}

	if jsonMode {
		printJSON(map[string]any{
			"ok":        true,
			"ecosystem": eco,
			"packages":  result.Packages,
			"message":   result.Message,
		})
		return 0
	}
	fmt.Printf("✓ Warmup started for %d package(s) in %s\n", result.Packages, eco)
	fmt.Println("  Pre-fetching in background. Use 'depsilo status' to check cache progress.")
	return 0
}

// ── runFlush ────────────────────────────────────────────────────

func runFlush(args []string) int {
	jsonMode, _ := stripJSONFlag(args)
	var result struct {
		Message string `json:"message"`
		Deleted int    `json:"deleted"`
	}

	status, err := postJSON(getServerURL()+"/api/v1/admin/cache/cleanup", nil, &result)
	if err != nil {
		if jsonMode {
			printJSON(map[string]any{"ok": false, "error": err.Error()})
			return 1
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if status != 200 {
		if jsonMode {
			printJSON(map[string]any{"ok": false, "http_status": status})
			return 1
		}
		fmt.Fprintf(os.Stderr, "Error: server returned HTTP %d\n", status)
		return 1
	}

	if jsonMode {
		printJSON(map[string]any{
			"ok":      true,
			"message": result.Message,
			"deleted": result.Deleted,
		})
		return 0
	}
	fmt.Printf("✓ %s (cleared %d expired entries)\n", result.Message, result.Deleted)
	return 0
}
