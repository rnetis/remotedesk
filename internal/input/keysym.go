package input

// keysymName maps common X11 keysyms (as sent in RFB KeyEvents) to the key
// names robotgo understands. Printable Latin-1 keysyms are handled separately
// by converting the keysym directly to a rune.
var keysymName = map[uint32]string{
	0xff08: "backspace",
	0xff09: "tab",
	0xff0d: "enter",
	0xff1b: "esc",
	0xff63: "insert",
	0xffff: "delete",
	0xff50: "home",
	0xff57: "end",
	0xff55: "pageup",
	0xff56: "pagedown",
	0xff51: "left",
	0xff52: "up",
	0xff53: "right",
	0xff54: "down",
	0xffe1: "shift", // Shift_L
	0xffe2: "shift", // Shift_R
	0xffe3: "ctrl",  // Control_L
	0xffe4: "ctrl",  // Control_R
	0xffe9: "alt",   // Alt_L
	0xffea: "alt",   // Alt_R
	0xffeb: "cmd",   // Super_L (maps to Windows/Command key)
	0xffec: "cmd",   // Super_R
	0xffbe: "f1",
	0xffbf: "f2",
	0xffc0: "f3",
	0xffc1: "f4",
	0xffc2: "f5",
	0xffc3: "f6",
	0xffc4: "f7",
	0xffc5: "f8",
	0xffc6: "f9",
	0xffc7: "f10",
	0xffc8: "f11",
	0xffc9: "f12",
	0x0020: "space",
}

// keyName resolves a keysym to a robotgo key name, or "" if unsupported.
func keyName(keysym uint32) string {
	if name, ok := keysymName[keysym]; ok {
		return name
	}
	// Printable Latin-1: the keysym equals the Unicode code point. robotgo
	// takes the lowercase character; the viewer sends Shift separately for
	// upper case.
	if keysym >= 0x21 && keysym <= 0xff {
		r := rune(keysym)
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		return string(r)
	}
	return ""
}
