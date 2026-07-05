package rfbclient_test

import (
	"image"
	"image/color"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"remotedesk/internal/rfb"
	"remotedesk/internal/rfbclient"
)

type fakeScreen struct{ img *image.RGBA }

func (f *fakeScreen) Bounds() image.Rectangle       { return f.img.Bounds() }
func (f *fakeScreen) Capture() (*image.RGBA, error) { return f.img, nil }

type nopSink struct{}

func (nopSink) KeyEvent(uint32, bool)        {}
func (nopSink) PointerEvent(int, int, uint8) {}

// TestClientDecodesServerFramebuffer wires our RFB client to our RFB server and
// checks that a gradient framebuffer decodes back pixel-for-pixel.
func TestClientDecodesServerFramebuffer(t *testing.T) {
	const w, h = 16, 8
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.SetRGBA(x, y, colorAt(x, y))
		}
	}

	srvConn, cliConn := net.Pipe()
	go rfb.Serve(srvConn, rfb.Options{
		Screen: &fakeScreen{src}, Sink: nopSink{},
		Password: "secret", Logger: log.New(io.Discard, "", 0),
	})

	done := make(chan error, 1)
	var client *rfbclient.Client
	go func() {
		c, err := rfbclient.Connect(cliConn, "secret")
		if err != nil {
			done <- err
			return
		}
		client = c
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connect timed out")
	}

	if client.Width != w || client.Height != h {
		t.Fatalf("size %dx%d, want %dx%d", client.Width, client.Height, w, h)
	}

	updated := make(chan struct{}, 1)
	client.OnUpdate = func() {
		select {
		case updated <- struct{}{}:
		default:
		}
	}
	go client.Run()
	if err := client.RequestUpdate(false); err != nil {
		t.Fatalf("request: %v", err)
	}

	select {
	case <-updated:
	case <-time.After(5 * time.Second):
		t.Fatal("no framebuffer update received")
	}

	got := client.Image()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			want := colorAt(x, y)
			c := got.RGBAAt(x, y)
			if c.R != want.R || c.G != want.G || c.B != want.B {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, c, want)
			}
		}
	}
}

// colorAt is a deterministic gradient so every pixel is distinct enough to
// catch row/column or byte-order mistakes.
func colorAt(x, y int) color.RGBA {
	return color.RGBA{R: uint8(x * 16), G: uint8(y * 32), B: uint8(x*8 + y*4), A: 255}
}

// swapScreen returns the first image once, then the second on every later
// capture — modelling a screen that changed between frames.
type swapScreen struct {
	mu    sync.Mutex
	a, b  *image.RGBA
	calls int
}

func (s *swapScreen) Bounds() image.Rectangle { return s.a.Bounds() }
func (s *swapScreen) Capture() (*image.RGBA, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls == 1 {
		return s.a, nil
	}
	return s.b, nil
}

// TestIncrementalUpdateReconstructs proves the dirty-region path: after a full
// frame, an incremental update carries only the changed tiles, yet the client
// reconstructs the new frame exactly.
func TestIncrementalUpdateReconstructs(t *testing.T) {
	const w, h = 96, 64
	a := image.NewRGBA(image.Rect(0, 0, w, h))
	b := image.NewRGBA(image.Rect(0, 0, w, h))
	// Fill A solid; B is A with a small changed block near one corner.
	for i := range a.Pix {
		a.Pix[i] = 200
		b.Pix[i] = 200
	}
	for y := 4; y < 12; y++ {
		for x := 4; x < 12; x++ {
			b.SetRGBA(x, y, color.RGBA{10, 20, 30, 255})
		}
	}

	srvConn, cliConn := net.Pipe()
	go rfb.Serve(srvConn, rfb.Options{
		Screen: &swapScreen{a: a, b: b}, Sink: nopSink{},
		Logger: log.New(io.Discard, "", 0),
	})

	client, err := rfbclient.Connect(cliConn, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	updates := make(chan struct{}, 4)
	client.OnUpdate = func() { updates <- struct{}{} }
	go client.Run()

	// Full frame (frame A).
	client.RequestUpdate(false)
	waitUpdate(t, updates)
	// Incremental (frame B — only the changed block travels).
	client.RequestUpdate(true)
	waitUpdate(t, updates)

	got := client.Image()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			want := b.RGBAAt(x, y)
			c := got.RGBAAt(x, y)
			if c.R != want.R || c.G != want.G || c.B != want.B {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, c, want)
			}
		}
	}
}

func waitUpdate(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for framebuffer update")
	}
}
