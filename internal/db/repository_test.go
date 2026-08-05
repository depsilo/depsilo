package db

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
