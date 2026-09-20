//go:build windows

package main

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

const cbGetCountPIP = 0x0146

// newPIPTestApplication creates the controls without running the real message
// loop. Its parent class is unique so tests do not depend on the application
// class registration or on another test's window lifetime.
func newPIPTestApplication(t *testing.T) *application {
	t.Helper()
	a := newApplication()
	a.instance, _, _ = procGetModuleHandle.Call(0)
	a.bgBrush, _, _ = procCreateSolidBrush.Call(colorBG)
	a.fieldBrush, _, _ = procCreateSolidBrush.Call(colorField)
	a.makeFonts()
	activeApp = a
	className := fmt.Sprintf("SpencerClickerPIPTest%d", time.Now().UnixNano())
	if err := a.registerClass(className, fixtureCallback, a.bgBrush); err != nil {
		a.cleanup()
		t.Fatal(err)
	}
	a.hwnd = createWindow(0, className, "Hidden PiP test UI", wsPopup|wsClipChildren, 0, 0, 520, 740, 0, 0, a.instance)
	if a.hwnd == 0 {
		a.cleanup()
		t.Fatal("test UI window creation failed")
	}
	a.createControls(a.hwnd)
	t.Cleanup(func() {
		a.cleanup()
	})
	return a
}

func TestPIPDefaultsAndOptionsWhileClickerRuns(t *testing.T) {
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	a := newPIPTestApplication(t)

	if got, want := a.pip.options, (captureOptions{width: 320, height: 180, fps: 5}); got != want {
		t.Fatalf("default PiP options = %+v, want %+v", got, want)
	}
	if got := int(sendMessage(a.pipSizeCombo, cbGetCountPIP, 0, 0)); got != len(pipSizes) {
		t.Fatalf("resolution options = %d, want %d", got, len(pipSizes))
	}
	if got := int(sendMessage(a.pipFPSCombo, cbGetCountPIP, 0, 0)); got != len(pipFrameRates) {
		t.Fatalf("FPS options = %d, want %d", got, len(pipFrameRates))
	}

	// Option changes while PiP is off must still update state without starting
	// the capture backend.
	sendMessage(a.pipSizeCombo, cbSetCurSel, 3, 0)
	sendMessage(a.pipFPSCombo, cbSetCurSel, 5, 0)
	a.handleCommand(idPIPSize, cbnSelChange)
	if got, want := a.pip.options, (captureOptions{width: 640, height: 360, fps: 30}); got != want {
		t.Fatalf("changed PiP options = %+v, want %+v", got, want)
	}
	if a.pip.enabled || a.pip.worker != nil {
		t.Fatal("changing options while off started PiP")
	}

	// Start the ordinary clicker against the harmless fixture, then verify the
	// optional PiP controls remain usable while the clicker locks its own ones.
	target := makeFixture(t, false)
	defer procDestroyWindow.Call(target)
	a.driver.owner = a.hwnd
	a.selected = targetWindow{hwnd: target, pid: windowPID(target), title: "fixture"}
	a.toggle()
	if !a.clicker.running {
		t.Fatal("clicker failed to start for running-state check")
	}
	defer a.clicker.stop()
	a.handleCommand(idPIPFPS, cbnSelChange)
	isEnabled := user32.NewProc("IsWindowEnabled")
	for _, hwnd := range []uintptr{a.pipButton, a.pipSizeCombo, a.pipFPSCombo} {
		if enabled, _, _ := isEnabled.Call(hwnd); enabled == 0 {
			t.Fatalf("PiP control %x became disabled while clicker was running", hwnd)
		}
	}
}

func TestPIPFrameBlankDetection(t *testing.T) {
	if !pipFrameIsBlank(captureResult{width: 2, height: 1, pixels: []byte{0, 0, 0, 255, 2, 2, 2, 255}}) {
		t.Fatal("dark frame was not classified as blank")
	}
	if pipFrameIsBlank(captureResult{width: 1, height: 1, pixels: []byte{3, 0, 0, 255}}) {
		t.Fatal("visible frame was classified as blank")
	}
}

func TestPIPWindowIsNonActivatingAndClosable(t *testing.T) {
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	a := newPIPTestApplication(t)
	a.pip.enabled = true
	if err := a.ensurePIPWindow(); err != nil {
		t.Fatal(err)
	}
	if a.pip.hwnd == 0 || !isWindow(a.pip.hwnd) {
		t.Fatal("PiP window was not created")
	}
	getWindowLongPtr := user32.NewProc("GetWindowLongPtrW")
	styleIndex := int32(gwlExStyle)
	exStyle, _, _ := getWindowLongPtr.Call(a.pip.hwnd, uintptr(styleIndex))
	if exStyle&uintptr(wsExToolWindow|wsExNoActivate) != uintptr(wsExToolWindow|wsExNoActivate) {
		t.Fatalf("PiP extended style %#x lacks tool/no-activate styles", exStyle)
	}
	styleIndex = int32(-16) // GWL_STYLE
	style, _, _ := getWindowLongPtr.Call(a.pip.hwnd, uintptr(styleIndex))
	// WS_CAPTION includes WS_BORDER; allow the requested subtle border, but
	// reject the dialog-frame/title-bar and system-button bits.
	if style&uintptr(0x00400000|wsSysMenu|wsMinimizeBox|wsMaximizeBox) != 0 {
		t.Fatalf("PiP unexpectedly has traditional title-bar controls: %#x", style)
	}
	if pipWindowProc(a.pip.hwnd, 0x0084, 0, 0) != 1 {
		t.Fatal("PiP client did not report HTCLIENT for borderless dragging")
	}
	if result := pipWindowProc(a.pip.hwnd, wmMouseActivatePIP, 0, 0); result != 3 {
		t.Fatalf("WM_MOUSEACTIVATE result = %d, want MA_NOACTIVATE (3)", result)
	}
	pipWindowProc(a.pip.hwnd, wmClose, 0, 0)
	if a.pip.enabled || a.pip.hwnd != 0 {
		t.Fatal("closing PiP did not disable and clear it")
	}
}

const wmMouseActivatePIP = 0x0021

func TestWindowCaptureStopIsIdempotentAndRestartWaits(t *testing.T) {
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	a := newPIPTestApplication(t)
	frames := make(chan captureResult)
	done := make(chan struct{})
	defer close(done)
	c := &windowCapture{frames: frames, done: done, cancel: make(chan struct{})}
	c.stop()
	c.stop()
	select {
	case <-c.cancel:
	default:
		t.Fatal("stop did not signal cancellation")
	}

	a.pip = pictureInPicture{
		enabled: true,
		worker:  c,
		options: captureOptions{width: 320, height: 180, fps: 5},
	}
	a.restartPIP()
	if a.pip.worker != c || !a.pip.stopping {
		t.Fatal("restart replaced capture worker before prior worker completed")
	}
	select {
	case <-done:
		t.Fatal("fake worker unexpectedly completed")
	default:
	}
}

func TestNativeDriverArmSkipsReservedPIPTimer(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := makeFixture(t, false)
	defer procDestroyWindow.Call(hwnd)
	d := &nativeDriver{owner: hwnd, timerID: pipTimerID - 1}
	if err := d.arm(20); err != nil {
		t.Fatal(err)
	}
	defer d.disarm()
	if d.timerID != 1 {
		t.Fatalf("wrapped native timer ID = %d, want 1 (reserved PiP ID skipped)", d.timerID)
	}
}
