// Export the full-color app artwork at every macOS iconset size.
package main

import (
	"computer-use-server/internal/brand"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	dir := "dist/readyrig.iconset"
	must(os.MkdirAll(dir, 0755))
	for _, n := range []int{16, 32, 128, 256, 512} {
		for _, retina := range []bool{false, true} {
			size, suffix := n, ""
			if retina {
				size *= 2
				suffix = "@2x"
			}
			must(os.WriteFile(filepath.Join(dir, fmt.Sprintf("icon_%dx%d%s.png", n, n, suffix)), brand.AppIcon(size), 0644))
		}
	}
	must(os.WriteFile("dist/readyrig-icon.png", brand.AppIcon(1024), 0644))
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
