package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	wmAppTray     = 0x8004
	wmContextMenu = 0x007B
	ninSelect     = 0x0400
	ninKeySelect  = 0x0401
	trayID        = 1
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetFocus   = 3
	nimSetVersion = 4
	nifMessage    = 0x01
	nifIcon       = 0x02
	nifTip        = 0x04
	nifShowTip    = 0x80
	swHide        = 0
	swRestore     = 9
	idTrayShow    = 2001
	idTrayToggle  = 2002
	idTrayExit    = 2003
)

var (
	shell32                   = syscall.NewLazyDLL("shell32.dll")
	procShellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	procRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	procAppendMenu            = user32.NewProc("AppendMenuW")
	procTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	procDestroyMenu           = user32.NewProc("DestroyMenu")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procIsIconic              = user32.NewProc("IsIconic")
)

// Full NOTIFYICONDATAW layout (976 bytes on Windows x64).
type notifyIconData struct {
	size                uint32
	hwnd                uintptr
	id, flags, callback uint32
	icon                uintptr
	tip                 [128]uint16
	state, stateMask    uint32
	info                [256]uint16
	version             uint32
	infoTitle           [64]uint16
	infoFlags           uint32
	guid                [16]byte
	balloonIcon         uintptr
}

type trayIcon struct {
	owner, idleIcon, activeIcon uintptr
	registered, version4        bool
	call                        func(uint32, *notifyIconData) bool
}

func shellNotify(message uint32, data *notifyIconData) bool {
	ok, _, _ := procShellNotifyIcon.Call(uintptr(message), uintptr(unsafe.Pointer(data)))
	return ok != 0
}

func (t *trayIcon) data() notifyIconData {
	return notifyIconData{size: uint32(unsafe.Sizeof(notifyIconData{})), hwnd: t.owner, id: trayID}
}

func (t *trayIcon) add(data notifyIconData) bool {
	if t.call == nil || !t.call(nimAdd, &data) {
		return false
	}
	t.registered = true
	data.version = 4
	t.version4 = t.call(nimSetVersion, &data)
	return true
}

func (t *trayIcon) update(data notifyIconData) bool {
	if !t.registered {
		return t.add(data)
	}
	return t.call(nimModify, &data)
}

func (t *trayIcon) remove() {
	if t.registered {
		data := t.data()
		t.call(nimDelete, &data)
		t.registered = false
	}
}

func (a *application) createTray() error {
	// The window keeps the branded PE icon. The tray uses dedicated status
	// glyphs so green always means clicking and never resembles the app artwork.
	a.tray = trayIcon{owner: a.hwnd, idleIcon: a.makeIcon(false), activeIcon: a.makeIcon(true), call: shellNotify}
	if a.tray.idleIcon == 0 || a.tray.activeIcon == 0 {
		return fmt.Errorf("Could not create the tray icons.")
	}
	message, _, _ := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(utf16Ptr("TaskbarCreated"))))
	a.taskbarCreated = uint32(message)
	if message == 0 || !a.tray.add(a.trayData()) {
		return fmt.Errorf("Could not add the status icon to the Windows system tray.")
	}
	return nil
}

func (a *application) trayData() notifyIconData {
	data := a.tray.data()
	data.flags = nifMessage | nifIcon | nifTip | nifShowTip
	data.callback = wmAppTray
	data.icon = a.tray.idleIcon
	status := "Idle - " + a.hotkey.name() + " to start"
	if a.clicker.running || a.clicker.pressed {
		data.icon = a.tray.activeIcon
		status = "Clicking - " + a.hotkey.name() + " to stop"
		if a.hold {
			status = "Holding - " + a.hotkey.name() + " to stop"
		}
		if !a.clicker.running {
			status = "Release pending - press Stop to retry"
		}
	}
	tip, _ := syscall.UTF16FromString(appName + " | " + status)
	copy(data.tip[:len(data.tip)-1], tip)
	return data
}

func (a *application) updateTray() {
	if a.tray.call != nil && !a.tray.update(a.trayData()) {
		a.setStatus("Could not update the system-tray status icon.", true)
	}
}

func (a *application) showWindow() {
	mode := uintptr(swShow)
	if minimized, _, _ := procIsIconic.Call(a.hwnd); minimized != 0 {
		mode = swRestore
	}
	procShowWindow.Call(a.hwnd, mode)
	procSetForegroundWindow.Call(a.hwnd)
}

func (a *application) handleTrayEvent(wparam, lparam uintptr) {
	event := uint32(lparam)
	var position point
	if a.tray.version4 {
		if hiword(lparam) != trayID {
			return
		}
		event = uint32(loword(lparam))
		position = point{int32(int16(loword(wparam))), int32(int16(hiword(wparam)))}
	} else {
		if wparam != trayID {
			return
		}
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&position)))
	}
	switch event {
	case ninSelect, ninKeySelect, wmLButtonUp:
		a.showWindow()
	case wmContextMenu, wmRButtonUp:
		if position.x == -1 && position.y == -1 {
			procGetCursorPos.Call(uintptr(unsafe.Pointer(&position)))
		}
		a.showTrayMenu(position)
	}
}

func (a *application) showTrayMenu(position point) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		a.showWindow()
		return
	}
	defer procDestroyMenu.Call(menu)
	appendItem := func(id uintptr, text string) {
		procAppendMenu.Call(menu, 0, id, uintptr(unsafe.Pointer(utf16Ptr(text))))
	}
	appendItem(idTrayShow, "Show Spencer Clicker")
	action := "Start clicker"
	if a.clicker.running || a.clicker.pressed {
		action = "Stop clicker"
	}
	appendItem(idTrayToggle, action+" ("+a.hotkey.name()+")")
	procAppendMenu.Call(menu, 0x0800, 0, 0) // MF_SEPARATOR
	appendItem(idTrayExit, "Exit")
	// Foreground ownership and WM_NULL allow the menu to dismiss normally.
	procSetForegroundWindow.Call(a.hwnd)
	choice, _, _ := procTrackPopupMenu.Call(menu, 0x0100|0x0002, uintptr(position.x), uintptr(position.y), 0, a.hwnd, 0)
	postMessage(a.hwnd, 0, 0, 0)
	switch choice {
	case idTrayShow:
		a.showWindow()
	case idTrayToggle:
		a.toggle()
		if a.statusError {
			a.showWindow()
		}
	case idTrayExit:
		postMessage(a.hwnd, wmClose, 0, 0)
	default:
		data := a.tray.data()
		a.tray.call(nimSetFocus, &data)
	}
}
