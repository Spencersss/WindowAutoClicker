package main

import (
	"fmt"
	"unsafe"
)

var (
	colorBG     = rgb(17, 21, 25)
	colorField  = rgb(28, 34, 40)
	colorBorder = rgb(47, 58, 64)
	colorText   = rgb(224, 232, 229)
	colorMuted  = rgb(135, 151, 149)
	colorGreen  = rgb(129, 224, 171)
	colorRed    = rgb(245, 157, 151)
)

func (a *application) makeFonts() {
	for _, object := range []uintptr{a.font, a.smallFont, a.titleFont} {
		if object != 0 {
			procDeleteObject.Call(object)
		}
	}
	create := func(size, weight int32) uintptr {
		font, _, _ := procCreateFont.Call(uintptr(-a.s(size)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0x31,
			uintptr(unsafe.Pointer(utf16Ptr("Consolas"))))
		return font
	}
	a.font, a.smallFont, a.titleFont = create(16, 400), create(13, 400), create(26, 700)
}

func (a *application) applyFonts() {
	for _, hwnd := range []uintptr{a.processCombo, a.pickerButton, a.clickPointButton, a.intervalEdit, a.holdButton, a.hotkeyButton, a.toggleButton, a.pipButton, a.pipSizeCombo, a.pipFPSCombo} {
		if hwnd != 0 {
			sendMessage(hwnd, wmSetFont, a.font, 1)
		}
	}
	sendMessage(a.processCombo, cbSetItemHeight, ^uintptr(0), uintptr(a.s(34)))
	sendMessage(a.processCombo, cbSetItemHeight, 0, uintptr(a.s(targetComboItemHeight)))
	for _, hwnd := range []uintptr{a.pipSizeCombo, a.pipFPSCombo} {
		if hwnd != 0 {
			sendMessage(hwnd, cbSetItemHeight, ^uintptr(0), uintptr(a.s(32)))
			sendMessage(hwnd, cbSetItemHeight, 0, uintptr(a.s(28)))
		}
	}
}

func (a *application) text(hdc uintptr, text string, box rect, font, color uintptr, flags uintptr) {
	old, _, _ := procSelectObject.Call(hdc, font)
	procSetTextColor.Call(hdc, color)
	procSetBkMode.Call(hdc, transparent)
	procDrawText.Call(hdc, uintptr(unsafe.Pointer(utf16Ptr(text))), ^uintptr(0), uintptr(unsafe.Pointer(&box)), flags|dtSingleLine|dtVCenter|dtEndEllipsis|0x0800)
	procSelectObject.Call(hdc, old)
}

func (a *application) box(x, y, w, h int32) rect { return rect{a.s(x), a.s(y), a.s(x + w), a.s(y + h)} }

func fill(hdc uintptr, bounds rect, color uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(color)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&bounds)), brush)
	procDeleteObject.Call(brush)
}

func roundBox(hdc uintptr, bounds rect, color, border uintptr, radius int32) {
	brush, _, _ := procCreateSolidBrush.Call(color)
	pen, _, _ := procCreatePen.Call(0, 1, border)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	procRoundRect.Call(hdc, uintptr(bounds.left), uintptr(bounds.top), uintptr(bounds.right), uintptr(bounds.bottom), uintptr(radius), uintptr(radius))
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func circle(hdc uintptr, bounds rect, color, border uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(color)
	pen, _, _ := procCreatePen.Call(0, 1, border)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	procEllipse.Call(hdc, uintptr(bounds.left), uintptr(bounds.top), uintptr(bounds.right), uintptr(bounds.bottom))
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func (a *application) paintMain(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var bounds rect
	getClientRect(hwnd, &bounds)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&bounds)), a.bgBrush)
	w, h := bounds.right*96/a.dpi, max(mainContentHeight, bounds.bottom*96/a.dpi)
	procSetViewportOrg.Call(hdc, 0, uintptr(-a.s(a.scroll)), 0)
	label := func(text string, x, y, width, height int32, font, color uintptr) {
		a.text(hdc, text, a.box(x, y, width, height), font, color, dtLeft)
	}
	label("spencer / clicker", 28, 24, w-56, 36, a.titleFont, colorText)
	label("A little less clicking.", 28, 64, w-56, 22, a.smallFont, colorMuted)
	fill(hdc, a.box(28, 98, w-56, 1), colorBorder)
	label("TARGET WINDOW", 28, 110, w-56, 20, a.smallFont, colorMuted)
	label("Clicks the chosen point, even when unfocused.", 28, 177, w-224, 24, a.smallFont, colorMuted)
	label("Hover for a point preview.", 28, 198, w-224, 18, a.smallFont, colorMuted)
	label("Client area: "+a.clickPointDescription(), 28, 214, w-56, 22, a.smallFont, colorMuted)
	fill(hdc, a.box(28, 241, w-56, 1), colorBorder)
	label("Click interval", 28, 261, w-235, 24, a.font, colorText)
	label("Delay between clicks / min. 20 ms", 28, 290, w-210, 22, a.smallFont, colorMuted)
	roundBox(hdc, a.box(w-186, 259, 158, 42), colorField, colorBorder, a.s(8))
	label("ms", w-58, 267, 25, 26, a.smallFont, colorMuted)
	label("Hold left click", 28, 334, w-218, 24, a.font, colorText)
	label("One press, held until you stop.", 28, 363, w-205, 22, a.smallFont, colorMuted)
	label("Toggle hotkey", 28, 410, w-255, 24, a.font, colorText)
	label("Keyboard or mouse button", 28, 439, w-245, 22, a.smallFont, colorMuted)
	fill(hdc, a.box(28, 473, w-56, 1), colorBorder)
	label("Picture-in-picture", 28, 490, w-200, 24, a.font, colorText)
	label("Optional preview / Windows 11 24H2+", 28, 526, w-56, 22, a.smallFont, colorMuted)
	label("MAX. PREVIEW SIZE", 28, 551, 230, 20, a.smallFont, colorMuted)
	label("REFRESH LIMIT", w-198, 551, 170, 20, a.smallFont, colorMuted)
	fill(hdc, a.box(28, h-141, w-56, 1), colorBorder)
	statusColor := colorMuted
	if a.statusError {
		statusColor = colorRed
	} else if a.clicker.running {
		statusColor = colorGreen
	}
	label(a.status, 28, h-134, w-56, 28, a.smallFont, statusColor)
	dot := colorMuted
	state := "IDLE"
	if a.clicker.running {
		dot, state = colorGreen, "RUNNING"
	}
	circle(hdc, a.box(29, h-29, 8, 8), dot, dot)
	label(state, 46, h-36, 100, 22, a.smallFont, dot)
	a.text(hdc, "LEFT BUTTON  /  BACKGROUND", a.box(158, h-36, w-186, 22), a.smallFont, colorMuted, 2)
}

func drawWindowSelector(hdc uintptr, bounds rect, color uintptr) {
	// Draw the lens and handle in a square viewport to keep the icon centered
	// and unstretched at every DPI.
	size := min(bounds.right-bounds.left, bounds.bottom-bounds.top)
	if size <= 0 {
		return
	}
	const view = int32(48)
	left := bounds.left + ((bounds.right-bounds.left)-size)/2
	top := bounds.top + ((bounds.bottom-bounds.top)-size)/2
	scale := func(v int32) int32 { return v * size / view }
	x := func(v int32) int32 { return left + scale(v) }
	y := func(v int32) int32 { return top + scale(v) }
	brush, _, _ := procCreateSolidBrush.Call(color)
	pen, _, _ := procCreatePen.Call(0, 1, color)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	handlePen, _, _ := procCreatePen.Call(0, uintptr(max(1, scale(4))), color)
	oldHandlePen, _, _ := procSelectObject.Call(hdc, handlePen)
	procMoveToEx.Call(hdc, uintptr(x(29)), uintptr(y(29)), 0)
	procLineTo.Call(hdc, uintptr(x(41)), uintptr(y(41)))
	procSelectObject.Call(hdc, oldHandlePen)
	procDeleteObject.Call(handlePen)
	outer := rect{x(6), y(6), x(35), y(35)}
	procEllipse.Call(hdc, uintptr(outer.left), uintptr(outer.top), uintptr(outer.right), uintptr(outer.bottom))
	procSelectObject.Call(hdc, oldPen)
	procSelectObject.Call(hdc, oldBrush)
	procDeleteObject.Call(pen)
	procDeleteObject.Call(brush)
	background := colorField
	if a := activeApp; a != nil && a.picker {
		background = rgb(32, 66, 49)
	}
	hole, _, _ := procCreateSolidBrush.Call(background)
	holePen, _, _ := procCreatePen.Call(0, 1, background)
	oldBrush, _, _ = procSelectObject.Call(hdc, hole)
	oldPen, _, _ = procSelectObject.Call(hdc, holePen)
	inner := rect{x(10), y(10), x(31), y(31)}
	procEllipse.Call(hdc, uintptr(inner.left), uintptr(inner.top), uintptr(inner.right), uintptr(inner.bottom))
	procSelectObject.Call(hdc, oldPen)
	procSelectObject.Call(hdc, oldBrush)
	procDeleteObject.Call(holePen)
	procDeleteObject.Call(hole)
}
func (a *application) drawItem(item *drawItemStruct) {
	if item.ctlID == idPIPSize || item.ctlID == idPIPFPS {
		text := ""
		index := int(item.itemID)
		if item.ctlID == idPIPSize && index >= 0 && index < len(pipSizes) {
			text = pipSizes[index].label
		} else if item.ctlID == idPIPFPS && index >= 0 && index < len(pipFrameRates) {
			text = fmt.Sprintf("%d FPS", pipFrameRates[index])
		}
		background := colorField
		if item.itemState&odsSelected != 0 {
			background = rgb(40, 65, 55)
		}
		fill(item.hdc, item.rcItem, background)
		box := item.rcItem
		box.left += a.s(8)
		a.text(item.hdc, text, box, a.font, colorText, dtLeft)
		return
	}
	if item.ctlID == idPicker {
		background, foreground, border := colorField, colorText, colorBorder
		if a.picker {
			background, foreground, border = rgb(32, 66, 49), colorGreen, colorGreen
		}
		if item.itemState&odsDisabled != 0 {
			foreground = colorMuted
		}
		if item.itemState&odsSelected != 0 {
			border = colorText
		}
		fill(item.hdc, item.rcItem, colorBG)
		roundBox(item.hdc, item.rcItem, background, border, a.s(8))
		iconBox := item.rcItem
		iconSize := min(item.rcItem.right-item.rcItem.left, item.rcItem.bottom-item.rcItem.top)
		iconSize = max(a.s(18), iconSize-a.s(8))
		iconBox.left = (item.rcItem.left + item.rcItem.right - iconSize) / 2
		iconBox.top = (item.rcItem.top + item.rcItem.bottom - iconSize) / 2
		iconBox.right = iconBox.left + iconSize
		iconBox.bottom = iconBox.top + iconSize
		drawWindowSelector(item.hdc, iconBox, foreground)
		if item.itemState&odsFocus != 0 {
			box := item.rcItem
			box.left += a.s(4)
			box.right -= a.s(4)
			box.top += a.s(4)
			box.bottom -= a.s(4)
			procDrawFocusRect.Call(item.hdc, uintptr(unsafe.Pointer(&box)))
		}
		return
	}
	if item.ctlID == idProcess {
		background, foreground := colorField, colorText
		if item.itemState&odsSelected != 0 {
			background = rgb(40, 65, 55)
		}
		fill(item.hdc, item.rcItem, background)
		text := "Select a window..."
		if item.itemID != 0xffffffff && int(item.itemID) < len(a.targets) {
			text = a.targets[item.itemID].title
			for i, target := range a.targets {
				if uint32(i) != item.itemID && target.title == text {
					text = fmt.Sprintf("%s [pid %d]", text, a.targets[item.itemID].pid)
					break
				}
			}
		} else {
			foreground = colorMuted
		}
		box := item.rcItem
		box.left += a.s(10)
		box.right -= a.s(8)
		a.text(item.hdc, text, box, a.font, foreground, dtLeft)
		return
	}
	background, foreground, border := colorField, colorText, colorBorder
	text := ""
	switch item.ctlID {
	case idClickPoint:
		text = "CHOOSE CLICK"
		if a.pointPicker {
			text, background, foreground = "CLICK A SPOT", rgb(32, 66, 49), colorGreen
		}
	case idPIP:
		text = "OFF"
		if a.pip.enabled {
			text, background, foreground = "ON", rgb(32, 66, 49), colorGreen
		}
	case idHold:
		text = "OFF"
		if a.hold {
			text, background, foreground = "ON", rgb(32, 66, 49), colorGreen
		}
	case idHotkey:
		text = a.hotkey.name()
		if a.capture {
			text, foreground, border = "PRESS A KEY...", colorGreen, colorGreen
		}
	case idToggle:
		text, background, foreground, border = "START CLICKER  ["+a.hotkey.name()+"]", colorGreen, colorBG, colorGreen
		if a.clicker.running || a.clicker.pressed {
			text, background, foreground, border = "STOP CLICKER  ["+a.hotkey.name()+"]", rgb(75, 42, 43), colorRed, rgb(125, 65, 65)
		}
	}
	if item.itemState&odsDisabled != 0 {
		foreground = colorMuted
	}
	if item.itemState&odsSelected != 0 {
		border = colorText
	}
	fill(item.hdc, item.rcItem, colorBG)
	roundBox(item.hdc, item.rcItem, background, border, a.s(8))
	box := item.rcItem
	box.left += a.s(8)
	box.right -= a.s(8)
	a.text(item.hdc, text, box, a.font, foreground, dtCenter)
	if item.itemState&odsFocus != 0 {
		box.top += a.s(4)
		box.bottom -= a.s(4)
		procDrawFocusRect.Call(item.hdc, uintptr(unsafe.Pointer(&box)))
	}
}

// Small native icon, using the same circular status motif as the UI. No image
// decoding library or bundled font/image assets are needed at runtime.
func (a *application) loadAppIcon() uintptr {
	// The resource is embedded into the PE by app.rc. Keep the procedural
	// fallback for source-only builds that have not regenerated the .syso file.
	icon, _, _ := procLoadIcon.Call(a.instance, 1)
	if icon != 0 {
		return icon
	}
	return a.makeIcon(true)
}

func (a *application) makeIcon(active bool) uintptr {
	const size = 32
	mask := make([]byte, size*size/8)
	pixels := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := 2*x-31, 2*y-31
			i := (y*size + x) * 4
			if dx*dx+dy*dy <= 28*28 {
				pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = 171, 224, 129, 255
				if !active {
					pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = 149, 151, 135, 180
				}
				if dx*dx+dy*dy < 10*10 {
					pixels[i], pixels[i+1], pixels[i+2] = 25, 21, 17
				}
			} else {
				mask[y*size/8+x/8] |= 0x80 >> (x % 8)
			}
		}
	}
	icon, _, _ := procCreateIcon.Call(a.instance, size, size, 1, 32, uintptr(unsafe.Pointer(&mask[0])), uintptr(unsafe.Pointer(&pixels[0])))
	return icon
}
