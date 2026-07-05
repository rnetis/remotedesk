// Package rfb implements a minimal RFB 3.8 (VNC) server. It is transport- and
// platform-agnostic: callers supply a Screen (framebuffer source) and a Sink
// (input destination), and Serve speaks RFB over any io.ReadWriteCloser — in
// remotedesk that stream is the SSH-tunneled connection from the relay.
package rfb

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"log"
	"time"
)

// Screen is a source of framebuffer images. Bounds must be stable for the life
// of a session (resolution changes are a later milestone).
type Screen interface {
	Bounds() image.Rectangle
	Capture() (*image.RGBA, error)
}

// Sink receives decoded input events from the viewer.
type Sink interface {
	// KeyEvent reports a key press (down=true) or release for an X11 keysym.
	KeyEvent(keysym uint32, down bool)
	// PointerEvent reports the pointer at (x,y) with a button bitmask
	// (bit0=left, bit1=middle, bit2=right, bit3/4=wheel up/down).
	PointerEvent(x, y int, buttons uint8)
}

// Options configures a Serve call.
type Options struct {
	Screen   Screen
	Sink     Sink
	Password string // if non-empty, VNC auth is required with this password
	Name     string // desktop name shown in the viewer
	Logger   *log.Logger

	// MaxFPS caps how often framebuffer updates are sent in response to
	// incremental requests. 0 means a sensible default.
	MaxFPS int
}

// RFB message types (client-to-server).
const (
	msgSetPixelFormat           = 0
	msgSetEncodings             = 2
	msgFramebufferUpdateRequest = 3
	msgKeyEvent                 = 4
	msgPointerEvent             = 5
	msgClientCutText            = 6
)

const (
	secTypeNone    = 1
	secTypeVNCAuth = 2
	encodingRaw    = 0
)

// Serve runs one RFB session on conn until the client disconnects or an error
// occurs. It closes conn before returning.
func Serve(conn io.ReadWriteCloser, opts Options) error {
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	if opts.Name == "" {
		opts.Name = "remotedesk"
	}
	if opts.MaxFPS <= 0 {
		opts.MaxFPS = 20
	}
	s := &session{
		opts: opts,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
		c:    conn,
		pf:   defaultPixelFormat(),
	}
	defer conn.Close()
	if err := s.handshake(); err != nil {
		return fmt.Errorf("rfb handshake: %w", err)
	}
	return s.loop()
}

type session struct {
	opts     Options
	r        *bufio.Reader
	w        *bufio.Writer
	c        io.Closer
	pf       pixelFormat
	minFrame time.Duration

	prev *image.RGBA // last frame sent, for dirty-region diffing
	buf  []byte      // reusable pixel-encoding scratch
}

func (s *session) handshake() error {
	// ProtocolVersion.
	if _, err := s.w.WriteString("RFB 003.008\n"); err != nil {
		return err
	}
	if err := s.w.Flush(); err != nil {
		return err
	}
	var ver [12]byte
	if _, err := io.ReadFull(s.r, ver[:]); err != nil {
		return err
	}

	// Security handshake (3.8).
	if s.opts.Password != "" {
		if err := s.securityVNCAuth(); err != nil {
			return err
		}
	} else {
		if err := s.securityNone(); err != nil {
			return err
		}
	}

	// ClientInit (shared-flag byte) — ignored; we always allow sharing.
	if _, err := io.ReadFull(s.r, make([]byte, 1)); err != nil {
		return err
	}

	// ServerInit.
	b := s.opts.Screen.Bounds()
	width, height := uint16(b.Dx()), uint16(b.Dy())
	if err := binWrite(s.w, width, height); err != nil {
		return err
	}
	if err := s.pf.write(s.w); err != nil {
		return err
	}
	name := []byte(s.opts.Name)
	if err := binWrite(s.w, uint32(len(name))); err != nil {
		return err
	}
	if _, err := s.w.Write(name); err != nil {
		return err
	}
	return s.w.Flush()
}

func (s *session) securityNone() error {
	if _, err := s.w.Write([]byte{1, secTypeNone}); err != nil {
		return err
	}
	if err := s.w.Flush(); err != nil {
		return err
	}
	var sel [1]byte
	if _, err := io.ReadFull(s.r, sel[:]); err != nil {
		return err
	}
	return s.sendSecurityResult(true)
}

func (s *session) securityVNCAuth() error {
	if _, err := s.w.Write([]byte{1, secTypeVNCAuth}); err != nil {
		return err
	}
	if err := s.w.Flush(); err != nil {
		return err
	}
	var sel [1]byte
	if _, err := io.ReadFull(s.r, sel[:]); err != nil {
		return err
	}
	if sel[0] != secTypeVNCAuth {
		return fmt.Errorf("client selected unsupported security type %d", sel[0])
	}

	challenge := make([]byte, 16)
	if _, err := rand.Read(challenge); err != nil {
		return err
	}
	if _, err := s.w.Write(challenge); err != nil {
		return err
	}
	if err := s.w.Flush(); err != nil {
		return err
	}
	response := make([]byte, 16)
	if _, err := io.ReadFull(s.r, response); err != nil {
		return err
	}
	if !checkVNCAuth(challenge, response, s.opts.Password) {
		s.sendSecurityResult(false)
		return fmt.Errorf("vnc auth failed")
	}
	return s.sendSecurityResult(true)
}

func (s *session) sendSecurityResult(ok bool) error {
	var code uint32 = 1
	if ok {
		code = 0
	}
	if err := binWrite(s.w, code); err != nil {
		return err
	}
	if !ok {
		reason := []byte("authentication failed")
		binWrite(s.w, uint32(len(reason)))
		s.w.Write(reason)
	}
	return s.w.Flush()
}

func (s *session) loop() error {
	s.minFrame = time.Second / time.Duration(s.opts.MaxFPS)
	var lastFrame time.Time
	for {
		var typ [1]byte
		if _, err := io.ReadFull(s.r, typ[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		switch typ[0] {
		case msgSetPixelFormat:
			if err := s.readSetPixelFormat(); err != nil {
				return err
			}
		case msgSetEncodings:
			if err := s.readSetEncodings(); err != nil {
				return err
			}
		case msgFramebufferUpdateRequest:
			incremental, err := s.readFBUR()
			if err != nil {
				return err
			}
			// Rate-limit incremental refreshes so a busy client can't spin us.
			if incremental {
				if d := time.Since(lastFrame); d < s.minFrame {
					time.Sleep(s.minFrame - d)
				}
			}
			if err := s.sendFramebufferUpdate(incremental); err != nil {
				return err
			}
			lastFrame = time.Now()
		case msgKeyEvent:
			if err := s.readKeyEvent(); err != nil {
				return err
			}
		case msgPointerEvent:
			if err := s.readPointerEvent(); err != nil {
				return err
			}
		case msgClientCutText:
			if err := s.readClientCutText(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown client message type %d", typ[0])
		}
	}
}

func (s *session) readSetPixelFormat() error {
	if _, err := io.ReadFull(s.r, make([]byte, 3)); err != nil { // padding
		return err
	}
	return s.pf.read(s.r)
}

func (s *session) readSetEncodings() error {
	var head [3]byte // 1 padding + 2 count
	if _, err := io.ReadFull(s.r, head[:]); err != nil {
		return err
	}
	count := binary.BigEndian.Uint16(head[1:3])
	// We only emit Raw; read and discard the client's preference list.
	_, err := io.CopyN(io.Discard, s.r, int64(count)*4)
	return err
}

// readFBUR reads a FramebufferUpdateRequest and returns its incremental flag.
func (s *session) readFBUR() (bool, error) {
	var buf [9]byte // incremental(1) + x,y,w,h (2 each)
	if _, err := io.ReadFull(s.r, buf[:]); err != nil {
		return false, err
	}
	return buf[0] != 0, nil
}

func (s *session) sendFramebufferUpdate(incremental bool) error {
	img, err := s.opts.Screen.Capture()
	if err != nil {
		return fmt.Errorf("capture: %w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	// A full request (or the first frame) sends everything; an incremental
	// request sends only the tiles that changed since the last frame.
	var rects []image.Rectangle
	if !incremental || s.prev == nil {
		rects = []image.Rectangle{image.Rect(0, 0, w, h)}
	} else {
		rects = dirtyRects(s.prev, img)
	}

	// Header: type(0), padding(1), num-rectangles(2).
	if _, err := s.w.Write([]byte{0, 0, byte(len(rects) >> 8), byte(len(rects))}); err != nil {
		return err
	}
	for _, r := range rects {
		if err := binWrite(s.w,
			uint16(r.Min.X), uint16(r.Min.Y), uint16(r.Dx()), uint16(r.Dy()), uint32(encodingRaw),
		); err != nil {
			return err
		}
		s.buf = s.pf.encodeRectInto(s.buf[:0], img, r.Min.X, r.Min.Y, r.Dx(), r.Dy())
		if _, err := s.w.Write(s.buf); err != nil {
			return err
		}
	}
	s.prev = img
	return s.w.Flush()
}

func (s *session) readKeyEvent() error {
	var buf [7]byte // down(1) + padding(2) + keysym(4)
	if _, err := io.ReadFull(s.r, buf[:]); err != nil {
		return err
	}
	down := buf[0] != 0
	keysym := binary.BigEndian.Uint32(buf[3:7])
	s.opts.Sink.KeyEvent(keysym, down)
	return nil
}

func (s *session) readPointerEvent() error {
	var buf [5]byte // button-mask(1) + x(2) + y(2)
	if _, err := io.ReadFull(s.r, buf[:]); err != nil {
		return err
	}
	buttons := buf[0]
	x := int(binary.BigEndian.Uint16(buf[1:3]))
	y := int(binary.BigEndian.Uint16(buf[3:5]))
	s.opts.Sink.PointerEvent(x, y, buttons)
	return nil
}

func (s *session) readClientCutText() error {
	var head [7]byte // padding(3) + length(4)
	if _, err := io.ReadFull(s.r, head[:]); err != nil {
		return err
	}
	length := binary.BigEndian.Uint32(head[3:7])
	_, err := io.CopyN(io.Discard, s.r, int64(length))
	return err
}

// binWrite writes each value in big-endian order (RFB's wire byte order).
func binWrite(w io.Writer, vals ...any) error {
	for _, v := range vals {
		if err := binary.Write(w, binary.BigEndian, v); err != nil {
			return err
		}
	}
	return nil
}
