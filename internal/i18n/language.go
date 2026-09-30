// Package i18n keeps the native windows, tray, and dialogs in the selected language.
package i18n

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed en.json
var englishJSON []byte
var english = func() map[string]string {
	var catalog map[string]string
	if err := json.Unmarshal(englishJSON, &catalog); err != nil {
		panic(err)
	}
	return catalog
}()

type Selection struct {
	Preference string `json:"preference"`
	Locale     string `json:"locale"`
}

type Language struct {
	mu        sync.RWMutex
	path      string
	selection Selection
}

func ValidPreference(value string) bool { return value == "auto" || value == "zh-CN" || value == "en" }
func ValidLocale(value string) bool     { return value == "zh-CN" || value == "en" }

func New(path string) *Language {
	l := &Language{path: path, selection: Selection{Preference: "auto", Locale: systemLocale()}}
	if data, err := os.ReadFile(path); err == nil {
		var saved Selection
		if json.Unmarshal(data, &saved) == nil && ValidPreference(saved.Preference) && ValidLocale(saved.Locale) {
			if saved.Preference != "auto" {
				l.selection = Selection{Preference: saved.Preference, Locale: saved.Preference}
			}
		}
	}
	return l
}

func (l *Language) Selection() Selection { l.mu.RLock(); defer l.mu.RUnlock(); return l.selection }

// Set receives the browser's resolved locale for automatic language selection.
func (l *Language) Set(selection Selection) error {
	if !ValidPreference(selection.Preference) || !ValidLocale(selection.Locale) {
		return fmt.Errorf("invalid language selection")
	}
	if selection.Preference != "auto" {
		selection.Locale = selection.Preference
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if selection == l.selection {
		return nil
	}
	data, err := json.Marshal(selection)
	if err != nil {
		return err
	}
	if l.path != "" {
		if err := os.MkdirAll(filepath.Dir(l.path), 0700); err != nil {
			return err
		}
		file, err := os.CreateTemp(filepath.Dir(l.path), ".language-*")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		if _, err = file.Write(data); err != nil {
			file.Close()
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
		if err = os.Rename(file.Name(), l.path); err != nil {
			return err
		}
	}
	l.selection = selection
	return nil
}

func (l *Language) Text(message string, args ...any) string {
	if l.Selection().Locale == "en" {
		if translated, ok := english[message]; ok {
			message = translated
		}
	}
	if len(args) != 0 {
		return fmt.Sprintf(message, args...)
	}
	return message
}

func systemLocale() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		value := strings.ToLower(os.Getenv(key))
		if strings.HasPrefix(value, "zh") {
			return "zh-CN"
		}
		if strings.HasPrefix(value, "en") {
			return "en"
		}
	}
	if runtime.GOOS == "darwin" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if data, err := exec.CommandContext(ctx, "/usr/bin/defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				value := strings.ToLower(strings.Trim(line, " \t\r\",()"))
				if strings.HasPrefix(value, "zh") {
					return "zh-CN"
				}
				if strings.HasPrefix(value, "en") {
					return "en"
				}
			}
		}
	}
	return "en"
}
