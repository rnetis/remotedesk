package rfbclient

import (
	"encoding/binary"
	"fmt"
	"image"
	"io"
)

// sendSetPixelFormat requests 32-bpp true colour, little-endian, byte order
// B,G,R,x — which makes Raw decoding a direct copy into image.RGBA.
func (c *Client) sendSetPixelFormat() error {
	pf := []byte{
		32, 24, 0, 1, // bpp, depth, big-endian=0, true-colour=1
		0, 255, 0, 255, 0, 255, // red/green/blue max = 255
		16, 8, 0, // red/green/blue shift
		0, 0, 0, // padding
	}
	msg := append([]byte{msgSetPixelFormat, 0, 0, 0}, pf...)
	return c.write(msg)
}

func (c *Client) sendSetEncodings(encodings ...int32) error {
	buf := []byte{msgSetEncodings, 0}
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(encodings)))
	for _, e := range encodings {
		buf = binary.BigEndian.AppendUint32(buf, uint32(e))
	}
	return c.write(buf)
}

// RequestUpdate asks for a framebuffer update covering the whole screen.
func (c *Client) RequestUpdate(incremental bool) error {
	inc := byte(0)
	if incremental {
		inc = 1
	}
	buf := []byte{msgFramebufferUpdateRequest, inc}
	buf = binary.BigEndian.AppendUint16(buf, 0)
	buf = binary.BigEndian.AppendUint16(buf, 0)
	buf = binary.BigEndian.AppendUint16(buf, uint16(c.Width))
	buf = binary.BigEndian.AppendUint16(buf, uint16(c.Height))
	return c.write(buf)
}

// SendPointer reports the pointer position and button bitmask.
func (c *Client) SendPointer(x, y int, buttons uint8) error {
	buf := []byte{msgPointerEvent, buttons}
	buf = binary.BigEndian.AppendUint16(buf, uint16(clamp(x, c.Width)))
	buf = binary.BigEndian.AppendUint16(buf, uint16(clamp(y, c.Height)))
	return c.write(buf)
}

// SendKey reports a key press/release for an X11 keysym.
func (c *Client) SendKey(keysym uint32, down bool) error {
	d := byte(0)
	if down {
		d = 1
	}
	buf := []byte{msgKeyEvent, d, 0, 0}
	buf = binary.BigEndian.AppendUint32(buf, keysym)
	return c.write(buf)
}

func (c *Client) write(b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.w.Write(b); err != nil {
		return err
	}
	return c.w.Flush()
}

// Run reads server messages and decodes framebuffer updates until the
// connection closes or an error occurs.
func (c *Client) Run() error {
	for {
		var typ [1]byte
		if _, err := io.ReadFull(c.r, typ[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		switch typ[0] {
		case sMsgFramebufferUpdate:
			if err := c.readFramebufferUpdate(); err != nil {
				return err
			}
			if c.OnUpdate != nil {
				c.OnUpdate()
			}
		case sMsgSetColourMapEntries:
			if err := c.skipColourMap(); err != nil {
				return err
			}
		case sMsgBell:
			// no-op
		case sMsgServerCutText:
			var head [7]byte
			if _, err := io.ReadFull(c.r, head[:]); err != nil {
				return err
			}
			length := binary.BigEndian.Uint32(head[3:7])
			if _, err := io.CopyN(io.Discard, c.r, int64(length)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown server message type %d", typ[0])
		}
	}
}

func (c *Client) readFramebufferUpdate() error {
	var head [3]byte // padding(1) + num-rectangles(2)
	if _, err := io.ReadFull(c.r, head[:]); err != nil {
		return err
	}
	nrects := binary.BigEndian.Uint16(head[1:3])
	for i := 0; i < int(nrects); i++ {
		if err := c.readRectangle(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) readRectangle() error {
	var hdr [12]byte // x,y,w,h(2 each) + encoding(4)
	if _, err := io.ReadFull(c.r, hdr[:]); err != nil {
		return err
	}
	x := int(binary.BigEndian.Uint16(hdr[0:2]))
	y := int(binary.BigEndian.Uint16(hdr[2:4]))
	w := int(binary.BigEndian.Uint16(hdr[4:6]))
	h := int(binary.BigEndian.Uint16(hdr[6:8]))
	enc := int32(binary.BigEndian.Uint32(hdr[8:12]))

	switch enc {
	case encodingRaw:
		return c.decodeRaw(x, y, w, h)
	case encodingCopyRect:
		return c.decodeCopyRect(x, y, w, h)
	default:
		return fmt.Errorf("unsupported encoding %d", enc)
	}
}

// decodeRaw reads w*h pixels (B,G,R,x) into the framebuffer at (x,y).
func (c *Client) decodeRaw(x, y, w, h int) error {
	row := make([]byte, w*4)
	c.fbMu.Lock()
	defer c.fbMu.Unlock()
	for j := 0; j < h; j++ {
		if _, err := io.ReadFull(c.r, row); err != nil {
			return err
		}
		off := c.fb.PixOffset(x, y+j)
		for i := 0; i < w; i++ {
			si := i * 4
			di := off + i*4
			if di+3 >= len(c.fb.Pix) {
				continue
			}
			c.fb.Pix[di] = row[si+2]   // R
			c.fb.Pix[di+1] = row[si+1] // G
			c.fb.Pix[di+2] = row[si]   // B
			c.fb.Pix[di+3] = 255       // A
		}
	}
	return nil
}

// decodeCopyRect copies an existing framebuffer region to (x,y).
func (c *Client) decodeCopyRect(x, y, w, h int) error {
	var src [4]byte
	if _, err := io.ReadFull(c.r, src[:]); err != nil {
		return err
	}
	sx := int(binary.BigEndian.Uint16(src[0:2]))
	sy := int(binary.BigEndian.Uint16(src[2:4]))
	c.fbMu.Lock()
	defer c.fbMu.Unlock()
	// Snapshot the source region first so overlapping copies stay correct.
	region := image.NewRGBA(image.Rect(0, 0, w, h))
	for j := 0; j < h; j++ {
		copy(region.Pix[j*w*4:(j+1)*w*4], c.fb.Pix[c.fb.PixOffset(sx, sy+j):c.fb.PixOffset(sx+w, sy+j)])
	}
	for j := 0; j < h; j++ {
		copy(c.fb.Pix[c.fb.PixOffset(x, y+j):c.fb.PixOffset(x+w, y+j)], region.Pix[j*w*4:(j+1)*w*4])
	}
	return nil
}

func (c *Client) skipColourMap() error {
	var head [5]byte // padding(1) + first-colour(2) + count(2)
	if _, err := io.ReadFull(c.r, head[:]); err != nil {
		return err
	}
	count := binary.BigEndian.Uint16(head[3:5])
	_, err := io.CopyN(io.Discard, c.r, int64(count)*6)
	return err
}

// Close terminates the session.
func (c *Client) Close() error { return c.conn.Close() }

func clamp(v, max int) int {
	if v < 0 {
		return 0
	}
	if v >= max {
		return max - 1
	}
	return v
}
