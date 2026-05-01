package cli

import (
	"fmt"

	"depsilo/internal/version"
)

func runVersion(args []string) int {
	fmt.Printf("Depsilo %s (commit: %s, built: %s)\n",
		version.Version, version.Commit, version.BuildDate)
	return 0
}
