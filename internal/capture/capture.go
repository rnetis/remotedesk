// Package capture provides screen capture as an rfb.Screen, backed by
// kbinani/screenshot (X11 on Linux, GDI on Windows).
package capture

import (
	"fmt"
	"image"

	"github.com/kbinani/screenshot"
)

// Display captures a single physical display.
type Display struct {
	index  int
	bounds image.Rectangle // absolute bounds in the virtual screen
}

// Primary returns the primary display (index 0).
func Primary() (*Display, error) {
	if screenshot.NumActiveDisplays() == 0 {
		return nil, fmt.Errorf("no active displays found")
	}
	b := screenshot.GetDisplayBounds(0)
	return &Display{index: 0, bounds: b}, nil
}

// Bounds returns the display size, normalized to the origin (what the RFB
// framebuffer reports).
func (d *Display) Bounds() image.Rectangle {
	return image.Rect(0, 0, d.bounds.Dx(), d.bounds.Dy())
}

// Capture grabs the current contents of the display.
func (d *Display) Capture() (*image.RGBA, error) {
	img, err := screenshot.CaptureRect(d.bounds)
	if err != nil {
		return nil, err
	}
	return img, nil
}
