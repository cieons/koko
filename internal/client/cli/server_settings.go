package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cieons/koko/internal/shared/api"
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
}

func serverFromAPI(s api.Server) serverJSON {
	return serverJSON{
		Address:            s.Address,
		ListenPort:         s.ListenPort,
		HostKeyFingerprint: s.HostKeyFingerprint,
		Version:            s.Version,
		RootKeyFile:        s.RootKeyFile,
		RootKeyID:          s.RootKeyID,
		DataDir:            s.DataDir,
		WebURL:             s.WebURL,
		WebListen:          s.WebListen,
	}
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
`, serveraddr.WithPort(s.Address, s.ListenPort), s.ListenPort, orNone(s.WebListen, "off"), orNone(s.WebURL, "not set"),
		s.HostKeyFingerprint, s.Version, s.RootKeyFile, s.RootKeyID, s.DataDir)
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
	// changed.
	address, webURL bool
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
	_, err := io.WriteString(w, b.String())
	return err
}

// serverSet is koko server set address=<host[:port]> web_url=<url> and
// --unset web_url. It refuses a key it does not know, a value that is not
// a Server Address or a Web URL, and an unset of the Server Address,
// before any call.
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
		default:
			return serverChanged{}, usagef("koko server set changes address=<host[:port]> and web_url=<https://host[:port]>; it has no setting %q", p.Key)
		}
	}
	for _, k := range in.Unset() {
		switch k {
		case "web_url":
		case "address":
			return serverChanged{}, usagef("the Server Address cannot be cleared: a koko server always has one; change it with address=<host[:port]>")
		default:
			return serverChanged{}, usagef("koko server set clears web_url alone; it has no setting %q", k)
		}
	}
	req.Unset = in.Unset()
	if req.Address == nil && req.WebURL == nil && len(req.Unset) == 0 {
		return serverChanged{}, usagef("name the setting to change: koko server set address=<host[:port]>, web_url=<https://host[:port]> or --unset web_url")
	}
	resp, err := in.Call(ctx, api.ServerSet, req)
	if err != nil {
		return serverChanged{}, err
	}
	return serverChanged{serverJSON: serverFromAPI(resp.Server), address: req.Address != nil, webURL: req.WebURL != nil || len(req.Unset) > 0}, nil
}

// serverShow is koko server show: the koko server's deployment.
func serverShow(ctx context.Context, in *Input) (serverJSON, error) {
	resp, err := in.Call(ctx, api.ServerShow, api.ServerShowRequest{})
	if err != nil {
		return serverJSON{}, err
	}
	return serverFromAPI(resp.Server), nil
}
