package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/compat"
	"github.com/cieons/koko/internal/shared/serveraddr"
	"github.com/cieons/koko/internal/shared/weburl"
)

// serverJSON is the koko server's deployment, as koko server show prints
// it. It names the Root Key's file and id, never the key.
type serverJSON struct {
	// Address is the Server Address as stored: without a port, its port is
	// ListenPort.
	Address string `json:"address"`
	// ListenPort is the TCP port the koko server listens on.
	ListenPort int `json:"listen_port"`
	// HostKeyFingerprint is the host key devices pin, as OpenSSH prints its
	// SHA-256 fingerprint.
	HostKeyFingerprint string `json:"host_key_fingerprint"`
	// Version is the koko server's version.
	Version string `json:"version"`
	// RootKeyFile is the Root Key's file as the Owner keeps it, and RootKeyID
	// the id it names.
	RootKeyFile string `json:"root_key_file"`
	RootKeyID   string `json:"root_key_id"`
	// DataDir is the koko server's data directory.
	DataDir string `json:"data_dir"`
	// WebURL is the Web URL, null while it is not set.
	WebURL *string `json:"web_url"`
	// WebListen is the web listener's address, null while it is off.
	WebListen *string `json:"web_listen"`
	// UpdateCheck is the koko server's check for a new koko release: its
	// setting and what the checks found.
	UpdateCheck updateCheckJSON `json:"update_check"`
	// UpgradeWay is how the koko server is upgraded: "systemd", "image" or
	// "none".
	UpgradeWay string `json:"upgrade_way"`
	// LastUpgrade is the outcome of its last upgrade through the upgrade
	// helper, null while there is none.
	LastUpgrade *lastUpgradeJSON `json:"last_upgrade"`
}

// lastUpgradeJSON is the outcome of the koko server's last upgrade through
// the upgrade helper.
type lastUpgradeJSON struct {
	State      string  `json:"state"`
	From       string  `json:"from"`
	To         string  `json:"to"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	Message    *string `json:"message"`
}

// upgradeLine says how the koko server is upgraded, as koko server show
// prints it, by its upgrade_way.
func (s serverJSON) upgradeLine() string {
	switch s.UpgradeWay {
	case api.UpgradeWaySystemd:
		return "koko server upgrade, or the web UI"
	case api.UpgradeWayImage:
		// An image's tag is the release without its v (0.5.0), as .env
		// names it.
		version := " to the new release without its v"
		if l := s.UpdateCheck.Latest; l != nil && compat.Newer(*l, s.Version) {
			version = "=" + strings.TrimPrefix(*l, "v")
		}
		return "pull the new image: set KOKO_VERSION" + version + " in .env, then docker compose pull && docker compose up -d"
	case api.UpgradeWayNone:
		return "replace kokod and restart it"
	}
	// A way a newer koko server names, which this CLI does not know.
	return s.UpgradeWay
}

// updateCheckJSON is the koko server's check for a new koko release.
type updateCheckJSON struct {
	// Enabled is the update_check setting.
	Enabled bool `json:"enabled"`
	// CheckedAt is when the last check ended, null before any.
	CheckedAt *string `json:"checked_at"`
	// Latest is koko's latest release, as the last check that succeeded
	// found it, null while unknown.
	Latest *string `json:"latest"`
	// Error is why the last check failed, null when it succeeded.
	Error *string `json:"error"`
}

// line is the update check as koko server show prints it, set against the
// koko server's version: on or off, then when it last checked and what it
// found.
func (u updateCheckJSON) line(version string) string {
	state := "off"
	if u.Enabled {
		state = "on"
	}
	if u.CheckedAt == nil {
		return state + ", not checked yet"
	}
	at := *u.CheckedAt
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		at = t.UTC().Format("2006-01-02 15:04 UTC")
	}
	switch {
	case u.Error != nil:
		return fmt.Sprintf("%s, last at %s, which failed: %s", state, at, *u.Error)
	case u.Latest == nil:
		return fmt.Sprintf("%s, last at %s", state, at)
	case compat.Newer(*u.Latest, version):
		return fmt.Sprintf("%s, last at %s: %s is available", state, at, *u.Latest)
	}
	return fmt.Sprintf("%s, last at %s: %s is the latest release", state, at, *u.Latest)
}

func serverFromAPI(s api.Server) serverJSON {
	j := serverJSON{
		Address:            s.Address,
		ListenPort:         s.ListenPort,
		HostKeyFingerprint: s.HostKeyFingerprint,
		Version:            s.Version,
		RootKeyFile:        s.RootKeyFile,
		RootKeyID:          s.RootKeyID,
		DataDir:            s.DataDir,
		WebURL:             s.WebURL,
		WebListen:          s.WebListen,
		UpdateCheck: updateCheckJSON{
			Enabled:   s.UpdateCheck.Enabled,
			CheckedAt: s.UpdateCheck.CheckedAt,
			Latest:    s.UpdateCheck.Latest,
			Error:     s.UpdateCheck.Error,
		},
		UpgradeWay: s.UpgradeWay,
	}
	if l := s.LastUpgrade; l != nil {
		j.LastUpgrade = &lastUpgradeJSON{
			State: l.State, From: l.From, To: l.To, StartedAt: l.StartedAt, FinishedAt: l.FinishedAt, Message: l.Message,
		}
	}
	return j
}

func (s serverJSON) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, `Server Address:  %s
Listen port:     %d
Web listener:    %s
Web URL:         %s
Host key:        %s
Version:         %s
Root Key:        %s (%s)
Data directory:  %s
Update check:    %s
Upgrade:         %s
`, serveraddr.WithPort(s.Address, s.ListenPort), s.ListenPort, orNone(s.WebListen, "off"), orNone(s.WebURL, "not set"),
		s.HostKeyFingerprint, s.Version, s.RootKeyFile, s.RootKeyID, s.DataDir, s.UpdateCheck.line(s.Version), s.upgradeLine())
	return err
}

// orNone is *s, or none when s is nil.
func orNone(s *string, none string) string {
	if s == nil {
		return none
	}
	return *s
}

// serverChanged is what koko server set prints: the koko server's
// deployment as it left it, and, for people, what changed.
type serverChanged struct {
	serverJSON
	// address and webURL tell that the Server Address, or the Web URL,
	// changed, and updateCheck that the update_check setting was set.
	address, webURL, updateCheck bool
}

func (s serverChanged) Human(w io.Writer) error {
	var b strings.Builder
	if s.address {
		address := serveraddr.WithPort(s.Address, s.ListenPort)
		fmt.Fprintf(&b, `Changed the Server Address to %s.
The Join Codes issued from now on carry it; those issued before keep the address they carry.
A joined device takes it, once the koko server answers there, with:
koko connect %s
`, address, address)
	}
	switch {
	case !s.webURL:
	case s.WebURL == nil:
		b.WriteString("Cleared the Web URL. The web listener answers every request with a page naming koko server set web_url=https://… until it is set again.\n")
	default:
		fmt.Fprintf(&b, "Changed the Web URL to %s. The web listener answers only requests for it, as the reverse proxy passes them on.\n", *s.WebURL)
	}
	if s.webURL && s.WebListen == nil {
		b.WriteString("The web listener is off: turn it on with koko server install --web-listen 127.0.0.1:7823, or with KOKO_WEB_LISTEN in a container.\n")
	}
	switch {
	case !s.updateCheck:
	case s.UpdateCheck.Enabled:
		b.WriteString("Turned the update check on. The koko server checks for a new koko release a few minutes from now, then every 6 hours.\n")
	default:
		b.WriteString("Turned the update check off. The koko server no longer checks for a new koko release by itself; the Owner or an Administrator can still check at once.\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// serverSet is koko server set address=<host[:port]> web_url=<url>
// update_check=on|off and --unset web_url. It refuses a key it does not
// know, a value that is not a Server Address, a Web URL, on or off, and an
// unset of the Server Address, before any call.
func serverSet(ctx context.Context, in *Input) (serverChanged, error) {
	var req api.ServerSetRequest
	for _, p := range in.Pairs() {
		switch p.Key {
		case "address":
			if err := serveraddr.Check(p.Value); err != nil {
				return serverChanged{}, usagef("address= takes the Server Address, host or host:port: %v", err)
			}
			req.Address = new(p.Value)
		case "web_url":
			if _, err := weburl.Parse(p.Value); err != nil {
				return serverChanged{}, usagef("web_url= takes the Web URL, https://<host>[:<port>]: %v", err)
			}
			req.WebURL = new(p.Value)
		case "update_check":
			switch p.Value {
			case "on", "off":
				req.UpdateCheck = new(p.Value == "on")
			default:
				return serverChanged{}, usagef("update_check= takes on or off, not %q", p.Value)
			}
		default:
			return serverChanged{}, usagef("koko server set changes address=<host[:port]>, web_url=<https://host[:port]> and update_check=on|off; it has no setting %q", p.Key)
		}
	}
	for _, k := range in.Unset() {
		switch k {
		case "web_url":
		case "address":
			return serverChanged{}, usagef("the Server Address cannot be cleared: a koko server always has one; change it with address=<host[:port]>")
		case "update_check":
			return serverChanged{}, usagef("the update check is not cleared but turned off: koko server set update_check=off")
		default:
			return serverChanged{}, usagef("koko server set clears web_url alone; it has no setting %q", k)
		}
	}
	req.Unset = in.Unset()
	if req.Address == nil && req.WebURL == nil && req.UpdateCheck == nil && len(req.Unset) == 0 {
		return serverChanged{}, usagef("name the setting to change: koko server set address=<host[:port]>, web_url=<https://host[:port]>, update_check=on|off or --unset web_url")
	}
	resp, err := in.Call(ctx, api.ServerSet, req)
	if err != nil {
		return serverChanged{}, err
	}
	return serverChanged{
		serverJSON: serverFromAPI(resp.Server), address: req.Address != nil,
		webURL: req.WebURL != nil || len(req.Unset) > 0, updateCheck: req.UpdateCheck != nil,
	}, nil
}

// serverShow is koko server show: the koko server's deployment.
func serverShow(ctx context.Context, in *Input) (serverJSON, error) {
	resp, err := in.Call(ctx, api.ServerShow, api.ServerShowRequest{})
	if err != nil {
		return serverJSON{}, err
	}
	return serverFromAPI(resp.Server), nil
}
