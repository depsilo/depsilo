package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// ErrInvalidStorageKey is returned when a local-storage key is not a
// canonical, slash-separated relative path.
var ErrInvalidStorageKey = errors.New("invalid local storage key")

type LocalStorage struct {
	basePath string
	dirMode  fs.FileMode
	fileMode fs.FileMode
	private  bool
}

func NewLocalStorage(basePath string) (*LocalStorage, error) {
	return newLocalStorage(basePath, 0755, 0666, false)
}

// NewPrivateLocalStorage creates a local storage root suitable for artifacts
// that must not be readable by other users on the host. Unlike
// NewLocalStorage, it enforces 0700 on the root and newly used directories and
// 0600 on newly written files.
func NewPrivateLocalStorage(basePath string) (*LocalStorage, error) {
	return newLocalStorage(basePath, 0700, 0600, true)
}

func newLocalStorage(basePath string, dirMode, fileMode fs.FileMode, private bool) (*LocalStorage, error) {
	absPath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("resolve storage directory: %w", err)
	}
	if err := os.MkdirAll(absPath, dirMode); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}
	// MkdirAll does not alter an existing directory. A private storage root
	// therefore needs an explicit chmod so a pre-created root cannot retain
	// group/world access.
	if private {
		if err := os.Chmod(absPath, dirMode); err != nil {
			return nil, fmt.Errorf("secure storage directory: %w", err)
		}
	}
	root, err := os.OpenRoot(absPath)
	if err != nil {
		return nil, fmt.Errorf("open storage directory: %w", err)
	}
	if err := root.Close(); err != nil {
		return nil, fmt.Errorf("close storage directory: %w", err)
	}
	return &LocalStorage{
		basePath: absPath,
		dirMode:  dirMode,
		fileMode: fileMode,
		private:  private,
	}, nil
}

func validateStorageKey(key string, allowRoot bool) (string, error) {
	if allowRoot && (key == "" || key == ".") {
		return ".", nil
	}
	if key == "" || key == "." {
		return "", fmt.Errorf("%w: key must name an object", ErrInvalidStorageKey)
	}
	// io/fs paths always use '/'. Rejecting '\\' keeps validation identical
	// on Unix and Windows instead of letting a key change meaning by platform.
	if strings.ContainsRune(key, '\\') {
		return "", fmt.Errorf("%w %q: backslashes are not allowed", ErrInvalidStorageKey, key)
	}
	// fs.ValidPath deliberately accepts colons. Reject a Windows drive prefix
	// explicitly so C:/... cannot be relative on Unix and absolute on Windows.
	if len(key) >= 2 && isASCIILetter(key[0]) && key[1] == ':' {
		return "", fmt.Errorf("%w %q: volume-qualified paths are not allowed", ErrInvalidStorageKey, key)
	}
	if !fs.ValidPath(key) {
		return "", fmt.Errorf("%w %q: use a canonical relative path", ErrInvalidStorageKey, key)
	}
	return key, nil
}

func isASCIILetter(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func (s *LocalStorage) openRoot() (*os.Root, error) {
	root, err := os.OpenRoot(s.basePath)
	if err != nil {
		return nil, fmt.Errorf("open storage directory: %w", err)
	}
	return root, nil
}

// CheckReady verifies the same root-directory capability needed by cache-hit
// reads without walking the cache tree.
func (s *LocalStorage) CheckReady(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := s.openRoot()
	if err != nil {
		return err
	}
	closeErr := root.Close()
	if err := ctx.Err(); err != nil {
		return errors.Join(closeErr, err)
	}
	return closeErr
}

func (s *LocalStorage) Exists(_ context.Context, key string) (bool, error) {
	name, err := validateStorageKey(key, false)
	if err != nil {
		return false, err
	}
	root, err := s.openRoot()
	if err != nil {
		return false, err
	}
	defer root.Close()

	_, err = root.Stat(name)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *LocalStorage) Get(_ context.Context, key string) (io.ReadCloser, int64, error) {
	name, err := validateStorageKey(key, false)
	if err != nil {
		return nil, 0, err
	}
	root, err := s.openRoot()
	if err != nil {
		return nil, 0, err
	}

	f, err := root.Open(name)
	if err != nil {
		root.Close()
		if os.IsNotExist(err) {
			return nil, 0, fmt.Errorf("key not found: %s", key)
		}
		return nil, 0, err
	}
	if err := root.Close(); err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("close storage directory: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	name, err := validateStorageKey(key, false)
	if err != nil {
		return err
	}
	root, err := s.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(name)))
	if err := root.MkdirAll(dir, s.dirMode); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	if s.private {
		if err := chmodDirectoryChain(root, dir, s.dirMode); err != nil {
			return fmt.Errorf("secure directory: %w", err)
		}
	}

	tmp := name + ".tmp"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, s.fileMode)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	// OpenFile only applies its mode when creating a file. Tighten a leftover
	// temporary file as well before writing any private content into it.
	if s.private {
		if err := f.Chmod(s.fileMode); err != nil {
			f.Close()
			root.Remove(tmp)
			return fmt.Errorf("secure temp file: %w", err)
		}
	}

	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		root.Remove(tmp)
		return fmt.Errorf("write file: %w", err)
	}
	if err := f.Close(); err != nil {
		root.Remove(tmp)
		return fmt.Errorf("close file: %w", err)
	}

	if err := root.Rename(tmp, name); err != nil {
		root.Remove(tmp)
		return fmt.Errorf("rename temp file: %w", err)
	}

	zap.L().Debug("stored cache file", zap.String("key", key))
	return nil
}

func chmodDirectoryChain(root *os.Root, dir string, mode fs.FileMode) error {
	if dir == "." {
		return nil
	}
	current := ""
	for _, element := range strings.Split(dir, "/") {
		if current == "" {
			current = element
		} else {
			current += "/" + element
		}
		if err := root.Chmod(current, mode); err != nil {
			return err
		}
	}
	return nil
}

func (s *LocalStorage) Delete(_ context.Context, key string) error {
	name, err := validateStorageKey(key, false)
	if err != nil {
		return err
	}
	root, err := s.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()

	if err := root.Remove(name); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *LocalStorage) Stat(_ context.Context, key string) (*ObjectMeta, error) {
	name, err := validateStorageKey(key, false)
	if err != nil {
		return nil, err
	}
	root, err := s.openRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	info, err := root.Stat(name)
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
	name, err := validateStorageKey(prefix, true)
	if err != nil {
		return nil, err
	}
	root, err := s.openRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	err = fs.WalkDir(root.FS(), name, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		results = append(results, ObjectMeta{
			Key:          path,
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
	root, err := s.openRoot()
	if err != nil {
		return 0, err
	}
	defer root.Close()

	var total int64
	err = fs.WalkDir(root.FS(), ".", func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// Ensure interface compliance at compile time.
var _ Storage = (*LocalStorage)(nil)
var _ ReadinessProber = (*LocalStorage)(nil)
