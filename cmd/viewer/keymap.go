package main

import "github.com/hajimehoshi/ebiten/v2"

// keysym maps an ebiten key to its X11 keysym for RFB KeyEvents. Returns 0 when
// unmapped.
func keysym(k ebiten.Key) uint32 {
	// Letters a–z (RFB sends the lowercase keysym; Shift is sent separately).
	if k >= ebiten.KeyA && k <= ebiten.KeyZ {
		return 0x61 + uint32(k-ebiten.KeyA)
	}
	// Digits 0–9.
	if k >= ebiten.KeyDigit0 && k <= ebiten.KeyDigit9 {
		return 0x30 + uint32(k-ebiten.KeyDigit0)
	}
	// Function keys F1–F12.
	if k >= ebiten.KeyF1 && k <= ebiten.KeyF12 {
		return 0xffbe + uint32(k-ebiten.KeyF1)
	}
	if v, ok := specialKeys[k]; ok {
		return v
	}
	return 0
}

var specialKeys = map[ebiten.Key]uint32{
	ebiten.KeySpace:        0x20,
	ebiten.KeyEnter:        0xff0d,
	ebiten.KeyBackspace:    0xff08,
	ebiten.KeyTab:          0xff09,
	ebiten.KeyEscape:       0xff1b,
	ebiten.KeyDelete:       0xffff,
	ebiten.KeyInsert:       0xff63,
	ebiten.KeyHome:         0xff50,
	ebiten.KeyEnd:          0xff57,
	ebiten.KeyPageUp:       0xff55,
	ebiten.KeyPageDown:     0xff56,
	ebiten.KeyArrowLeft:    0xff51,
	ebiten.KeyArrowUp:      0xff52,
	ebiten.KeyArrowRight:   0xff53,
	ebiten.KeyArrowDown:    0xff54,
	ebiten.KeyShiftLeft:    0xffe1,
	ebiten.KeyShiftRight:   0xffe2,
	ebiten.KeyControlLeft:  0xffe3,
	ebiten.KeyControlRight: 0xffe4,
	ebiten.KeyAltLeft:      0xffe9,
	ebiten.KeyAltRight:     0xffea,
	ebiten.KeyMetaLeft:     0xffeb,
	ebiten.KeyMetaRight:    0xffec,
	ebiten.KeyMinus:        0x2d,
	ebiten.KeyEqual:        0x3d,
	ebiten.KeyBracketLeft:  0x5b,
	ebiten.KeyBracketRight: 0x5d,
	ebiten.KeyBackslash:    0x5c,
	ebiten.KeySemicolon:    0x3b,
	ebiten.KeyQuote:        0x27,
	ebiten.KeyComma:        0x2c,
	ebiten.KeyPeriod:       0x2e,
	ebiten.KeySlash:        0x2f,
	ebiten.KeyBackquote:    0x60,
}
