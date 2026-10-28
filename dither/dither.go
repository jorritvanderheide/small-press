// Package dither gives a photo the site's look: grey, at most size pixels
// wide or high, in six shades with an ordered (Bayer 8×8) pattern, as
// hitherdither makes it of a grey photo. The page puts it on a colour, which
// shows through the light shades.
package dither

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	_ "image/jpeg" // the formats Photo reads
	"image/png"
	"math"
)

// MaxPixels is the largest photo Photo reads: a phone's is about 12 million.
const MaxPixels = 50_000_000

// ErrFormat is returned for a file that isn't a JPEG or PNG photo, or one
// larger than MaxPixels.
var ErrFormat = errors.New("not a JPEG or PNG photo, or too large")

// palette is the six greys.
var palette = color.Palette{
	color.Gray{25}, color.Gray{75}, color.Gray{125}, color.Gray{175}, color.Gray{225}, color.Gray{250},
}

// threshold is how far the pattern pushes a pixel up, at most.
const threshold = 96

// Photo reads a JPEG or PNG photo, turns it upright as the camera noted
// (EXIF), makes it fit in size×size pixels and dithers it.
func Photo(data []byte, size int) (*image.Paletted, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width*cfg.Height > MaxPixels {
		return nil, ErrFormat
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrFormat
	}
	g := orient(shrink(img, size), orientation(data))
	return pattern(g), nil
}

// PNG is p as a PNG file.
func PNG(p *image.Paletted) []byte {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	enc.Encode(&buf, p) // writing to a buffer doesn't fail
	return buf.Bytes()
}

// shrink is img in grey, scaled down to fit in size×size: each pixel the
// average of those it covers.
func shrink(img image.Image, size int) *image.Gray {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	w, h := sw, sh
	if w > size || h > size {
		if w >= h {
			w, h = size, (sh*size+sw/2)/sw
		} else {
			w, h = (sw*size+sh/2)/sh, size
		}
		w, h = max(w, 1), max(h, 1)
	}
	luma := lumaOf(img)
	out := image.NewGray(image.Rect(0, 0, w, h))
	for y := range h {
		y0, y1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		for x := range w {
			x0, x1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
			sum := 0
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					sum += int(luma(b.Min.X+sx, b.Min.Y+sy))
				}
			}
			out.Pix[y*out.Stride+x] = uint8(sum / ((y1 - y0) * (x1 - x0)))
		}
	}
	return out
}

// lumaOf reads a pixel's brightness: from a JPEG's Y plane directly, as
// that's what it stores, or else converted to grey.
func lumaOf(img image.Image) func(x, y int) uint8 {
	switch m := img.(type) {
	case *image.YCbCr:
		return func(x, y int) uint8 { return m.Y[m.YOffset(x, y)] }
	case *image.Gray:
		return func(x, y int) uint8 { return m.Pix[m.PixOffset(x, y)] }
	}
	return func(x, y int) uint8 { return color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y }
}

// pattern dithers g to the palette: each pixel pushed up by the Bayer
// matrix's share of the threshold, then the nearest grey.
func pattern(g *image.Gray) *image.Paletted {
	b := g.Bounds()
	out := image.NewPaletted(b, palette)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			v := float64(g.Pix[g.PixOffset(x, y)]) + float64(1+bayer[y%8][x%8])*threshold/65
			out.Pix[out.PixOffset(x, y)] = nearest(v)
		}
	}
	return out
}

// nearest is the palette's index of the grey nearest to v; the darker of
// two as near.
func nearest(v float64) uint8 {
	best := 0
	for i, c := range palette {
		if math.Abs(v-float64(c.(color.Gray).Y)) < math.Abs(v-float64(palette[best].(color.Gray).Y)) {
			best = i
		}
	}
	return uint8(best)
}

// bayer is the 8×8 ordered dithering matrix, 0 to 63.
var bayer = func() [8][8]int {
	m := [][]int{{0}}
	for len(m) < 8 {
		n := len(m)
		next := make([][]int, 2*n)
		for y := range next {
			next[y] = make([]int, 2*n)
			for x := range next[y] {
				next[y][x] = 4*m[y%n][x%n] + [2][2]int{{0, 2}, {3, 1}}[y/n][x/n]
			}
		}
		m = next
	}
	var out [8][8]int
	for y := range out {
		copy(out[y][:], m[y])
	}
	return out
}()

// orient turns g upright for an EXIF orientation (1 to 8; others leave it).
func orient(g *image.Gray, o int) *image.Gray {
	if o < 2 || o > 8 {
		return g
	}
	w, h := g.Rect.Dx(), g.Rect.Dy()
	ow, oh := w, h
	if o >= 5 {
		ow, oh = h, w
	}
	out := image.NewGray(image.Rect(0, 0, ow, oh))
	for y := range h {
		for x := range w {
			var nx, ny int
			switch o {
			case 2: // mirrored
				nx, ny = w-1-x, y
			case 3: // upside down
				nx, ny = w-1-x, h-1-y
			case 4: // upside down, mirrored
				nx, ny = x, h-1-y
			case 5: // mirrored, a quarter turn
				nx, ny = y, x
			case 6: // a quarter turn clockwise
				nx, ny = h-1-y, x
			case 7: // mirrored, three quarters
				nx, ny = h-1-y, w-1-x
			case 8: // a quarter turn anticlockwise
				nx, ny = y, w-1-x
			}
			out.Pix[ny*out.Stride+nx] = g.Pix[y*g.Stride+x]
		}
	}
	return out
}

// orientation is a JPEG's EXIF orientation, or 0 when it has none.
func orientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0
	}
	for i := 2; i+4 <= len(data) && data[i] == 0xFF; {
		marker, size := data[i+1], int(binary.BigEndian.Uint16(data[i+2:]))
		if marker == 0xDA || i+2+size > len(data) { // the image itself
			return 0
		}
		if seg := data[i+4 : i+2+size]; marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return exifOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 0
}

// exifOrientation reads tag 0x0112 from the first IFD of the TIFF data in
// an EXIF segment.
func exifOrientation(t []byte) int {
	if len(t) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	ifd := int(order.Uint32(t[4:]))
	if ifd+2 > len(t) {
		return 0
	}
	n := int(order.Uint16(t[ifd:]))
	for i := range n {
		e := ifd + 2 + 12*i
		if e+12 > len(t) {
			return 0
		}
		if order.Uint16(t[e:]) == 0x0112 {
			return int(order.Uint16(t[e+8:]))
		}
	}
	return 0
}
