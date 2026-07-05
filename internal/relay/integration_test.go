package relay_test

import (
	"bufio"
	"io"
	"log"
	"net"
	"testing"
	"time"

	"remotedesk/internal/relay"
	"remotedesk/internal/tunnel"
	"remotedesk/internal/wire"
)

// TestTunnelEndToEnd proves Milestone 1: a byte stream from a viewer reaches the
// host's local target through the relay over SSH, and the reply comes back.
func TestTunnelEndToEnd(t *testing.T) {
	logger := log.New(io.Discard, "", 0)

	// Relay host key + relay listening on a random loopback port.
	relaySigner, _, err := wire.NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srv := relay.New(relaySigner, logger)
	go srv.Serve(ln)
	relayAddr := ln.Addr().String()
	relayKey := relaySigner.PublicKey()

	// Local "target" the host forwards to: an echo server that upper-cases input
	// so we can distinguish the round-trip from an accidental loopback.
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoLn.Close()
	go func() {
		for {
			c, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					c.Write([]byte("ECHO:" + sc.Text() + "\n"))
				}
			}()
		}
	}()

	// Host agent registers and forwards to the echo server.
	hostSigner, _, _ := wire.NewSigner()
	h, err := tunnel.Connect(tunnel.HostConfig{
		RelayAddr:  relayAddr,
		Signer:     hostSigner,
		RelayKey:   relayKey,
		Unattended: true,
		Target:     echoLn.Addr().String(),
		Logger:     logger,
	})
	if err != nil {
		t.Fatalf("host connect: %v", err)
	}
	defer h.Close()
	go h.Serve()

	if h.ID == "" || h.PIN == "" {
		t.Fatalf("host did not receive ID/PIN: id=%q pin=%q", h.ID, h.PIN)
	}

	// Viewer opens a tunnel and exchanges a line with the echo server.
	viewerSigner, _, _ := wire.NewSigner()
	stream, err := tunnel.Dial(tunnel.ViewerConfig{
		RelayAddr: relayAddr,
		Signer:    viewerSigner,
		RelayKey:  relayKey,
		ID:        h.ID,
		PIN:       h.PIN,
		Logger:    logger,
	})
	if err != nil {
		t.Fatalf("viewer dial: %v", err)
	}
	defer stream.Close()

	if _, err := stream.Write([]byte("hello\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	type readResult struct {
		line string
		err  error
	}
	res := make(chan readResult, 1)
	go func() {
		line, err := bufio.NewReader(stream).ReadString('\n')
		res <- readResult{line, err}
	}()
	select {
	case r := <-res:
		if r.err != nil {
			t.Fatalf("read: %v", r.err)
		}
		if r.line != "ECHO:hello\n" {
			t.Fatalf("round-trip mismatch: got %q", r.line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for tunnel round-trip")
	}
}

// TestRejectedByHost verifies the consent gate: when the host declines an
// incoming viewer, the tunnel is refused even with the correct PIN.
func TestRejectedByHost(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	relaySigner, _, _ := wire.NewSigner()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go relay.New(relaySigner, logger).Serve(ln)

	sink, _ := net.Listen("tcp", "127.0.0.1:0")
	defer sink.Close()

	hostSigner, _, _ := wire.NewSigner()
	h, err := tunnel.Connect(tunnel.HostConfig{
		RelayAddr: ln.Addr().String(), Signer: hostSigner,
		RelayKey: relaySigner.PublicKey(),
		Target:   sink.Addr().String(), Logger: logger,
		OnIncoming: func(string) bool { return false }, // always decline
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	go h.Serve()

	viewerSigner, _, _ := wire.NewSigner()
	_, err = tunnel.Dial(tunnel.ViewerConfig{
		RelayAddr: ln.Addr().String(), Signer: viewerSigner,
		RelayKey: relaySigner.PublicKey(), ID: h.ID, PIN: h.PIN, Logger: logger,
	})
	if err == nil {
		t.Fatal("expected rejection when host declines, got success")
	}
}

// TestBadPINRejected verifies a wrong PIN cannot open a tunnel.
func TestBadPINRejected(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	relaySigner, _, _ := wire.NewSigner()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	srv := relay.New(relaySigner, logger)
	go srv.Serve(ln)

	sink, _ := net.Listen("tcp", "127.0.0.1:0")
	defer sink.Close()

	hostSigner, _, _ := wire.NewSigner()
	h, err := tunnel.Connect(tunnel.HostConfig{
		RelayAddr: ln.Addr().String(), Signer: hostSigner,
		RelayKey: relaySigner.PublicKey(), Unattended: true,
		Target: sink.Addr().String(), Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	go h.Serve()

	viewerSigner, _, _ := wire.NewSigner()
	_, err = tunnel.Dial(tunnel.ViewerConfig{
		RelayAddr: ln.Addr().String(), Signer: viewerSigner,
		RelayKey: relaySigner.PublicKey(), ID: h.ID, PIN: "000000", Logger: logger,
	})
	if err == nil {
		t.Fatal("expected rejection with wrong PIN, got success")
	}
}
