package computer

import (
	"bytes"
	"computer-use-server/internal/harness"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/image/draw"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Permissions struct {
	Supported     bool   `json:"supported"`
	Screen        bool   `json:"screen"`
	Accessibility bool   `json:"accessibility"`
	Platform      string `json:"platform"`
}
type Bounds struct{ Width, Height float64 }
type Action struct {
	Action       string    `json:"action"`
	Coordinate   []float64 `json:"coordinate,omitempty"`
	To           []float64 `json:"to,omitempty"`
	Text         string    `json:"text,omitempty"`
	Keys         []string  `json:"keys,omitempty"`
	Scroll       []int     `json:"scroll_delta,omitempty"`
	CaptureAfter *bool     `json:"capture_after,omitempty"`
	FrameID      string    `json:"frame_id,omitempty"`
}
type Driver interface {
	Permissions() Permissions
	Capture(context.Context) ([]byte, Bounds, error)
	Input(context.Context, Action) error
}
type frame struct {
	Width, Height int
	Bounds        Bounds
	At            time.Time
	Session       string
}
type Computer struct {
	driver Driver
	dir    string
	mu     sync.Mutex
	frames map[string]frame
}

func New(dir string) *Computer {
	return &Computer{driver: nativeDriver{}, dir: dir, frames: map[string]frame{}}
}
func (c *Computer) Permissions() Permissions { return c.driver.Permissions() }
func (c *Computer) Register(r *harness.Registry) {
	r.Register(harness.Tool{Spec: harness.Spec{Name: "computer_screenshot", Category: "computer", Description: "Capture the primary display as a JPEG, scaled to at most 1280 pixels. Returns a frame_id and image dimensions. Use coordinates from this frame in computer_action. Requires macOS Screen Recording permission.", InputSchema: harness.Schema(map[string]any{})}, Run: func(ctx context.Context, in harness.Invocation) (harness.Output, error) { return c.capture(ctx, in) }})
	r.Register(harness.Tool{Spec: harness.Spec{Name: "computer_action", Category: "computer", Mutating: true, Description: "Act on the primary display. Actions: mouse_move, left_click, right_click, middle_click, double_click, drag, scroll, type, key. Coordinate actions require a frame_id from a screenshot in this session within 5 minutes. Coordinates are image pixels, mapped to macOS display points. Defaults to capture_after=true. Mouse in a display corner stops input. Actions execute sequentially.", InputSchema: harness.Schema(map[string]any{"action": harness.Prop("string", "Action name"), "coordinate": map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "minItems": 2, "maxItems": 2, "description": "[x,y] in screenshot pixels"}, "to": map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "minItems": 2, "maxItems": 2}, "frame_id": harness.Prop("string", "Screenshot frame ID"), "text": harness.Prop("string", "Unicode text for type"), "keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "scroll_delta": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "minItems": 2, "maxItems": 2, "description": "[horizontal,vertical] wheel pixels"}, "capture_after": harness.Prop("boolean", "Capture result after settling, default true")}, "action")}, Run: c.action})
}
func (c *Computer) capture(ctx context.Context, in harness.Invocation) (harness.Output, error) {
	raw, bounds, err := c.driver.Capture(ctx)
	if err != nil {
		return harness.Output{}, err
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return harness.Output{}, err
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if w == 0 || h == 0 {
		return harness.Output{}, errors.New("empty capture")
	}
	if max(w, h) > 1280 {
		scale := 1280 / float64(max(w, h))
		w = max(1, int(float64(w)*scale))
		h = max(1, int(float64(h)*scale))
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var buf bytes.Buffer
	if err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return harness.Output{}, err
	}
	if err = ctx.Err(); err != nil {
		return harness.Output{}, err
	}
	id := harness.ID()
	filename := id + ".jpg"
	if err = os.WriteFile(filepath.Join(c.dir, filename), buf.Bytes(), 0600); err != nil {
		return harness.Output{}, err
	}
	c.mu.Lock()
	for key, f := range c.frames {
		if time.Since(f.At) > 5*time.Minute {
			delete(c.frames, key)
		}
	}
	c.frames[id] = frame{w, h, bounds, time.Now(), in.Session}
	c.mu.Unlock()
	return harness.Output{Screenshot: filename, Value: map[string]any{"status": "success", "frame_id": id, "image_size": []int{w, h}, "screen_size": []float64{bounds.Width, bounds.Height}, "screenshot": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())}}, nil
}
func (c *Computer) action(ctx context.Context, in harness.Invocation) (harness.Output, error) {
	var a Action
	if err := json.Unmarshal(in.Arguments, &a); err != nil {
		return harness.Output{}, err
	}
	switch a.Action {
	case "mouse_move", "left_click", "right_click", "middle_click", "double_click", "drag":
		c.mu.Lock()
		f, ok := c.frames[a.FrameID]
		c.mu.Unlock()
		if !ok || f.Session != in.Session || time.Since(f.At) > 5*time.Minute {
			return harness.Output{}, errors.New("take a fresh screenshot in this session before coordinate actions")
		}
		convert := func(point []float64) ([]float64, error) {
			if len(point) != 2 || point[0] < 0 || point[1] < 0 || point[0] >= float64(f.Width) || point[1] >= float64(f.Height) {
				return nil, errors.New("coordinate outside screenshot")
			}
			return []float64{point[0] * f.Bounds.Width / float64(f.Width), point[1] * f.Bounds.Height / float64(f.Height)}, nil
		}
		var err error
		a.Coordinate, err = convert(a.Coordinate)
		if err != nil {
			return harness.Output{}, err
		}
		if a.Action == "drag" {
			a.To, err = convert(a.To)
			if err != nil {
				return harness.Output{}, err
			}
		}
	case "type":
		if a.Text == "" || len(a.Text) > 10000 {
			return harness.Output{}, errors.New("type requires 1–10000 bytes of text")
		}
	case "key":
		if len(a.Keys) < 1 || len(a.Keys) > 5 {
			return harness.Output{}, errors.New("key requires 1–5 keys")
		}
	case "scroll":
		if len(a.Scroll) != 2 || a.Scroll[0] < -10000 || a.Scroll[0] > 10000 || a.Scroll[1] < -10000 || a.Scroll[1] > 10000 {
			return harness.Output{}, errors.New("scroll_delta must contain two values between -10000 and 10000")
		}
	default:
		return harness.Output{}, fmt.Errorf("unknown action %q", a.Action)
	}
	if err := c.driver.Input(ctx, a); err != nil {
		return harness.Output{}, err
	}
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return harness.Output{}, ctx.Err()
	}
	if a.CaptureAfter == nil || *a.CaptureAfter {
		return c.capture(ctx, in)
	}
	return harness.Output{Value: map[string]any{"status": "success", "action": a.Action}}, nil
}
