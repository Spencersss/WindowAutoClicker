package main

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestSmallViewportKeepsSettingsReachable(t *testing.T) {
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	a := newPIPTestApplication(t)
	for _, tc := range []struct{ dpi, height int32 }{{96, 680}, {144, 970}} {
		a.dpi = tc.dpi
		procMoveWindow.Call(a.hwnd, 0, 0, uintptr(a.s(520)), uintptr(tc.height), 0)
		a.layout()
		a.scrollBy(mainContentHeight)
		var client, button rect
		getClientRect(a.hwnd, &client)
		procGetWindowRect.Call(a.toggleButton, uintptr(unsafe.Pointer(&button)))
		procMapWindowPoints.Call(0, a.hwnd, uintptr(unsafe.Pointer(&button)), 2)
		if a.scroll <= 0 || button.top < 0 || button.bottom > client.bottom {
			t.Fatalf("%d DPI: bottom control not reachable: scroll=%d button=%+v client=%+v", tc.dpi, a.scroll, button, client)
		}
		a.handleScroll(6)
		if a.scroll != 0 {
			t.Fatal("could not return to top of settings")
		}
	}
	if unsafe.Sizeof(scrollInfo{}) != 28 {
		t.Fatal("SCROLLINFO ABI mismatch")
	}
}

func TestPIPDPIIndependentOfMainWindow(t *testing.T) {
	a := newApplication()
	a.pip.setDPI(96)
	defer func() { procDeleteObject.Call(a.pip.font) }()
	w, h := a.pip.windowSize()
	a.dpi = 192
	w2, h2 := a.pip.windowSize()
	if w != w2 || h != h2 || a.pip.s(26) != 26 {
		t.Fatal("main DPI changed preview geometry")
	}
	a.pip.setDPI(192)
	_, h2 = a.pip.windowSize()
	if a.pip.s(26) != 52 || h2 <= h || a.pip.options.height != 180 {
		t.Fatal("preview DPI did not scale header independently of bitmap size")
	}
}
