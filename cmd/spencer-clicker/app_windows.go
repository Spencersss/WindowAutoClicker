package main

import (
	"fmt"
	"strconv"
	"strings"
	"unsafe"
)

const (
	idProcess = 1001 + iota
	idPicker
	idClickPoint
	idInterval
	idHold
	idHotkey
	idToggle
	idPIP
	idPIPSize
	idPIPFPS
)

const healthTimerID = 0x7FFFFFFF

const targetComboItemHeight int32 = 32
const targetControlHeight = targetComboItemHeight + 2

var activeApp *application

type application struct {
	scroll                                                                                             int32
	layingOut                                                                                          bool
	instance, hwnd, icon                                                                               uintptr
	tray                                                                                               trayIcon
	taskbarCreated                                                                                     uint32
	pid                                                                                                uint32
	dpi                                                                                                int32
	font, smallFont, titleFont                                                                         uintptr
	bgBrush, fieldBrush                                                                                uintptr
	processCombo, pickerButton, clickPointButton, intervalEdit, holdButton, hotkeyButton, toggleButton uintptr
	keyboardHook, mouseHook                                                                            uintptr
	keysDown                                                                                           [256]bool
	mouseDown                                                                                          [5]bool
	hotkey                                                                                             hotkey
	hold, capture, picker, pointPicker, pickerConsumed                                                 bool
	pickerHover, pickerHighlighted, pickerOriginalBorder                                               uintptr
	pickerOriginalBorderValid                                                                          bool
	pickerPreview                                                                                      targetWindow
	clickPreviewOverlay                                                                                uintptr
	clickPreviewShown                                                                                  bool
	clickPreviewPoint                                                                                  point
	clickPreviewMousePoint                                                                             point
	clickPreviewMovePosted                                                                             bool
	status                                                                                             string
	statusError                                                                                        bool
	targets                                                                                            []targetWindow
	selected                                                                                           targetWindow
	driver                                                                                             nativeDriver
	clicker                                                                                            clicker
	pip                                                                                                pictureInPicture
	pipButton, pipSizeCombo, pipFPSCombo                                                               uintptr
}

func newApplication() *application {
	a := &application{dpi: 96, hotkey: hotkey{keyboardHotkey, vkF9}, status: "Select a window to get started."}
	if procGetDpiForSystem.Find() == nil {
		if value, _, _ := procGetDpiForSystem.Call(); value != 0 {
			a.dpi = int32(value)
		}
	}
	a.clicker.driver = &a.driver
	a.pip.options = captureOptions{width: 320, height: 180, fps: 5}
	return a
}

func (a *application) s(value int32) int32 { return value * a.dpi / 96 }

func (a *application) run() error {
	activeApp = a
	a.instance, _, _ = procGetModuleHandle.Call(0)
	pid, _, _ := procGetCurrentProcessID.Call()
	a.pid = uint32(pid)
	a.bgBrush, _, _ = procCreateSolidBrush.Call(colorBG)
	a.fieldBrush, _, _ = procCreateSolidBrush.Call(colorField)
	a.makeFonts()
	a.icon = a.loadAppIcon()
	defer a.cleanup()
	if err := a.registerClass(mainClassName, mainCallback, a.bgBrush); err != nil {
		return err
	}
	if err := a.registerClass(clickPreviewClassName, clickPreviewCallback, 0); err != nil {
		return err
	}

	style := uint32(wsCaption | wsSysMenu | wsMinimizeBox | wsThickFrame | wsMaximizeBox | wsClipChildren | wsVScroll)
	bounds := rect{right: a.s(520), bottom: a.s(740)}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&bounds)), uintptr(style), 0, 0)
	width, height := bounds.right-bounds.left, bounds.bottom-bounds.top
	var work rect
	procSystemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&work)), 0)
	height = min(height, work.bottom-work.top)
	a.hwnd = createWindow(0, mainClassName, appName, style,
		work.left+(work.right-work.left-width)/2, work.top+(work.bottom-work.top-height)/2,
		width, height, 0, 0, a.instance)
	if a.hwnd == 0 {
		return fmt.Errorf("Could not create the application window.")
	}
	a.driver.owner = a.hwnd
	a.refreshTargets()
	if err := a.createTray(); err != nil {
		return err
	}
	if err := a.installHooks(); err != nil {
		return err
	}
	a.updateControls()
	procShowWindow.Call(a.hwnd, swShow)
	procUpdateWindow.Call(a.hwnd)

	var message msg
	for {
		result, _, err := procGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 {
			return fmt.Errorf("Windows message loop failed: %v", err)
		}
		if result == 0 {
			return nil
		}
		// Keep Enter/Space/Tab hotkeys from also activating a focused control.
		// The global hook has already queued their action; other apps still
		// receive those keys normally. Capture is similarly handled by the hook.
		keyboard := message.message >= wmKeyDown && message.message <= wmSysKeyUp
		if keyboard && (a.capture || (a.hotkey.kind == keyboardHotkey && uint32(message.wParam) == a.hotkey.code)) {
			continue
		}
		dialog, _, _ := procIsDialogMessage.Call(a.hwnd, uintptr(unsafe.Pointer(&message)))
		if dialog == 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
		}
		if keyboard {
			a.revealFocusedControl()
		}
	}
}

func (a *application) cleanup() {
	a.stopPIP()
	a.waitPIPShutdown()
	a.uninstallHooks()
	_ = a.clicker.stop()
	a.tray.remove()
	if a.clickPreviewOverlay != 0 && isWindow(a.clickPreviewOverlay) {
		procDestroyWindow.Call(a.clickPreviewOverlay)
	}
	if a.hwnd != 0 && isWindow(a.hwnd) {
		procDestroyWindow.Call(a.hwnd)
	}
	for _, object := range []uintptr{a.font, a.smallFont, a.titleFont, a.bgBrush, a.fieldBrush} {
		if object != 0 {
			procDeleteObject.Call(object)
		}
	}
	if a.icon != 0 {
		procDestroyIcon.Call(a.icon)
	}
	if a.tray.idleIcon != 0 {
		procDestroyIcon.Call(a.tray.idleIcon)
	}
	if a.tray.activeIcon != 0 {
		procDestroyIcon.Call(a.tray.activeIcon)
	}
	activeApp = nil
}

func (a *application) registerClass(name string, callback, brush uintptr) error {
	cursor, _, _ := procLoadCursor.Call(0, 32512)
	class := wndClassEx{
		cbSize: uint32(unsafe.Sizeof(wndClassEx{})), style: csHRedraw | csVRedraw,
		lpfnWndProc: callback, hInstance: a.instance, hIcon: a.icon,
		hCursor: cursor, hbrBackground: brush, lpszClassName: utf16Ptr(name), hIconSm: a.icon,
	}
	ok, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
	if ok == 0 {
		return fmt.Errorf("Could not register %s: %v", name, err)
	}
	return nil
}

func (a *application) createControls(hwnd uintptr) {
	a.hwnd = hwnd
	create := func(class, text string, style uint32, id uintptr) uintptr {
		return createWindow(0, class, text, wsChild|wsVisible|wsTabStop|style, 0, 0, 1, 1, hwnd, id, a.instance)
	}
	a.processCombo = create("COMBOBOX", "Target window", wsVScroll|cbsDropdownList|cbsOwnerDrawFixed|cbsHasStrings, idProcess)
	a.pickerButton = create("BUTTON", "Pick target", bsOwnerDraw, idPicker)
	a.clickPointButton = create("BUTTON", "Choose Click", bsOwnerDraw, idClickPoint)
	a.intervalEdit = create("EDIT", "50", esNumber|esAutoHScroll, idInterval)
	sendMessage(a.intervalEdit, emSetLimitText, 10, 0)
	a.holdButton = create("BUTTON", "Hold left click: Off", bsOwnerDraw, idHold)
	a.hotkeyButton = create("BUTTON", "Hotkey: F9", bsOwnerDraw, idHotkey)
	a.toggleButton = create("BUTTON", "Start clicker [F9]", bsOwnerDraw, idToggle)
	a.pipButton = create("BUTTON", "Picture-in-picture: Off", bsOwnerDraw, idPIP)
	a.pipSizeCombo = create("COMBOBOX", "Preview resolution", cbsDropdownList|cbsOwnerDrawFixed|cbsHasStrings, idPIPSize)
	a.pipFPSCombo = create("COMBOBOX", "Preview refresh rate", cbsDropdownList|cbsOwnerDrawFixed|cbsHasStrings, idPIPFPS)
	for _, size := range pipSizes {
		sendMessage(a.pipSizeCombo, cbAddString, 0, uintptr(unsafe.Pointer(utf16Ptr(size.label))))
	}
	for _, fps := range pipFrameRates {
		sendMessage(a.pipFPSCombo, cbAddString, 0, uintptr(unsafe.Pointer(utf16Ptr(fmt.Sprintf("%d FPS", fps)))))
	}
	sendMessage(a.pipSizeCombo, cbSetCurSel, 1, 0)
	sendMessage(a.pipFPSCombo, cbSetCurSel, 2, 0)
	a.applyFonts()
	a.layout()
	dark := int32(1)
	procDwmSetWindowAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
	caption := uint32(colorBG)
	procDwmSetWindowAttribute.Call(hwnd, 35, uintptr(unsafe.Pointer(&caption)), unsafe.Sizeof(caption))
}

func (a *application) layout() {
	if a.toggleButton == 0 || a.layingOut {
		return
	}
	a.layingOut = true
	defer func() { a.layingOut = false }()
	var bounds rect
	getClientRect(a.hwnd, &bounds)
	page := bounds.bottom * 96 / a.dpi
	a.scroll = clampScroll(a.scroll, page)
	info := scrollInfo{size: uint32(unsafe.Sizeof(scrollInfo{})), mask: 1 | 2 | 4, max: mainContentHeight - 1, page: uint32(max(1, page)), pos: a.scroll}
	procSetScrollInfo.Call(a.hwnd, 1, uintptr(unsafe.Pointer(&info)), 1)
	getClientRect(a.hwnd, &bounds)
	w, h := bounds.right*96/a.dpi, max(mainContentHeight, page)
	move := func(hwnd uintptr, x, y, width, height int32) {
		procMoveWindow.Call(hwnd, uintptr(a.s(x)), uintptr(a.s(y-a.scroll)), uintptr(a.s(width)), uintptr(a.s(height)), 1)
	}
	move(a.processCombo, 28, 134, w-106, 300)
	move(a.pickerButton, w-70, 134, 42, targetControlHeight)
	move(a.clickPointButton, w-184, 177, 156, 36)
	move(a.intervalEdit, w-174, 267, 112, 26)
	move(a.holdButton, w-158, 329, 130, 42)
	move(a.hotkeyButton, w-218, 405, 190, 42)
	move(a.pipButton, w-158, 485, 130, 42)
	move(a.pipSizeCombo, 28, 575, 230, 220)
	move(a.pipFPSCombo, w-198, 575, 170, 240)
	move(a.toggleButton, 28, h-100, w-56, 48)
	a.hideClickPointPreview()
	procInvalidateRect.Call(a.hwnd, 0, 1)
}

func (a *application) setStatus(text string, isError bool) {
	a.status, a.statusError = text, isError
	procInvalidateRect.Call(a.hwnd, 0, 0)
}

func (a *application) updateControls() {
	if a.keyboardHook != 0 {
		if err := a.syncMouseHook(); err != nil {
			a.capture = false
			a.picker = false
			a.pointPicker = false
			a.pickerConsumed = false
			a.hideClickPointPreview()
			a.setStatus(err.Error(), true)
		}
	}
	running := a.clicker.running || a.clicker.pressed
	if running {
		if timer, _, _ := procSetTimer.Call(a.hwnd, healthTimerID, 500, 0); timer == 0 {
			_ = a.clicker.stop()
			running = a.clicker.pressed
			a.setStatus("Could not monitor the target window.", true)
		}
	} else {
		procKillTimer.Call(a.hwnd, healthTimerID)
	}
	for _, hwnd := range []uintptr{a.processCombo, a.pickerButton, a.intervalEdit, a.holdButton, a.hotkeyButton} {
		enabled := uintptr(1)
		if running {
			enabled = 0
		}
		procEnableWindow.Call(hwnd, enabled)
	}
	clickPointEnabled := uintptr(1)
	if running || a.selected.hwnd == 0 {
		clickPointEnabled = 0
	}
	procEnableWindow.Call(a.clickPointButton, clickPointEnabled)
	hold := "Off"
	if a.hold {
		hold = "On"
	}
	setWindowText(a.holdButton, "Hold left click: "+hold)
	key := "Hotkey: " + a.hotkey.name()
	if a.capture {
		key = "Press a key..."
	}
	setWindowText(a.hotkeyButton, key)
	if a.picker {
		setWindowText(a.pickerButton, "Cancel target picker")
	} else {
		setWindowText(a.pickerButton, "Pick target")
	}
	if a.pointPicker {
		setWindowText(a.clickPointButton, "Click target point")
	} else {
		setWindowText(a.clickPointButton, "Choose Click")
	}
	action := "Start clicker"
	if running {
		action = "Stop clicker"
	}
	setWindowText(a.toggleButton, action+" ["+a.hotkey.name()+"]")
	for _, hwnd := range []uintptr{a.pickerButton, a.clickPointButton, a.holdButton, a.hotkeyButton, a.toggleButton, a.hwnd} {
		if hwnd != 0 {
			procInvalidateRect.Call(hwnd, 0, 1)
		}
	}
	a.updateTray()
}

func parseInterval(text string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return minIntervalMS, nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("Enter a whole number of milliseconds.")
	}
	if value < minIntervalMS {
		return minIntervalMS, nil
	}
	if value > maxIntervalMS {
		return maxIntervalMS, nil
	}
	return int(value), nil
}

func (a *application) toggle() {
	a.capture = false
	if a.picker || a.pickerHighlighted != 0 {
		a.clearPickerPreview()
	}
	a.picker = false
	a.pointPicker = false
	a.hideClickPointPreview()
	defer a.updateControls()
	if a.clicker.running || a.clicker.pressed {
		if err := a.clicker.stop(); err != nil {
			a.setStatus(err.Error(), true)
			return
		}
		a.setStatus("Stopped. "+a.hotkey.name()+" to start again.", false)
		return
	}
	if a.selected.hwnd == 0 {
		a.setStatus("Select a target window first.", true)
		return
	}
	interval, err := parseInterval(windowText(a.intervalEdit))
	if err != nil {
		a.setStatus(err.Error(), true)
		procSetFocus.Call(a.intervalEdit)
		return
	}
	setWindowText(a.intervalEdit, strconv.Itoa(interval))
	a.driver.target = a.selected
	clickPoint, _, hasPoint := targetClickPoint(a.selected)
	if !a.driver.valid() || !hasPoint {
		a.setStatus("Target closed. Select another window.", true)
		return
	}
	configureDriverClickPoint(&a.driver, a.selected, clickPoint)
	if err := a.clicker.start(interval, a.hold); err != nil {
		a.setStatus(err.Error(), true)
		return
	}
	mode := "Clicking"
	if a.hold {
		mode = "Holding"
	}
	a.setStatus(mode+" in background. "+a.hotkey.name()+" to stop.", false)
}

func (a *application) handleCommand(id, notification uint16) {
	switch id {
	case idProcess:
		if notification == cbnDropdown {
			a.refreshTargets()
		}
		if notification == cbnSelChange {
			index := int32(sendMessage(a.processCombo, cbGetCurSel, 0, 0))
			if index >= 0 && int(index) < len(a.targets) {
				if a.pointPicker {
					a.cancelClickPicker()
				}
				if a.picker {
					a.clearPickerPreview()
				}
				a.picker = false
				a.selected = a.targets[index]
				a.setStatus("Ready. "+a.hotkey.name()+" to start clicking.", false)
				a.restartPIP()
				a.updateControls()
			}
		}
	case idPicker:
		if notification == bnClicked && !a.clicker.running {
			a.togglePicker()
		}
	case idClickPoint:
		if notification == bnClicked && !a.clicker.running {
			a.toggleClickPicker()
		}
	case idHold:
		if notification == bnClicked && !a.clicker.running {
			a.hold = !a.hold
			a.updateControls()
		}
	case idHotkey:
		if notification == bnClicked && !a.clicker.running {
			if a.pointPicker {
				a.cancelClickPicker()
			}
			if a.picker {
				a.clearPickerPreview()
			}
			a.picker = false
			a.capture = !a.capture
			if a.capture {
				a.setStatus("Press a key or mouse button. Click again to cancel.", false)
			} else {
				a.setStatus("Hotkey unchanged.", false)
			}
			a.updateControls()
		}
	case idToggle:
		if notification == bnClicked {
			a.toggle()
		}
	case idPIP:
		if notification == bnClicked {
			if a.pip.enabled {
				a.stopPIP()
			} else {
				a.enablePIP()
			}
		}
	case idPIPSize, idPIPFPS:
		if notification == cbnSelChange {
			a.changePIPOptions()
		}
	}
}

func mainWindowProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	a := activeApp
	if a == nil {
		return defaultWindowProc(hwnd, message, wparam, lparam)
	}
	if a.taskbarCreated != 0 && message == a.taskbarCreated {
		// Explorer has discarded every tray icon. Re-add the current state.
		a.tray.registered = false
		if !a.tray.add(a.trayData()) {
			a.showWindow()
			a.setStatus("Tray unavailable. Minimize will keep the taskbar button.", true)
		}
		return 0
	}
	switch message {
	case wmCreate:
		a.createControls(hwnd)
		return 0
	case wmSize:
		a.hideClickPointPreview()
		if wparam == 1 && a.tray.registered {
			procShowWindow.Call(hwnd, swHide)
			return 0
		}
		a.layout()
		return 0
	case wmAppTray:
		a.handleTrayEvent(wparam, lparam)
		return 0
	case wmActivate:
		if loword(wparam) == 0 {
			a.hideClickPointPreview()
		}
	case wmGetMinMaxInfo:
		info := (*minMaxInfo)(unsafe.Pointer(lparam))
		info.minTrackSize = point{a.s(536), a.s(360)}
		return 0
	case 0x0115: // WM_VSCROLL
		a.handleScroll(loword(wparam))
		return 0
	case 0x020A: // WM_MOUSEWHEEL
		a.scrollBy(-int32(int16(hiword(wparam))) * 48 / 120)
		return 0
	case wmCommand:
		a.handleCommand(loword(wparam), hiword(wparam))
		return 0
	case wmAppInput:
		next := hotkey{hotkeyKind(wparam), uint32(lparam)}
		if a.picker || a.pointPicker {
			if next.kind == keyboardHotkey && next.code == vkEscape {
				if a.pointPicker {
					a.cancelClickPicker()
				} else {
					a.cancelPicker()
				}
			}
			return 0
		}
		if a.capture {
			a.hotkey, a.capture = next, false
			a.setStatus("Hotkey set to "+next.name()+".", false)
			a.updateControls()
		} else if next == a.hotkey {
			a.toggle()
		}
		return 0
	case wmAppPickTarget:
		a.selectPickedTarget(wparam, unpackPoint(lparam))
		return 0
	case wmAppPickPoint:
		a.selectPickedClickPoint(wparam, unpackPoint(lparam))
		return 0
	case wmAppPickReleased:
		a.updateControls()
		return 0
	case wmAppClickPreviewMove:
		a.clickPreviewMovePosted = false
		a.updateClickPointPreview(a.clickPreviewMousePoint)
		return 0
	case wmAppPickHover:
		a.updatePickerPreview(wparam)
		return 0
	case wmTimer:
		if wparam == pipTimerID {
			a.tickPIP()
			return 0
		}
		if wparam == healthTimerID {
			if (a.clicker.running || a.clicker.pressed) && !a.driver.valid() {
				_ = a.clicker.stop()
				a.setStatus("Target closed. Select another window.", true)
				a.updateControls()
			}
			return 0
		}
		if wparam == a.driver.timerID {
			if err := a.clicker.tick(); err != nil {
				a.setStatus(err.Error(), true)
				a.updateControls()
			}
		}
		return 0
	case wmMeasureItem:
		item := (*measureItemStruct)(unsafe.Pointer(lparam))
		item.itemHeight = uint32(a.s(targetControlHeight))
		return 1
	case wmDrawItem:
		a.drawItem((*drawItemStruct)(unsafe.Pointer(lparam)))
		return 1
	case wmCtlColorEdit, wmCtlColorList, wmCtlColorStatic:
		procSetTextColor.Call(wparam, colorText)
		procSetBkColor.Call(wparam, colorField)
		return a.fieldBrush
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		a.paintMain(hwnd)
		return 0
	case wmDPIChanged:
		a.dpi = int32(loword(wparam))
		a.makeFonts()
		a.applyFonts()
		bounds := (*rect)(unsafe.Pointer(lparam))
		procSetWindowPos.Call(hwnd, 0, uintptr(bounds.left), uintptr(bounds.top), uintptr(bounds.right-bounds.left), uintptr(bounds.bottom-bounds.top), 0x0004|swpNoActivate)
		a.layout()
		return 0
	case wmQueryEndSession:
		a.stopPIP()
		_ = a.clicker.stop()
		a.updateControls()
		return 1
	case wmEndSession:
		if wparam != 0 {
			procDestroyWindow.Call(hwnd)
		}
		return 0
	case wmPowerBroadcast:
		if wparam == 4 {
			a.stopPIP()
			_ = a.clicker.stop()
			a.setStatus("Stopped for sleep. Start again when ready.", false)
			a.updateControls()
		}
	case wmClose:
		if err := a.clicker.stop(); err != nil {
			a.showWindow()
			a.setStatus("Release failed. Press Stop to retry before closing.", true)
			a.updateControls()
			return 0
		}
		a.uninstallHooks()
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		a.stopPIP()
		_ = a.clicker.stop()
		a.tray.remove()
		procPostQuitMessage.Call(0)
		return 0
	}
	return defaultWindowProc(hwnd, message, wparam, lparam)
}
