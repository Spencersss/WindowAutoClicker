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
		a.clickerSettingsExpanded, a.pipSettingsExpanded = true, true
		a.layout()
		a.scrollBy(a.settingsContentHeight())
		var client, pipHeader rect
		getClientRect(a.hwnd, &client)
		procGetWindowRect.Call(a.pipSettingsButton, uintptr(unsafe.Pointer(&pipHeader)))
		procMapWindowPoints.Call(0, a.hwnd, uintptr(unsafe.Pointer(&pipHeader)), 2)
		if a.scroll <= 0 || pipHeader.top < a.s(settingsTop)-a.s(a.scroll) || pipHeader.bottom > client.bottom {
			t.Fatalf("%d DPI: settings control not reachable: scroll=%d header=%+v client=%+v", tc.dpi, a.scroll, pipHeader, client)
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

func TestSettingsSectionsExpandIndependentlyAndHideControls(t *testing.T) {
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	a := newPIPTestApplication(t)
	styleVisible := func(hwnd uintptr) bool {
		style, _, _ := procGetWindowLongPtr.Call(hwnd, ^uintptr(15))
		return style&wsVisible != 0
	}
	if styleVisible(a.intervalEdit) || styleVisible(a.pipButton) || !styleVisible(a.clickerSettingsButton) || !styleVisible(a.pipSettingsButton) {
		t.Fatal("collapsed settings did not hide their controls and retain both headers")
	}

	a.handleCommand(idClickerSettings, bnClicked)
	if !a.clickerSettingsExpanded || a.pipSettingsExpanded || !styleVisible(a.intervalEdit) || styleVisible(a.pipButton) {
		t.Fatal("expanding clicker settings changed PiP state or visibility")
	}
	a.handleCommand(idPIPSettings, bnClicked)
	if !a.clickerSettingsExpanded || !a.pipSettingsExpanded || !styleVisible(a.pipButton) {
		t.Fatal("PiP settings did not expand independently")
	}
	a.handleCommand(idClickerSettings, bnClicked)
	if a.clickerSettingsExpanded || !a.pipSettingsExpanded || styleVisible(a.intervalEdit) || !styleVisible(a.pipButton) {
		t.Fatal("collapsing clicker settings also collapsed PiP or left hidden controls visible")
	}
}

func TestEssentialControlsStayFixedWhenSettingsScroll(t *testing.T) {
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	a := newPIPTestApplication(t)
	a.clickerSettingsExpanded, a.pipSettingsExpanded = true, true
	procMoveWindow.Call(a.hwnd, 0, 0, 520, 430, 0)
	a.layout()
	controls := []uintptr{a.processCombo, a.pickerButton, a.clickPointButton, a.toggleButton}
	positions := make([]rect, len(controls))
	for i, hwnd := range controls {
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&positions[i])))
		procMapWindowPoints.Call(0, a.hwnd, uintptr(unsafe.Pointer(&positions[i])), 2)
	}
	a.scrollBy(a.settingsContentHeight())
	if a.scroll == 0 {
		t.Fatal("expanded settings did not provide a scroll range in the small viewport")
	}
	for i, hwnd := range controls {
		var got rect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&got)))
		procMapWindowPoints.Call(0, a.hwnd, uintptr(unsafe.Pointer(&got)), 2)
		if got != positions[i] {
			t.Fatalf("essential control %d moved with settings scroll: before=%+v after=%+v", hwnd, positions[i], got)
		}
	}
}

func TestSettingsScrollRangeClampsAfterCollapse(t *testing.T) {
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	a := newPIPTestApplication(t)
	a.clickerSettingsExpanded, a.pipSettingsExpanded = true, true
	procMoveWindow.Call(a.hwnd, 0, 0, 520, 470, 0)
	a.layout()
	page := a.settingsViewportPage()
	a.scrollBy(a.settingsContentHeight())
	full := settingsContentHeight(true, true)
	if want := clampScroll(full, page, full); a.scroll != want || want == 0 {
		t.Fatalf("expanded scroll=%d, want positive max %d", a.scroll, want)
	}

	a.handleCommand(idClickerSettings, bnClicked)
	oneExpanded := settingsContentHeight(false, true)
	if want := clampScroll(a.scroll, page, oneExpanded); a.scroll != want {
		t.Fatalf("scroll after one section collapse=%d, want clamped value %d", a.scroll, want)
	}
	a.handleCommand(idPIPSettings, bnClicked)
	collapsed := settingsContentHeight(false, false)
	if want := clampScroll(a.scroll, page, collapsed); a.scroll != want {
		t.Fatalf("scroll after collapsing both sections=%d, want clamped value %d", a.scroll, want)
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

func TestTargetAndChooseClickHaveGapAtSupportedDPI(t *testing.T) {
	for _, dpi := range []int32{96, 144} {
		a := newApplication()
		a.dpi = dpi
		gap := a.s(chooseClickTop) - (a.s(targetControlTop) + a.s(targetControlHeight))
		if want := a.s(18); gap < want {
			t.Fatalf("%d DPI: target to Choose Click gap is %d pixels, want at least %d", dpi, gap, want)
		}
	}
}

func TestOwnerDrawFocusCueFollowsWindowsUIState(t *testing.T) {
	if shouldDrawOwnerFocusCue(odsFocus | odsNoFocusRect) {
		t.Fatal("owner-drawn control showed a mouse focus rectangle despite ODS_NOFOCUSRECT")
	}
	if !shouldDrawOwnerFocusCue(odsFocus) {
		t.Fatal("owner-drawn control hid the keyboard focus cue")
	}
	if shouldDrawOwnerFocusCue(0) {
		t.Fatal("owner-drawn control showed a focus cue without focus")
	}
}

func TestDiffChildPlacementsSkipsUnchangedChildren(t *testing.T) {
	previous := []childPlacement{
		{hwnd: 1, x: 10, y: 20, width: 100, height: 30, visible: true},
		{hwnd: 2, x: 40, y: 50, width: 60, height: 20, visible: true},
	}
	current := []childPlacement{
		previous[0],
		{hwnd: 2, x: 40, y: 80, width: 60, height: 20, visible: true},
		{hwnd: 3, x: 0, y: 0, width: 12, height: 12, visible: false},
	}

	changes := diffChildPlacements(previous, current)
	if len(changes) != 2 {
		t.Fatalf("got %d placement changes, want 2: %+v", len(changes), changes)
	}
	if changes[0].current.hwnd != 2 || !changes[0].hasPrevious || changes[0].previous.y != 50 || changes[0].current.y != 80 {
		t.Fatalf("moved child did not retain its prior bounds: %+v", changes[0])
	}
	if changes[1].current.hwnd != 3 || changes[1].hasPrevious {
		t.Fatalf("new child was not reported as an initial placement: %+v", changes[1])
	}
}
