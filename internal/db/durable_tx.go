package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"gorm.io/gorm"
)

// BeginDurableTransaction starts a transaction on one pinned SQLite
// connection after raising that connection's synchronous mode to FULL. The
// caller must invoke the returned release function after Commit or Rollback;
// it restores the normal connection setting and returns the connection to the
// pool. Pinning matters because PRAGMA synchronous is connection-scoped while
// db.Open intentionally keeps the rest of the pool at NORMAL for throughput.
func BeginDurableTransaction(ctx context.Context, database *gorm.DB) (*gorm.DB, func() error, error) {
	if database == nil {
		return nil, nil, errors.New("database is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if database.Dialector == nil {
		return nil, nil, errors.New("database dialector is nil")
	}
	if !strings.EqualFold(database.Dialector.Name(), "sqlite") {
		return nil, nil, fmt.Errorf("durable transaction is unsupported for database driver %q", database.Dialector.Name())
	}

	sqlDatabase, err := database.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("access database pool: %w", err)
	}
	if sqlDatabase == nil {
		return nil, nil, errors.New("database pool is nil")
	}
	connection, err := sqlDatabase.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("pin database connection: %w", err)
	}

	var originalSynchronous int
	if err := connection.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&originalSynchronous); err != nil {
		_ = connection.Close()
		return nil, nil, fmt.Errorf("read synchronous mode: %w", err)
	}
	if originalSynchronous < 0 || originalSynchronous > 3 {
		_ = connection.Close()
		return nil, nil, fmt.Errorf("SQLite reported unsupported synchronous mode %d", originalSynchronous)
	}
	release := durableConnectionRelease(connection, originalSynchronous)
	if _, err := connection.ExecContext(ctx, "PRAGMA synchronous=FULL"); err != nil {
		_ = release()
		return nil, nil, fmt.Errorf("enable FULL synchronous mode: %w", err)
	}
	var synchronous int
	if err := connection.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
		_ = release()
		return nil, nil, fmt.Errorf("verify FULL synchronous mode: %w", err)
	}
	if synchronous < 2 {
		_ = release()
		return nil, nil, fmt.Errorf("SQLite rejected FULL synchronous mode (reported %d)", synchronous)
	}

	session := database.Session(&gorm.Session{Context: ctx, NewDB: true})
	// *sql.Conn implements both gorm.ConnPool and gorm.TxBeginner. Assigning it
	// before Begin pins the resulting *sql.Tx to the connection whose pragma we
	// just raised.
	session.Statement.ConnPool = connection
	tx := session.Begin()
	if tx.Error != nil {
		_ = release()
		return nil, nil, fmt.Errorf("begin durable transaction: %w", tx.Error)
	}

	return tx, release, nil
}

func durableConnectionRelease(connection *sql.Conn, originalSynchronous int) func() error {
	var once sync.Once
	var releaseErr error
	return func() error {
		once.Do(func() {
			var errs []error
			pragma := "PRAGMA synchronous=" + strconv.Itoa(originalSynchronous)
			if _, err := connection.ExecContext(context.Background(), pragma); err != nil {
				errs = append(errs, fmt.Errorf("restore synchronous mode %d: %w", originalSynchronous, err))
			}
			if err := connection.Close(); err != nil {
				errs = append(errs, fmt.Errorf("release pinned database connection: %w", err))
			}
			releaseErr = errors.Join(errs...)
		})
		return releaseErr
	}
}
