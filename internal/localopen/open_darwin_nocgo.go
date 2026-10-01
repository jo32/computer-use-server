//go:build darwin && !cgo

package localopen

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

func Supported() Capabilities { return Capabilities{Applications: true} }

// Portable macOS builds query the same NSWorkspace APIs through JXA.
const applicationsScript = `
ObjC.import('AppKit');
function run(argv) {
 const workspace = $.NSWorkspace.sharedWorkspace;
 const target = $.NSURL.fileURLWithPath($(argv[0]));
 const preferred = workspace.URLForApplicationToOpenURL(target);
 const defaultPath = preferred.isNil() ? '' : ObjC.unwrap(preferred.URLByResolvingSymlinksInPath.path);
 const urls = workspace.URLsForApplicationsToOpenURL(target);
 const result = [];
 for (let i = 0; i < urls.count; i++) {
  const url = urls.objectAtIndex(i);
  const path = ObjC.unwrap(url.URLByResolvingSymlinksInPath.path);
  const name = ObjC.unwrap($.NSFileManager.defaultManager.displayNameAtPath(url.path)).replace(/\.app$/i, '');
  result.push({id:path, name:name, default:path === defaultPath});
 }
 return JSON.stringify(result);
}
`

func applications(ctx context.Context, path string) ([]Application, error) {
	output, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", applicationsScript, path).Output()
	if err != nil {
		return nil, fmt.Errorf("无法读取系统打开方式：%w", err)
	}
	var apps []Application
	if err := json.Unmarshal(output, &apps); err != nil {
		return nil, err
	}
	return apps, nil
}

func open(ctx context.Context, path, application string) error {
	args := []string{path}
	if application != "" {
		args = []string{"-a", application, path}
	}
	output, err := exec.CommandContext(ctx, "/usr/bin/open", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("无法打开文件或文件夹：%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
