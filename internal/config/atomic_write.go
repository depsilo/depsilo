package config

import (
	"io/fs"
	"os"
	"path/filepath"
)

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
		if err != nil {
			os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(mode.Perm()); err != nil {
		return outcome, err
	}
	if _, err = tmp.Write(data); err != nil {
		return outcome, err
	}
	if err = tmp.Sync(); err != nil {
		return outcome, err
	}
	if err = tmp.Close(); err != nil {
		return outcome, err
	}
	rename := w.rename
	if rename == nil {
		rename = os.Rename
	}
	if err = rename(tmpName, path); err != nil {
		return outcome, err
	}
	outcome.committed = true
	syncDir := w.syncDir
	if syncDir == nil {
		syncDir = syncDirectory
	}
	outcome.durabilityErr = syncDir(dir)
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
