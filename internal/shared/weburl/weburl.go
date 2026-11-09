// Package weburl holds the form of the Web URL, the one https:// address the
// Owner's reverse proxy serves the web UI at (ADR 0042): an origin,
// https://<host>[:<port>], whose host is a DNS name, an IPv4 address or an
// IPv6 address in brackets, as a Server Address's is, with no path, query,
// fragment or user information. The CLI checks it before its call, and the
// koko server again before it stores it.
package weburl

import (
	"fmt"
	"strings"

	"github.com/cieons/koko/internal/shared/serveraddr"
)

// scheme starts every Web URL: the Sign-in cookie is Secure, and a password
// must not cross the network in clear.
const scheme = "https://"

// Parse returns s as the Web URL is stored, lowercased and without the
// default port 443 or a lone trailing "/", or why s is not a Web URL.
func Parse(s string) (string, error) {
	lower := strings.ToLower(s)
	authority, ok := strings.CutPrefix(lower, scheme)
	switch {
	case s == "":
		return "", fmt.Errorf("a Web URL cannot be empty: write https://<host>, such as https://koko.example.com")
	case !ok:
		return "", fmt.Errorf("%q does not start with https://: the reverse proxy serves the web UI over HTTPS, such as at https://koko.example.com", s)
	case strings.ContainsAny(authority, "?#@"):
		return "", fmt.Errorf("%q holds a query, a fragment or user information: a Web URL is https://<host>[:<port>] alone", s)
	}
	authority = strings.TrimSuffix(authority, "/")
	if strings.Contains(authority, "/") {
		return "", fmt.Errorf("%q has a path: the web UI is served at the root of its host, so a Web URL is https://<host>[:<port>] alone", s)
	}
	if err := serveraddr.Check(authority); err != nil {
		return "", fmt.Errorf("%q does not name a host as https://<host>[:<port>]: %w", s, err)
	}
	return scheme + strings.TrimSuffix(authority, ":443"), nil
}

// Authority returns the Web URL u's host, and its port when u names one, as
// a request's Host header names them. u must be a Web URL as Parse returns
// it.
func Authority(u string) string {
	return strings.TrimPrefix(u, scheme)
}
