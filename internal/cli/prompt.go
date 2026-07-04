package cli

import (
	"fmt"
	"os"
	"strings"

	"depsilo/internal/prompts"
)

// runPrompt prints the transparent project-integration prompt to stdout.
// Users pipe this into their AI coding agent (Claude Code / Cursor / Copilot
// Chat) to have it rewrite Dockerfiles / CI / build scripts to route installs
// through this Depsilo mirror.
//
// Usage:
//
//	depsilo prompt                       # uses $DEPSILO_URL or http://localhost:23333
//	depsilo prompt --url http://lan:8080
//	depsilo prompt > integrate.md        # pipe into a file
//	depsilo prompt | claude              # or directly into your agent
func runPrompt(args []string) int {
	url := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			printPromptHelp()
			return 0
		case strings.HasPrefix(a, "--url="):
			url = strings.TrimPrefix(a, "--url=")
		case a == "--url" && i+1 < len(args):
			url = args[i+1]
			i++
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n\n", a)
			printPromptHelp()
			return 1
		}
	}

	if url == "" {
		url = getServerURL()
	}

	fmt.Print(prompts.Integration(url))
	return 0
}

func printPromptHelp() {
	fmt.Println(`Usage: depsilo prompt [flags]

Print the project-integration prompt for an AI coding agent. Paste the
output into Claude Code / Cursor / Copilot Chat and the agent will edit
the current project's Dockerfile / CI / build scripts to route package
installs through this Depsilo mirror.

The prompt is transparent by default: every generated config edit must name
Depsilo, include the mirror URL, and preserve the public registry as a
documented fallback unless the user explicitly asks for mirror-only mode.

Flags:
    --url <url>     mirror URL to embed (default: $DEPSILO_URL or
                    http://localhost:23333)
    -h, --help      show this message

Examples:
    depsilo prompt
    depsilo prompt --url http://10.4.20.52:23333
    depsilo prompt > integrate.md
    depsilo prompt | pbcopy            # macOS
    depsilo prompt | xclip -selection clipboard  # Linux`)
}
