//go:build windows

package runtimeinfo

import "errors"

func readProcessSample() (processSample, error) {
	return processSample{}, errors.New("process resource sampling is not supported on windows")
}
