package cli

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"
)

func TestParseServeFlags_Defaults(t *testing.T) {
	var buf bytes.Buffer
	opts, err := ParseServeFlags(nil, &buf)
	if err != nil {
		t.Fatalf("ParseServeFlags(nil): %v", err)
	}
	// All flags default to zero values; the loader picks up env / config / defaults.
	if opts.Port != 0 || opts.Host != "" || opts.ConfigPath != "" || opts.LogLevel != "" {
		t.Errorf("expected zero ServeOptions, got %+v", opts)
	}
}

func TestParseServeFlags_LongAndShort(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want ServeOptions
	}{
		{"port long", []string{"--port", "18080"}, ServeOptions{Port: 18080}},
		{"port short", []string{"-p", "8080"}, ServeOptions{Port: 8080}},
		{"host long", []string{"--host", "127.0.0.1"}, ServeOptions{Host: "127.0.0.1"}},
		{"config long", []string{"--config", "/etc/depsilo.toml"}, ServeOptions{ConfigPath: "/etc/depsilo.toml"}},
		{"config short", []string{"-c", "x.toml"}, ServeOptions{ConfigPath: "x.toml"}},
		{"log-level", []string{"--log-level", "debug"}, ServeOptions{LogLevel: "debug"}},
		{"combined", []string{"-p", "8080", "-c", "x.toml", "--host", "0.0.0.0", "--log-level", "warn"},
			ServeOptions{Port: 8080, Host: "0.0.0.0", ConfigPath: "x.toml", LogLevel: "warn"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			opts, err := ParseServeFlags(c.args, &buf)
			if err != nil {
				t.Fatalf("ParseServeFlags(%v): %v", c.args, err)
			}
			if *opts != c.want {
				t.Errorf("got %+v, want %+v", *opts, c.want)
			}
		})
	}
}

func TestParseServeFlags_PortValidation(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		isErr bool
	}{
		{"negative port", []string{"--port", "-1"}, true},
		{"too high", []string{"--port", "70000"}, true},
		{"valid edge low", []string{"--port", "1"}, false},
		{"valid edge high", []string{"--port", "65535"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			_, err := ParseServeFlags(c.args, &buf)
			if c.isErr {
				if err == nil {
					t.Fatalf("expected error for %v, got nil", c.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %v: %v", c.args, err)
			}
		})
	}
}

func TestParseServeFlags_LogLevelValidation(t *testing.T) {
	bad := []string{"--log-level", "trace"}
	var buf bytes.Buffer
	if _, err := ParseServeFlags(bad, &buf); err == nil {
		t.Errorf("expected error for unknown log level")
	}
	// Accept case-insensitively and emit the canonical value expected by the
	// config loader. "warning" is zap's user-facing alias for "warn".
	for _, input := range []string{"WARN", "warning"} {
		opts, err := ParseServeFlags([]string{"--log-level", input}, &buf)
		if err != nil {
			t.Errorf("%s should be accepted: %v", input, err)
			continue
		}
		if opts.LogLevel != "warn" {
			t.Errorf("log level %q normalized to %q, want warn", input, opts.LogLevel)
		}
	}
}

func TestParseServeFlags_Help(t *testing.T) {
	var buf bytes.Buffer
	_, err := ParseServeFlags([]string{"--help"}, &buf)
	if err != flag.ErrHelp {
		t.Fatalf("expected flag.ErrHelp, got %v", err)
	}
	out := buf.String()
	for _, frag := range []string{"--port", "--host", "--config", "DEPSILO_SERVER_PORT", "Precedence"} {
		if !strings.Contains(out, frag) {
			t.Errorf("serve --help output missing %q", frag)
		}
	}
}

func TestServeOptions_ApplyEnv(t *testing.T) {
	// Snapshot + restore the env keys we touch so the test is
	// hermetic regardless of how the rest of the suite ran.
	keys := []string{"DEPSILO_SERVER_PORT", "DEPSILO_SERVER_HOST", "DEPSILO_CONFIG", "DEPSILO_SERVER_LOG_LEVEL"}
	prev := make(map[string]string, len(keys))
	for _, k := range keys {
		prev[k] = os.Getenv(k)
		_ = os.Unsetenv(k)
	}
	defer func() {
		for _, k := range keys {
			if prev[k] == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, prev[k])
			}
		}
	}()

	(&ServeOptions{Port: 8080, Host: "127.0.0.1", ConfigPath: "x.toml", LogLevel: "debug"}).ApplyEnv()
	if got := os.Getenv("DEPSILO_SERVER_PORT"); got != "8080" {
		t.Errorf("DEPSILO_SERVER_PORT = %q, want 8080", got)
	}
	if got := os.Getenv("DEPSILO_SERVER_HOST"); got != "127.0.0.1" {
		t.Errorf("DEPSILO_SERVER_HOST = %q, want 127.0.0.1", got)
	}
	if got := os.Getenv("DEPSILO_CONFIG"); got != "x.toml" {
		t.Errorf("DEPSILO_CONFIG = %q, want x.toml", got)
	}
	if got := os.Getenv("DEPSILO_SERVER_LOG_LEVEL"); got != "debug" {
		t.Errorf("DEPSILO_SERVER_LOG_LEVEL = %q, want debug", got)
	}
}

func TestServeOptions_ApplyEnv_ZeroValuesPreservePreexistingEnv(t *testing.T) {
	// If the user already set DEPSILO_SERVER_PORT=9000 in their
	// environment AND passes `depsilo serve` with no flags, ApplyEnv
	// must NOT clobber the env back to empty. Only explicit flags
	// overwrite.
	prev := os.Getenv("DEPSILO_SERVER_PORT")
	_ = os.Setenv("DEPSILO_SERVER_PORT", "9000")
	defer func() {
		if prev == "" {
			_ = os.Unsetenv("DEPSILO_SERVER_PORT")
		} else {
			_ = os.Setenv("DEPSILO_SERVER_PORT", prev)
		}
	}()

	(&ServeOptions{}).ApplyEnv()
	if got := os.Getenv("DEPSILO_SERVER_PORT"); got != "9000" {
		t.Errorf("env was clobbered; got %q, want preserved 9000", got)
	}
}
