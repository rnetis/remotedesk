// Package input injects viewer input events into the local machine as an
// rfb.Sink, backed by go-vgo/robotgo.
package input

import "github.com/go-vgo/robotgo"

// Button bitmask positions used by the RFB PointerEvent message.
const (
	btnLeft      = 1 << 0
	btnMiddle    = 1 << 1
	btnRight     = 1 << 2
	btnWheelUp   = 1 << 3
	btnWheelDown = 1 << 4
)

// Robot injects input via robotgo. It is not safe for concurrent use; the RFB
// session drives it from a single goroutine.
type Robot struct {
	prevButtons uint8
}

// New returns an input Robot.
func New() *Robot { return &Robot{} }

// PointerEvent moves the cursor and translates button-mask transitions into
// press/release and scroll actions.
func (r *Robot) PointerEvent(x, y int, buttons uint8) {
	robotgo.Move(x, y)

	r.toggleButton(buttons, btnLeft, "left")
	r.toggleButton(buttons, btnMiddle, "center")
	r.toggleButton(buttons, btnRight, "right")

	// Wheel events are momentary presses; act on the leading edge.
	if buttons&btnWheelUp != 0 && r.prevButtons&btnWheelUp == 0 {
		robotgo.ScrollDir(1, "up")
	}
	if buttons&btnWheelDown != 0 && r.prevButtons&btnWheelDown == 0 {
		robotgo.ScrollDir(1, "down")
	}
	r.prevButtons = buttons
}

func (r *Robot) toggleButton(buttons uint8, bit uint8, name string) {
	now := buttons&bit != 0
	was := r.prevButtons&bit != 0
	if now == was {
		return
	}
	if now {
		robotgo.Toggle(name, "down")
	} else {
		robotgo.Toggle(name, "up")
	}
}

// KeyEvent presses or releases a key identified by an X11 keysym.
func (r *Robot) KeyEvent(keysym uint32, down bool) {
	name := keyName(keysym)
	if name == "" {
		return
	}
	if down {
		robotgo.KeyToggle(name, "down")
	} else {
		robotgo.KeyToggle(name, "up")
	}
}
