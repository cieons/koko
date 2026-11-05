package record

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The protocols a Service's port carries. A port written without one
// carries TCP.
const (
	TCP = "tcp"
	UDP = "udp"
)

// ServicePort is one port a Service listens on, with its protocol.
type ServicePort struct {
	Port     int
	Protocol string
}

// String writes p in the one form koko writes a port in, <port>/<protocol>,
// such as 443/tcp, whether or not it was written with its protocol.
func (p ServicePort) String() string {
	return strconv.Itoa(p.Port) + "/" + p.Protocol
}

// ParseServicePort reads a port a Service listens on, or says why s is not
// one: a port 1 to 65535, then /tcp or /udp, or nothing for tcp, as in 443
// for 443/tcp.
func ParseServicePort(s string) (ServicePort, error) {
	number, protocol, written := strings.Cut(s, "/")
	if !written {
		protocol = TCP
	}
	if protocol != TCP && protocol != UDP {
		return ServicePort{}, fmt.Errorf("the port %q carries %q, which is neither %s nor %s", s, protocol, TCP, UDP)
	}
	port, err := ParsePort(number)
	if err != nil {
		return ServicePort{}, fmt.Errorf("the port %q: %w", s, err)
	}
	return ServicePort{Port: port, Protocol: protocol}, nil
}

// ParseServicePorts reads a Service's set of ports, which may be empty, in
// the order koko keeps them: by port, then tcp before udp. It refuses a
// port given twice, such as 443 and 443/tcp, since a set holds each once.
func ParseServicePorts(ports []string) ([]ServicePort, error) {
	set := make([]ServicePort, 0, len(ports))
	for _, s := range ports {
		p, err := ParseServicePort(s)
		if err != nil {
			return nil, err
		}
		if slices.Contains(set, p) {
			return nil, fmt.Errorf("the port %s is given twice", p)
		}
		set = append(set, p)
	}
	slices.SortFunc(set, func(a, b ServicePort) int {
		return cmp.Or(cmp.Compare(a.Port, b.Port), cmp.Compare(a.Protocol, b.Protocol))
	})
	return set, nil
}
