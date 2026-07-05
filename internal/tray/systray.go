package tray

import (
	"fmt"
	"sync"
	"time"

	"fyne.io/systray"
)

// SystrayUI shows a real tray icon with the connection ID/PIN and presents
// Accept/Reject menu items when a viewer requests a session.
type SystrayUI struct {
	mu       sync.Mutex
	ready    bool
	id, pin  string
	idItem   *systray.MenuItem
	pinItem  *systray.MenuItem
	statusIt *systray.MenuItem
	acceptIt *systray.MenuItem
	rejectIt *systray.MenuItem
	pending  chan bool
}

// NewSystray returns a SystrayUI.
func NewSystray() *SystrayUI { return &SystrayUI{} }

// Run starts the tray event loop and blocks. Must run on the main goroutine.
func (s *SystrayUI) Run() { systray.Run(s.onReady, func() {}) }

func (s *SystrayUI) onReady() {
	systray.SetTitle("remotedesk")
	systray.SetTooltip("remotedesk — waiting for connections")

	s.mu.Lock()
	s.idItem = systray.AddMenuItem("ID: —", "Your connection ID")
	s.pinItem = systray.AddMenuItem("PIN: —", "Your session PIN")
	systray.AddSeparator()
	s.statusIt = systray.AddMenuItem("Idle", "Connection status")
	s.acceptIt = systray.AddMenuItem("Accept", "Accept the pending connection")
	s.rejectIt = systray.AddMenuItem("Reject", "Reject the pending connection")
	s.acceptIt.Hide()
	s.rejectIt.Hide()
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "Exit remotedesk")
	s.ready = true
	if s.id != "" {
		s.applyIdentityLocked()
	}
	s.mu.Unlock()

	go func() {
		for {
			select {
			case <-s.acceptIt.ClickedCh:
				s.resolve(true)
			case <-s.rejectIt.ClickedCh:
				s.resolve(false)
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func (s *SystrayUI) SetIdentity(id, pin string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id, s.pin = id, pin
	if s.ready {
		s.applyIdentityLocked()
	}
}

func (s *SystrayUI) applyIdentityLocked() {
	s.idItem.SetTitle("ID:  " + s.id)
	s.pinItem.SetTitle("PIN: " + s.pin)
}

func (s *SystrayUI) Confirm(session string) bool {
	s.mu.Lock()
	if !s.ready {
		s.mu.Unlock()
		return false
	}
	result := make(chan bool, 1)
	s.pending = result
	s.statusIt.SetTitle("Incoming: " + session)
	s.acceptIt.Show()
	s.rejectIt.Show()
	systray.SetTooltip("remotedesk — incoming connection")
	s.mu.Unlock()

	select {
	case ok := <-result:
		s.finishConfirm()
		return ok
	case <-time.After(ConfirmTimeout):
		s.finishConfirm()
		return false
	}
}

func (s *SystrayUI) resolve(ok bool) {
	s.mu.Lock()
	ch := s.pending
	s.pending = nil
	s.mu.Unlock()
	if ch != nil {
		select {
		case ch <- ok:
		default:
		}
	}
}

func (s *SystrayUI) finishConfirm() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acceptIt.Hide()
	s.rejectIt.Hide()
	s.statusIt.SetTitle("Idle")
	systray.SetTooltip(fmt.Sprintf("remotedesk — ID %s", s.id))
}
