// Command relayd is the remotedesk relay/broker. Run it on a public VPS.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"remotedesk/internal/config"
	"remotedesk/internal/relay"
	"remotedesk/internal/version"

	"golang.org/x/crypto/ssh"
)

func main() {
	listen := flag.String("listen", ":7700", "address to listen on")
	keyPath := flag.String("hostkey", "", "path to relay SSH host key (default: config dir)")
	authKeys := flag.String("authorized-keys", "", "restrict access to agents whose key is in this authorized_keys file (default: open)")
	pinAttempts := flag.Int("pin-attempts", 5, "wrong PINs tolerated before a host is locked out")
	lockout := flag.Duration("lockout", 30*time.Second, "how long a host is locked out after too many bad PINs")
	handshakeTimeout := flag.Duration("handshake-timeout", 15*time.Second, "deadline for the SSH handshake and a peer's first channel")
	maxConns := flag.Int("max-conns", 512, "maximum concurrent connections serviced at once")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.String())
		return
	}

	if *keyPath == "" {
		dir, err := config.Dir()
		if err != nil {
			log.Fatalf("relayd: config dir: %v", err)
		}
		*keyPath = filepath.Join(dir, "relay_host_key")
	}
	signer, err := config.LoadOrCreateSigner(*keyPath)
	if err != nil {
		log.Fatalf("relayd: host key: %v", err)
	}

	var authorize func(ssh.PublicKey) bool
	if *authKeys != "" {
		authorize, err = loadAuthorizedKeys(*authKeys)
		if err != nil {
			log.Fatalf("relayd: authorized-keys: %v", err)
		}
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("relayd: listen: %v", err)
	}
	log.Printf("relayd: listening on %s", ln.Addr())
	log.Printf("relayd: host key fingerprint %s", relay.FingerprintSHA256(signer.PublicKey()))
	log.Printf("relayd: pin this on agents with --relay-key:\n%s",
		strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))))
	if authorize == nil {
		log.Printf("relayd: WARNING no --authorized-keys set; any key may connect")
	}

	srv := relay.NewWithOptions(relay.Options{
		Signer:           signer,
		Logger:           log.Default(),
		Authorize:        authorize,
		MaxPINAttempts:   *pinAttempts,
		LockoutDuration:  *lockout,
		HandshakeTimeout: *handshakeTimeout,
		MaxConns:         *maxConns,
	})

	// Serve until a fatal error or a shutdown signal. Closing the listener makes
	// Serve return cleanly so the process exits 0 under systemd/docker stop.
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		log.Fatalf("relayd: %v", err)
	case s := <-sig:
		log.Printf("relayd: received %s, shutting down", s)
		ln.Close()
		<-errCh
	}
}

// loadAuthorizedKeys parses an OpenSSH authorized_keys file and returns a
// predicate that reports whether a presented public key is in it.
func loadAuthorizedKeys(path string) (func(ssh.PublicKey) bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool)
	for len(data) > 0 {
		key, _, _, rest, perr := ssh.ParseAuthorizedKey(data)
		if perr != nil {
			break
		}
		allowed[string(key.Marshal())] = true
		data = rest
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("no keys parsed from %s", path)
	}
	return func(k ssh.PublicKey) bool { return allowed[string(k.Marshal())] }, nil
}
