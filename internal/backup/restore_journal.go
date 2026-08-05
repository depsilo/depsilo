package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	restoreJournalVersion = 1
	maxJournalBytes       = 1 << 20
)

type restorePhase string

const (
	restorePrepared          restorePhase = "prepared"
	restoreConfigCommitted   restorePhase = "config_committed"
	restoreSidecarsDetached  restorePhase = "sidecars_detached"
	restoreDatabaseCommitted restorePhase = "database_committed"
)

type restoreJournalEntry struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Candidate   string `json:"candidate,omitempty"`
	Previous    string `json:"previous,omitempty"`
	HadPrevious bool   `json:"had_previous"`
	NewSize     int64  `json:"new_size,omitempty"`
	NewSHA256   string `json:"new_sha256,omitempty"`
}

type restoreJournal struct {
	Version        int                   `json:"version"`
	Transaction    string                `json:"transaction"`
	Phase          restorePhase          `json:"phase"`
	DatabaseTarget string                `json:"database_target"`
	CreatedAt      time.Time             `json:"created_at"`
	Config         restoreJournalEntry   `json:"config"`
	Database       restoreJournalEntry   `json:"database"`
	Sidecars       []restoreJournalEntry `json:"sidecars"`
}

func installJournaled(ctx context.Context, files map[string]extractedFile, targets Paths, operations fileOperations) (RestoreResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if operations.publish == nil {
		operations.publish = publishFile
	}
	if err := ctx.Err(); err != nil {
		return RestoreResult{}, err
	}
	for label, target := range map[string]string{"config": targets.Config, "database": targets.Database} {
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return RestoreResult{}, fmt.Errorf("create %s restore directory: %w", label, err)
		}
		canonical, err := canonicalRestoreTarget(target, label)
		if err != nil {
			return RestoreResult{}, err
		}
		if !sameCanonicalPath(canonical, target) {
			return RestoreResult{}, fmt.Errorf("%s restore parent changed identity while preparing the restore", label)
		}
	}
	if err := validateRestoreTargetPair(targets.Config, targets.Database); err != nil {
		return RestoreResult{}, err
	}
	if err := recoverPendingRestoreWith(targets.Database, operations); err != nil {
		return RestoreResult{}, err
	}

	transaction, err := newRestoreTransactionID()
	if err != nil {
		return RestoreResult{}, fmt.Errorf("create restore transaction id: %w", err)
	}
	journal := restoreJournal{
		Version:        restoreJournalVersion,
		Transaction:    transaction,
		Phase:          restorePrepared,
		DatabaseTarget: targets.Database,
		CreatedAt:      time.Now().UTC(),
		Config: restoreJournalEntry{
			Name: "config.toml", Target: targets.Config,
			Candidate: restoreCandidatePath(targets.Config, transaction),
			NewSize:   files["config.toml"].size, NewSHA256: files["config.toml"].sha256,
		},
		Database: restoreJournalEntry{
			Name: "depsilo.db", Target: targets.Database,
			Candidate: restoreCandidatePath(targets.Database, transaction),
			NewSize:   files["depsilo.db"].size, NewSHA256: files["depsilo.db"].sha256,
		},
		Sidecars: []restoreJournalEntry{
			{Name: "depsilo.db-wal", Target: targets.Database + "-wal"},
			{Name: "depsilo.db-shm", Target: targets.Database + "-shm"},
		},
	}
	created := []string{journal.Config.Candidate, journal.Database.Candidate}
	journalDurable := false
	defer func() {
		if journalDurable {
			return
		}
		for _, path := range created {
			_ = removeRegularIfExists(path)
		}
		_ = syncRestoreDirectories(journal)
	}()

	if err := copyFileExclusive(ctx, files["config.toml"].path, journal.Config.Candidate); err != nil {
		return RestoreResult{}, fmt.Errorf("stage config restore candidate: %w", err)
	}
	if err := copyFileExclusive(ctx, files["depsilo.db"].path, journal.Database.Candidate); err != nil {
		return RestoreResult{}, fmt.Errorf("stage database restore candidate: %w", err)
	}
	if err := verifyJournalFile(journal.Config.Candidate, journal.Config.NewSize, journal.Config.NewSHA256); err != nil {
		return RestoreResult{}, fmt.Errorf("verify config restore candidate: %w", err)
	}
	if err := verifyJournalFile(journal.Database.Candidate, journal.Database.NewSize, journal.Database.NewSHA256); err != nil {
		return RestoreResult{}, fmt.Errorf("verify database restore candidate: %w", err)
	}

	for _, entry := range []*restoreJournalEntry{&journal.Config, &journal.Database, &journal.Sidecars[0], &journal.Sidecars[1]} {
		if err := preserveLiveFile(ctx, entry, transaction); err != nil {
			return RestoreResult{}, err
		}
		if entry.HadPrevious {
			created = append(created, entry.Previous)
		}
	}
	if err := syncRestoreDirectories(journal); err != nil {
		return RestoreResult{}, fmt.Errorf("sync prepared restore files: %w", err)
	}
	if err := writeRestoreJournal(journal); err != nil {
		var publicationError *journalPublicationError
		if errors.As(err, &publicationError) {
			journalDurable = true
			return RestoreResult{}, interruptedRestoreError(err)
		}
		return RestoreResult{}, err
	}
	journalDurable = true
	if err := runRestoreCheckpoint(operations, "prepared"); err != nil {
		return RestoreResult{}, interruptedRestoreError(err)
	}

	if err := ctx.Err(); err != nil {
		return RestoreResult{}, interruptedRestoreError(err)
	}
	if err := publishJournalEntry(journal.Config, operations); err != nil {
		return RestoreResult{}, interruptedRestoreError(fmt.Errorf("publish restored config.toml: %w", err))
	}
	if err := runRestoreCheckpoint(operations, "config_published"); err != nil {
		return RestoreResult{}, interruptedRestoreError(err)
	}
	journal.Phase = restoreConfigCommitted
	if err := writeRestoreJournal(journal); err != nil {
		return RestoreResult{}, interruptedRestoreError(err)
	}

	if err := detachJournalSidecars(journal, operations); err != nil {
		return RestoreResult{}, interruptedRestoreError(err)
	}
	if err := runRestoreCheckpoint(operations, "sidecars_detached"); err != nil {
		return RestoreResult{}, interruptedRestoreError(err)
	}
	journal.Phase = restoreSidecarsDetached
	if err := writeRestoreJournal(journal); err != nil {
		return RestoreResult{}, interruptedRestoreError(err)
	}

	if err := ctx.Err(); err != nil {
		return RestoreResult{}, interruptedRestoreError(err)
	}
	if err := publishJournalEntry(journal.Database, operations); err != nil {
		return RestoreResult{}, interruptedRestoreError(fmt.Errorf("publish restored depsilo.db: %w", err))
	}
	if err := runRestoreCheckpoint(operations, "database_published"); err != nil {
		return RestoreResult{}, interruptedRestoreError(err)
	}
	journal.Phase = restoreDatabaseCommitted
	if err := writeRestoreJournal(journal); err != nil {
		return RestoreResult{}, interruptedRestoreError(err)
	}
	if err := validateSQLite(ctx, targets.Database); err != nil {
		return RestoreResult{}, interruptedRestoreError(fmt.Errorf("validate published database: %w", err))
	}
	if err := runLeaseValidation(operations); err != nil {
		return RestoreResult{}, interruptedRestoreError(err)
	}
	if err := finishRestoreJournal(journal); err != nil {
		return RestoreResult{}, interruptedRestoreError(err)
	}

	result := RestoreResult{Restored: []string{"config.toml", "depsilo.db"}}
	for _, entry := range journalEntries(journal) {
		if entry.HadPrevious {
			result.Previous = append(result.Previous, entry.Previous)
		}
	}
	sort.Strings(result.Previous)
	return result, nil
}

func recoverPendingRestoreWith(databasePath string, operations fileOperations) error {
	if operations.publish == nil {
		operations.publish = publishFile
	}
	journalPath := databasePath + restoreJournalSuffix
	info, err := os.Lstat(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("restore journal %q is not a regular file", journalPath)
	}
	if info.Size() < 0 || info.Size() > maxJournalBytes {
		return fmt.Errorf("restore journal %q exceeds the %d-byte limit", journalPath, maxJournalBytes)
	}
	data, err := os.ReadFile(journalPath)
	if err != nil {
		return err
	}
	var journal restoreJournal
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return fmt.Errorf("decode restore journal: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("restore journal contains trailing JSON")
	}
	if err := validateRestoreJournal(journal, databasePath); err != nil {
		return err
	}

	configCandidateExists, err := regularFileExists(journal.Config.Candidate)
	if err != nil {
		return err
	}
	if journal.Phase == restorePrepared && configCandidateExists {
		// The commit rename consumes the candidate. Its continued presence
		// proves the live config was never touched, so this transaction can be
		// safely abandoned rather than guessing.
		if err := runLeaseValidation(operations); err != nil {
			return err
		}
		return abortPreparedRestore(journal)
	}
	configMatches, err := journalTargetMatches(journal.Config)
	if err != nil {
		return err
	}
	if !configMatches {
		return errors.New("interrupted restore cannot be recovered safely: committed config does not match the journal")
	}

	if journal.Phase == restorePrepared {
		journal.Phase = restoreConfigCommitted
		if err := writeRestoreJournal(journal); err != nil {
			return err
		}
	}
	if err := detachJournalSidecars(journal, operations); err != nil {
		return err
	}
	if journal.Phase == restoreConfigCommitted {
		journal.Phase = restoreSidecarsDetached
		if err := writeRestoreJournal(journal); err != nil {
			return err
		}
	}

	databaseMatches, err := journalTargetMatches(journal.Database)
	if err != nil {
		return err
	}
	if !databaseMatches {
		candidateExists, err := regularFileExists(journal.Database.Candidate)
		if err != nil {
			return err
		}
		if !candidateExists {
			return errors.New("interrupted restore cannot be recovered safely: database candidate and committed database are both missing")
		}
		if err := publishJournalEntry(journal.Database, operations); err != nil {
			return fmt.Errorf("complete interrupted database restore: %w", err)
		}
	}
	journal.Phase = restoreDatabaseCommitted
	if err := writeRestoreJournal(journal); err != nil {
		return err
	}
	if err := validateSQLite(context.Background(), journal.Database.Target); err != nil {
		return fmt.Errorf("validate recovered database: %w", err)
	}
	if err := runLeaseValidation(operations); err != nil {
		return err
	}
	return finishRestoreJournal(journal)
}

func validateRestoreJournal(journal restoreJournal, databasePath string) error {
	if journal.Version != restoreJournalVersion {
		return fmt.Errorf("unsupported restore journal version %d", journal.Version)
	}
	if len(journal.Transaction) != 32 {
		return errors.New("restore journal has an invalid transaction id")
	}
	if _, err := hex.DecodeString(journal.Transaction); err != nil {
		return errors.New("restore journal has an invalid transaction id")
	}
	switch journal.Phase {
	case restorePrepared, restoreConfigCommitted, restoreSidecarsDetached, restoreDatabaseCommitted:
	default:
		return fmt.Errorf("restore journal has an invalid phase %q", journal.Phase)
	}
	canonicalDatabase, err := canonicalDatabasePath(databasePath)
	if err != nil {
		return err
	}
	if !sameCanonicalPath(canonicalDatabase, journal.DatabaseTarget) || !sameCanonicalPath(journal.Database.Target, journal.DatabaseTarget) {
		return errors.New("restore journal database target does not match the leased database")
	}
	canonicalConfig, err := canonicalRestoreTarget(journal.Config.Target, "journal config")
	if err != nil {
		return err
	}
	if !sameCanonicalPath(canonicalConfig, journal.Config.Target) {
		return errors.New("restore journal config parent changed identity")
	}
	if err := validateRestoreTargetPair(journal.Config.Target, journal.Database.Target); err != nil {
		return err
	}
	wantEntries := []struct {
		entry  restoreJournalEntry
		target string
		name   string
		new    bool
	}{
		{journal.Config, journal.Config.Target, "config.toml", true},
		{journal.Database, journal.Database.Target, "depsilo.db", true},
	}
	if len(journal.Sidecars) != 2 {
		return errors.New("restore journal must list exactly two SQLite sidecars")
	}
	wantEntries = append(wantEntries,
		struct {
			entry  restoreJournalEntry
			target string
			name   string
			new    bool
		}{journal.Sidecars[0], journal.Database.Target + "-wal", "depsilo.db-wal", false},
		struct {
			entry  restoreJournalEntry
			target string
			name   string
			new    bool
		}{journal.Sidecars[1], journal.Database.Target + "-shm", "depsilo.db-shm", false},
	)
	for _, want := range wantEntries {
		entry := want.entry
		if entry.Name != want.name || !sameCanonicalPath(entry.Target, want.target) {
			return fmt.Errorf("restore journal entry %q has an unexpected target", entry.Name)
		}
		if want.new {
			if entry.Candidate != restoreCandidatePath(entry.Target, journal.Transaction) || entry.NewSize < 0 || !validSHA256(entry.NewSHA256) {
				return fmt.Errorf("restore journal entry %q has invalid candidate metadata", entry.Name)
			}
		} else if entry.Candidate != "" || entry.NewSize != 0 || entry.NewSHA256 != "" {
			return fmt.Errorf("restore journal sidecar %q contains replacement metadata", entry.Name)
		}
		if entry.HadPrevious {
			if entry.Previous != restorePreviousPath(entry.Target, journal.Transaction) {
				return fmt.Errorf("restore journal entry %q has an invalid previous path", entry.Name)
			}
			exists, err := regularFileExists(entry.Previous)
			if err != nil || !exists {
				return fmt.Errorf("restore journal previous copy for %q is unavailable: %w", entry.Name, err)
			}
		} else if entry.Previous != "" {
			return fmt.Errorf("restore journal entry %q has an unexpected previous path", entry.Name)
		}
	}
	return nil
}

func preserveLiveFile(ctx context.Context, entry *restoreJournalEntry, transaction string) error {
	info, err := os.Lstat(entry.Target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing %s: %w", entry.Name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("existing %s target must be a regular file, not a symbolic link or special file", entry.Name)
	}
	entry.Previous = restorePreviousPath(entry.Target, transaction)
	if err := copyFileExclusive(ctx, entry.Target, entry.Previous); err != nil {
		_ = removeRegularIfExists(entry.Previous)
		return fmt.Errorf("preserve existing %s: %w", entry.Name, err)
	}
	entry.HadPrevious = true
	return nil
}

func publishJournalEntry(entry restoreJournalEntry, operations fileOperations) error {
	if operations.publish == nil {
		operations.publish = publishFile
	}
	if err := verifyJournalFile(entry.Candidate, entry.NewSize, entry.NewSHA256); err != nil {
		return err
	}
	if err := runLeaseValidation(operations); err != nil {
		return err
	}
	if info, err := os.Lstat(entry.Target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse to replace symbolic-link target %q", entry.Target)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := operations.publish(entry.Candidate, entry.Target); err != nil {
		return err
	}
	if err := os.Chmod(entry.Target, 0o600); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(entry.Target)); err != nil {
		return err
	}
	return verifyJournalFile(entry.Target, entry.NewSize, entry.NewSHA256)
}

func detachJournalSidecars(journal restoreJournal, operations fileOperations) error {
	for _, entry := range journal.Sidecars {
		if err := runLeaseValidation(operations); err != nil {
			return err
		}
		if err := removeRegularIfExists(entry.Target); err != nil {
			return fmt.Errorf("detach stale %s: %w", entry.Name, err)
		}
	}
	return syncDirectory(filepath.Dir(journal.Database.Target))
}

func abortPreparedRestore(journal restoreJournal) error {
	// The live pair is still untouched in PREPARED. Remove and sync the
	// journal first; after that point leftover candidates are harmless garbage
	// and a crash cannot be mistaken for a committed transaction.
	if err := removeRestoreJournal(journal); err != nil {
		return err
	}
	var result error
	for _, entry := range journalEntries(journal) {
		if entry.Candidate != "" {
			if err := removeRegularIfExists(entry.Candidate); err != nil {
				result = errors.Join(result, err)
			}
		}
		if entry.HadPrevious {
			if err := removeRegularIfExists(entry.Previous); err != nil {
				result = errors.Join(result, err)
			}
		}
	}
	return errors.Join(result, syncRestoreDirectories(journal))
}

func finishRestoreJournal(journal restoreJournal) error {
	for _, entry := range []restoreJournalEntry{journal.Config, journal.Database} {
		if err := removeRegularIfExists(entry.Candidate); err != nil {
			return err
		}
	}
	return removeRestoreJournal(journal)
}

func removeRestoreJournal(journal restoreJournal) error {
	journalPath := journal.DatabaseTarget + restoreJournalSuffix
	if err := removeRegularIfExists(journalPath); err != nil {
		return fmt.Errorf("remove completed restore journal: %w", err)
	}
	return syncRestoreDirectories(journal)
}

func writeRestoreJournal(journal restoreJournal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return fmt.Errorf("encode restore journal: %w", err)
	}
	if len(data) > maxJournalBytes {
		return errors.New("restore journal exceeds its size limit")
	}
	path := journal.DatabaseTarget + restoreJournalSuffix
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create restore journal: %w", err)
	}
	temporaryPath := temporary.Name()
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(temporaryPath)
		}
	}()
	_, writeErr := temporary.Write(data)
	err = errors.Join(writeErr, temporary.Chmod(0o600), temporary.Sync(), temporary.Close())
	if err != nil {
		return fmt.Errorf("persist restore journal: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("restore journal path is a symbolic link")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := publishFile(temporaryPath, path); err != nil {
		return fmt.Errorf("publish restore journal: %w", err)
	}
	completed = true
	if err := syncDirectory(directory); err != nil {
		return &journalPublicationError{err: fmt.Errorf("sync restore journal directory: %w", err)}
	}
	return nil
}

// journalPublicationError means the namespace update already happened even
// though its directory durability could not be confirmed. Callers must retain
// candidates and previous copies so either the current filesystem state or a
// post-crash reappearance of the journal remains recoverable.
type journalPublicationError struct{ err error }

func (err *journalPublicationError) Error() string { return err.err.Error() }
func (err *journalPublicationError) Unwrap() error { return err.err }

func copyFileExclusive(ctx context.Context, source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = in.Close()
		return err
	}
	_, copyErr := copyContext(ctx, out, in)
	return errors.Join(copyErr, in.Close(), out.Chmod(0o600), out.Sync(), out.Close())
}

func verifyJournalFile(path string, size int64, digest string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("path does not name a regular file")
	}
	if info.Size() != size {
		return fmt.Errorf("size is %d, want %d", info.Size(), size)
	}
	metadata, err := inspectFile(filepath.Base(path), path)
	if err != nil {
		return err
	}
	if metadata.SHA256 != digest {
		return errors.New("SHA-256 checksum does not match")
	}
	return nil
}

func journalTargetMatches(entry restoreJournalEntry) (bool, error) {
	exists, err := regularFileExists(entry.Target)
	if err != nil || !exists {
		return false, err
	}
	return verifyJournalFile(entry.Target, entry.NewSize, entry.NewSHA256) == nil, nil
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("%q is not a regular file", path)
	}
	return true, nil
}

func removeRegularIfExists(path string) error {
	if path == "" {
		return nil
	}
	exists, err := regularFileExists(path)
	if err != nil || !exists {
		return err
	}
	return os.Remove(path)
}

func syncRestoreDirectories(journal restoreJournal) error {
	directories := map[string]bool{
		filepath.Dir(journal.Config.Target):   true,
		filepath.Dir(journal.Database.Target): true,
	}
	var result error
	for directory := range directories {
		result = errors.Join(result, syncDirectory(directory))
	}
	return result
}

func journalEntries(journal restoreJournal) []restoreJournalEntry {
	return append([]restoreJournalEntry{journal.Config, journal.Database}, journal.Sidecars...)
}

func restoreCandidatePath(target, transaction string) string {
	return filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".restore-"+transaction)
}

func restorePreviousPath(target, transaction string) string {
	return filepath.Join(filepath.Dir(target), filepath.Base(target)+".pre-restore-"+transaction+".bak")
}

func newRestoreTransactionID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
}

func runRestoreCheckpoint(operations fileOperations, name string) error {
	if operations.checkpoint == nil {
		return nil
	}
	return operations.checkpoint(name)
}

func runLeaseValidation(operations fileOperations) error {
	if operations.validateLease == nil {
		return nil
	}
	if err := operations.validateLease(); err != nil {
		return fmt.Errorf("database restore lease identity changed: %w", err)
	}
	return nil
}

func interruptedRestoreError(err error) error {
	return fmt.Errorf("restore was interrupted; durable recovery state was retained and the next server start or restore will converge it: %w", err)
}
