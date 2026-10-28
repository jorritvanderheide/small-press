package dither

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// A photo comes out in the six greys, scaled to fit, its shape kept.
func TestPhoto(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	for y := range 500 {
		for x := range 1000 {
			src.Set(x, y, color.RGBA{uint8(x / 4), 100, 200, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, src)
	p, err := Photo(buf.Bytes(), 800)
	if err != nil {
		t.Fatal(err)
	}
	if p.Rect != image.Rect(0, 0, 800, 400) {
		t.Errorf("size %v, want 800×400", p.Rect)
	}
	if len(p.Palette) != 6 {
		t.Errorf("%d colours", len(p.Palette))
	}
	// Smaller ones keep their size.
	if p, _ := Photo(buf.Bytes(), 2000); p.Rect.Dx() != 1000 {
		t.Errorf("a small photo grew or shrank: %v", p.Rect)
	}
	// Read back from its PNG, it's the same.
	back, err := png.Decode(bytes.NewReader(PNG(p)))
	if err != nil || back.Bounds() != p.Rect {
		t.Errorf("its PNG: %v", err)
	}
}

// An even grey becomes a pattern of the shades around it, not one flat
// shade.
func TestPattern(t *testing.T) {
	g := image.NewGray(image.Rect(0, 0, 8, 8))
	for i := range g.Pix {
		g.Pix[i] = 100
	}
	p := pattern(g)
	seen := map[uint8]int{}
	for _, i := range p.Pix {
		seen[i]++
	}
	if len(seen) != 2 || seen[2] == 0 || seen[3] == 0 { // 125 and 175
		t.Errorf("grey 100 became %v", seen)
	}
}

// A phone photo taken upright, stored on its side with an EXIF note, comes
// out upright; a photo without a note as it is.
func TestOrientation(t *testing.T) {
	// 40×20, the left half black: turned a quarter clockwise, the top half.
	src := image.NewGray(image.Rect(0, 0, 40, 20))
	for y := range 20 {
		for x := range 40 {
			if x >= 20 {
				src.Pix[y*src.Stride+x] = 255
			}
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, src, &jpeg.Options{Quality: 100})
	plain := buf.Bytes()
	if o := orientation(plain); o != 0 {
		t.Errorf("a JPEG without EXIF: orientation %d", o)
	}
	for _, order := range []binary.AppendByteOrder{binary.LittleEndian, binary.BigEndian} {
		turned := withOrientation(plain, order, 6)
		if o := orientation(turned); o != 6 {
			t.Fatalf("orientation %d, want 6", o)
		}
		p, err := Photo(turned, 800)
		if err != nil {
			t.Fatal(err)
		}
		if p.Rect != image.Rect(0, 0, 20, 40) {
			t.Fatalf("size %v, want 20×40", p.Rect)
		}
		if top, bottom := p.Pix[5*p.Stride+10], p.Pix[35*p.Stride+10]; top > 1 || bottom < 5 {
			t.Errorf("not turned: top shade %d, bottom shade %d", top, bottom)
		}
	}
}

// Anything but a JPEG or PNG is refused.
func TestNotAPhoto(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("GIF89a"), []byte("<svg></svg>"), {0xFF, 0xD8, 0xFF}} {
		if _, err := Photo(data, 800); !errors.Is(err, ErrFormat) {
			t.Errorf("%q: %v", data, err)
		}
	}
}

// withOrientation puts an EXIF segment with orientation o after a JPEG's
// start, as a camera does.
func withOrientation(jpg []byte, order binary.AppendByteOrder, o uint16) []byte {
	var tiff []byte
	if order == binary.LittleEndian {
		tiff = []byte("II")
	} else {
		tiff = []byte("MM")
	}
	tiff = order.AppendUint16(tiff, 42)
	tiff = order.AppendUint32(tiff, 8)      // the first IFD
	tiff = order.AppendUint16(tiff, 1)      // one entry
	tiff = order.AppendUint16(tiff, 0x0112) // orientation
	tiff = order.AppendUint16(tiff, 3)      // a short
	tiff = order.AppendUint32(tiff, 1)
	tiff = order.AppendUint16(tiff, o)
	tiff = append(tiff, 0, 0)
	tiff = order.AppendUint32(tiff, 0) // no next IFD
	seg := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(seg)+2))
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}
