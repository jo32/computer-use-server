//go:build darwin && cgo

package computer

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreGraphics
#include <ApplicationServices/ApplicationServices.h>
#include <CoreGraphics/CoreGraphics.h>
static int screenAllowed(){return CGPreflightScreenCaptureAccess();}
static int inputAllowed(){return AXIsProcessTrusted();}
static double displayW(){return CGDisplayBounds(CGMainDisplayID()).size.width;}
static double displayH(){return CGDisplayBounds(CGMainDisplayID()).size.height;}
static int corner(){CGEventRef e=CGEventCreate(NULL); if(!e)return 0; CGPoint p=CGEventGetLocation(e);CFRelease(e);return (p.x<=2 || p.x>=displayW()-2) && (p.y<=2 || p.y>=displayH()-2);}
static void mouseEvent(int type,double x,double y,int button,int count){CGEventRef e=CGEventCreateMouseEvent(NULL,(CGEventType)type,CGPointMake(x,y),(CGMouseButton)button);CGEventSetIntegerValueField(e,kCGMouseEventClickState,count);CGEventPost(kCGHIDEventTap,e);CFRelease(e);}
static void scrollEvent(int x,int y){CGEventRef e=CGEventCreateScrollWheelEvent(NULL,kCGScrollEventUnitPixel,2,y,x);CGEventPost(kCGHIDEventTap,e);CFRelease(e);}
static void keyEvent(int code,int down,unsigned long long flags){CGEventRef e=CGEventCreateKeyboardEvent(NULL,(CGKeyCode)code,down);CGEventSetFlags(e,(CGEventFlags)flags);CGEventPost(kCGHIDEventTap,e);CFRelease(e);}
static void unicodeEvent(UniChar *chars,int count){CGEventRef e=CGEventCreateKeyboardEvent(NULL,0,true);CGEventKeyboardSetUnicodeString(e,count,chars);CGEventPost(kCGHIDEventTap,e);CGEventSetType(e,kCGEventKeyUp);CGEventPost(kCGHIDEventTap,e);CFRelease(e);}
*/
import "C"
import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"
)

type nativeDriver struct{}

func (nativeDriver) Permissions() Permissions {
	return Permissions{true, C.screenAllowed() != 0, C.inputAllowed() != 0, "macOS"}
}
func (nativeDriver) Capture(ctx context.Context) ([]byte, Bounds, error) {
	if C.screenAllowed() == 0 {
		return nil, Bounds{}, errors.New("请在 macOS 系统设置 → 隐私与安全性 → 屏幕与系统音频录制中允许此程序，然后重新启动")
	}
	f, err := os.CreateTemp("", "adapter-capture-*.png")
	if err != nil {
		return nil, Bounds{}, err
	}
	name := f.Name()
	f.Close()
	defer os.Remove(name)
	cmd := exec.CommandContext(ctx, "/usr/sbin/screencapture", "-x", "-D", "1", "-t", "png", name)
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, Bounds{}, fmt.Errorf("screen capture: %w: %s", err, b)
	}
	b, err := os.ReadFile(name)
	return b, Bounds{float64(C.displayW()), float64(C.displayH())}, err
}
func (nativeDriver) Input(ctx context.Context, a Action) error {
	if C.inputAllowed() == 0 {
		return errors.New("请在 macOS 系统设置 → 隐私与安全性 → 辅助功能中允许此程序")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if C.corner() != 0 {
		return errors.New("紧急制动：鼠标位于屏幕角落，请手动移开后重试")
	}
	move := func(typ int, p []float64, button, count int) {
		C.mouseEvent(C.int(typ), C.double(p[0]), C.double(p[1]), C.int(button), C.int(count))
	}
	switch a.Action {
	case "mouse_move":
		move(5, a.Coordinate, 0, 1)
	case "left_click", "right_click", "middle_click", "double_click":
		down, up, button := 1, 2, 0
		if a.Action == "right_click" {
			down, up, button = 3, 4, 1
		}
		if a.Action == "middle_click" {
			down, up, button = 25, 26, 2
		}
		count := 1
		if a.Action == "double_click" {
			count = 2
		}
		for i := 1; i <= count; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			move(down, a.Coordinate, button, i)
			move(up, a.Coordinate, button, i)
		}
	case "drag":
		move(1, a.Coordinate, 0, 1)
		pos := a.Coordinate
		defer func() { move(2, pos, 0, 1) }()
		for i := 1; i <= 20; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if C.corner() != 0 {
				return errors.New("drag stopped by corner failsafe")
			}
			t := float64(i) / 20
			pos = []float64{a.Coordinate[0] + (a.To[0]-a.Coordinate[0])*t, a.Coordinate[1] + (a.To[1]-a.Coordinate[1])*t}
			move(6, pos, 0, 1)
			time.Sleep(10 * time.Millisecond)
		}
	case "scroll":
		C.scrollEvent(C.int(a.Scroll[0]), C.int(a.Scroll[1]))
	case "type":
		units := utf16.Encode([]rune(a.Text))
		for len(units) > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			n := min(len(units), 20)
			if n < len(units) && units[n-1] >= 0xD800 && units[n-1] <= 0xDBFF {
				n--
			}
			C.unicodeEvent((*C.UniChar)(unsafe.Pointer(&units[0])), C.int(n))
			units = units[n:]
		}
	case "key":
		var flags uint64
		code := -1
		for _, key := range a.Keys {
			k := strings.ToLower(key)
			switch k {
			case "cmd", "command", "meta", "super":
				flags |= 1 << 20
			case "ctrl", "control":
				flags |= 1 << 18
			case "alt", "option":
				flags |= 1 << 19
			case "shift":
				flags |= 1 << 17
			default:
				v, ok := keyCodes[k]
				if !ok {
					return fmt.Errorf("unsupported key: %s", key)
				}
				if code != -1 {
					return errors.New("only one non-modifier key is allowed")
				}
				code = v
			}
		}
		if code < 0 {
			return errors.New("a non-modifier key is required")
		}
		C.keyEvent(C.int(code), 1, C.ulonglong(flags))
		C.keyEvent(C.int(code), 0, 0)
	}
	return nil
}

var keyCodes = map[string]int{"a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9, "b": 11, "q": 12, "w": 13, "e": 14, "r": 15, "y": 16, "t": 17, "1": 18, "2": 19, "3": 20, "4": 21, "6": 22, "5": 23, "=": 24, "9": 25, "7": 26, "-": 27, "8": 28, "0": 29, "]": 30, "o": 31, "u": 32, "[": 33, "i": 34, "p": 35, "enter": 36, "return": 36, "l": 37, "j": 38, "'": 39, "k": 40, ";": 41, "\\": 42, ",": 43, "/": 44, "n": 45, "m": 46, ".": 47, "tab": 48, "space": 49, "`": 50, "backspace": 51, "escape": 53, "esc": 53, "delete": 117, "home": 115, "end": 119, "pageup": 116, "pagedown": 121, "left": 123, "right": 124, "down": 125, "up": 126, "f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97, "f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111}
