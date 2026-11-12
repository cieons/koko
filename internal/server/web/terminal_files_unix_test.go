//go:build unix

package web_test

import (
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/api"
)

// A download takes regular files alone, links followed, so a FIFO, or a
// link to one, is not_a_file, and never streams for ever.
func TestFilesDownloadRefusesAFIFO(t *testing.T) {
	t.Parallel()
	_, host, b, w := filesTerminal(t, sshhost.SFTP())
	home := host.Home()
	if err := syscall.Mkfifo(filepath.Join(home, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("pipe", filepath.Join(home, "pipe-link")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pipe", "pipe-link"} {
		a := filesPost(t, b, api.FilesDownloadPath, api.FilesDownloadRequest{Tunnel: w.ID, Path: home + "/" + name})
		wantAPIError(t, a, http.StatusConflict, "not_a_file")
	}
}
