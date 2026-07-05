package rfb

import (
	"image"
	"image/color"
	"testing"
)

func TestDirtyRectsNoChange(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 100, 100))
	b := image.NewRGBA(image.Rect(0, 0, 100, 100))
	if rects := dirtyRects(a, b); len(rects) != 0 {
		t.Fatalf("identical images should have no dirty rects, got %v", rects)
	}
}

func TestDirtyRectsSizeChange(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 100, 100))
	b := image.NewRGBA(image.Rect(0, 0, 120, 100))
	rects := dirtyRects(a, b)
	if len(rects) != 1 || rects[0] != image.Rect(0, 0, 120, 100) {
		t.Fatalf("size change should yield one full rect, got %v", rects)
	}
}

func TestDirtyRectsLocalChange(t *testing.T) {
	const w, h = 128, 96
	a := image.NewRGBA(image.Rect(0, 0, w, h))
	b := image.NewRGBA(image.Rect(0, 0, w, h))
	// Change a single pixel at (40,40): its tile (32..64, 32..64) must be dirty
	// and nothing else.
	b.SetRGBA(40, 40, color.RGBA{1, 2, 3, 255})

	rects := dirtyRects(a, b)
	if len(rects) != 1 {
		t.Fatalf("expected 1 dirty rect, got %d: %v", len(rects), rects)
	}
	got := rects[0]
	if !pointIn(got, 40, 40) {
		t.Fatalf("dirty rect %v does not contain the changed pixel", got)
	}
	// It must not cover the whole frame — that's the whole point.
	if got.Dx()*got.Dy() >= w*h {
		t.Fatalf("dirty rect %v covers the whole frame; diffing gained nothing", got)
	}
	// A far-away pixel's tile must be clean.
	if pointIn(got, 100, 90) {
		t.Fatalf("dirty rect %v wrongly includes an unchanged region", got)
	}
}

func TestDirtyRectsHorizontalCoalesce(t *testing.T) {
	const w, h = 200, 40
	a := image.NewRGBA(image.Rect(0, 0, w, h))
	b := image.NewRGBA(image.Rect(0, 0, w, h))
	// Change pixels across three adjacent tiles in the same tile row; they
	// should coalesce into a single wide rectangle.
	for x := 10; x < 180; x += 5 {
		b.SetRGBA(x, 10, color.RGBA{9, 9, 9, 255})
	}
	rects := dirtyRects(a, b)
	if len(rects) != 1 {
		t.Fatalf("adjacent dirty tiles should coalesce to 1 rect, got %d: %v", len(rects), rects)
	}
}

func pointIn(r image.Rectangle, x, y int) bool {
	return image.Pt(x, y).In(r)
}
