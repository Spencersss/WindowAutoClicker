package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

// Win32 callbacks are allocated once; syscall.NewCallback keeps them for the
// process lifetime, so creating one on every dropdown refresh would leak.
const clickPreviewClassName = "SpencerClickerClickPreview"

var (
	mainCallback         = syscall.NewCallback(mainWindowProc)
	enumCallback         = syscall.NewCallback(enumWindowCallback)
	keyCallback          = syscall.NewCallback(keyboardHookProc)
	mouseCallback        = syscall.NewCallback(mouseHookProc)
	clickPreviewCallback = syscall.NewCallback(clickPreviewWindowProc)
	clickPreviewColorKey = rgb(1, 2, 3)
)

type nativeDriver struct {
	owner      uintptr
	target     targetWindow
	inputHwnd  uintptr
	coords     uintptr
	rootCoords uintptr
	timerID    uintptr
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
	hwnd, coords := d.inputHwnd, d.coords
	if !inputWindowBelongsTo(hwnd, d.target) {
		hwnd, coords = d.target.hwnd, d.rootCoords
	}
	ok, _, err := procPostMessage.Call(hwnd, uintptr(message), flags, coords)
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
		if target.hwnd == a.selected.hwnd && target.pid == a.selected.pid {
			target.inputHwnd = a.selected.inputHwnd
			if a.selected.hasInputPoint {
				target.inputPoint = a.selected.inputPoint
				target.hasInputPoint = true
			}
			a.targets[i] = target
		}
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
		a.pointPicker = false
		a.hideClickPointPreview()
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
	if target, ok := targetWindowFor(hwnd); ok {
		a.targets = append(a.targets, target)
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
	needed := a.capture || a.picker || a.pointPicker || a.pickerConsumed || a.hotkey.kind == mouseHotkey || a.selected.hwnd != 0
	if !needed && a.mouseHook != 0 {
		procUnhookWindowsHookEx.Call(a.mouseHook)
		a.mouseHook = 0
		a.mouseDown = [5]bool{}
	}
	// A selected target also uses mouse movement for the point-preview marker.
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
			if (a.picker || a.pointPicker) && data.vkCode == vkEscape {
				postMessage(a.hwnd, wmAppInput, uintptr(keyboardHotkey), uintptr(data.vkCode))
				return 1
			}
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
		message := uint32(wparam)
		if message == wmMouseMove {
			if a.picker {
				var hovered uintptr
				if target, ok := pickTargetAt(data.pt); ok {
					hovered = target
				}
				if hovered != a.pickerHover {
					a.pickerHover = hovered
					postMessage(a.hwnd, wmAppPickHover, hovered, 0)
				}
			} else if !a.pointPicker {
				a.queueClickPointPreview(data.pt)
			}
		}
		if a.pointPicker && message == wmLButtonDown {
			if target, inputHwnd, ok := pickInputAt(data.pt); ok && target.hwnd == a.selected.hwnd && target.pid == a.selected.pid {
				if _, valid := clickPointInTarget(a.selected, inputHwnd, data.pt); valid {
					a.pickerConsumed = true
					postMessage(a.hwnd, wmAppPickPoint, inputHwnd, packPoint(data.pt))
					return 1
				}
			}
		}
		if a.picker && message == wmLButtonDown {
			if _, inputHwnd, ok := pickInputAt(data.pt); ok {
				a.pickerConsumed = true
				postMessage(a.hwnd, wmAppPickTarget, inputHwnd, packPoint(data.pt))
				return 1
			}
		}
		if message == wmLButtonUp && a.pickerConsumed {
			a.pickerConsumed = false
			postMessage(a.hwnd, wmAppPickReleased, 0, 0)
			return 1
		}
		button, down, up := mouseEvent(message, data.mouseData)
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
func windowFromPoint(screen point) uintptr {
	packed := uintptr(uint32(screen.x)) | uintptr(uint64(uint32(screen.y))<<32)
	hwnd, _, _ := procWindowFromPoint.Call(packed)
	return hwnd
}

func packPoint(p point) uintptr {
	return uintptr(uint64(uint32(p.x)) | uint64(uint32(p.y))<<32)
}

func unpackPoint(packed uintptr) point {
	return point{x: int32(uint32(packed)), y: int32(uint32(uint64(packed) >> 32))}
}

func mouseCoords(p point) uintptr {
	return uintptr(uint32(uint16(p.x)) | uint32(uint16(p.y))<<16)
}

func isChildWindow(parent, child uintptr) bool {
	result, _, _ := procIsChild.Call(parent, child)
	return result != 0
}

func pointInClient(p point, bounds rect) bool {
	return p.x >= bounds.left && p.x < bounds.right && p.y >= bounds.top && p.y < bounds.bottom
}

func targetWindowFor(hwnd uintptr) (targetWindow, bool) {
	a := activeApp
	if a == nil || hwnd == 0 {
		return targetWindow{}, false
	}
	visible, _, _ := procIsWindowVisible.Call(hwnd)
	pid := windowPID(hwnd)
	owner, _, _ := procGetWindow.Call(hwnd, gwOwner)
	index := int32(gwlExStyle)
	style, _, _ := procGetWindowLongPtr.Call(hwnd, uintptr(index))
	title := strings.TrimSpace(windowText(hwnd))
	if visible == 0 || pid == 0 || pid == a.pid || owner != 0 || style&wsExToolWindow != 0 || title == "" {
		return targetWindow{}, false
	}
	return targetWindow{hwnd: hwnd, pid: pid, title: title, identity: windowIdentityFor(hwnd, pid, title)}, true
}

func windowIdentityFor(hwnd uintptr, pid uint32, title string) windowIdentity {
	identity := windowIdentity{WindowClass: windowClassName(hwnd), Title: title}
	process, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if process == 0 {
		return identity
	}
	defer procCloseHandle.Call(process)

	buffer := make([]uint16, 32768)
	length := uint32(len(buffer))
	ok, _, _ := procQueryFullProcessImageName.Call(process, 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&length)))
	if ok != 0 && length > 0 {
		identity.ExecutablePath = normalizeExecutablePath(syscall.UTF16ToString(buffer[:length]))
	}
	return identity
}

func windowClassName(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	buffer := make([]uint16, 256)
	length, _, _ := procGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:length])
}

func pickTargetAt(screen point) (uintptr, bool) {
	hwnd := windowFromPoint(screen)
	if hwnd == 0 {
		return 0, false
	}
	root, _, _ := procGetAncestor.Call(hwnd, gaRoot)
	if root == 0 {
		return 0, false
	}
	if target, ok := targetWindowFor(root); ok {
		return target.hwnd, true
	}
	return 0, false
}

// pickInputAt keeps the child HWND hit by the user while retaining the root
// window for target identity, lifetime checks, and picture-in-picture capture.
func pickInputAt(screen point) (targetWindow, uintptr, bool) {
	hwnd := windowFromPoint(screen)
	if hwnd == 0 {
		return targetWindow{}, 0, false
	}
	root, _, _ := procGetAncestor.Call(hwnd, gaRoot)
	if root == 0 {
		return targetWindow{}, 0, false
	}
	target, ok := targetWindowFor(root)
	if !ok || windowPID(hwnd) != target.pid || (hwnd != root && !isChildWindow(root, hwnd)) {
		return targetWindow{}, 0, false
	}

	// WindowFromPoint may return the root when a child reports HTTRANSPARENT.
	// Descend through visible, enabled children at the same screen point.
	inputHwnd := root
	for depth := 0; depth < 32; depth++ {
		clientPoint := screen
		if converted, _, _ := procScreenToClient.Call(inputHwnd, uintptr(unsafe.Pointer(&clientPoint))); converted == 0 {
			break
		}
		child, _, _ := procChildWindowFromPointEx.Call(inputHwnd, packPoint(clientPoint), cwpSkipDisabled|cwpSkipInvisible)
		if child == 0 || child == inputHwnd || windowPID(child) != target.pid || !isChildWindow(root, child) {
			break
		}
		inputHwnd = child
	}
	return target, inputHwnd, true
}

func inputWindowBelongsTo(hwnd uintptr, target targetWindow) bool {
	if hwnd == 0 || target.hwnd == 0 || !isWindow(hwnd) || windowPID(hwnd) != target.pid {
		return false
	}
	if hwnd == target.hwnd {
		return true
	}
	root, _, _ := procGetAncestor.Call(hwnd, gaRoot)
	return root == target.hwnd && isChildWindow(target.hwnd, hwnd)
}

func targetClickPoint(target targetWindow) (point, bool, bool) {
	if target.hwnd == 0 || !isWindow(target.hwnd) || windowPID(target.hwnd) != target.pid {
		return point{}, false, false
	}
	var bounds rect
	if !getClientRect(target.hwnd, &bounds) {
		return point{}, false, false
	}
	clickPoint := point{x: bounds.right / 2, y: bounds.bottom / 2}
	centered := true
	if target.hasInputPoint && pointInClient(target.inputPoint, bounds) {
		clickPoint = target.inputPoint
		centered = false
	}
	return clickPoint, centered, true
}

func clickPointInTarget(target targetWindow, inputHwnd uintptr, screenPoint point) (point, bool) {
	if !inputWindowBelongsTo(inputHwnd, target) {
		return point{}, false
	}
	root, _, _ := procGetAncestor.Call(inputHwnd, gaRoot)
	if root != target.hwnd {
		return point{}, false
	}
	rootPoint := screenPoint
	if converted, _, _ := procScreenToClient.Call(root, uintptr(unsafe.Pointer(&rootPoint))); converted == 0 {
		return point{}, false
	}
	var bounds rect
	if !getClientRect(root, &bounds) || !pointInClient(rootPoint, bounds) {
		return point{}, false
	}
	return rootPoint, true
}

func configureDriverClickPoint(driver *nativeDriver, target targetWindow, clickPoint point) {
	driver.target = target
	driver.rootCoords = mouseCoords(clickPoint)
	driver.inputHwnd = 0
	driver.coords = driver.rootCoords
	inputHwnd := target.inputHwnd
	if !inputWindowBelongsTo(inputHwnd, target) || inputHwnd == target.hwnd {
		return
	}
	screenPoint := clickPoint
	if converted, _, _ := procClientToScreen.Call(target.hwnd, uintptr(unsafe.Pointer(&screenPoint))); converted == 0 {
		return
	}
	if converted, _, _ := procScreenToClient.Call(inputHwnd, uintptr(unsafe.Pointer(&screenPoint))); converted == 0 {
		return
	}
	var bounds rect
	if !getClientRect(inputHwnd, &bounds) || !pointInClient(screenPoint, bounds) {
		return
	}
	driver.inputHwnd = inputHwnd
	driver.coords = mouseCoords(screenPoint)
}

func focusTargetForPicker(target targetWindow) error {
	if target.hwnd == 0 || !isWindow(target.hwnd) || windowPID(target.hwnd) != target.pid {
		return fmt.Errorf("Target closed. Select another window.")
	}
	if iconic, _, _ := procIsIconic.Call(target.hwnd); iconic != 0 {
		procShowWindow.Call(target.hwnd, swRestore)
	}
	procSetForegroundWindow.Call(target.hwnd)
	foreground, _, _ := procGetForegroundWindow.Call()
	owner, _, _ := procGetAncestor.Call(foreground, gaRootOwner)
	if foreground != target.hwnd && owner != target.hwnd {
		return fmt.Errorf("Windows could not focus the selected application. Click its window, then choose a click point.")
	}
	return nil
}

func (a *application) clickPointDescription() string {
	if a.selected.hwnd == 0 {
		return "Select an application to set its click point."
	}
	p, centered, ok := targetClickPoint(a.selected)
	if !ok {
		return "The selected application is unavailable."
	}
	kind := "Selected"
	if centered {
		kind = "Center"
	}
	return fmt.Sprintf("%s  (x=%d, y=%d)", kind, p.x, p.y)
}

func (a *application) queueClickPointPreview(screen point) {
	if a.selected.hwnd == 0 || a.clickPointButton == 0 || a.picker || a.pointPicker || a.clicker.running {
		return
	}
	a.clickPreviewMousePoint = screen
	if a.clickPreviewMovePosted {
		return
	}
	a.clickPreviewMovePosted = true
	if !postMessage(a.hwnd, wmAppClickPreviewMove, 0, 0) {
		a.clickPreviewMovePosted = false
	}
}
func (a *application) updateClickPointPreview(screen point) {
	if a.picker || a.pointPicker || a.clicker.running || a.selected.hwnd == 0 || a.clickPointButton == 0 {
		a.hideClickPointPreview()
		return
	}
	visible, _, _ := procIsWindowVisible.Call(a.hwnd)
	iconic, _, _ := procIsIconic.Call(a.hwnd)
	foreground, _, _ := procGetForegroundWindow.Call()
	foregroundOwner, _, _ := procGetAncestor.Call(foreground, gaRootOwner)
	if visible == 0 || iconic != 0 || (foreground != a.hwnd && foregroundOwner != a.hwnd) || windowFromPoint(screen) != a.clickPointButton {
		a.hideClickPointPreview()
		return
	}
	var buttonBounds rect
	if result, _, _ := procGetWindowRect.Call(a.clickPointButton, uintptr(unsafe.Pointer(&buttonBounds))); result == 0 || !pointInClient(screen, buttonBounds) {
		a.hideClickPointPreview()
		return
	}
	clickPoint, _, ok := targetClickPoint(a.selected)
	if !ok {
		a.hideClickPointPreview()
		return
	}
	if converted, _, _ := procClientToScreen.Call(a.selected.hwnd, uintptr(unsafe.Pointer(&clickPoint))); converted == 0 {
		a.hideClickPointPreview()
		return
	}
	if a.clickPreviewOverlay == 0 {
		a.clickPreviewOverlay = createWindow(wsExTopmost|wsExTransparent|wsExNoActivate|wsExToolWindow|wsExLayered,
			clickPreviewClassName, "", wsPopup, 0, 0, 30, 30, 0, 0, a.instance)
		if a.clickPreviewOverlay == 0 {
			return
		}
		procSetLayeredWindowAttributes.Call(a.clickPreviewOverlay, clickPreviewColorKey, 0, lwaColorKey)
	}
	if a.clickPreviewShown && a.clickPreviewPoint == clickPoint {
		return
	}
	if result, _, _ := procSetWindowPos.Call(a.clickPreviewOverlay, hwndTopmost, uintptr(clickPoint.x-15), uintptr(clickPoint.y-15), 30, 30, swpShowWindow|swpNoActivate); result != 0 {
		a.clickPreviewShown = true
		a.clickPreviewPoint = clickPoint
	}
}

func (a *application) hideClickPointPreview() {
	if a.clickPreviewOverlay != 0 && a.clickPreviewShown {
		procShowWindow.Call(a.clickPreviewOverlay, 0)
		a.clickPreviewShown = false
	}
}

func clickPreviewWindowProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	switch message {
	case wmNCHitTest:
		return htTransparent
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		bounds := rect{right: 30, bottom: 30}
		fill(hdc, bounds, clickPreviewColorKey)
		circle(hdc, rect{left: 2, top: 2, right: 28, bottom: 28}, clickPreviewColorKey, colorRed)
		pen, _, _ := procCreatePen.Call(0, 2, colorRed)
		old, _, _ := procSelectObject.Call(hdc, pen)
		procMoveToEx.Call(hdc, 15, 0, 0)
		procLineTo.Call(hdc, 15, 8)
		procMoveToEx.Call(hdc, 15, 21, 0)
		procLineTo.Call(hdc, 15, 30)
		procMoveToEx.Call(hdc, 0, 15, 0)
		procLineTo.Call(hdc, 8, 15)
		procMoveToEx.Call(hdc, 21, 15, 0)
		procLineTo.Call(hdc, 30, 15)
		procSelectObject.Call(hdc, old)
		procDeleteObject.Call(pen)
		circle(hdc, rect{left: 11, top: 11, right: 19, bottom: 19}, colorGreen, colorBG)
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	}
	return defaultWindowProc(hwnd, message, wparam, lparam)
}
func (a *application) targetIndex(target targetWindow) int {
	for i, candidate := range a.targets {
		if candidate.hwnd == target.hwnd && candidate.pid == target.pid {
			return i
		}
	}
	return -1
}

func (a *application) selectComboTarget(target targetWindow) {
	index := a.targetIndex(target)
	selection := ^uintptr(0)
	if index >= 0 {
		selection = uintptr(index)
	}
	sendMessage(a.processCombo, cbSetCurSel, selection, 0)
}

func (a *application) clearPickerHighlight() {
	if a.pickerHighlighted != 0 {
		color := uint32(dwmColorDefault)
		if a.pickerOriginalBorderValid {
			color = uint32(a.pickerOriginalBorder)
		}
		procDwmSetWindowAttribute.Call(a.pickerHighlighted, dwmwaBorderColor, uintptr(unsafe.Pointer(&color)), unsafe.Sizeof(color))
	}
	a.pickerHighlighted = 0
	a.pickerOriginalBorder = 0
	a.pickerOriginalBorderValid = false
}

func (a *application) updatePickerHighlight(hwnd uintptr) {
	if a.pickerHighlighted == hwnd {
		return
	}
	a.clearPickerHighlight()
	if hwnd == 0 {
		return
	}
	var original uint32
	if result, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaBorderColor, uintptr(unsafe.Pointer(&original)), unsafe.Sizeof(original)); result == 0 {
		a.pickerOriginalBorder = uintptr(original)
		a.pickerOriginalBorderValid = true
	}
	color := uint32(rgb(64, 139, 255))
	procDwmSetWindowAttribute.Call(hwnd, dwmwaBorderColor, uintptr(unsafe.Pointer(&color)), unsafe.Sizeof(color))
	a.pickerHighlighted = hwnd
}

func (a *application) clearPickerPreview() {
	a.clearPickerHighlight()
	a.pickerHover = 0
	a.pickerPreview = targetWindow{}
	a.selectComboTarget(a.selected)
	if a.processCombo != 0 {
		procInvalidateRect.Call(a.processCombo, 0, 1)
	}
}

func (a *application) updatePickerPreview(hwnd uintptr) {
	if !a.picker {
		return
	}
	target, ok := targetWindowFor(hwnd)
	if ok {
		index := a.targetIndex(target)
		if index < 0 {
			a.refreshTargets()
			index = a.targetIndex(target)
		}
		if index >= 0 {
			a.pickerPreview = target
			sendMessage(a.processCombo, cbSetCurSel, uintptr(index), 0)
			a.updatePickerHighlight(target.hwnd)
			if a.processCombo != 0 {
				procInvalidateRect.Call(a.processCombo, 0, 1)
			}
			return
		}
	}
	a.pickerPreview = targetWindow{}
	a.selectComboTarget(a.selected)
	a.updatePickerHighlight(0)
	if a.processCombo != 0 {
		procInvalidateRect.Call(a.processCombo, 0, 1)
	}
}
func (a *application) togglePicker() {
	if a.clicker.running || a.clicker.pressed {
		return
	}
	if a.picker {
		a.cancelPicker()
		return
	}
	if a.pointPicker {
		a.cancelClickPicker()
	}
	a.capture = false
	a.hideClickPointPreview()
	a.refreshTargets()
	a.clearPickerPreview()
	a.picker, a.pickerConsumed = true, false
	a.setStatus("Click the target window. Escape or Pick target to cancel.", false)
	a.updateControls()
}

func (a *application) cancelPicker() {
	a.clearPickerPreview()
	a.picker = false
	a.setStatus("Target picker cancelled.", false)
	a.updateControls()
}

func (a *application) toggleClickPicker() {
	if a.clicker.running || a.clicker.pressed {
		return
	}
	if a.pointPicker {
		a.cancelClickPicker()
		return
	}
	if a.selected.hwnd == 0 {
		a.setStatus("Select an application first.", true)
		a.updateControls()
		return
	}
	if a.picker {
		a.clearPickerPreview()
		a.picker = false
	}
	a.capture = false
	a.hideClickPointPreview()
	if err := focusTargetForPicker(a.selected); err != nil {
		a.setStatus(err.Error(), true)
		a.updateControls()
		return
	}
	a.pointPicker, a.pickerConsumed = true, false
	a.setStatus("Click a point inside "+a.selected.title+". Escape or Choose Click to cancel.", false)
	a.updateControls()
}

func (a *application) cancelClickPicker() {
	if !a.pointPicker {
		return
	}
	a.pointPicker = false
	a.hideClickPointPreview()
	a.setStatus("Click-point selection cancelled.", false)
	a.updateControls()
}

func (a *application) selectPickedClickPoint(inputHwnd uintptr, screenPoint point) bool {
	if !a.pointPicker {
		return false
	}
	rootPoint, ok := clickPointInTarget(a.selected, inputHwnd, screenPoint)
	if !ok {
		return false
	}
	a.selected.inputHwnd = inputHwnd
	a.selected.inputPoint = rootPoint
	a.selected.hasInputPoint = true
	for i, candidate := range a.targets {
		if candidate.hwnd == a.selected.hwnd && candidate.pid == a.selected.pid {
			a.targets[i] = a.selected
			break
		}
	}
	a.pointPicker = false
	a.hideClickPointPreview()
	a.setStatus(fmt.Sprintf("Click point saved at x=%d, y=%d in the application client area.", rootPoint.x, rootPoint.y), false)
	a.updateControls()
	return true
}
func (a *application) selectPickedTarget(inputHwnd uintptr, _ point) {
	if !a.picker || inputHwnd == 0 || inputHwnd == a.hwnd || !isWindow(inputHwnd) {
		return
	}
	root, _, _ := procGetAncestor.Call(inputHwnd, gaRoot)
	if root == 0 || root == a.hwnd {
		return
	}
	target, ok := targetWindowFor(root)
	if !ok || windowPID(inputHwnd) != target.pid || (inputHwnd != root && !isChildWindow(root, inputHwnd)) {
		return
	}
	target.inputHwnd = inputHwnd
	a.clearPickerPreview()
	a.selected = target
	a.picker = false
	a.pointPicker = false
	a.hideClickPointPreview()
	a.refreshTargets()
	for i, candidate := range a.targets {
		if candidate.hwnd == target.hwnd && candidate.pid == target.pid {
			sendMessage(a.processCombo, cbSetCurSel, uintptr(i), 0)
			break
		}
	}
	a.setStatus("Ready. "+a.hotkey.name()+" to start clicking.", false)
	a.restartPIP()
	a.updateControls()
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
