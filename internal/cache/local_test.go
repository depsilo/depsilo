package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

type firstReadGate struct {
	reader  io.Reader
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (reader *firstReadGate) Read(buffer []byte) (int, error) {
	reader.once.Do(func() {
		close(reader.started)
		<-reader.release
	})
	return reader.reader.Read(buffer)
}

func closeTestSignal(signal chan struct{}) {
	select {
	case <-signal:
	default:
		close(signal)
	}
}

func TestLocalStorageLifecycle(t *testing.T) {
	t.Parallel()

	storage, err := NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const key = "npm/@scope/package/index.json"
	const payload = `{"name":"@scope/package"}`

	if err := storage.Put(ctx, key, strings.NewReader(payload), int64(len(payload)), "application/json"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	exists, err := storage.Exists(ctx, key)
	if err != nil || !exists {
		t.Fatalf("Exists = %v, %v; want true, nil", exists, err)
	}

	reader, size, err := storage.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read = %v, close = %v", readErr, closeErr)
	}
	if string(got) != payload || size != int64(len(payload)) {
		t.Fatalf("Get = %q (%d bytes), want %q (%d bytes)", got, size, payload, len(payload))
	}

	meta, err := storage.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if meta.Key != key || meta.Size != int64(len(payload)) {
		t.Fatalf("Stat = %+v", meta)
	}

	objects, err := storage.List(ctx, "npm")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objects) != 1 || objects[0].Key != key {
		t.Fatalf("List = %+v, want key %q", objects, key)
	}
	allObjects, err := storage.List(ctx, "")
	if err != nil {
		t.Fatalf("List root: %v", err)
	}
	if len(allObjects) != 1 || allObjects[0].Key != key {
		t.Fatalf("List root = %+v, want key %q", allObjects, key)
	}

	total, err := storage.TotalSize(ctx)
	if err != nil || total != int64(len(payload)) {
		t.Fatalf("TotalSize = %d, %v; want %d, nil", total, err, len(payload))
	}
	if err := storage.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, err = storage.Exists(ctx, key)
	if err != nil || exists {
		t.Fatalf("Exists after Delete = %v, %v; want false, nil", exists, err)
	}
}

func TestLocalStorageConcurrentPrefixKeysDoNotShareStaging(t *testing.T) {
	storage, err := NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}

	gate := &firstReadGate{
		reader:  strings.NewReader("plain payload"),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	plainDone := make(chan error, 1)
	go func() {
		plainDone <- storage.Put(context.Background(), "foo", gate, 13, "text/plain")
	}()
	<-gate.started

	if err := storage.Put(context.Background(), "foo.tmp", strings.NewReader("tmp payload"), 11, "text/plain"); err != nil {
		close(gate.release)
		t.Fatalf("Put foo.tmp: %v", err)
	}
	close(gate.release)
	if err := <-plainDone; err != nil {
		t.Fatalf("Put foo: %v", err)
	}

	for key, want := range map[string]string{
		"foo":     "plain payload",
		"foo.tmp": "tmp payload",
	} {
		reader, _, err := storage.Get(context.Background(), key)
		if err != nil {
			t.Fatalf("Get %s: %v", key, err)
		}
		got, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read %s = %v, close = %v", key, readErr, closeErr)
		}
		if string(got) != want {
			t.Fatalf("Get %s = %q, want %q", key, got, want)
		}
	}
}

func TestLocalStorageCleansSharedLongKeyStagingDirectoriesAfterConcurrentPuts(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	storage, err := NewLocalStorage(base)
	if err != nil {
		t.Fatal(err)
	}
	commonPrefix := strings.Repeat("shared-prefix-", 8)
	firstKey := commonPrefix + "first"
	secondKey := commonPrefix + "second"
	firstStaging, err := localStagingPath(firstKey)
	if err != nil {
		t.Fatal(err)
	}
	secondStaging, err := localStagingPath(secondKey)
	if err != nil {
		t.Fatal(err)
	}
	firstParts := strings.Split(firstStaging, "/")
	secondParts := strings.Split(secondStaging, "/")
	if len(firstParts) < 4 || len(secondParts) < 4 || firstParts[1] != secondParts[1] {
		t.Fatalf("staging paths do not share a chunk directory: %q, %q", firstStaging, secondStaging)
	}

	firstGate := &firstReadGate{
		reader:  strings.NewReader("first payload"),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	secondGate := &firstReadGate{
		reader:  strings.NewReader("second payload"),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	defer closeTestSignal(firstGate.release)
	defer closeTestSignal(secondGate.release)
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() {
		firstDone <- storage.Put(context.Background(), firstKey, firstGate, 13, "text/plain")
	}()
	go func() {
		secondDone <- storage.Put(context.Background(), secondKey, secondGate, 14, "text/plain")
	}()
	<-firstGate.started
	<-secondGate.started

	closeTestSignal(firstGate.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Put: %v", err)
	}
	secondStagingPath := filepath.Join(base, filepath.FromSlash(secondStaging))
	if _, err := os.Stat(secondStagingPath); err != nil {
		t.Fatalf("second active staging file was disturbed: %v", err)
	}

	closeTestSignal(secondGate.release)
	if err := <-secondDone; err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, localStagingNamespace)); !os.IsNotExist(err) {
		t.Fatalf("empty staging namespace remains after concurrent Puts: %v", err)
	}
}

func TestLocalStorageStagingDirectoryInodesDoNotAccumulate(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	storage, err := NewLocalStorage(base)
	if err != nil {
		t.Fatal(err)
	}

	for index := 0; index < 100; index++ {
		key := fmt.Sprintf("artifact-%03d", index)
		if err := storage.Put(context.Background(), key, strings.NewReader("x"), 1, "application/octet-stream"); err != nil {
			t.Fatalf("Put %q: %v", key, err)
		}
		if err := storage.Delete(context.Background(), key); err != nil {
			t.Fatalf("Delete %q: %v", key, err)
		}
	}
	if _, err := os.Stat(filepath.Join(base, localStagingNamespace)); !os.IsNotExist(err) {
		t.Fatalf("staging directory inodes accumulated after sequential Puts: %v", err)
	}
}

func TestLocalStorageStagingCleanupStateStaysBoundedDuringLongPut(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	storage, err := NewLocalStorage(base)
	if err != nil {
		t.Fatal(err)
	}

	gate := &firstReadGate{
		reader:  strings.NewReader("long payload"),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	longDone := make(chan error, 1)
	go func() {
		longDone <- storage.Put(context.Background(), "long-running", gate, 12, "application/octet-stream")
	}()
	<-gate.started
	released := false
	defer func() {
		if !released {
			close(gate.release)
		}
	}()

	for index := 0; index < 100; index++ {
		key := fmt.Sprintf("short-%03d", index)
		if err := storage.Put(context.Background(), key, strings.NewReader("x"), 1, "application/octet-stream"); err != nil {
			t.Fatalf("Put %q: %v", key, err)
		}
	}
	storage.stagingMu.Lock()
	activeCount := len(storage.activeStaging)
	cleanupNeeded := storage.stagingCleanupNeeded
	storage.stagingMu.Unlock()
	if activeCount != 1 || !cleanupNeeded {
		t.Fatalf("staging state while long Put is active = active:%d cleanup:%v, want 1,true", activeCount, cleanupNeeded)
	}

	close(gate.release)
	released = true
	if err := <-longDone; err != nil {
		t.Fatalf("long Put: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, localStagingNamespace)); !os.IsNotExist(err) {
		t.Fatalf("staging namespace remains after the final active Put: %v", err)
	}
}

func TestPrivateLocalStoragePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix owner/group/other permission bits")
	}

	base := filepath.Join(t.TempDir(), "compile-cache")
	objectDir := filepath.Join(base, "v1", "team", "ab")
	if err := os.MkdirAll(objectDir, 0777); err != nil {
		t.Fatal(err)
	}
	// Make the pre-existing modes deterministic regardless of the test
	// process's umask. The private storage must tighten every directory it
	// uses, including a pre-created root and object hierarchy.
	for _, dir := range []string{
		base,
		filepath.Join(base, "v1"),
		filepath.Join(base, "v1", "team"),
		objectDir,
	} {
		if err := os.Chmod(dir, 0777); err != nil {
			t.Fatal(err)
		}
	}

	const key = "v1/team/ab/artifact"
	neighborPath := filepath.Join(base, filepath.FromSlash(key+".tmp"))
	if err := os.WriteFile(neighborPath, []byte("neighbor"), 0600); err != nil {
		t.Fatal(err)
	}

	storage, err := NewPrivateLocalStorage(base)
	if err != nil {
		t.Fatal(err)
	}
	assertFilePermissions(t, base, 0700)

	const payload = "private compiler artifact"
	if err := storage.Put(context.Background(), key, strings.NewReader(payload), int64(len(payload)), "application/octet-stream"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	for _, dir := range []string{
		filepath.Join(base, "v1"),
		filepath.Join(base, "v1", "team"),
		objectDir,
	} {
		assertFilePermissions(t, dir, 0700)
	}
	assertFilePermissions(t, filepath.Join(base, filepath.FromSlash(key)), 0600)
	neighbor, err := os.ReadFile(neighborPath)
	if err != nil {
		t.Fatalf("read neighboring .tmp object: %v", err)
	}
	if string(neighbor) != "neighbor" {
		t.Fatalf("neighboring .tmp object changed to %q", neighbor)
	}
	assertFilePermissions(t, neighborPath, 0600)
}

func TestPrivateLocalStorageStagingPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix owner/group/other permission bits")
	}

	base := filepath.Join(t.TempDir(), "compile-cache")
	storage, err := NewPrivateLocalStorage(base)
	if err != nil {
		t.Fatal(err)
	}
	const key = "v1/team/private-artifact"
	gate := &firstReadGate{
		reader:  strings.NewReader("private payload"),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	putDone := make(chan error, 1)
	go func() {
		putDone <- storage.Put(context.Background(), key, gate, 15, "application/octet-stream")
	}()
	<-gate.started
	released := false
	defer func() {
		if !released {
			close(gate.release)
		}
	}()

	stagingKey, err := localStagingPath(key)
	if err != nil {
		t.Fatal(err)
	}
	stagingPath := filepath.Join(base, filepath.FromSlash(stagingKey))
	assertFilePermissions(t, stagingPath, 0600)
	for dir := filepath.Dir(stagingPath); dir != base; dir = filepath.Dir(dir) {
		assertFilePermissions(t, dir, 0700)
	}
	assertFilePermissions(t, base, 0700)

	close(gate.release)
	released = true
	if err := <-putDone; err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Fatalf("staging file still exists after publication: %v", err)
	}
}

func assertFilePermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("permissions for %s = %04o, want %04o", path, got, want)
	}
}

func TestLocalStorageRejectsNonCanonicalKeys(t *testing.T) {
	t.Parallel()

	storage, err := NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	keys := []string{
		"",
		".",
		"..",
		"../escape",
		"nested/../../escape",
		"nested/../escape",
		"./nested/file",
		"nested//file",
		"nested/file/",
		"/absolute/path",
		`\absolute\path`,
		`nested\..\escape`,
		`\\server\share\file`,
		"C:/windows/path",
		`C:\windows\path`,
		localStagingNamespace,
		localStagingNamespace + "/object",
	}

	operations := []struct {
		name string
		run  func(string) error
	}{
		{name: "Exists", run: func(key string) error { _, err := storage.Exists(ctx, key); return err }},
		{name: "Get", run: func(key string) error { _, _, err := storage.Get(ctx, key); return err }},
		{name: "Put", run: func(key string) error {
			return storage.Put(ctx, key, strings.NewReader("payload"), 7, "text/plain")
		}},
		{name: "Delete", run: func(key string) error { return storage.Delete(ctx, key) }},
		{name: "Stat", run: func(key string) error { _, err := storage.Stat(ctx, key); return err }},
	}

	for _, operation := range operations {
		operation := operation
		t.Run(operation.name, func(t *testing.T) {
			for _, key := range keys {
				key := key
				t.Run(key, func(t *testing.T) {
					if err := operation.run(key); !errors.Is(err, ErrInvalidStorageKey) {
						t.Fatalf("error = %v, want ErrInvalidStorageKey", err)
					}
				})
			}
		})
	}

	// List allows an empty prefix to mean the root, but all non-canonical
	// non-empty prefixes are rejected just like object keys.
	for _, prefix := range keys[2:] {
		if _, err := storage.List(ctx, prefix); !errors.Is(err, ErrInvalidStorageKey) {
			t.Errorf("List(%q) error = %v, want ErrInvalidStorageKey", prefix, err)
		}
	}
}

func TestLocalStorageRejectsSymlinkEscape(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	storage, err := NewLocalStorage(base)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	secretPath := filepath.Join(outside, "secret.txt")
	const secret = "must remain outside the cache"
	if err := os.WriteFile(secretPath, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "escape")); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	ctx := context.Background()
	operations := []struct {
		name string
		run  func() error
	}{
		{name: "Exists", run: func() error { _, err := storage.Exists(ctx, "escape/secret.txt"); return err }},
		{name: "Get", run: func() error { _, _, err := storage.Get(ctx, "escape/secret.txt"); return err }},
		{name: "Put", run: func() error {
			return storage.Put(ctx, "escape/new.txt", strings.NewReader("escaped"), 7, "text/plain")
		}},
		{name: "Delete", run: func() error { return storage.Delete(ctx, "escape/secret.txt") }},
		{name: "Stat", run: func() error { _, err := storage.Stat(ctx, "escape/secret.txt"); return err }},
		{name: "List", run: func() error { _, err := storage.List(ctx, "escape"); return err }},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.run(); err == nil {
				t.Fatal("operation unexpectedly followed a symlink outside the storage root")
			}
		})
	}

	got, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("outside file was removed: %v", err)
	}
	if string(got) != secret {
		t.Fatalf("outside file changed to %q", got)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file was created, Stat error = %v", err)
	}

	objects, err := storage.List(ctx, "")
	if err != nil {
		t.Fatalf("List root: %v", err)
	}
	keys := make([]string, 0, len(objects))
	for _, object := range objects {
		keys = append(keys, object.Key)
	}
	if !slices.Equal(keys, []string{"escape"}) {
		t.Fatalf("List root keys = %q, want only the symlink itself", keys)
	}
}

func TestLocalStorageRejectsSymlinkStagingNamespaceEscape(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	storage, err := NewLocalStorage(base)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim")
	if err := os.WriteFile(victim, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, localStagingNamespace)); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	err = storage.Put(context.Background(), "safe/file", strings.NewReader("overwritten"), 11, "text/plain")
	if err == nil {
		t.Fatal("Put unexpectedly followed the staging-namespace symlink outside the storage root")
	}
	got, readErr := os.ReadFile(victim)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "original" {
		t.Fatalf("outside target changed to %q", got)
	}
}
