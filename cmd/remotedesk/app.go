package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"remotedesk/internal/rfb"
	"remotedesk/internal/rfbclient"
	"remotedesk/internal/tunnel"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"golang.org/x/crypto/ssh"
)

// consentTimeout bounds how long the Accept dialog waits before auto-rejecting.
const consentTimeout = 45 * time.Second

// App is the unified host+viewer GUI application. One window shows this
// machine's ID/PIN (so it can be controlled) and offers a form to connect out
// to another machine (so it can control).
type App struct {
	fyneApp fyne.App
	win     fyne.Window

	relayAddr  string
	signer     ssh.Signer
	relayKey   ssh.PublicKey
	unattended bool

	screen rfb.Screen // nil if screen capture is unavailable (viewer-only)
	sink   rfb.Sink

	connectID  string // if set, auto-connect to this remote on launch
	connectPIN string

	home fyne.CanvasObject

	// Home-screen widgets updated from background goroutines (via fyne.Do).
	idLabel     *widget.Label
	pinLabel    *widget.Label
	statusLabel *widget.Label

	mu           sync.Mutex
	activeClient *rfbclient.Client // non-nil while a remote session is open
}

// Run builds the UI and blocks running the event loop until the window closes.
func (a *App) Run() {
	a.home = a.homeContent()
	a.win.SetContent(a.home)
	a.win.Resize(fyne.NewSize(440, 480))
	a.win.SetMaster()

	if a.screen != nil {
		go a.runHost()
	} else {
		a.setStatus("Screen capture unavailable — you can connect out, but this computer can't be controlled")
	}
	if a.connectID != "" {
		go a.connect(a.connectID, a.connectPIN)
	}
	a.win.ShowAndRun()
}

// homeContent builds the landing screen.
func (a *App) homeContent() fyne.CanvasObject {
	mono := fyne.TextStyle{Monospace: true, Bold: true}
	a.idLabel = widget.NewLabelWithStyle("—", fyne.TextAlignLeading, mono)
	a.pinLabel = widget.NewLabelWithStyle("—", fyne.TextAlignLeading, mono)
	a.statusLabel = widget.NewLabel("Connecting to relay…")
	a.statusLabel.Wrapping = fyne.TextWrapWord

	self := widget.NewCard("This computer", "Share these so someone can control it",
		container.NewVBox(
			container.NewHBox(widget.NewLabel("Your ID:"), a.idLabel),
			container.NewHBox(widget.NewLabel("PIN:    "), a.pinLabel),
		),
	)

	remoteID := widget.NewEntry()
	remoteID.SetPlaceHolder("123-456-789")
	remotePIN := widget.NewEntry()
	remotePIN.SetPlaceHolder("PIN")
	do := func() { a.connect(remoteID.Text, remotePIN.Text) }
	remotePIN.OnSubmitted = func(string) { do() }
	connectBtn := widget.NewButton("Connect", do)
	connectBtn.Importance = widget.HighImportance

	remote := widget.NewCard("Connect to a remote computer", "",
		container.NewVBox(
			widget.NewForm(
				widget.NewFormItem("Remote ID", remoteID),
				widget.NewFormItem("PIN", remotePIN),
			),
			connectBtn,
		),
	)

	return container.NewVBox(self, remote, widget.NewSeparator(), a.statusLabel)
}

// --- host side (being controlled) ---

// runHost keeps this machine registered with the relay, reconnecting with
// capped backoff, and serves the embedded RFB server to authorized viewers.
func (a *App) runHost() {
	const minBackoff, maxBackoff = time.Second, 30 * time.Second
	backoff := minBackoff
	for {
		h, err := tunnel.Connect(a.hostConfig())
		if err != nil {
			a.setStatus(fmt.Sprintf("Relay unreachable: %v — retrying in %s", err, backoff))
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = minBackoff
		a.setIdentity(h.ID, h.PIN)
		a.setStatus("Ready — share your ID and PIN to let someone control this computer")
		err = h.Serve()
		a.setIdentity("—", "—")
		a.setStatus(fmt.Sprintf("Relay connection lost: %v — reconnecting…", err))
	}
}

func (a *App) hostConfig() tunnel.HostConfig {
	return tunnel.HostConfig{
		RelayAddr:  a.relayAddr,
		Signer:     a.signer,
		RelayKey:   a.relayKey,
		Unattended: a.unattended,
		Handler:    a.serveRFB,
		OnIncoming: a.confirmIncoming,
		OnAlert:    a.showAlert,
		Logger:     log.Default(),
	}
}

func (a *App) serveRFB(stream io.ReadWriteCloser) {
	err := rfb.Serve(stream, rfb.Options{Screen: a.screen, Sink: a.sink, Name: "remotedesk"})
	if err != nil {
		log.Printf("host: rfb session ended: %v", err)
	}
}

// confirmIncoming asks the user (on the UI thread) whether to allow an incoming
// viewer, blocking the calling relay goroutine until they decide or it times out.
func (a *App) confirmIncoming(session string) bool {
	if a.unattended {
		return true
	}
	result := make(chan bool, 1)
	fyne.Do(func() {
		dialog.ShowConfirm("Incoming connection",
			"Allow this computer to be viewed and controlled remotely?",
			func(ok bool) { result <- ok }, a.win)
	})
	select {
	case ok := <-result:
		return ok
	case <-time.After(consentTimeout):
		return false
	}
}

func (a *App) showAlert(msg string) {
	fyne.Do(func() { dialog.ShowInformation("Security alert", msg, a.win) })
}

func (a *App) setIdentity(id, pin string) {
	fyne.Do(func() {
		a.idLabel.SetText(id)
		a.pinLabel.SetText(pin)
	})
}

func (a *App) setStatus(msg string) {
	fyne.Do(func() { a.statusLabel.SetText(msg) })
}

// --- viewer side (controlling a remote) ---

// connect dials the remote and, on success, swaps the window to the remote view.
func (a *App) connect(id, pin string) {
	id = normalizeID(id)
	pin = strings.TrimSpace(pin)
	if id == "" || pin == "" {
		dialog.ShowError(errors.New("enter a remote ID and PIN"), a.win)
		return
	}
	a.setStatus("Connecting to " + id + "…")
	go func() {
		cfg := tunnel.ViewerConfig{
			RelayAddr: a.relayAddr, Signer: a.signer, RelayKey: a.relayKey,
			ID: id, PIN: pin, Logger: log.Default(),
		}
		stream, err := tunnel.Dial(cfg)
		if err != nil {
			a.fail("Connection", err)
			return
		}
		client, err := rfbclient.Connect(stream, "")
		if err != nil {
			stream.Close()
			a.fail("Session", err)
			return
		}
		// Wire up the view (which sets OnUpdate and requests the first frame) on
		// the UI thread, then start the decode loop — so OnUpdate is set before
		// Run() can read it, and the `go` gives the needed happens-before edge.
		fyne.Do(func() {
			a.showRemote(client)
			go func() {
				if err := client.Run(); err != nil {
					log.Printf("viewer: session ended: %v", err)
				}
				a.disconnect() // remote closed or errored -> back to home
			}()
		})
	}()
}

func (a *App) showRemote(client *rfbclient.Client) {
	a.mu.Lock()
	a.activeClient = client
	a.mu.Unlock()

	rv := newRemoteView(client)
	disconnect := widget.NewButton("Disconnect", func() { a.disconnect() })
	toolbar := container.NewHBox(disconnect, widget.NewLabel(fmt.Sprintf("%dx%d", client.Width, client.Height)))
	content := container.NewBorder(toolbar, nil, nil, nil, container.NewScroll(rv))

	a.win.SetContent(content)
	a.win.Resize(fyne.NewSize(float32(min(client.Width, 1280)), float32(min(client.Height, 800)+48)))
	a.win.Canvas().Focus(rv)
}

// disconnect tears down any active remote session and returns to the home
// screen. Safe to call from either the UI thread or a background goroutine, and
// idempotent if the session is already closed.
func (a *App) disconnect() {
	a.mu.Lock()
	c := a.activeClient
	a.activeClient = nil
	a.mu.Unlock()
	if c == nil {
		return
	}
	c.Close()
	fyne.Do(func() {
		a.win.SetContent(a.home)
		a.win.Resize(fyne.NewSize(440, 480))
	})
	a.setStatus("Ready")
}

func (a *App) fail(stage string, err error) {
	fyne.Do(func() { dialog.ShowError(fmt.Errorf("%s failed: %w", stage, err), a.win) })
	a.setStatus("Ready")
}

// normalizeID accepts an ID with or without separators and, when it holds
// exactly nine digits, formats it as the NNN-NNN-NNN the relay registered.
func normalizeID(s string) string {
	var digits []rune
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	if len(digits) != 9 {
		return strings.TrimSpace(s)
	}
	d := string(digits)
	return d[0:3] + "-" + d[3:6] + "-" + d[6:9]
}
