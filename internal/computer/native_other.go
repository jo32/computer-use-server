//go:build !darwin || !cgo

package computer

import (
	"context"
	"errors"
	"runtime"
)

type nativeDriver struct{}

func (nativeDriver) Permissions() Permissions { return Permissions{Platform: runtime.GOOS} }
func (nativeDriver) Capture(context.Context) ([]byte, Bounds, error) {
	return nil, Bounds{}, errors.New("native desktop control requires a macOS build with CGO enabled")
}
func (nativeDriver) Input(context.Context, Action) error {
	return errors.New("native desktop control requires a macOS build with CGO enabled")
}
