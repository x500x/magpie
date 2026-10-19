//go:build !android || !nogui

package termuxdns

// Install does nothing here: only the Android terminal build's resolver
// looks for a resolv.conf Android hasn't (see termuxdns.go).
func Install() string { return "" }
