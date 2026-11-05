package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
)

// Peek runs fn in a read-only transaction on koko.db in dataDir as it is,
// so that `koko server install` can read the record before the koko server
// runs, and beside one that runs. It never creates, migrates, snapshots or
// writes the database, and makes no file in dataDir: it calls nothing, and
// returns nil, when dataDir holds no koko.db or one that no migration step
// has reached, which holds nothing.
func Peek(ctx context.Context, dataDir string, fn func(Tx) error) error {
	path := filepath.Join(dataDir, FileName)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	// Without a -wal file nothing has koko.db open, so it holds every
	// commit and is read as immutable, which takes no lock and makes no
	// -shm or -wal file. With one, it is read as the koko server reads it.
	_, err := os.Stat(path + "-wal")
	immutable := errors.Is(err, fs.ErrNotExist)
	db, err := sql.Open("sqlite", peekDSN(path, immutable))
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	defer func() { _ = db.Close() }()
	err = run(ctx, db, &sql.TxOptions{ReadOnly: true}, func(tx Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return err
		}
		if version == 0 {
			return nil
		}
		return fn(tx)
	})
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

// peekDSN opens the file read-only and query-only, immutable when asked,
// with the busy timeout of koko's own DSN and none of the settings that
// would write.
func peekDSN(path string, immutable bool) string {
	q := url.Values{"mode": {"ro"}}
	if immutable {
		q.Set("immutable", "1")
	}
	q.Add("_pragma", busyTimeout)
	q.Add("_pragma", "query_only(1)")
	u := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: q.Encode()}
	return u.String()
}
