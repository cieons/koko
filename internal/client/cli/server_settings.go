package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/serveraddr"
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
	}
}

func (s serverJSON) Human(w io.Writer) error {
	_, err := fmt.Fprintf(w, `Server Address:  %s
Listen port:     %d
Host key:        %s
Version:         %s
Root Key:        %s (%s)
Data directory:  %s
`, serveraddr.WithPort(s.Address, s.ListenPort), s.ListenPort, s.HostKeyFingerprint, s.Version, s.RootKeyFile, s.RootKeyID, s.DataDir)
	return err
}

// serverChanged is what koko server set prints: the koko server's
// deployment as it left it.
type serverChanged serverJSON

func (s serverChanged) Human(w io.Writer) error {
	address := serveraddr.WithPort(s.Address, s.ListenPort)
	_, err := fmt.Fprintf(w, `Changed the Server Address to %s.
The Join Codes issued from now on carry it; those issued before keep the address they carry.
A joined device takes it, once the koko server answers there, with:
koko connect %s
`, address, address)
	return err
}

// serverSet is koko server set address=<host[:port]>. It refuses a key it
// does not know and a value that is not a Server Address before any call.
func serverSet(ctx context.Context, in *Input) (serverChanged, error) {
	var req api.ServerSetRequest
	for _, p := range in.Pairs() {
		if p.Key != "address" {
			return serverChanged{}, usagef("koko server set changes address=<host[:port]>; it has no setting %q", p.Key)
		}
		if err := serveraddr.Check(p.Value); err != nil {
			return serverChanged{}, usagef("address= takes the Server Address, host or host:port: %v", err)
		}
		req.Address = new(p.Value)
	}
	if req.Address == nil {
		return serverChanged{}, usagef("name the setting to change: koko server set address=<host[:port]>")
	}
	resp, err := in.Call(ctx, api.ServerSet, req)
	if err != nil {
		return serverChanged{}, err
	}
	return serverChanged(serverFromAPI(resp.Server)), nil
}

// serverShow is koko server show: the koko server's deployment.
func serverShow(ctx context.Context, in *Input) (serverJSON, error) {
	resp, err := in.Call(ctx, api.ServerShow, api.ServerShowRequest{})
	if err != nil {
		return serverJSON{}, err
	}
	return serverFromAPI(resp.Server), nil
}
