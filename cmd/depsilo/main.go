package main

import (
	"os"

	"depsilo/internal/cli"
)

func main() {
	// main() stays thin: the command list, dispatch, help and the
	// unknown-command answer all live in internal/cli, so there is exactly
	// one place that knows which commands exist.
	os.Exit(cli.Main(os.Args[1:]))
}
