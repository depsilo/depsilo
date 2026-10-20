package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"depsilo/internal/config"
)

func runSetup(args []string) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printSetupHelp()
		if len(args) == 0 {
			return 1
		}
		return 0
	}
	if args[0] != "token" {
		fmt.Fprintln(os.Stderr, "Error: expected 'depsilo setup token'; use --help for usage")
		return 1
	}
	flags := flag.NewFlagSet("depsilo setup token", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = printSetupHelp
	var configPath string
	flags.StringVar(&configPath, "config", "", "Path to the running instance's config.toml")
	flags.StringVar(&configPath, "c", "", "Path to the running instance's config.toml")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Error: setup token does not accept positional arguments")
		return 1
	}
	configFlagSet := false
	flags.Visit(func(f *flag.Flag) { configFlagSet = configFlagSet || f.Name == "config" || f.Name == "c" })
	if configFlagSet {
		if strings.TrimSpace(configPath) == "" {
			fmt.Fprintln(os.Stderr, "Error: --config requires a non-empty path")
			return 1
		}
		previous, present := os.LookupEnv("DEPSILO_CONFIG")
		if err := os.Setenv("DEPSILO_CONFIG", configPath); err != nil {
			fmt.Fprintln(os.Stderr, "Error: cannot select the local configuration")
			return 1
		}
		defer func() {
			if present {
				_ = os.Setenv("DEPSILO_CONFIG", previous)
			} else {
				_ = os.Unsetenv("DEPSILO_CONFIG")
			}
		}()
	}

	// This command must work before config.toml or jwt.key exists and must
	// never initialize or rotate the running instance's signing identity.
	cfg, err := config.LoadForRestore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: cannot load local configuration; check --config or DEPSILO_CONFIG")
		return 1
	}
	// Use the configured listener, just like daemon readiness. DEPSILO_URL
	// intentionally has no effect: token retrieval belongs to local state,
	// and a different remote instance cannot validate that state's lifecycle.
	statusURL := strings.TrimSuffix(daemonReadinessURL(cfg.Server.Host, cfg.Server.Port), "/ready") + "/api/v1/setup/status"
	if err := checkSetupTokenStatus(statusURL); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	token, err := config.ReadBootstrapToken(cfg.ConfigPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: cannot read the current bootstrap token; run this command as the service user with the same configuration, or restart Depsilo to publish it")
		return 1
	}
	fmt.Fprintln(os.Stdout, token)
	return 0
}

func printSetupHelp() {
	fmt.Fprintln(os.Stderr, `Usage:
    depsilo setup token [--config PATH]

Print the bootstrap token for the running local instance's initial setup.
Run as the service user, with the same config path and listener environment.
In Docker Compose: docker compose exec depsilo depsilo setup token
The token is not the JWT signing key and is unavailable after setup completes.`)
}

func checkSetupTokenStatus(statusURL string) error {
	client := newDaemonReadinessClient()
	// Do not attach a stored administrator credential, follow a redirect, or
	// echo an untrusted response body into a terminal that will display a secret.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(statusURL)
	if err != nil {
		return fmt.Errorf("cannot contact the local Depsilo server at %s; start the service and use its config path and listener environment", statusURL)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("local Depsilo setup status returned HTTP %d; no token was read", response.StatusCode)
	}
	var status struct {
		NeedsSetup    *bool `json:"needs_setup"`
		TokenRequired *bool `json:"token_required"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4096))
	if err := decoder.Decode(&status); err != nil || status.NeedsSetup == nil || status.TokenRequired == nil {
		return errors.New("local Depsilo returned an invalid setup status; no token was read")
	}
	if !*status.NeedsSetup {
		return errors.New("initial setup is already complete; no bootstrap token is available")
	}
	if !*status.TokenRequired {
		return errors.New("the running instance does not require a bootstrap token")
	}
	return nil
}
