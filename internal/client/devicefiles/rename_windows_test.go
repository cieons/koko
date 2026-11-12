package devicefiles_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cieons/koko/internal/client/devicefiles"
)

// On Windows a rename over a file is refused while another handle holds the
// file open without FILE_SHARE_DELETE, which Go's os.Open never passes, or
// while another rename over it is under way, as when koko device revoke and
// the Device Mux write the revocation record at once (#183). Rename waits
// for the file to be let go of, and then replaces it.
func TestRenameWaitsForAReaderOfTheFileItReplaces(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "revoked.json")
	tmp := filepath.Join(dir, ".revoked.json-1")
	for p, data := range map[string]string{path: "old", tmp: "new"} {
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	if err := os.Rename(tmp, path); err == nil {
		t.Fatal("os.Rename replaced a file open for reading, which Windows refuses: this test no longer reproduces the refusal Rename waits out")
	}

	done := make(chan error, 1)
	go func() { done <- devicefiles.Rename(tmp, path) }()
	// The reader holds the file a moment, as one reading it would.
	select {
	case err := <-done:
		t.Fatalf("Rename returned while a reader held the file it replaces (%v), want it to wait for the reader", err)
	case <-time.After(200 * time.Millisecond):
	}
	_ = reader.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Rename once the reader let go: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Rename still waited 10 s after the reader let go")
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "new" {
		t.Errorf("after Rename the file holds %q (%v), want the new bytes", b, err)
	}
}
