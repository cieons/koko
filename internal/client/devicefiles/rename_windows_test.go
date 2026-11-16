package devicefiles_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"

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

// On Windows a rename holds the file it puts in place open with delete
// access while it runs, and an open that does not share delete, as Go's
// never does, is refused meanwhile with a sharing violation, as the Device
// Mux's rewrite of koko's ssh_config refused a reader (#234). ReadFile
// waits for the rename to let go, and then reads the file.
func TestReadFileWaitsForARenameThatHoldsTheFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ssh_config")
	if err := os.WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	// The handle a rename holds: delete access, sharing everything.
	held, err := windows.CreateFile(name, windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(held) }()
	if _, err := os.ReadFile(path); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("os.ReadFile of a file a rename holds: %v, want a sharing violation: this test no longer reproduces the refusal ReadFile waits out", err)
	}

	type read struct {
		b   []byte
		err error
	}
	done := make(chan read, 1)
	go func() {
		b, err := devicefiles.ReadFile(path)
		done <- read{b, err}
	}()
	select {
	case r := <-done:
		t.Fatalf("ReadFile returned while a rename held the file (%q, %v), want it to wait for the rename", r.b, r.err)
	case <-time.After(200 * time.Millisecond):
	}
	_ = windows.CloseHandle(held)
	select {
	case r := <-done:
		if r.err != nil || string(r.b) != "new" {
			t.Errorf("ReadFile once the rename let go: %q (%v), want the file's bytes", r.b, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ReadFile still waited 10 s after the rename let go")
	}
}
