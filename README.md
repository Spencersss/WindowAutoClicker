# Spencer Clicker

A native Windows autoclicker written in Go. Dark, resizable UI in Consolas;
one portable executable; no external Go modules, WebView, .NET, CGO, installer,
network requests, or background services.

## Run

Open **`dist/spencer-clicker.exe`** on Windows 10/11 x64.

1. Open the target-window dropdown and select an application. Opening the list refreshes it. You can also click the magnifying-glass button beside the dropdown, then click any visible top-level application window to choose it directly. Press Escape or click the picker button again to cancel; clicks on Spencer Clicker and invalid windows pass through normally.
2. Set the click interval, or turn on **Hold left click**.
3. Click **Start clicker** or press **F9** anywhere. Press again to stop.
4. Click the hotkey button to bind any single keyboard key, right/middle mouse,
   Mouse 4, or Mouse 5. Click the hotkey button again to cancel binding.
   Left mouse is reserved for normal UI interaction. Held keys do not repeatedly toggle.

The status is docked in the **Windows system tray beside the clock**: green
while clicking/holding, gray while idle. Hover for the current state and hotkey.
There is no permanent floating status overlay. Windows may initially put it in the tray's hidden
icons menu; use the taskbar's overflow arrow to find it and drag it beside the
clock if desired. Windows controls visibility and placement.

Minimize tucks the window into the tray; clicking the icon reopens it. Right-click
for **Show Spencer Clicker**, **Start/Stop clicker**, and **Exit**. The Close button
still stops clicking and exits. Exiting removes the icon, and restarting Explorer
restores the current status icon automatically. The footer also reports state in text.

Settings match the C# application: F9 by default, a 50 ms interval (minimum 20 ms),
and hold mode off. Settings are session-only, as in the original. Intervals above
2,147,483,647 ms are clamped to that Windows timer limit. Blank or smaller values
become 20 ms; invalid pasted text produces an inline error.

Each normal click presses for approximately **10 ms**, releases, then waits the
configured interval. Thus 50 ms means roughly a 60 ms cycle, not 20 clicks per
second. Timing is approximate and depends on Windows scheduling. Hold mode sends
one down and one up. Stop, normal exit, suspend, and session shutdown release a
held button; force-killing a process cannot run its cleanup.

## Picture-in-picture

Select a target, then turn **Picture-in-picture** on in the settings window.
The small always-on-top preview has no title bar, maximize/minimize controls, or
close button. Hold **Shift** while dragging anywhere on the image to move the
whole view. Toggle PiP off from the settings window. It stays visible when the
settings window is minimized to the tray. PiP is off by default, and operates
independently of whether clicking is running.

Choose a maximum preview size of **160 x 90**, **320 x 180**, **480 x 270**, or
**640 x 360**, and a refresh limit of **1, 2, 5, 10, 15, or 30 FPS**. The default
is **320 x 180 at 5 FPS**. Content keeps its aspect ratio with black padding;
changing the target, size, or refresh limit safely restarts the capture session.
These settings are session-only, like the clicker settings.

PiP requires **Windows 11 24H2 or newer** and a compatible graphics driver.
It uses Windows Graphics Capture's
[`MinUpdateInterval`](https://learn.microsoft.com/en-us/uwp/api/windows.graphics.capture.graphicscapturesession.minupdateinterval)
to limit the capture
producer, not merely discard full-speed frames. Older Windows versions can still
run the clicker; PiP reports an error instead of silently running unthrottled.
Windows may show its standard capture border around the target.

Capture runs on a separate thread so graphics work does not block click timing or
global hotkeys. Only the latest small preview bitmap is retained for display.
Lower FPS reduces capture frequency, and smaller previews reduce CPU sampling
and bitmap size. Windows still allocates source-sized GPU capture/staging buffers:
a tiny preview does **not** make a large source window free to capture. Turning
PiP off closes its capture session and releases those resources; no capture worker
runs while PiP is off after shutdown completes.

Minimized targets may stop producing frames (the preview labels this state).
Protected content, some exclusive-fullscreen games, or capture-restricted windows
may appear blank or reject capture. Restore the target or use windowed/borderless
mode where supported. PiP is a view only; dragging or clicking the preview does
not send mouse input to the target. Target closure or sleep switches PiP off.

## Compatibility with the original

Input uses `PostMessage(WM_LBUTTONDOWN/UP)` to the selected top-level window,
without moving the physical cursor or taking focus. Applications that ignore
these messages (including some games/raw-input applications) will also ignore
this clicker. A minimized target may stop processing input. If Windows reports access denied,
run the clicker at the same administrator level as the target.

Improvements over the old implementation:

- Uses client-area coordinates so the click is at the actual content center.
- Identifies targets by window handle and process ID instead of finding a title.
- Preserves duplicate window titles as separate choices.
- Locks target/settings while running; detects a closed target within about 500 ms.
- Releases on stop and exit; retries a failed release before allowing a new run.
- Sleeps in the Windows message loop; no busy spin in hold mode.

Hotkeys are single inputs, not chords, and are not swallowed from other applications,
matching the original behavior. Input generated by other software can trigger them;
the clicker's own posted mouse messages do not re-enter global input hooks.

## Build

Requires Go 1.25 or newer on Windows x64. From this directory:

```powershell
.\build.ps1
# Equivalent:
go build -buildvcs=false -trimpath -ldflags '-s -w -H=windowsgui' -o dist/spencer-clicker.exe ./cmd/spencer-clicker
```

The included `cmd/spencer-clicker/resource_windows_amd64.syso` embeds
`cmd/spencer-clicker/app.ico` as the executable's Windows icon and includes
the manifest for DPI awareness, Windows compatibility, standard privileges, and
standard privileges, and native control styles. Explorer and the taskbar therefore
show the Spencer Clicker artwork instead of Go's generic application icon. If editing
`cmd/spencer-clicker/app.manifest`, regenerate it with MinGW's resource compiler before rebuilding:

```powershell
.\build.ps1 -RegenerateResources
```

The resource compiler is unnecessary for an ordinary build. Close the app before
rebuilding. The build also copies the same executable to the project root for convenience.

## Migration and releases

This project is the native Go migration of the original C# Windows autoclicker.
The `main` branch is the source of truth for the migrated application; Git
history preserves the earlier project lineage. The Go module is
`github.com/Spencersss/WindowAutoClicker` and uses only the standard library.

Pull requests targeting `main` run the Windows tests, vet, and build checks but
do not publish anything. A push or merge to `main` builds a release, creates a
seven-character commit-SHA tag, and publishes a release titled `Release
<sha>`. Each release includes the executable directly and a zip archive of the
same executable. `workflow_dispatch` performs validation on other branches and
publishes only when dispatched from `main`.

## Verification

```powershell
go test -count=1 -v ./...
go vet -unsafeptr=false ./...
```

Tests cover cadence, balanced down/up events, hold mode, rapid restart,
failure cleanup, interval validation, mouse-button decoding, and real Win32
message delivery to an isolated test window. Tray tests verify state changes,
Explorer restart recovery, and actual shell registration/removal. The `unsafeptr` vet analyzer is
disabled because Win32 window callbacks supply native structure addresses in
`LPARAM`; the remaining vet analyzers run normally.

GitHub-hosted runners do not provide an interactive Explorer tray. CI explicitly
sets `SPENCER_CLICKER_SKIP_SHELL_TEST=1` to skip only the shell registration test;
all other tests still run. Leave it unset on a normal Windows desktop to also
verify the real gray/green notification-area icon.

For a harmless window to test manually:

```powershell
go test ./cmd/spencer-clicker -run TestInteractiveTarget -timeout 30m -args -interactive-target
```

Select **Spencer Clicker - Test target** in the app. The receiver displays mouse-down,
mouse-up, held state, and coordinates. Close the receiver to finish that test.

## Code map

Go application code and Windows resources live in `cmd/spencer-clicker/`:

- `engine.go`: small, synchronous click/hold state machine with testable I/O.
- `app_windows.go`: window lifecycle, settings, and UI events.
- `native_windows.go`: target discovery, timers, and global input hooks.
- `ui_windows.go`: painting, fonts, and icon artwork.
- `tray_windows.go`: native notification-area icon, status, and tray menu.

The repository root keeps the Go module, build/release scripts, documentation,
and shared assets; executable output remains in `dist/` and at the root.
- `pip_windows.go`: optional draggable preview, settings, and UI lifecycle.
- `capture_windows.go`: rate-limited Windows Graphics Capture worker and scaling.
- `win32_windows.go`: Win32 declarations; standard library only.

Native API references: [mouse-message coordinates](https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-lbuttondown),
[Windows timers](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-settimer),
[Windows notification-area icons](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shell_notifyiconw).
