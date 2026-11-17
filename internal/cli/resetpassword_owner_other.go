//go:build !unix

package cli

import "os"

// fileOwnerUID reports no owner where files have no numeric owner.
func fileOwnerUID(os.FileInfo) (int, bool) { return 0, false }
