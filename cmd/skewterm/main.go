// Command skewterm is the web terminal's page, for the skew check
// (release/skew-check.sh). Through a run of koko web on this machine, it
// trades the address's one-time token for koko web's cookie, opens an SSH
// Session to a Host over the terminal's WebSocket, sends the shell one line,
// reads the shell's answer and closes the SSH Session, as the web UI's page
// does with its tab:
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

// terminalPath is the web terminal's WebSocket endpoint, the web front's
// web.TerminalPath, which a program outside the server group cannot import.
const terminalPath = "/terminal/socket"

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

// run opens an SSH Session to host through koko web's address, sends the
// line, waits for the answer and closes the SSH Session, and returns its id.
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
	h := http.Header{}
	h.Set("Origin", "http://localhost:"+port)
	h.Set("Cookie", cookie.String())
	c, resp, err := websocket.Dial(ctx, "ws://localhost:"+port+terminalPath, &websocket.DialOptions{HTTPClient: client, HTTPHeader: h})
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
		return "", fmt.Errorf("open the terminal's WebSocket%s: %w", page, err)
	}
	defer func() { _ = c.CloseNow() }()

	first, err := json.Marshal(wire.TerminalFirst{Protocol: wire.TerminalProtocol, Open: host, Cols: 80, Rows: 24})
	if err != nil {
		return "", err
	}
	if err := c.Write(ctx, websocket.MessageText, first); err != nil {
		return "", fmt.Errorf("send the first message: %w", err)
	}
	typ, msg, err := c.Read(ctx)
	if err != nil {
		return "", fmt.Errorf("read the answer to the open: %w", err)
	}
	var a wire.TerminalAnswer
	if typ != websocket.MessageText {
		return "", fmt.Errorf("the open was answered %s %q, not text", typ, msg)
	}
	if err := json.Unmarshal(msg, &a); err != nil {
		return "", fmt.Errorf("the open was answered %q: %w", msg, err)
	}
	if a.Refused != "" {
		why := ""
		if a.Message != nil {
			why = *a.Message
		}
		return "", fmt.Errorf("the open was refused: %s: %s", a.Refused, why)
	}
	if a.Tunnel == "" {
		return "", fmt.Errorf("the open was answered %s, with no SSH Session's id", msg)
	}
	fmt.Fprintf(os.Stderr, "skewterm: SSH Session %s to %s opened\n", a.Tunnel, host)

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
	// The page's close: the stream's close frame, then the WebSocket's.
	if err := stream.Close(); err != nil {
		return "", fmt.Errorf("close the SSH Session: %w", err)
	}
	return a.Tunnel, nil
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
