//go:build darwin

package cli

import "golang.org/x/sys/unix"

// disableTerminalEcho switches echo off and changes nothing else, so input
// that arrives before the read starts is neither displayed nor reinterpreted.
func disableTerminalEcho(fd int) error {
	termios, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return err
	}
	termios.Lflag &^= unix.ECHO
	return unix.IoctlSetTermios(fd, unix.TIOCSETA, termios)
}
