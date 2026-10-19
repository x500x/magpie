package termuxdns

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Termux's resolv.conf names the servers the resolver is pointed at: its
// nameserver lines, as host:port, and nothing else.
func TestServersReadsNameserverLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "resolv.conf")
	conf := "# Termux's\nnameserver 8.8.8.8\n" +
		"nameserver ::1\n" +
		"search example.com\n" +
		"options edns0\n" +
		"nameserver not-an-address\n" +
		"nameserver\n" +
		"nameserver 1.1.1.1  # trailing comment\n" +
		"nameserver 9.9.9.9\n" +
		"nameserver 4.4.4.4\n"
	if err := os.WriteFile(p, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	want := []string{"8.8.8.8:53", "[::1]:53", "1.1.1.1:53"}
	got := Servers(p)
	if len(got) != len(want) {
		t.Fatalf("servers %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("servers %q, want %q", got, want)
		}
	}
}

// A file that names no server is no resolv.conf, and neither is one that
// isn't there: the next path is read, and none left is no servers.
func TestServersSkipsWhatNamesNone(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("# nothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "good")
	if err := os.WriteFile(good, []byte("nameserver 10.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing")

	if got := Servers(missing, empty, good); len(got) != 1 || got[0] != "10.0.0.1:53" {
		t.Fatalf("servers %q, want the good file's", got)
	}
	if got := Servers(missing, empty); got != nil {
		t.Fatalf("servers %q, want none", got)
	}
}

// Go's own resolv.conf decides: when it names a server, its resolver asks
// that one and nothing is moved; when it names none, as on Android, Termux's
// servers are the ones the fallbacks go to.
func TestRedirectForReadsBothFiles(t *testing.T) {
	dir := t.TempDir()
	etc := filepath.Join(dir, "etc")
	termux := filepath.Join(dir, "termux")
	if err := os.WriteFile(etc, []byte("nameserver 192.168.1.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(termux, []byte("nameserver 8.8.8.8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RedirectFor(etc, termux); got != nil {
		t.Fatalf("moved to %q though Go's resolv.conf names a server", got)
	}
	missing := filepath.Join(dir, "missing")
	if got := RedirectFor(missing, termux); len(got) != 1 || got[0] != "8.8.8.8:53" {
		t.Fatalf("servers %q, want Termux's", got)
	}
	// Go's resolv.conf there but naming nothing is the same as absent
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("# nothing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RedirectFor(empty, termux); len(got) != 1 || got[0] != "8.8.8.8:53" {
		t.Fatalf("servers %q, want Termux's", got)
	}
	if got := RedirectFor(etc, missing); got != nil {
		t.Fatalf("moved to %q with no Termux resolv.conf", got)
	}
}

// Only the addresses Go made up when it found no resolv.conf are moved: a
// query Go aimed at a server it read from a resolv.conf goes where it sent
// it, so a machine that resolves as it did keeps doing so.
func TestServerForMovesOnlyTheFallbacks(t *testing.T) {
	servers := []string{"10.0.0.1:53", "10.0.0.2:53", "10.0.0.3:53"}
	for _, c := range []struct{ address, want string }{
		{"127.0.0.1:53", "10.0.0.1:53"},
		{"[::1]:53", "10.0.0.2:53"},
		{"192.168.1.1:53", ""}, // read from a resolv.conf: left alone
		{"8.8.8.8:53", ""},
		{"", ""},
	} {
		if got := serverFor(servers, c.address); got != c.want {
			t.Errorf("serverFor(%q) = %q, want %q", c.address, got, c.want)
		}
	}
	// one server stands in for both fallbacks
	if got := serverFor([]string{"10.0.0.1:53"}, "[::1]:53"); got != "10.0.0.1:53" {
		t.Errorf("a lone server: %q", got)
	}
	// none named: nothing is moved
	if got := serverFor(nil, "127.0.0.1:53"); got != "" {
		t.Errorf("no servers: %q", got)
	}
}

// Redirect is net.Resolver.Dial, so a query net aimed at its fallback
// arrives at the server Termux's resolv.conf names, and one it aimed
// elsewhere arrives where net sent it (#2872).
func TestRedirectDialsTheServerItNames(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	arrived := make(chan struct{}, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			arrived <- struct{}{}
			c.Close()
		}
	}()
	dial := Redirect([]string{ln.Addr().String()})

	// net's fallback: dialled at the server resolv.conf names instead
	c, err := dial(context.Background(), "tcp", "127.0.0.1:53")
	if err != nil {
		t.Fatalf("dial net's fallback: %v", err)
	}
	c.Close()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("a query net aimed at 127.0.0.1:53 didn't reach the server resolv.conf names")
	}

	// a server Go read from a resolv.conf: dialled as asked, and nothing
	// here answers it
	if c, err := dial(context.Background(), "tcp", "127.0.0.1:1"); err == nil {
		c.Close()
		t.Fatal("a query for 127.0.0.1:1 reached a server, though only net's fallbacks move")
	}
}
