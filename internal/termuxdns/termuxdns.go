// Package termuxdns makes Go's own resolver work in the Android terminal
// build, where Android has no /etc/resolv.conf.
//
// Android's lookups go to netd, and Termux keeps the file the programs it
// packages read at $PREFIX/etc/resolv.conf (Termux's resolv-conf package
// writes it). Go's resolver in a build without cgo, which the terminal
// build is, reads /etc/resolv.conf and, finding none, falls back to
// 127.0.0.1:53 and [::1]:53, where nothing answers: every lookup in Termux
// failed with "connection refused", so the CLI reached no vendor (#2872).
//
// Install moves a query Go aimed at those two addresses to Termux's
// servers instead. Only when Go's own resolv.conf names no server does that
// happen, so a machine that resolves as it did keeps doing so, and a server
// a user named is never passed over.
package termuxdns

import (
	"context"
	"net"
	"net/netip"
	"os"
	"strings"
)

// maxServers is how many name servers Go's resolver takes from a
// resolv.conf, and so how many are read here.
const maxServers = 3

// fallbacks are the addresses Go's resolver asks when it finds no
// resolv.conf, in its own order.
var fallbacks = []string{"127.0.0.1:53", "[::1]:53"}

// Servers is the name servers of the first of paths that reads, in the
// form Go's resolver asks them (host:port), or none when no file there
// names one.
func Servers(paths ...string) []string {
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if servers := serversIn(string(b)); len(servers) > 0 {
			return servers
		}
	}
	return nil
}

// serversIn is the nameserver lines of a resolv.conf. A comment, an
// option, a line without an address and an address that doesn't parse are
// skipped, as Go's own reader skips them.
func serversIn(conf string) []string {
	var servers []string
	for _, line := range strings.Split(conf, "\n") {
		f := strings.Fields(line)
		if len(servers) >= maxServers || len(f) < 2 || f[0] != "nameserver" {
			continue
		}
		// Only an address: a name there would itself need a lookup.
		if addr, err := netip.ParseAddr(f[1]); err == nil {
			servers = append(servers, net.JoinHostPort(addr.String(), "53"))
		}
	}
	return servers
}

// RedirectFor is the servers a lookup is moved to, given the resolv.conf
// Go's resolver reads and Termux's own: none when Go's names a server, since
// its resolver asks that one, and Termux's when Go's names none.
func RedirectFor(goConf, termuxConf string) []string {
	if len(Servers(goConf)) > 0 {
		return nil
	}
	return Servers(termuxConf)
}

// Redirect is a net.Resolver.Dial that asks servers instead of a query Go
// aimed at its own fallbacks: the first fallback's query goes to the first
// server, the second's to the second. A query for any other address is
// dialled as Go asked it, so a server read from a resolv.conf is never
// passed over.
func Redirect(servers []string) func(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if to := serverFor(servers, address); to != "" {
			address = to
		}
		return d.DialContext(ctx, network, address)
	}
}

// serverFor is the server a query for address goes to, "" for one address
// isn't one of Go's fallbacks.
func serverFor(servers []string, address string) string {
	if len(servers) == 0 {
		return ""
	}
	for i, f := range fallbacks {
		if address != f {
			continue
		}
		// Go retries across its fallbacks, so the second one is the second
		// server; with only one named, both go to it.
		if i < len(servers) {
			return servers[i]
		}
		return servers[0]
	}
	return ""
}
