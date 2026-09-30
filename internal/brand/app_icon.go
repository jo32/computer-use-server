package brand

import (
	"bytes"
	_ "embed"
	"image"
	"image/png"

	"golang.org/x/image/draw"
)

//go:embed assets/readyrig-app-icon.png
var appIconSource []byte

// AppIcon renders the full-color Dock artwork at the requested native size.
// The monochrome Icon renderer remains the menu bar's animated template.
func AppIcon(size int) []byte {
	source, err := png.Decode(bytes.NewReader(appIconSource))
	if err != nil {
		panic(err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), source, source.Bounds(), draw.Src, nil)
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		panic(err)
	}
	return out.Bytes()
}
