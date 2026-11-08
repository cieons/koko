package cli_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/cieons/koko/internal/harness"
	"github.com/cieons/koko/internal/harness/sshhost"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/timing"
)

// lostSetup is a koko server reached through a forwarder, on a data
// directory and Root Key that a second koko server can start on, with the
// Host web1 and a device joined on the direct path.
type lostSetup struct {
	h   *harness.Harness
	fwd *harness.Forwarder
	// listen is the address the koko server listens on, behind fwd.
	listen           string
	dataDir, rootKey string
	timings          timing.Timings
	host             *sshhost.Host
	d                *harness.Device
	// detached is when the test saw the koko server Detach the Tunnel,
	// after the koko server did.
	detached time.Time
}

// startLost starts a lostSetup whose koko server takes server as its
// timing-constants hook and whose device takes device, its commands going
// through a Device Mux when mux is true and direct otherwise.
func startLost(t *testing.T, mux bool, server, device timing.Timings) *lostSetup {
	t.Helper()
	s := &lostSetup{listen: harness.FreeAddr(t), dataDir: t.TempDir(), rootKey: harness.NewRootKeyFile(t), timings: server}
	s.h = harness.Start(t, harness.NoInit(), harness.Listen(s.listen), harness.Timings(server), harness.DataDir(s.dataDir), harness.RootKeyFile(s.rootKey))
	s.fwd = harness.Forward(t, s.listen)
	init, err := s.h.Call(api.ServerInit, api.ServerInitRequest{Owner: harness.Owner, Address: s.fwd.Addr()})
	if err != nil {
		t.Fatalf("server.init: %v", err)
	}
	s.host = tunnelHost(t, s.h, "web1")
	if mux {
		s.d = harness.NewMuxDevice(t, device)
	} else {
		s.d = harness.NewDevice(t)
		s.d.UseTimings(device)
	}
	s.d.Koko("join", init.JoinCode.Code).WantSuccess(t)
	return s
}

// detach cuts the Device Connection beneath the SSH Session and keeps the
// device from reaching the koko server, until the koko server has Detached
// the SSH Session's Tunnel.
func (s *lostSetup) detach(t *testing.T) {
	t.Helper()
	// The cut falls on the koko server's next bytes, such as its probe of
	// the quiet device, which then finds nothing to reconnect to.
	s.fwd.To(recordTries(t).addr())
	s.fwd.CutWhen(func() bool { return true })
	wantDetached(t, s.h)
	s.detached = time.Now()
}

// keptGrace is the grace of the koko server whose kept ending expires.
const keptGrace = 1500 * time.Millisecond

// restart stops the koko server and starts another on its data directory,
// which the forwarder then leads to, as an upgrade's restart does.
func (s *lostSetup) restart(t *testing.T) {
	t.Helper()
	s.h.Stop()
	s.listen = harness.FreeAddr(t)
	s.h = harness.Start(t, harness.NoInit(), harness.Listen(s.listen), harness.Timings(s.timings), harness.DataDir(s.dataDir), harness.RootKeyFile(s.rootKey))
}

// wantLostLine fails the test unless the line is koko's one line for an
// SSH Session to web1 that could not Resume, ending with why after a colon,
// or with no reason when why is empty.
func wantLostLine(t *testing.T, what, line, why string) {
	t.Helper()
	ending := "."
	if why != "" {
		ending = ": " + why + "."
	}
	re := regexp.MustCompile(`^koko: lost the connection to the koko server at \d\d:\d\d:\d\d; the SSH Session to web1 could not be resumed` + regexp.QuoteMeta(ending) + "\n$")
	if !re.MatchString(line) {
		t.Errorf("%s: koko printed %q, want the line that it could not Resume, ending %q", what, line, ending)
	}
}

// lostCases are the ways a Detached Tunnel cannot Resume: what happens
// while it is Detached, why the line says it could not Resume, and the
// reason the koko server logs for the refused resume, if it gets one.
var lostCases = []struct {
	name string
	// server and device change the koko server's and the device's timings
	// from the resumption tests'.
	server, device func(*timing.Timings)
	// while happens while the Tunnel is Detached; then the device reaches
	// the koko server again, unless stayAway.
	while    func(t *testing.T, s *lostSetup)
	stayAway bool
	why      string
	// refused is the reason the koko server logs for the resume, as a
	// regular expression, or "" for none.
	refused string
}{
	{
		name:   "the grace ran out",
		server: func(tm *timing.Timings) { tm.Grace = 200 * time.Millisecond },
		while: func(t *testing.T, s *lostSetup) {
			waitServerLog(t, s.h, `msg="ssh session ended" .* reason="grace over"\n`)
		},
		refused: `"unknown tunnel"`,
	},
	{
		name:    "the koko server restarted",
		while:   func(t *testing.T, s *lostSetup) { s.restart(t) },
		refused: `"unknown tunnel"`,
	},
	{
		name: "access was withdrawn",
		while: func(t *testing.T, s *lostSetup) {
			s.h.Koko("host", "retire", "web1").WantSuccess(t)
			waitServerLog(t, s.h, `msg="ssh session ended" .* reason="host retired"\n`)
		},
		why:     "access to web1 was withdrawn: web1 was Retired",
		refused: `"host retired"`,
	},
	{
		name: "the Host closed the connection",
		while: func(t *testing.T, s *lostSetup) {
			s.host.DropConns()
			waitServerLog(t, s.h, `msg="ssh session ended" .* reason="Host closed"\n`)
		},
		why:     "web1 closed the connection",
		refused: `"Host closed"`,
	},
	{
		// The ended Tunnel's reason is kept only until its grace would have
		// ended, after which the koko server holds no such Tunnel. The grace
		// is long enough for the retire to come first under load, and the
		// device stays away until it would have ended.
		name:   "the reason was kept past the grace",
		server: func(tm *timing.Timings) { tm.Grace = keptGrace },
		while: func(t *testing.T, s *lostSetup) {
			s.h.Koko("host", "retire", "web1").WantSuccess(t)
			waitServerLog(t, s.h, `msg="ssh session ended" .* reason="host retired"\n`)
			time.Sleep(time.Until(s.detached.Add(keptGrace + 100*time.Millisecond)))
		},
		refused: `"unknown tunnel"`,
	},
	{
		// A Revoked device reaches the koko server no more, so it says why
		// itself.
		name: "the Device Session was Revoked",
		while: func(t *testing.T, s *lostSetup) {
			s.h.Koko("device", "revoke", "--last", harness.Owner+"/"+readClientConfig(t, s.d).Device).WantSuccess(t)
			waitServerLog(t, s.h, `msg="ssh session ended" .* reason=revoked\n`)
		},
		why: "this device's Device Session was Revoked",
	},
	{
		name:     "the device gave up",
		device:   func(tm *timing.Timings) { tm.ResumeGiveUp = 300 * time.Millisecond },
		while:    func(*testing.T, *lostSetup) {},
		stayAway: true,
		why:      "koko could not reach the koko server for 2 minutes",
	},
}

// When the Tunnel beneath an open SSH Session cannot Resume, koko tunnel
// says so in one line, with why when it is known, and fails, and ssh's
// connection ends: when the grace ran out, the koko server restarted, its
// access was withdrawn, its Host closed the connection, or its Device
// Session was Revoked while it was Detached, and once the device has spent
// timing.ResumeGiveUp awake without reaching the koko server.
func kokoTunnelSaysInOneLineWhyItCouldNotResume(t *testing.T) {
	for _, path := range []struct {
		name string
		mux  bool
	}{{"direct", false}, {"through the Device Mux", true}} {
		t.Run(path.name, func(t *testing.T) {
			for _, tc := range lostCases {
				t.Run(tc.name, func(t *testing.T) {
					server, device := resumeServer, resumeDevice
					if tc.server != nil {
						tc.server(&server)
					}
					if tc.device != nil {
						tc.device(&device)
					}
					s := startLost(t, path.mux, server, device)
					p := s.d.Proxy(nil, "tunnel", "web1.koko")
					c := sshOver(t, p, "web1.koko", gatewayHostKey(t, s.h))
					cut := time.Now()
					s.detach(t)
					tc.while(t, s)
					if !tc.stayAway {
						s.fwd.To(s.listen)
					}
					r := p.Wait()
					if r.Status != 1 {
						t.Errorf("koko tunnel exited %d with stderr %q, want 1", r.Status, r.Stderr)
					}
					wantLostLine(t, "koko tunnel", r.Stderr, tc.why)
					if err := c.Wait(); err == nil {
						t.Errorf("ssh's connection ended cleanly, want it lost")
					}
					if tc.refused != "" {
						waitServerLog(t, s.h, `msg="ssh session resume refused" .* reason=`+tc.refused+`\n`)
					}
					if tc.stayAway && time.Since(cut) < device.ResumeGiveUp {
						t.Errorf("koko tunnel gave up %v after the cut, before its %v", time.Since(cut), device.ResumeGiveUp)
					}
				})
			}
		})
	}
}

// koko exec prints the same line as koko tunnel when its Tunnel cannot
// Resume, and exits 255.
func kokoExecSaysInOneLineWhyItCouldNotResume(t *testing.T) {
	s := startLost(t, false, resumeServer, resumeDevice)
	p := s.d.Proxy(nil, "exec", "web1", "cat")
	waitServerLog(t, s.h, `msg="ssh session request" .* request=exec\n`)
	s.detach(t)
	s.h.Koko("host", "retire", "web1").WantSuccess(t)
	waitServerLog(t, s.h, `msg="ssh session ended" .* reason="host retired"\n`)
	s.fwd.To(s.listen)
	r := p.Wait()
	if r.Status != 255 {
		t.Errorf("koko exec exited %d with stderr %q, want 255", r.Status, r.Stderr)
	}
	wantLostLine(t, "koko exec", r.Stderr, "access to web1 was withdrawn: web1 was Retired")
}
