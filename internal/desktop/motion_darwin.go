//go:build darwin && !nogui

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
static bool readyrigReduceMotion(void) {
 return [[NSWorkspace sharedWorkspace] accessibilityDisplayShouldReduceMotion];
}
*/
import "C"

func reduceMotion() bool { return bool(C.readyrigReduceMotion()) }
