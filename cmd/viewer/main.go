// Command viewer is the remotedesk client. By default it opens a built-in
// window that renders the remote screen and forwards input. With --forward it
// instead exposes the tunnel as a local port for an external VNC client (the
// ssh -L equivalent).
package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"

	"remotedesk/internal/config"
	"remotedesk/internal/rfbclient"
	"remotedesk/internal/tunnel"
	"remotedesk/internal/version"

	"github.com/hajimehoshi/ebiten/v2"
)

func main() {
	relayAddr := flag.String("relay", "127.0.0.1:7700", "relay address")
	relayKeyFlag := flag.String("relay-key", "", "pin the relay host key (authorized-keys line or file path)")
	hostID := flag.String("id", "", "host connection ID")
	pin := flag.String("pin", "", "session PIN")
	keyPath := flag.String("key", "", "path to agent key (default: config dir)")
	forward := flag.Bool("forward", false, "expose a local port for an external VNC client instead of the built-in window")
	listen := flag.String("listen", "127.0.0.1:5901", "local address for --forward mode")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.String())
		return
	}

	if *hostID == "" || *pin == "" {
		log.Fatal("viewer: --id and --pin are required")
	}
	if *keyPath == "" {
		dir, err := config.Dir()
		if err != nil {
			log.Fatalf("viewer: config dir: %v", err)
		}
		*keyPath = filepath.Join(dir, "viewer_key")
	}
	signer, err := config.LoadOrCreateSigner(*keyPath)
	if err != nil {
		log.Fatalf("viewer: key: %v", err)
	}
	relayKey, err := config.ParseRelayKey(*relayKeyFlag)
	if err != nil {
		log.Fatalf("viewer: %v", err)
	}

	cfg := tunnel.ViewerConfig{
		RelayAddr: *relayAddr,
		Signer:    signer,
		RelayKey:  relayKey,
		ID:        *hostID,
		PIN:       *pin,
		Logger:    log.Default(),
	}

	if *forward {
		log.Printf("viewer: point a VNC client at %s", *listen)
		log.Fatalf("viewer: %v", tunnel.LocalForward(cfg, *listen))
	}

	// Built-in GUI: open one tunnel and drive an RFB client through it.
	stream, err := tunnel.Dial(cfg)
	if err != nil {
		log.Fatalf("viewer: connect: %v", err)
	}
	client, err := rfbclient.Connect(stream, "")
	if err != nil {
		log.Fatalf("viewer: rfb: %v", err)
	}
	go func() {
		if err := client.Run(); err != nil {
			log.Printf("viewer: session ended: %v", err)
		}
	}()

	g := newGame(client)
	ebiten.SetWindowSize(client.Width, client.Height)
	ebiten.SetWindowTitle("remotedesk — " + *hostID)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	if err := ebiten.RunGame(g); err != nil {
		log.Fatalf("viewer: %v", err)
	}
}
