package main

import (
	"flag"
	"fmt"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var interactiveTarget = flag.Bool("interactive-target", false, "show a harmless click receiver for manual testing")
var procPeekMessage = user32.NewProc("PeekMessageW")
var fixtureCallback = syscall.NewCallback(fixtureWindowProc)
var fixture = struct {
	hwnd, label uintptr
	engine      *clicker
	driver      *nativeDriver
	events      []mouseRecord
	err         error
}{}

type mouseRecord struct {
	down          bool
	flags, coords uintptr
	at            time.Time
}

func fixtureWindowProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	switch message {
	case wmLButtonDown, wmLButtonUp:
		fixture.events = append(fixture.events, mouseRecord{message == wmLButtonDown, wparam, lparam, time.Now()})
		if fixture.label != 0 {
			downs, ups := 0, 0
			for _, e := range fixture.events {
				if e.down {
					downs++
				} else {
					ups++
				}
			}
			setWindowText(fixture.label, fmt.Sprintf("Safe click receiver\r\n\r\nDown: %d    Up: %d\r\nHeld: %t\r\nPosition: %d, %d", downs, ups, message == wmLButtonDown, loword(lparam), hiword(lparam)))
		}
		return 0
	case wmTimer:
		if fixture.driver != nil && wparam == fixture.driver.timerID {
			fixture.err = fixture.engine.tick()
		}
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	}
	return defaultWindowProc(hwnd, message, wparam, lparam)
}

func makeFixture(t *testing.T, visible bool) uintptr {
	t.Helper()
	instance, _, _ := procGetModuleHandle.Call(0)
	nameText := fmt.Sprintf("SpencerClickerTest%d", time.Now().UnixNano())
	name := utf16Ptr(nameText)
	class := wndClassEx{cbSize: uint32(unsafe.Sizeof(wndClassEx{})), lpfnWndProc: fixtureCallback, hInstance: instance, lpszClassName: name, hbrBackground: colorWindow + 1}
	if ok, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class))); ok == 0 {
		t.Fatal(err)
	}
	style := uint32(wsPopup)
	if visible {
		style = wsCaption | wsSysMenu | wsVisible
	}
	hwnd := createWindow(0, nameText, "Spencer Clicker - Test target", style, 50, 50, 460, 300, 0, 0, instance)
	if hwnd == 0 {
		t.Fatal("fixture window creation failed")
	}
	fixture.hwnd, fixture.events, fixture.err = hwnd, nil, nil
	if visible {
		fixture.label = createWindow(0, "STATIC", "Safe click receiver\r\n\r\nDown: 0    Up: 0\r\nHeld: false", wsChild|wsVisible, 24, 24, 400, 220, hwnd, 0, instance)
		font, _, _ := procGetStockObject.Call(17)
		sendMessage(fixture.label, wmSetFont, font, 1)
		procShowWindow.Call(hwnd, swShow)
	}
	t.Cleanup(func() { fixture.label, fixture.hwnd = 0, 0; fixture.engine = nil; fixture.driver = nil })
	return hwnd
}

func pumpFor(duration time.Duration) {
	deadline := time.Now().Add(duration)
	var message msg
	for time.Now().Before(deadline) {
		for {
			ok, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, 1)
			if ok == 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
		}
		time.Sleep(time.Millisecond)
	}
}

func TestNativeDelivery(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := makeFixture(t, false)
	defer procDestroyWindow.Call(hwnd)
	d := &nativeDriver{owner: hwnd, target: targetWindow{hwnd: hwnd, pid: windowPID(hwnd)}, coords: 150<<16 | 230}
	c := &clicker{driver: d}
	fixture.engine, fixture.driver = c, d
	if err := c.start(50, false); err != nil {
		t.Fatal(err)
	}
	pumpFor(245 * time.Millisecond)
	if err := c.stop(); err != nil {
		t.Fatal(err)
	}
	pumpFor(30 * time.Millisecond)
	if fixture.err != nil {
		t.Fatal(fixture.err)
	}
	if len(fixture.events) < 4 || len(fixture.events)%2 != 0 {
		t.Fatalf("unbalanced clicks: %+v", fixture.events)
	}
	for i, e := range fixture.events {
		if e.down != (i%2 == 0) || e.coords != d.coords {
			t.Fatalf("incorrect event: %+v", e)
		}
		if e.down && e.flags != mkLButton || !e.down && e.flags != 0 {
			t.Fatal("incorrect button flags")
		}
		if i > 0 && e.down && e.at.Sub(fixture.events[i-1].at) < 45*time.Millisecond {
			t.Fatal("interval was shorter than configured")
		}
	}
	count := len(fixture.events)
	pumpFor(100 * time.Millisecond)
	if len(fixture.events) != count {
		t.Fatal("input continued after stop")
	}
	if err := c.start(20, true); err != nil {
		t.Fatal(err)
	}
	pumpFor(650 * time.Millisecond)
	if len(fixture.events) != count+1 {
		t.Fatal("hold repeated clicks")
	}
	_ = c.stop()
	pumpFor(20 * time.Millisecond)
	if len(fixture.events) != count+2 || fixture.events[len(fixture.events)-1].down {
		t.Fatal("hold did not release")
	}
	if err := c.start(20, true); err != nil {
		t.Fatal(err)
	}
	procDestroyWindow.Call(hwnd)
	if c.tick() == nil || c.running || c.pressed {
		t.Fatal("target exit did not stop hold mode")
	}
}

func TestIntervalAndHotkeys(t *testing.T) {
	for _, tc := range []struct {
		text    string
		want    int
		invalid bool
	}{
		{"", 20, false}, {"0", 20, false}, {"19", 20, false}, {"50", 50, false}, {"2147483647", 2147483647, false},
		{"9999999999", 2147483647, false}, {"hello", 0, true}, {"1.5", 0, true}, {"99999999999999999999999", 0, true},
	} {
		got, err := parseInterval(tc.text)
		if got != tc.want || (err != nil) != tc.invalid {
			t.Fatalf("%q: %d, %v", tc.text, got, err)
		}
	}
	for _, tc := range []struct {
		msg, data, button uint32
		down, up          bool
	}{
		{wmRButtonDown, 0, mouseRight, true, false}, {wmMButtonUp, 0, mouseMiddle, false, true},
		{wmXButtonDown, 1 << 16, mouseX1, true, false}, {wmXButtonUp, 2 << 16, mouseX2, false, true},
		{wmLButtonDown, 0, 0, false, false},
	} {
		button, down, up := mouseEvent(tc.msg, tc.data)
		if button != tc.button || down != tc.down || up != tc.up {
			t.Fatal(tc)
		}
	}
}

func TestInteractiveTarget(t *testing.T) {
	if !*interactiveTarget {
		t.Skip("use -interactive-target to open the click receiver")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	setProcessDPIAware()
	hwnd := makeFixture(t, true)
	defer procDestroyWindow.Call(hwnd)
	for isWindow(hwnd) {
		pumpFor(20 * time.Millisecond)
	}
}

func TestPickerWindowResolution(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := makeFixture(t, true)
	pumpFor(20 * time.Millisecond)

	a := newApplication()
	a.pid = windowPID(hwnd) + 1
	activeApp = a
	t.Cleanup(func() { activeApp = nil })

	var bounds rect
	procGetWindowRect.Call(fixture.label, uintptr(unsafe.Pointer(&bounds)))
	screen := point{(bounds.left + bounds.right) / 2, (bounds.top + bounds.bottom) / 2}
	root := windowFromPoint(screen)
	ancestor, _, _ := procGetAncestor.Call(root, gaRoot)
	if ancestor != hwnd {
		t.Fatalf("child point resolved to %x, want root %x", ancestor, hwnd)
	}
	picked, ok := pickTargetAt(screen)
	if !ok || picked != hwnd {
		t.Fatalf("pickTargetAt = %x, %t; want %x, true", picked, ok, hwnd)
	}
	target, ok := targetWindowFor(hwnd)
	if !ok || target.pid != windowPID(hwnd) || target.title == "" {
		t.Fatalf("targetWindowFor = %+v, %t", target, ok)
	}

	a.pid = windowPID(hwnd)
	if _, ok := targetWindowFor(hwnd); ok {
		t.Fatal("targetWindowFor accepted a window from the current process")
	}
}

func TestPickerConsumesValidSelectionClick(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := makeFixture(t, true)
	pumpFor(20 * time.Millisecond)

	a := newApplication()
	a.pid = windowPID(hwnd) + 1
	a.picker = true
	activeApp = a
	t.Cleanup(func() { activeApp = nil })

	var bounds rect
	procGetWindowRect.Call(fixture.label, uintptr(unsafe.Pointer(&bounds)))
	data := &msLLHookStruct{pt: point{(bounds.left + bounds.right) / 2, (bounds.top + bounds.bottom) / 2}}
	if got := mouseHookProc(hcAction, wmLButtonDown, data); got != 1 || !a.pickerConsumed {
		t.Fatalf("picker mouse-down result = %d, consumed = %t; want 1, true", got, a.pickerConsumed)
	}
	_, inputHwnd, ok := pickInputAt(data.pt)
	if !ok || inputHwnd != fixture.label {
		t.Fatalf("pickInputAt = %x, %t; want child %x, true", inputHwnd, ok, fixture.label)
	}
	mainWindowProc(a.hwnd, wmAppPickTarget, inputHwnd, packPoint(data.pt))
	if a.picker || !a.pickerConsumed || a.selected.hwnd != hwnd || a.selected.inputHwnd != fixture.label || !a.selected.hasInputPoint {
		t.Fatalf("selection dispatch state = picker %t, consumed %t, selected %+v", a.picker, a.pickerConsumed, a.selected)
	}
	if got := mouseHookProc(hcAction, wmLButtonUp, data); got != 1 || a.pickerConsumed {
		t.Fatalf("picker mouse-up result = %d, consumed = %t; want 1, false", got, a.pickerConsumed)
	}
	mainWindowProc(a.hwnd, wmAppPickReleased, 0, 0)
}
func TestPickerStateAndSelection(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := makeFixture(t, true)
	pumpFor(20 * time.Millisecond)
	a := newApplication()
	a.pid = windowPID(hwnd) + 1
	a.picker = true
	activeApp = a
	t.Cleanup(func() { activeApp = nil })

	a.toggle()
	if a.picker {
		t.Fatal("starting the clicker did not disarm the picker")
	}
	a.picker = true
	var bounds rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
	screenPoint := point{(bounds.left + bounds.right) / 2, (bounds.top + bounds.bottom) / 2}
	a.selectPickedTarget(hwnd, screenPoint)
	if a.picker || a.selected.hwnd != hwnd || a.selected.pid != windowPID(hwnd) || !a.selected.hasInputPoint {
		t.Fatalf("picked target state = picker %t, selected %+v", a.picker, a.selected)
	}

	previous := a.selected
	a.picker = true
	a.cancelPicker()
	if a.picker || a.selected != previous {
		t.Fatalf("cancel changed target state: picker %t, selected %+v", a.picker, a.selected)
	}

	a.picker = true
	a.togglePicker()
	if a.picker {
		t.Fatal("picker button did not cancel an armed picker")
	}
}
func TestApplicationLifecycle(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	target := makeFixture(t, false)
	defer procDestroyWindow.Call(target)
	a := newApplication()
	activeApp = a
	a.instance, _, _ = procGetModuleHandle.Call(0)
	a.bgBrush, _, _ = procCreateSolidBrush.Call(colorBG)
	a.fieldBrush, _, _ = procCreateSolidBrush.Call(colorField)
	a.makeFonts()
	defer a.cleanup()
	className := fmt.Sprintf("SpencerClickerLifecycle%d", time.Now().UnixNano())
	if err := a.registerClass(className, mainCallback, a.bgBrush); err != nil {
		t.Fatal(err)
	}
	a.hwnd = createWindow(0, className, "Hidden test UI", wsPopup|wsClipChildren, 0, 0, 520, 580, 0, 0, a.instance)
	if a.hwnd == 0 {
		t.Fatal("application window creation failed")
	}
	a.driver.owner = a.hwnd
	a.targets = []targetWindow{{hwnd: target, pid: windowPID(target), title: "Isolated receiver"}}
	sendMessage(a.processCombo, cbAddString, 0, uintptr(unsafe.Pointer(utf16Ptr(a.targets[0].title))))
	sendMessage(a.processCombo, cbSetCurSel, 0, 0)
	a.handleCommand(idProcess, cbnSelChange)
	a.handleCommand(idHold, bnClicked)
	if !a.hold || a.selected.hwnd != target {
		t.Fatal("settings were not applied")
	}
	a.handleCommand(idToggle, bnClicked)
	pumpFor(30 * time.Millisecond)
	if !a.clicker.running || len(fixture.events) != 1 || !fixture.events[0].down {
		t.Fatal("UI did not start hold")
	}
	isEnabled := user32.NewProc("IsWindowEnabled")
	for _, hwnd := range []uintptr{a.processCombo, a.intervalEdit, a.holdButton, a.hotkeyButton} {
		if enabled, _, _ := isEnabled.Call(hwnd); enabled != 0 {
			t.Fatal("setting not locked while running")
		}
	}
	mainWindowProc(a.hwnd, wmClose, 0, 0)
	pumpFor(30 * time.Millisecond)
	if isWindow(a.hwnd) || a.clicker.running || a.clicker.pressed {
		t.Fatal("close did not shut down")
	}
	if len(fixture.events) != 2 || fixture.events[1].down {
		t.Fatal("closing app failed to release target")
	}
}

func TestEmbeddedApplicationIcon(t *testing.T) {
	instance, _, _ := procGetModuleHandle.Call(0)
	icon, _, _ := procLoadIcon.Call(instance, 1)
	if icon == 0 {
		t.Fatal("application icon resource ID 1 was not linked into the Windows binary")
	}
	// LoadIcon returns a shared system resource; Windows owns its lifetime.
}
