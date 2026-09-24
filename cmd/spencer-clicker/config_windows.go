package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const settingsSchemaVersion = 1

var errUnsupportedSettingsVersion = errors.New("unsupported settings version")

// windowIdentity is stable across process launches. HWND and PID intentionally
// stay out of the on-disk format because Windows assigns new values on launch.
type windowIdentity struct {
	ExecutablePath string `json:"exe"`
	WindowClass    string `json:"class"`
	Title          string `json:"title"`
}

type savedPoint struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

type savedPreset struct {
	Name  string     `json:"name"`
	Point savedPoint `json:"point"`
}

type savedWindow struct {
	Identity    windowIdentity `json:"identity"`
	CustomPoint *savedPoint    `json:"point,omitempty"`
	Presets     []savedPreset  `json:"presets,omitempty"`
}

type savedHotkey struct {
	Kind uint8  `json:"kind"`
	Code uint32 `json:"code"`
}

type appSettings struct {
	Version    int             `json:"version"`
	IntervalMS int             `json:"interval_ms"`
	Hold       bool            `json:"hold"`
	Hotkey     savedHotkey     `json:"hotkey"`
	PIPWidth   int             `json:"pip_width"`
	PIPHeight  int             `json:"pip_height"`
	PIPFPS     int             `json:"pip_fps"`
	Selected   *windowIdentity `json:"selected,omitempty"`
	Windows    []savedWindow   `json:"windows,omitempty"`
}

func defaultAppSettings() appSettings {
	return appSettings{
		Version:    settingsSchemaVersion,
		IntervalMS: minIntervalMS,
		Hotkey:     savedHotkey{Kind: uint8(keyboardHotkey), Code: vkF9},
		PIPWidth:   320,
		PIPHeight:  180,
		PIPFPS:     5,
	}
}

func settingsFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(configDir, "SpencerClicker", "settings.json"), nil
}

func loadAppSettings(path string) (appSettings, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultAppSettings(), nil
	}
	if err != nil {
		return appSettings{}, fmt.Errorf("open settings: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return appSettings{}, fmt.Errorf("read settings: %w", err)
	}
	settings := defaultAppSettings()
	if err := json.Unmarshal(data, &settings); err != nil {
		return appSettings{}, fmt.Errorf("decode settings: %w", err)
	}
	if settings.Version > settingsSchemaVersion {
		return appSettings{}, fmt.Errorf("%w: %d", errUnsupportedSettingsVersion, settings.Version)
	}
	if settings.Version != 0 && settings.Version != settingsSchemaVersion {
		return appSettings{}, fmt.Errorf("%w: %d", errUnsupportedSettingsVersion, settings.Version)
	}
	return normalizeAppSettings(settings), nil
}

func saveAppSettings(path string, settings appSettings) error {
	settings = normalizeAppSettings(settings)
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary settings file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return fmt.Errorf("secure temporary settings file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary settings file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync temporary settings file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary settings file: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replace settings file: %w", err)
	}
	return nil
}

func normalizeAppSettings(settings appSettings) appSettings {
	defaults := defaultAppSettings()
	settings.Version = settingsSchemaVersion
	if settings.IntervalMS <= 0 {
		settings.IntervalMS = defaults.IntervalMS
	} else {
		settings.IntervalMS = normalizeInterval(settings.IntervalMS)
	}
	validHotkey := false
	switch settings.Hotkey.Kind {
	case uint8(keyboardHotkey):
		validHotkey = settings.Hotkey.Code > 0 && settings.Hotkey.Code <= 0xff
	case uint8(mouseHotkey):
		validHotkey = settings.Hotkey.Code >= uint32(mouseRight) && settings.Hotkey.Code <= uint32(mouseX2)
	}
	if !validHotkey {
		settings.Hotkey = defaults.Hotkey
	}
	if !validPIPSize(settings.PIPWidth, settings.PIPHeight) {
		settings.PIPWidth, settings.PIPHeight = defaults.PIPWidth, defaults.PIPHeight
	}
	if !validPIPFPS(settings.PIPFPS) {
		settings.PIPFPS = defaults.PIPFPS
	}
	if settings.Selected != nil {
		selected := *settings.Selected
		selected.ExecutablePath = normalizeExecutablePath(selected.ExecutablePath)
		settings.Selected = &selected
	}
	for i := range settings.Windows {
		settings.Windows[i].Identity.ExecutablePath = normalizeExecutablePath(settings.Windows[i].Identity.ExecutablePath)
	}
	return settings
}

func validPIPSize(width, height int) bool {
	for _, size := range pipSizes {
		if size.width == width && size.height == height {
			return true
		}
	}
	return false
}

func validPIPFPS(fps int) bool {
	for _, supported := range pipFrameRates {
		if supported == fps {
			return true
		}
	}
	return false
}

// pruneUnavailableWindows removes app-specific state only when the executable
// is definitively absent. Permission and network errors keep the user's data.
func pruneUnavailableWindows(settings *appSettings) bool {
	if settings == nil {
		return false
	}
	changed := false
	kept := settings.Windows[:0]
	for _, window := range settings.Windows {
		path := window.Identity.ExecutablePath
		if path != "" {
			_, err := os.Stat(path)
			if errors.Is(err, os.ErrNotExist) {
				changed = true
				continue
			}
		}
		kept = append(kept, window)
	}
	settings.Windows = kept
	if settings.Selected != nil && settings.Selected.ExecutablePath != "" {
		if _, err := os.Stat(settings.Selected.ExecutablePath); errors.Is(err, os.ErrNotExist) {
			settings.Selected = nil
			changed = true
		}
	}
	return changed
}

func normalizeExecutablePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return strings.ToLower(filepath.Clean(path))
}

func matchTargetIdentity(identity windowIdentity, targets []targetWindow) (targetWindow, bool) {
	if normalizeExecutablePath(identity.ExecutablePath) == "" || identity.WindowClass == "" || identity.Title == "" {
		return targetWindow{}, false
	}
	var match targetWindow
	matches := 0
	for _, target := range targets {
		candidate := target.identity
		if normalizeExecutablePath(candidate.ExecutablePath) == normalizeExecutablePath(identity.ExecutablePath) &&
			candidate.WindowClass == identity.WindowClass && candidate.Title == identity.Title {
			match = target
			matches++
		}
	}
	return match, matches == 1
}

func validSavedPoint(point savedPoint, width, height int32) bool {
	return width > 0 && height > 0 && point.X >= 0 && point.Y >= 0 && point.X < width && point.Y < height
}
