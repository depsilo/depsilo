package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDaemonLastMessage(t *testing.T) {
	long := strings.Repeat("é", 400) // 800 bytes; byte 500 falls between two characters
	for _, test := range []struct {
		name, log, want string
	}{
		{name: "empty log", log: "", want: ""},
		{name: "only blank lines", log: "\n  \n\t\n", want: ""},
		{name: "last line without a newline", log: "first\nsecond", want: "second"},
		{name: "trailing blank lines are skipped", log: "first\nPort 1 is already in use.\n\n   \n", want: "Port 1 is already in use."},
		{name: "control characters are removed", log: "first\n\x1b[31mfailed\x07 to\tstart\x00\r\n", want: "[31mfailed tostart"},
		{name: "invalid bytes are removed", log: "ok\nbad\xff\xfebytes\n", want: "badbytes"},
		{name: "cut at 500 bytes on a character boundary", log: "first\n" + long + "\n", want: strings.Repeat("é", 250)},
		{name: "a line past the read window", log: strings.Repeat("x", daemonLastMessageWindow+10) + "\nlast\n", want: "last"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "depsilo.log")
			if err := os.WriteFile(path, []byte(test.log), 0o600); err != nil {
				t.Fatal(err)
			}
			log, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			got := daemonLastMessage(log)
			if got != test.want || len(got) > daemonLastMessageLimit || !utf8.ValidString(got) {
				t.Fatalf("daemonLastMessage() = %q (%d bytes), want %q", got, len(got), test.want)
			}
		})
	}

	t.Run("unreadable log", func(t *testing.T) {
		log, err := os.Create(filepath.Join(t.TempDir(), "depsilo.log"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := log.WriteString("line\n"); err != nil {
			t.Fatal(err)
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
		if got := daemonLastMessage(log); got != "" {
			t.Fatalf("daemonLastMessage() on a closed file = %q, want nothing", got)
		}
	})
}
