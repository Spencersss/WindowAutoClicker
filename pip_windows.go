package main

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const pipTimerID = healthTimerID - 1
const pipClassName = "SpencerClickerPreview"

var pipSizes = [...]struct {
	label         string
	width, height int
}{
	{"160 x 90", 160, 90},
	{"320 x 180", 320, 180},
	{"480 x 270", 480, 270},
	{"640 x 360", 640, 360},
}
var pipFrameRates = [...]int{1, 2, 5, 10, 15, 30}
var (
	pipCallback         = syscall.NewCallback(pipWindowProc)
	pipIsIconic         = user32.NewProc("IsIconic")
	pipStretchDIBits    = gdi32.NewProc("StretchDIBits")
	pipGetDpiForWindow  = user32.NewProc("GetDpiForWindow")
	pipAdjustWindowRect = user32.NewProc("AdjustWindowRectExForDpi")
	pipUnregisterClass  = user32.NewProc("UnregisterClassW")
	pipGetKeyState      = user32.NewProc("GetKeyState")
	pipReleaseCapture   = user32.NewProc("ReleaseCapture")
)

// All preview state belongs to the UI thread. The capture worker only publishes
// immutable, small BGRA frames; it never calls the clicker's UI or timer code.
type pictureInPicture struct {
	dpi                 int32
	font                uintptr
	hwnd                uintptr
	enabled, registered bool
	stopping            bool
	worker              *windowCapture
	options             captureOptions
	target              targetWindow
	frame               captureResult
	lastFrame           time.Time
	note                string
	dragging            bool
}

func (a *application) enablePIP() {
	target := nativeDriver{target: a.selected}
	if !target.valid() {
		a.setStatus("Select a target window before enabling PiP.", true)
		return
	}
	a.pip.enabled = true
	a.syncPIPButton()
	a.restartPIP()
}

func (a *application) syncPIPButton() {
	if a.pipButton == 0 {
		return
	}
	label := "Picture-in-picture: Off"
	if a.pip.enabled {
		label = "Picture-in-picture: On"
	}
	setWindowText(a.pipButton, label)
	procInvalidateRect.Call(a.pipButton, 0, 0)
}

func (a *application) changePIPOptions() {
	size := int32(sendMessage(a.pipSizeCombo, cbGetCurSel, 0, 0))
	fps := int32(sendMessage(a.pipFPSCombo, cbGetCurSel, 0, 0))
	if size < 0 || int(size) >= len(pipSizes) || fps < 0 || int(fps) >= len(pipFrameRates) {
		return
	}
	a.pip.options = captureOptions{pipSizes[size].width, pipSizes[size].height, pipFrameRates[fps]}
	a.restartPIP()
}

func (a *application) restartPIP() {
	if !a.pip.enabled {
		return
	}
	if a.pip.worker != nil {
		a.pip.worker.stop()
		a.pip.stopping = true
	}
	a.pip.frame = captureResult{}
	a.pip.note = "Starting preview..."
	if a.pip.hwnd != 0 {
		procInvalidateRect.Call(a.pip.hwnd, 0, 0)
	}
	// Monitor target/capture completion at least five times a second. Frames
	// themselves are only generated and delivered at the configured FPS limit.
	interval := min(200, 1000/a.pip.options.fps)
	if timer, _, _ := procSetTimer.Call(a.hwnd, pipTimerID, uintptr(interval), 0); timer == 0 {
		a.stopPIP()
		a.setStatus("Could not schedule the preview window.", true)
		return
	}
	a.tickPIP()
}

func (a *application) stopPIP() {
	a.pip.enabled = false
	if a.pip.worker != nil {
		a.pip.worker.stop()
		a.pip.stopping = true
	} else {
		procKillTimer.Call(a.hwnd, pipTimerID)
	}
	if a.pip.hwnd != 0 {
		hwnd := a.pip.hwnd
		a.pip.hwnd = 0
		procDestroyWindow.Call(hwnd)
	}
	if a.pip.font != 0 {
		procDeleteObject.Call(a.pip.font)
		a.pip.font = 0
	}
	a.pip.frame = captureResult{}
	a.pip.dragging = false
	a.syncPIPButton()
}

func (a *application) waitPIPShutdown() {
	if a.pip.worker != nil {
		// Only used after the main message loop has exited. Interactive stop and
		// reconfiguration never wait for COM/graphics-driver cleanup on the UI.
		select {
		case <-a.pip.worker.done:
		case <-time.After(2 * time.Second):
		}
		a.pip.worker = nil
	}
	if a.pip.registered {
		pipUnregisterClass.Call(uintptr(unsafe.Pointer(utf16Ptr(pipClassName))), a.instance)
		a.pip.registered = false
	}
}

func (a *application) tickPIP() {
	p := &a.pip
	if p.stopping && p.worker != nil {
		select {
		case <-p.worker.done:
			p.worker, p.stopping = nil, false
		default:
			return
		}
	}
	if !p.enabled {
		procKillTimer.Call(a.hwnd, pipTimerID)
		return
	}
	target := nativeDriver{target: a.selected}
	if !target.valid() {
		a.stopPIP()
		a.setStatus("PiP stopped: the target window closed.", true)
		return
	}
	if p.worker == nil {
		if err := a.ensurePIPWindow(); err != nil {
			a.stopPIP()
			a.setStatus(err.Error(), true)
			return
		}
		p.target = a.selected
		p.lastFrame = time.Now()
		p.worker = startWindowCapture(p.target, p.options)
	}
	changed := false
	select {
	case frame, ok := <-p.worker.frames:
		if frame.err != nil {
			a.stopPIP()
			a.setStatus("PiP: "+frame.err.Error(), true)
			return
		}
		if !ok {
			a.stopPIP()
			a.setStatus("PiP capture ended. Toggle it on to retry.", true)
			return
		}
		if frame.width > 0 && frame.height > 0 && len(frame.pixels) == frame.width*frame.height*4 {
			p.frame, p.lastFrame = frame, time.Now()
			a.resizePIPToFrame(frame.width, frame.height)
			changed = true
		}
	default:
	}
	note := ""
	if iconic, _, _ := pipIsIconic.Call(p.target.hwnd); iconic != 0 {
		note = "Target minimized"
	} else if time.Since(p.lastFrame) > 3*time.Second {
		note = "No new frames"
	} else if len(p.frame.pixels) == 0 {
		note = "Starting preview..."
	}
	if p.note != note {
		p.note, changed = note, true
	}
	if changed {
		procInvalidateRect.Call(p.hwnd, 0, 0)
	}
}

func (a *application) resizePIPToFrame(width, height int) {
	if a.pip.hwnd == 0 || width < 1 || height < 1 {
		return
	}
	wantWidth := int32(width) + a.pip.s(2)
	wantHeight := int32(height) + a.pip.s(2)
	var current rect
	if getClientRect(a.pip.hwnd, &current) && current.right == wantWidth && current.bottom == wantHeight {
		return
	}
	procSetWindowPos.Call(a.pip.hwnd, ^uintptr(0), 0, 0, uintptr(wantWidth), uintptr(wantHeight), swpNoMove|swpNoActivate|swpShowWindow)
}

func (a *application) ensurePIPWindow() error {
	p := &a.pip
	if !p.registered {
		if err := a.registerClass(pipClassName, pipCallback, a.bgBrush); err != nil {
			return err
		}
		p.registered = true
	}
	if p.dpi == 0 {
		p.dpi = a.dpi
	}
	width, height := p.windowSize()
	if p.hwnd == 0 {
		var work rect
		procSystemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&work)), 0)
		p.hwnd = createWindow(wsExToolWindow|wsExNoActivate|0x8, pipClassName,
			"Spencer Clicker - Picture-in-picture", wsPopup|wsBorder,
			work.right-width-20, work.bottom-height-20, width, height, 0, 0, a.instance)
		if p.hwnd == 0 {
			return fmt.Errorf("Could not create the preview window.")
		}
	}
	if pipGetDpiForWindow.Find() == nil {
		if dpi, _, _ := pipGetDpiForWindow.Call(p.hwnd); dpi != 0 {
			p.setDPI(int32(dpi))
		}
	}
	if p.font == 0 {
		p.setDPI(p.dpi)
	}
	width, height = p.windowSize()
	// An independent non-activating tool window stays visible when the main
	// settings window is minimized into the tray, without stealing target focus.
	procSetWindowPos.Call(p.hwnd, ^uintptr(0), 0, 0, uintptr(width), uintptr(height), swpNoMove|swpNoActivate|swpShowWindow)
	procInvalidateRect.Call(p.hwnd, 0, 0)
	return nil
}

func (p *pictureInPicture) s(value int32) int32 { return value * max(96, p.dpi) / 96 }

func (p *pictureInPicture) setDPI(dpi int32) {
	if p.dpi == dpi && p.font != 0 {
		return
	}
	p.dpi = max(96, dpi)
	if p.font != 0 {
		procDeleteObject.Call(p.font)
	}
	p.font, _, _ = procCreateFont.Call(uintptr(-p.s(13)), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0x31,
		uintptr(unsafe.Pointer(utf16Ptr("Consolas"))))
}

func (p *pictureInPicture) windowSize() (int32, int32) {
	// WS_POPUP has no non-client area. Reserve a scaled two-pixel frame for
	// the subtle custom border painted by paintPIP.
	return int32(p.options.width) + p.s(2), int32(p.options.height) + p.s(2)
}

func pipWindowProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	a := activeApp
	if a == nil {
		return defaultWindowProc(hwnd, message, wparam, lparam)
	}
	switch message {
	case wmDPIChanged:
		a.pip.setDPI(int32(loword(wparam)))
		bounds := (*rect)(unsafe.Pointer(lparam))
		width, height := a.pip.windowSize()
		procSetWindowPos.Call(hwnd, ^uintptr(0), uintptr(bounds.left), uintptr(bounds.top), uintptr(width), uintptr(height), swpNoActivate)
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case 0x0084: // WM_NCHITTEST: the whole view is client content.
		return 1 // HTCLIENT
	case 0x0021: // WM_MOUSEACTIVATE
		return 3 // MA_NOACTIVATE
	case wmLButtonDown:
		// Match tcpowell/picture-in-picture: Shift+drag moves the entire
		// borderless preview without requiring a title bar or close button.
		if state, _, _ := pipGetKeyState.Call(vkShift); int32(state)&0x8000 != 0 {
			a.pip.dragging = true
			pipReleaseCapture.Call()
			sendMessage(hwnd, 0x00A1, 2, 0) // WM_NCLBUTTONDOWN / HTCAPTION
		}
		return 0
	case wmLButtonUp, 0x00A2: // WM_NCLBUTTONUP
		wasDragging := a.pip.dragging
		a.pip.dragging = false
		if !wasDragging && a.pip.target.hwnd != 0 && isWindow(a.pip.target.hwnd) {
			procShowWindow.Call(a.pip.target.hwnd, 9) // SW_RESTORE
			procSetForegroundWindow.Call(a.pip.target.hwnd)
		}
		return 0
	case wmClose:
		a.stopPIP()
		return 0
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		a.paintPIP(hwnd)
		return 0
	}
	return defaultWindowProc(hwnd, message, wparam, lparam)
}

type pipBitmapInfo struct {
	size                         uint32
	width, height                int32
	planes, bitCount             uint16
	compression, sizeImage       uint32
	xPelsPerMeter, yPelsPerMeter int32
	clrUsed, clrImportant        uint32
}

func (a *application) paintPIP(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var bounds rect
	getClientRect(hwnd, &bounds)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&bounds)), a.bgBrush)
	// The reference PiP has no title bar or controls: image plus a subtle frame.
	roundBox(hdc, bounds, colorBG, colorBorder, a.pip.s(3))
	frame := a.pip.frame
	if len(frame.pixels) != 0 {
		info := pipBitmapInfo{size: uint32(unsafe.Sizeof(pipBitmapInfo{})), width: int32(frame.width), height: -int32(frame.height), planes: 1, bitCount: 32}
		x, y := int32(1), int32(1)
		width, height := bounds.right-2, bounds.bottom-2
		pipStretchDIBits.Call(hdc, uintptr(x), uintptr(y), uintptr(width), uintptr(height),
			0, 0, uintptr(frame.width), uintptr(frame.height), uintptr(unsafe.Pointer(&frame.pixels[0])), uintptr(unsafe.Pointer(&info)), 0, 0x00CC0020)
	}
}
