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
	idClickerSettings
	idPIPSettings
	idPresetDrawer
	idPresetList
	idPresetName
	idPresetSave
	idPresetLoad
	idPresetOverwrite
	idPresetDelete
)

const healthTimerID = 0x7FFFFFFF

const targetComboItemHeight int32 = 32
const targetControlHeight = targetComboItemHeight + 2
const presetDrawerWidth int32 = 320

const (
	clickerIntervalEditWidth int32 = 190
	clickerIntervalUnitWidth int32 = 26
	clickerIntervalUnitGap   int32 = 8
	clickerIntervalRightGap  int32 = 28
)

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
	clickerSettingsButton, pipSettingsButton                                                           uintptr
	presetDrawerButton, presetCombo, presetNameEdit, presetSaveButton, presetLoadButton                uintptr
	presetOverwriteButton, presetDeleteButton                                                          uintptr
	clickerSettingsExpanded, pipSettingsExpanded                                                       bool
	presetDrawerExpanded                                                                               bool
	presetSelectedName                                                                                 string
	settingsPath                                                                                       string
	settingsLoadError                                                                                  string
	settingsWritable                                                                                   bool
	settings                                                                                           appSettings
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
	lastLayout                                                                                         layoutSnapshot
}

func newApplication() *application {
	a := &application{dpi: 96, hotkey: hotkey{keyboardHotkey, vkF9}, status: "Select a window to get started."}
	if procGetDpiForSystem.Find() == nil {
		if value, _, _ := procGetDpiForSystem.Call(); value != 0 {
			a.dpi = int32(value)
		}
	}
	a.clicker.driver = &a.driver
	a.settings = defaultAppSettings()
	if path, err := settingsFilePath(); err == nil {
		a.settingsPath = path
		if settings, loadErr := loadAppSettings(path); loadErr == nil {
			a.settings = settings
			a.settingsWritable = true
		} else {
			a.settingsLoadError = loadErr.Error()
		}
	} else {
		a.settingsLoadError = err.Error()
	}
	a.settings = normalizeAppSettings(a.settings)
	a.hotkey = hotkey{kind: hotkeyKind(a.settings.Hotkey.Kind), code: a.settings.Hotkey.Code}
	a.hold = a.settings.Hold
	a.pip.options = captureOptions{width: a.settings.PIPWidth, height: a.settings.PIPHeight, fps: a.settings.PIPFPS}
	return a
}

func (a *application) s(value int32) int32 { return value * a.dpi / 96 }

func intervalControlLayout(mainWidth int32) (editX, editWidth, unitX int32) {
	right := max(28, mainWidth-clickerIntervalRightGap)
	unitX = max(28, right-clickerIntervalUnitWidth)
	available := max(0, unitX-clickerIntervalUnitGap-28)
	editWidth = min(clickerIntervalEditWidth, available)
	editX = unitX - clickerIntervalUnitGap - editWidth
	return editX, editWidth, unitX
}

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
	a.restoreStartupTarget()
	if a.settingsLoadError != "" {
		a.setStatus("Settings could not be loaded; the existing file will be preserved. "+a.settingsLoadError, true)
	}
	if err := a.createTray(); err != nil {
		return err
	}
	if err := a.installHooks(); err != nil {
		return err
	}
	a.updateControls()
	a.refreshPresetControls()
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
	_ = a.saveSettings()
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
	a.toggleButton = create("BUTTON", "Start clicker [F9]", bsOwnerDraw, idToggle)
	a.clickerSettingsButton = create("BUTTON", "Clicker settings", bsOwnerDraw, idClickerSettings)
	a.intervalEdit = create("EDIT", strconv.Itoa(a.settings.IntervalMS), esNumber|esAutoHScroll, idInterval)
	sendMessage(a.intervalEdit, emSetLimitText, 10, 0)
	a.holdButton = create("BUTTON", "Hold left click: Off", bsOwnerDraw, idHold)
	a.hotkeyButton = create("BUTTON", "Hotkey: F9", bsOwnerDraw, idHotkey)
	a.pipSettingsButton = create("BUTTON", "Picture-in-picture settings", bsOwnerDraw, idPIPSettings)
	a.pipButton = create("BUTTON", "Picture-in-picture: Off", bsOwnerDraw, idPIP)
	a.pipSizeCombo = create("COMBOBOX", "Preview resolution", cbsDropdownList|cbsOwnerDrawFixed|cbsHasStrings, idPIPSize)
	a.pipFPSCombo = create("COMBOBOX", "Preview refresh rate", cbsDropdownList|cbsOwnerDrawFixed|cbsHasStrings, idPIPFPS)
	a.presetDrawerButton = create("BUTTON", "Saved clicks +", bsOwnerDraw, idPresetDrawer)
	a.presetCombo = create("COMBOBOX", "Choose a saved click...", cbsDropdownList|cbsOwnerDrawFixed|cbsHasStrings, idPresetList)
	a.presetNameEdit = create("EDIT", "", esAutoHScroll, idPresetName)
	a.presetSaveButton = create("BUTTON", "Save new", bsOwnerDraw, idPresetSave)
	a.presetLoadButton = create("BUTTON", "Load", bsOwnerDraw, idPresetLoad)
	a.presetOverwriteButton = create("BUTTON", "Overwrite", bsOwnerDraw, idPresetOverwrite)
	a.presetDeleteButton = create("BUTTON", "Delete", bsOwnerDraw, idPresetDelete)
	sendMessage(a.presetNameEdit, emSetLimitText, 80, 0)
	for _, size := range pipSizes {
		sendMessage(a.pipSizeCombo, cbAddString, 0, uintptr(unsafe.Pointer(utf16Ptr(size.label))))
	}
	for _, fps := range pipFrameRates {
		sendMessage(a.pipFPSCombo, cbAddString, 0, uintptr(unsafe.Pointer(utf16Ptr(fmt.Sprintf("%d FPS", fps)))))
	}
	sendMessage(a.pipSizeCombo, cbSetCurSel, uintptr(pipSizeIndex(a.settings.PIPWidth, a.settings.PIPHeight)), 0)
	sendMessage(a.pipFPSCombo, cbSetCurSel, uintptr(pipFPSIndex(a.settings.PIPFPS)), 0)
	a.updateSettingsHeaders()
	a.applyFonts()
	a.layout()
	dark := int32(1)
	procDwmSetWindowAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
	caption := uint32(colorBG)
	procDwmSetWindowAttribute.Call(hwnd, 35, uintptr(unsafe.Pointer(&caption)), unsafe.Sizeof(caption))
}

const (
	targetControlTop int32 = 134
	chooseClickTop   int32 = 189
)

type childPlacement struct {
	hwnd                uintptr
	x, y, width, height int32
	visible             bool
}

type placementChange struct {
	previous, current childPlacement
	hasPrevious       bool
}

type layoutSnapshot struct {
	children                  []childPlacement
	scroll, contentHeight     int32
	clientWidth, clientHeight int32
	dpi                       int32
	valid                     bool
}

func diffChildPlacements(previous, current []childPlacement) []placementChange {
	previousByWindow := make(map[uintptr]childPlacement, len(previous))
	for _, item := range previous {
		previousByWindow[item.hwnd] = item
	}

	changes := make([]placementChange, 0, len(current))
	for _, item := range current {
		old, found := previousByWindow[item.hwnd]
		if found && old == item {
			continue
		}
		changes = append(changes, placementChange{previous: old, current: item, hasPrevious: found})
	}
	return changes
}

func (a *application) layout() {
	if a.toggleButton == 0 || a.layingOut {
		return
	}
	a.layingOut = true
	defer func() { a.layingOut = false }()
	var bounds rect
	getClientRect(a.hwnd, &bounds)
	page := a.settingsViewportPage()
	contentHeight := a.settingsContentHeight()
	a.scroll = clampScroll(a.scroll, page, contentHeight)
	info := scrollInfo{size: uint32(unsafe.Sizeof(scrollInfo{})), mask: 1 | 2 | 4, max: max(0, contentHeight-1), page: uint32(max(1, page)), pos: a.scroll}
	procSetScrollInfo.Call(a.hwnd, 1, uintptr(unsafe.Pointer(&info)), 1)
	getClientRect(a.hwnd, &bounds)
	w := bounds.right * 96 / a.dpi
	mainW := w
	if a.presetDrawerExpanded {
		mainW = max(28, w-presetDrawerWidth)
	}
	placements := make([]childPlacement, 0, 20)
	place := func(hwnd uintptr, x, y, width, height int32, visible bool) {
		if hwnd != 0 {
			placements = append(placements, childPlacement{hwnd: hwnd, x: x, y: y, width: width, height: height, visible: visible})
		}
	}
	placeSettings := func(hwnd uintptr, x, y, width, height int32, visible bool) {
		screenY := y - a.scroll
		if screenY < settingsTop {
			screenY = -height - 1
		}
		place(hwnd, x, screenY, width, height, visible)
	}

	// The essentials remain anchored to the window while only the settings
	// cards move with the vertical scrollbar.
	place(a.processCombo, 28, targetControlTop, mainW-106, 300, true)
	place(a.pickerButton, mainW-70, targetControlTop, 42, targetControlHeight, true)
	place(a.clickPointButton, 28, chooseClickTop, 156, 36, true)
	place(a.toggleButton, 28, 256, mainW-56, 44, true)
	place(a.presetDrawerButton, mainW-148, 338, 120, 30, true)

	clickerTop := settingsTop
	pipTop := clickerTop + clickerSettingsHeight(a.clickerSettingsExpanded) + settingsSectionGap
	placeSettings(a.clickerSettingsButton, 28, clickerTop, mainW-56, settingsSectionHeaderHeight, true)
	placeSettings(a.pipSettingsButton, 28, pipTop, mainW-56, settingsSectionHeaderHeight, true)
	intervalEditX, intervalEditWidth, _ := intervalControlLayout(mainW)
	placeSettings(a.intervalEdit, intervalEditX, clickerTop+52, intervalEditWidth, 36, a.clickerSettingsExpanded)
	placeSettings(a.holdButton, mainW-158, clickerTop+98, 130, 36, a.clickerSettingsExpanded)
	placeSettings(a.hotkeyButton, mainW-158, clickerTop+144, 130, 36, a.clickerSettingsExpanded)
	placeSettings(a.pipButton, mainW-158, pipTop+52, 130, 36, a.pipSettingsExpanded)
	columnWidth := (mainW - 68) / 2
	placeSettings(a.pipSizeCombo, 28, pipTop+120, columnWidth, 220, a.pipSettingsExpanded)
	placeSettings(a.pipFPSCombo, 40+columnWidth, pipTop+120, columnWidth, 220, a.pipSettingsExpanded)

	drawerX := mainW
	drawerVisible := a.presetDrawerExpanded
	place(a.presetCombo, drawerX+24, 132, presetDrawerWidth-48, 240, drawerVisible)
	place(a.presetNameEdit, drawerX+24, 214, presetDrawerWidth-48, 36, drawerVisible)
	place(a.presetLoadButton, drawerX+24, 268, presetDrawerWidth-48, 36, drawerVisible)
	place(a.presetSaveButton, drawerX+24, 314, presetDrawerWidth-48, 36, drawerVisible)
	place(a.presetOverwriteButton, drawerX+24, 360, (presetDrawerWidth-56)/2, 36, drawerVisible)
	place(a.presetDeleteButton, drawerX+32+(presetDrawerWidth-56)/2, 360, (presetDrawerWidth-56)/2, 36, drawerVisible)

	previousLayout := a.lastLayout
	previousPlacements := previousLayout.children
	if previousLayout.valid && previousLayout.dpi != a.dpi {
		previousPlacements = nil
	}
	a.applyChildPlacements(diffChildPlacements(previousPlacements, placements))
	if !previousLayout.valid || previousLayout.clientWidth != bounds.right || previousLayout.clientHeight != bounds.bottom || previousLayout.dpi != a.dpi {
		client := rect{right: bounds.right, bottom: bounds.bottom}
		procInvalidateRect.Call(a.hwnd, uintptr(unsafe.Pointer(&client)), 0)
	} else if previousLayout.scroll != a.scroll || previousLayout.contentHeight != contentHeight {
		settings := rect{top: a.s(settingsTop), right: bounds.right, bottom: bounds.bottom}
		procInvalidateRect.Call(a.hwnd, uintptr(unsafe.Pointer(&settings)), 0)
	}
	a.lastLayout = layoutSnapshot{
		children: append([]childPlacement(nil), placements...),
		scroll:   a.scroll, contentHeight: contentHeight,
		clientWidth: bounds.right, clientHeight: bounds.bottom, dpi: a.dpi, valid: true,
	}
	a.hideClickPointPreview()
}

func (a *application) applyChildPlacements(changes []placementChange) {
	if len(changes) == 0 {
		return
	}
	flagsFor := func(change placementChange) uintptr {
		flags := uintptr(swpNoZOrder | swpNoActivate | swpNoRedraw)
		if !change.current.visible {
			flags |= swpHideWindow
		} else if change.hasPrevious && !change.previous.visible {
			flags |= swpShowWindow
		}
		return flags
	}
	applyOne := func(change placementChange) {
		item := change.current
		procSetWindowPos.Call(item.hwnd, 0,
			uintptr(a.s(item.x)), uintptr(a.s(item.y)), uintptr(a.s(item.width)), uintptr(a.s(item.height)), flagsFor(change))
	}

	applied := false
	batch, _, _ := procBeginDeferWindowPos.Call(uintptr(len(changes)))
	if batch != 0 {
		for _, change := range changes {
			item := change.current
			batch, _, _ = procDeferWindowPos.Call(batch, item.hwnd, 0,
				uintptr(a.s(item.x)), uintptr(a.s(item.y)), uintptr(a.s(item.width)), uintptr(a.s(item.height)), flagsFor(change))
			if batch == 0 {
				break
			}
		}
		if batch != 0 {
			ok, _, _ := procEndDeferWindowPos.Call(batch)
			applied = ok != 0
		}
	}
	if !applied {
		for _, change := range changes {
			applyOne(change)
		}
	}

	for _, change := range changes {
		if change.hasPrevious && change.previous.visible {
			a.invalidateExposedPlacement(change.previous)
		}
		if !change.current.visible {
			continue
		}
		a.invalidateExposedPlacement(change.current)
		procRedrawWindow.Call(change.current.hwnd, 0, 0, rdwInvalidate|rdwNoErase|rdwFrame)
	}
}

func (a *application) invalidateExposedPlacement(item childPlacement) {
	left, top := a.s(item.x), a.s(item.y)
	bounds := rect{left: left, top: top, right: left + a.s(item.width), bottom: top + a.s(item.height)}
	procInvalidateRect.Call(a.hwnd, uintptr(unsafe.Pointer(&bounds)), 0)
}

func (a *application) updateSettingsHeaders() {
	interval := strings.TrimSpace(windowText(a.intervalEdit))
	if interval == "" {
		interval = strconv.Itoa(minIntervalMS)
	}
	hold := "Hold off"
	if a.hold {
		hold = "Hold on"
	}
	clickerState := "collapsed"
	if a.clickerSettingsExpanded {
		clickerState = "expanded"
	}
	pipState := "collapsed"
	if a.pipSettingsExpanded {
		pipState = "expanded"
	}
	if a.clickerSettingsButton != 0 {
		setWindowText(a.clickerSettingsButton, fmt.Sprintf("Clicker settings, %s ms, %s, %s, %s", interval, hold, a.hotkey.name(), clickerState))
	}
	if a.pipSettingsButton != 0 {
		mode := "Off"
		if a.pip.enabled {
			mode = "On"
		}
		setWindowText(a.pipSettingsButton, fmt.Sprintf("Picture-in-picture settings, %s, %d x %d, %d FPS, %s", mode, a.pip.options.width, a.pip.options.height, a.pip.options.fps, pipState))
	}
}

func (a *application) clickerSettingsSummary() string {
	interval := strings.TrimSpace(windowText(a.intervalEdit))
	if interval == "" {
		interval = strconv.Itoa(minIntervalMS)
	}
	hold := "Hold off"
	if a.hold {
		hold = "Hold on"
	}
	return fmt.Sprintf("%s ms / %s / %s", interval, hold, a.hotkey.name())
}

func (a *application) pipSettingsSummary() string {
	mode := "Off"
	if a.pip.enabled {
		mode = "On"
	}
	return fmt.Sprintf("%s / %d x %d / %d FPS", mode, a.pip.options.width, a.pip.options.height, a.pip.options.fps)
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
	canPreset := a.hasPersistentSelection() && !running
	hasPreset := a.selectedPresetIndex() >= 0
	for _, hwnd := range []uintptr{a.presetCombo, a.presetNameEdit, a.presetSaveButton} {
		procEnableWindow.Call(hwnd, boolToUintptr(canPreset))
	}
	for _, hwnd := range []uintptr{a.presetLoadButton, a.presetOverwriteButton, a.presetDeleteButton} {
		procEnableWindow.Call(hwnd, boolToUintptr(canPreset && hasPreset))
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
	if a.presetDrawerExpanded {
		setWindowText(a.presetDrawerButton, "Saved clicks  -")
	} else {
		setWindowText(a.presetDrawerButton, "Saved clicks  +")
	}
	a.updateSettingsHeaders()
	for _, hwnd := range []uintptr{a.pickerButton, a.clickPointButton, a.holdButton, a.hotkeyButton, a.toggleButton, a.clickerSettingsButton, a.pipSettingsButton, a.presetDrawerButton, a.hwnd} {
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

func pipSizeIndex(width, height int) int {
	for i, size := range pipSizes {
		if size.width == width && size.height == height {
			return i
		}
	}
	return 0
}

func pipFPSIndex(fps int) int {
	for i, supported := range pipFrameRates {
		if supported == fps {
			return i
		}
	}
	return 2
}

func (a *application) presetTargetTitle() string {
	if a.selected.hwnd == 0 {
		return "Select a target window to manage its saved clicks."
	}
	if !a.hasPersistentSelection() {
		return "This window has no stable identity for presets."
	}
	return a.selected.title
}
func boolToUintptr(value bool) uintptr {
	if value {
		return 1
	}
	return 0
}

func (a *application) saveSettings() error {
	if a.settingsPath == "" {
		path, err := settingsFilePath()
		if err != nil {
			return err
		}
		a.settingsPath = path
	}
	if a.intervalEdit != 0 && isWindow(a.intervalEdit) {
		if interval, err := parseInterval(windowText(a.intervalEdit)); err == nil {
			a.settings.IntervalMS = interval
		}
	}
	a.settings.Hold = a.hold
	a.settings.Hotkey = savedHotkey{Kind: uint8(a.hotkey.kind), Code: a.hotkey.code}
	a.settings.PIPWidth, a.settings.PIPHeight, a.settings.PIPFPS =
		a.pip.options.width, a.pip.options.height, a.pip.options.fps

	if !a.hasPersistentSelection() {
		a.settings.Selected = nil
		a.settings = normalizeAppSettings(a.settings)
		if !a.settingsWritable {
			return a.settingsUnavailableError()
		}
		return saveAppSettings(a.settingsPath, a.settings)
	}

	identity := a.selected.identity
	savedIdentity := identity
	a.settings.Selected = &savedIdentity
	index := a.ensureSavedWindow(identity)
	window := &a.settings.Windows[index]
	if a.selected.hasInputPoint {
		var bounds rect
		candidate := savedPoint{X: a.selected.inputPoint.x, Y: a.selected.inputPoint.y}
		if getClientRect(a.selected.hwnd, &bounds) &&
			validSavedPoint(candidate, bounds.right-bounds.left, bounds.bottom-bounds.top) {
			window.CustomPoint = &candidate
		} else {
			window.CustomPoint = nil
			a.selected.hasInputPoint = false
			a.selected.inputPoint = point{}
			a.syncSelectedTargetList()
		}
	} else {
		window.CustomPoint = nil
	}
	a.settings = normalizeAppSettings(a.settings)
	if !a.settingsWritable {
		return a.settingsUnavailableError()
	}
	return saveAppSettings(a.settingsPath, a.settings)
}

func (a *application) settingsUnavailableError() error {
	if a.settingsLoadError != "" {
		return fmt.Errorf("existing settings were not changed because loading failed: %s", a.settingsLoadError)
	}
	return fmt.Errorf("settings could not be loaded, so changes were not saved")
}

func (a *application) saveOrReport() {
	if err := a.saveSettings(); err != nil {
		a.setStatus("Could not save settings: "+err.Error(), true)
	}
}

func (a *application) restoreStartupTarget() {
	pruned := pruneUnavailableWindows(&a.settings)
	hadSelection := a.settings.Selected != nil
	restored := false
	if a.settings.Selected != nil {
		savedIdentity := *a.settings.Selected
		target, ok := matchTargetIdentity(savedIdentity, a.targets)
		if !ok {
			if savedIndex, uniqueSaved := a.uniqueSavedWindowClassIndex(savedIdentity); uniqueSaved {
				if rebound, uniqueTarget := uniqueTargetByExecutableClass(savedIdentity, a.targets); uniqueTarget {
					target, ok = rebound, true
					a.settings.Windows[savedIndex].Identity = rebound.identity
				}
			}
		}
		if ok {
			if !sameWindowIdentity(savedIdentity, target.identity) {
				identity := target.identity
				a.settings.Selected = &identity
				if savedIndex, uniqueSaved := a.uniqueSavedWindowClassIndex(savedIdentity); uniqueSaved {
					a.settings.Windows[savedIndex].Identity = target.identity
				}
				pruned = true
			}
			priorWindow := a.savedWindowIndex(target.identity)
			hadCustomPoint := priorWindow >= 0 && a.settings.Windows[priorWindow].CustomPoint != nil
			a.selected = target
			a.applyStoredCustomPoint()
			if hadCustomPoint {
				currentWindow := a.savedWindowIndex(target.identity)
				if currentWindow < 0 || a.settings.Windows[currentWindow].CustomPoint == nil {
					pruned = true
				}
			}
			for i := range a.targets {
				if a.targets[i].hwnd == target.hwnd && a.targets[i].pid == target.pid {
					a.targets[i] = a.selected
					sendMessage(a.processCombo, cbSetCurSel, uintptr(i), 0)
					break
				}
			}
			a.setStatus("Ready. "+a.hotkey.name()+" to start clicking.", false)
			restored = true
		} else {
			a.settings.Selected = nil
		}
	}
	if !restored {
		a.selected = targetWindow{}
	}
	a.refreshPresetControls()
	if pruned || (hadSelection && !restored) {
		a.saveOrReport()
	}
}

func (a *application) hasPersistentSelection() bool {
	identity := a.selected.identity
	return a.selected.hwnd != 0 && identity.ExecutablePath != "" &&
		identity.WindowClass != "" && identity.Title != ""
}

func sameWindowIdentity(left, right windowIdentity) bool {
	return normalizeExecutablePath(left.ExecutablePath) == normalizeExecutablePath(right.ExecutablePath) &&
		left.WindowClass == right.WindowClass && left.Title == right.Title
}

func sameExecutableClass(left, right windowIdentity) bool {
	return normalizeExecutablePath(left.ExecutablePath) != "" &&
		normalizeExecutablePath(left.ExecutablePath) == normalizeExecutablePath(right.ExecutablePath) &&
		left.WindowClass != "" && left.WindowClass == right.WindowClass
}

func uniqueTargetByExecutableClass(identity windowIdentity, targets []targetWindow) (targetWindow, bool) {
	var match targetWindow
	count := 0
	for _, target := range targets {
		if sameExecutableClass(identity, target.identity) {
			match = target
			count++
		}
	}
	return match, count == 1
}

func (a *application) uniqueSavedWindowClassIndex(identity windowIdentity) (int, bool) {
	index, count := -1, 0
	for i := range a.settings.Windows {
		if sameExecutableClass(identity, a.settings.Windows[i].Identity) {
			index = i
			count++
		}
	}
	if count != 1 {
		return -1, false
	}
	return index, true
}

func (a *application) savedWindowIndex(identity windowIdentity) int {
	exactIndex, exactCount := -1, 0
	fallbackIndex, fallbackCount := -1, 0
	for i := range a.settings.Windows {
		saved := a.settings.Windows[i].Identity
		if sameWindowIdentity(saved, identity) {
			exactIndex, exactCount = i, exactCount+1
			continue
		}
		if normalizeExecutablePath(saved.ExecutablePath) == normalizeExecutablePath(identity.ExecutablePath) &&
			saved.WindowClass == identity.WindowClass {
			fallbackIndex, fallbackCount = i, fallbackCount+1
		}
	}
	if exactCount == 1 {
		return exactIndex
	}
	if exactCount > 1 {
		return -1
	}
	if fallbackCount == 1 {
		return fallbackIndex
	}
	return -1
}

func (a *application) ensureSavedWindow(identity windowIdentity) int {
	if i := a.savedWindowIndex(identity); i >= 0 {
		a.settings.Windows[i].Identity = identity
		return i
	}
	a.settings.Windows = append(a.settings.Windows, savedWindow{Identity: identity})
	return len(a.settings.Windows) - 1
}

func (a *application) applyStoredCustomPoint() {
	if !a.hasPersistentSelection() {
		return
	}
	index := a.savedWindowIndex(a.selected.identity)
	if index < 0 || a.settings.Windows[index].CustomPoint == nil {
		return
	}
	saved := *a.settings.Windows[index].CustomPoint
	var bounds rect
	if !getClientRect(a.selected.hwnd, &bounds) ||
		!validSavedPoint(saved, bounds.right-bounds.left, bounds.bottom-bounds.top) {
		a.settings.Windows[index].CustomPoint = nil
		return
	}
	a.selected.inputPoint = point{x: saved.X, y: saved.Y}
	a.selected.hasInputPoint = true
	a.syncSelectedTargetList()
}

func (a *application) syncSelectedTargetList() {
	for i := range a.targets {
		if a.targets[i].hwnd == a.selected.hwnd && a.targets[i].pid == a.selected.pid {
			a.targets[i] = a.selected
			return
		}
	}
}

func (a *application) currentSavedWindowIndex() int {
	if !a.hasPersistentSelection() {
		return -1
	}
	return a.savedWindowIndex(a.selected.identity)
}

func (a *application) currentPresets() []savedPreset {
	index := a.currentSavedWindowIndex()
	if index < 0 {
		return nil
	}
	return a.settings.Windows[index].Presets
}

func findSavedPreset(presets []savedPreset, name string) int {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1
	}
	for i := range presets {
		if strings.EqualFold(strings.TrimSpace(presets[i].Name), name) {
			return i
		}
	}
	return -1
}

func addOrReplaceSavedPreset(presets []savedPreset, preset savedPreset, replace bool) ([]savedPreset, bool) {
	preset.Name = strings.TrimSpace(preset.Name)
	if preset.Name == "" {
		return presets, false
	}
	if index := findSavedPreset(presets, preset.Name); index >= 0 {
		if !replace {
			return presets, false
		}
		presets[index] = preset
		return presets, true
	}
	if replace {
		return presets, false
	}
	return append(presets, preset), true
}

func deleteSavedPreset(presets []savedPreset, name string) ([]savedPreset, bool) {
	index := findSavedPreset(presets, name)
	if index < 0 {
		return presets, false
	}
	copy(presets[index:], presets[index+1:])
	return presets[:len(presets)-1], true
}

func (a *application) selectedPresetIndex() int {
	index := int32(sendMessage(a.presetCombo, cbGetCurSel, 0, 0))
	presets := a.currentPresets()
	if index < 0 || int(index) >= len(presets) {
		return -1
	}
	return int(index)
}

func (a *application) selectedPresetName() string {
	index := a.selectedPresetIndex()
	if index < 0 {
		return ""
	}
	return a.currentPresets()[index].Name
}

func (a *application) refreshPresetControls() {
	if a.presetCombo == 0 {
		return
	}
	presets := a.currentPresets()
	sendMessage(a.presetCombo, cbResetContent, 0, 0)
	selected := -1
	for i, preset := range presets {
		sendMessage(a.presetCombo, cbAddString, 0, uintptr(unsafe.Pointer(utf16Ptr(preset.Name))))
		if strings.EqualFold(preset.Name, a.presetSelectedName) {
			selected = i
		}
	}
	if selected >= 0 {
		sendMessage(a.presetCombo, cbSetCurSel, uintptr(selected), 0)
	} else {
		a.presetSelectedName = ""
		sendMessage(a.presetCombo, cbSetCurSel, ^uintptr(0), 0)
	}
	procInvalidateRect.Call(a.presetCombo, 0, 1)
}

func (a *application) currentClickPoint() (savedPoint, bool) {
	if a.selected.hwnd == 0 {
		return savedPoint{}, false
	}
	clickPoint, _, ok := targetClickPoint(a.selected)
	if !ok {
		return savedPoint{}, false
	}
	var bounds rect
	if !getClientRect(a.selected.hwnd, &bounds) {
		return savedPoint{}, false
	}
	saved := savedPoint{X: clickPoint.x, Y: clickPoint.y}
	return saved, validSavedPoint(saved, bounds.right-bounds.left, bounds.bottom-bounds.top)
}

func (a *application) saveNewPreset() {
	name := strings.TrimSpace(windowText(a.presetNameEdit))
	if name == "" {
		a.setStatus("Enter a name for this click preset.", true)
		procSetFocus.Call(a.presetNameEdit)
		return
	}
	point, ok := a.currentClickPoint()
	if !ok {
		a.setStatus("The selected application is unavailable.", true)
		return
	}
	index := a.ensureSavedWindow(a.selected.identity)
	presets, added := addOrReplaceSavedPreset(a.settings.Windows[index].Presets, savedPreset{Name: name, Point: point}, false)
	if !added {
		a.setStatus("A preset with that name already exists. Choose it and overwrite it.", true)
		return
	}
	a.settings.Windows[index].Presets = presets
	a.presetSelectedName = name
	a.refreshPresetControls()
	if err := a.saveSettings(); err != nil {
		a.setStatus("Could not save settings: "+err.Error(), true)
		return
	}
	a.setStatus("Saved click preset “"+name+"”.", false)
	a.updateControls()
}

func (a *application) loadSelectedPreset() {
	presets := a.currentPresets()
	index := a.selectedPresetIndex()
	if index < 0 || index >= len(presets) {
		a.setStatus("Choose a saved click preset first.", true)
		return
	}
	saved := presets[index]
	var bounds rect
	if !getClientRect(a.selected.hwnd, &bounds) ||
		!validSavedPoint(saved.Point, bounds.right-bounds.left, bounds.bottom-bounds.top) {
		a.setStatus("That preset point is outside the current application area.", true)
		return
	}
	a.selected.inputPoint = point{x: saved.Point.X, y: saved.Point.Y}
	a.selected.hasInputPoint = true
	a.syncSelectedTargetList()
	a.presetSelectedName = saved.Name
	if err := a.saveSettings(); err != nil {
		a.setStatus("Could not save settings: "+err.Error(), true)
		return
	}
	a.setStatus("Loaded click preset “"+saved.Name+"”.", false)
	a.updateControls()
}

func (a *application) overwriteSelectedPreset() {
	presets := a.currentPresets()
	index := a.selectedPresetIndex()
	if index < 0 || index >= len(presets) {
		a.setStatus("Choose a saved click preset to overwrite.", true)
		return
	}
	point, ok := a.currentClickPoint()
	if !ok {
		a.setStatus("The selected application is unavailable.", true)
		return
	}
	name := presets[index].Name
	windowIndex := a.currentSavedWindowIndex()
	updated, replaced := addOrReplaceSavedPreset(presets, savedPreset{Name: name, Point: point}, true)
	if !replaced {
		a.setStatus("Could not update that preset.", true)
		return
	}
	a.settings.Windows[windowIndex].Presets = updated
	a.presetSelectedName = name
	if err := a.saveSettings(); err != nil {
		a.setStatus("Could not save settings: "+err.Error(), true)
		return
	}
	a.setStatus("Updated click preset “"+name+"”.", false)
	a.updateControls()
}

func (a *application) deleteSelectedPreset() {
	presets := a.currentPresets()
	index := a.selectedPresetIndex()
	if index < 0 || index >= len(presets) {
		a.setStatus("Choose a saved click preset to delete.", true)
		return
	}
	name := presets[index].Name
	windowIndex := a.currentSavedWindowIndex()
	updated, deleted := deleteSavedPreset(presets, name)
	if !deleted {
		a.setStatus("Could not delete that preset.", true)
		return
	}
	a.settings.Windows[windowIndex].Presets = updated
	a.presetSelectedName = ""
	a.refreshPresetControls()
	if err := a.saveSettings(); err != nil {
		a.setStatus("Could not save settings: "+err.Error(), true)
		return
	}
	a.setStatus("Deleted click preset “"+name+"”.", false)
	a.updateControls()
}

func workAreaForWindow(hwnd uintptr) (rect, bool) {
	monitor, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if monitor == 0 {
		return rect{}, false
	}
	info := monitorInfo{cbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	ok, _, _ := procGetMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info)))
	if ok == 0 || info.work.right <= info.work.left || info.work.bottom <= info.work.top {
		return rect{}, false
	}
	return info.work, true
}

func clampWindowRectToWorkArea(bounds, work rect) rect {
	workWidth := work.right - work.left
	workHeight := work.bottom - work.top
	width := min(bounds.right-bounds.left, workWidth)
	height := min(bounds.bottom-bounds.top, workHeight)
	left, top := bounds.left, bounds.top
	if left < work.left {
		left = work.left
	}
	if left+width > work.right {
		left = work.right - width
	}
	if top < work.top {
		top = work.top
	}
	if top+height > work.bottom {
		top = work.bottom - height
	}
	return rect{left: left, top: top, right: left + width, bottom: top + height}
}
func (a *application) togglePresetDrawer() {
	wasExpanded := a.presetDrawerExpanded
	a.presetDrawerExpanded = !wasExpanded
	if a.hwnd != 0 && isWindow(a.hwnd) {
		var bounds rect
		result, _, _ := procGetWindowRect.Call(a.hwnd, uintptr(unsafe.Pointer(&bounds)))
		if result != 0 {
			delta := a.s(presetDrawerWidth)
			if a.presetDrawerExpanded {
				bounds.right += delta
			} else {
				bounds.right = max(bounds.left+a.s(536), bounds.right-delta)
			}
			if work, ok := workAreaForWindow(a.hwnd); ok {
				bounds = clampWindowRectToWorkArea(bounds, work)
			}
			width, height := bounds.right-bounds.left, bounds.bottom-bounds.top
			if resized, _, _ := procSetWindowPos.Call(a.hwnd, 0,
				uintptr(bounds.left), uintptr(bounds.top), uintptr(width), uintptr(height),
				swpNoZOrder|swpNoActivate); resized == 0 {
				a.presetDrawerExpanded = wasExpanded
			}
		} else {
			a.presetDrawerExpanded = wasExpanded
		}
	}
	a.layout()
	a.updateControls()
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
			a.refreshPresetControls()
			a.saveOrReport()
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
				a.applyStoredCustomPoint()
				a.setStatus("Ready. "+a.hotkey.name()+" to start clicking.", false)
				a.restartPIP()
				a.refreshPresetControls()
				a.updateControls()
				a.saveOrReport()
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
			a.saveOrReport()
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
	case idClickerSettings:
		if notification == bnClicked {
			a.clickerSettingsExpanded = !a.clickerSettingsExpanded
			a.updateSettingsHeaders()
			a.layout()
		}
	case idPIPSettings:
		if notification == bnClicked {
			a.pipSettingsExpanded = !a.pipSettingsExpanded
			a.updateSettingsHeaders()
			a.layout()
		}
	case idInterval:
		if notification == enChange {
			a.updateSettingsHeaders()
			procInvalidateRect.Call(a.clickerSettingsButton, 0, 1)
			if _, err := parseInterval(windowText(a.intervalEdit)); err == nil {
				a.saveOrReport()
			}
		}
	case idPIP:
		if notification == bnClicked {
			if a.pip.enabled {
				a.stopPIP()
			} else {
				a.enablePIP()
			}
			a.updateSettingsHeaders()
			procInvalidateRect.Call(a.pipSettingsButton, 0, 1)
		}
	case idPIPSize, idPIPFPS:
		if notification == cbnSelChange {
			a.changePIPOptions()
			a.updateSettingsHeaders()
			procInvalidateRect.Call(a.pipSettingsButton, 0, 1)
			a.saveOrReport()
		}
	case idPresetDrawer:
		if notification == bnClicked {
			a.togglePresetDrawer()
		}
	case idPresetList:
		if notification == cbnSelChange {
			a.presetSelectedName = a.selectedPresetName()
			a.updateControls()
		}
	case idPresetSave:
		if notification == bnClicked {
			a.saveNewPreset()
		}
	case idPresetLoad:
		if notification == bnClicked {
			a.loadSelectedPreset()
		}
	case idPresetOverwrite:
		if notification == bnClicked {
			a.overwriteSelectedPreset()
		}
	case idPresetDelete:
		if notification == bnClicked {
			a.deleteSelectedPreset()
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
		minWidth := int32(536)
		if a.presetDrawerExpanded {
			minWidth += presetDrawerWidth
		}
		info.minTrackSize = point{a.s(minWidth), a.s(450)}
		return 0
	case 0x0115: // WM_VSCROLL
		a.handleScroll(loword(wparam))
		return 0
	case 0x020A: // WM_MOUSEWHEEL
		position := point{x: int32(int16(loword(lparam))), y: int32(int16(hiword(lparam)))}
		procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&position)))
		if position.y >= a.s(settingsTop) {
			a.scrollBy(-int32(int16(hiword(wparam))) * 48 / 120)
		}
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
			a.saveOrReport()
		} else if next == a.hotkey {
			a.toggle()
		}
		return 0
	case wmAppPickTarget:
		a.selectPickedTarget(wparam, unpackPoint(lparam))
		a.applyStoredCustomPoint()
		a.refreshPresetControls()
		a.saveOrReport()
		return 0
	case wmAppPickPoint:
		if a.selectPickedClickPoint(wparam, unpackPoint(lparam)) {
			a.saveOrReport()
		}
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
		a.saveOrReport()
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
		a.saveOrReport()
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		a.saveOrReport()
		a.stopPIP()
		_ = a.clicker.stop()
		a.tray.remove()
		procPostQuitMessage.Call(0)
		return 0
	}
	return defaultWindowProc(hwnd, message, wparam, lparam)
}
