//go:build darwin && cgo

package localopen

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#include <stdlib.h>

static char *readyrigApplications(const char *path) {
 @autoreleasepool {
  NSWorkspace *workspace = NSWorkspace.sharedWorkspace;
  NSURL *target = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
  NSURL *defaultApp = [workspace URLForApplicationToOpenURL:target];
  NSArray<NSURL *> *urls = [workspace URLsForApplicationsToOpenURL:target];
  NSMutableArray *result = [NSMutableArray array];
  for (NSURL *url in urls) {
   NSURL *canonical = url.URLByResolvingSymlinksInPath;
   NSString *name = nil;
   [url getResourceValue:&name forKey:NSURLLocalizedNameKey error:nil];
   if (name.length == 0) name = url.lastPathComponent;
   if ([name.pathExtension.lowercaseString isEqualToString:@"app"]) name = name.stringByDeletingPathExtension;
   NSMutableDictionary *item = [NSMutableDictionary dictionaryWithDictionary:@{
    @"id": canonical.path, @"name": name,
    @"default": @([canonical isEqual:defaultApp.URLByResolvingSymlinksInPath])
   }];
   // Render menu-sized PNGs instead of returning full-size application icons.
   NSImage *icon = [workspace iconForFile:url.path];
   NSBitmapImageRep *bitmap = [[NSBitmapImageRep alloc] initWithBitmapDataPlanes:NULL
    pixelsWide:32 pixelsHigh:32 bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES
    isPlanar:NO colorSpaceName:NSDeviceRGBColorSpace bytesPerRow:0 bitsPerPixel:0];
   if (bitmap) {
    [NSGraphicsContext saveGraphicsState];
    NSGraphicsContext.currentContext = [NSGraphicsContext graphicsContextWithBitmapImageRep:bitmap];
    [icon drawInRect:NSMakeRect(0, 0, 32, 32) fromRect:NSZeroRect operation:NSCompositingOperationCopy fraction:1.0];
    [NSGraphicsContext restoreGraphicsState];
    NSData *png = [bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
    if (png) item[@"icon"] = [@"data:image/png;base64," stringByAppendingString:[png base64EncodedStringWithOptions:0]];
    [bitmap release];
   }
   [result addObject:item];
  }
  NSData *json = [NSJSONSerialization dataWithJSONObject:result options:0 error:nil];
  if (!json) return NULL;
  NSString *text = [[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding];
  char *copy = strdup(text.UTF8String);
  [text release];
  return copy;
 }
}

static char *readyrigOpenPath(const char *path, const char *application) {
 @autoreleasepool {
  NSURL *target = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
  NSWorkspace *workspace = NSWorkspace.sharedWorkspace;
  NSWorkspaceOpenConfiguration *config = [NSWorkspaceOpenConfiguration configuration];
  dispatch_semaphore_t done = dispatch_semaphore_create(0);
  __block char *failure = NULL;
  void (^completed)(NSRunningApplication *, NSError *) = ^(NSRunningApplication *app, NSError *error) {
   if (error) failure = strdup(error.localizedDescription.UTF8String);
   dispatch_semaphore_signal(done);
  };
  if (application[0]) {
   NSURL *app = [NSURL fileURLWithPath:[NSString stringWithUTF8String:application]];
   [workspace openURLs:@[target] withApplicationAtURL:app configuration:config completionHandler:completed];
  } else {
   [workspace openURL:target configuration:config completionHandler:completed];
  }
  // NSWorkspace completes on a concurrent queue, independently of the webview.
  dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
  dispatch_release(done);
  return failure;
 }
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"unsafe"
)

func Supported() Capabilities { return Capabilities{Applications: true} }

func applications(ctx context.Context, path string) ([]Application, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	result := C.readyrigApplications(p)
	if result == nil {
		return nil, fmt.Errorf("无法读取系统打开方式")
	}
	defer C.free(unsafe.Pointer(result))
	var apps []Application
	if err := json.Unmarshal([]byte(C.GoString(result)), &apps); err != nil {
		return nil, err
	}
	return apps, nil
}

func open(ctx context.Context, path, application string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, a := C.CString(path), C.CString(application)
	defer C.free(unsafe.Pointer(p))
	defer C.free(unsafe.Pointer(a))
	failure := C.readyrigOpenPath(p, a)
	if failure != nil {
		defer C.free(unsafe.Pointer(failure))
		return fmt.Errorf("无法打开文件或文件夹：%s", C.GoString(failure))
	}
	return nil
}
