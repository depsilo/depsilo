package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"unicode"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/audit"
	"depsilo/internal/backup"
	"depsilo/internal/config"
	"depsilo/internal/credential"
	"depsilo/internal/db"
)

// resetPasswordAuditAction marks a password set by `depsilo reset-password`.
// It is deliberately not the name of any Admin action: this change was made
// by someone with access to the host, not by a signed-in administrator.
const resetPasswordAuditAction = "user.cli_reset"

// maxPipedPasswordLine bounds the single line read from a non-terminal
// standard input. The password policy rejects anything above 72 bytes.
const maxPipedPasswordLine = 4096

const resetPasswordUsage = `Usage:
    depsilo reset-password [--config PATH] [<username>]

Set a new password for a local account, directly in the local database.
Depsilo must be stopped first: the command refuses to run while a server
uses the database. Run it as the service user, with the same configuration
and working directory as the server.

Without a username, or with one that does not exist, the command lists the
local accounts and exits 1.

The new password is never taken from a flag, an argument or an environment
variable, and is never printed. On a terminal the command asks for it twice
without echo. When standard input is not a terminal, it reads one line.

Flags:
    --config, -c PATH    Path to the instance's config.toml (default:
                         DEPSILO_CONFIG, then ./config.toml, /app/config.toml,
                         ~/.depsilo/config.toml)

Afterwards every signed-in session of the account is invalid. API tokens of
the account keep working. The change is written to the audit log.
Disabled accounts and accounts managed by an external identity provider are
refused. The database is not migrated: use the Depsilo version that last ran.

In Docker, stop the container and run the command in a one-off container
that uses the same volume (the image's entrypoint is the depsilo binary):
    docker stop <container>
    docker run --rm -it --volumes-from <container> <image> reset-password <username>
    docker start <container>
docker run does not copy the stopped container's environment: pass the same
-e, --env-file and --user options as the server container was started with.
In Docker Compose (the service's environment is applied automatically):
    docker compose stop depsilo
    docker compose run --rm depsilo reset-password <username>
    docker compose start depsilo
To pipe the password instead of typing it, replace -it with -i (Docker) or
add -T (Compose) and redirect standard input from a file.
`

// resetPasswordIO is everything `depsilo reset-password` reads from and
// writes to outside the database, so tests can drive it without a terminal.
type resetPasswordIO struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	// isTerminal reports whether standard input is an interactive terminal.
	isTerminal func() bool
	// readPassword shows prompt and reads one line without echo.
	readPassword func(prompt string) (string, error)
	// osUser names the operating-system user for the audit record.
	osUser func() string
	// effectiveUID is the numeric user the command acts as.
	effectiveUID func() int
}

func runResetPassword(args []string) int {
	return resetPassword(args, resetPasswordIO{
		stdin:        os.Stdin,
		stdout:       os.Stdout,
		stderr:       os.Stderr,
		isTerminal:   func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		readPassword: readTerminalPassword,
		osUser:       currentOSUser,
		effectiveUID: os.Geteuid,
	})
}

type resetPasswordOptions struct {
	help       bool
	configPath string
	configSet  bool
	username   string
}

func parseResetPasswordArgs(args []string) (resetPasswordOptions, error) {
	var options resetPasswordOptions
	positional := make([]string, 0, 1)
	flagsEnded := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case flagsEnded || arg == "" || arg == "-" || !strings.HasPrefix(arg, "-"):
			positional = append(positional, arg)
		case arg == "--":
			flagsEnded = true
		case arg == "--help" || arg == "-h":
			options.help = true
		case arg == "--config" || arg == "-c":
			if i+1 >= len(args) {
				return resetPasswordOptions{}, fmt.Errorf("%s requires a path", arg)
			}
			options.configPath, options.configSet = args[i+1], true
			i++
		case strings.HasPrefix(arg, "--config="):
			options.configPath, options.configSet = strings.TrimPrefix(arg, "--config="), true
		default:
			// Only the name: a value after "=" may be a password.
			name, _, _ := strings.Cut(arg, "=")
			return resetPasswordOptions{}, fmt.Errorf("unknown flag %q; the new password is read from the terminal or from standard input, never from a flag", name)
		}
	}
	if options.configSet && strings.TrimSpace(options.configPath) == "" {
		return resetPasswordOptions{}, errors.New("--config requires a non-empty path")
	}
	if len(positional) > 1 {
		return resetPasswordOptions{}, errors.New("expected at most one username; the new password is read from the terminal or from standard input, never from an argument")
	}
	if len(positional) == 1 {
		options.username = positional[0]
	}
	return options, nil
}

// resetPassword is the whole behavior of `depsilo reset-password`.
func resetPassword(args []string, streams resetPasswordIO) int {
	fail := func(format string, values ...any) int {
		fmt.Fprintf(streams.stderr, "Error: "+format+"\n", values...)
		return 1
	}

	options, err := parseResetPasswordArgs(args)
	if err != nil {
		fail("%v", err)
		fmt.Fprintln(streams.stderr, "Usage: depsilo reset-password [--config PATH] [<username>]")
		return 1
	}
	if options.help {
		fmt.Fprint(streams.stdout, resetPasswordUsage)
		return 0
	}

	// A named file that is missing would silently fall back to the default
	// state directory and reset a password in a database nobody named.
	if options.configSet {
		if info, err := os.Stat(options.configPath); err != nil || info.IsDir() {
			return fail("config file %q does not exist; pass the path of the instance's config.toml, or leave --config out to use DEPSILO_CONFIG and the default search", options.configPath)
		}
		previous, present := os.LookupEnv("DEPSILO_CONFIG")
		if err := os.Setenv("DEPSILO_CONFIG", options.configPath); err != nil {
			return fail("cannot select the local configuration")
		}
		defer func() {
			if present {
				_ = os.Setenv("DEPSILO_CONFIG", previous)
			} else {
				_ = os.Unsetenv("DEPSILO_CONFIG")
			}
		}()
	} else if named := os.Getenv("DEPSILO_CONFIG"); named != "" {
		if info, err := os.Stat(named); err != nil || info.IsDir() {
			return fail("DEPSILO_CONFIG names the config file %q, which does not exist; set it to the path of the instance's config.toml, pass --config, or unset it to use the default search", named)
		}
	}
	// Like restore and setup token, this command only reads the instance's
	// configuration: it must never create or rotate the signing key.
	cfg, err := config.LoadForRestore()
	if err != nil {
		return fail("cannot load the local configuration: %v; check --config or DEPSILO_CONFIG", err)
	}
	if cfg.Database.Driver != "sqlite" {
		return fail("reset-password supports sqlite databases, got %q", cfg.Database.Driver)
	}
	// The file rule below ignores surrounding space, so the refusal here and
	// the options handed to the open must do the same.
	dsn := strings.TrimSpace(cfg.Database.DSN)
	if dsn == "" || dsn == ":memory:" || strings.HasPrefix(dsn, "file:") {
		return fail("reset-password needs a SQLite database at a filesystem path and does not support file: URIs or in-memory databases; the configured database is %q", dsn)
	}
	// Like the server, treat what follows "?" as SQLite connection options:
	// they are not part of the file name, and the database is opened with
	// them. The file is found by the rule the database lease uses.
	databasePath, err := backup.DatabaseFilePath(dsn)
	if err != nil {
		return fail("resolve database path: %v", err)
	}
	connectionOptions := ""
	if index := strings.IndexByte(dsn, '?'); index >= 0 {
		connectionOptions = dsn[index:]
	}
	info, err := os.Stat(databasePath)
	if err != nil || !info.Mode().IsRegular() {
		return fail("no Depsilo database at %s; run the command as the service user, from the server's working directory, with the same configuration as the server: --config or DEPSILO_CONFIG, and DEPSILO_DATABASE_DSN if the server sets it", databasePath)
	}
	// As another unprivileged user the command can create lock and journal
	// files that the service user cannot open afterwards. Root creates none:
	// the lease gives the lock entries it creates to the owner of the
	// database directory, and SQLite does the same for its journal files.
	// Lock entries that already exist keep their owner, so ones an earlier
	// run as root left behind still stop the service, and the note says so.
	if owner, known := fileOwnerUID(info); known && streams.effectiveUID() != owner {
		if streams.effectiveUID() == 0 {
			fmt.Fprintf(streams.stderr, "Note: the database belongs to uid %d and this command runs as root. Lock files it creates are given to the owner of the database directory; lock files that already exist keep their owner.\n", owner)
		} else {
			fmt.Fprintf(streams.stderr, "Warning: the database belongs to uid %d, but this command runs as uid %d. If Depsilo cannot start afterwards, files created here belong to the wrong user; run the command as the service user instead (for example with sudo -u).\n", owner, streams.effectiveUID())
		}
	}

	// The server holds this lease for its whole lifetime. Holding it here
	// proves that no running server reads the old credential state, and keeps
	// one from starting until the change is committed.
	lease, err := backup.HoldDatabase(databasePath)
	if errors.Is(err, backup.ErrDatabaseInUse) {
		fail("Depsilo is running; stop it first, then run the command again.")
		fmt.Fprintln(streams.stderr, "If no server is running, another depsilo restore or reset-password is using the database.")
		fmt.Fprintf(streams.stderr, "Database: %s\n", databasePath)
		return 1
	}
	if err != nil {
		return fail("cannot lock the database at %s: %v", databasePath, err)
	}
	defer lease.Close()

	updated, code := resetPasswordInDatabase(databasePath, connectionOptions, options.username, streams)
	if code != 0 {
		return code
	}
	fmt.Fprintf(streams.stdout, "Password updated for account %s (role %s) in %s.\n", terminalSafe(updated.Username), terminalSafe(updated.Role), databasePath)
	fmt.Fprintln(streams.stdout, "Every signed-in session of this account is now invalid, including sessions signed in with the old password. Sign in again with the new password.")
	fmt.Fprintln(streams.stdout, "API tokens of this account were not changed and keep working.")
	fmt.Fprintln(streams.stdout, "You can start Depsilo again.")
	return 0
}

// resetPasswordInDatabase runs with the database lease held. It returns the
// updated account and 0, or prints the reason and returns 1.
func resetPasswordInDatabase(databasePath, connectionOptions, username string, streams resetPasswordIO) (db.User, int) {
	fail := func(format string, values ...any) (db.User, int) {
		fmt.Fprintf(streams.stderr, "Error: "+format+"\n", values...)
		return db.User{}, 1
	}

	database, err := db.Open("sqlite", databasePath+connectionOptions)
	if err != nil {
		return fail("open database: %v", err)
	}
	if pool, err := database.DB(); err == nil {
		defer pool.Close()
	}

	// This command never migrates: a migration belongs to a server start,
	// which also repairs and backfills what a version change requires.
	recorded, err := db.RecordedSchemaVersion(database)
	if err != nil {
		return fail("%v", err)
	}
	if recorded < db.CurrentSchemaVersion {
		return fail("the database at %s has schema version %d, older than this Depsilo binary expects (%d). Start this Depsilo version once so it migrates the database, stop it, then run the command again; or use the Depsilo binary that last ran this instance.", databasePath, recorded, db.CurrentSchemaVersion)
	}
	if recorded > db.CurrentSchemaVersion {
		return fail("the database at %s has schema version %d, newer than this Depsilo binary supports (%d). Run reset-password with the Depsilo binary that matches the database.", databasePath, recorded, db.CurrentSchemaVersion)
	}

	var users []db.User
	if err := database.Order("id").Find(&users).Error; err != nil {
		return fail("read accounts: %v", err)
	}
	var target *db.User
	if username != "" {
		for index := range users {
			if users[index].Username == username {
				target = &users[index]
				break
			}
		}
	}
	if target == nil {
		if username == "" {
			fmt.Fprintln(streams.stderr, "Error: a username is required.")
		} else {
			fmt.Fprintf(streams.stderr, "Error: no account named %s.\n", strconv.Quote(username))
		}
		printLocalAccounts(streams.stdout, databasePath, users)
		return db.User{}, 1
	}
	// The same conditions as password login and the Admin user update: an
	// account whose permissions an identity provider owns has no local
	// password, and a disabled account cannot sign in with any password.
	if !localAccount(*target) {
		return fail("account %s is managed by an external identity provider (%s). Such accounts sign in through their provider and have no local password.", terminalSafe(target.Username), terminalSafe(target.PermissionSource))
	}
	if !target.Enabled {
		return fail("account %s is disabled. This command does not enable accounts.", terminalSafe(target.Username))
	}

	if streams.isTerminal() {
		fmt.Fprintf(streams.stderr, "Setting a new password for account %s in %s. Press Ctrl-C to cancel.\n", terminalSafe(target.Username), databasePath)
	} else {
		// Without this line a person at `ssh host depsilo reset-password`
		// or `docker run -i` sees a silent wait and types into a visible line.
		fmt.Fprintf(streams.stderr, "Reading the new password for account %s from standard input, which is not a terminal: one line, and anything typed is visible.\n", terminalSafe(target.Username))
	}
	password, err := readNewPassword(target.Username, streams)
	if err != nil {
		return fail("%v; nothing was changed.", err)
	}
	// The same cost as the server uses for every stored password.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fail("cannot hash the new password; nothing was changed.")
	}

	// The query logger prints a slow or failed statement with its values to
	// standard output; the password hash must not appear there.
	quiet := database.Session(&gorm.Session{Logger: logger.Discard})
	err = quiet.Transaction(func(tx *gorm.DB) error {
		// Incrementing credential_version is what the Admin user update does
		// on a password change; signed-in sessions carry the old version.
		result := tx.Model(target).Updates(map[string]any{
			"password_hash":      string(hash),
			"credential_version": gorm.Expr("credential_version + ?", 1),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("expected to update one account, updated %d", result.RowsAffected)
		}
		return audit.RecordManagementEvent(context.Background(), tx, audit.ManagementEvent{
			Action:  resetPasswordAuditAction,
			Target:  fmt.Sprintf("user:%d", target.ID),
			Success: true,
			Detail:  resetPasswordAuditDetail(streams.osUser()),
		})
	})
	if err != nil {
		return fail("update account: %v; nothing was changed.", err)
	}
	return *target, 0
}

func localAccount(account db.User) bool {
	return account.PermissionSource == "" || account.PermissionSource == "local"
}

func printLocalAccounts(out io.Writer, databasePath string, users []db.User) {
	local := make([]db.User, 0, len(users))
	for _, account := range users {
		if localAccount(account) {
			local = append(local, account)
		}
	}
	if len(local) == 0 {
		fmt.Fprintf(out, "No local accounts exist in %s.\n", databasePath)
		fmt.Fprintln(out, "Start Depsilo and complete initial setup to create an administrator.")
		return
	}
	fmt.Fprintf(out, "Local accounts in %s:\n", databasePath)
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "  USERNAME\tROLE\tSTATE")
	for _, account := range local {
		state := "enabled"
		if !account.Enabled {
			state = "disabled"
		}
		fmt.Fprintf(table, "  %s\t%s\t%s\n", terminalSafe(account.Username), terminalSafe(account.Role), state)
	}
	_ = table.Flush()
	fmt.Fprintln(out, "Run: depsilo reset-password <username>")
}

// readNewPassword returns a password that satisfies the shared policy. Its
// errors never contain the password.
func readNewPassword(username string, streams resetPasswordIO) (string, error) {
	if !streams.isTerminal() {
		line, err := bufio.NewReader(io.LimitReader(streams.stdin, maxPipedPasswordLine)).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", errors.New("cannot read the new password from standard input")
		}
		// Only the line ending is removed: spaces are part of a passphrase.
		password := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if password == "" {
			return "", errors.New("standard input is not a terminal and carried no password; pipe the new password as one line, or run the command in a terminal (in Docker, add -it)")
		}
		if err := credential.CredentialPolicy.ValidatePassword(username, password); err != nil {
			return "", err
		}
		return password, nil
	}

	password, err := streams.readPassword(fmt.Sprintf("New password for %s: ", terminalSafe(username)))
	if err != nil {
		return "", errors.New("cannot read the new password from the terminal")
	}
	if password == "" {
		return "", errors.New("no password was entered")
	}
	if err := credential.CredentialPolicy.ValidatePassword(username, password); err != nil {
		return "", err
	}
	repeated, err := streams.readPassword("Repeat the new password: ")
	if err != nil {
		return "", errors.New("cannot read the new password from the terminal")
	}
	if repeated != password {
		return "", errors.New("the two passwords do not match")
	}
	return password, nil
}

// readTerminalPassword prompts on standard error, so standard output carries
// only the result, and reads one line from the terminal without echo.
func readTerminalPassword(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	original, err := term.GetState(fd)
	if err != nil {
		return "", err
	}
	restore := func() { _ = term.Restore(fd, original) }
	// A signal at the prompt would otherwise end the process with echo still
	// switched off in the operator's shell.
	signals := make(chan os.Signal, 1)
	finished := make(chan struct{})
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	defer func() {
		signal.Stop(signals)
		close(finished)
	}()
	go func() {
		select {
		case <-signals:
			restore()
			fmt.Fprintln(os.Stderr)
			os.Exit(1)
		case <-finished:
		}
	}()
	// term.ReadPassword switches echo off only when it starts to read, so an
	// answer sent the moment the prompt appears (a script driving the
	// terminal, or fast typing) would be displayed. Echo goes off before the
	// prompt is visible instead.
	if err := disableTerminalEcho(fd); err != nil {
		restore()
		return "", err
	}
	fmt.Fprint(os.Stderr, prompt)
	password, err := term.ReadPassword(fd)
	restore()
	fmt.Fprintln(os.Stderr)
	return string(password), err
}

func currentOSUser() string {
	if current, err := user.Current(); err == nil && current.Username != "" {
		return fmt.Sprintf("%s (uid %s)", current.Username, current.Uid)
	}
	if uid := os.Getuid(); uid >= 0 {
		return fmt.Sprintf("uid %d", uid)
	}
	return "unknown"
}

// resetPasswordAuditDetail fits the audit record's 128-character detail.
func resetPasswordAuditDetail(osUser string) string {
	detail := []rune("password reset from the local CLI by OS user " + printableOnly(osUser))
	if len(detail) > 128 {
		detail = detail[:128]
	}
	return string(detail)
}

func printableOnly(value string) string {
	return strings.Map(func(character rune) rune {
		if !unicode.IsPrint(character) {
			return '?'
		}
		return character
	}, value)
}

// terminalSafe quotes a stored name unless it is plain printable text, so a
// name cannot carry control sequences into the operator's terminal.
func terminalSafe(value string) string {
	if value == "" || value != printableOnly(value) || strings.ContainsAny(value, " \"") {
		return strconv.Quote(value)
	}
	return value
}
