//go:build linux || darwin

package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
)

// givenEntry is one entry that changed owner: the name it had and the inode
// it was at that moment.
type givenEntry struct {
	name     string
	info     os.FileInfo
	uid, gid int
}

// recordedOwnership stands in for root working in directories that belong to
// other accounts. owners names those directories and the uid of each (its gid
// is the uid plus one); a directory that is not listed is one no owner may be
// read from. Every entry that would change owner is recorded, and must at
// that moment be the open entry under its own name: not a link, and not a
// file that was already renamed into place.
type recordedOwnership struct {
	t      *testing.T
	owners map[string]int
	given  []givenEntry
	// refuse makes changing the owner of matching entries fail.
	refuse func(name string) bool
}

var errOwnerRefused = errors.New("operation not permitted")

func (recorded *recordedOwnership) rule() entryOwnership {
	return entryOwnership{
		newEntryOwner: func(directory os.FileInfo) (int, int, bool) {
			for path, uid := range recorded.owners {
				if listed, err := os.Stat(path); err == nil && os.SameFile(listed, directory) {
					return uid, uid + 1, true
				}
			}
			recorded.t.Errorf("an owner was read from the unlisted directory %q", directory.Name())
			return 0, 0, false
		},
		chown: func(entry *os.File, uid, gid int) error {
			named, err := os.Lstat(entry.Name())
			if err != nil {
				recorded.t.Errorf("%q changed owner when it no longer had that name: %v", entry.Name(), err)
				return err
			}
			opened, err := entry.Stat()
			if err != nil {
				return err
			}
			if named.Mode()&os.ModeSymlink != 0 || !os.SameFile(named, opened) {
				recorded.t.Errorf("%q is not the entry that was opened", entry.Name())
			}
			if recorded.refuse != nil && recorded.refuse(entry.Name()) {
				return errOwnerRefused
			}
			recorded.given = append(recorded.given, givenEntry{name: entry.Name(), info: opened, uid: uid, gid: gid})
			return nil
		},
	}
}

// givenAs reports how the entry now at path changed owner: the name it had
// then and the owner it got.
func (recorded *recordedOwnership) givenAs(path string) (name, owner string, ok bool) {
	recorded.t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		recorded.t.Fatal(err)
	}
	for _, entry := range recorded.given {
		if os.SameFile(entry.info, info) {
			return entry.name, fmt.Sprintf("%d:%d", entry.uid, entry.gid), true
		}
	}
	return "", "", false
}

const ownershipTestConfig = "[auth]\njwt_secret = \"legacy-test-signing-key\"\n[database]\ndriver = \"sqlite\"\n"

// ownershipTestRoot returns a private directory and an archive of a small
// instance inside it.
func ownershipTestRoot(t *testing.T) (root, archivePath string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWriteRecoveryFile(t, filepath.Join(source, "config.toml"), ownershipTestConfig)
	mustWriteRecoveryDatabase(t, filepath.Join(source, "depsilo.db"), "restored")
	archivePath = filepath.Join(root, "archive", "backup.tar.gz")
	if _, err := Create(context.Background(), Paths{
		Config: filepath.Join(source, "config.toml"), Database: filepath.Join(source, "depsilo.db"),
	}, archivePath); err != nil {
		t.Fatal(err)
	}
	return root, archivePath
}

// ownershipTestState writes a configured instance: state/config.toml and
// state/data/depsilo.db with a stale WAL, plus entries a restore never
// replaces.
func ownershipTestState(t *testing.T, root string) (state, data string) {
	t.Helper()
	state = filepath.Join(root, "state")
	data = filepath.Join(state, "data")
	for _, directory := range []string{state, data, filepath.Join(data, "cache")} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteRecoveryFile(t, filepath.Join(state, "config.toml"), "old config")
	mustWriteRecoveryFile(t, filepath.Join(state, "notes.txt"), "kept")
	mustWriteRecoveryDatabase(t, filepath.Join(data, "depsilo.db"), "old")
	mustWriteRecoveryFile(t, filepath.Join(data, "depsilo.db-wal"), "stale wal")
	mustWriteRecoveryFile(t, filepath.Join(data, "cache", "artifact"), "cached")
	return state, data
}

func directoryNames(t *testing.T, directories ...string) []string {
	t.Helper()
	var names []string
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			names = append(names, filepath.Join(directory, entry.Name()))
		}
	}
	sort.Strings(names)
	return names
}

// "depsilo restore" run as root on a state directory of the service account
// left config.toml, the database and the copies of the previous state owned
// by root with mode 0600, and the service could not start.
func TestRestoreGivesWhatItCreatesToTheOwnerOfTheReceivingDirectory(t *testing.T) {
	root, archivePath := ownershipTestRoot(t)
	state, data := ownershipTestState(t, root)
	recorded := &recordedOwnership{t: t, owners: map[string]int{state: 10001, data: 20001}}

	result, err := restoreWith(context.Background(), archivePath, Paths{
		Config: filepath.Join(state, "config.toml"), Database: filepath.Join(data, "depsilo.db"),
	}, recorded.rule())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Previous) != 3 {
		t.Fatalf("previous copies = %q, want the config, the database and its WAL", result.Previous)
	}

	// The two restored files changed owner under the names they were staged
	// with: before they were renamed into place.
	accounted := make(map[string]bool)
	for _, want := range []struct{ path, stagedAs, owner string }{
		{filepath.Join(state, "config.toml"), filepath.Join(state, ".config.toml.restore-"), "10001:10002"},
		{filepath.Join(data, "depsilo.db"), filepath.Join(data, ".depsilo.db.restore-"), "20001:20002"},
	} {
		name, owner, ok := recorded.givenAs(want.path)
		if !ok || owner != want.owner || !strings.HasPrefix(name, want.stagedAs) {
			t.Errorf("%s: given=%v as %q to %s, want it given as %s* to %s", want.path, ok, name, owner, want.stagedAs, want.owner)
		}
		accounted[name] = true
	}
	// Each copy of the previous state belongs to the directory it is in.
	for _, path := range result.Previous {
		wantOwner := "20001:20002"
		if filepath.Dir(path) == state {
			wantOwner = "10001:10002"
		}
		name, owner, ok := recorded.givenAs(path)
		if !ok || owner != wantOwner || name != path {
			t.Errorf("%s: given=%v as %q to %s, want it given to %s", path, ok, name, owner, wantOwner)
		}
		accounted[name] = true
	}
	// So do the lease entries (decided from the database directory) and
	// every version of the restore journal, which a server start reads when
	// the restore did not finish. Nothing else changed owner: not the entries
	// the restore left alone, and nothing the archive names.
	for _, entry := range recorded.given {
		if accounted[entry.name] {
			continue
		}
		lease := strings.HasPrefix(entry.name, filepath.Join(data, ".depsilo-locks"))
		journal := strings.HasPrefix(entry.name, filepath.Join(data, ".depsilo.db"+restoreJournalSuffix+".tmp-"))
		if !lease && !journal {
			t.Errorf("%q changed owner, which the restore neither created nor replaced", entry.name)
		}
		if entry.uid != 20001 || entry.gid != 20002 {
			t.Errorf("%q was given to %d:%d, want the owner of the database directory", entry.name, entry.uid, entry.gid)
		}
		if journal {
			accounted["journal"] = true
		}
	}
	if !accounted["journal"] {
		t.Error("the restore journal was written without being handed over")
	}

	// Outside the rule (not root, or root in its own directory) nothing does.
	recorded.given = nil
	rule := recorded.rule()
	rule.newEntryOwner = func(os.FileInfo) (int, int, bool) { return 0, 0, false }
	if _, err := restoreWith(context.Background(), archivePath, Paths{
		Config: filepath.Join(state, "config.toml"), Database: filepath.Join(data, "depsilo.db"),
	}, rule); err != nil {
		t.Fatal(err)
	}
	if len(recorded.given) != 0 {
		t.Fatalf("a restore outside the rule changed the owner of %d entries", len(recorded.given))
	}
}

// A first restore into a place that does not exist yet creates directories:
// each one belongs to the directory that receives it.
func TestRestoreGivesDirectoriesItCreatesToTheOwnerOfTheReceivingDirectory(t *testing.T) {
	root, archivePath := ownershipTestRoot(t)
	instance := filepath.Join(root, "instance")
	if err := os.Mkdir(instance, 0o700); err != nil {
		t.Fatal(err)
	}
	etc := filepath.Join(instance, "etc")
	lib := filepath.Join(instance, "lib")
	data := filepath.Join(lib, "data")
	recorded := &recordedOwnership{t: t, owners: map[string]int{instance: 10001, etc: 20001, lib: 30001, data: 40001}}

	if _, err := restoreWith(context.Background(), archivePath, Paths{
		Config: filepath.Join(etc, "config.toml"), Database: filepath.Join(data, "depsilo.db"),
	}, recorded.rule()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ path, owner string }{
		{etc, "10001:10002"},
		{lib, "10001:10002"},
		{data, "30001:30002"},
		{filepath.Join(etc, "config.toml"), "20001:20002"},
		{filepath.Join(data, "depsilo.db"), "40001:40002"},
		{filepath.Join(data, ".depsilo-locks"), "40001:40002"},
	} {
		if _, owner, ok := recorded.givenAs(want.path); !ok || owner != want.owner {
			t.Errorf("%s: given=%v to %s, want it given to %s", want.path, ok, owner, want.owner)
		}
	}
	if _, _, ok := recorded.givenAs(instance); ok {
		t.Error("a directory that existed changed owner")
	}
}

// A file that cannot be handed over must not be installed: the previous state
// stays in place, nothing of the attempt is left behind, and the error names
// the file.
func TestRestoreThatCannotGiveAnEntryAwayLeavesThePreviousState(t *testing.T) {
	for _, test := range []struct{ name, directory, prefix string }{
		{"the staged config", "state", ".config.toml.restore-"},
		{"the staged database", "data", ".depsilo.db.restore-"},
		{"the copy of the previous config", "state", "config.toml.pre-restore-"},
		{"the copy of the previous database", "data", "depsilo.db.pre-restore-"},
		{"the copy of the previous WAL", "data", "depsilo.db-wal.pre-restore-"},
		{"the restore journal", "data", ".depsilo.db" + restoreJournalSuffix + ".tmp-"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, archivePath := ownershipTestRoot(t)
			state, data := ownershipTestState(t, root)
			// A lease taken earlier, so the attempt adds no lease entries.
			lease, err := HoldDatabase(filepath.Join(data, "depsilo.db"))
			if err != nil {
				t.Fatal(err)
			}
			if err := lease.Close(); err != nil {
				t.Fatal(err)
			}
			before := directoryNames(t, state, data, filepath.Join(data, ".depsilo-locks"))
			directory := map[string]string{"state": state, "data": data}[test.directory]
			var refused string
			recorded := &recordedOwnership{t: t, owners: map[string]int{state: 10001, data: 10001}}
			recorded.refuse = func(name string) bool {
				if !strings.HasPrefix(name, filepath.Join(directory, test.prefix)) {
					return false
				}
				refused = name
				return true
			}

			_, err = restoreWith(context.Background(), archivePath, Paths{
				Config: filepath.Join(state, "config.toml"), Database: filepath.Join(data, "depsilo.db"),
			}, recorded.rule())
			if refused == "" {
				t.Fatalf("the restore never tried to hand over %s; error = %v", test.name, err)
			}
			if !errors.Is(err, errOwnerRefused) || !strings.Contains(err.Error(), fmt.Sprintf("%q", refused)) {
				t.Fatalf("restoreWith() error = %v, want the refusal with the path %s", err, refused)
			}
			if strings.Contains(err.Error(), "restore was interrupted") {
				t.Fatalf("restoreWith() error = %v: nothing was published, so nothing is left to recover", err)
			}
			after := directoryNames(t, state, data, filepath.Join(data, ".depsilo-locks"))
			if strings.Join(after, "\n") != strings.Join(before, "\n") {
				t.Fatalf("entries after the failed restore:\n%s\nwant only the previous ones:\n%s", strings.Join(after, "\n"), strings.Join(before, "\n"))
			}
			assertRecoveryFile(t, filepath.Join(state, "config.toml"), "old config")
			assertRecoveryFile(t, filepath.Join(data, "depsilo.db-wal"), "stale wal")
			if got := readRecoveryDatabase(t, filepath.Join(data, "depsilo.db")); got != "old" {
				t.Fatalf("database value = %q, want the previous database", got)
			}
		})
	}
}

func assertRecoveryFile(t *testing.T, path, want string) {
	t.Helper()
	if got := string(mustReadRecoveryFile(t, path)); got != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

// A directory that cannot be handed over is removed again, so the next
// attempt does not take it for one that existed.
func TestRestoreRemovesADirectoryItCannotGiveAway(t *testing.T) {
	for _, test := range []struct{ name, directory string }{
		{"the config directory", "etc"},
		{"the database directory", "data"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, archivePath := ownershipTestRoot(t)
			instance := filepath.Join(root, "instance")
			if err := os.Mkdir(instance, 0o700); err != nil {
				t.Fatal(err)
			}
			refused := filepath.Join(instance, test.directory)
			recorded := &recordedOwnership{t: t, owners: map[string]int{instance: 10001, filepath.Join(instance, "data"): 10001}}
			recorded.refuse = func(name string) bool { return name == refused }

			_, err := restoreWith(context.Background(), archivePath, Paths{
				Config: filepath.Join(instance, "etc", "config.toml"), Database: filepath.Join(instance, "data", "depsilo.db"),
			}, recorded.rule())
			if !errors.Is(err, errOwnerRefused) || !strings.Contains(err.Error(), fmt.Sprintf("%q", refused)) {
				t.Fatalf("restoreWith() error = %v, want the refusal with the path %s", err, refused)
			}
			if _, err := os.Lstat(refused); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the directory that could not be handed over was left behind: %v", err)
			}
		})
	}
}

// The journal of a restore that did not finish is completed by the next
// holder of the database lease; what that writes is handed over too.
func TestRecoveryOfAnInterruptedRestoreHandsOverTheJournalItRewrites(t *testing.T) {
	root, _ := ownershipTestRoot(t)
	state, data := ownershipTestState(t, root)
	source := filepath.Join(root, "source")
	configMetadata, err := inspectFile("config.toml", filepath.Join(source, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	databaseMetadata, err := inspectFile("depsilo.db", filepath.Join(source, "depsilo.db"))
	if err != nil {
		t.Fatal(err)
	}
	stopped := errors.New("process stopped")
	_, err = installStateWith(context.Background(), map[string]extractedFile{
		"config.toml": {path: filepath.Join(source, "config.toml"), size: configMetadata.Size, sha256: configMetadata.SHA256},
		"depsilo.db":  {path: filepath.Join(source, "depsilo.db"), size: databaseMetadata.Size, sha256: databaseMetadata.SHA256},
	}, Paths{Config: filepath.Join(state, "config.toml"), Database: filepath.Join(data, "depsilo.db")}, fileOperations{
		checkpoint: func(name string) error {
			if name == "config_published" {
				return stopped
			}
			return nil
		},
	})
	if !errors.Is(err, stopped) {
		t.Fatalf("installStateWith() error = %v, want the interruption", err)
	}

	recorded := &recordedOwnership{t: t, owners: map[string]int{state: 10001, data: 20001}}
	lease, err := holdDatabase(filepath.Join(data, "depsilo.db"), recorded.rule())
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readRecoveryDatabase(t, filepath.Join(data, "depsilo.db")); got != "restored" {
		t.Fatalf("database value = %q, want the recovered restore", got)
	}
	journals := 0
	for _, entry := range recorded.given {
		if strings.HasPrefix(entry.name, filepath.Join(data, ".depsilo.db"+restoreJournalSuffix+".tmp-")) && entry.uid == 20001 && entry.gid == 20002 {
			journals++
		}
	}
	if journals == 0 {
		t.Fatalf("the recovery rewrote the journal without handing it over; it gave away %d entries", len(recorded.given))
	}
}

// The real thing, for restore and backup. It needs root, and no automated
// gate runs the suite as root, so it is skipped there: it is a manual check
// for a change to the ownership rule. Build the test binary for Linux
// (GOOS=linux CGO_ENABLED=0 go test -c ./internal/backup) and run it as root
// in a container, for example: docker run --rm --network none -u 0 -v
// "$PWD":/t:ro alpine:3.20 /t/backup.test -test.run AsRoot -test.v
func TestRestoreAsRootLeavesStateTheDirectoryOwnerCanUse(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	root, archivePath := ownershipTestRoot(t)
	state, data := ownershipTestState(t, root)
	owner := func(path string) string {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		return fmt.Sprintf("%d:%d", stat.Uid, stat.Gid)
	}
	// The service account owns the instance, except for one file root left
	// in it, which the restore does not replace.
	for _, path := range []string{state, data, filepath.Join(state, "config.toml"), filepath.Join(data, "depsilo.db"), filepath.Join(data, "depsilo.db-wal")} {
		if err := os.Chown(path, 10001, 10002); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Restore(context.Background(), archivePath, Paths{
		Config: filepath.Join(state, "config.toml"), Database: filepath.Join(data, "depsilo.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	created := append([]string{
		filepath.Join(state, "config.toml"), filepath.Join(data, "depsilo.db"), filepath.Join(data, ".depsilo-locks"),
	}, result.Previous...)
	for _, path := range created {
		if owner(path) != "10001:10002" {
			t.Errorf("%s belongs to %s, want 10001:10002", path, owner(path))
		}
	}
	for _, path := range []string{filepath.Join(state, "notes.txt"), filepath.Join(data, "cache"), filepath.Join(data, "cache", "artifact")} {
		if owner(path) != "0:0" {
			t.Errorf("%s, which the restore does not replace, now belongs to %s", path, owner(path))
		}
	}
	for _, path := range directoryNames(t, state, data) {
		if strings.Contains(path, ".restore-") || strings.HasSuffix(path, restoreJournalSuffix) {
			t.Errorf("the finished restore left %s behind", path)
		}
	}

	// A first restore into a new directory of the service account's.
	fresh := filepath.Join(state, "second", "data")
	if _, err := Restore(context.Background(), archivePath, Paths{
		Config: filepath.Join(state, "second", "config.toml"), Database: filepath.Join(fresh, "depsilo.db"),
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(state, "second"), fresh, filepath.Join(state, "second", "config.toml"), filepath.Join(fresh, "depsilo.db")} {
		if owner(path) != "10001:10002" {
			t.Errorf("%s belongs to %s, want 10001:10002", path, owner(path))
		}
	}

	// Root in its own directory keeps what it creates.
	own := filepath.Join(root, "own")
	if _, err := Restore(context.Background(), archivePath, Paths{
		Config: filepath.Join(own, "config.toml"), Database: filepath.Join(own, "depsilo.db"),
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{own, filepath.Join(own, "config.toml"), filepath.Join(own, "depsilo.db")} {
		if owner(path) != "0:0" {
			t.Errorf("%s belongs to %s, want 0:0", path, owner(path))
		}
	}
}
