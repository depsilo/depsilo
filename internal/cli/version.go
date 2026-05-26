package cli

import (
	"fmt"

	"depsilo/internal/version"
)

func runVersion(args []string) int {
	jsonMode, _ := stripJSONFlag(args)
	if jsonMode {
		printJSON(map[string]string{
			"version": version.Version,
			"commit":  version.Commit,
			"built":   version.BuildDate,
		})
		return 0
	}
	fmt.Printf("Depsilo %s (commit: %s, built: %s)\n",
		version.Version, version.Commit, version.BuildDate)
	return 0
}
