package brand

import (
	"bytes"
	"image/png"
	"testing"
)

func TestNativeIcons(t *testing.T) {
	idle := Icon(44, 0, false)
	for frame := 0; frame <= 10; frame++ {
		data := Icon(44, frame, false)
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 44 || img.Bounds().Dy() != 44 {
			t.Fatal("wrong tray dimensions")
		}
		_, _, _, alpha := img.At(0, 0).RGBA()
		if alpha != 0 {
			t.Fatal("tray background must be transparent")
		}
		_, _, _, bodyAlpha := img.At(22, 37).RGBA()
		if bodyAlpha == 0 {
			t.Fatal("computer body was erased while cutting screen")
		}
		if frame != 0 && bytes.Equal(idle, data) {
			t.Fatalf("frame %d must differ from idle", frame)
		}
	}
	for step := 0; step < 100; step++ {
		if AnimationFrame(true, 3, step, false) != 9 {
			t.Fatal("pause must be static")
		}
		if AnimationFrame(false, 3, step, true) != 0 {
			t.Fatal("reduce motion must be static")
		}
	}
	if AnimationFrame(false, 1, 1, false) == AnimationFrame(false, 1, 2, false) {
		t.Fatal("working animation did not advance")
	}
}
