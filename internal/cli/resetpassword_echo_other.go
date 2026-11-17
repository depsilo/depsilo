//go:build !darwin && !linux

package cli

// disableTerminalEcho is a no-op where echo cannot be switched off on its
// own; the no-echo read that follows still hides what is typed.
func disableTerminalEcho(int) error { return nil }
