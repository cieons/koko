package cli_test

import (
	"net"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/timing"
)

// managedHost records an in-process Host as the Host named hostName, as the
// Local caller, and Enrolls it, so the Gateway logs in to it with the koko
// key.
func managedHost(t *testing.T, h *harness.Harness, hostName string) *sshhost.Host {
	t.Helper()
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, hostName, host)
	enroll(t, h, hostName)
	return host
}

// ranCommands returns the command of each exec request in reqs, in order.
func ranCommands(t *testing.T, reqs []sshhost.Request) []string {
	t.Helper()
	var commands []string
	for _, r := range reqs {
		if r.Type != "exec" {
			continue
		}
		var exec struct{ Command string }
		if err := ssh.Unmarshal(r.Payload, &exec); err != nil {
			t.Fatalf("the Host received an exec request it cannot read: %v", err)
		}
		commands = append(commands, exec.Command)
	}
	return commands
}

// wantRefused fails the test unless koko exec exited 255, printing nothing
// but the banner line on stderr.
func wantRefused(t *testing.T, r harness.Result, banner string) {
	t.Helper()
	if r.Status != 255 || r.Stdout != "" || r.Stderr != banner {
		t.Errorf("exit status %d, stdout %q, stderr %q; want 255 and only %q", r.Status, r.Stdout, r.Stderr, banner)
	}
}

// waitServerLog waits until the koko server's log holds a line matching
// pattern, and returns its submatches. It fails the test after 10 s.
func waitServerLog(t *testing.T, h *harness.Harness, pattern string) []string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	var m []string
	eventually(t, "the log holds a line matching "+pattern, func() bool {
		m = re.FindStringSubmatch(h.Log())
		return m != nil
	})
	return m
}

// A joined device reaches a Host over its Device Connection, as its User.
// The koko server logs the SSH Session's start and end, one line each, with
// their fields, and never the command's text.
func TestExecOnAJoinedDeviceIsLoggedWithoutItsCommand(t *testing.T) {
	h := harness.Start(t)
	managedHost(t, h, "web1")
	hostID := showHost(t, h.Koko, "web1").ID
	laptop, laptopID := joinAs(t, deviceAdd(t, h, "laptop"))

	const marker = "not-for-the-log-7f3a"
	r := laptop.KokoStdin("one two three\n", "exec", "web1", "wc -w;", "echo", marker, ">/dev/null;", "exit 7")
	if r.Status != 7 || strings.TrimSpace(r.Stdout) != "3" || r.Stderr != "" {
		t.Fatalf("exit status %d, stdout %q, stderr %q; want 7 and the word count", r.Status, r.Stdout, r.Stderr)
	}

	fields := ` user=alice device_session=` + laptopID + ` host=web1 host_id=` + hostID
	started := waitServerLog(t, h, `level=INFO msg="ssh session started" tunnel=([0-9a-f]{32})`+fields+` addr=127\.0\.0\.1:\d+\n`)
	ended := waitServerLog(t, h, `level=INFO msg="ssh session ended" tunnel=([0-9a-f]{32})`+fields+
		` duration=\d\S*s bytes_from_device=[1-9]\d* bytes_to_device=[1-9]\d* exit_status=7 reason="client closed"\n`)
	if started[1] != ended[1] {
		t.Errorf("the SSH Session started as Tunnel %s and ended as %s; want one Tunnel id", started[1], ended[1])
	}
	if strings.Contains(h.Log(), marker) {
		t.Errorf("the log holds the command's text:\n%s", h.Log())
	}
}

// On a device whose commands go through its Device Mux, koko exec's Tunnel
// rides the Device Mux's Device Connection too, and opens none of its own.
func TestExecGoesThroughTheDeviceMux(t *testing.T) {
	h, fwd, d := muxDevice(t, timing.Timings{})
	managedHost(t, h, "web1")

	r := d.KokoStdin("through the mux\n", "exec", "web1", "cat; exit 4")
	if r.Status != 4 || r.Stdout != "through the mux\n" || r.Stderr != "" {
		t.Errorf("exit status %d, stdout %q, stderr %q; want 4 and the stdin back", r.Status, r.Stdout, r.Stderr)
	}
	if n := fwd.Conns(); n != 2 {
		t.Errorf("the device opened %d connections, want 2: the join's, and the Device Mux's", n)
	}
}

// Every word after the Host is the command's, flags and --json included, as
// typed: koko exec takes no flag there.
func TestExecPassesEveryWordAfterTheHostToTheCommand(t *testing.T) {
	h := harness.Start(t)
	host := managedHost(t, h, "web1")
	reqs := len(host.Requests())

	r := h.Koko("exec", "web1", "echo", "--json", "-x", "key=value", "--help", "--")
	if r.Status != 0 || r.Stdout != "--json -x key=value --help --\n" || r.Stderr != "" {
		t.Errorf("exit status %d, stdout %q, stderr %q; want the words echoed", r.Status, r.Stdout, r.Stderr)
	}
	if got, want := ranCommands(t, host.Requests()[reqs:]), []string{"echo --json -x key=value --help --"}; !slices.Equal(got, want) {
		t.Errorf("the Host ran %q, want %q", got, want)
	}
}

// koko help says that koko exec takes no flag, not even --json, and where
// its command's words begin; koko skill says so for agents.
func TestHelpSaysKokoExecTakesNoFlags(t *testing.T) {
	r := harness.Run(t, offline(t), nil, "help", "exec")
	r.WantSuccess(t)
	if strings.Contains(r.Stdout, "Flags:") || !strings.Contains(r.Stdout, "It takes no flag, not even --json: every word after <host> is the command's") {
		t.Errorf("koko help exec = %q, want no flags and the command's words after <host>", r.Stdout)
	}
	r = harness.Run(t, offline(t), nil, "help")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, "Every command but koko exec and koko tunnel takes --json.") {
		t.Errorf("koko help = %q, want it to leave koko exec out of the commands that take --json", r.Stdout)
	}
	r = harness.Run(t, offline(t), nil, "skill")
	r.WantSuccess(t)
	if !strings.Contains(r.Stdout, "- Every command but `koko exec` and `koko tunnel` takes `--json`.") || !strings.Contains(r.Stdout, "every word after\n  `<host>` is the command's") {
		t.Errorf("koko skill does not say how koko exec takes its words:\n%s", r.Stdout)
	}
}

// koko exec exits 255 whenever koko itself fails, as ssh does, so no
// failure of its own passes for the remote command's status: a refused
// command line, a device that has not joined, a Host the Gateway cannot
// connect to. Each says why in one line, never as JSON.
func TestExecExits255WhenKokoItselfFails(t *testing.T) {
	h := harness.Start(t, harness.Timings(timing.Timings{HostConnectWait: time.Millisecond}))
	host := managedHost(t, h, "web1")
	_, deadPort, err := net.SplitHostPort(harness.FreeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	managedHost(t, h, "web2")
	h.Koko("host", "set", "web2", "port="+deadPort).WantSuccess(t)
	conns := host.Conns()

	for _, tc := range []struct {
		run  func(args ...string) harness.Result
		args []string
		says string
	}{
		{h.Koko, []string{"exec"}, "koko: koko exec needs <host>: write koko exec <host> <command>...\n"},
		{h.Koko, []string{"exec", "web1"}, "koko: koko exec needs <command>...: write koko exec <host> <command>...\n"},
		{h.Koko, []string{"exec", "--json", "web1", "true"}, "koko: koko exec takes no flags, not even --json: write koko exec <host> <command>...\n"},
		{h.Koko, []string{"--json", "exec", "web1", "true"}, "koko: koko exec takes no flags, not even --json: write koko exec <host> <command>...\n"},
		{h.Koko, []string{"exec", "", "true"}, "koko: <host> cannot be empty in koko exec\n"},
		{h.Koko, []string{"exec", "--help"}, "koko: there is no --help flag: run koko help exec\n"},
		{h.Koko, []string{"--help", "exec", "web1", "true"}, "koko: there is no --help flag: run koko help exec\n"},
		{h.Koko, []string{"exec", "web2", "true"}, "koko: koko could not connect to web2; this SSH Session did not start.\n"},
	} {
		wantRefused(t, tc.run(tc.args...), tc.says)
	}
	r := harness.NewDevice(t).Koko("exec", "web1", "true")
	if r.Status != 255 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, "koko: this device has not joined a koko server") {
		t.Errorf("on a device that has not joined: exit status %d, stdout %q, stderr %q; want 255 and why", r.Status, r.Stdout, r.Stderr)
	}
	if got := host.Conns(); got != conns {
		t.Errorf("web1 received %d connections, want none", got-conns)
	}
	waitServerLog(t, h, `level=WARN msg="ssh session refused" tunnel=[0-9a-f]{32} name=web2 device_session="unix socket" addr=\S+ reason="host connection failed" err=`)
}

// A command a signal ends has no exit status: koko exec exits 255, as ssh
// does, and names the signal, which the koko server logs too.
func TestExecExits255WhenASignalEndsTheCommand(t *testing.T) {
	h := harness.Start(t)
	managedHost(t, h, "web1")
	wantRefused(t, h.Koko("exec", "web1", "kill -TERM $$"), "koko: the command on web1 was ended by SIGTERM\n")
	waitServerLog(t, h, `msg="ssh session ended" .* exit_signal=TERM reason="client closed"\n`)
}

// koko exec forwards no signal: Ctrl-C ends the SSH Session at once, says
// so, and exits 255, as a failure of koko's own.
func TestExecEndsTheSSHSessionWhenInterrupted(t *testing.T) {
	h := harness.Start(t)
	managedHost(t, h, "web1")
	p := h.Background(nil, "exec", "web1", "sleep 30")
	waitServerLog(t, h, `msg="ssh session started"`)
	wantRefused(t, p.Stop(), "koko: interrupted, so koko ended the SSH Session to web1\n")
	waitServerLog(t, h, `msg="ssh session ended" .* reason="client closed"\n`)
}

// A koko server stop ends every SSH Session at once: koko exec exits 255 as
// soon as it loses its Tunnel, and the koko server logs the end.
func TestAKokoServerStopEndsTheSSHSession(t *testing.T) {
	h := harness.Start(t)
	managedHost(t, h, "web1")
	p := h.Background(nil, "exec", "web1", "sleep 30")
	waitServerLog(t, h, `msg="ssh session started"`)

	h.Stop()
	if r := p.Wait(); r.Status != 255 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, "koko: ") {
		t.Errorf("exit status %d, stdout %q, stderr %q; want 255 and why", r.Status, r.Stdout, r.Stderr)
	}
	waitServerLog(t, h, `msg="ssh session ended" .* reason=`)
}

// An SSH Session uses its Device Session, as a command does, which moves
// its last used time.
func TestExecUsesItsDeviceSession(t *testing.T) {
	h := harness.Start(t)
	managedHost(t, h, "web1")
	laptop, id := joinAs(t, deviceAdd(t, h, "laptop"))
	h.Advance(time.Hour)

	laptop.Koko("exec", "web1", "true").WantSuccess(t)
	l := deviceLs(t, h.Koko)
	i := slices.IndexFunc(l, func(d deviceSessionOut) bool { return d.ID == id })
	if want := h.Now().Format(time.RFC3339); i < 0 || l[i].LastUsedAt == nil || *l[i].LastUsedAt != want {
		t.Errorf("device ls lists %+v, want the laptop %s last used at %s", l, id, want)
	}
}

// Either form of the SSH Name reaches the Host: web1 and web1.koko.
func TestExecTakesEitherFormOfTheSSHName(t *testing.T) {
	h := harness.Start(t)
	managedHost(t, h, "web1")
	for _, sshName := range []string{"web1", "web1.koko"} {
		r := h.Koko("exec", sshName, "echo", "reached")
		if r.Status != 0 || r.Stdout != "reached\n" || r.Stderr != "" {
			t.Errorf("koko exec %s: exit status %d, stdout %q, stderr %q; want it to reach web1", sshName, r.Status, r.Stdout, r.Stderr)
		}
	}
}

// The Gateway refuses a name no Host holds and a Host in a Space the caller
// cannot act in with the same banner, so it tells nothing of Hosts outside
// their Spaces, and connects to no Host for either.
func TestExecRefusesAnUnknownNameAndAnUnseenHostAlike(t *testing.T) {
	h := harness.Start(t)
	host := managedHost(t, h, "web1")
	h.Koko("user", "invite", "bob").WantSuccess(t)
	bob := h.Join("bob")
	conns := host.Conns()

	unseen := bob.Koko("exec", "web1", "true")
	wantRefused(t, unseen, "koko: no Host is named \"web1\".\n")
	unknown := bob.Koko("exec", "ghost.koko", "true")
	wantRefused(t, unknown, "koko: no Host is named \"ghost\".\n")
	if strings.ReplaceAll(unknown.Stderr, "ghost", "web1") != unseen.Stderr {
		t.Errorf("an unknown name got %q and an unseen Host %q; want the same banner", unknown.Stderr, unseen.Stderr)
	}
	if got := host.Conns(); got != conns {
		t.Errorf("the Host received %d connections for a Member who cannot see it, want none", got-conns)
	}
}

// Only a Managed Host is reached: a Registered one, which koko cannot log in
// to yet, is refused, saying how to Enroll it, without a connection.
func TestExecRefusesARegisteredHost(t *testing.T) {
	h := harness.Start(t)
	host := sshhost.Start(t, sshhost.Password(hostPassword))
	addHostAt(t, h.Koko, "db1", host)

	wantRefused(t, h.Koko("exec", "db1", "true"),
		"koko: db1 is Registered: koko logs in to it only once it is Enrolled, with koko host enroll db1.\n")
	if host.Conns() != 0 {
		t.Errorf("the Host received %d connections, want none", host.Conns())
	}
}

// koko exec runs one command on a Managed Host through the Gateway: it
// forwards stdin, keeps stdout and stderr apart, allocates no pty and exits
// with the remote status. The Gateway logs in as the Host Account with the
// koko key.
func TestExecRunsOneCommandOnAManagedHost(t *testing.T) {
	h := harness.Start(t)
	host := managedHost(t, h, "web1")
	kokoKey := kokoKeyLine(t, host)
	logins, reqs := len(host.Logins()), len(host.Requests())

	r := h.KokoStdin("from stdin\n", "exec", "web1", "cat;", "echo", "to stderr", ">&2;", "exit", "3")
	if r.Status != 3 || r.Stdout != "from stdin\n" || r.Stderr != "to stderr\n" {
		t.Fatalf("exit status %d, stdout %q, stderr %q; want 3, the stdin, and the stderr apart", r.Status, r.Stdout, r.Stderr)
	}

	got := host.Requests()[reqs:]
	if commands, want := ranCommands(t, got), []string{"cat; echo to stderr >&2; exit 3"}; !slices.Equal(commands, want) {
		t.Errorf("the Host ran %q, want %q: the words joined by spaces, as ssh joins them", commands, want)
	}
	for _, req := range got {
		if req.Type == "pty-req" || req.Type == "shell" {
			t.Errorf("the Host received a %s request; koko exec asks for none", req.Type)
		}
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(kokoKey))
	if err != nil {
		t.Fatal(err)
	}
	want := sshhost.Login{Account: sshhost.Account, Method: "publickey", Key: ssh.FingerprintSHA256(key), OK: true}
	if got := host.Logins()[logins:]; !slices.Equal(got, []sshhost.Login{want}) {
		t.Errorf("the Host received the logins %+v, want one as the Host Account with the koko key", got)
	}
}
