package wire_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cieons/koko/internal/shared/wire"
)

// A tunnel@koko "open" payload is two SSH strings, the kind and the SSH
// Name, as every CLI that ever opens a Tunnel writes it, byte for byte.
func TestATunnelOpenPayloadIsTwoSSHStrings(t *testing.T) {
	want := []byte("\x00\x00\x00\x04open\x00\x00\x00\x09web1.koko")
	if got := wire.TunnelOpen(1, "web1.koko"); !bytes.Equal(got, want) {
		t.Fatalf("TunnelOpen(web1.koko) = %q, want %q", got, want)
	}
	p, err := wire.ParseTunnelPayload(want)
	if err != nil || p != (wire.TunnelPayload{Kind: wire.TunnelKindOpen, SSHName: "web1.koko"}) {
		t.Errorf("ParseTunnelPayload = %+v, %v; want open, web1.koko", p, err)
	}
}

// A tunnel@koko "resume" payload is the SSH string "resume", the 16 bytes
// of the Tunnel id, and the offset the device has received as a uint64, as
// every CLI that ever Resumes a Tunnel writes it, byte for byte.
func TestATunnelResumePayloadIsTheKindTheIDAndTheOffset(t *testing.T) {
	id := [wire.TunnelIDSize]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	want := []byte("\x00\x00\x00\x06resume" +
		"\x00\x11\x22\x33\x44\x55\x66\x77\x88\x99\xaa\xbb\xcc\xdd\xee\xff" +
		"\x00\x00\x00\x01\x00\x00\x00\x10")
	if got := wire.TunnelResume(1, id, 1<<32+16); !bytes.Equal(got, want) {
		t.Fatalf("TunnelResume = %q, want %q", got, want)
	}
	p, err := wire.ParseTunnelPayload(want)
	if err != nil || p != (wire.TunnelPayload{Kind: wire.TunnelKindResume, ID: id, Received: 1<<32 + 16}) {
		t.Errorf("ParseTunnelPayload = %+v, %v; want the resume with its id and offset", p, err)
	}
}

// Another kind comes back unread, for the Gateway to refuse; a payload that
// is no kind, an "open" that is not one SSH Name, or a "resume" that is not
// an id and an offset, is refused.
func TestATunnelPayloadIsReadByItsKind(t *testing.T) {
	p, err := wire.ParseTunnelPayload([]byte("\x00\x00\x00\x05later\x01\x02\x03"))
	if err != nil || p != (wire.TunnelPayload{Kind: "later"}) {
		t.Errorf("a payload of another kind: %+v, %v; want the kind alone", p, err)
	}
	for _, b := range [][]byte{
		nil,
		[]byte("open"),
		[]byte("\x00\x00\x00\x04open"),
		[]byte("\x00\x00\x00\x04open\x00\x00\x00\x04web1\x00"),
		[]byte("\x00\x00\x00\x04open\x00\x00\x00\x09web1"),
		[]byte("\x00\x00\x00\x06resume"),
		[]byte("\x00\x00\x00\x06resume" + strings.Repeat("\x01", wire.TunnelIDSize) + "\x00\x00\x00\x00\x00\x00\x00"),
		[]byte("\x00\x00\x00\x06resume" + strings.Repeat("\x01", wire.TunnelIDSize+8+1)),
	} {
		if p, err := wire.ParseTunnelPayload(b); err == nil {
			t.Errorf("ParseTunnelPayload(%q) = %+v, want a refusal", b, p)
		}
	}
}

// Either form of an SSH Name names the same Host: a trailing .koko is
// dropped, once.
func TestTunnelHostDropsATrailingDotKoko(t *testing.T) {
	for sshName, want := range map[string]string{
		"web1":      "web1",
		"web1.koko": "web1",
		"koko":      "koko",
		"koko.koko": "koko",
		"a.koko.b":  "a.koko.b",
	} {
		if got := wire.TunnelHost(sshName); got != want {
			t.Errorf("TunnelHost(%q) = %q, want %q", sshName, got, want)
		}
	}
}
