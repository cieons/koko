// Package serveraddr holds the form of a Server Address, as the Owner gives
// it at initialisation: host, or host:port when NAT maps another port than
// the koko server's listen port. The host is a DNS name, an IPv4 address, or
// an IPv6 address in brackets. Without a port, the Server Address's port is
// the listen port (P0 spec: install and run).
package serveraddr

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Check reports why s is not a Server Address, or nil when it is one.
func Check(s string) error {
	switch {
	case s == "":
		return errors.New("a Server Address cannot be empty")
	case strings.Contains(s, "/"):
		return fmt.Errorf("%q holds '/': a Server Address is host or host:port, not a URL", s)
	}
	port, hasPort, err := checkHostPart(s)
	if err != nil {
		return err
	}
	if hasPort {
		if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 || port[0] == '0' {
			return fmt.Errorf("%q: its port %q is not a number from 1 to 65535", s, port)
		}
	}
	return nil
}

// checkHostPart checks s's host, an IPv6 address only in brackets and any
// other host only outside them, and returns what follows it as the port.
func checkHostPart(s string) (port string, hasPort bool, err error) {
	if rest, ok := strings.CutPrefix(s, "["); ok {
		inner, after, ok := strings.Cut(rest, "]")
		if !ok {
			return "", false, fmt.Errorf("%q opens '[' with no ']'", s)
		}
		if a, err := netip.ParseAddr(inner); err != nil || !a.Is6() || a.Zone() != "" {
			return "", false, fmt.Errorf("%q holds %q in brackets, which is not an IPv6 address without a zone", s, inner)
		}
		if after == "" {
			return "", false, nil
		}
		port, ok := strings.CutPrefix(after, ":")
		if !ok {
			return "", false, fmt.Errorf("%q holds %q after ']', where only :<port> may follow", s, after)
		}
		return port, true, nil
	}
	host, port, hasPort := strings.Cut(s, ":")
	if strings.Contains(port, ":") {
		return "", false, fmt.Errorf("%q holds more than one ':': put an IPv6 address in brackets, as [2001:db8::1]:443", s)
	}
	if err := checkHost(host); err != nil {
		return "", false, fmt.Errorf("%q: %w", s, err)
	}
	return port, hasPort, nil
}

// maxNameLen is the longest DNS name, written without its final dot.
const maxNameLen = 253

// checkHost checks a host outside brackets: an IPv4 address, or a DNS name
// of labels of letters, digits and '-', whose last label is not all digits,
// so that a mistyped IPv4 address is not taken for a name.
func checkHost(host string) error {
	if host == "" {
		return errors.New("it has no host")
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil // an IPv4 address: an IPv6 one holds ':'
	}
	if len(host) > maxNameLen {
		return fmt.Errorf("its host is longer than %d characters", maxNameLen)
	}
	last := ""
	for label := range strings.SplitSeq(host, ".") {
		if err := checkLabel(label); err != nil {
			return err
		}
		last = label
	}
	if strings.Trim(last, "0123456789") == "" {
		return fmt.Errorf("its host %q is neither an IP address nor a DNS name", host)
	}
	return nil
}

// checkLabel checks one label of a DNS name: 1 to 63 of letters, digits and
// '-', not starting or ending with '-'.
func checkLabel(label string) error {
	switch {
	case label == "":
		return errors.New("its host has an empty label")
	case len(label) > 63:
		return fmt.Errorf("its host's label %q is longer than 63 characters", label)
	case label[0] == '-' || label[len(label)-1] == '-':
		return fmt.Errorf("its host's label %q starts or ends with '-'", label)
	}
	for _, r := range label {
		if !isLabelRune(r) {
			return fmt.Errorf("its host holds %q: a DNS name uses only letters, digits, '-' and '.'", r)
		}
	}
	return nil
}

func isLabelRune(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '-'
}
