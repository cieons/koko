package harness

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// A file system hands a removed file's inode to a later file, but when it
// does is its own business: ext4 waits some seconds, and APFS never does.
// So the test also checks what keeps the inode from being handed out, that
// the machine still holds the removed file open.
func TestANewFileIsNotGivenARemovedFilesOwner(t *testing.T) {
	t.Parallel()
	m := NewMachine(t)
	dir := m.Path("/etc/koko")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range 64 {
		old := filepath.Join(dir, "old")
		if err := os.WriteFile(old, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := m.Chown(old, "koko"); err != nil {
			t.Fatal(err)
		}
		removed, err := os.Stat(old)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(old); err != nil {
			t.Fatal(err)
		}
		if !m.holds(removed) {
			t.Fatalf("the machine does not hold removed file %d open, so a new file can get its inode and its owner", i)
		}
		file := filepath.Join(dir, "new"+strconv.Itoa(i))
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		owner, err := m.FileOwner(file, info)
		if err != nil {
			t.Fatal(err)
		}
		if owner != "root" {
			t.Fatalf("file %d, made after a file given to koko was removed, is owned by %q; want root, the default of its directory", i, owner)
		}
	}
}

func TestAFileKeepsItsOwnerAcrossARename(t *testing.T) {
	t.Parallel()
	m := NewMachine(t)
	dir := m.Path("/etc/koko")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tmp, file := filepath.Join(dir, "tmp"), filepath.Join(dir, "file")
	if err := os.WriteFile(tmp, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Chown(tmp, "koko"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, file); err != nil {
		t.Fatal(err)
	}
	if owner := m.Owner("/etc/koko/file"); owner != "koko" {
		t.Errorf("the renamed file is owned by %q; want koko", owner)
	}
}

// holds tells whether the machine holds the file info describes open.
func (m *Machine) holds(info os.FileInfo) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, o := range m.owners {
		if o.file == nil {
			continue
		}
		if held, err := o.file.Stat(); err == nil && os.SameFile(held, info) {
			return true
		}
	}
	return false
}
