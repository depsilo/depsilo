package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOSAtomicFileWriterReplacesAndPreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	outcome, err := (osAtomicFileWriter{}).Write(path, []byte("new"), before.Mode())
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.committed || outcome.durabilityErr != nil {
		t.Fatalf("outcome = %+v", outcome)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" || after.Mode().Perm() != 0o640 {
		t.Fatalf("data/mode = %q/%o", data, after.Mode().Perm())
	}
	if os.SameFile(before, after) {
		t.Fatal("expected rename to replace inode")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".config.toml.tmp-*"))
	if len(matches) != 0 {
		t.Fatalf("leftover temp files: %v", matches)
	}
}

func TestOSAtomicFileWriterRenameFailureIsNotCommittedAndDoesNotMutate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	renameErr := errors.New("injected rename failure")
	writer := osAtomicFileWriter{rename: func(string, string) error { return renameErr }}
	outcome, err := writer.Write(path, []byte("new"), 0o640)
	if !errors.Is(err, renameErr) || outcome.committed {
		t.Fatalf("outcome/error = %+v/%v", outcome, err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "old" {
		t.Fatalf("pre-rename failure changed bytes: %q", data)
	}
	matches, globErr := filepath.Glob(filepath.Join(dir, ".config.toml.tmp-*"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("rename failure left temp files: %v", matches)
	}
}

func TestOSAtomicFileWriterDirectorySyncFailureIsAlreadyCommitted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	syncErr := errors.New("injected directory sync failure")
	writer := osAtomicFileWriter{syncDir: func(string) error { return syncErr }}
	outcome, err := writer.Write(path, []byte("new"), 0o640)
	if err != nil {
		t.Fatalf("committed rename reported write failure: %v", err)
	}
	if !outcome.committed || !errors.Is(outcome.durabilityErr, syncErr) {
		t.Fatalf("outcome = %+v", outcome)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "new" {
		t.Fatalf("committed bytes = %q", data)
	}
}

func TestConfigWritableHonorsReadOnlyBits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	if configWritable(path) {
		t.Fatal("read-only file reported writable")
	}
}

func TestConfigWritableRejectsReadOnlyOrMissingParent(t *testing.T) {
	dir := t.TempDir()
	readOnlyDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0o555); err != nil {
		t.Fatal(err)
	}
	if configWritable(filepath.Join(readOnlyDir, "config.toml")) {
		t.Fatal("read-only directory reported writable")
	}
	if configWritable(filepath.Join(dir, "missing", "config.toml")) {
		t.Fatal("missing parent reported writable")
	}
	if configWritable(dir) {
		t.Fatal("directory reported as writable config file")
	}
}
