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
//	depsilo prompt --url http://lan:23333
//	depsilo prompt --ecosystem python    # only this ecosystem
//	depsilo prompt > integrate.md        # pipe into a file
//	depsilo prompt | claude              # or directly into your agent
func runPrompt(args []string) int {
	url := ""
	ecosystem := ""
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
		case strings.HasPrefix(a, "--ecosystem="):
			ecosystem = strings.TrimPrefix(a, "--ecosystem=")
		case a == "--ecosystem" && i+1 < len(args):
			ecosystem = args[i+1]
			i++
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n\n", a)
			printPromptHelp()
			return 1
		}
	}

	if url == "" {
		url = getServerURL()
	} else if err := prompts.ValidateBaseURL(url); err != nil {
		// The value is written into every instruction of the prompt, so it
		// has to be an origin and nothing that could carry more text.
		fmt.Fprintf(os.Stderr, "invalid --url %q: %v\n", url, err)
		fmt.Fprintln(os.Stderr, "Pass the service origin only, for example: depsilo prompt --url http://10.4.20.52:23333")
		return 1
	}

	prompt, ok := prompts.IntegrationFor(url, ecosystem)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown ecosystem: %s (use one of: %s)\n", ecosystem, strings.Join(prompts.Ecosystems(), ", "))
		return 1
	}
	fmt.Print(prompt)
	return 0
}

func printPromptHelp() {
	fmt.Println(`Usage: depsilo prompt [flags]

Print the project-integration prompt for an AI coding agent. Paste the
output into Claude Code / Cursor / Copilot Chat and the agent will edit
the current project's Dockerfile / CI / build scripts to route package
installs through this Depsilo mirror.

The prompt is transparent by default: every generated config edit must name
Depsilo, include the mirror URL, and preserve the original public registry as
a documented rollback rather than an active parallel source, unless the user
explicitly asks for mirror-only mode.

Flags:
    --url <url>     mirror URL to embed (default: $DEPSILO_URL or
                    http://localhost:23333); the origin only, such as
                    http://host:port, with no path, query or credentials
    --ecosystem <id>
                    limit the prompt to one ecosystem (see below)
    -h, --help      show this message

Examples:
    depsilo prompt
    depsilo prompt --url http://10.4.20.52:23333
    depsilo prompt --ecosystem python
    depsilo prompt > integrate.md
    depsilo prompt | pbcopy            # macOS
    depsilo prompt | xclip -selection clipboard  # Linux`)
	fmt.Printf("\nEcosystems:\n    %s\n", strings.Join(prompts.Ecosystems(), ", "))
}
