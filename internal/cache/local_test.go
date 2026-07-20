package cache

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

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

func TestLocalStorageRejectsSymlinkTempFileEscape(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	storage, err := NewLocalStorage(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "safe"), 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(outside, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "safe", "file.tmp")); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	err = storage.Put(context.Background(), "safe/file", strings.NewReader("overwritten"), 11, "text/plain")
	if err == nil {
		t.Fatal("Put unexpectedly followed a temporary-file symlink outside the storage root")
	}
	got, readErr := os.ReadFile(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "original" {
		t.Fatalf("outside temp target changed to %q", got)
	}
}
