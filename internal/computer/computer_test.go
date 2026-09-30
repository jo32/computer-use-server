package computer

import (
	"bytes"
	"computer-use-server/internal/harness"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"testing"
	"time"
)

type fakeDriver struct{ last Action }

func (*fakeDriver) Permissions() Permissions {
	return Permissions{Supported: true, Screen: true, Accessibility: true}
}
func (*fakeDriver) Capture(context.Context) ([]byte, Bounds, error) {
	im := image.NewRGBA(image.Rect(0, 0, 2560, 1600))
	var b bytes.Buffer
	png.Encode(&b, im)
	return b.Bytes(), Bounds{1280, 800}, nil
}
func (f *fakeDriver) Input(_ context.Context, a Action) error { f.last = a; return nil }
func TestRetinaCoordinatesAndSession(t *testing.T) {
	d := &fakeDriver{}
	c := &Computer{driver: d, dir: t.TempDir(), frames: map[string]frame{}}
	out, err := c.capture(context.Background(), harness.Invocation{Session: "one"})
	if err != nil {
		t.Fatal(err)
	}
	v := out.Value.(map[string]any)
	if v["image_size"].([]int)[0] != 1280 {
		t.Fatal(v)
	}
	args, _ := json.Marshal(map[string]any{"action": "left_click", "frame_id": v["frame_id"], "coordinate": []int{640, 400}, "capture_after": false})
	in := harness.Invocation{Session: "one", Arguments: args}
	if _, err = c.action(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if d.last.Coordinate[0] != 640 || d.last.Coordinate[1] != 400 {
		t.Fatal("Retina points mapping wrong", d.last)
	}
	in.Session = "other"
	if _, err = c.action(context.Background(), in); err == nil {
		t.Fatal("cross-session frame allowed")
	}
	id := v["frame_id"].(string)
	f := c.frames[id]
	f.At = time.Now().Add(-6 * time.Minute)
	c.frames[id] = f
	in.Session = "one"
	if _, err = c.action(context.Background(), in); err == nil {
		t.Fatal("stale frame allowed")
	}
}
func TestInvalidActions(t *testing.T) {
	c := &Computer{driver: &fakeDriver{}, dir: t.TempDir(), frames: map[string]frame{}}
	for _, args := range []string{`{"action":"left_click","coordinate":[-1,20]}`, `{"action":"scroll","scroll_delta":[1]}`, `{"action":"key","keys":[]}`, `{"action":"type","text":""}`, `{"action":"unknown"}`} {
		if _, err := c.action(context.Background(), harness.Invocation{Arguments: []byte(args)}); err == nil {
			t.Fatal("invalid input accepted", args)
		}
	}
}
