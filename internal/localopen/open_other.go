//go:build !darwin && !windows

package localopen

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func Supported() Capabilities { return Capabilities{} }

func applications(context.Context, string) ([]Application, error) { return nil, ErrUnsupported }

func open(ctx context.Context, path, _ string) error {
	output, err := exec.CommandContext(ctx, "xdg-open", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("无法打开文件或文件夹：%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
