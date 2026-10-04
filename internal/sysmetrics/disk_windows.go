//go:build windows

package sysmetrics

import "errors"

func platformDisk(string) (int64, int64, error) {
	return 0, 0, errors.New("disk capacity sampling is not implemented on this platform")
}
