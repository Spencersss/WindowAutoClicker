package main

import (
	"os"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestTrayStateAndLifecycle(t *testing.T) {
	a := newApplication()
	var calls []uint32
	a.tray = trayIcon{owner: 123, idleIcon: 11, activeIcon: 22, call: func(message uint32, data *notifyIconData) bool {
		calls = append(calls, message)
		if data.hwnd != 123 || data.id != trayID || data.size != 976 {
			t.Fatal("bad notification identity/layout")
		}
		return true
	}}
	if unsafe.Sizeof(notifyIconData{}) != 976 || unsafe.Offsetof(notifyIconData{}.tip) != 40 || unsafe.Offsetof(notifyIconData{}.version) != 816 {
		t.Fatal("NOTIFYICONDATAW ABI mismatch")
	}
	if !a.tray.add(a.trayData()) || !a.tray.version4 {
		t.Fatal("tray registration failed")
	}
	for _, tc := range []struct {
		running, pressed, hold bool
		icon                   uintptr
		tip                    string
	}{
		{false, false, false, 11, "Idle"}, {true, true, false, 22, "Clicking"},
		{true, true, true, 22, "Holding"}, {false, true, true, 22, "Release pending"},
	} {
		a.clicker.running, a.clicker.pressed, a.hold = tc.running, tc.pressed, tc.hold
		data := a.trayData()
		if data.icon != tc.icon || !strings.Contains(syscall.UTF16ToString(data.tip[:]), tc.tip) {
			t.Fatal("incorrect status", tc)
		}
		if data.callback != wmAppTray || data.flags&(nifIcon|nifMessage|nifTip) != nifIcon|nifMessage|nifTip {
			t.Fatal("missing tray callbacks/status")
		}
		if !a.tray.update(data) {
			t.Fatal("update failed")
		}
	}
	// Simulate Explorer discarding icons, without restarting the user's shell.
	a.tray.registered = false
	if !a.tray.add(a.trayData()) {
		t.Fatal("could not restore tray icon")
	}
	a.tray.remove()
	a.tray.remove()
	if !reflect.DeepEqual(calls, []uint32{nimAdd, nimSetVersion, nimModify, nimModify, nimModify, nimModify, nimAdd, nimSetVersion, nimDelete}) {
		t.Fatal(calls)
	}
}

func TestNativeTrayRegistration(t *testing.T) {
	if os.Getenv("SPENCER_CLICKER_SKIP_SHELL_TEST") == "1" {
		t.Skip("Explorer shell integration explicitly disabled; state/lifecycle tests still run")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := makeFixture(t, false)
	defer procDestroyWindow.Call(hwnd)
	instance, _, _ := procGetModuleHandle.Call(0)
	a := newApplication()
	a.instance, a.hwnd = instance, hwnd
	a.icon = a.makeIcon(true)
	defer procDestroyIcon.Call(a.icon)
	if err := a.createTray(); err != nil {
		t.Fatal(err)
	}
	if a.tray.activeIcon == a.icon {
		t.Fatal("tray active state reused the branded application icon")
	}
	defer procDestroyIcon.Call(a.tray.idleIcon)
	defer procDestroyIcon.Call(a.tray.activeIcon)
	defer a.tray.remove()
	identifier := struct {
		size uint32
		hwnd uintptr
		id   uint32
		guid [16]byte
	}{}
	identifier.size = uint32(unsafe.Sizeof(identifier))
	identifier.hwnd = hwnd
	identifier.id = trayID
	var bounds rect
	getRect := shell32.NewProc("Shell_NotifyIconGetRect")
	result, _, _ := getRect.Call(uintptr(unsafe.Pointer(&identifier)), uintptr(unsafe.Pointer(&bounds)))
	if int32(result) < 0 {
		t.Fatalf("Windows did not register the docked icon: HRESULT=%X", result)
	}
	if bounds.right <= bounds.left || bounds.bottom <= bounds.top {
		t.Fatal("tray icon has no shell-owned bounds")
	}
	a.clicker.running = true
	if !a.tray.update(a.trayData()) {
		t.Fatal("could not update docked icon")
	}
	a.tray.remove()
	result, _, _ = getRect.Call(uintptr(unsafe.Pointer(&identifier)), uintptr(unsafe.Pointer(&bounds)))
	if int32(result) >= 0 {
		t.Fatal("tray icon remained after removal")
	}
}
