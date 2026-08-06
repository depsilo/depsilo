package cache

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

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

	stagingMu            sync.Mutex
	activeStaging        map[string]chan struct{}
	stagingCleanupNeeded bool
}

const (
	localStagingNamespace = ".depsilo-staging-v1"
	localStagingMarker    = "~"
	localStagingChunkSize = 120
)

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
		basePath:      absPath,
		dirMode:       dirMode,
		fileMode:      fileMode,
		private:       private,
		activeStaging: make(map[string]chan struct{}),
	}, nil
}

func validateStorageKey(key string, allowRoot bool) (string, error) {
	if allowRoot && (key == "" || key == ".") {
		return ".", nil
	}
	if key == "" || key == "." {
		return "", fmt.Errorf("%w: key must name an object", ErrInvalidStorageKey)
	}
	if key == localStagingNamespace || strings.HasPrefix(key, localStagingNamespace+"/") {
		return "", fmt.Errorf("%w %q: key uses the reserved staging namespace", ErrInvalidStorageKey, key)
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

// localStagingPath maps every legal object key into a disjoint, reversible
// namespace. Chunking the base64url form avoids filesystem component limits;
// the marker cannot collide with an encoded chunk. Unlike a sibling suffix,
// foo and foo.tmp (or a key and one of its path prefixes) never alias.
func localStagingPath(key string) (string, error) {
	name, err := validateStorageKey(key, false)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(name))
	parts := []string{localStagingNamespace}
	for len(encoded) > localStagingChunkSize {
		parts = append(parts, encoded[:localStagingChunkSize])
		encoded = encoded[localStagingChunkSize:]
	}
	parts = append(parts, encoded, localStagingMarker)
	return strings.Join(parts, "/"), nil
}

func localStagingKey(stagingPath string) (string, bool) {
	prefix := localStagingNamespace + "/"
	suffix := "/" + localStagingMarker
	if !strings.HasPrefix(stagingPath, prefix) || !strings.HasSuffix(stagingPath, suffix) {
		return "", false
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(stagingPath, prefix), suffix)
	chunks := strings.Split(middle, "/")
	if len(chunks) == 0 {
		return "", false
	}
	for index, chunk := range chunks {
		if chunk == "" || len(chunk) > localStagingChunkSize ||
			(index < len(chunks)-1 && len(chunk) != localStagingChunkSize) {
			return "", false
		}
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.Join(chunks, ""))
	if err != nil {
		return "", false
	}
	key, err := validateStorageKey(string(decoded), false)
	if err != nil {
		return "", false
	}
	canonical, err := localStagingPath(key)
	return key, err == nil && canonical == stagingPath
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

func (s *LocalStorage) Put(ctx context.Context, key string, r io.Reader, _ int64, _ string) error {
	name, err := validateStorageKey(key, false)
	if err != nil {
		return err
	}
	stagingPath, err := localStagingPath(name)
	if err != nil {
		return err
	}
	finishStaging, err := s.beginStaging(ctx, name)
	if err != nil {
		return err
	}
	defer finishStaging()

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

	stagingDir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(stagingPath)))
	if err := root.MkdirAll(stagingDir, s.dirMode); err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	if s.private {
		if err := chmodDirectoryChain(root, stagingDir, s.dirMode); err != nil {
			return fmt.Errorf("secure staging directory: %w", err)
		}
	}

	f, err := root.OpenFile(stagingPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, s.fileMode)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	// OpenFile only applies its mode when creating a file. Tighten a leftover
	// temporary file as well before writing any private content into it.
	if s.private {
		if err := f.Chmod(s.fileMode); err != nil {
			f.Close()
			root.Remove(stagingPath)
			return fmt.Errorf("secure temp file: %w", err)
		}
	}

	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		root.Remove(stagingPath)
		return fmt.Errorf("write file: %w", err)
	}
	if err := f.Close(); err != nil {
		root.Remove(stagingPath)
		return fmt.Errorf("close file: %w", err)
	}

	if err := root.Rename(stagingPath, name); err != nil {
		root.Remove(stagingPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	zap.L().Debug("stored cache file", zap.String("key", key))
	return nil
}

func (s *LocalStorage) beginStaging(ctx context.Context, key string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.stagingMu.Lock()
		active := s.activeStaging[key]
		if active == nil {
			done := make(chan struct{})
			s.activeStaging[key] = done
			s.stagingMu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					s.finishStaging(key, done)
				})
			}, nil
		}
		s.stagingMu.Unlock()
		select {
		case <-active:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *LocalStorage) finishStaging(key string, done chan struct{}) {
	s.stagingMu.Lock()
	defer s.stagingMu.Unlock()
	if s.activeStaging[key] != done {
		return
	}
	delete(s.activeStaging, key)
	close(done)
	s.stagingCleanupNeeded = true
	if len(s.activeStaging) != 0 {
		return
	}

	root, err := s.openRoot()
	if err != nil {
		return
	}
	defer root.Close()
	s.cleanupStagingDirectoriesLocked(root)
}

// cleanupStagingDirectoriesLocked runs only while stagingMu is held and no Put
// is active. That invariant lets it remove shared chunk directories without
// racing another writer between MkdirAll and OpenFile. One bounded dirty bit
// replaces a per-key backlog while a long upload keeps the store busy.
func (s *LocalStorage) cleanupStagingDirectoriesLocked(root *os.Root) {
	if len(s.activeStaging) != 0 || !s.stagingCleanupNeeded {
		return
	}

	directories := make([]string, 0)
	err := fs.WalkDir(root.FS(), localStagingNamespace, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() && path != localStagingNamespace {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return
	}
	for index := len(directories) - 1; index >= 0; index-- {
		// Non-empty directories contain an orphan marker and are deliberately
		// retained for the next retention pass.
		_ = root.Remove(directories[index])
	}
	_ = root.Remove(localStagingNamespace)
	s.stagingCleanupNeeded = false
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
			if path == localStagingNamespace {
				return fs.SkipDir
			}
			return nil
		}
		if path == localStagingNamespace {
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
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// ListStaging returns inactive files left in the reserved namespace by an
// interrupted Put. The candidate key is the original object key; internal
// paths never escape this optional capability into Storage.List.
func (s *LocalStorage) ListStaging(ctx context.Context) ([]ObjectMeta, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := s.openRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	staging := make([]ObjectMeta, 0)
	err = fs.WalkDir(root.FS(), localStagingNamespace, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		key, ok := localStagingKey(path)
		if !ok {
			return nil
		}
		s.stagingMu.Lock()
		active := s.activeStaging[key] != nil
		s.stagingMu.Unlock()
		if active {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		staging = append(staging, ObjectMeta{Key: key, Size: info.Size(), LastModified: info.ModTime()})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return staging, nil
}

// RemoveStaging removes one inactive crash remainder. The activity check and
// unlink share stagingMu with Put admission, so a candidate cannot become an
// active writer between the check and deletion.
func (s *LocalStorage) RemoveStaging(ctx context.Context, key string) (bool, error) {
	name, err := validateStorageKey(key, false)
	if err != nil {
		return false, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	stagingPath, err := localStagingPath(name)
	if err != nil {
		return false, err
	}

	s.stagingMu.Lock()
	defer s.stagingMu.Unlock()
	if s.activeStaging[name] != nil {
		return false, nil
	}
	root, err := s.openRoot()
	if err != nil {
		return false, err
	}
	defer root.Close()
	if err := root.Remove(stagingPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	s.stagingCleanupNeeded = true
	s.cleanupStagingDirectoriesLocked(root)
	return true, nil
}

// Ensure interface compliance at compile time.
var _ Storage = (*LocalStorage)(nil)
var _ ReadinessProber = (*LocalStorage)(nil)
var _ StagingObjectStore = (*LocalStorage)(nil)
