package db

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestSQLiteConnectionDSNPreservesExistingOptions(t *testing.T) {
	const original = "file:depsilo.db?cache=shared&_pragma=foreign_keys%281%29"
	got := sqliteConnectionDSN(original)
	if !strings.HasPrefix(got, original+"&") {
		t.Fatalf("sqliteConnectionDSN()=%q does not preserve %q", got, original)
	}
	query, err := url.ParseQuery(got[strings.IndexByte(got, '?')+1:])
	if err != nil {
		t.Fatal(err)
	}
	if query.Get("cache") != "shared" {
		t.Fatalf("cache=%q want=shared", query.Get("cache"))
	}
	wantPragmas := map[string]bool{
		"foreign_keys(1)":     false,
		"busy_timeout(5000)":  false,
		"synchronous(NORMAL)": false,
	}
	for _, pragma := range query["_pragma"] {
		if _, ok := wantPragmas[pragma]; ok {
			wantPragmas[pragma] = true
		}
	}
	for pragma, found := range wantPragmas {
		if !found {
			t.Errorf("pragma %q missing from %q", pragma, got)
		}
	}
}

func TestSQLiteConnectionDSNHandlesTrailingQuerySeparators(t *testing.T) {
	for _, dsn := range []string{"depsilo.db?", "depsilo.db?cache=shared&"} {
		got := sqliteConnectionDSN(dsn)
		if strings.Contains(got, "?&") || strings.Contains(got, "&&") {
			t.Errorf("sqliteConnectionDSN(%q) produced malformed separators: %q", dsn, got)
		}
	}
}

func TestSQLiteConnectionLimitKeepsMemoryDatabasesOnOneConnection(t *testing.T) {
	for _, dsn := range []string{
		":memory:",
		":memory:?cache=shared",
		"file::memory:?cache=shared",
		"file:ephemeral?mode=memory&cache=shared",
	} {
		if got := sqliteConnectionLimit(dsn); got != 1 {
			t.Errorf("sqliteConnectionLimit(%q)=%d want=1", dsn, got)
		}
	}
	if got := sqliteConnectionLimit("data/depsilo.db?cache=shared"); got != 4 {
		t.Errorf("file sqlite limit=%d want=4", got)
	}
	if got := sqliteConnectionLimit("data/mode=memory.db"); got != 4 {
		t.Errorf("path text must not select memory mode: limit=%d want=4", got)
	}
}

func TestSQLiteDatabaseDirectoryIgnoresDSNOptions(t *testing.T) {
	for dsn, want := range map[string]string{
		"/data/depsilo.db": "/data",
		"/data/depsilo.db?_pragma=temp_store_directory('/tmp/x')":    "/data",
		"  /data/depsilo.db?cache=shared  ":                          "/data",
		"data/depsilo.db?cache=shared":                               "data",
		"depsilo.db":                                                 ".",
		"depsilo.db?_pragma=temp_store_directory('/tmp/x')":          ".",
		"file:/data/depsilo.db":                                      "/data",
		"file:/data/depsilo.db?_pragma=temp_store_directory('/t/x')": "/data",
		"file:///data/depsilo.db?cache=shared":                       "/data",
		"file://localhost/data/depsilo.db":                           "/data",
		"file:data/depsilo.db?mode=rwc":                              "data",
		"file:depsilo.db?cache=shared":                               ".",
		// Nothing is created for a database without a local file.
		"":                           "",
		":memory:":                   "",
		":memory:?cache=shared":      "",
		"file::memory:?cache=shared": "",
		"file:ephemeral?mode=memory&cache=shared": "",
		"file:/data/ephemeral?mode=memory":        "",
		"file://fileserver/share/depsilo.db":      "",
	} {
		if got := filepath.ToSlash(sqliteDatabaseDirectory(dsn)); got != want {
			t.Errorf("sqliteDatabaseDirectory(%q) = %q, want %q", dsn, got, want)
		}
	}
}

// Open creates the directory of the database file. Options in the DSN are not
// part of that path, even when one of them contains a path of its own.
func TestOpenSQLiteCreatesOnlyTheDatabaseDirectory(t *testing.T) {
	for _, form := range []string{"plain path", "file URI"} {
		t.Run(form, func(t *testing.T) {
			root := t.TempDir()
			scratch := filepath.Join(root, "scratch")
			if err := os.Mkdir(scratch, 0o700); err != nil {
				t.Fatal(err)
			}
			// The working directory is where a path derived from the wrong
			// part of the DSN would end up.
			workdir := filepath.Join(root, "workdir")
			if err := os.Mkdir(workdir, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(workdir)
			dataDirectory := filepath.Join(root, "state", "data")
			dsn := filepath.Join(dataDirectory, "depsilo.db") + "?_pragma=temp_store_directory('" + scratch + "')"
			if form == "file URI" {
				dsn = "file:" + filepath.ToSlash(dsn)
			}

			database, err := Open("sqlite", dsn)
			if err != nil {
				t.Fatal(err)
			}
			sqlDatabase, err := database.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDatabase.Close() })

			if info, err := os.Stat(filepath.Join(dataDirectory, "depsilo.db")); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("database file was not created in its directory: %v", err)
			}
			for _, directory := range []string{dataDirectory, workdir, scratch} {
				entries, err := os.ReadDir(directory)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if entry.IsDir() {
						t.Errorf("Open created the directory %q in %s from the DSN options", entry.Name(), directory)
					}
				}
			}
		})
	}
}

func TestOpenSQLiteConfiguresEveryPooledConnection(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "depsilo.db") +
		"?_pragma=synchronous(FULL)&_pragma=busy_timeout(1)"
	database, err := Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })

	if got := sqlDatabase.Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("MaxOpenConnections=%d want=4", got)
	}

	connections := make([]*sql.Conn, 0, 4)
	t.Cleanup(func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for range 4 {
		connection, err := sqlDatabase.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, connection)
	}

	for index, connection := range connections {
		var journalMode string
		var synchronous int
		var busyTimeout int
		if err := connection.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journalMode); err != nil {
			t.Fatal(err)
		}
		if err := connection.QueryRowContext(context.Background(), "PRAGMA synchronous").Scan(&synchronous); err != nil {
			t.Fatal(err)
		}
		if err := connection.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatal(err)
		}
		if journalMode != "wal" || synchronous != 1 || busyTimeout != 5000 {
			t.Errorf(
				"connection %d pragmas: journal_mode=%q synchronous=%d busy_timeout=%d",
				index+1,
				journalMode,
				synchronous,
				busyTimeout,
			)
		}
	}
}

// openCapturingQueryLog opens a migrated database whose query log goes to a
// file instead of the test's stdout. The logger binds os.Stdout when Open
// constructs it, so stdout only needs replacing for that call.
func openCapturingQueryLog(t *testing.T) (*gorm.DB, func() string) {
	t.Helper()
	capture, err := os.Create(filepath.Join(t.TempDir(), "query.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = capture.Close() })

	stdout := os.Stdout
	os.Stdout = capture
	database, err := Open("sqlite", filepath.Join(t.TempDir(), "depsilo.db"))
	os.Stdout = stdout
	if err != nil {
		t.Fatal(err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	return database, func() string {
		t.Helper()
		logged, err := os.ReadFile(capture.Name())
		if err != nil {
			t.Fatal(err)
		}
		return string(logged)
	}
}

func TestOpenLogsFailedStatementsButNotAHealthyStart(t *testing.T) {
	database, queryLog := openCapturingQueryLog(t)

	var user User
	if err := database.First(&user, "username = ?", "nobody").Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("lookup of a missing row error = %v, want ErrRecordNotFound", err)
	}
	// The SQLite driver's migrator asks for debug output while it reads index
	// lists; the migration above and this call must not print SQL traces.
	if err := database.Debug().First(&user, "username = ?", "nobody").Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("debug lookup of a missing row error = %v, want ErrRecordNotFound", err)
	}
	// Not "the log is empty": a slow statement on a loaded machine is reported
	// legitimately. Only the lookups and the migrator's traces must be absent.
	if logged := queryLog(); strings.Contains(logged, "record not found") || strings.Contains(logged, "nobody") || strings.Contains(logged, "PRAGMA_index_list") {
		t.Fatalf("migration and missing-row lookups logged:\n%s", logged)
	}

	// A real failure is still reported, which also proves the capture is live.
	if err := database.Exec("SELECT * FROM table_that_does_not_exist").Error; err == nil {
		t.Fatal("statement against a missing table succeeded")
	}
	logged := queryLog()
	if !strings.Contains(logged, "table_that_does_not_exist") || !strings.Contains(logged, "repository_test.go") {
		t.Fatalf("failed statement log = %q, want the statement and its call site", logged)
	}
	if strings.Contains(logged, "\x1b[") {
		t.Fatalf("failed statement log carries ANSI colour codes: %q", logged)
	}
}

func TestOpenLogsFailedStatementsWithoutTheirValues(t *testing.T) {
	database, queryLog := openCapturingQueryLog(t)

	const secret = "$2a$10$secretbcrypthashthatmustnotbelogged"
	first := User{Username: "admin", PasswordHash: secret, Role: "admin"}
	if err := database.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	// The unique username makes this INSERT fail with the hash among its values.
	duplicate := User{Username: "admin", PasswordHash: secret, Role: "admin"}
	if err := database.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate user insert succeeded")
	}
	logged := queryLog()
	if !strings.Contains(logged, "INSERT INTO `users`") || !strings.Contains(logged, "?") {
		t.Fatalf("failed insert log = %q, want the statement with placeholders", logged)
	}
	if strings.Contains(logged, secret) || strings.Contains(logged, "secretbcrypt") {
		t.Fatalf("failed insert log carries a statement value:\n%s", logged)
	}
}
