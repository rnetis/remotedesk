// Package rfbclient is a minimal RFB 3.8 (VNC) client. It connects over any
// io.ReadWriteCloser (in remotedesk, the SSH-tunneled stream), decodes
// framebuffer updates into an in-memory image, and sends input events back.
package rfbclient

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"sync"
)

// Client is a live RFB session to a host.
type Client struct {
	Width  int
	Height int

	conn    io.ReadWriteCloser
	r       *bufio.Reader
	writeMu sync.Mutex
	w       *bufio.Writer

	fbMu sync.RWMutex
	fb   *image.RGBA

	// OnUpdate, if set, is called after each framebuffer update is decoded.
	OnUpdate func()
}

// RFB message types.
const (
	msgSetPixelFormat           = 0
	msgSetEncodings             = 2
	msgFramebufferUpdateRequest = 3
	msgKeyEvent                 = 4
	msgPointerEvent             = 5

	sMsgFramebufferUpdate   = 0
	sMsgSetColourMapEntries = 1
	sMsgBell                = 2
	sMsgServerCutText       = 3

	encodingRaw      = 0
	encodingCopyRect = 1
)

// Connect performs the RFB handshake and returns a ready Client. Password may be
// empty when the server offers no-auth (as it does over the relay tunnel).
func Connect(conn io.ReadWriteCloser, password string) (*Client, error) {
	c := &Client{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
	}
	if err := c.handshake(password); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) handshake(password string) error {
	// ProtocolVersion.
	ver := make([]byte, 12)
	if _, err := io.ReadFull(c.r, ver); err != nil {
		return fmt.Errorf("read version: %w", err)
	}
	if _, err := c.w.WriteString("RFB 003.008\n"); err != nil {
		return err
	}
	if err := c.w.Flush(); err != nil {
		return err
	}

	// Security: server sends a count then that many type bytes.
	var n [1]byte
	if _, err := io.ReadFull(c.r, n[:]); err != nil {
		return fmt.Errorf("read security count: %w", err)
	}
	if n[0] == 0 {
		// A zero count is followed by a failure reason string.
		return fmt.Errorf("server refused: %s", c.readString())
	}
	types := make([]byte, n[0])
	if _, err := io.ReadFull(c.r, types); err != nil {
		return err
	}
	if err := c.doSecurity(types, password); err != nil {
		return err
	}

	// SecurityResult.
	var result uint32
	if err := binary.Read(c.r, binary.BigEndian, &result); err != nil {
		return err
	}
	if result != 0 {
		return fmt.Errorf("authentication failed: %s", c.readString())
	}

	// ClientInit (request shared session).
	if _, err := c.w.Write([]byte{1}); err != nil {
		return err
	}
	if err := c.w.Flush(); err != nil {
		return err
	}

	// ServerInit.
	var dims [4]byte
	if _, err := io.ReadFull(c.r, dims[:]); err != nil {
		return err
	}
	c.Width = int(binary.BigEndian.Uint16(dims[0:2]))
	c.Height = int(binary.BigEndian.Uint16(dims[2:4]))
	if _, err := io.ReadFull(c.r, make([]byte, 16)); err != nil { // server pixel format
		return err
	}
	name := c.readString()
	_ = name
	c.fb = image.NewRGBA(image.Rect(0, 0, c.Width, c.Height))

	// Ask the server for our preferred format (32bpp, little-endian BGRX) and
	// the encodings we can decode.
	if err := c.sendSetPixelFormat(); err != nil {
		return err
	}
	if err := c.sendSetEncodings(encodingCopyRect, encodingRaw); err != nil {
		return err
	}
	return nil
}

func (c *Client) doSecurity(types []byte, password string) error {
	has := func(t byte) bool {
		for _, x := range types {
			if x == t {
				return true
			}
		}
		return false
	}
	switch {
	case has(1): // None
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		if _, err := c.w.Write([]byte{1}); err != nil {
			return err
		}
		return c.w.Flush()
	case has(2): // VNC auth
		c.writeMu.Lock()
		if _, err := c.w.Write([]byte{2}); err != nil {
			c.writeMu.Unlock()
			return err
		}
		if err := c.w.Flush(); err != nil {
			c.writeMu.Unlock()
			return err
		}
		c.writeMu.Unlock()
		challenge := make([]byte, 16)
		if _, err := io.ReadFull(c.r, challenge); err != nil {
			return err
		}
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		if _, err := c.w.Write(vncAuthResponse(challenge, password)); err != nil {
			return err
		}
		return c.w.Flush()
	default:
		return fmt.Errorf("no supported security type in %v", types)
	}
}

// readString reads a uint32-length-prefixed string (best-effort on error).
func (c *Client) readString() string {
	var l uint32
	if err := binary.Read(c.r, binary.BigEndian, &l); err != nil {
		return ""
	}
	buf := make([]byte, l)
	io.ReadFull(c.r, buf)
	return string(buf)
}

// Image returns a snapshot copy of the current framebuffer, safe to hand to a
// renderer while decoding continues.
func (c *Client) Image() *image.RGBA {
	c.fbMu.RLock()
	defer c.fbMu.RUnlock()
	cp := image.NewRGBA(c.fb.Rect)
	copy(cp.Pix, c.fb.Pix)
	return cp
}
