// Package brand renders ReadyRig's computer mark for the Dock and menu bar.
package brand

import (
	"bytes"
	"golang.org/x/image/vector"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// Icon uses a 32-unit coordinate system shared with the web app's mark.
// Frame 0 is idle, 1–8 type terminal lines, and 9 shows pause bars.
func Icon(size, frame int, tile bool) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	scale, offset := float32(size)/32, float32(0)
	if tile {
		scale = float32(size) / 42
		offset = 5 * scale
	}
	rect := func(x, y, w, h, r float32, c color.Color, op draw.Op) {
		x = x*scale + offset
		y = y*scale + offset
		w *= scale
		h *= scale
		r *= scale
		z := vector.NewRasterizer(size, size)
		k := float32(.55228475)
		z.MoveTo(x+r, y)
		z.LineTo(x+w-r, y)
		z.CubeTo(x+w-r+k*r, y, x+w, y+r-k*r, x+w, y+r)
		z.LineTo(x+w, y+h-r)
		z.CubeTo(x+w, y+h-r+k*r, x+w-r+k*r, y+h, x+w-r, y+h)
		z.LineTo(x+r, y+h)
		z.CubeTo(x+r-k*r, y+h, x, y+h-r+k*r, x, y+h-r)
		z.LineTo(x, y+r)
		z.CubeTo(x, y+r-k*r, x+r-k*r, y, x+r, y)
		z.ClosePath()
		mask := image.NewAlpha(img.Bounds())
		z.Draw(mask, mask.Bounds(), image.NewUniform(color.Alpha{A: 255}), image.Point{})
		if op == draw.Src {
			cr, cg, cb, ca := c.RGBA()
			source := []uint32{cr >> 8, cg >> 8, cb >> 8, ca >> 8}
			for y := 0; y < size; y++ {
				for x := 0; x < size; x++ {
					a := uint32(mask.AlphaAt(x, y).A)
					if a == 0 {
						continue
					}
					i := img.PixOffset(x, y)
					for ch := 0; ch < 4; ch++ {
						img.Pix[i+ch] = uint8((source[ch]*a + uint32(img.Pix[i+ch])*(255-a)) / 255)
					}
				}
			}
		} else {
			draw.DrawMask(img, img.Bounds(), image.NewUniform(c), image.Point{}, mask, image.Point{}, op)
		}
	}
	ink := color.NRGBA{R: 24, G: 27, B: 29, A: 255}
	paper := color.NRGBA{}
	if tile {
		paper = color.NRGBA{R: 242, G: 240, B: 233, A: 255}
		oldScale, oldOffset := scale, offset
		scale = float32(size) / 32
		offset = 0
		rect(1, 1, 30, 30, 7, paper, draw.Over)
		scale, offset = oldScale, oldOffset
	}
	rect(6, 2, 20, 28, 3, ink, draw.Over)
	rect(9, 6, 14, 11, 1.5, paper, draw.Src)
	rect(16, 21, 7, 2, 1, paper, draw.Src)
	rect(9, 21, 2, 2, 1, paper, draw.Src)
	if frame == 9 {
		rect(12, 9, 2, 5, .5, ink, draw.Over)
		rect(18, 9, 2, 5, .5, ink, draw.Over)
	} else {
		length := float32(5)
		if frame > 0 && frame <= 8 {
			length = 2 + float32((frame-1)%4)*2
		}
		rect(11, 9, length, 1.5, .5, ink, draw.Over)
		rect(11, 12, 3, 1.5, .5, ink, draw.Over)
		if frame != 10 && (frame == 0 || frame%2 == 0) {
			rect(17, 12, 3, 1.5, .5, ink, draw.Over)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		panic(err)
	}
	return out.Bytes()
}

// AnimationFrame keeps pause and reduced-motion states static.
func AnimationFrame(paused bool, running, step int, reduced bool) int {
	if paused {
		return 9
	}
	if reduced {
		return 0
	}
	if running > 0 {
		return 1 + step%8
	}
	if step%48 >= 46 {
		return 10
	}
	return 0
}
