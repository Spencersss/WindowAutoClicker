package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSettingsJSONRoundTripAndAtomicReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	identity := windowIdentity{ExecutablePath: normalizeExecutablePath(filepath.Join(t.TempDir(), "Game.exe")), WindowClass: "GameWindow", Title: "Game"}
	point := savedPoint{X: 31, Y: 47}
	settings := appSettings{
		Version:    settingsSchemaVersion,
		IntervalMS: 125,
		Hold:       true,
		Hotkey:     savedHotkey{Kind: uint8(mouseHotkey), Code: mouseX2},
		PIPWidth:   640,
		PIPHeight:  360,
		PIPFPS:     30,
		Selected:   &identity,
		Windows: []savedWindow{{
			Identity:    identity,
			CustomPoint: &point,
			Presets:     []savedPreset{{Name: "loot", Point: savedPoint{X: 5, Y: 9}}},
		}},
	}
	if err := saveAppSettings(path, settings); err != nil {
		t.Fatalf("save initial settings: %v", err)
	}

	settings.IntervalMS = 250
	settings.Hold = false
	if err := saveAppSettings(path, settings); err != nil {
		t.Fatalf("atomically replace existing settings: %v", err)
	}
	got, err := loadAppSettings(path)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if got.IntervalMS != 250 || got.Hold {
		t.Fatalf("replaced settings did not load: interval=%d hold=%v", got.IntervalMS, got.Hold)
	}
	if got.Hotkey != settings.Hotkey || got.PIPWidth != 640 || got.PIPHeight != 360 || got.PIPFPS != 30 {
		t.Fatalf("global settings round trip mismatch: %+v", got)
	}
	if got.Selected == nil || *got.Selected != *settings.Selected || len(got.Windows) != 1 || !reflect.DeepEqual(got.Windows[0].CustomPoint, &point) || !reflect.DeepEqual(got.Windows[0].Presets, settings.Windows[0].Presets) {
		t.Fatalf("window settings round trip mismatch: %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("saved JSON is invalid: %v", err)
	}
	if _, ok := decoded["hwnd"]; ok {
		t.Fatal("settings must not persist HWND")
	}
	if _, ok := decoded["pid"]; ok {
		t.Fatal("settings must not persist PID")
	}
	if strings.Contains(string(data), `"hwnd"`) || strings.Contains(string(data), `"pid"`) {
		t.Fatal("settings must not persist native window handles or process IDs")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		t.Fatalf("temporary file was not cleaned up after replace: %v", entries)
	}
}

func TestLoadSettingsDefaultsAndVersionValidation(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	got, err := loadAppSettings(missing)
	if err != nil {
		t.Fatalf("load missing settings: %v", err)
	}
	if want := defaultAppSettings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("missing settings defaults = %+v, want %+v", got, want)
	}

	partial := filepath.Join(t.TempDir(), "partial.json")
	if err := os.WriteFile(partial, []byte(`{"version":1,"hold":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = loadAppSettings(partial)
	if err != nil {
		t.Fatalf("load partial settings: %v", err)
	}
	defaults := defaultAppSettings()
	if !got.Hold || got.IntervalMS != defaults.IntervalMS || got.Hotkey != defaults.Hotkey || got.PIPWidth != defaults.PIPWidth || got.PIPHeight != defaults.PIPHeight || got.PIPFPS != defaults.PIPFPS {
		t.Fatalf("missing fields did not retain defaults: %+v", got)
	}

	future := filepath.Join(t.TempDir(), "future.json")
	if err := os.WriteFile(future, []byte(`{"version":999}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAppSettings(future); !errors.Is(err, errUnsupportedSettingsVersion) {
		t.Fatalf("future schema error = %v, want unsupported version", err)
	}
}

func TestNormalizeAppSettingsUsesDefaultsForInvalidValues(t *testing.T) {
	settings := normalizeAppSettings(appSettings{Version: settingsSchemaVersion, IntervalMS: -1, PIPWidth: 321, PIPHeight: 180, PIPFPS: 7})
	defaults := defaultAppSettings()
	if settings.IntervalMS != defaults.IntervalMS || settings.Hotkey != defaults.Hotkey || settings.PIPWidth != defaults.PIPWidth || settings.PIPHeight != defaults.PIPHeight || settings.PIPFPS != defaults.PIPFPS {
		t.Fatalf("invalid values were not normalized: %+v", settings)
	}
}

func TestPruneUnavailableWindowsKeepsInstalledPrograms(t *testing.T) {
	installed := filepath.Join(t.TempDir(), "installed.exe")
	if err := os.WriteFile(installed, []byte("exe"), 0600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "uninstalled.exe")
	installedID := windowIdentity{ExecutablePath: installed, WindowClass: "Installed", Title: "Installed"}
	missingID := windowIdentity{ExecutablePath: missing, WindowClass: "Missing", Title: "Missing"}
	settings := appSettings{
		Selected: &missingID,
		Windows: []savedWindow{
			{Identity: installedID, Presets: []savedPreset{{Name: "kept", Point: savedPoint{X: 1, Y: 2}}}},
			{Identity: missingID, Presets: []savedPreset{{Name: "removed", Point: savedPoint{X: 3, Y: 4}}}},
		},
	}
	if !pruneUnavailableWindows(&settings) {
		t.Fatal("pruning missing executable should report a change")
	}
	if len(settings.Windows) != 1 || settings.Windows[0].Identity != installedID || len(settings.Windows[0].Presets) != 1 {
		t.Fatalf("installed program presets were not retained: %+v", settings.Windows)
	}
	if settings.Selected != nil {
		t.Fatalf("selection for removed executable was retained: %+v", settings.Selected)
	}
}

func TestMatchTargetIdentityRequiresOneExactMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Target.exe")
	identity := windowIdentity{ExecutablePath: path, WindowClass: "TargetClass", Title: "Target Window"}
	target := targetWindow{hwnd: 10, pid: 20, identity: identity}
	candidatePath := filepath.ToSlash(path)
	target.identity.ExecutablePath = candidatePath
	want := target.identity
	want.ExecutablePath = normalizeExecutablePath(path)
	if got, ok := matchTargetIdentity(want, []targetWindow{target}); !ok || got.hwnd != target.hwnd {
		t.Fatalf("exact normalized identity match = (%+v, %v)", got, ok)
	}
	if _, ok := matchTargetIdentity(identity, []targetWindow{target, target}); ok {
		t.Fatal("ambiguous identity unexpectedly matched")
	}
	wrongTitle := target
	wrongTitle.identity.Title = "Other"
	if _, ok := matchTargetIdentity(identity, []targetWindow{wrongTitle}); ok {
		t.Fatal("different title unexpectedly matched")
	}
	wrongClass := target
	wrongClass.identity.WindowClass = "Other"
	if _, ok := matchTargetIdentity(identity, []targetWindow{wrongClass}); ok {
		t.Fatal("different class unexpectedly matched")
	}
}

func TestValidSavedPointUsesClientBounds(t *testing.T) {
	for _, test := range []struct {
		point  savedPoint
		width  int32
		height int32
		want   bool
	}{
		{savedPoint{X: 0, Y: 0}, 100, 80, true},
		{savedPoint{X: 99, Y: 79}, 100, 80, true},
		{savedPoint{X: 100, Y: 79}, 100, 80, false},
		{savedPoint{X: -1, Y: 0}, 100, 80, false},
		{savedPoint{X: 0, Y: 0}, 0, 80, false},
	} {
		if got := validSavedPoint(test.point, test.width, test.height); got != test.want {
			t.Errorf("validSavedPoint(%+v, %d, %d) = %v, want %v", test.point, test.width, test.height, got, test.want)
		}
	}
}
