package server_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/server"
	"github.com/cieons/koko/internal/server/store"
)

// A koko server refuses to start on a database a newer koko migrated, naming
// the schema version it needs, and opens no socket.
func TestStartRefusesADatabaseANewerKokoMigrated(t *testing.T) {
	dataDir := t.TempDir()
	newer := store.SchemaVersion() + 1
	st, err := store.Open(t.Context(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Write(t.Context(), func(tx store.Tx) error {
		_, err := tx.ExecContext(t.Context(), fmt.Sprintf("PRAGMA user_version = %d", newer))
		return err
	})
	if err := errors.Join(err, st.Close()); err != nil {
		t.Fatalf("play a newer koko: %v", err)
	}

	socket := filepath.Join(harness.ShortTempDir(t), "koko.sock")
	srv, err := server.Start(t.Context(), server.Config{
		DataDir:     dataDir,
		RootKeyPath: harness.NewRootKeyFile(t),
		Log:         io.Discard,
		SocketPath:  socket,
	})
	if err == nil {
		_ = srv.Close()
		t.Fatal("the koko server started on a database at a newer schema")
	}
	if want := fmt.Sprintf("schema version %d", newer); !strings.Contains(err.Error(), want) {
		t.Errorf("refusal %q does not name %q", err, want)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Errorf("the refused koko server left its socket: %v", err)
	}
}
