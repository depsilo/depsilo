//go:build linux || darwin

package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestHoldDatabaseRejectsWritableSharedParent(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	_, err := HoldDatabase(filepath.Join(shared, "depsilo.db"))
	if err == nil || !strings.Contains(err.Error(), "group/world-writable") {
		t.Fatalf("HoldDatabase() error = %v, want writable-parent rejection", err)
	}
}

func TestDatabaseLeaseDetectsLockInodeReplacement(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(root, "depsilo.db")
	closer, err := HoldDatabase(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	lease := closer.(*databaseLease)
	defer lease.Close()
	if err := os.Remove(lease.lockPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lease.lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lease.validate(); err == nil {
		t.Fatal("database lease accepted a replaced lock inode")
	}
	replacement, err := HoldDatabase(databasePath)
	if err == nil {
		_ = replacement.Close()
	} else if !errors.Is(err, ErrDatabaseInUse) {
		t.Fatalf("replacement lock acquisition returned unexpected error: %v", err)
	}
}

type ownedDirectory struct {
	os.FileInfo
	uid, gid uint32
}

func (directory ownedDirectory) Sys() any {
	return &syscall.Stat_t{Uid: directory.uid, Gid: directory.gid}
}

// An operator who runs restore or reset-password with sudo must not leave
// lease entries the service account cannot open.
func TestNewLeaseEntryOwnerIsTheDirectoryOwnerOnlyForRoot(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name                       string
		effectiveUID               int
		directoryUID, directoryGID uint32
		wantUID, wantGID           int
		wantChange                 bool
	}{
		{name: "root in the service account's directory", effectiveUID: 0, directoryUID: 10001, directoryGID: 10002, wantUID: 10001, wantGID: 10002, wantChange: true},
		{name: "root in its own directory", effectiveUID: 0, directoryUID: 0, directoryGID: 10002},
		{name: "the service account itself", effectiveUID: 10001, directoryUID: 10001, directoryGID: 10002},
		{name: "another unprivileged account", effectiveUID: 501, directoryUID: 10001, directoryGID: 10002},
	} {
		t.Run(test.name, func(t *testing.T) {
			uid, gid, change := newLeaseEntryOwner(test.effectiveUID, ownedDirectory{FileInfo: info, uid: test.directoryUID, gid: test.directoryGID})
			if uid != test.wantUID || gid != test.wantGID || change != test.wantChange {
				t.Fatalf("newLeaseEntryOwner = (%d, %d, %v), want (%d, %d, %v)", uid, gid, change, test.wantUID, test.wantGID, test.wantChange)
			}
		})
	}
}

// recordedLeaseOwnership stands in for root in a directory that belongs to
// uid 10001, gid 10002, and records what would change owner.
func recordedLeaseOwnership(t *testing.T, databaseDirectory string, given *[]string) leaseOwnership {
	t.Helper()
	want, err := os.Stat(databaseDirectory)
	if err != nil {
		t.Fatal(err)
	}
	return leaseOwnership{
		newEntryOwner: func(directory os.FileInfo) (int, int, bool) {
			if !os.SameFile(directory, want) {
				t.Errorf("the owner was read from %q, not from the database directory", directory.Name())
			}
			return 10001, 10002, true
		},
		chown: func(entry *os.File, uid, gid int) error {
			named, err := os.Lstat(entry.Name())
			if err != nil {
				return err
			}
			opened, err := entry.Stat()
			if err != nil {
				return err
			}
			if named.Mode()&os.ModeSymlink != 0 || !os.SameFile(named, opened) {
				t.Errorf("%q is not the entry that was opened", entry.Name())
			}
			*given = append(*given, fmt.Sprintf("%s %d:%d", entry.Name(), uid, gid))
			return nil
		},
	}
}

func TestHoldDatabaseGivesOnlyEntriesItCreatedToTheDirectoryOwner(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	var given []string
	ownership := recordedLeaseOwnership(t, root, &given)
	hold := func(database string) *databaseLease {
		t.Helper()
		closer, err := holdDatabase(filepath.Join(root, database), ownership)
		if err != nil {
			t.Fatal(err)
		}
		lease := closer.(*databaseLease)
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
		return lease
	}

	first := hold("depsilo.db")
	lockDirectory := filepath.Join(root, ".depsilo-locks")
	want := []string{lockDirectory + " 10001:10002", first.lockPath + " 10001:10002"}
	if strings.Join(given, "\n") != strings.Join(want, "\n") {
		t.Fatalf("first acquisition gave away %q, want %q", given, want)
	}

	given = nil
	hold("depsilo.db")
	if len(given) != 0 {
		t.Fatalf("an acquisition that created nothing gave away %q", given)
	}

	// An existing directory keeps its owner; only the new file changes hands.
	second := hold("other.db")
	if len(given) != 1 || given[0] != second.lockPath+" 10001:10002" {
		t.Fatalf("an acquisition that created only the file gave away %q", given)
	}

	// Without the rule (not root, or root in its own directory) nothing does.
	given = nil
	ownership.newEntryOwner = func(os.FileInfo) (int, int, bool) { return 0, 0, false }
	hold("third.db")
	if len(given) != 0 {
		t.Fatalf("an acquisition outside the rule gave away %q", given)
	}
}

func TestHoldDatabaseNeverGivesAwayWhatASymbolicLinkNames(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	var given []string
	ownership := recordedLeaseOwnership(t, root, &given)

	if err := os.Symlink(elsewhere, filepath.Join(root, ".depsilo-locks")); err != nil {
		t.Fatal(err)
	}
	lease, err := holdDatabase(filepath.Join(root, "depsilo.db"), ownership)
	if err == nil {
		_ = lease.Close()
		t.Fatal("a symbolic link was accepted as the lease directory")
	}
	if !strings.Contains(err.Error(), "must be a real directory") {
		t.Fatalf("holdDatabase() error = %v, want the rejection of a lease directory that is a link", err)
	}
	if len(given) != 0 {
		t.Fatalf("a symbolic link at the lease directory changed an owner: %q", given)
	}
	if err := os.Remove(filepath.Join(root, ".depsilo-locks")); err != nil {
		t.Fatal(err)
	}

	closer, err := holdDatabase(filepath.Join(root, "depsilo.db"), ownership)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := closer.(*databaseLease).lockPath
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	given = nil
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(elsewhere, "target")
	if err := os.Symlink(target, lockPath); err != nil {
		t.Fatal(err)
	}
	lease, err = holdDatabase(filepath.Join(root, "depsilo.db"), ownership)
	if err == nil {
		_ = lease.Close()
		t.Fatal("a symbolic link was accepted as the lease file")
	}
	if !strings.Contains(err.Error(), "open database lease") {
		t.Fatalf("holdDatabase() error = %v, want a failure to open the lease file", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the lease file was created through a symbolic link: %v", err)
	}
	if len(given) != 0 {
		t.Fatalf("an entry behind a symbolic link changed owner: %q", given)
	}
}

// Creating the lease directory does not open it. The owner of the database
// directory can move another entry to that name before it is opened to change
// its owner; only the empty directory this call created may change hands.
func TestHoldDatabaseGivesAwayOnlyTheDirectoryItCreated(t *testing.T) {
	for _, test := range []struct {
		name      string
		replace   func(t *testing.T, lockDirectory, elsewhere string)
		wantError string
	}{
		{
			name: "a directory with content",
			replace: func(t *testing.T, lockDirectory, elsewhere string) {
				if err := os.Mkdir(lockDirectory, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(lockDirectory, "secret"), []byte("secret"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: "changed while acquiring it: it is not empty",
		},
		{
			name: "a symbolic link to a directory",
			replace: func(t *testing.T, lockDirectory, elsewhere string) {
				if err := os.Symlink(elsewhere, lockDirectory); err != nil {
					t.Fatal(err)
				}
			},
			wantError: "open new database lease directory",
		},
		{
			name: "a regular file",
			replace: func(t *testing.T, lockDirectory, elsewhere string) {
				if err := os.WriteFile(lockDirectory, []byte("secret"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: "open new database lease directory",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			elsewhere := t.TempDir()
			lockDirectory := filepath.Join(root, ".depsilo-locks")
			var given []string
			ownership := recordedLeaseOwnership(t, root, &given)
			// The owner is decided after the directory is created and before
			// it is opened: that is the moment of the replacement.
			decide := ownership.newEntryOwner
			replaced := false
			ownership.newEntryOwner = func(directory os.FileInfo) (int, int, bool) {
				if !replaced {
					replaced = true
					if err := os.Rename(lockDirectory, filepath.Join(root, "moved")); err != nil {
						t.Fatal(err)
					}
					test.replace(t, lockDirectory, elsewhere)
				}
				return decide(directory)
			}
			before, err := os.Lstat(elsewhere)
			if err != nil {
				t.Fatal(err)
			}

			lease, err := holdDatabase(filepath.Join(root, "depsilo.db"), ownership)
			if err == nil {
				_ = lease.Close()
				t.Fatal("an entry this call did not create was accepted as its new lease directory")
			}
			if !strings.Contains(err.Error(), "acquire database lease") || !strings.Contains(err.Error(), test.wantError) || !strings.Contains(err.Error(), fmt.Sprintf("%q", lockDirectory)) {
				t.Fatalf("holdDatabase() error = %v, want a lease failure with %q that names %s", err, test.wantError, lockDirectory)
			}
			if len(given) != 0 {
				t.Fatalf("an entry this call did not create changed owner: %q", given)
			}
			// What was put there is not removed either.
			if _, err := os.Lstat(lockDirectory); err != nil {
				t.Fatalf("the entry put at the lease directory's name: %v", err)
			}
			if after, err := os.Lstat(elsewhere); err != nil || !os.SameFile(before, after) {
				t.Fatalf("the directory the link names changed: %v", err)
			}
		})
	}
}

func TestNewLockDirectoryBelongsToItsCreatorAndIsEmpty(t *testing.T) {
	path := t.TempDir()
	open := func() *os.File {
		t.Helper()
		directory, err := openLockDirectory(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = directory.Close() })
		return directory
	}
	if err := newLockDirectoryProblem(os.Geteuid(), open()); err != nil {
		t.Fatalf("an empty directory of the creator's: %v", err)
	}
	if err := newLockDirectoryProblem(os.Geteuid()+1, open()); err == nil || !strings.Contains(err.Error(), "belongs to uid") {
		t.Fatalf("a directory of another user's: %v, want a refusal that names its owner", err)
	}
	if err := os.WriteFile(filepath.Join(path, "entry"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := newLockDirectoryProblem(os.Geteuid(), open()); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("a directory with content: %v, want a refusal", err)
	}
}

// A new entry that cannot be handed over must not stay behind: the next call
// would take it for an existing entry, leave its owner alone and succeed.
func TestHoldDatabaseRemovesANewEntryItCannotGiveToTheDirectoryOwner(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	var given []string
	ownership := recordedLeaseOwnership(t, root, &given)
	record := ownership.chown
	refuse := errors.New("operation not permitted")
	lockDirectory := filepath.Join(root, ".depsilo-locks")
	entries := func() []string {
		t.Helper()
		names, err := filepath.Glob(filepath.Join(lockDirectory, "*"))
		if err != nil {
			t.Fatal(err)
		}
		return names
	}

	ownership.chown = func(*os.File, int, int) error { return refuse }
	for attempt := 1; attempt <= 2; attempt++ {
		_, err = holdDatabase(filepath.Join(root, "depsilo.db"), ownership)
		if !errors.Is(err, refuse) || !strings.Contains(err.Error(), "acquire database lease") || !strings.Contains(err.Error(), fmt.Sprintf("%q", lockDirectory)) {
			t.Fatalf("attempt %d: holdDatabase() error = %v, want a lease failure that names %s", attempt, err, lockDirectory)
		}
		if _, err := os.Lstat(lockDirectory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("attempt %d: the lease directory that could not be handed over was left behind: %v", attempt, err)
		}
	}

	// The same for the file, in a directory that was handed over.
	ownership.chown = func(entry *os.File, uid, gid int) error {
		if entry.Name() == lockDirectory {
			return record(entry, uid, gid)
		}
		return refuse
	}
	for attempt := 1; attempt <= 2; attempt++ {
		_, err = holdDatabase(filepath.Join(root, "depsilo.db"), ownership)
		if !errors.Is(err, refuse) || !strings.Contains(err.Error(), "acquire database lease") || !strings.Contains(err.Error(), lockDirectory+string(filepath.Separator)) || !strings.Contains(err.Error(), ".lock") {
			t.Fatalf("attempt %d: holdDatabase() error = %v, want a lease failure that names the lease file", attempt, err)
		}
		if left := entries(); len(left) != 0 {
			t.Fatalf("attempt %d: the lease file that could not be handed over was left behind: %q", attempt, left)
		}
	}
	if len(given) != 1 || given[0] != lockDirectory+" 10001:10002" {
		t.Fatalf("handed over %q, want the lease directory once", given)
	}

	// Once the owner can be changed, the acquisition works as usual.
	given = nil
	ownership.chown = record
	lease, err := holdDatabase(filepath.Join(root, "depsilo.db"), ownership)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if left := entries(); len(given) != 1 || len(left) != 1 || given[0] != left[0]+" 10001:10002" {
		t.Fatalf("handed over %q of the entries %q, want the one new lease file", given, left)
	}
}

// The real thing. It needs root, and no automated gate runs the suite as
// root, so it is skipped there: it is a manual check for a change to the
// ownership rule. Build the test binary for Linux (GOOS=linux CGO_ENABLED=0
// go test -c ./internal/backup) and run it as root in a container, for
// example: docker run --rm --network none -u 0 -v "$PWD":/t:ro alpine:3.20
// /t/backup.test -test.run AsRoot -test.v
func TestHoldDatabaseAsRootLeavesEntriesTheDirectoryOwnerCanUse(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(root, 10001, 10002); err != nil {
		t.Fatal(err)
	}
	owner := func(path string) string {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		return fmt.Sprintf("%d:%d", stat.Uid, stat.Gid)
	}
	hold := func(database string) string {
		t.Helper()
		closer, err := HoldDatabase(filepath.Join(root, database))
		if err != nil {
			t.Fatal(err)
		}
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
		return closer.(*databaseLease).lockPath
	}

	lockPath := hold("depsilo.db")
	lockDirectory := filepath.Join(root, ".depsilo-locks")
	if owner(lockDirectory) != "10001:10002" || owner(lockPath) != "10001:10002" {
		t.Fatalf("lease directory %s, lease file %s, want 10001:10002 for both", owner(lockDirectory), owner(lockPath))
	}

	// Existing entries are left alone, whoever owns them.
	if err := os.Chown(lockDirectory, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(lockPath, 0, 0); err != nil {
		t.Fatal(err)
	}
	hold("depsilo.db")
	otherLockPath := hold("other.db")
	if owner(lockDirectory) != "0:0" || owner(lockPath) != "0:0" || owner(otherLockPath) != "10001:10002" {
		t.Fatalf("lease directory %s, existing lease file %s, new lease file %s, want 0:0, 0:0, 10001:10002",
			owner(lockDirectory), owner(lockPath), owner(otherLockPath))
	}

	// Another directory of root's, moved to the name of the new lease
	// directory before it is opened, is not given away.
	if err := os.RemoveAll(lockDirectory); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "secret"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	ownership := processLeaseOwnership
	ownership.newEntryOwner = func(directory os.FileInfo) (int, int, bool) {
		if err := os.Remove(lockDirectory); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(private, lockDirectory); err != nil {
			t.Fatal(err)
		}
		return processNewLeaseEntryOwner(directory)
	}
	lease, err := holdDatabase(filepath.Join(root, "depsilo.db"), ownership)
	if err == nil {
		_ = lease.Close()
		t.Fatal("a directory this call did not create was accepted as its new lease directory")
	}
	if !strings.Contains(err.Error(), "changed while acquiring it") {
		t.Fatalf("holdDatabase() error = %v, want the refusal of a replaced lease directory", err)
	}
	if owner(lockDirectory) != "0:0" || owner(filepath.Join(lockDirectory, "secret")) != "0:0" {
		t.Fatalf("replaced lease directory %s, its content %s, want 0:0 for both",
			owner(lockDirectory), owner(filepath.Join(lockDirectory, "secret")))
	}
}
