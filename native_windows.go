package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

// Win32 callbacks are allocated once; syscall.NewCallback keeps them for the
// process lifetime, so creating one on every dropdown refresh would leak.
var (
	mainCallback  = syscall.NewCallback(mainWindowProc)
	enumCallback  = syscall.NewCallback(enumWindowCallback)
	keyCallback   = syscall.NewCallback(keyboardHookProc)
	mouseCallback = syscall.NewCallback(mouseHookProc)
)

type nativeDriver struct {
	owner   uintptr
	target  targetWindow
	coords  uintptr
	timerID uintptr
}

func windowPID(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func (d *nativeDriver) valid() bool {
	return d.target.hwnd != 0 && isWindow(d.target.hwnd) && windowPID(d.target.hwnd) == d.target.pid
}

func (d *nativeDriver) button(down bool) error {
	message, flags := uint32(wmLButtonUp), uintptr(0)
	if down {
		message, flags = wmLButtonDown, mkLButton
	}
	ok, _, err := procPostMessage.Call(d.target.hwnd, uintptr(message), flags, d.coords)
	if ok == 0 {
		if err == syscall.Errno(5) {
			return fmt.Errorf("Access denied. Match the target's administrator level.")
		}
		return fmt.Errorf("Could not send mouse input (%v).", err)
	}
	return nil
}

func (d *nativeDriver) arm(milliseconds int) error {
	d.disarm()
	d.timerID++ // Ignore a stale queued WM_TIMER after stop/restart.
	if d.timerID == 0 || d.timerID >= pipTimerID {
		d.timerID = 1
	}
	ok, _, err := procSetTimer.Call(d.owner, d.timerID, uintptr(milliseconds), 0)
	if ok == 0 {
		return fmt.Errorf("Could not schedule clicks (%v).", err)
	}
	return nil
}

func (d *nativeDriver) disarm() {
	if d.timerID != 0 {
		procKillTimer.Call(d.owner, d.timerID)
	}
}

func windowText(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLength.Call(hwnd)
	if length == 0 {
		return ""
	}
	buffer := make([]uint16, length+1)
	procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return syscall.UTF16ToString(buffer)
}

func (a *application) refreshTargets() {
	if a.clicker.running {
		return
	}
	a.targets = nil
	procEnumWindows.Call(enumCallback, 0)
	sortTargets(a.targets)
	sendMessage(a.processCombo, cbResetContent, 0, 0)
	counts := make(map[string]int)
	for _, target := range a.targets {
		counts[target.title]++
	}
	found := false
	for i, target := range a.targets {
		label := target.title
		if counts[label] > 1 {
			label = fmt.Sprintf("%s  [pid %d / %X]", label, target.pid, target.hwnd)
		}
		sendMessage(a.processCombo, cbAddString, 0, uintptr(unsafe.Pointer(utf16Ptr(label))))
		if target.hwnd == a.selected.hwnd && target.pid == a.selected.pid {
			sendMessage(a.processCombo, cbSetCurSel, uintptr(i), 0)
			found = true
		}
	}
	if !found {
		a.selected = targetWindow{}
		if a.pip.enabled {
			a.stopPIP()
		}
	}
}

func enumWindowCallback(hwnd, _ uintptr) uintptr {
	a := activeApp
	if a == nil {
		return 1
	}
	visible, _, _ := procIsWindowVisible.Call(hwnd)
	pid := windowPID(hwnd)
	if visible == 0 || pid == a.pid {
		return 1
	}
	owner, _, _ := procGetWindow.Call(hwnd, gwOwner)
	index := int32(gwlExStyle)
	style, _, _ := procGetWindowLongPtr.Call(hwnd, uintptr(index))
	if owner != 0 || style&wsExToolWindow != 0 {
		return 1
	}
	title := strings.TrimSpace(windowText(hwnd))
	if title != "" {
		a.targets = append(a.targets, targetWindow{hwnd: hwnd, pid: pid, title: title})
	}
	return 1
}

func (a *application) installHooks() error {
	var err error
	a.keyboardHook, _, err = procSetWindowsHookEx.Call(whKeyboardLL, keyCallback, a.instance, 0)
	if a.keyboardHook == 0 {
		return fmt.Errorf("Keyboard hotkey unavailable: %v", err)
	}
	return a.syncMouseHook()
}

func (a *application) syncMouseHook() error {
	needed := a.capture || a.hotkey.kind == mouseHotkey
	if !needed && a.mouseHook != 0 {
		procUnhookWindowsHookEx.Call(a.mouseHook)
		a.mouseHook = 0
		a.mouseDown = [5]bool{}
	}
	// Keyboard-only operation does not need high-frequency mouse events.
	if needed && a.mouseHook == 0 {
		var err error
		a.mouseHook, _, err = procSetWindowsHookEx.Call(whMouseLL, mouseCallback, a.instance, 0)
		if a.mouseHook == 0 {
			return fmt.Errorf("Mouse hotkey unavailable: %v", err)
		}
	}
	return nil
}

func (a *application) uninstallHooks() {
	for _, handle := range []uintptr{a.keyboardHook, a.mouseHook} {
		if handle != 0 {
			procUnhookWindowsHookEx.Call(handle)
		}
	}
	a.keyboardHook, a.mouseHook = 0, 0
}

// Hooks only debounce and queue input. All UI work happens after returning to
// Windows, keeping the global hooks fast. Hotkeys continue to reach other apps,
// matching the original clicker. Posted click messages do not re-enter hooks.
func keyboardHookProc(code int32, wparam uintptr, data *kbdLLHookStruct) uintptr {
	a := activeApp
	if code == hcAction && a != nil && data != nil && data.vkCode < 256 {
		down := uint32(wparam) == wmKeyDown || uint32(wparam) == wmSysKeyDown
		up := uint32(wparam) == wmKeyUp || uint32(wparam) == wmSysKeyUp
		if up {
			a.keysDown[data.vkCode] = false
		}
		if down && !a.keysDown[data.vkCode] {
			a.keysDown[data.vkCode] = true
			if a.capture || a.hotkey == (hotkey{keyboardHotkey, data.vkCode}) {
				postMessage(a.hwnd, wmAppInput, uintptr(keyboardHotkey), uintptr(data.vkCode))
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wparam, uintptr(unsafe.Pointer(data)))
	return r
}

func mouseHookProc(code int32, wparam uintptr, data *msLLHookStruct) uintptr {
	a := activeApp
	if code == hcAction && a != nil && data != nil {
		button, down, up := mouseEvent(uint32(wparam), data.mouseData)
		if button != 0 {
			if up {
				a.mouseDown[button] = false
			}
			if down && !a.mouseDown[button] {
				a.mouseDown[button] = true
				if a.capture || a.hotkey == (hotkey{mouseHotkey, button}) {
					postMessage(a.hwnd, wmAppInput, uintptr(mouseHotkey), uintptr(button))
				}
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wparam, uintptr(unsafe.Pointer(data)))
	return r
}

func mouseEvent(message, data uint32) (button uint32, down, up bool) {
	switch message {
	case wmRButtonDown, wmRButtonUp:
		return mouseRight, message == wmRButtonDown, message == wmRButtonUp
	case wmMButtonDown, wmMButtonUp:
		return mouseMiddle, message == wmMButtonDown, message == wmMButtonUp
	case wmXButtonDown, wmXButtonUp:
		button = mouseX1
		if data>>16 == 2 {
			button = mouseX2
		}
		return button, message == wmXButtonDown, message == wmXButtonUp
	}
	return 0, false, false // Left mouse remains reserved for normal UI use.
}
