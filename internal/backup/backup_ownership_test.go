//go:build linux || darwin

package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// backupOwnershipState writes an instance to back up: state/config.toml and
// state/data/depsilo.db.
func backupOwnershipState(t *testing.T) (root, state, data string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	state = filepath.Join(root, "state")
	data = filepath.Join(state, "data")
	for _, directory := range []string{state, data} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteRecoveryFile(t, filepath.Join(state, "config.toml"), ownershipTestConfig)
	mustWriteRecoveryDatabase(t, filepath.Join(data, "depsilo.db"), "backed up")
	return root, state, data
}

// "depsilo backup --out <state>/backups/..." run as root left a directory and
// an archive in the state directory that the service account could neither
// read nor write.
func TestCreateGivesAnArchiveInTheStateDirectoryToTheOwnerOfTheReceivingDirectory(t *testing.T) {
	_, state, data := backupOwnershipState(t)
	paths := Paths{Config: filepath.Join(state, "config.toml"), Database: filepath.Join(data, "depsilo.db")}
	backups := filepath.Join(state, "backups")
	nightly := filepath.Join(backups, "nightly")
	recorded := &recordedOwnership{t: t, owners: map[string]int{state: 10001, backups: 20001, nightly: 30001, data: 40001}}
	stateBefore := directoryNames(t, state, data)

	archivePath := filepath.Join(nightly, "depsilo-backup.tar.gz")
	if _, err := createWith(context.Background(), paths, archivePath, recorded.rule()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ path, owner string }{
		{backups, "10001:10002"},
		{nightly, "20001:20002"},
		{archivePath, "30001:30002"},
	} {
		if _, owner, ok := recorded.givenAs(want.path); !ok || owner != want.owner {
			t.Errorf("%s: given=%v to %s, want it given to %s", want.path, ok, owner, want.owner)
		}
	}
	// The archive changed owner under its temporary name, before it was
	// renamed into place; the private workspace next to it never did.
	if name, _, _ := recorded.givenAs(archivePath); !strings.HasPrefix(name, filepath.Join(nightly, ".depsilo-backup-")) || strings.Contains(name, "-work-") {
		t.Errorf("the archive changed owner as %q, want its temporary name", name)
	}
	if len(recorded.given) != 3 {
		t.Errorf("%d entries changed owner, want the two new directories and the archive", len(recorded.given))
	}
	if names := directoryNames(t, nightly); len(names) != 1 || names[0] != archivePath {
		t.Errorf("the output directory holds %q, want only the archive", names)
	}
	stateAfter := directoryNames(t, state, data)
	if want := append([]string{backups}, stateBefore...); strings.Join(stateAfter, "\n") != strings.Join(want, "\n") {
		t.Errorf("the state directory holds %q after the backup, want %q", stateAfter, want)
	}

	// An existing directory keeps its owner; the database's directory is
	// part of the state as well.
	recorded.given = nil
	besideDatabase := filepath.Join(data, "depsilo-backup.tar.gz")
	if _, err := createWith(context.Background(), paths, besideDatabase, recorded.rule()); err != nil {
		t.Fatal(err)
	}
	if _, owner, ok := recorded.givenAs(besideDatabase); !ok || owner != "40001:40002" || len(recorded.given) != 1 {
		t.Errorf("beside the database: given=%v to %s among %d entries, want only the archive given to 40001:40002", ok, owner, len(recorded.given))
	}
}

// An archive taken out of the instance belongs to whoever took it.
func TestCreateKeepsAnArchiveOutsideTheStateDirectoryWithItsCreator(t *testing.T) {
	root, state, data := backupOwnershipState(t)
	paths := Paths{Config: filepath.Join(state, "config.toml"), Database: filepath.Join(data, "depsilo.db")}
	elsewhere := filepath.Join(root, "elsewhere")
	if err := os.Mkdir(elsewhere, 0o700); err != nil {
		t.Fatal(err)
	}
	// A link in the state directory that names a directory elsewhere is
	// elsewhere too.
	if err := os.Symlink(elsewhere, filepath.Join(state, "exported")); err != nil {
		t.Fatal(err)
	}
	// No directory is listed: reading an owner from any fails the test.
	recorded := &recordedOwnership{t: t}

	for _, archivePath := range []string{
		filepath.Join(elsewhere, "new", "depsilo-backup.tar.gz"),
		filepath.Join(state, "exported", "depsilo-backup.tar.gz"),
	} {
		if _, err := createWith(context.Background(), paths, archivePath, recorded.rule()); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(archivePath); err != nil {
			t.Fatal(err)
		}
	}
	if len(recorded.given) != 0 {
		t.Fatalf("%d entries outside the state directory changed owner", len(recorded.given))
	}
}

// An archive or a directory that cannot be handed over is not left behind,
// and the error names it.
func TestCreateThatCannotGiveAnEntryAwayLeavesNothing(t *testing.T) {
	_, state, data := backupOwnershipState(t)
	paths := Paths{Config: filepath.Join(state, "config.toml"), Database: filepath.Join(data, "depsilo.db")}
	backups := filepath.Join(state, "backups")
	archivePath := filepath.Join(backups, "depsilo-backup.tar.gz")
	recorded := &recordedOwnership{t: t, owners: map[string]int{state: 10001, backups: 10001}}

	recorded.refuse = func(name string) bool { return name == backups }
	_, err := createWith(context.Background(), paths, archivePath, recorded.rule())
	if !errors.Is(err, errOwnerRefused) || !strings.Contains(err.Error(), fmt.Sprintf("%q", backups)) {
		t.Fatalf("createWith() error = %v, want the refusal with the path %s", err, backups)
	}
	// The directory was created; it is not one the user cannot write to.
	if errors.As(err, new(*OutputDirectoryError)) {
		t.Fatalf("createWith() error = %v, reported as an output directory that cannot be written", err)
	}
	if _, err := os.Lstat(backups); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the directory that could not be handed over was left behind: %v", err)
	}

	var refused string
	recorded.refuse = func(name string) bool {
		if filepath.Dir(name) != backups {
			return false
		}
		refused = name
		return true
	}
	_, err = createWith(context.Background(), paths, archivePath, recorded.rule())
	if refused == "" || !errors.Is(err, errOwnerRefused) || !strings.Contains(err.Error(), fmt.Sprintf("%q", refused)) {
		t.Fatalf("createWith() error = %v, want the refusal with the path of the archive %q", err, refused)
	}
	if names := directoryNames(t, backups); len(names) != 0 {
		t.Fatalf("the failed backup left %q behind", names)
	}
}

// The real thing; see TestRestoreAsRootLeavesStateTheDirectoryOwnerCanUse
// for how to run it.
func TestCreateAsRootLeavesAnArchiveTheStateOwnerCanUse(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	root, state, data := backupOwnershipState(t)
	paths := Paths{Config: filepath.Join(state, "config.toml"), Database: filepath.Join(data, "depsilo.db")}
	owner := func(path string) string {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		return fmt.Sprintf("%d:%d", stat.Uid, stat.Gid)
	}
	for _, path := range []string{state, data, paths.Config, paths.Database} {
		if err := os.Chown(path, 10001, 10002); err != nil {
			t.Fatal(err)
		}
	}

	inside := filepath.Join(state, "backups", "depsilo-backup.tar.gz")
	outside := filepath.Join(root, "exported", "depsilo-backup.tar.gz")
	for _, archivePath := range []string{inside, outside} {
		if _, err := Create(context.Background(), paths, archivePath); err != nil {
			t.Fatal(err)
		}
	}
	for path, want := range map[string]string{
		filepath.Dir(inside): "10001:10002", inside: "10001:10002",
		filepath.Dir(outside): "0:0", outside: "0:0",
		paths.Config: "10001:10002", paths.Database: "10001:10002",
	} {
		if owner(path) != want {
			t.Errorf("%s belongs to %s, want %s", path, owner(path), want)
		}
	}
	if names := directoryNames(t, state, data, filepath.Dir(inside)); len(names) != 5 {
		t.Errorf("the state directory holds %q after the backups, want the config, data, the database, backups and one archive", names)
	}
}
