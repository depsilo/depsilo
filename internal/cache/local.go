package cache

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

type LocalStorage struct {
	basePath string
}

func NewLocalStorage(basePath string) (*LocalStorage, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}
	return &LocalStorage{basePath: basePath}, nil
}

func (s *LocalStorage) fullPath(key string) string {
	// Sanitize key to prevent path traversal
	clean := filepath.Clean(key)
	clean = strings.TrimPrefix(clean, "/")
	return filepath.Join(s.basePath, clean)
}

func (s *LocalStorage) Exists(_ context.Context, key string) (bool, error) {
	_, err := os.Stat(s.fullPath(key))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *LocalStorage) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	p := s.fullPath(key)
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, fmt.Errorf("key not found: %s", key)
		}
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	p := s.fullPath(key)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close file: %w", err)
	}

	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename temp file: %w", err)
	}

	zap.L().Debug("stored cache file", zap.String("key", key))
	return nil
}

func (s *LocalStorage) Delete(_ context.Context, key string) error {
	p := s.fullPath(key)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *LocalStorage) Stat(_ context.Context, key string) (*ObjectMeta, error) {
	p := s.fullPath(key)
	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("key not found: %s", key)
		}
		return nil, err
	}
	return &ObjectMeta{
		Key:          key,
		Size:         info.Size(),
		LastModified: info.ModTime(),
	}, nil
}

func (s *LocalStorage) List(_ context.Context, prefix string) ([]ObjectMeta, error) {
	var results []ObjectMeta
	root := s.fullPath(prefix)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.basePath, path)
		if err != nil {
			return err
		}
		results = append(results, ObjectMeta{
			Key:          rel,
			Size:         info.Size(),
			LastModified: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (s *LocalStorage) TotalSize(_ context.Context) (int64, error) {
	var total int64
	err := filepath.Walk(s.basePath, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// Ensure interface compliance at compile time.
var _ Storage = (*LocalStorage)(nil)

// metaPath returns the path for storing content-type metadata.
// Not used yet but reserved for future metadata storage.
func (s *LocalStorage) metaPath(key string) string {
	return s.fullPath(key) + ".meta"
}

// TouchLastModified updates the modification time to track access for LRU.
func (s *LocalStorage) TouchLastModified(key string) error {
	p := s.fullPath(key)
	now := time.Now()
	return os.Chtimes(p, now, now)
}
