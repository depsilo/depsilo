package server

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLocalStoragePathsOverlapResolvesSymlinkParents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not reliably available on Windows test hosts")
	}
	root := t.TempDir()
	realRoot := filepath.Join(root, "real")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Fatal(err)
	}
	overlaps, err := localStoragePathsOverlap(
		filepath.Join(realRoot, "package-cache"),
		filepath.Join(alias, "package-cache", "compile-cache"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !overlaps {
		t.Fatal("symlinked nested storage roots were not detected")
	}

	overlaps, err = localStoragePathsOverlap(
		filepath.Join(realRoot, "package-cache"),
		filepath.Join(alias, "compile-cache"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if overlaps {
		t.Fatal("distinct sibling storage roots were reported as overlapping")
	}
}
