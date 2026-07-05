package main

import (
	"sync/atomic"

	"remotedesk/internal/rfbclient"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// game renders the remote framebuffer and forwards local input to the host.
type game struct {
	client *rfbclient.Client
	frame  *ebiten.Image
	dirty  atomic.Bool
}

func newGame(c *rfbclient.Client) *game {
	g := &game{
		client: c,
		frame:  ebiten.NewImage(c.Width, c.Height),
	}
	// Pull loop: each decoded update triggers a request for the next one, so
	// updates flow as fast as they can be rendered.
	c.OnUpdate = func() {
		g.dirty.Store(true)
		c.RequestUpdate(true)
	}
	c.RequestUpdate(false)
	return g
}

func (g *game) Update() error {
	// Pointer: position + button bitmask.
	x, y := ebiten.CursorPosition()
	var buttons uint8
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
		buttons |= 1 << 0
	}
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonMiddle) {
		buttons |= 1 << 1
	}
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonRight) {
		buttons |= 1 << 2
	}
	g.client.SendPointer(x, y, buttons)

	// Wheel: emit a momentary press/release for each notch.
	if _, wy := ebiten.Wheel(); wy != 0 {
		bit := uint8(1 << 3) // wheel up
		if wy < 0 {
			bit = 1 << 4 // wheel down
		}
		g.client.SendPointer(x, y, buttons|bit)
		g.client.SendPointer(x, y, buttons)
	}

	// Keyboard: translate just-pressed / just-released keys to keysyms.
	var keys []ebiten.Key
	for _, k := range inpututil.AppendJustPressedKeys(keys[:0]) {
		if sym := keysym(k); sym != 0 {
			g.client.SendKey(sym, true)
		}
	}
	for _, k := range inpututil.AppendJustReleasedKeys(keys[:0]) {
		if sym := keysym(k); sym != 0 {
			g.client.SendKey(sym, false)
		}
	}
	return nil
}

func (g *game) Draw(screen *ebiten.Image) {
	if g.dirty.Swap(false) {
		g.frame.WritePixels(g.client.Image().Pix)
	}
	screen.DrawImage(g.frame, nil)
}

func (g *game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return g.client.Width, g.client.Height
}
