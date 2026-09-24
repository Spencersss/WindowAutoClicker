package main

import "unsafe"

const (
	settingsTop                   int32 = 350
	settingsSectionHeaderHeight   int32 = 42
	settingsSectionGap            int32 = 12
	clickerSettingsExpandedHeight int32 = 192
	pipSettingsExpandedHeight     int32 = 174
)

var (
	procSetScrollInfo   = user32.NewProc("SetScrollInfo")
	procGetScrollInfo   = user32.NewProc("GetScrollInfo")
	procIsChild         = user32.NewProc("IsChild")
	procGetWindowRect   = user32.NewProc("GetWindowRect")
	procMapWindowPoints = user32.NewProc("MapWindowPoints")
)

type scrollInfo struct {
	size, mask uint32
	min, max   int32
	page       uint32
	pos, track int32
}

func clickerSettingsHeight(expanded bool) int32 {
	if expanded {
		return clickerSettingsExpandedHeight
	}
	return settingsSectionHeaderHeight
}

func pipSettingsHeight(expanded bool) int32 {
	if expanded {
		return pipSettingsExpandedHeight
	}
	return settingsSectionHeaderHeight
}

func settingsContentHeight(clickerExpanded, pipExpanded bool) int32 {
	return clickerSettingsHeight(clickerExpanded) + settingsSectionGap + pipSettingsHeight(pipExpanded)
}

func clampScroll(pos, page, contentHeight int32) int32 {
	return max(0, min(pos, max(0, contentHeight-page)))
}

func (a *application) settingsViewportPage() int32 {
	var bounds rect
	getClientRect(a.hwnd, &bounds)
	return max(1, bounds.bottom*96/a.dpi-settingsTop)
}

func (a *application) settingsContentHeight() int32 {
	return settingsContentHeight(a.clickerSettingsExpanded, a.pipSettingsExpanded)
}

func (a *application) scrollBy(delta int32) {
	next := clampScroll(a.scroll+delta, a.settingsViewportPage(), a.settingsContentHeight())
	if next != a.scroll {
		a.scroll = next
		a.layout()
	}
}

func (a *application) handleScroll(code uint16) {
	page := a.settingsViewportPage()
	switch code {
	case 0:
		a.scrollBy(-24)
	case 1:
		a.scrollBy(24)
	case 2:
		a.scrollBy(-page)
	case 3:
		a.scrollBy(page)
	case 4, 5:
		info := scrollInfo{size: uint32(unsafe.Sizeof(scrollInfo{})), mask: 0x10}
		procGetScrollInfo.Call(a.hwnd, 1, uintptr(unsafe.Pointer(&info)))
		a.scrollBy(info.track - a.scroll)
	case 6:
		a.scrollBy(-a.settingsContentHeight())
	case 7:
		a.scrollBy(a.settingsContentHeight())
	}
}

func (a *application) revealFocusedControl() {
	focus, _, _ := procGetFocus.Call()
	if child, _, _ := procIsChild.Call(a.hwnd, focus); child == 0 {
		return
	}
	control := a.settingsControl(focus)
	if control == 0 {
		return
	}
	contentTop, height := a.settingsControlPosition(control)
	screenTop := contentTop - a.scroll
	page := a.settingsViewportPage()
	if screenTop < settingsTop {
		a.scrollBy(screenTop - settingsTop - 8)
	} else if screenTop+height > settingsTop+page {
		a.scrollBy(screenTop + height - settingsTop - page + 8)
	}
}

func (a *application) settingsControl(hwnd uintptr) uintptr {
	controls := []uintptr{
		a.clickerSettingsButton, a.intervalEdit, a.holdButton, a.hotkeyButton,
		a.pipSettingsButton, a.pipButton, a.pipSizeCombo, a.pipFPSCombo,
	}
	for _, control := range controls {
		if hwnd == control {
			return control
		}
		if control != 0 {
			if child, _, _ := procIsChild.Call(control, hwnd); child != 0 {
				return control
			}
		}
	}
	return 0
}

func (a *application) settingsControlPosition(hwnd uintptr) (int32, int32) {
	clickerTop := settingsTop
	pipTop := clickerTop + clickerSettingsHeight(a.clickerSettingsExpanded) + settingsSectionGap
	switch hwnd {
	case a.clickerSettingsButton:
		return clickerTop, settingsSectionHeaderHeight
	case a.intervalEdit:
		return clickerTop + 52, 36
	case a.holdButton:
		return clickerTop + 98, 36
	case a.hotkeyButton:
		return clickerTop + 144, 36
	case a.pipSettingsButton:
		return pipTop, settingsSectionHeaderHeight
	case a.pipButton:
		return pipTop + 52, 36
	case a.pipSizeCombo, a.pipFPSCombo:
		return pipTop + 120, 36
	default:
		return 0, 0
	}
}
