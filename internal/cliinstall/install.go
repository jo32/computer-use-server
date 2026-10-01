// Package cliinstall registers the CLI bundled with the macOS desktop app.
package cliinstall

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Status struct {
	State string `json:"state"`
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"`
}

type Options struct {
	Executable, Home, Shell, ZDotDir, ConfigHome string
}

func HelperPath(executable string) (string, bool) {
	contents := filepath.Dir(filepath.Dir(executable))
	if filepath.Base(filepath.Dir(executable)) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(filepath.Dir(contents)) != ".app" {
		return "", false
	}
	return filepath.Join(contents, "Helpers", "readyrig"), true
}

// Install never needs administrator privileges and does not replace a separately
// installed command. Managed links follow app upgrades and are repaired on moves.
func Install(o Options) Status {
	helper, bundled := HelperPath(o.Executable)
	if !bundled {
		return Status{}
	}
	s := Status{Path: filepath.Join(o.Home, ".local", "bin", "readyrig")}
	if strings.Contains(filepath.ToSlash(helper), "/AppTranslocation/") {
		s.State = "relocate"
		return s
	}
	info, err := os.Lstat(helper)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		s.State, s.Error = "error", "App 包中缺少可执行的 CLI，请重新安装完整的 ReadyRig App。"
		return s
	}
	if err := registerLink(helper, s.Path); err != nil {
		if errors.Is(err, errExistingCLI) {
			s.State = "existing"
		} else {
			s.State, s.Error = "error", err.Error()
			return s
		}
	} else {
		s.State = "installed"
	}
	for _, profile := range shellProfiles(o) {
		if err := configurePath(profile, filepath.Base(o.Shell) == "fish"); err != nil {
			s.State, s.Error = "error", fmt.Sprintf("无法配置终端命令路径：%v", err)
			return s
		}
	}
	return s
}

var errExistingCLI = errors.New("an independently installed CLI already exists")

func managedLink(target string) bool {
	contents := filepath.Dir(filepath.Dir(target))
	if filepath.Base(target) != "readyrig" || filepath.Base(filepath.Dir(target)) != "Helpers" || filepath.Base(contents) != "Contents" {
		return false
	}
	switch filepath.Base(filepath.Dir(contents)) {
	case "ReadyRig.app", "Readrig.app", "Relay.app":
		return true
	}
	return false
}

func registerLink(helper, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		// Symlink creation is exclusive: another installer cannot be overwritten.
		return os.Symlink(helper, path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		if info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return errExistingCLI
		}
		return fmt.Errorf("CLI 安装路径已被占用：%s", path)
	}
	target, err := os.Readlink(path)
	if err != nil {
		return err
	}
	if target == helper {
		return nil
	}
	if !managedLink(target) {
		return errExistingCLI
	}
	staging, err := os.MkdirTemp(filepath.Dir(path), ".readyrig-link-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	temporary := filepath.Join(staging, "readyrig")
	if err := os.Symlink(helper, temporary); err != nil {
		return err
	}
	// Preserve a command changed independently while preparing the new link.
	if current, err := os.Readlink(path); err != nil || current != target {
		return errExistingCLI
	}
	return os.Rename(temporary, path)
}

func shellProfiles(o Options) []string {
	switch filepath.Base(o.Shell) {
	case "", ".", "zsh":
		dir := o.ZDotDir
		if dir == "" {
			dir = o.Home
		}
		return []string{filepath.Join(dir, ".zprofile"), filepath.Join(dir, ".zshrc")}
	case "bash":
		login := filepath.Join(o.Home, ".bash_profile")
		for _, name := range []string{".bash_profile", ".bash_login", ".profile"} {
			candidate := filepath.Join(o.Home, name)
			if _, err := os.Stat(candidate); err == nil {
				login = candidate
				break
			}
		}
		return []string{login, filepath.Join(o.Home, ".bashrc")}
	case "fish":
		dir := o.ConfigHome
		if dir == "" {
			dir = filepath.Join(o.Home, ".config")
		}
		return []string{filepath.Join(dir, "fish", "conf.d", "readyrig.fish")}
	default:
		return []string{filepath.Join(o.Home, ".profile")}
	}
}

const pathMarker = "# >>> ReadyRig CLI >>>"
const pathEnd = "# <<< ReadyRig CLI <<<"
const shPath = `# >>> ReadyRig CLI >>>
case ":$PATH:" in
  *":$HOME/.local/bin:"*) ;;
  *) export PATH="$HOME/.local/bin:$PATH" ;;
esac
# <<< ReadyRig CLI <<<
`
const fishPath = `# >>> ReadyRig CLI >>>
if not contains -- "$HOME/.local/bin" $PATH
  set -gx PATH "$HOME/.local/bin" $PATH
end
# <<< ReadyRig CLI <<<
`

func configurePath(path string, fish bool) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if bytes.Contains(existing, []byte(pathMarker)) {
		if !bytes.Contains(existing, []byte(pathEnd)) {
			return fmt.Errorf("unfinished ReadyRig PATH block in %s", path)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	block := shPath
	if fish {
		block = fishPath
	}
	if len(existing) != 0 && existing[len(existing)-1] != '\n' {
		block = "\n" + block
	}
	if _, err := f.WriteString("\n" + block); err != nil {
		return err
	}
	return f.Sync()
}
