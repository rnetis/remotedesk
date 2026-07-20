package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

func TestKeysym(t *testing.T) {
	cases := []struct {
		name fyne.KeyName
		want uint32
	}{
		{fyne.KeyA, 0x61},           // 'a' (lowercase; shift sent separately)
		{fyne.KeyZ, 0x7a},           // 'z'
		{fyne.Key0, 0x30},           // '0'
		{fyne.Key9, 0x39},           // '9'
		{fyne.KeySpace, 0x20},       // space
		{fyne.KeyReturn, 0xff0d},    // Return
		{fyne.KeyEnter, 0xff0d},     // keypad Enter -> Return
		{fyne.KeyEscape, 0xff1b},    // Escape
		{fyne.KeyBackspace, 0xff08}, // Backspace
		{fyne.KeyLeft, 0xff51},      // arrows
		{fyne.KeyF5, 0xffc2},        // function key
		{fyne.KeyMinus, 0x2d},       // '-'
		{fyne.KeySlash, 0x2f},       // '/'
		{desktop.KeyShiftLeft, 0xffe1},
		{desktop.KeyControlLeft, 0xffe3},
		{desktop.KeyAltRight, 0xffea},
		{desktop.KeySuperLeft, 0xffeb},
		{fyne.KeyUnknown, 0}, // unsupported -> 0 (ignored)
	}
	for _, c := range cases {
		if got := keysym(c.name); got != c.want {
			t.Errorf("keysym(%q) = %#x, want %#x", c.name, got, c.want)
		}
	}
}

func TestNormalizeID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"314798609", "314-798-609"},   // bare 9 digits -> formatted
		{"314-798-609", "314-798-609"}, // already formatted -> unchanged
		{" 314 798 609 ", "314-798-609"},
		{"12345", "12345"}, // not 9 digits -> trimmed as-is
		{"  abc  ", "abc"},
	}
	for _, c := range cases {
		if got := normalizeID(c.in); got != c.want {
			t.Errorf("normalizeID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
