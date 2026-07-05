package rfb

import (
	"encoding/binary"
	"image"
	"io"
)

// pixelFormat mirrors the RFB PIXEL_FORMAT structure. The server advertises a
// default in ServerInit; the client may override it with SetPixelFormat, and we
// must then encode framebuffer pixels in exactly that format.
type pixelFormat struct {
	bitsPerPixel uint8
	depth        uint8
	bigEndian    uint8
	trueColor    uint8
	redMax       uint16
	greenMax     uint16
	blueMax      uint16
	redShift     uint8
	greenShift   uint8
	blueShift    uint8
	// 3 bytes padding on the wire
}

// defaultPixelFormat is 32-bpp true colour (8 bits each R/G/B), little-endian.
func defaultPixelFormat() pixelFormat {
	return pixelFormat{
		bitsPerPixel: 32, depth: 24, bigEndian: 0, trueColor: 1,
		redMax: 255, greenMax: 255, blueMax: 255,
		redShift: 16, greenShift: 8, blueShift: 0,
	}
}

func (pf pixelFormat) write(w io.Writer) error {
	buf := []byte{
		pf.bitsPerPixel, pf.depth, pf.bigEndian, pf.trueColor,
		byte(pf.redMax >> 8), byte(pf.redMax),
		byte(pf.greenMax >> 8), byte(pf.greenMax),
		byte(pf.blueMax >> 8), byte(pf.blueMax),
		pf.redShift, pf.greenShift, pf.blueShift,
		0, 0, 0, // padding
	}
	_, err := w.Write(buf)
	return err
}

func (pf *pixelFormat) read(r io.Reader) error {
	var buf [16]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	pf.bitsPerPixel = buf[0]
	pf.depth = buf[1]
	pf.bigEndian = buf[2]
	pf.trueColor = buf[3]
	pf.redMax = binary.BigEndian.Uint16(buf[4:6])
	pf.greenMax = binary.BigEndian.Uint16(buf[6:8])
	pf.blueMax = binary.BigEndian.Uint16(buf[8:10])
	pf.redShift = buf[10]
	pf.greenShift = buf[11]
	pf.blueShift = buf[12]
	return nil
}

func (pf pixelFormat) bytesPerPixel() int { return int(pf.bitsPerPixel) / 8 }

// encodeInto appends img's whole framebuffer to dst in this pixel format.
func (pf pixelFormat) encodeInto(dst []byte, img *image.RGBA) []byte {
	b := img.Bounds()
	return pf.encodeRectInto(dst, img, 0, 0, b.Dx(), b.Dy())
}

// encodeRectInto appends the framebuffer-relative rectangle (rx,ry,rw,rh) of img
// to dst in this pixel format. Only true-colour formats are produced; the source
// is always 8-bit RGBA.
func (pf pixelFormat) encodeRectInto(dst []byte, img *image.RGBA, rx, ry, rw, rh int) []byte {
	bpp := pf.bytesPerPixel()
	b := img.Bounds()
	for fy := ry; fy < ry+rh; fy++ {
		row := img.PixOffset(b.Min.X+rx, b.Min.Y+fy)
		for fx := rx; fx < rx+rw; fx++ {
			r := img.Pix[row]
			g := img.Pix[row+1]
			bl := img.Pix[row+2]
			row += 4

			cr := uint32(r) * uint32(pf.redMax) / 255
			cg := uint32(g) * uint32(pf.greenMax) / 255
			cb := uint32(bl) * uint32(pf.blueMax) / 255
			val := cr<<pf.redShift | cg<<pf.greenShift | cb<<pf.blueShift

			var pix [4]byte
			if pf.bigEndian != 0 {
				for i := 0; i < bpp; i++ {
					pix[i] = byte(val >> (8 * uint(bpp-1-i)))
				}
			} else {
				for i := 0; i < bpp; i++ {
					pix[i] = byte(val >> (8 * uint(i)))
				}
			}
			dst = append(dst, pix[:bpp]...)
		}
	}
	return dst
}
