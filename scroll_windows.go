package main

import "unsafe"

const mainContentHeight int32 = 740

var (
	procSetScrollInfo   = user32.NewProc("SetScrollInfo")
	procGetScrollInfo   = user32.NewProc("GetScrollInfo")
	procSetViewportOrg  = gdi32.NewProc("SetViewportOrgEx")
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

func clampScroll(pos, page int32) int32 { return max(0, min(pos, mainContentHeight-page)) }

func (a *application) scrollBy(delta int32) {
	var bounds rect
	getClientRect(a.hwnd, &bounds)
	next := clampScroll(a.scroll+delta, bounds.bottom*96/a.dpi)
	if next != a.scroll {
		a.scroll = next
		a.layout()
	}
}

func (a *application) handleScroll(code uint16) {
	var bounds rect
	getClientRect(a.hwnd, &bounds)
	page := bounds.bottom * 96 / a.dpi
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
		a.scrollBy(-mainContentHeight)
	case 7:
		a.scrollBy(mainContentHeight)
	}
}

// Tab navigation reveals offscreen controls instead of moving focus invisibly.
func (a *application) revealFocusedControl() {
	focus, _, _ := procGetFocus.Call()
	if child, _, _ := procIsChild.Call(a.hwnd, focus); child == 0 {
		return
	}
	var bounds, client rect
	procGetWindowRect.Call(focus, uintptr(unsafe.Pointer(&bounds)))
	procMapWindowPoints.Call(0, a.hwnd, uintptr(unsafe.Pointer(&bounds)), 2)
	getClientRect(a.hwnd, &client)
	if bounds.top < 0 {
		a.scrollBy(bounds.top*96/a.dpi - 8)
	} else if bounds.bottom > client.bottom {
		a.scrollBy((bounds.bottom-client.bottom)*96/a.dpi + 8)
	}
}
