package rfb

import (
	"encoding/binary"
	"image"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"
)

type fakeScreen struct{ img *image.RGBA }

func (f *fakeScreen) Bounds() image.Rectangle       { return f.img.Bounds() }
func (f *fakeScreen) Capture() (*image.RGBA, error) { return f.img, nil }

type fakeSink struct {
	mu      sync.Mutex
	keys    []uint32
	pointer []int // x,y,buttons flattened
}

func (f *fakeSink) KeyEvent(keysym uint32, down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if down {
		f.keys = append(f.keys, keysym)
	}
}
func (f *fakeSink) PointerEvent(x, y int, buttons uint8) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pointer = append(f.pointer, x, y, int(buttons))
}

func TestRFBSession(t *testing.T) {
	// 4x2 framebuffer; pixel (0,0) has a distinctive colour.
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3] = 10, 20, 30, 255

	sink := &fakeSink{}
	srv, cli := net.Pipe()
	go func() {
		Serve(srv, Options{
			Screen:   &fakeScreen{img},
			Sink:     sink,
			Password: "123456",
			Name:     "test",
			Logger:   log.New(io.Discard, "", 0),
		})
	}()
	cli.SetDeadline(time.Now().Add(5 * time.Second))

	// ProtocolVersion.
	buf := make([]byte, 12)
	mustRead(t, cli, buf)
	if string(buf) != "RFB 003.008\n" {
		t.Fatalf("bad version %q", buf)
	}
	mustWrite(t, cli, []byte("RFB 003.008\n"))

	// Security: expect [1, VNCAuth].
	sec := make([]byte, 2)
	mustRead(t, cli, sec)
	if sec[0] != 1 || sec[1] != secTypeVNCAuth {
		t.Fatalf("bad security list %v", sec)
	}
	mustWrite(t, cli, []byte{secTypeVNCAuth})

	// Challenge/response.
	challenge := make([]byte, 16)
	mustRead(t, cli, challenge)
	mustWrite(t, cli, vncAuthResponse(challenge, "123456"))

	var result uint32
	if err := binary.Read(cli, binary.BigEndian, &result); err != nil || result != 0 {
		t.Fatalf("security result=%d err=%v", result, err)
	}

	// ClientInit -> ServerInit.
	mustWrite(t, cli, []byte{1})
	var w, h uint16
	binary.Read(cli, binary.BigEndian, &w)
	binary.Read(cli, binary.BigEndian, &h)
	if w != 4 || h != 2 {
		t.Fatalf("server init size %dx%d", w, h)
	}
	mustRead(t, cli, make([]byte, 16)) // pixel format
	var nameLen uint32
	binary.Read(cli, binary.BigEndian, &nameLen)
	mustRead(t, cli, make([]byte, nameLen))

	// Set the default pixel format explicitly so the encoding is predictable.
	pfBytes := make([]byte, 16)
	defaultPixelFormat().write(bytesWriter{pfBytes})
	mustWrite(t, cli, append([]byte{msgSetPixelFormat, 0, 0, 0}, pfBytes...))

	// Request a full framebuffer update.
	fbur := []byte{msgFramebufferUpdateRequest, 0}
	fbur = append(fbur, be16(0)...) // x
	fbur = append(fbur, be16(0)...) // y
	fbur = append(fbur, be16(4)...) // w
	fbur = append(fbur, be16(2)...) // h
	mustWrite(t, cli, fbur)

	// Read FramebufferUpdate header.
	hdr := make([]byte, 4)
	mustRead(t, cli, hdr)
	if hdr[0] != 0 {
		t.Fatalf("expected FramebufferUpdate, got msg %d", hdr[0])
	}
	numRects := binary.BigEndian.Uint16(hdr[2:4])
	if numRects != 1 {
		t.Fatalf("expected 1 rect, got %d", numRects)
	}
	rect := make([]byte, 12) // x,y,w,h(2 each) + encoding(4)
	mustRead(t, cli, rect)
	rw := binary.BigEndian.Uint16(rect[4:6])
	rh := binary.BigEndian.Uint16(rect[6:8])
	enc := binary.BigEndian.Uint32(rect[8:12])
	if enc != encodingRaw {
		t.Fatalf("expected Raw encoding, got %d", enc)
	}
	pixels := make([]byte, int(rw)*int(rh)*4)
	mustRead(t, cli, pixels)
	// Default format: little-endian 32bpp, so pixel (0,0) = B,G,R,0.
	if pixels[0] != 30 || pixels[1] != 20 || pixels[2] != 10 {
		t.Fatalf("pixel (0,0) = %v, want B=30 G=20 R=10", pixels[0:4])
	}

	// Send input events.
	key := []byte{msgKeyEvent, 1, 0, 0}
	key = append(key, be32(0x0041)...) // keysym 'A'
	mustWrite(t, cli, key)
	ptr := []byte{msgPointerEvent, 0x01} // left button
	ptr = append(ptr, be16(2)...)        // x
	ptr = append(ptr, be16(1)...)        // y
	mustWrite(t, cli, ptr)

	// Round-trip another update to guarantee the server processed the input
	// (the message loop is sequential).
	mustWrite(t, cli, fbur)
	mustRead(t, cli, hdr)
	mustRead(t, cli, rect)
	mustRead(t, cli, pixels)

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.keys) != 1 || sink.keys[0] != 0x0041 {
		t.Fatalf("keys = %v, want [0x41]", sink.keys)
	}
	if len(sink.pointer) != 3 || sink.pointer[0] != 2 || sink.pointer[1] != 1 || sink.pointer[2] != 1 {
		t.Fatalf("pointer = %v, want [2 1 1]", sink.pointer)
	}
}

// helpers

func mustRead(t *testing.T, r io.Reader, b []byte) {
	t.Helper()
	if _, err := io.ReadFull(r, b); err != nil {
		t.Fatalf("read: %v", err)
	}
}
func mustWrite(t *testing.T, w io.Writer, b []byte) {
	t.Helper()
	if _, err := w.Write(b); err != nil {
		t.Fatalf("write: %v", err)
	}
}
func be16(v uint16) []byte { b := make([]byte, 2); binary.BigEndian.PutUint16(b, v); return b }
func be32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }

// bytesWriter lets pixelFormat.write render into a fixed slice.
type bytesWriter struct{ b []byte }

func (w bytesWriter) Write(p []byte) (int, error) { return copy(w.b, p), nil }
