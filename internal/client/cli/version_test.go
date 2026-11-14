package cli_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/client/cli"
	"github.com/cieons/koko/internal/harness"
)

var buildValue = regexp.MustCompile(`"(\w+)": "[^"]*"`)

// version prints the build offline: it needs no koko server.
func TestVersionPrintsTheBuild(t *testing.T) {
	t.Parallel()
	noServer := cli.Runner{SocketPath: filepath.Join(t.TempDir(), "none.sock")}
	r := harness.Run(t, noServer, nil, "version", "--json")
	r.WantSuccess(t)
	var v struct {
		Version  string `json:"version"`
		Revision string `json:"revision"`
		Go       string `json:"go"`
		Platform string `json:"platform"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &v, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("version --json = %s: %v", r.Stdout, err)
	}
	if v.Version == "" || v.Go != runtime.Version() || v.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Errorf("version --json = %+v", v)
	}

	// The values depend on the build; the golden file pins the fields.
	harness.Golden(t, "version.json", buildValue.ReplaceAllString(r.Stdout, `"$1": "<$1>"`))

	r = harness.Run(t, noServer, nil, "version")
	r.WantSuccess(t)
	if !strings.HasPrefix(r.Stdout, "koko "+v.Version+" (") {
		t.Errorf("version = %q", r.Stdout)
	}
}

// version --licenses prints the third-party notices koko embeds, offline,
// as text or in the build's JSON.
func TestVersionPrintsTheThirdPartyNotices(t *testing.T) {
	t.Parallel()
	notices, err := os.ReadFile("THIRD_PARTY_NOTICES")
	if err != nil {
		t.Fatal(err)
	}
	noServer := cli.Runner{SocketPath: filepath.Join(t.TempDir(), "none.sock")}
	r := harness.Run(t, noServer, nil, "version", "--licenses")
	r.WantSuccess(t)
	if r.Stdout != string(notices) || !strings.HasPrefix(r.Stdout, "Third-party notices for koko\n") {
		t.Errorf("version --licenses printed %d bytes, not THIRD_PARTY_NOTICES's %d", len(r.Stdout), len(notices))
	}

	r = harness.Run(t, noServer, nil, "version", "--licenses", "--json")
	r.WantSuccess(t)
	var v struct {
		Version  string `json:"version"`
		Licenses string `json:"licenses"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &v); err != nil {
		t.Fatalf("version --licenses --json: %v", err)
	}
	if v.Version == "" || v.Licenses != string(notices) {
		t.Errorf("version --licenses --json has version %q and %d bytes of licenses, want THIRD_PARTY_NOTICES's %d", v.Version, len(v.Licenses), len(notices))
	}
}
