// Package store is the koko server's SQLite database: koko.db in the data
// directory, opened with koko's settings and migrated to this binary's schema.
//
// Every read runs in a read-only transaction on a query-only connection, and
// every write in one IMMEDIATE transaction, both opened and finished here, so
// a caller never begins, commits or rolls back on its own.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// FileName is the database file inside the data directory.
const FileName = "koko.db"

// pragmas are the settings every connection gets, through the DSN that P0
// spec: install and run (#26) specifies. modernc's own defaults are a busy
// timeout of 0, foreign keys off and deferred transactions; synchronous=FULL
// syncs every commit, so a power cut never loses a write the koko server
// reported done.
var pragmas = []string{
	"busy_timeout(5000)",
	"journal_mode(WAL)",
	"foreign_keys(1)",
	"synchronous(FULL)",
}

// Store is an open koko.db: one pool of connections for writes, and one of
// query-only connections for reads, which modernc's read-only transactions
// alone do not stop from writing.
type Store struct {
	write *sql.DB
	read  *sql.DB
}

// Tx is what a transaction's function sees: it queries and executes, and the
// store alone commits or rolls back.
type Tx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Open opens koko.db in dataDir, creating the directory and the file when they
// are missing, and migrates it forward to this binary's schema.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory %s: %w", dataDir, err)
	}
	path := filepath.Join(dataDir, FileName)
	write, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	s := &Store{write: write}
	if err := s.migrate(ctx); err != nil {
		_ = write.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	// The read pool opens once the file exists in WAL mode, which a
	// query-only connection could not switch it to.
	if s.read, err = sql.Open("sqlite", dsn(path, true)); err != nil {
		_ = write.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return s, nil
}

// dsn is koko's DSN with writes beginning IMMEDIATE, or with every statement
// refused that would write. The path is escaped, so a data directory may hold
// any character.
func dsn(path string, queryOnly bool) string {
	q := url.Values{"_txlock": {"immediate"}}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	if queryOnly {
		q.Add("_pragma", "query_only(1)")
	}
	u := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: q.Encode()}
	return u.String()
}

// Close closes the database.
func (s *Store) Close() error {
	return errors.Join(s.read.Close(), s.write.Close())
}

// Read runs fn in a read-only transaction on a query-only connection, so a
// statement that would write fails.
func (s *Store) Read(ctx context.Context, fn func(Tx) error) error {
	return run(ctx, s.read, &sql.TxOptions{ReadOnly: true}, fn)
}

// Write runs fn in one IMMEDIATE transaction and commits it when fn returns
// nil. When fn fails, nothing it wrote remains, and Write returns its error.
func (s *Store) Write(ctx context.Context, fn func(Tx) error) error {
	return run(ctx, s.write, nil, fn)
}

func run(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(Tx) error) error {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
