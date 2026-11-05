package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cieons/koko/internal/server/store"
)

func open(t *testing.T, dir string) *store.Store {
	t.Helper()
	s, err := store.Open(t.Context(), dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return s
}

// The research DSN plus synchronous=FULL holds on every connection the store hands out.
func TestOpenSetsKokoPragmas(t *testing.T) {
	s := open(t, t.TempDir())
	want := map[string]string{
		"journal_mode": "wal",
		"foreign_keys": "1",
		"busy_timeout": "5000",
		"synchronous":  "2", // FULL
	}
	check := func(t *testing.T, tx store.Tx) {
		t.Helper()
		for pragma, value := range want {
			var got string
			if err := tx.QueryRowContext(t.Context(), "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatalf("read PRAGMA %s: %v", pragma, err)
			}
			if got != value {
				t.Errorf("PRAGMA %s = %s, want %s", pragma, got, value)
			}
		}
	}
	if err := s.Read(t.Context(), func(tx store.Tx) error { check(t, tx); return nil }); err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := s.Write(t.Context(), func(tx store.Tx) error { check(t, tx); return nil }); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// A write that reads and then writes would fail at once with SQLITE_BUSY in a
// deferred transaction; every one commits because writes begin IMMEDIATE.
func TestConcurrentReadThenWriteTransactionsAllCommit(t *testing.T) {
	s := open(t, t.TempDir())
	ctx := t.Context()
	err := s.Write(ctx, func(tx store.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE TABLE counter (n INTEGER NOT NULL) STRICT"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO counter (n) VALUES (0)")
		return err
	})
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}

	const writers, each = 16, 10
	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for range writers {
		wg.Go(func() {
			for range each {
				errs <- s.Write(ctx, func(tx store.Tx) error {
					var n int
					if err := tx.QueryRowContext(ctx, "SELECT n FROM counter").Scan(&n); err != nil {
						return err
					}
					_, err := tx.ExecContext(ctx, "UPDATE counter SET n = ?", n+1)
					return err
				})
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("read-then-write transaction: %v", err)
		}
	}

	var n int
	err = s.Read(ctx, func(tx store.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT n FROM counter").Scan(&n)
	})
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if n != writers*each {
		t.Errorf("counter = %d, want %d: some writes were lost", n, writers*each)
	}
}

// A write whose function fails leaves nothing behind.
func TestFailedWriteRollsBack(t *testing.T) {
	s := open(t, t.TempDir())
	ctx := t.Context()
	errFail := errors.New("the operation failed")
	err := s.Write(ctx, func(tx store.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE TABLE t (n INTEGER) STRICT"); err != nil {
			return err
		}
		return errFail
	})
	if !errors.Is(err, errFail) {
		t.Fatalf("write returned %v, want the function's error", err)
	}
	err = s.Read(ctx, func(tx store.Tx) error {
		_, err := tx.ExecContext(ctx, "SELECT * FROM t")
		return err
	})
	if err == nil {
		t.Error("table t exists after a failed write")
	}
}

// A read cannot write: its connections are query-only.
func TestReadCannotWrite(t *testing.T) {
	s := open(t, t.TempDir())
	ctx := t.Context()
	err := s.Read(ctx, func(tx store.Tx) error {
		_, err := tx.ExecContext(ctx, "CREATE TABLE t (n INTEGER) STRICT")
		return err
	})
	if err == nil {
		t.Fatal("a read created a table")
	}
	err = s.Write(ctx, func(tx store.Tx) error {
		_, err := tx.ExecContext(ctx, "CREATE TABLE t (n INTEGER) STRICT")
		return err
	})
	if err != nil {
		t.Errorf("a write after the refused read failed: %v", err)
	}
}

// The data directory may hold characters that mean something in a URI.
func TestOpenInDirectoryWithURICharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a b?c#d%20e")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	open(t, dir)
	if _, err := os.Stat(filepath.Join(dir, "koko.db")); err != nil {
		t.Errorf("koko.db is not in the data directory: %v", err)
	}
}

// Opening migrates to this binary's schema, and opening again finds nothing to do.
func TestOpenMigratesOnceAndRecordsUserVersion(t *testing.T) {
	dir := t.TempDir()
	for range 2 {
		s, err := store.Open(t.Context(), dir)
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		var version int
		err = s.Read(t.Context(), func(tx store.Tx) error {
			return tx.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version)
		})
		if err != nil {
			t.Fatalf("read user_version: %v", err)
		}
		if version != store.SchemaVersion() {
			t.Errorf("user_version = %d, want %d", version, store.SchemaVersion())
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
