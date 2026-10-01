package localopen

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"

	"golang.org/x/sys/windows"
)

func Supported() Capabilities { return Capabilities{} }

func applications(context.Context, string) ([]Application, error) { return nil, ErrUnsupported }

func open(ctx context.Context, path, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Shell APIs require COM on the calling thread, including in browser mode.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && !errors.Is(err, syscall.Errno(1)) { // S_FALSE is success too.
		return err
	}
	defer windows.CoUninitialize()
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("open")
	if err := windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("无法打开文件或文件夹：%w", err)
	}
	return nil
}
