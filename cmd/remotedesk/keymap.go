package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// specialKeys maps Fyne's named keys (and the desktop modifier keys) to the X11
// keysyms the RFB protocol carries. Printable keys are handled in keysym() by
// mapping the key name's rune directly, since an X11 keysym for a Latin-1
// character equals its code point.
var specialKeys = map[fyne.KeyName]uint32{
	fyne.KeyReturn:    0xff0d,
	fyne.KeyEnter:     0xff0d, // keypad Enter -> Return
	fyne.KeyEscape:    0xff1b,
	fyne.KeyBackspace: 0xff08,
	fyne.KeyTab:       0xff09,
	fyne.KeySpace:     0x0020,
	fyne.KeyLeft:      0xff51,
	fyne.KeyUp:        0xff52,
	fyne.KeyRight:     0xff53,
	fyne.KeyDown:      0xff54,
	fyne.KeyHome:      0xff50,
	fyne.KeyEnd:       0xff57,
	fyne.KeyPageUp:    0xff55,
	fyne.KeyPageDown:  0xff56,
	fyne.KeyInsert:    0xff63,
	fyne.KeyDelete:    0xffff,
	fyne.KeyF1:        0xffbe,
	fyne.KeyF2:        0xffbf,
	fyne.KeyF3:        0xffc0,
	fyne.KeyF4:        0xffc1,
	fyne.KeyF5:        0xffc2,
	fyne.KeyF6:        0xffc3,
	fyne.KeyF7:        0xffc4,
	fyne.KeyF8:        0xffc5,
	fyne.KeyF9:        0xffc6,
	fyne.KeyF10:       0xffc7,
	fyne.KeyF11:       0xffc8,
	fyne.KeyF12:       0xffc9,

	// Modifier keys arrive as their own down/up events, so the remote can hold
	// them while other keys are pressed (e.g. Ctrl+C, Shift+letter).
	desktop.KeyShiftLeft:    0xffe1,
	desktop.KeyShiftRight:   0xffe2,
	desktop.KeyControlLeft:  0xffe3,
	desktop.KeyControlRight: 0xffe4,
	desktop.KeyAltLeft:      0xffe9,
	desktop.KeyAltRight:     0xffea,
	desktop.KeySuperLeft:    0xffeb,
	desktop.KeySuperRight:   0xffec,
}

// keysym returns the X11 keysym for a Fyne key name, or 0 if unsupported.
// Letters are lowercased and shift is sent separately (as its own key event),
// matching what the host's input layer expects.
func keysym(name fyne.KeyName) uint32 {
	if k, ok := specialKeys[name]; ok {
		return k
	}
	s := string(name)
	if len(s) == 1 {
		r := s[0]
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if r >= 0x20 && r <= 0x7e {
			return uint32(r)
		}
	}
	return 0
}
