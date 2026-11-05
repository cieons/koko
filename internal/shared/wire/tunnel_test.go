package wire_test

import (
	"bytes"
	"testing"

	"github.com/cieons/koko/internal/shared/wire"
)

// A tunnel@koko "open" payload is two SSH strings, the kind and the SSH
// Name, as every CLI that ever opens a Tunnel writes it, byte for byte.
func TestATunnelOpenPayloadIsTwoSSHStrings(t *testing.T) {
	want := []byte("\x00\x00\x00\x04open\x00\x00\x00\x09web1.koko")
	if got := wire.TunnelOpen("web1.koko"); !bytes.Equal(got, want) {
		t.Fatalf("TunnelOpen(web1.koko) = %q, want %q", got, want)
	}
	kind, sshName, err := wire.ParseTunnelPayload(want)
	if err != nil || kind != wire.TunnelKindOpen || sshName != "web1.koko" {
		t.Errorf("ParseTunnelPayload = %q, %q, %v; want open, web1.koko", kind, sshName, err)
	}
}

// Another kind comes back unread, for the Gateway to refuse until it takes
// it; a payload that is no kind, or an "open" that is not one SSH Name, is
// refused.
func TestATunnelPayloadIsReadByItsKind(t *testing.T) {
	kind, sshName, err := wire.ParseTunnelPayload([]byte("\x00\x00\x00\x06resume\x01\x02\x03"))
	if err != nil || kind != "resume" || sshName != "" {
		t.Errorf("a resume payload: %q, %q, %v; want the kind alone", kind, sshName, err)
	}
	for _, b := range [][]byte{
		nil,
		[]byte("open"),
		[]byte("\x00\x00\x00\x04open"),
		[]byte("\x00\x00\x00\x04open\x00\x00\x00\x04web1\x00"),
		[]byte("\x00\x00\x00\x04open\x00\x00\x00\x09web1"),
	} {
		if kind, _, err := wire.ParseTunnelPayload(b); err == nil {
			t.Errorf("ParseTunnelPayload(%q) = %q, want a refusal", b, kind)
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
