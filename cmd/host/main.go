// Command host is the remotedesk host agent (the machine being controlled). It
// captures the screen and injects input via an embedded RFB server, exposing it
// both through the relay tunnel and (optionally) on a local port for testing.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"path/filepath"
	"time"

	"remotedesk/internal/capture"
	"remotedesk/internal/config"
	"remotedesk/internal/input"
	"remotedesk/internal/rfb"
	"remotedesk/internal/tray"
	"remotedesk/internal/tunnel"
	"remotedesk/internal/version"
)

func main() {
	relayAddr := flag.String("relay", "127.0.0.1:7700", "relay address")
	relayKeyFlag := flag.String("relay-key", "", "pin the relay host key (authorized-keys line or file path)")
	unattended := flag.Bool("unattended", false, "auto-accept incoming viewers")
	keyPath := flag.String("key", "", "path to agent key (default: config dir)")
	vncListen := flag.String("vnc-listen", "", "also serve RFB on this local address (e.g. 127.0.0.1:5900)")
	vncPassword := flag.String("vnc-password", "", "VNC password for --vnc-listen (default: generated)")
	noRelay := flag.Bool("no-relay", false, "skip the relay; use with --vnc-listen for local testing")
	useTray := flag.Bool("tray", false, "show a system-tray UI instead of the console")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.String())
		return
	}

	screen, err := capture.Primary()
	if err != nil {
		log.Fatalf("host: screen capture unavailable: %v", err)
	}
	b := screen.Bounds()
	log.Printf("host: capturing %dx%d display", b.Dx(), b.Dy())
	sink := input.New()

	// Optional local RFB listener for direct testing with a standard viewer.
	if *vncListen != "" {
		pw := *vncPassword
		if pw == "" {
			pw = "remotedesk"
		}
		go serveLocal(*vncListen, pw, screen, sink)
		log.Printf("host: local RFB on %s (password %q)", *vncListen, pw)
	}

	if *noRelay {
		select {} // block forever serving the local listener
	}

	if *keyPath == "" {
		dir, err := config.Dir()
		if err != nil {
			log.Fatalf("host: config dir: %v", err)
		}
		*keyPath = filepath.Join(dir, "host_key")
	}
	signer, err := config.LoadOrCreateSigner(*keyPath)
	if err != nil {
		log.Fatalf("host: key: %v", err)
	}
	relayKey, err := config.ParseRelayKey(*relayKeyFlag)
	if err != nil {
		log.Fatalf("host: %v", err)
	}

	// Front-end: a real tray on a desktop, otherwise the console.
	var ui tray.UI
	if *useTray {
		ui = tray.NewSystray()
	} else {
		ui = tray.NewConsole(*unattended)
	}

	// Over the tunnel, the relay's PIN check plus the Accept prompt gate access,
	// so the RFB layer itself runs without a second password.
	handler := func(stream io.ReadWriteCloser) {
		err := rfb.Serve(stream, rfb.Options{Screen: screen, Sink: sink, Name: "remotedesk"})
		if err != nil {
			log.Printf("host: rfb session ended: %v", err)
		}
	}

	cfg := tunnel.HostConfig{
		RelayAddr:  *relayAddr,
		Signer:     signer,
		RelayKey:   relayKey,
		Unattended: *unattended,
		Handler:    handler,
		OnIncoming: ui.Confirm,
		Logger:     log.Default(),
	}

	// Maintain the relay connection in the background, reconnecting with backoff
	// if it drops. The UI owns the main goroutine (systray requires this).
	go serveWithReconnect(cfg, ui)
	ui.Run()
}

// serveWithReconnect keeps the host registered with the relay, re-registering
// after any disconnect with capped exponential backoff.
func serveWithReconnect(cfg tunnel.HostConfig, ui tray.UI) {
	const minBackoff, maxBackoff = time.Second, 30 * time.Second
	backoff := minBackoff
	for {
		h, err := tunnel.Connect(cfg)
		if err != nil {
			log.Printf("host: connect failed: %v; retrying in %s", err, backoff)
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = minBackoff
		ui.SetIdentity(h.ID, h.PIN)
		err = h.Serve()
		log.Printf("host: relay connection lost (%v); reconnecting…", err)
	}
}

func serveLocal(addr, password string, screen rfb.Screen, sink rfb.Sink) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("host: vnc-listen: %v", err)
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			err := rfb.Serve(c, rfb.Options{
				Screen: screen, Sink: sink, Password: password, Name: "remotedesk",
			})
			if err != nil {
				log.Printf("host: local rfb session ended: %v", err)
			}
		}()
	}
}
