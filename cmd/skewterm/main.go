// Command skewterm is the web terminal's page, for the skew check
// (release/skew-check.sh). Through a run of koko web on this machine, it
// trades the address's one-time token for koko web's cookie, opens a tab to
// a Host on the Workspace socket, attaches to its SSH Session on the
// terminal socket, sends the shell one line, reads the shell's answer and
// closes the tab, as the web UI's page does:
//
//	skewterm <address> <host>
//
// <address> is the one koko web printed. It prints the SSH Session's id,
// for the check to find the koko server's log lines, and exits 0 once the
// shell answered; otherwise it says why on stderr and exits 1. It gives up
// after a minute.
//
// It is not released: the check builds it from the commit it runs on, the
// page's own, since a page and its koko server are one build. The CLI it
// goes through may be another's, which forwards the WebSocket unchanged.
package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/coder/websocket"

	"github.com/cieons/koko/internal/shared/resumable"
	"github.com/cieons/koko/internal/shared/wire"
)

// The web terminal's sockets, the web front's web.TerminalPath and
// web.WorkspacePath, which a program outside the server group cannot
// import.
const (
	terminalPath  = "/terminal/socket"
	workspacePath = "/terminal/workspace"
)

// The line sent, and what only the shell's answer holds: the line's echo
// shows the arithmetic unworked.
const (
	line   = "echo koko-skew-$((6*7))\n"
	answer = "koko-skew-42"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: skewterm <address> <host>")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	id, err := run(ctx, os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "skewterm:", err)
		os.Exit(1)
	}
	fmt.Println(id)
}

// run opens a tab to host through koko web's address, attaches to its SSH
// Session, sends the line, waits for the answer and closes the tab, and
// returns the SSH Session's id.
func run(ctx context.Context, address, host string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || u.Hostname() != "localhost" || u.Port() == "" {
		return "", fmt.Errorf("%q is no address koko web prints, http://localhost:<port>/?t=<token>", address)
	}
	port := u.Port()
	// koko web listens on 127.0.0.1 alone, and the page names it localhost.
	var d net.Dialer
	client := &http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return d.DialContext(ctx, "tcp4", net.JoinHostPort("127.0.0.1", port))
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	cookie, err := trade(ctx, client, address, "koko_"+port)
	if err != nil {
		return "", err
	}
	dial := func(path string, first any) (*websocket.Conn, []byte, error) {
		h := http.Header{}
		h.Set("Origin", "http://localhost:"+port)
		h.Set("Cookie", cookie.String())
		c, resp, err := websocket.Dial(ctx, "ws://localhost:"+port+path, &websocket.DialOptions{HTTPClient: client, HTTPHeader: h})
		// An upgrade's answer has no body; a refusal's is the web front's page.
		if resp != nil && resp.Body != nil {
			defer func() { _ = resp.Body.Close() }()
		}
		if err != nil {
			page := ""
			if resp != nil && resp.Body != nil {
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
				page = fmt.Sprintf(", answered %s: %q", resp.Status, b)
			}
			return nil, nil, fmt.Errorf("open %s%s: %w", path, page, err)
		}
		body, err := json.Marshal(first)
		if err == nil {
			err = c.Write(ctx, websocket.MessageText, body)
		}
		if err != nil {
			_ = c.CloseNow()
			return nil, nil, fmt.Errorf("send the first message on %s: %w", path, err)
		}
		msg, err := readText(ctx, c)
		if err != nil {
			_ = c.CloseNow()
			return nil, nil, fmt.Errorf("read the answer on %s: %w", path, err)
		}
		return c, msg, nil
	}

	ws, msg, err := dial(workspacePath, wire.WorkspaceFirst{Protocol: wire.TerminalProtocol})
	if err != nil {
		return "", err
	}
	defer func() { _ = ws.CloseNow() }()
	if err := refused("the Workspace socket", msg); err != nil {
		return "", err
	}
	tab, err := request(ctx, ws, wire.WorkspaceRequest{Request: 1, Open: host, Cols: 80, Rows: 24})
	if err != nil {
		return "", err
	}
	id, err := running(ctx, ws, tab)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "skewterm: SSH Session %s to %s opened\n", id, host)

	c, msg, err := dial(terminalPath, wire.TerminalFirst{Protocol: wire.TerminalProtocol, Attach: id, Cols: 80, Rows: 24})
	if err != nil {
		return "", err
	}
	defer func() { _ = c.CloseNow() }()
	if err := refused("the attach", msg); err != nil {
		return "", err
	}
	stream := resumable.New(websocket.NetConn(ctx, c, websocket.MessageBinary))
	answered := make(chan error, 1)
	go func() { answered <- read(stream) }()
	rec, err := wire.AppendTerminalRecord(nil, wire.TerminalRecord{Type: wire.TerminalData, Data: []byte(line)})
	if err != nil {
		return "", err
	}
	if _, err := stream.Write(rec); err != nil {
		return "", fmt.Errorf("send the line: %w", err)
	}
	select {
	case err := <-answered:
		if err != nil {
			return "", err
		}
	case <-ctx.Done():
		return "", errors.New("the shell did not answer within a minute")
	}
	fmt.Fprintf(os.Stderr, "skewterm: the shell answered %s\n", answer)
	// The page's close of the tab, which ends the SSH Session everywhere.
	if _, err := request(ctx, ws, wire.WorkspaceRequest{Request: 2, Close: tab}); err != nil {
		return "", fmt.Errorf("close the tab: %w", err)
	}
	_ = stream.Close()
	return id, nil
}

// readText reads one text message off c.
func readText(ctx context.Context, c *websocket.Conn) ([]byte, error) {
	typ, msg, err := c.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		return nil, fmt.Errorf("a message of %s %q, not text", typ, msg)
	}
	return msg, nil
}

// refused fails with a first message's refusal, what names it.
func refused(what string, msg []byte) error {
	var a wire.TerminalAnswer
	if err := json.Unmarshal(msg, &a); err != nil {
		return fmt.Errorf("%s was answered %q: %w", what, msg, err)
	}
	if a.Refused != "" {
		why := ""
		if a.Message != nil {
			why = *a.Message
		}
		return fmt.Errorf("%s was refused: %s: %s", what, a.Refused, why)
	}
	return nil
}

// request sends r on the Workspace socket ws and returns the tab of its
// answer, skipping the Workspaces before it.
func request(ctx context.Context, ws *websocket.Conn, r wire.WorkspaceRequest) (string, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	if err := ws.Write(ctx, websocket.MessageText, body); err != nil {
		return "", fmt.Errorf("send a Workspace request: %w", err)
	}
	for {
		msg, err := readText(ctx, ws)
		if err != nil {
			return "", fmt.Errorf("read the answer to a Workspace request: %w", err)
		}
		var a wire.WorkspaceAnswer
		if json.Unmarshal(msg, &a) != nil || a.Request != r.Request {
			continue
		}
		if a.Refused != "" {
			return "", fmt.Errorf("the Workspace request was refused: %s", msg)
		}
		return a.Tab, nil
	}
}

// running reads Workspaces off ws until the tab runs, and returns its SSH
// Session's id.
func running(ctx context.Context, ws *websocket.Conn, tab string) (string, error) {
	for {
		msg, err := readText(ctx, ws)
		if err != nil {
			return "", fmt.Errorf("read the Workspace: %w", err)
		}
		var w wire.Workspace
		if json.Unmarshal(msg, &w) != nil {
			continue
		}
		for _, t := range w.Tabs {
			switch {
			case t.ID != tab:
			case t.State == wire.TabStateRunning:
				return t.Session, nil
			case t.State != wire.TabStateStarting:
				return "", fmt.Errorf("the tab did not start: %s", msg)
			}
		}
	}
}

// trade opens koko web's address, as a browser does, and returns the
// cookie named name that its token was traded for.
func trade(ctx context.Context, client *http.Client, address, name string) (*http.Cookie, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open koko web's address: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return &http.Cookie{Name: c.Name, Value: c.Value}, nil
		}
	}
	return nil, fmt.Errorf("opening koko web's address was answered %s, with no cookie %s", resp.Status, name)
}

// read reads the koko server's records until the shell's output holds the
// answer, and fails if the stream ends first.
func read(stream *resumable.Stream) error {
	r := wire.NewTerminalReader(stream)
	var out []byte
	for {
		rec, err := r.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = stream.Err()
			}
			return fmt.Errorf("the SSH Session ended before the shell answered, having printed %q: %w", out[max(0, len(out)-200):], err)
		}
		if rec.Type == wire.TerminalData {
			out = append(out, rec.Data...)
			if bytes.Contains(out, []byte(answer)) {
				return nil
			}
		}
	}
}
