//go:build android && nogui

package termuxdns

import (
	"net"
	"path/filepath"

	"github.com/yetone/magpie/internal/appdir"
)

// etcResolvConf is the file Go's own resolver reads on Android, and finds
// nowhere: its absence is what leaves the resolver on 127.0.0.1:53 and
// [::1]:53. A rooted device may have one, and then it is Go's to read.
const etcResolvConf = "/etc/resolv.conf"

// Install points net's resolver at Termux's name servers. Only this build
// looks for a resolv.conf Android hasn't, so only it has fallbacks to
// replace. The CLI calls Install before anything looks a name up.
//
// What it returns is what to tell the user: Termux's resolv-conf is only a
// recommendation of the packages that read it, so a Termux without the file
// has no name server at all, and every lookup there goes to this machine's
// port 53 and fails (#2872).
func Install() string {
	termuxConf := termuxResolvConf()
	if termuxConf == "" {
		return "" // no PREFIX: not Termux, so no file of its own to read
	}
	servers := RedirectFor(etcResolvConf, termuxConf)
	if len(servers) == 0 {
		if len(Servers(etcResolvConf)) > 0 {
			return "" // Go reads this one, so its resolver asks a server
		}
		return termuxConf + " names no name server, so magpie reaches no vendor: `pkg install resolv-conf`, or write a nameserver line there"
	}
	// This build has no cgo, so Go's own resolver is the one that asks, and
	// the addresses it made up are the ones moved. A build with cgo asks
	// libc, which goes to netd and needs none of this.
	net.DefaultResolver.Dial = Redirect(servers)
	return ""
}

// termuxResolvConf is where Termux keeps the resolv.conf its programs read.
// A PREFIX that isn't an absolute path names no file of Termux's.
func termuxResolvConf() string {
	prefix := appdir.Getenv("PREFIX")
	if !filepath.IsAbs(prefix) {
		return ""
	}
	return filepath.Join(prefix, "etc", "resolv.conf")
}
