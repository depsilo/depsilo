package config

import (
	"io/fs"
	"os"
	"path/filepath"
)

// WriteStage marks the points at which an atomic config publication can be
// interrupted. These stages are useful to integration tests that model a
// process stop without relying on a timing-sensitive SIGKILL. They are an
// internal test seam, not a compatibility contract for callers outside this
// repository.
type WriteStage uint8

const (
	WriteStageAfterTempWrite WriteStage = iota
	WriteStageAfterTempSync
	WriteStageBeforeRename
	WriteStageAfterRename
	WriteStageAfterDirectorySync
)

// WriteStageHook is invoked only by WriteConfigWithStageHook. Normal callers
// use WriteConfig and perform no fault injection.
type WriteStageHook func(WriteStage) error

type atomicWriteOutcome struct {
	committed     bool
	durabilityErr error
}

type atomicFileWriter interface {
	Write(path string, data []byte, mode fs.FileMode) (atomicWriteOutcome, error)
}

type osAtomicFileWriter struct {
	rename  func(string, string) error
	syncDir func(string) error
	onStage WriteStageHook
}

func (w osAtomicFileWriter) Write(path string, data []byte, mode fs.FileMode) (outcome atomicWriteOutcome, err error) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return outcome, err
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		// Once rename succeeds, tmpName no longer names the temporary inode.
		// Do not remove that path after a post-rename hook failure: another
		// process could have legitimately created a new file with the same
		// name while the publication was completing.
		if err != nil && !outcome.committed {
			os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(mode.Perm()); err != nil {
		return outcome, err
	}
	if _, err = tmp.Write(data); err != nil {
		return outcome, err
	}
	if w.onStage != nil {
		if err = w.onStage(WriteStageAfterTempWrite); err != nil {
			return outcome, err
		}
	}
	if err = tmp.Sync(); err != nil {
		return outcome, err
	}
	if w.onStage != nil {
		if err = w.onStage(WriteStageAfterTempSync); err != nil {
			return outcome, err
		}
	}
	if err = tmp.Close(); err != nil {
		return outcome, err
	}
	if w.onStage != nil {
		if err = w.onStage(WriteStageBeforeRename); err != nil {
			return outcome, err
		}
	}
	rename := w.rename
	if rename == nil {
		rename = os.Rename
	}
	if err = rename(tmpName, path); err != nil {
		return outcome, err
	}
	outcome.committed = true
	if w.onStage != nil {
		if err = w.onStage(WriteStageAfterRename); err != nil {
			return outcome, err
		}
	}
	syncDir := w.syncDir
	if syncDir == nil {
		syncDir = syncDirectory
	}
	outcome.durabilityErr = syncDir(dir)
	if outcome.durabilityErr == nil && w.onStage != nil {
		if err = w.onStage(WriteStageAfterDirectorySync); err != nil {
			return outcome, err
		}
	}
	return outcome, nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		dir.Close()
		return err
	}
	return dir.Close()
}

func configWritable(path string) bool {
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() || info.Mode().Perm()&0o222 == 0 {
			return false
		}
	} else if !os.IsNotExist(err) {
		return false
	}
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o222 == 0 {
		return false
	}
	probe, err := os.CreateTemp(dir, ".depsilo-config-write-probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	return closeErr == nil && removeErr == nil
}
