package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"depsilo/internal/audit"
	"depsilo/internal/backup"
	"depsilo/internal/config"
	"depsilo/internal/db"
)

const (
	resetTestOldPassword = "Old-Passw0rd-For-Tests"
	resetTestNewPassword = "N3w-Secret-Passphrase!"
	resetTestOSUser      = "svc-depsilo (uid 10001)"
	// resetTestPipedNotice is all a successful piped run writes to stderr.
	resetTestPipedNotice = "Reading the new password for account admin from standard input, which is not a terminal: one line, and anything typed is visible.\n"
)

// clearDepsiloEnvironment keeps the developer's or CI's own DEPSILO_*
// settings out of the command under test. DEPSILO_DATABASE_DSN in particular
// would point it at a database other than the fixture's, and a passing run
// sets that database's password to a constant in this file. The configuration
// loader ignores empty values.
func clearDepsiloEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		if name, _, _ := strings.Cut(entry, "="); strings.HasPrefix(name, "DEPSILO_") {
			t.Setenv(name, "")
		}
	}
}

// resetPasswordFixture is a stopped instance: a config.toml that names a
// migrated SQLite database holding four accounts and one audit row.
type resetPasswordFixture struct {
	home         string
	configPath   string
	databasePath string
}

func newResetPasswordFixture(t *testing.T) resetPasswordFixture {
	t.Helper()
	root := t.TempDir()
	fixture := resetPasswordFixture{
		home:         filepath.Join(root, "home"),
		configPath:   filepath.Join(root, "instance", "config.toml"),
		databasePath: filepath.Join(root, "instance", "data", "depsilo.db"),
	}
	for _, directory := range []string{fixture.home, filepath.Dir(fixture.databasePath)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	document := "config_version = 1\n" +
		"[server]\nhost = \"127.0.0.1\"\nport = 23333\n" +
		"[database]\ndriver = \"sqlite\"\ndsn = '" + fixture.databasePath + "'\n"
	if err := os.WriteFile(fixture.configPath, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	clearDepsiloEnvironment(t)
	t.Setenv("HOME", fixture.home)
	t.Setenv("USERPROFILE", fixture.home)
	t.Setenv("DEPSILO_CONFIG", fixture.configPath)

	// The fixture hash uses the cheapest cost so a test can tell that the
	// command stores its own hash at the server's cost.
	hash, err := bcrypt.GenerateFromPassword([]byte(resetTestOldPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	fixture.withDatabase(t, func(database *gorm.DB) {
		if err := db.AutoMigrate(database); err != nil {
			t.Fatal(err)
		}
		users := []db.User{
			{Username: "admin", PasswordHash: string(hash), Role: "admin", Enabled: true, CredentialVersion: 3},
			{Username: "viewer", PasswordHash: string(hash), Role: "readonly", Enabled: true, CredentialVersion: 1},
			{Username: "retired", PasswordHash: string(hash), Role: "admin", Enabled: true, CredentialVersion: 1},
			{Username: "oidc_4f2a", Role: "admin", Enabled: true, CredentialVersion: 1, PermissionSource: "oidc"},
		}
		if err := database.Create(&users).Error; err != nil {
			t.Fatal(err)
		}
		// Rows written before the permission_source column carry an empty
		// value, which every reader treats as "local".
		if err := database.Model(&db.User{}).Where("username = ?", "viewer").Update("permission_source", "").Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Model(&db.User{}).Where("username = ?", "retired").Update("enabled", false).Error; err != nil {
			t.Fatal(err)
		}
		if err := audit.AppendAuditRows(context.Background(), database, []db.AuditLog{
			{Action: "download", CacheResult: "hit", StatusCode: 200, CreatedAt: time.Now().UTC()},
		}); err != nil {
			t.Fatal(err)
		}
	})
	return fixture
}

func (fixture resetPasswordFixture) withDatabase(t *testing.T, use func(*gorm.DB)) {
	t.Helper()
	database, err := db.Open("sqlite", fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	use(database)
}

// resetPasswordState is everything the command may change.
type resetPasswordState struct {
	users         []db.User
	auditRows     int64
	schemaVersion int
}

func (fixture resetPasswordFixture) state(t *testing.T) resetPasswordState {
	t.Helper()
	var state resetPasswordState
	fixture.withDatabase(t, func(database *gorm.DB) {
		if err := database.Order("id").Find(&state.users).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Model(&db.AuditLog{}).Count(&state.auditRows).Error; err != nil {
			t.Fatal(err)
		}
		version, err := db.RecordedSchemaVersion(database)
		if err != nil {
			t.Fatal(err)
		}
		state.schemaVersion = version
	})
	return state
}

func (state resetPasswordState) user(t *testing.T, username string) db.User {
	t.Helper()
	for _, user := range state.users {
		if user.Username == username {
			return user
		}
	}
	t.Fatalf("fixture has no account %q", username)
	return db.User{}
}

func assertResetPasswordChangedNothing(t *testing.T, before, after resetPasswordState) {
	t.Helper()
	if before.auditRows != after.auditRows || before.schemaVersion != after.schemaVersion || len(before.users) != len(after.users) {
		t.Fatalf("state changed: audit rows %d -> %d, schema %d -> %d, accounts %d -> %d",
			before.auditRows, after.auditRows, before.schemaVersion, after.schemaVersion, len(before.users), len(after.users))
	}
	for index, user := range before.users {
		now := after.users[index]
		if user.PasswordHash != now.PasswordHash || user.CredentialVersion != now.CredentialVersion ||
			user.Enabled != now.Enabled || user.Role != now.Role || user.PermissionSource != now.PermissionSource ||
			!user.UpdatedAt.Equal(now.UpdatedAt) {
			t.Fatalf("account %q changed", user.Username)
		}
	}
}

type resetPasswordRun struct {
	stdout  string
	stderr  string
	code    int
	prompts []string
}

type failingReader struct{ t *testing.T }

func (reader failingReader) Read([]byte) (int, error) {
	reader.t.Error("the command read standard input although it must not")
	return 0, errors.New("unexpected read")
}

// runResetPasswordPiped runs the command with standard input that is not a
// terminal. A nil input fails the test when the command reads it.
func runResetPasswordPiped(t *testing.T, input *string, args ...string) resetPasswordRun {
	t.Helper()
	var stdout, stderr strings.Builder
	streams := resetPasswordIO{
		stdout:     &stdout,
		stderr:     &stderr,
		isTerminal: func() bool { return false },
		readPassword: func(string) (string, error) {
			t.Error("the command prompted although standard input is not a terminal")
			return "", errors.New("unexpected prompt")
		},
		osUser:       func() string { return resetTestOSUser },
		effectiveUID: os.Geteuid,
	}
	secrets := []string{}
	if input == nil {
		streams.stdin = failingReader{t}
	} else {
		streams.stdin = strings.NewReader(*input)
		secrets = append(secrets, strings.TrimRight(*input, "\r\n"))
	}
	run := resetPasswordRun{code: resetPassword(args, streams)}
	run.stdout, run.stderr = stdout.String(), stderr.String()
	assertResetPasswordOutputHasNoSecret(t, run, secrets)
	return run
}

// runResetPasswordOnTerminal runs the command as if standard input were a
// terminal; answers are what the operator types at each no-echo prompt.
func runResetPasswordOnTerminal(t *testing.T, answers []string, args ...string) resetPasswordRun {
	t.Helper()
	var stdout, stderr strings.Builder
	var run resetPasswordRun
	streams := resetPasswordIO{
		stdin:      failingReader{t},
		stdout:     &stdout,
		stderr:     &stderr,
		isTerminal: func() bool { return true },
		readPassword: func(prompt string) (string, error) {
			if len(run.prompts) >= len(answers) {
				t.Errorf("unexpected prompt %q", prompt)
				return "", errors.New("unexpected prompt")
			}
			answer := answers[len(run.prompts)]
			run.prompts = append(run.prompts, prompt)
			return answer, nil
		},
		osUser:       func() string { return resetTestOSUser },
		effectiveUID: os.Geteuid,
	}
	run.code = resetPassword(args, streams)
	run.stdout, run.stderr = stdout.String(), stderr.String()
	assertResetPasswordOutputHasNoSecret(t, run, answers)
	return run
}

func assertResetPasswordOutputHasNoSecret(t *testing.T, run resetPasswordRun, secrets []string) {
	t.Helper()
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		if strings.Contains(run.stdout, secret) || strings.Contains(run.stderr, secret) {
			t.Fatalf("the command printed a password it was given: stdout=%q stderr=%q", run.stdout, run.stderr)
		}
	}
}

func passwordMatches(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func resetPasswordAuditRows(t *testing.T, fixture resetPasswordFixture) []db.AuditLog {
	t.Helper()
	var rows []db.AuditLog
	fixture.withDatabase(t, func(database *gorm.DB) {
		if err := database.Where("action = ?", "user.cli_reset").Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		report, err := audit.VerifyChain(context.Background(), database, 2000)
		if err != nil {
			t.Fatal(err)
		}
		if !report.OK {
			t.Fatalf("audit chain broken at row %d: %s", report.BrokenAtID, report.Reason)
		}
	})
	return rows
}

func TestResetPasswordWithPipedInputUpdatesTheAccountAndTheAuditLog(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	before := fixture.state(t)

	input := resetTestNewPassword + "\n"
	run := runResetPasswordPiped(t, &input, "admin")
	if run.code != 0 || run.stderr != resetTestPipedNotice {
		t.Fatalf("reset-password admin: code=%d stderr=%q stdout=%q", run.code, run.stderr, run.stdout)
	}
	for _, want := range []string{
		"Password updated for account admin (role admin) in ",
		"depsilo.db",
		"Every signed-in session of this account is now invalid, including sessions signed in with the old password.",
		"API tokens of this account were not changed and keep working.",
		"You can start Depsilo again.",
	} {
		if !strings.Contains(run.stdout, want) {
			t.Errorf("stdout does not contain %q: %q", want, run.stdout)
		}
	}

	after := fixture.state(t)
	was, now := before.user(t, "admin"), after.user(t, "admin")
	if !passwordMatches(now.PasswordHash, resetTestNewPassword) {
		t.Fatal("the stored hash does not match the new password")
	}
	if passwordMatches(now.PasswordHash, resetTestOldPassword) {
		t.Fatal("the stored hash still matches the old password")
	}
	if cost, err := bcrypt.Cost([]byte(now.PasswordHash)); err != nil || cost != bcrypt.DefaultCost {
		t.Fatalf("stored bcrypt cost = %d, %v; want the server's cost %d", cost, err, bcrypt.DefaultCost)
	}
	if now.CredentialVersion != was.CredentialVersion+1 {
		t.Fatalf("credential_version = %d, want %d", now.CredentialVersion, was.CredentialVersion+1)
	}
	if !now.Enabled || now.Role != was.Role || now.PermissionSource != was.PermissionSource {
		t.Fatalf("the reset changed more than the credential: %+v", now)
	}
	for _, username := range []string{"viewer", "retired", "oidc_4f2a"} {
		other, same := after.user(t, username), before.user(t, username)
		if other.PasswordHash != same.PasswordHash || other.CredentialVersion != same.CredentialVersion {
			t.Fatalf("account %q changed", username)
		}
	}

	rows := resetPasswordAuditRows(t, fixture)
	if len(rows) != 1 || after.auditRows != before.auditRows+1 {
		t.Fatalf("audit rows for the reset = %d (total %d -> %d), want exactly one new row", len(rows), before.auditRows, after.auditRows)
	}
	row := rows[0]
	if row.Ecosystem != "admin" || row.PackageName != "user:1" || row.CacheResult != "success" || row.StatusCode != 200 {
		t.Fatalf("audit record = %+v", row)
	}
	if row.Version != "password reset from the local CLI by OS user "+resetTestOSUser {
		t.Fatalf("audit detail = %q", row.Version)
	}
	if strings.Contains(row.Version+row.PackageName+row.UserAgent, now.PasswordHash) {
		t.Fatal("the audit record carries the password hash")
	}

	keyPath, err := config.ManagedJWTSecretPath(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNoCLIManagedKey(t, keyPath)
}

func TestResetPasswordKeepsSpacesAndStripsOnlyTheLineEnding(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	// --config wins over DEPSILO_CONFIG, which names no instance here.
	elsewhere := filepath.Join(fixture.home, "elsewhere.toml")
	t.Setenv("DEPSILO_CONFIG", elsewhere)
	password := "  four words with spaces  "
	input := password + "\r\nsecond line is ignored\n"
	run := runResetPasswordPiped(t, &input, "--config", fixture.configPath, "viewer")
	if run.code != 0 {
		t.Fatalf("code=%d stderr=%q", run.code, run.stderr)
	}
	if os.Getenv("DEPSILO_CONFIG") != elsewhere {
		t.Fatal("the command left DEPSILO_CONFIG changed")
	}
	if !passwordMatches(fixture.state(t).user(t, "viewer").PasswordHash, password) {
		t.Fatal("the stored hash does not match the piped line without its line ending")
	}
}

func TestResetPasswordOnATerminalAsksTwiceWithoutEcho(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	before := fixture.state(t)

	run := runResetPasswordOnTerminal(t, []string{resetTestNewPassword, resetTestNewPassword}, "admin")
	if run.code != 0 {
		t.Fatalf("code=%d stderr=%q", run.code, run.stderr)
	}
	if len(run.prompts) != 2 || run.prompts[0] != "New password for admin: " || run.prompts[1] != "Repeat the new password: " {
		t.Fatalf("prompts = %q", run.prompts)
	}
	if !strings.Contains(run.stderr, "Setting a new password for account admin in ") {
		t.Fatalf("the operator is not told which account and database before typing: %q", run.stderr)
	}
	// End of input (Ctrl-D) does not end a no-echo prompt.
	if !strings.Contains(run.stderr, "depsilo.db. Press Ctrl-C to cancel.\n") {
		t.Fatalf("the operator is not told how to leave the prompt: %q", run.stderr)
	}
	after := fixture.state(t)
	if !passwordMatches(after.user(t, "admin").PasswordHash, resetTestNewPassword) {
		t.Fatal("the stored hash does not match the new password")
	}
	if after.user(t, "admin").CredentialVersion != before.user(t, "admin").CredentialVersion+1 {
		t.Fatal("credential_version did not go up by one")
	}
	if rows := resetPasswordAuditRows(t, fixture); len(rows) != 1 {
		t.Fatalf("audit rows for the reset = %d, want 1", len(rows))
	}
}

func TestResetPasswordRejectsAMismatchedRepeat(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	before := fixture.state(t)

	run := runResetPasswordOnTerminal(t, []string{resetTestNewPassword, resetTestNewPassword + "x"}, "admin")
	if run.code != 1 || !strings.Contains(run.stderr, "Error: the two passwords do not match; nothing was changed.") {
		t.Fatalf("code=%d stderr=%q", run.code, run.stderr)
	}
	if run.stdout != "" {
		t.Fatalf("stdout = %q, want nothing", run.stdout)
	}
	assertResetPasswordChangedNothing(t, before, fixture.state(t))
}

func TestResetPasswordAppliesTheSharedPasswordPolicy(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	before := fixture.state(t)

	t.Run("piped", func(t *testing.T) {
		input := "Sh0rt!x\n"
		run := runResetPasswordPiped(t, &input, "admin")
		if run.code != 1 || !strings.Contains(run.stderr, "Error: password must be at least 12 characters; nothing was changed.") {
			t.Fatalf("code=%d stderr=%q", run.code, run.stderr)
		}
		assertResetPasswordChangedNothing(t, before, fixture.state(t))
	})
	t.Run("terminal", func(t *testing.T) {
		// The policy speaks before the repeat prompt.
		run := runResetPasswordOnTerminal(t, []string{"Admin-Is-Safe-123!"}, "admin")
		if run.code != 1 || !strings.Contains(run.stderr, "Error: password must not contain the username; nothing was changed.") {
			t.Fatalf("code=%d stderr=%q", run.code, run.stderr)
		}
		assertResetPasswordChangedNothing(t, before, fixture.state(t))
	})
	t.Run("empty input", func(t *testing.T) {
		for _, input := range []string{"", "\n", "\r\n"} {
			run := runResetPasswordPiped(t, &input, "admin")
			if run.code != 1 || !strings.Contains(run.stderr, "standard input is not a terminal and carried no password") {
				t.Fatalf("input %q: code=%d stderr=%q", input, run.code, run.stderr)
			}
		}
		assertResetPasswordChangedNothing(t, before, fixture.state(t))
	})
}

func TestResetPasswordListsLocalAccountsWithoutAKnownUsername(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	before := fixture.state(t)

	for name, test := range map[string]struct {
		args      []string
		wantError string
	}{
		"missing username": {nil, "Error: a username is required.\n"},
		"unknown username": {[]string{"root"}, "Error: no account named \"root\".\n"},
	} {
		t.Run(name, func(t *testing.T) {
			run := runResetPasswordPiped(t, nil, test.args...)
			if run.code != 1 || run.stderr != test.wantError {
				t.Fatalf("code=%d stderr=%q", run.code, run.stderr)
			}
			lines := strings.Split(strings.TrimRight(run.stdout, "\n"), "\n")
			if len(lines) != 6 || !strings.HasPrefix(lines[0], "Local accounts in ") || !strings.HasSuffix(lines[0], "depsilo.db:") {
				t.Fatalf("stdout = %q", run.stdout)
			}
			got := make([][]string, 0, 4)
			for _, line := range lines[1:5] {
				got = append(got, strings.Fields(line))
			}
			want := [][]string{
				{"USERNAME", "ROLE", "STATE"},
				{"admin", "admin", "enabled"},
				{"viewer", "readonly", "enabled"},
				{"retired", "admin", "disabled"},
			}
			for index := range want {
				if strings.Join(got[index], " ") != strings.Join(want[index], " ") {
					t.Fatalf("account line %d = %q, want %q; stdout=%q", index, got[index], want[index], run.stdout)
				}
			}
			if strings.Contains(run.stdout, "oidc_4f2a") {
				t.Fatalf("an account without a local password is listed as resettable: %q", run.stdout)
			}
			if lines[5] != "Run: depsilo reset-password <username>" {
				t.Fatalf("last line = %q", lines[5])
			}
			assertResetPasswordChangedNothing(t, before, fixture.state(t))
		})
	}
}

func TestResetPasswordRefusesAccountsItMustNotChange(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	before := fixture.state(t)

	for username, want := range map[string]string{
		"oidc_4f2a": "Error: account oidc_4f2a is managed by an external identity provider (oidc). Such accounts sign in through their provider and have no local password.\n",
		"retired":   "Error: account retired is disabled. This command does not enable accounts.\n",
	} {
		t.Run(username, func(t *testing.T) {
			run := runResetPasswordPiped(t, nil, username)
			if run.code != 1 || run.stderr != want || run.stdout != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", run.code, run.stdout, run.stderr)
			}
			assertResetPasswordChangedNothing(t, before, fixture.state(t))
		})
	}
}

func TestResetPasswordRefusesWhileAServerHoldsTheDatabase(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	before := fixture.state(t)

	// A running server holds this lease for its whole lifetime.
	lease, err := backup.HoldDatabase(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	run := runResetPasswordPiped(t, nil, "admin")
	if closeErr := lease.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if run.code != 1 || run.stdout != "" ||
		!strings.HasPrefix(run.stderr, "Error: Depsilo is running; stop it first, then run the command again.\n"+
			"If no server is running, another depsilo restore or reset-password is using the database.\n") {
		t.Fatalf("code=%d stdout=%q stderr=%q", run.code, run.stdout, run.stderr)
	}
	assertResetPasswordChangedNothing(t, before, fixture.state(t))

	// Once the server has stopped, the same command succeeds.
	input := resetTestNewPassword + "\n"
	if run := runResetPasswordPiped(t, &input, "admin"); run.code != 0 {
		t.Fatalf("after the lease was released: code=%d stderr=%q", run.code, run.stderr)
	}
}

// A server could otherwise start while the operator sits at the prompt, and
// the command would then write to a database a live server is using.
func TestResetPasswordHoldsTheDatabaseUntilTheChangeIsCommitted(t *testing.T) {
	fixture := newResetPasswordFixture(t)

	var attempts []error
	var stdout, stderr strings.Builder
	code := resetPassword([]string{"admin"}, resetPasswordIO{
		stdin:      failingReader{t},
		stdout:     &stdout,
		stderr:     &stderr,
		isTerminal: func() bool { return true },
		readPassword: func(string) (string, error) {
			// What a server start does first.
			lease, err := backup.HoldDatabase(fixture.databasePath)
			if err == nil {
				_ = lease.Close()
			}
			attempts = append(attempts, err)
			return resetTestNewPassword, nil
		},
		osUser:       func() string { return resetTestOSUser },
		effectiveUID: os.Geteuid,
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if len(attempts) != 2 {
		t.Fatalf("prompts = %d, want 2", len(attempts))
	}
	for index, err := range attempts {
		if !errors.Is(err, backup.ErrDatabaseInUse) {
			t.Fatalf("at prompt %d a server could take the database: %v", index+1, err)
		}
	}
	// Afterwards the server can start.
	lease, err := backup.HoldDatabase(fixture.databasePath)
	if err != nil {
		t.Fatalf("the command did not release the database: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

// A failed write must leave nothing behind: neither a new password without
// its audit record, nor the statement with the new hash on standard output,
// where the database query logger prints failed statements.
func TestResetPasswordChangesNothingAndPrintsNoHashWhenAWriteFails(t *testing.T) {
	for name, trigger := range map[string]string{
		"account update": "CREATE TRIGGER block_users BEFORE UPDATE ON users BEGIN SELECT RAISE(ABORT, 'users blocked'); END",
		"audit record":   "CREATE TRIGGER block_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT, 'audit blocked'); END",
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newResetPasswordFixture(t)
			fixture.withDatabase(t, func(database *gorm.DB) {
				if err := database.Exec(trigger).Error; err != nil {
					t.Fatal(err)
				}
			})
			before := fixture.state(t)

			var run resetPasswordRun
			processOutput := captureStdout(t, func() {
				input := resetTestNewPassword + "\n"
				run = runResetPasswordPiped(t, &input, "admin")
			})
			if run.code != 1 || run.stdout != "" || !strings.Contains(run.stderr, "blocked") ||
				!strings.HasSuffix(run.stderr, "; nothing was changed.\n") {
				t.Fatalf("code=%d stdout=%q stderr=%q", run.code, run.stdout, run.stderr)
			}
			assertResetPasswordChangedNothing(t, before, fixture.state(t))
			if everything := processOutput + run.stdout + run.stderr; strings.Contains(everything, "$2a$") || strings.Contains(everything, resetTestNewPassword) {
				t.Fatalf("output carries the password or its hash: %q", everything)
			}
		})
	}
}

func TestResetPasswordRefusesADatabaseOfAnotherSchemaVersion(t *testing.T) {
	for name, test := range map[string]struct {
		change string
		want   []string
	}{
		"older database": {
			change: "DELETE FROM schema_migrations WHERE version = (SELECT MAX(version) FROM schema_migrations)",
			want: []string{
				"older than this Depsilo binary expects",
				"Start this Depsilo version once so it migrates the database, stop it, then run the command again",
				"or use the Depsilo binary that last ran this instance",
			},
		},
		"newer database": {
			change: "INSERT INTO schema_migrations (version, name, applied_at) SELECT MAX(version) + 1, 'future', CURRENT_TIMESTAMP FROM schema_migrations",
			want: []string{
				"newer than this Depsilo binary supports",
				"Run reset-password with the Depsilo binary that matches the database.",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newResetPasswordFixture(t)
			fixture.withDatabase(t, func(database *gorm.DB) {
				if err := database.Exec(test.change).Error; err != nil {
					t.Fatal(err)
				}
			})
			before := fixture.state(t)
			if before.schemaVersion == db.CurrentSchemaVersion {
				t.Fatal("the fixture did not change the recorded schema version")
			}

			run := runResetPasswordPiped(t, nil, "admin")
			if run.code != 1 || run.stdout != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", run.code, run.stdout, run.stderr)
			}
			for _, want := range test.want {
				if !strings.Contains(run.stderr, want) {
					t.Errorf("stderr does not contain %q: %q", want, run.stderr)
				}
			}
			// In particular the command did not migrate the database.
			assertResetPasswordChangedNothing(t, before, fixture.state(t))
		})
	}
}

func TestResetPasswordNeedsAnExistingDatabaseAndConfig(t *testing.T) {
	t.Run("missing database", func(t *testing.T) {
		fixture := newResetPasswordFixture(t)
		if err := os.Remove(fixture.databasePath); err != nil {
			t.Fatal(err)
		}
		for _, sidecar := range []string{"-wal", "-shm"} {
			_ = os.Remove(fixture.databasePath + sidecar)
		}
		run := runResetPasswordPiped(t, nil, "admin")
		if run.code != 1 || !strings.Contains(run.stderr, "Error: no Depsilo database at ") {
			t.Fatalf("code=%d stderr=%q", run.code, run.stderr)
		}
		if _, err := os.Stat(fixture.databasePath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("the command created a database")
		}
	})
	t.Run("missing DEPSILO_CONFIG file", func(t *testing.T) {
		// Without the refusal the command falls back to the default state
		// directory, where another stopped instance may live.
		fixture := newResetPasswordFixture(t)
		defaultDatabase := filepath.Join(fixture.home, ".depsilo", "data", "depsilo.db")
		if err := os.MkdirAll(filepath.Dir(defaultDatabase), 0o700); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(fixture.databasePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(defaultDatabase, content, 0o600); err != nil {
			t.Fatal(err)
		}
		missing := filepath.Join(fixture.home, "typo.toml")
		t.Setenv("DEPSILO_CONFIG", missing)
		run := runResetPasswordPiped(t, nil, "admin")
		if run.code != 1 || run.stdout != "" || !strings.Contains(run.stderr, "Error: DEPSILO_CONFIG names the config file ") ||
			!strings.Contains(run.stderr, "typo.toml") || !strings.Contains(run.stderr, "which does not exist") {
			t.Fatalf("code=%d stdout=%q stderr=%q", run.code, run.stdout, run.stderr)
		}
	})
	t.Run("missing --config file", func(t *testing.T) {
		fixture := newResetPasswordFixture(t)
		before := fixture.state(t)
		missing := filepath.Join(fixture.home, "typo.toml")
		run := runResetPasswordPiped(t, nil, "--config", missing, "admin")
		if run.code != 1 || !strings.Contains(run.stderr, "does not exist") {
			t.Fatalf("code=%d stderr=%q", run.code, run.stderr)
		}
		assertResetPasswordChangedNothing(t, before, fixture.state(t))
		if os.Getenv("DEPSILO_CONFIG") != fixture.configPath {
			t.Fatal("the command left DEPSILO_CONFIG changed")
		}
	})
}

// The server accepts SQLite connection options after "?" in the DSN and takes
// its lease on the file in front of them; the command must find the same file
// and the same lease.
func TestResetPasswordAcceptsADSNWithConnectionOptions(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	before := fixture.state(t)
	dsn := fixture.databasePath + "?_pragma=foreign_keys(1)"
	t.Setenv("DEPSILO_DATABASE_DSN", dsn)

	lease, err := backup.HoldDatabase(dsn)
	if err != nil {
		t.Fatal(err)
	}
	run := runResetPasswordPiped(t, nil, "admin")
	if closeErr := lease.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if run.code != 1 || !strings.HasPrefix(run.stderr, "Error: Depsilo is running; stop it first, then run the command again.\n") {
		t.Fatalf("while a server holds the database: code=%d stderr=%q", run.code, run.stderr)
	}
	assertResetPasswordChangedNothing(t, before, fixture.state(t))

	input := resetTestNewPassword + "\n"
	run = runResetPasswordPiped(t, &input, "admin")
	if run.code != 0 {
		t.Fatalf("code=%d stderr=%q", run.code, run.stderr)
	}
	if !strings.Contains(run.stdout, "depsilo.db.\n") || strings.Contains(run.stdout, "_pragma") {
		t.Fatalf("stdout does not name the database file alone: %q", run.stdout)
	}
	if !passwordMatches(fixture.state(t).user(t, "admin").PasswordHash, resetTestNewPassword) {
		t.Fatal("the stored hash does not match the new password")
	}
	if rows := resetPasswordAuditRows(t, fixture); len(rows) != 1 {
		t.Fatalf("audit rows for the reset = %d, want 1", len(rows))
	}
}

// The options are not only cut off to find the file: the database is opened
// with them. A connection the options make read-only cannot store a password.
func TestResetPasswordOpensTheDatabaseWithTheDSNsConnectionOptions(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	before := fixture.state(t)
	t.Setenv("DEPSILO_DATABASE_DSN", fixture.databasePath+"?_pragma=query_only(1)")

	input := resetTestNewPassword + "\n"
	run := runResetPasswordPiped(t, &input, "admin")
	if run.code != 1 || run.stdout != "" || !strings.Contains(run.stderr, "Error: update account: ") ||
		!strings.Contains(run.stderr, "nothing was changed") {
		t.Fatalf("with a read-only connection: code=%d stdout=%q stderr=%q", run.code, run.stdout, run.stderr)
	}
	assertResetPasswordChangedNothing(t, before, fixture.state(t))
}

func TestResetPasswordRefusesADatabaseThatIsNotAPlainFile(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	before := fixture.state(t)
	fileURI := "file:" + filepath.ToSlash(fixture.databasePath)
	for _, dsn := range []string{fileURI, ":memory:", " " + fileURI, "\t" + fileURI + " ", " :memory: "} {
		t.Setenv("DEPSILO_DATABASE_DSN", dsn)
		run := runResetPasswordPiped(t, nil, "admin")
		if run.code != 1 || run.stdout != "" ||
			!strings.Contains(run.stderr, "needs a SQLite database at a filesystem path and does not support file: URIs or in-memory databases") {
			t.Fatalf("dsn %q: code=%d stdout=%q stderr=%q", dsn, run.code, run.stdout, run.stderr)
		}
	}
	assertResetPasswordChangedNothing(t, before, fixture.state(t))
}

// Run as root (with sudo, usually) the command creates nothing the service
// user cannot open: the lease gives what it creates to the owner of the
// database directory. The operator is told that, and that lock files which
// already exist keep their owner, instead of the warning about new files.
func TestResetPasswordAsRootSaysWhoGetsTheFilesItCreates(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	info, err := os.Stat(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, known := fileOwnerUID(info)
	if !known || owner == 0 {
		t.Skip("needs a database that belongs to a user other than root")
	}

	var stdout, stderr strings.Builder
	code := resetPassword([]string{"admin"}, resetPasswordIO{
		stdin:        strings.NewReader(resetTestNewPassword + "\n"),
		stdout:       &stdout,
		stderr:       &stderr,
		isTerminal:   func() bool { return false },
		readPassword: func(string) (string, error) { return "", errors.New("unexpected prompt") },
		osUser:       func() string { return resetTestOSUser },
		effectiveUID: func() int { return 0 },
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	want := "Note: the database belongs to uid " + strconv.Itoa(owner) + " and this command runs as root. Lock files it creates are given to the owner of the database directory; lock files that already exist keep their owner.\n"
	if !strings.HasPrefix(stderr.String(), want) || strings.Contains(stderr.String(), "Warning") || strings.Contains(stderr.String(), "wrong user") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// Run as another unprivileged user the command can leave lock files the
// service user cannot open; the operator is told before anything changes.
func TestResetPasswordWarnsWhenItDoesNotRunAsTheDatabaseOwner(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	info, err := os.Stat(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, known := fileOwnerUID(info)
	if !known {
		t.Skip("files have no numeric owner on this platform")
	}

	var stdout, stderr strings.Builder
	code := resetPassword([]string{"admin"}, resetPasswordIO{
		stdin:        strings.NewReader(resetTestNewPassword + "\n"),
		stdout:       &stdout,
		stderr:       &stderr,
		isTerminal:   func() bool { return false },
		readPassword: func(string) (string, error) { return "", errors.New("unexpected prompt") },
		osUser:       func() string { return resetTestOSUser },
		effectiveUID: func() int { return owner + 1 },
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	want := "Warning: the database belongs to uid " + strconv.Itoa(owner) + ", but this command runs as uid " + strconv.Itoa(owner+1) + "."
	if !strings.HasPrefix(stderr.String(), want) || !strings.Contains(stderr.String(), "run the command as the service user") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestResetPasswordTakesNoPasswordFromTheCommandLine(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	before := fixture.state(t)

	for _, args := range [][]string{
		{"--password", resetTestNewPassword, "admin"},
		{"--password=" + resetTestNewPassword, "admin"},
		{"admin", resetTestNewPassword},
		{"--json", "admin"},
		{"--config"},
		{"--config", "", "admin"},
	} {
		run := runResetPasswordPiped(t, nil, args...)
		if run.code != 1 || run.stdout != "" || !strings.Contains(run.stderr, "Usage: depsilo reset-password [--config PATH] [<username>]") {
			t.Fatalf("args %q: code=%d stdout=%q stderr=%q", args, run.code, run.stdout, run.stderr)
		}
		if strings.Contains(run.stderr, resetTestNewPassword) {
			t.Fatalf("args %q: the rejected password was echoed: %q", args, run.stderr)
		}
	}
	assertResetPasswordChangedNothing(t, before, fixture.state(t))
}

func TestResetPasswordHelpTouchesNoState(t *testing.T) {
	home := t.TempDir()
	clearDepsiloEnvironment(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(t.TempDir())

	for _, args := range [][]string{{"--help"}, {"-h"}, {"admin", "--help"}} {
		run := runResetPasswordPiped(t, nil, args...)
		if run.code != 0 || run.stderr != "" || !strings.HasPrefix(run.stdout, "Usage:\n    depsilo reset-password [--config PATH] [<username>]\n") {
			t.Fatalf("args %q: code=%d stdout=%q stderr=%q", args, run.code, run.stdout, run.stderr)
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("--help created state under the home directory: %v", entries)
	}
}

// Neither the injected streams nor the process's own standard output (where
// the database query logger writes) may ever carry the new password or its
// stored hash.
func TestResetPasswordNeverPrintsThePassword(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	var runs []resetPasswordRun
	processOutput := captureStdout(t, func() {
		input := resetTestNewPassword + "\n"
		runs = append(runs,
			runResetPasswordOnTerminal(t, []string{resetTestNewPassword, "another-" + resetTestNewPassword}, "admin"),
			runResetPasswordPiped(t, &input, "retired"),
			runResetPasswordPiped(t, &input, "admin"),
			runResetPasswordOnTerminal(t, []string{resetTestNewPassword + "-2", resetTestNewPassword + "-2"}, "viewer"),
		)
	})
	if codes := []int{runs[0].code, runs[1].code, runs[2].code, runs[3].code}; codes[0] != 1 || codes[1] != 1 || codes[2] != 0 || codes[3] != 0 {
		t.Fatalf("exit codes = %v, want [1 1 0 0]", codes)
	}
	state := fixture.state(t)
	everything := processOutput
	for _, run := range runs {
		everything += run.stdout + run.stderr + strings.Join(run.prompts, "")
	}
	for _, secret := range []string{resetTestNewPassword, state.user(t, "admin").PasswordHash, state.user(t, "viewer").PasswordHash} {
		if strings.Contains(everything, secret) {
			t.Fatalf("output carries a password or its hash: %q", everything)
		}
	}
	for _, row := range resetPasswordAuditRows(t, fixture) {
		recorded := row.PackageName + row.Version + row.UserAgent + row.ClientIP + row.UpstreamURL + row.RequestID
		if strings.Contains(recorded, resetTestNewPassword) || strings.Contains(recorded, "$2a$") {
			t.Fatalf("the audit record carries a secret: %+v", row)
		}
	}
}

func TestResetPasswordAuditDetailNamesTheOSUserWithinTheColumn(t *testing.T) {
	fixture := newResetPasswordFixture(t)
	var stdout, stderr strings.Builder
	code := resetPassword([]string{"admin"}, resetPasswordIO{
		stdin:        strings.NewReader(resetTestNewPassword + "\n"),
		stdout:       &stdout,
		stderr:       &stderr,
		isTerminal:   func() bool { return false },
		readPassword: func(string) (string, error) { return "", errors.New("unexpected prompt") },
		osUser:       func() string { return "CORP\\operator\x1b[2J\n" + strings.Repeat("x", 300) },
		effectiveUID: os.Geteuid,
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	rows := resetPasswordAuditRows(t, fixture)
	if len(rows) != 1 {
		t.Fatalf("audit rows for the reset = %d, want 1", len(rows))
	}
	detail := rows[0].Version
	if !strings.HasPrefix(detail, "password reset from the local CLI by OS user CORP\\operator?[2J?xxx") || len([]rune(detail)) != 128 {
		t.Fatalf("audit detail = %q (%d characters)", detail, len([]rune(detail)))
	}
	for _, character := range detail {
		if unicode.IsControl(character) {
			t.Fatalf("audit detail carries a control character: %q", detail)
		}
	}
}
