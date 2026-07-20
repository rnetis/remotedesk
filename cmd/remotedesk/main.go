// Command remotedesk is the unified desktop application: a single window that
// is both host (this machine can be controlled — it shows an ID/PIN and prompts
// to accept incoming viewers) and viewer (connect out to another machine's
// ID/PIN and control it). The relay, and the standalone host/viewer CLIs, remain
// available for headless and scripted use.
package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"

	"remotedesk/internal/capture"
	"remotedesk/internal/config"
	"remotedesk/internal/input"
	"remotedesk/internal/version"

	"fyne.io/fyne/v2/app"
)

func main() {
	relayAddr := flag.String("relay", "127.0.0.1:7700", "relay address")
	relayKeyFlag := flag.String("relay-key", "", "pin the relay host key (authorized-keys line or file path)")
	unattended := flag.Bool("unattended", false, "auto-accept incoming viewers (no prompt)")
	keyPath := flag.String("key", "", "path to agent key (default: config dir)")
	connectID := flag.String("connect", "", "on launch, immediately connect to this remote ID (with --connect-pin)")
	connectPIN := flag.String("connect-pin", "", "PIN for --connect")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version.String())
		return
	}

	if *keyPath == "" {
		dir, err := config.Dir()
		if err != nil {
			log.Fatalf("remotedesk: config dir: %v", err)
		}
		*keyPath = filepath.Join(dir, "agent_key")
	}
	signer, err := config.LoadOrCreateSigner(*keyPath)
	if err != nil {
		log.Fatalf("remotedesk: key: %v", err)
	}
	relayKey, err := config.ParseRelayKey(*relayKeyFlag)
	if err != nil {
		log.Fatalf("remotedesk: %v", err)
	}

	a := &App{
		relayAddr:  *relayAddr,
		signer:     signer,
		relayKey:   relayKey,
		unattended: *unattended,
		sink:       input.New(),
		connectID:  *connectID,
		connectPIN: *connectPIN,
	}

	// Screen capture is what makes this machine shareable. If it isn't available
	// (e.g. a headless/locked-down session), run viewer-only rather than failing.
	if screen, err := capture.Primary(); err != nil {
		log.Printf("remotedesk: screen capture unavailable, viewer-only: %v", err)
	} else {
		b := screen.Bounds()
		log.Printf("remotedesk: sharing %dx%d display", b.Dx(), b.Dy())
		a.screen = screen
	}

	fyneApp := app.NewWithID("io.remotedesk.app")
	a.fyneApp = fyneApp
	a.win = fyneApp.NewWindow("remotedesk")
	a.Run()
}
