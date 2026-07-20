package main

import (
	"sync"

	"remotedesk/internal/rfbclient"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// remoteView renders the remote framebuffer and forwards local pointer and
// keyboard input to the host over an rfbclient.Client. It is a focusable Fyne
// widget so it can receive raw key-down/up events (including modifiers).
type remoteView struct {
	widget.BaseWidget

	client *rfbclient.Client
	img    *canvas.Image
	w, h   int

	mu      sync.Mutex
	buttons uint8 // current RFB button bitmask
	x, y    int   // last framebuffer pointer position
}

// Compile-time checks that we satisfy the input interfaces we rely on.
var (
	_ desktop.Hoverable = (*remoteView)(nil)
	_ desktop.Mouseable = (*remoteView)(nil)
	_ desktop.Keyable   = (*remoteView)(nil)
	_ fyne.Scrollable   = (*remoteView)(nil)
)

func newRemoteView(c *rfbclient.Client) *remoteView {
	img := canvas.NewImageFromImage(c.Image())
	img.FillMode = canvas.ImageFillStretch
	img.ScaleMode = canvas.ImageScalePixels

	rv := &remoteView{client: c, img: img, w: c.Width, h: c.Height}
	rv.ExtendBaseWidget(rv)

	// Each decoded frame updates the canvas (on the UI thread) and requests the
	// next one, so updates flow as fast as they can be rendered.
	c.OnUpdate = func() {
		fyne.Do(func() {
			rv.img.Image = c.Image()
			canvas.Refresh(rv.img)
		})
		_ = c.RequestUpdate(true)
	}
	_ = c.RequestUpdate(false)
	return rv
}

func (rv *remoteView) CreateRenderer() fyne.WidgetRenderer {
	return &remoteViewRenderer{rv: rv}
}

// fbCoords maps a widget-relative position to framebuffer pixel coordinates,
// scaling proportionally so it stays correct whether the view is shown at
// native size or stretched.
func (rv *remoteView) fbCoords(pos fyne.Position) (int, int) {
	sz := rv.Size()
	if sz.Width <= 0 || sz.Height <= 0 {
		return 0, 0
	}
	x := int(pos.X / sz.Width * float32(rv.w))
	y := int(pos.Y / sz.Height * float32(rv.h))
	return clamp(x, rv.w), clamp(y, rv.h)
}

func clamp(v, max int) int {
	if v < 0 {
		return 0
	}
	if v >= max {
		return max - 1
	}
	return v
}

func buttonBit(b desktop.MouseButton) uint8 {
	switch b {
	case desktop.MouseButtonPrimary:
		return 1 << 0
	case desktop.MouseButtonTertiary:
		return 1 << 1
	case desktop.MouseButtonSecondary:
		return 1 << 2
	}
	return 0
}

func (rv *remoteView) sendPointer() {
	rv.mu.Lock()
	x, y, buttons := rv.x, rv.y, rv.buttons
	rv.mu.Unlock()
	_ = rv.client.SendPointer(x, y, buttons)
}

// --- desktop.Hoverable ---

func (rv *remoteView) MouseIn(e *desktop.MouseEvent) { rv.MouseMoved(e) }

func (rv *remoteView) MouseMoved(e *desktop.MouseEvent) {
	x, y := rv.fbCoords(e.Position)
	rv.mu.Lock()
	rv.x, rv.y = x, y
	rv.mu.Unlock()
	rv.sendPointer()
}

func (rv *remoteView) MouseOut() {}

// --- desktop.Mouseable ---

func (rv *remoteView) MouseDown(e *desktop.MouseEvent) {
	x, y := rv.fbCoords(e.Position)
	rv.mu.Lock()
	rv.x, rv.y = x, y
	rv.buttons |= buttonBit(e.Button)
	rv.mu.Unlock()
	rv.sendPointer()
}

func (rv *remoteView) MouseUp(e *desktop.MouseEvent) {
	x, y := rv.fbCoords(e.Position)
	rv.mu.Lock()
	rv.x, rv.y = x, y
	rv.buttons &^= buttonBit(e.Button)
	rv.mu.Unlock()
	rv.sendPointer()
}

// --- fyne.Scrollable ---

func (rv *remoteView) Scrolled(e *fyne.ScrollEvent) {
	if e.Scrolled.DY == 0 {
		return
	}
	rv.mu.Lock()
	x, y, base := rv.x, rv.y, rv.buttons
	rv.mu.Unlock()
	bit := uint8(1 << 3) // wheel up
	if e.Scrolled.DY < 0 {
		bit = 1 << 4 // wheel down
	}
	// A wheel notch is a momentary press/release.
	_ = rv.client.SendPointer(x, y, base|bit)
	_ = rv.client.SendPointer(x, y, base)
}

// --- desktop.Keyable (+ fyne.Focusable) ---

func (rv *remoteView) KeyDown(e *fyne.KeyEvent) {
	if sym := keysym(e.Name); sym != 0 {
		_ = rv.client.SendKey(sym, true)
	}
}

func (rv *remoteView) KeyUp(e *fyne.KeyEvent) {
	if sym := keysym(e.Name); sym != 0 {
		_ = rv.client.SendKey(sym, false)
	}
}

func (rv *remoteView) FocusGained()            {}
func (rv *remoteView) FocusLost()              {}
func (rv *remoteView) TypedRune(rune)          {}
func (rv *remoteView) TypedKey(*fyne.KeyEvent) {}

// remoteViewRenderer draws the framebuffer image, sized to the remote's native
// resolution but stretched to whatever space the layout allots.
type remoteViewRenderer struct{ rv *remoteView }

func (r *remoteViewRenderer) Destroy() {}

func (r *remoteViewRenderer) Layout(s fyne.Size) { r.rv.img.Resize(s) }

func (r *remoteViewRenderer) MinSize() fyne.Size {
	return fyne.NewSize(float32(r.rv.w), float32(r.rv.h))
}

func (r *remoteViewRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.rv.img}
}

func (r *remoteViewRenderer) Refresh() { canvas.Refresh(r.rv.img) }
