package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/cieons/koko/internal/client/serverconn"
	"github.com/cieons/koko/internal/shared/api"
	"github.com/cieons/koko/internal/shared/portowner"
	"github.com/cieons/koko/internal/shared/wire"
)

// defaultWebPort is koko web's port, fixed so that bookmarks keep working.
const defaultWebPort = "5656"

var webCommands = []Command{
	{
		Verb:    "web",
		Summary: "Serve the web UI on this machine's localhost and open it in a browser",
		Flags: []Flag{
			{
				Name:    "port",
				Value:   "port",
				Default: defaultWebPort,
				Summary: "the port to listen on, on 127.0.0.1; koko web never moves to another",
			},
			{
				Name:    "no-open",
				Bool:    true,
				Summary: "print the address without opening a browser",
			},
		},
		// It prints a credential, its one-time token (P0 spec: overview).
		Tier: Irreversible,
		Op:   api.WebStart,
		Run:  longAction(web),
	},
}

var webMappings = []mapping{
	{
		api:     reflect.TypeFor[api.WebStartResponse](),
		cli:     reflect.TypeFor[webJSON](),
		omitted: map[string]string{"token": "printed inside address, as its t query parameter"},
	},
}

// webJSON is what koko web prints once it serves: the address to open.
type webJSON struct {
	// Address is http://localhost:<port>/?t=<token>. Its token works once,
	// within minutes, and only through this run of koko web.
	Address string `json:"address"`

	// opening says koko web opens the address in a browser itself.
	opening bool
}

func (w webJSON) Human(out io.Writer) error {
	var err error
	if w.opening {
		_, err = fmt.Fprintf(out, "Opening the web UI in a browser: %s\n", w.Address)
	} else {
		_, err = fmt.Fprintf(out, "Open the web UI in a browser on this machine: %s\nThe address works once.\n", w.Address)
	}
	if err == nil {
		_, err = fmt.Fprintln(out, "koko web runs until Ctrl-C.")
	}
	return err
}

// web is koko web: it listens on 127.0.0.1, gets a token from the koko
// server, prints the address and opens it, then forwards each browser
// connection into a web@koko channel until it is interrupted or the
// connection to the koko server ends.
func web(ctx context.Context, in *Input, ready func(webJSON) error) error {
	port, err := strconv.Atoi(in.Flag("port"))
	if err != nil || port < 1 || port > 65535 {
		return usagef("--port takes a TCP port from 1 to 65535, not %q", in.Flag("port"))
	}
	ln, err := listenWeb(ctx, port)
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()

	start, err := in.Call(ctx, api.WebStart, api.WebStartRequest{})
	if err != nil {
		return err
	}
	conn, err := in.server(ctx)
	if err != nil {
		return err
	}
	// Every browser connection goes at the lower of this build's revision
	// of web@koko and the koko server's.
	report, err := in.report(ctx)
	if err != nil {
		return err
	}
	payload := wire.WebPayload(report.ChannelRevision(wire.ChannelWeb, wire.WebRevision), port)
	res := webJSON{Address: fmt.Sprintf("http://localhost:%d/?t=%s", port, start.Token), opening: !in.Bool("no-open")}
	if err := ready(res); err != nil {
		return err
	}
	if res.opening {
		waitForOpener := openBrowser(ctx, in.inv, res.Address)
		defer waitForOpener()
	}
	f := forwarder{conn: conn, payload: payload, stderr: in.inv.Stderr, passOn: in.runner.Timings.Resolve().PassOn}
	err = f.serve(ctx, ln)
	if lost, ok := errors.AsType[*connectionEnded](err); ok {
		return webEnded(in, lost.err)
	}
	return err
}

// webEnded says why koko web ended when its connection to the koko server
// did, with err from Wait. koko disconnect stopping it is a stop as Ctrl-C
// is, so koko web exits 0, saying so. A Revoked Device Session ends koko web
// with revoked, once the device's files are deleted: by the Device Mux, or
// here on the direct path. On a joined device the cookie outlives every
// other end; over the Unix socket it ends with the run.
func webEnded(in *Input, err error) error {
	switch {
	case errors.Is(err, serverconn.ErrDisconnected):
		_, _ = fmt.Fprintln(in.inv.Stderr, "koko: koko disconnect stopped koko web.")
		return nil
	case in.joined == nil:
		return &cliError{code: codeServerUnreachable, message: "lost the connection to the koko server, and with it this run of koko web and its cookie: run koko web again once the koko server is back"}
	case errors.Is(err, serverconn.ErrRevoked):
		return revoked(in.inv.home(), "", *in.joined, &serverconn.RevokedError{Device: in.joined.Device})
	}
	return &cliError{code: codeServerUnreachable, message: "lost the connection to the koko server: run koko web again, and the browser's cookie still works"}
}

// listenWeb listens on 127.0.0.1:port, refusing a taken port by naming the
// process holding it. A browser may reach localhost on [::1] first, so a
// port another process holds there is refused too, though koko web does not
// listen there itself.
func listenWeb(ctx context.Context, port int) (net.Listener, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if errors.Is(err, syscall.EADDRINUSE) {
		return nil, portTaken(ctx, port)
	}
	if err != nil {
		return nil, fmt.Errorf("listen on 127.0.0.1:%d: %w", port, err)
	}
	// A machine without IPv6 fails this probe otherwise, and has nothing
	// on [::1] to take the browser elsewhere.
	probe, err := lc.Listen(ctx, "tcp6", net.JoinHostPort("::1", strconv.Itoa(port)))
	if errors.Is(err, syscall.EADDRINUSE) {
		_ = ln.Close()
		return nil, portTaken(ctx, port)
	}
	if err == nil {
		_ = probe.Close()
	}
	return ln, nil
}

// portTaken is the refusal of a taken port, naming the process holding it.
// koko web never moves to another port, which would break bookmarks.
func portTaken(ctx context.Context, port int) error {
	var holder string
	switch p, err := portowner.Lookup(ctx, port); {
	case err == nil:
		holder = p.String()
	case errors.Is(err, portowner.ErrNotFound):
		holder = "a process koko cannot see"
	default:
		holder = fmt.Sprintf("another process (%v)", err)
	}
	return &cliError{code: codePortTaken, message: fmt.Sprintf(
		"port %d on localhost is taken by %s: stop it, or give koko web another port with --port <port>",
		port, holder)}
}

// forwarder carries browser connections into web@koko channels of conn,
// each opened with payload, saying on stderr when one cannot be carried.
type forwarder struct {
	conn    serverconn.Conn
	payload []byte
	stderr  io.Writer
	// passOn bounds how long, once the connection to the koko server has
	// ended, the browser connections may take to pass on what it sent
	// before (timing.PassOn).
	passOn time.Duration
}

// connectionEnded is serve's error when the connection to the koko server
// ended, with err from its Wait.
type connectionEnded struct {
	err error
}

func (e *connectionEnded) Error() string {
	return fmt.Sprintf("the connection to the koko server ended: %v", e.err)
}

// serve forwards each browser connection on ln. It returns nil once ctx
// ends, and a *connectionEnded when the connection to the koko server ends
// first. Either way it closes every browser connection before it returns:
// at once when ctx ends, and once each has passed on what the koko server
// sent before, within passOn in all, when the connection ends.
func (f forwarder) serve(ctx context.Context, ln net.Listener) error {
	lost := make(chan error, 1)
	go func() { lost <- f.conn.Wait() }()
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		browsers  = map[net.Conn]struct{}{}
		acceptErr error
		accepting = make(chan struct{})
	)
	go func() {
		defer close(accepting)
		for {
			c, err := ln.Accept()
			if err != nil {
				acceptErr = err
				return
			}
			mu.Lock()
			browsers[c] = struct{}{}
			mu.Unlock()
			wg.Go(func() {
				f.pipe(ctx, c)
				mu.Lock()
				delete(browsers, c)
				mu.Unlock()
			})
		}
	}()

	var (
		err   error
		ended bool
	)
	select {
	case <-ctx.Done():
	case waitErr := <-lost:
		err, ended = &connectionEnded{err: waitErr}, true
	case <-accepting:
		err = fmt.Errorf("accept a browser connection: %w", acceptErr)
	}
	_ = ln.Close()
	<-accepting
	// Nothing accepts any more, so the set of browser connections is final.
	if ended {
		// Each channel ends with the connection, once it has given up what
		// the koko server sent before, such as the answer to a call in
		// flight when the Device Session was Revoked: the browsers get it
		// first, unless one does not read it within passOn, or koko web is
		// interrupted meanwhile.
		piped := make(chan struct{})
		go func() {
			wg.Wait()
			close(piped)
		}()
		select {
		case <-piped:
		case <-time.After(f.passOn):
		case <-ctx.Done():
		}
	}
	mu.Lock()
	for c := range browsers {
		_ = c.Close()
	}
	mu.Unlock()
	wg.Wait()
	return err
}

// pipe carries one browser connection through a web@koko channel, both ways,
// until either side closes it. Browsers never half-close an HTTP/1.1
// connection, and the web front's end of the channel closes both ways at its
// end (wire.Conn), so neither does pipe.
func (f forwarder) pipe(ctx context.Context, c net.Conn) {
	defer func() { _ = c.Close() }()
	ch, err := f.conn.OpenChannel(ctx, wire.ChannelWeb, f.payload)
	if err != nil {
		if ctx.Err() == nil {
			_, _ = fmt.Fprintf(f.stderr, "koko: could not carry a browser connection to the koko server: %v\n", err)
		}
		return
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(ch, c); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, ch); done <- struct{}{} }()
	<-done
	_ = ch.Close()
	_ = c.Close()
	<-done
}
