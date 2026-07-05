package rfb

import (
	"bytes"
	"image"
)

// tileSize is the granularity of change detection. Dirty tiles in the same tile
// row are coalesced horizontally into a single rectangle.
const tileSize = 32

// dirtyRects returns framebuffer-relative rectangles covering the regions that
// differ between prev and cur. If the two images differ in size, the whole
// framebuffer is returned as a single rectangle.
func dirtyRects(prev, cur *image.RGBA) []image.Rectangle {
	pb, cb := prev.Bounds(), cur.Bounds()
	w, h := cb.Dx(), cb.Dy()
	if pb.Dx() != w || pb.Dy() != h {
		return []image.Rectangle{image.Rect(0, 0, w, h)}
	}

	var rects []image.Rectangle
	for ty := 0; ty < h; ty += tileSize {
		th := min(tileSize, h-ty)
		runStart := -1
		flush := func(endX int) {
			if runStart >= 0 {
				rects = append(rects, image.Rect(runStart, ty, endX, ty+th))
				runStart = -1
			}
		}
		for tx := 0; tx < w; tx += tileSize {
			tw := min(tileSize, w-tx)
			if tileDiffers(prev, cur, tx, ty, tw, th) {
				if runStart < 0 {
					runStart = tx
				}
			} else {
				flush(tx)
			}
		}
		flush(w)
	}
	return rects
}

// tileDiffers reports whether the framebuffer-relative region (x,y,w,h) differs
// between the two images, comparing raw RGBA scanlines.
func tileDiffers(prev, cur *image.RGBA, x, y, w, h int) bool {
	pb, cb := prev.Bounds(), cur.Bounds()
	for j := 0; j < h; j++ {
		po := prev.PixOffset(pb.Min.X+x, pb.Min.Y+y+j)
		co := cur.PixOffset(cb.Min.X+x, cb.Min.Y+y+j)
		if !bytes.Equal(prev.Pix[po:po+w*4], cur.Pix[co:co+w*4]) {
			return true
		}
	}
	return false
}
