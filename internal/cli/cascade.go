package cli

import (
	"fmt"
	"os"
	"strings"
)

const cascadeUsage = `Usage: depsilo cascade pair [--url URL] [--json]
       depsilo cascade join <code> [--name NAME] [--no-route-all] [--json]

pair      Make this node a cascade parent and print the pairing code. It turns
          cascade on, with a generated token when none is set. --url is the
          address children reach this node at (default: the server address
          below); the code embeds it and the token, so share it like a
          password.
join      Use the parent a pairing code names: checks that the parent accepts
          the token, adds it, and routes every upstream through it, falling
          back to the origin while the parent is down. --no-route-all only
          adds the parent; --name names it (default "parent").

Both take effect without a restart. They need an administrator token:
DEPSILO_TOKEN, or the file ~/.config/depsilo/token.
` + serverAddressUsage

func printCascadeHelp() { fmt.Print(cascadeUsage) }

func runCascade(args []string) int {
	jsonMode, args := stripJSONFlag(args)
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printCascadeHelp()
		if len(args) == 0 {
			return 1
		}
		return 0
	}
	switch args[0] {
	case "pair":
		return runCascadePair(args[1:], jsonMode)
	case "join":
		return runCascadeJoin(args[1:], jsonMode)
	}
	return cascadeFail(jsonMode, fmt.Sprintf("unknown cascade command %q; run depsilo cascade --help", args[0]))
}

func runCascadePair(args []string, jsonMode bool) int {
	address := getServerURL()
	for index := 0; index < len(args); index++ {
		switch {
		case args[index] == "--url" && index+1 < len(args):
			index++
			address = args[index]
		case strings.HasPrefix(args[index], "--url="):
			address = strings.TrimPrefix(args[index], "--url=")
		default:
			return cascadeFail(jsonMode, fmt.Sprintf("unexpected argument %q", args[index]))
		}
	}
	var result struct {
		Code            string `json:"code"`
		URL             string `json:"url"`
		EnabledNow      bool   `json:"enabled_now"`
		Loopback        bool   `json:"loopback"`
		RestartRequired bool   `json:"restart_required"`
	}
	if _, err := postJSON(getServerURL()+"/api/v1/admin/cascade/pairing", map[string]string{"url": address}, &result); err != nil {
		return cascadeFail(jsonMode, err.Error())
	}
	if jsonMode {
		printJSON(map[string]any{"ok": true, "code": result.Code, "url": result.URL, "enabled_now": result.EnabledNow, "loopback": result.Loopback, "restart_required": result.RestartRequired})
		return 0
	}
	if result.EnabledNow {
		fmt.Println("Cascade is now on: this node accepts children.")
	}
	if result.Loopback {
		fmt.Fprintf(os.Stderr, "Warning: %s reaches this node only from this machine; pass --url with an address children can reach.\n", result.URL)
	}
	if result.RestartRequired {
		fmt.Fprintln(os.Stderr, "Warning: cascade settings come from the environment here; restart Depsilo to apply them.")
	}
	fmt.Printf("Pairing code for %s (it contains the token; share it like a password):\n\n%s\n\nOn the child: depsilo cascade join <code>, or paste it in Admin > Upstreams > Cascade.\n", result.URL, result.Code)
	return 0
}

func runCascadeJoin(args []string, jsonMode bool) int {
	code, name, routeAll := "", "", true
	for index := 0; index < len(args); index++ {
		switch argument := args[index]; {
		case argument == "--name" && index+1 < len(args):
			index++
			name = args[index]
		case strings.HasPrefix(argument, "--name="):
			name = strings.TrimPrefix(argument, "--name=")
		case argument == "--no-route-all":
			routeAll = false
		case code == "" && !strings.HasPrefix(argument, "-"):
			code = argument
		default:
			return cascadeFail(jsonMode, fmt.Sprintf("unexpected argument %q", argument))
		}
	}
	if code == "" {
		return cascadeFail(jsonMode, "missing pairing code; run depsilo cascade pair on the parent")
	}
	var result struct {
		Peer                string   `json:"peer"`
		URL                 string   `json:"url"`
		Bound               int      `json:"bound"`
		Skipped             []string `json:"skipped"`
		InsecureHTTPAllowed bool     `json:"insecure_http_allowed"`
		RestartRequired     bool     `json:"restart_required"`
	}
	body := map[string]any{"code": code, "name": name, "route_all": routeAll}
	if _, err := postJSON(getServerURL()+"/api/v1/admin/cascade/join", body, &result); err != nil {
		return cascadeFail(jsonMode, err.Error())
	}
	if jsonMode {
		printJSON(map[string]any{"ok": true, "peer": result.Peer, "url": result.URL, "bound": result.Bound, "skipped": result.Skipped, "insecure_http_allowed": result.InsecureHTTPAllowed, "restart_required": result.RestartRequired})
		return 0
	}
	fmt.Printf("Joined %s as %q.\n", result.URL, result.Peer)
	if routeAll && !result.RestartRequired {
		fmt.Printf("%d upstream(s) now fetch through it.\n", result.Bound)
		if len(result.Skipped) > 0 {
			fmt.Printf("Left direct (local or private origin): %s\n", strings.Join(result.Skipped, ", "))
		}
	}
	if result.InsecureHTTPAllowed {
		fmt.Fprintln(os.Stderr, "Warning: the parent uses plain HTTP, so allow_insecure_http is now on; the token crosses the network unencrypted.")
	}
	if result.RestartRequired {
		fmt.Fprintln(os.Stderr, "Warning: cascade settings come from the environment here; restart Depsilo, then bind upstreams in Admin.")
	}
	return 0
}

func cascadeFail(jsonMode bool, message string) int {
	if jsonMode {
		printJSON(map[string]any{"ok": false, "error": message})
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	}
	return 1
}
