// Package tray provides the host's user interface: it displays the connection
// ID/PIN and asks the user to accept or reject incoming viewers.
//
// Two implementations are provided: a Console UI (works everywhere, including
// headless servers) and a Systray UI (a real tray icon on a desktop session).
package tray

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// ConfirmTimeout bounds how long the UI waits for the user to decide.
const ConfirmTimeout = 45 * time.Second

// UI is the host's front-end.
type UI interface {
	// SetIdentity updates the displayed connection ID and PIN.
	SetIdentity(id, pin string)
	// Confirm asks the user to allow an incoming viewer session. It returns
	// true to accept. It may block on user interaction.
	Confirm(session string) bool
	// Run runs the UI event loop and blocks until the process exits. For some
	// backends (e.g. systray) this must be called on the main goroutine.
	Run()
}

// ConsoleUI prints identity to the log and prompts on stdin for consent. If
// AutoAccept is set, incoming sessions are accepted without prompting.
type ConsoleUI struct {
	AutoAccept bool

	mu     sync.Mutex
	reader *bufio.Reader
}

// NewConsole returns a ConsoleUI.
func NewConsole(autoAccept bool) *ConsoleUI {
	return &ConsoleUI{AutoAccept: autoAccept, reader: bufio.NewReader(os.Stdin)}
}

func (c *ConsoleUI) SetIdentity(id, pin string) {
	log.Printf("┌─ remotedesk ─────────────────")
	log.Printf("│ Your ID:  %s", id)
	log.Printf("│ PIN:      %s", pin)
	log.Printf("└──────────────────────────────")
}

func (c *ConsoleUI) Confirm(session string) bool {
	if c.AutoAccept {
		log.Printf("tray: auto-accepting session %s", session)
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Printf("\nIncoming connection request (%s). Allow? [y/N]: ", session)
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

// Run blocks forever; the console UI has no event loop of its own.
func (c *ConsoleUI) Run() { select {} }
