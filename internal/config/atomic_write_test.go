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

func TestOSAtomicFileWriterDoesNotRemoveReusedTempPathAfterRenameHookFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected post-rename stop")
	writer := osAtomicFileWriter{
		rename: func(source, target string) error {
			if err := os.Rename(source, target); err != nil {
				return err
			}
			// Model a concurrent creator reusing the now-free temporary name.
			return os.WriteFile(source, []byte("must-survive"), 0o600)
		},
		onStage: func(stage WriteStage) error {
			if stage == WriteStageAfterRename {
				return injected
			}
			return nil
		},
	}
	outcome, err := writer.Write(path, []byte("new"), 0o600)
	if !errors.Is(err, injected) || !outcome.committed {
		t.Fatalf("outcome/error = %+v/%v", outcome, err)
	}
	entries, err := filepath.Glob(filepath.Join(dir, ".config.toml.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("reused temporary path count = %d, want 1", len(entries))
	}
	content, err := os.ReadFile(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "must-survive" {
		t.Fatalf("reused temporary path content = %q, want sentinel", content)
	}
}

func TestOSAtomicFileWriterFaultStagesPreserveAtomicRecoveryInvariant(t *testing.T) {
	stages := []struct {
		name          string
		stage         WriteStage
		wantCommitted bool
		wantContent   string
	}{
		{
			name:          "after temporary write",
			stage:         WriteStageAfterTempWrite,
			wantCommitted: false,
			wantContent:   "old",
		},
		{
			name:          "after temporary fsync",
			stage:         WriteStageAfterTempSync,
			wantCommitted: false,
			wantContent:   "old",
		},
		{
			name:          "before rename",
			stage:         WriteStageBeforeRename,
			wantCommitted: false,
			wantContent:   "old",
		},
		{
			name:          "after rename",
			stage:         WriteStageAfterRename,
			wantCommitted: true,
			wantContent:   "new",
		},
		{
			name:          "after directory fsync",
			stage:         WriteStageAfterDirectorySync,
			wantCommitted: true,
			wantContent:   "new",
		},
	}

	for _, test := range stages {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected process stop")
			writer := osAtomicFileWriter{
				onStage: func(stage WriteStage) error {
					if stage == test.stage {
						return injected
					}
					return nil
				},
			}
			outcome, err := writer.Write(path, []byte("new"), 0o600)
			if !errors.Is(err, injected) {
				t.Fatalf("Write error = %v, want injected stop", err)
			}
			if outcome.committed != test.wantCommitted {
				t.Fatalf("committed = %t, want %t", outcome.committed, test.wantCommitted)
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(content) != test.wantContent {
				t.Fatalf("config content = %q, want %q", content, test.wantContent)
			}
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("config mode = %o, want 600", info.Mode().Perm())
			}
			matches, globErr := filepath.Glob(filepath.Join(dir, ".config.toml.tmp-*"))
			if globErr != nil {
				t.Fatal(globErr)
			}
			if len(matches) != 0 {
				t.Fatalf("fault stage left temporary files: %v", matches)
			}
		})
	}
}

func TestOSAtomicFileWriterInvokesStagesInDurableOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	var stages []WriteStage
	outcome, err := (osAtomicFileWriter{
		onStage: func(stage WriteStage) error {
			stages = append(stages, stage)
			return nil
		},
	}).Write(path, []byte("new"), 0o600)
	if err != nil || !outcome.committed {
		t.Fatalf("outcome/error = %+v/%v", outcome, err)
	}
	want := []WriteStage{
		WriteStageAfterTempWrite,
		WriteStageAfterTempSync,
		WriteStageBeforeRename,
		WriteStageAfterRename,
		WriteStageAfterDirectorySync,
	}
	if len(stages) != len(want) {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
	for index := range want {
		if stages[index] != want[index] {
			t.Fatalf("stage %d = %v, want %v", index, stages[index], want[index])
		}
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
