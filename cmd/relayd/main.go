// Command relayd is the remotedesk relay/broker. Run it on a public VPS.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strings"

	"remotedesk/internal/config"
	"remotedesk/internal/relay"
	"remotedesk/internal/version"

	"golang.org/x/crypto/ssh"
)

func main() {
	listen := flag.String("listen", ":7700", "address to listen on")
	keyPath := flag.String("hostkey", "", "path to relay SSH host key (default: config dir)")
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

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("relayd: listen: %v", err)
	}
	log.Printf("relayd: listening on %s", ln.Addr())
	log.Printf("relayd: host key fingerprint %s", relay.FingerprintSHA256(signer.PublicKey()))
	log.Printf("relayd: pin this on agents with --relay-key:\n%s",
		strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))))

	srv := relay.New(signer, log.Default())
	log.Fatalf("relayd: %v", srv.Serve(ln))
}
