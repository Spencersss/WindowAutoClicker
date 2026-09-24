package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSavedWindowIndexPrefersExactIdentity(t *testing.T) {
	app := &application{settings: appSettings{Windows: []savedWindow{
		{Identity: windowIdentity{ExecutablePath: "C:\\Apps\\sample.exe", WindowClass: "SampleWindow", Title: "Old title"}},
		{Identity: windowIdentity{ExecutablePath: "c:\\apps\\SAMPLE.exe", WindowClass: "SampleWindow", Title: "Current title"}},
	}}}
	index := app.savedWindowIndex(windowIdentity{ExecutablePath: "C:\\Apps\\sample.exe", WindowClass: "SampleWindow", Title: "Current title"})
	if index != 1 {
		t.Fatalf("savedWindowIndex() = %d, want exact record 1", index)
	}
}

func TestSavedWindowIndexRebindsOnlyUniqueExecutableClass(t *testing.T) {
	identity := windowIdentity{ExecutablePath: "C:\\Apps\\sample.exe", WindowClass: "SampleWindow", Title: "New title"}
	app := &application{settings: appSettings{Windows: []savedWindow{
		{Identity: windowIdentity{ExecutablePath: identity.ExecutablePath, WindowClass: identity.WindowClass, Title: "Old title"}},
	}}}

	index := app.savedWindowIndex(identity)
	if index != 0 {
		t.Fatalf("savedWindowIndex() = %d, want unique fallback record 0", index)
	}
	if actual := app.ensureSavedWindow(identity); actual != 0 {
		t.Fatalf("ensureSavedWindow() = %d, want rebound record 0", actual)
	}
	if got := app.settings.Windows[0].Identity.Title; got != identity.Title {
		t.Fatalf("rebound title = %q, want %q", got, identity.Title)
	}
}

func TestSavedWindowIndexPreservesAmbiguousRecords(t *testing.T) {
	identity := windowIdentity{ExecutablePath: "C:\\Apps\\sample.exe", WindowClass: "SampleWindow", Title: "New title"}
	app := &application{settings: appSettings{Windows: []savedWindow{
		{Identity: windowIdentity{ExecutablePath: identity.ExecutablePath, WindowClass: identity.WindowClass, Title: "Old title"}},
		{Identity: windowIdentity{ExecutablePath: identity.ExecutablePath, WindowClass: identity.WindowClass, Title: "Another title"}},
	}}}

	if index := app.savedWindowIndex(identity); index != -1 {
		t.Fatalf("savedWindowIndex() = %d, want no match for ambiguous fallback", index)
	}
	if index, unique := app.uniqueSavedWindowClassIndex(identity); unique || index != -1 {
		t.Fatalf("uniqueSavedWindowClassIndex() = (%d, %t), want (-1, false)", index, unique)
	}
	if index := app.ensureSavedWindow(identity); index != 2 {
		t.Fatalf("ensureSavedWindow() = %d, want a new independent record 2", index)
	}
	if app.settings.Windows[0].Identity.Title != "Old title" || app.settings.Windows[1].Identity.Title != "Another title" {
		t.Fatal("ambiguous saved records were modified")
	}
}

func TestStartupRestoreRebindsUniqueWindowTitle(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable = normalizeExecutablePath(executable)
	oldIdentity := windowIdentity{ExecutablePath: executable, WindowClass: "SampleWindow", Title: "Old title"}
	newIdentity := windowIdentity{ExecutablePath: executable, WindowClass: "SampleWindow", Title: "New title"}
	path := filepath.Join(t.TempDir(), "settings.json")
	app := &application{
		settingsPath:     path,
		settingsWritable: true,
		settings: appSettings{
			Version:    settingsSchemaVersion,
			IntervalMS: minIntervalMS,
			Hotkey:     savedHotkey{Kind: uint8(keyboardHotkey), Code: vkF9},
			PIPWidth:   320,
			PIPHeight:  180,
			PIPFPS:     5,
			Selected:   &oldIdentity,
			Windows:    []savedWindow{{Identity: oldIdentity}},
		},
		targets: []targetWindow{{hwnd: 1, pid: 2, title: newIdentity.Title, identity: newIdentity}},
	}

	app.restoreStartupTarget()

	if app.selected.identity != newIdentity {
		t.Fatalf("restored identity = %+v, want %+v", app.selected.identity, newIdentity)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadAppSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Selected == nil || *loaded.Selected != newIdentity {
		t.Fatalf("persisted selection = %+v, want %+v", loaded.Selected, newIdentity)
	}
	if len(loaded.Windows) != 1 || loaded.Windows[0].Identity != newIdentity {
		t.Fatalf("persisted window identity = %+v, want one rebound identity", loaded.Windows)
	}
	if len(data) == 0 {
		t.Fatal("settings file was not written")
	}
}

func TestStartupRestoreDoesNotRebindAmbiguousWindowRecords(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable = normalizeExecutablePath(executable)
	oldIdentity := windowIdentity{ExecutablePath: executable, WindowClass: "SampleWindow", Title: "Old title"}
	newIdentity := windowIdentity{ExecutablePath: executable, WindowClass: "SampleWindow", Title: "New title"}
	secondIdentity := windowIdentity{ExecutablePath: executable, WindowClass: "SampleWindow", Title: "Another title"}
	path := filepath.Join(t.TempDir(), "settings.json")
	app := &application{
		settingsPath:     path,
		settingsWritable: true,
		settings: appSettings{
			Version:    settingsSchemaVersion,
			IntervalMS: minIntervalMS,
			Hotkey:     savedHotkey{Kind: uint8(keyboardHotkey), Code: vkF9},
			PIPWidth:   320,
			PIPHeight:  180,
			PIPFPS:     5,
			Selected:   &oldIdentity,
			Windows:    []savedWindow{{Identity: oldIdentity}, {Identity: secondIdentity}},
		},
		targets: []targetWindow{{hwnd: 1, pid: 2, title: newIdentity.Title, identity: newIdentity}},
	}

	app.restoreStartupTarget()

	if app.selected.hwnd != 1 {
		t.Fatal("unique live application window was not restored")
	}
	if app.settings.Windows[0].Identity != oldIdentity || app.settings.Windows[1].Identity != secondIdentity {
		t.Fatal("ambiguous saved records were rebound")
	}
	if app.settings.Selected == nil || *app.settings.Selected != newIdentity {
		t.Fatalf("selected identity was not updated: %+v", app.settings.Selected)
	}
	if len(app.settings.Windows) != 3 || app.settings.Windows[2].Identity != newIdentity {
		t.Fatal("ambiguous saved records were not preserved separately")
	}
}

func TestSavedPresetsAddOverwriteAndDelete(t *testing.T) {
	presets := []savedPreset{{Name: "Main", Point: savedPoint{X: 10, Y: 20}}}
	presets, added := addOrReplaceSavedPreset(presets, savedPreset{Name: "  Detail  ", Point: savedPoint{X: 40, Y: 50}}, false)
	if !added || len(presets) != 2 || presets[1].Name != "Detail" {
		t.Fatalf("new preset not added and trimmed: added=%t presets=%+v", added, presets)
	}
	presets, added = addOrReplaceSavedPreset(presets, savedPreset{Name: "main", Point: savedPoint{X: 15, Y: 25}}, false)
	if added || presets[0].Point != (savedPoint{X: 10, Y: 20}) {
		t.Fatalf("duplicate preset was added or changed: added=%t presets=%+v", added, presets)
	}
	presets, replaced := addOrReplaceSavedPreset(presets, savedPreset{Name: "MAIN", Point: savedPoint{X: 15, Y: 25}}, true)
	if !replaced || presets[0].Name != "MAIN" || presets[0].Point != (savedPoint{X: 15, Y: 25}) {
		t.Fatalf("preset not overwritten case-insensitively: replaced=%t presets=%+v", replaced, presets)
	}
	presets, deleted := deleteSavedPreset(presets, "detail")
	if !deleted || len(presets) != 1 || presets[0].Name != "MAIN" {
		t.Fatalf("preset not deleted case-insensitively: deleted=%t presets=%+v", deleted, presets)
	}
}

func TestClampWindowRectUsesSecondaryMonitorWorkArea(t *testing.T) {
	work := rect{left: 1920, top: 0, right: 3840, bottom: 1080}
	window := rect{left: 3500, top: 100, right: 4420, bottom: 800}

	got := clampWindowRectToWorkArea(window, work)
	if got.left != 2920 || got.right != 3840 || got.top != window.top || got.bottom != window.bottom {
		t.Fatalf("clamped secondary-monitor bounds = %+v, want left=2920 right=3840 with vertical position preserved", got)
	}
}

func TestClampWindowRectLimitsOversizedWindowToWorkArea(t *testing.T) {
	work := rect{left: 100, top: 40, right: 900, bottom: 640}
	window := rect{left: 200, top: 20, right: 1200, bottom: 700}

	got := clampWindowRectToWorkArea(window, work)
	if got != (rect{left: 100, top: 40, right: 900, bottom: 640}) {
		t.Fatalf("oversized window bounds = %+v, want full work area %+v", got, work)
	}
}
func TestSaveSettingsDoesNotOverwriteUnreadableSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	contents := []byte("{\"version\":999,\"private\":\"preserve-me\"}")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatal(err)
	}
	app := &application{
		settingsPath:      path,
		settings:          defaultAppSettings(),
		settingsLoadError: errUnsupportedSettingsVersion.Error(),
		settingsWritable:  false,
	}

	if err := app.saveSettings(); err == nil {
		t.Fatal("saveSettings() succeeded after settings load failed")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(contents) {
		t.Fatalf("settings file changed after load failure: got %q", got)
	}
}
