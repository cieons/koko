package devicemux

import (
	"context"
	"time"

	"github.com/cieons/koko/internal/client/apiclient"
	"github.com/cieons/koko/internal/client/devicefiles"
	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/api"
)

// refreshBareNames keeps the bare names in koko's ssh_config following the
// record (P0 spec: reach, "Bare names"), so that a Host someone else
// records, or one in a Space the User is added to, becomes a bare name by
// itself: it refreshes them once the Device Mux holds its first Device
// Connection, at once whenever the koko server says on it that the Hosts
// the User sees may have changed (wire.RequestHostsChanged), and otherwise
// timing.BareNameRefresh after each refresh, the fallback for a koko server
// that sends no such notice; and once that Device Connection has ended,
// again on the next, since a notice sent while it held none is lost, and
// a refresh that failed is due.
// A refresh is no command: it neither keeps the Device Mux from idling out
// nor makes it reconnect, and waits for the next Device Connection when it
// holds none.
//
// Each Device Connection gets one API client, whose api@koko channel stays
// open between refreshes and carries nothing, so a refresh that has its
// answer has had the last the koko server sends for it.
func (m *deviceMux) refreshBareNames() {
	var (
		on     *deviceConn
		client *apiclient.Client
	)
	defer func() {
		if client != nil {
			client.Close()
		}
	}()
	for {
		d := m.connected()
		if d == nil {
			return
		}
		if d != on {
			if client != nil {
				client.Close()
			}
			on, client = d, apiclient.New(func(context.Context) (serverconn.Conn, error) { return lentConn{d.conn}, nil })
		}
		m.writeBareNames(client)
		timer := time.NewTimer(m.timings.BareNameRefresh)
		select {
		case <-timer.C:
		case <-d.conn.HostsChanged():
		case <-d.done:
		case <-m.ctx.Done():
		}
		timer.Stop()
	}
}

// connected returns the Device Connection the Device Mux holds, waiting for
// one without asking for it, as await does; nil once it is stopping.
func (m *deviceMux) connected() *deviceConn {
	m.mu.Lock()
	defer m.mu.Unlock()
	for m.ctx.Err() == nil {
		if m.current != nil {
			return m.current
		}
		tried := m.tried
		m.mu.Unlock()
		select {
		case <-tried:
		case <-m.ctx.Done():
		}
		m.mu.Lock()
	}
	return nil
}

// writeBareNames lists the Hosts the device's User can see through c and
// rewrites koko's ssh_config with their names, unless it holds them
// already, keeping the koko its ProxyCommand runs
// (devicefiles.RefreshSSHConfig). It writes holding the Device Mux's lock,
// and not once the Device Mux is stopping, so it never puts back the names
// its revocation cleanup took out. It logs whether the refresh succeeded.
func (m *deviceMux) writeBareNames(c *apiclient.Client) {
	resp, err := c.Call(m.ctx, api.HostList, api.HostListRequest{})
	if err != nil {
		if m.ctx.Err() == nil {
			m.Log.Warn("cannot refresh the bare names", "err", err)
		}
		return
	}
	names := make([]string, 0, len(resp.Hosts))
	for _, h := range resp.Hosts {
		names = append(names, h.Name)
	}
	m.mu.Lock()
	if m.ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	wrote, err := devicefiles.RefreshSSHConfig(m.Home, names)
	m.mu.Unlock()
	if err != nil {
		m.Log.Warn("cannot refresh the bare names", "err", err)
		return
	}
	m.Log.Info("refreshed the bare names", "names", len(names), "rewrote", wrote)
}

// lentConn is a Device Connection lent to a refresh's API client: closing
// the client closes the channels it opened, never the Device Connection.
type lentConn struct{ serverconn.Conn }

func (lentConn) Close() error { return nil }
