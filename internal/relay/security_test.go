package relay_test

import (
	"io"
	"log"
	"net"
	"testing"
	"time"

	"remotedesk/internal/relay"
	"remotedesk/internal/tunnel"
	"remotedesk/internal/wire"

	"golang.org/x/crypto/ssh"
)

// startRelay launches a relay with the given options on a loopback port and
// returns its address and pinned host key.
func startRelay(t *testing.T, opts relay.Options) (addr string, hostKey []byte) {
	t.Helper()
	if opts.Signer == nil {
		s, _, err := wire.NewSigner()
		if err != nil {
			t.Fatal(err)
		}
		opts.Signer = s
	}
	if opts.Logger == nil {
		opts.Logger = log.New(io.Discard, "", 0)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go relay.NewWithOptions(opts).Serve(ln)
	return ln.Addr().String(), opts.Signer.PublicKey().Marshal()
}

// TestPINLockout proves the relay stops brute-force PIN guessing: after
// MaxPINAttempts wrong PINs a host is locked out, the correct PIN is refused
// during the lockout window, the host is alerted exactly once, and access is
// restored after the window elapses.
func TestPINLockout(t *testing.T) {
	logger := log.New(io.Discard, "", 0)

	relaySigner, _, _ := wire.NewSigner()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go relay.NewWithOptions(relay.Options{
		Signer:          relaySigner,
		Logger:          logger,
		MaxPINAttempts:  3,
		LockoutDuration: 600 * time.Millisecond,
	}).Serve(ln)
	relayKey := relaySigner.PublicKey()

	sink, _ := net.Listen("tcp", "127.0.0.1:0")
	defer sink.Close()

	alerts := make(chan string, 8)
	hostSigner, _, _ := wire.NewSigner()
	h, err := tunnel.Connect(tunnel.HostConfig{
		RelayAddr: ln.Addr().String(), Signer: hostSigner,
		RelayKey: relayKey, Unattended: true,
		Target: sink.Addr().String(), Logger: logger,
		OnAlert: func(msg string) { alerts <- msg },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	go h.Serve()

	dial := func(pin string) error {
		viewerSigner, _, _ := wire.NewSigner()
		s, err := tunnel.Dial(tunnel.ViewerConfig{
			RelayAddr: ln.Addr().String(), Signer: viewerSigner,
			RelayKey: relayKey, ID: h.ID, PIN: pin, Logger: logger,
		})
		if err == nil {
			s.Close()
		}
		return err
	}

	// Burn through the allowed wrong attempts; each is refused.
	for i := 0; i < 3; i++ {
		if err := dial("000000"); err == nil {
			t.Fatalf("wrong PIN attempt %d unexpectedly succeeded", i+1)
		}
	}

	// The host should have been alerted about the lockout, exactly once.
	select {
	case <-alerts:
	case <-time.After(2 * time.Second):
		t.Fatal("host was not alerted about the lockout")
	}
	select {
	case msg := <-alerts:
		t.Fatalf("host alerted more than once: %q", msg)
	case <-time.After(200 * time.Millisecond):
	}

	// While locked out, even the correct PIN is refused.
	if err := dial(h.PIN); err == nil {
		t.Fatal("correct PIN succeeded during lockout window")
	}

	// After the lockout window, the correct PIN works again.
	time.Sleep(700 * time.Millisecond)
	if err := dial(h.PIN); err != nil {
		t.Fatalf("correct PIN refused after lockout expired: %v", err)
	}
}

// TestAuthorizeRestrictsKeys proves that when an allowlist is configured, only
// agents whose key is permitted can complete the SSH handshake.
func TestAuthorizeRestrictsKeys(t *testing.T) {
	logger := log.New(io.Discard, "", 0)

	allowedSigner, _, _ := wire.NewSigner()
	allowedMarshaled := string(allowedSigner.PublicKey().Marshal())

	relaySigner, _, _ := wire.NewSigner()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go relay.NewWithOptions(relay.Options{
		Signer: relaySigner,
		Logger: logger,
		Authorize: func(k ssh.PublicKey) bool {
			return string(k.Marshal()) == allowedMarshaled
		},
	}).Serve(ln)
	relayKey := relaySigner.PublicKey()

	sink, _ := net.Listen("tcp", "127.0.0.1:0")
	defer sink.Close()

	// A host with the allowed key connects successfully.
	h, err := tunnel.Connect(tunnel.HostConfig{
		RelayAddr: ln.Addr().String(), Signer: allowedSigner,
		RelayKey: relayKey, Unattended: true,
		Target: sink.Addr().String(), Logger: logger,
	})
	if err != nil {
		t.Fatalf("allowed key was refused: %v", err)
	}
	h.Close()

	// A host with a different key is refused at the SSH layer.
	otherSigner, _, _ := wire.NewSigner()
	if _, err := tunnel.Connect(tunnel.HostConfig{
		RelayAddr: ln.Addr().String(), Signer: otherSigner,
		RelayKey: relayKey, Unattended: true,
		Target: sink.Addr().String(), Logger: logger,
	}); err == nil {
		t.Fatal("disallowed key was accepted")
	}
}

// TestHandshakeTimeout proves a client that connects but never completes the SSH
// handshake is dropped rather than holding the connection open indefinitely.
func TestHandshakeTimeout(t *testing.T) {
	addr, _ := startRelay(t, relay.Options{HandshakeTimeout: 200 * time.Millisecond})

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Never send an SSH client banner. The relay writes its banner, waits for
	// ours, hits the handshake deadline, and closes. Reading to EOF should
	// therefore return well before our generous read deadline.
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	start := time.Now()
	io.Copy(io.Discard, conn) // drains the server banner, then unblocks at EOF
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("connection not closed by handshake timeout (waited %s)", elapsed)
	}
}
