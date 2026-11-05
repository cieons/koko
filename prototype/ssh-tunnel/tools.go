// PROTOTYPE, throwaway: never merged. See README.md.
//
// tools.go holds the scaffolding around the question: a stand-in for
// redeeming a Join Code, revoking a Device Session, a WAN emulator, and the
// measuring harness.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// runJoin stands in for redeeming a Join Code: it makes the device key,
// registers it as a Device Session, and pins the server's host key both
// for `koko tunnel` and in koko's known_hosts for ssh's HostKeyAlias.
func runJoin(args []string) int {
	fs := flag.NewFlagSet("join", flag.ExitOnError)
	device := fs.String("device", "alice/laptop", "user/device name of the Device Session")
	server := fs.String("server", "127.0.0.1:47443", "koko server address the Join Code names")
	alias := fs.String("alias", "koko-proto", "HostKeyAlias in koko's ssh_config block")
	fs.Parse(args)

	hostPub, err := os.ReadFile(serverHostKeyPath() + ".pub")
	if err != nil {
		log.Fatalf("start the server once first: %v", err)
	}
	hk, _, _, _, err := ssh.ParseAuthorizedKey(hostPub)
	if err != nil {
		log.Fatal(err)
	}
	os.Remove(deviceKeyPath())
	os.Remove(deviceKeyPath() + ".pub")
	key, err := loadOrCreateKey(deviceKeyPath())
	if err != nil {
		log.Fatal(err)
	}
	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key.PublicKey())))
	appendLine(devicesPath(), pub+" "+*device)
	os.WriteFile(deviceServerPath(), fmt.Appendf(nil, "%s %s\n", *server, ssh.FingerprintSHA256(hk)), 0o600)
	os.MkdirAll(filepath.Dir(knownHostsPath()), 0o700)
	if kh, _ := os.ReadFile(knownHostsPath()); !bytes.HasPrefix(kh, []byte(*alias+" ")) && !bytes.Contains(kh, []byte("\n"+*alias+" ")) {
		appendLine(knownHostsPath(), *alias+" "+strings.TrimSpace(string(hostPub)))
	}
	fmt.Printf("joined %s as %s (%s), pinned %s\n", *server, *device, ssh.FingerprintSHA256(key.PublicKey()), ssh.FingerprintSHA256(hk))
	return 0
}

// runRevoke moves a device's key from devices to revoked; the server keeps
// it there so the holder can be told why it is refused.
func runRevoke(args []string) int {
	fs := flag.NewFlagSet("revoke", flag.ExitOnError)
	device := fs.String("device", "alice/laptop", "user/device name of the Device Session")
	fs.Parse(args)
	b, _ := os.ReadFile(devicesPath())
	var keep []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		if strings.HasSuffix(line, " "+*device) {
			appendLine(revokedPath(), line)
			continue
		}
		if line != "" {
			keep = append(keep, line)
		}
	}
	os.WriteFile(devicesPath(), []byte(strings.Join(keep, "\n")+"\n"), 0o600)
	fmt.Printf("revoked %s\n", *device)
	return 0
}

func appendLine(p, line string) {
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

// runDelay is a WAN emulator: a TCP relay that holds every chunk for the
// one-way delay in each direction, with no bandwidth limit.
func runDelay(args []string) int {
	fs := flag.NewFlagSet("delay", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:47444", "")
	to := fs.String("to", "127.0.0.1:47443", "")
	oneWay := fs.Duration("one-way", 20*time.Millisecond, "delay in each direction (RTT is twice this)")
	fs.Parse(args)
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("delay %s -> %s, one-way %v", *listen, *to, *oneWay)
	for {
		a, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go func() {
			b, err := net.Dial("tcp", *to)
			if err != nil {
				a.Close()
				return
			}
			go delayCopy(b, a, *oneWay)
			delayCopy(a, b, *oneWay)
		}()
	}
}

func delayCopy(dst, src net.Conn, d time.Duration) {
	type chunk struct {
		due  time.Time
		data []byte
	}
	q := make(chan chunk, 1<<14)
	go func() {
		for c := range q {
			time.Sleep(time.Until(c.due))
			if c.data == nil {
				dst.(*net.TCPConn).CloseWrite()
				return
			}
			if _, err := dst.Write(c.data); err != nil {
				return
			}
		}
	}()
	buf := make([]byte, 64*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			q <- chunk{time.Now().Add(d), bytes.Clone(buf[:n])}
		}
		if err != nil {
			q <- chunk{due: time.Now().Add(d)}
			close(q)
			return
		}
	}
}

// runBench runs a command repeatedly and reports wall-clock times, or (with
// "echo") measures keystroke round trips through an interactive session.
func runBench(args []string) int {
	if len(args) > 0 && args[0] == "echo" {
		return benchEcho(args[1:])
	}
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	n := fs.Int("n", 10, "runs")
	warm := fs.Int("warm", 1, "untimed warm-up runs")
	size := fs.Int64("bytes", 0, "bytes moved per run, to report MiB/s")
	label := fs.String("label", "", "")
	fs.Parse(args)
	cmd := fs.Args()
	var ds []time.Duration
	for i := range *warm + *n {
		c := exec.Command(cmd[0], cmd[1:]...)
		var stderr bytes.Buffer
		c.Stderr = &stderr
		t := time.Now()
		err := c.Run()
		d := time.Since(t)
		if err != nil {
			fmt.Fprintf(os.Stderr, "run %d failed: %v\n%s", i, err, stderr.String())
			return 1
		}
		if i >= *warm {
			ds = append(ds, d)
		}
	}
	slices.Sort(ds)
	ms := func(d time.Duration) string { return fmt.Sprintf("%.1f", float64(d.Microseconds())/1000) }
	line := fmt.Sprintf("%-28s n=%d min=%sms p50=%sms p90=%sms max=%sms", *label, len(ds),
		ms(ds[0]), ms(ds[len(ds)/2]), ms(ds[len(ds)*9/10]), ms(ds[len(ds)-1]))
	if *size > 0 {
		mib := float64(*size) / (1 << 20)
		line += fmt.Sprintf("  p50 %.0f MiB/s (best %.0f)", mib/ds[len(ds)/2].Seconds(), mib/ds[0].Seconds())
	}
	fmt.Println(line)
	return 0
}

// benchEcho drives `ssh -tt ... 'stty raw -echo; cat'`: one byte out, wait
// for it to come back, as a keystroke and its echo would.
func benchEcho(args []string) int {
	fs := flag.NewFlagSet("bench echo", flag.ExitOnError)
	n := fs.Int("n", 300, "keystrokes")
	gap := fs.Duration("gap", 15*time.Millisecond, "pause between keystrokes")
	label := fs.String("label", "", "")
	fs.Parse(args)
	cmd := fs.Args()
	c := exec.Command(cmd[0], cmd[1:]...)
	in, _ := c.StdinPipe()
	outPipe, _ := c.StdoutPipe()
	c.Stderr = os.Stderr
	if err := c.Start(); err != nil {
		log.Fatal(err)
	}
	out := bufio.NewReader(outPipe)
	readUntil := func(b byte) error {
		for {
			got, err := out.ReadByte()
			if err != nil {
				return err
			}
			if got == b {
				return nil
			}
		}
	}
	// Wait for the session to be up and the remote tty to be raw.
	for {
		in.Write([]byte{'R'})
		done := make(chan error, 1)
		go func() { done <- readUntil('R') }()
		select {
		case err := <-done:
			if err != nil {
				log.Fatalf("session ended: %v", err)
			}
		case <-time.After(3 * time.Second):
			continue
		}
		break
	}
	time.Sleep(200 * time.Millisecond)
	// Drain anything left from the readiness probes.
	for out.Buffered() > 0 {
		out.ReadByte()
	}
	var ds []time.Duration
	for i := range *n {
		b := byte('a' + i%26)
		t := time.Now()
		in.Write([]byte{b})
		if err := readUntil(b); err != nil {
			log.Fatalf("session ended: %v", err)
		}
		ds = append(ds, time.Since(t))
		time.Sleep(*gap)
	}
	in.Close()
	c.Process.Kill()
	c.Wait()
	slices.Sort(ds)
	us := func(d time.Duration) string { return fmt.Sprintf("%.2f", float64(d.Microseconds())/1000) }
	fmt.Printf("%-28s keystrokes=%d p50=%sms p90=%sms p99=%sms max=%sms\n", *label, len(ds),
		us(ds[len(ds)/2]), us(ds[len(ds)*9/10]), us(ds[len(ds)*99/100]), us(ds[len(ds)-1]))
	return 0
}
