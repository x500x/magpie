//go:build android && nogui

package termuxdns

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Install is what makes a Termux lookup work: Go's resolver, finding no
// resolv.conf where it looks, asks 127.0.0.1:53 and [::1]:53, where Android
// answers nothing, and Install moves those two addresses to the servers
// Termux's resolv.conf names (#2872). The server here answers on port 53,
// the only port a resolv.conf can name, so the lookup reaches it only
// through that move.
func TestInstallPointsTheResolverAtTermuxsServers(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:53")
	if err != nil {
		t.Skipf("port 53 is taken: %v", err)
	}
	defer pc.Close()
	asked := make(chan string, 1)
	go func() {
		buf := make([]byte, 512)
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		var p dnsmessage.Parser
		h, err := p.Start(buf[:n])
		if err != nil {
			return
		}
		q, err := p.Question()
		if err != nil {
			return
		}
		asked <- q.Name.String()
		b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: h.ID, Response: true, Authoritative: true})
		b.EnableCompression()
		b.StartQuestions()
		b.Question(q)
		b.StartAnswers()
		b.AResource(dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60},
			dnsmessage.AResource{A: [4]byte{203, 0, 113, 9}})
		out, _ := b.Finish()
		pc.WriteTo(out, from)
	}()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "resolv.conf"), []byte("nameserver 127.0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PREFIX", dir)

	dial := net.DefaultResolver.Dial
	t.Cleanup(func() { net.DefaultResolver.Dial = dial })
	if said := Install(); said != "" {
		t.Fatalf("Install said %q with a resolv.conf that names a server", said)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, "termux.example")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(addrs) != 1 || addrs[0] != "203.0.113.9" {
		t.Fatalf("lookup answered %q, want the server's 203.0.113.9", addrs)
	}
	select {
	case name := <-asked:
		t.Logf("the server Termux names was asked for %s", name)
	default:
	}
}

// A resolv.conf that names no server leaves the resolver alone, and says
// what to install: Termux's resolv-conf is only a recommendation, so a
// Termux without it must be told why it reaches no vendor.
func TestInstallSaysWhyWithoutAServer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "resolv.conf"), []byte("# nothing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PREFIX", dir)

	dial := net.DefaultResolver.Dial
	t.Cleanup(func() { net.DefaultResolver.Dial = dial })
	if said := Install(); said == "" {
		t.Fatal("Install said nothing with no name server to read")
	}
	if dial == nil && net.DefaultResolver.Dial != nil {
		t.Fatal("the resolver's Dial was set though no resolv.conf names a server")
	}
}
