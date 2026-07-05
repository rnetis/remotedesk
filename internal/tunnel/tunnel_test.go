package tunnel_test

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

// startEcho returns a listener that echoes each line back prefixed with "E:".
func startEcho(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					c.Write([]byte("E:" + sc.Text() + "\n"))
				}
			}()
		}
	}()
	return ln
}

// TestLocalForward exercises the viewer's ssh -L path: a plain TCP client
// connects to the local forward and reaches the host's target through the relay.
func TestLocalForward(t *testing.T) {
	logger := log.New(io.Discard, "", 0)

	relaySigner, _, _ := wire.NewSigner()
	relayLn, _ := net.Listen("tcp", "127.0.0.1:0")
	defer relayLn.Close()
	go relay.New(relaySigner, logger).Serve(relayLn)

	echo := startEcho(t)
	defer echo.Close()

	hostSigner, _, _ := wire.NewSigner()
	h, err := tunnel.Connect(tunnel.HostConfig{
		RelayAddr: relayLn.Addr().String(), Signer: hostSigner,
		RelayKey: relaySigner.PublicKey(), Unattended: true,
		Target: echo.Addr().String(), Logger: logger,
	})
	if err != nil {
		t.Fatalf("host connect: %v", err)
	}
	defer h.Close()
	go h.Serve()

	viewerSigner, _, _ := wire.NewSigner()
	fwdLn, _ := net.Listen("tcp", "127.0.0.1:0")
	go tunnel.LocalForwardListener(tunnel.ViewerConfig{
		RelayAddr: relayLn.Addr().String(), Signer: viewerSigner,
		RelayKey: relaySigner.PublicKey(), ID: h.ID, PIN: h.PIN, Logger: logger,
	}, fwdLn)

	// Connect to the local forward like an ordinary VNC client would.
	var conn net.Conn
	for i := 0; i < 50; i++ {
		conn, err = net.Dial("tcp", fwdLn.Addr().String())
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if conn == nil {
		t.Fatalf("could not connect to local forward: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if line != "E:ping\n" {
		t.Fatalf("round-trip through local forward = %q, want %q", line, "E:ping\n")
	}
}
