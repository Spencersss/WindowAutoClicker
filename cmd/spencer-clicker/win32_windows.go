package main

import (
	"syscall"
	"unsafe"
)

const (
	appName       = "Spencer Clicker"
	mainClassName = "SpencerClickerMain"

	minIntervalMS = 20
	maxIntervalMS = 2_147_483_647
)

const (
	wmCreate              = 0x0001
	wmActivate            = 0x0006
	wmDestroy             = 0x0002
	wmSize                = 0x0005
	wmPaint               = 0x000F
	wmClose               = 0x0010
	wmGetMinMaxInfo       = 0x0024
	wmCommand             = 0x0111
	wmCtlColorEdit        = 0x0133
	wmCtlColorList        = 0x0134
	wmCtlColorBtn         = 0x0135
	wmCtlColorStatic      = 0x0138
	wmDrawItem            = 0x002B
	wmKeyDown             = 0x0100
	wmKeyUp               = 0x0101
	wmSysKeyDown          = 0x0104
	wmSysKeyUp            = 0x0105
	wmMouseMove           = 0x0200
	wmLButtonDown         = 0x0201
	wmLButtonUp           = 0x0202
	wmRButtonDown         = 0x0204
	wmRButtonUp           = 0x0205
	wmMButtonDown         = 0x0207
	wmMButtonUp           = 0x0208
	wmXButtonDown         = 0x020B
	wmXButtonUp           = 0x020C
	wmAppClickerExit      = 0x8001
	wmAppToggle           = 0x8002
	wmSetFont             = 0x0030
	wmTimer               = 0x0113
	wmMeasureItem         = 0x002C
	wmEraseBkgnd          = 0x0014
	wmDisplayChange       = 0x007E
	wmSettingChange       = 0x001A
	wmDPIChanged          = 0x02E0
	wmQueryEndSession     = 0x0011
	wmEndSession          = 0x0016
	wmPowerBroadcast      = 0x0218
	wmAppInput            = 0x8003
	wmAppPickTarget       = 0x8005
	wmAppPickReleased     = 0x8006
	wmAppPickHover        = 0x8007
	wmAppPickPoint        = 0x8008
	wmNCHitTest           = 0x0084
	wmAppClickPreviewMove = 0x8009
)

const (
	wsOverlapped   = 0x00000000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsTabStop      = 0x00010000
	wsVScroll      = 0x00200000
	wsBorder       = 0x00800000
	wsPopup        = 0x80000000
	wsThickFrame   = 0x00040000
	wsMaximizeBox  = 0x00010000
	wsClipChildren = 0x02000000

	wsExClientEdge  = 0x00000200
	wsExToolWindow  = 0x00000080
	wsExNoActivate  = 0x08000000
	wsExTransparent = 0x00000020
	wsExLayered     = 0x00080000
	wsExTopmost     = 0x00000008

	csHRedraw = 0x0002
	csVRedraw = 0x0001

	ssLeft            = 0x00000000
	bsOwnerDraw       = 0x0000000B
	esNumber          = 0x00002000
	esAutoHScroll     = 0x00000080
	cbsDropdownList   = 0x0003
	cbsOwnerDrawFixed = 0x0010
	cbsHasStrings     = 0x0200
)

const (
	bnClicked       = 0
	cbnSelChange    = 1
	cbnDropdown     = 7
	cbnCloseUp      = 8
	cbnSelEndOK     = 9
	cbnSelEndCancel = 10
	enChange        = 0x0300
	cbAddString     = 0x0143
	cbResetContent  = 0x014B
	cbGetCurSel     = 0x0147
	cbSetCurSel     = 0x014E
	cbGetItemData   = 0x0150
	cbSetItemData   = 0x0151
	emSetLimitText  = 0x00C5
	cbSetItemHeight = 0x0153
	lbItemFromPoint = 0x01A9

	swpNoActivate = 0x0010
	swpShowWindow = 0x0040
	swpNoZOrder   = 0x0004
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002

	swShow           = 5
	swShowNoActivate = 4

	gwOwner          = 4
	gaRoot           = 2
	gaRootOwner      = 3
	htTransparent    = ^uintptr(0)
	hwndTopmost      = ^uintptr(0)
	lwaColorKey      = 0x00000001
	cwpSkipDisabled  = 0x0002
	cwpSkipInvisible = 0x0001
	gwlExStyle       = -20

	processQueryLimitedInformation = 0x1000
	monitorDefaultToNearest        = 2

	colorWindow = 5
	transparent = 1

	dtLeft        = 0x0000
	dtCenter      = 0x0001
	dtVCenter     = 0x0004
	dtSingleLine  = 0x0020
	dtEndEllipsis = 0x8000

	odsSelected    = 0x0001
	odsDisabled    = 0x0004
	odsFocus       = 0x0010
	odsNoFocusRect = 0x0200

	rdwInvalidate = 0x0001
	rdwNoErase    = 0x0020
	rdwFrame      = 0x0400

	swpNoRedraw   = 0x0008
	swpHideWindow = 0x0080

	whKeyboardLL = 13
	whMouseLL    = 14
	hcAction     = 0

	llkhfInjected = 0x10
	llmhfInjected = 0x01

	mkLButton = 0x0001

	spiGetWorkArea = 0x0030

	mbIconError      = 0x00000010
	dwmwaBorderColor = 34
	dwmColorDefault  = 0xFFFFFFFF
)

const (
	vkBack      = 0x08
	vkTab       = 0x09
	vkReturn    = 0x0D
	vkShift     = 0x10
	vkControl   = 0x11
	vkMenu      = 0x12
	vkPause     = 0x13
	vkCapital   = 0x14
	vkEscape    = 0x1B
	vkSpace     = 0x20
	vkPrior     = 0x21
	vkNext      = 0x22
	vkEnd       = 0x23
	vkHome      = 0x24
	vkLeft      = 0x25
	vkUp        = 0x26
	vkRight     = 0x27
	vkDown      = 0x28
	vkInsert    = 0x2D
	vkDelete    = 0x2E
	vkLWin      = 0x5B
	vkRWin      = 0x5C
	vkNumpad0   = 0x60
	vkNumpad9   = 0x69
	vkMultiply  = 0x6A
	vkAdd       = 0x6B
	vkSubtract  = 0x6D
	vkDecimal   = 0x6E
	vkDivide    = 0x6F
	vkF1        = 0x70
	vkF9        = 0x78
	vkF24       = 0x87
	vkNumlock   = 0x90
	vkScroll    = 0x91
	vkLShift    = 0xA0
	vkRShift    = 0xA1
	vkLControl  = 0xA2
	vkRControl  = 0xA3
	vkLMenu     = 0xA4
	vkRMenu     = 0xA5
	vkOem1      = 0xBA
	vkOemPlus   = 0xBB
	vkOemComma  = 0xBC
	vkOemMinus  = 0xBD
	vkOemPeriod = 0xBE
	vkOem2      = 0xBF
	vkOem3      = 0xC0
	vkOem4      = 0xDB
	vkOem5      = 0xDC
	vkOem6      = 0xDD
	vkOem7      = 0xDE
)

const (
	mouseRight uint32 = iota + 1
	mouseMiddle
	mouseX1
	mouseX2
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	uxtheme  = syscall.NewLazyDLL("uxtheme.dll")

	procRegisterClassEx               = user32.NewProc("RegisterClassExW")
	procCreateWindowEx                = user32.NewProc("CreateWindowExW")
	procDefWindowProc                 = user32.NewProc("DefWindowProcW")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	procSetLayeredWindowAttributes    = user32.NewProc("SetLayeredWindowAttributes")
	procUpdateWindow                  = user32.NewProc("UpdateWindow")
	procGetMessage                    = user32.NewProc("GetMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessage               = user32.NewProc("DispatchMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procPostMessage                   = user32.NewProc("PostMessageW")
	procSendMessage                   = user32.NewProc("SendMessageW")
	procSetWindowText                 = user32.NewProc("SetWindowTextW")
	procGetWindowText                 = user32.NewProc("GetWindowTextW")
	procGetWindowTextLength           = user32.NewProc("GetWindowTextLengthW")
	procGetClassName                  = user32.NewProc("GetClassNameW")
	procEnumWindows                   = user32.NewProc("EnumWindows")
	procIsWindowVisible               = user32.NewProc("IsWindowVisible")
	procIsWindow                      = user32.NewProc("IsWindow")
	procGetWindow                     = user32.NewProc("GetWindow")
	procWindowFromPoint               = user32.NewProc("WindowFromPoint")
	procGetComboBoxInfo               = user32.NewProc("GetComboBoxInfo")
	procChildWindowFromPointEx        = user32.NewProc("ChildWindowFromPointEx")
	procGetAncestor                   = user32.NewProc("GetAncestor")
	procClientToScreen                = user32.NewProc("ClientToScreen")
	procScreenToClient                = user32.NewProc("ScreenToClient")
	procGetWindowLongPtr              = user32.NewProc("GetWindowLongPtrW")
	procGetClientRect                 = user32.NewProc("GetClientRect")
	procMonitorFromWindow             = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfo                = user32.NewProc("GetMonitorInfoW")
	procGetClientRectRaw              = procGetClientRect
	procGetWindowThreadProcessID      = user32.NewProc("GetWindowThreadProcessId")
	procOpenProcess                   = kernel32.NewProc("OpenProcess")
	procQueryFullProcessImageName     = kernel32.NewProc("QueryFullProcessImageNameW")
	procCloseHandle                   = kernel32.NewProc("CloseHandle")
	procGetCurrentProcessID           = kernel32.NewProc("GetCurrentProcessId")
	procSetTimer                      = user32.NewProc("SetTimer")
	procKillTimer                     = user32.NewProc("KillTimer")
	procAdjustWindowRectEx            = user32.NewProc("AdjustWindowRectEx")
	procSetFocus                      = user32.NewProc("SetFocus")
	procGetFocus                      = user32.NewProc("GetFocus")
	procDrawFocusRect                 = user32.NewProc("DrawFocusRect")
	procMoveToEx                      = gdi32.NewProc("MoveToEx")
	procLineTo                        = gdi32.NewProc("LineTo")
	procCreateIcon                    = user32.NewProc("CreateIcon")
	procDestroyIcon                   = user32.NewProc("DestroyIcon")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procMoveWindow                    = user32.NewProc("MoveWindow")
	procEnableWindow                  = user32.NewProc("EnableWindow")
	procInvalidateRect                = user32.NewProc("InvalidateRect")
	procBeginPaint                    = user32.NewProc("BeginPaint")
	procEndPaint                      = user32.NewProc("EndPaint")
	procFillRect                      = user32.NewProc("FillRect")
	procDrawText                      = user32.NewProc("DrawTextW")
	procSetTextColor                  = gdi32.NewProc("SetTextColor")
	procSetBkColor                    = gdi32.NewProc("SetBkColor")
	procSetBkMode                     = gdi32.NewProc("SetBkMode")
	procCreateSolidBrush              = gdi32.NewProc("CreateSolidBrush")
	procCreatePen                     = gdi32.NewProc("CreatePen")
	procSelectObject                  = gdi32.NewProc("SelectObject")
	procDeleteObject                  = gdi32.NewProc("DeleteObject")
	procRoundRect                     = gdi32.NewProc("RoundRect")
	procEllipse                       = gdi32.NewProc("Ellipse")
	procCreateFont                    = gdi32.NewProc("CreateFontW")
	procGetStockObject                = gdi32.NewProc("GetStockObject")
	procLoadCursor                    = user32.NewProc("LoadCursorW")
	procLoadIcon                      = user32.NewProc("LoadIconW")
	procGetModuleHandle               = kernel32.NewProc("GetModuleHandleW")
	procSetWindowsHookEx              = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx           = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx                = user32.NewProc("CallNextHookEx")
	procGetSystemMetrics              = user32.NewProc("GetSystemMetrics")
	procSystemParametersInfo          = user32.NewProc("SystemParametersInfoW")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procBeginDeferWindowPos           = user32.NewProc("BeginDeferWindowPos")
	procDeferWindowPos                = user32.NewProc("DeferWindowPos")
	procEndDeferWindowPos             = user32.NewProc("EndDeferWindowPos")
	procRedrawWindow                  = user32.NewProc("RedrawWindow")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procMessageBox                    = user32.NewProc("MessageBoxW")
	procMessageBeep                   = user32.NewProc("MessageBeep")
	procIsDialogMessage               = user32.NewProc("IsDialogMessageW")
	procSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	procGetDpiForSystem               = user32.NewProc("GetDpiForSystem")
	procSetWindowTheme                = uxtheme.NewProc("SetWindowTheme")
	procDwmSetWindowAttribute         = dwmapi.NewProc("DwmSetWindowAttribute")
	procDwmGetWindowAttribute         = dwmapi.NewProc("DwmGetWindowAttribute")
)

type comboBoxInfo struct {
	size                          uint32
	rcItem                        rect
	rcButton                      rect
	uButtonState                  uint32
	hwndCombo, hwndItem, hwndList uintptr
}

type point struct{ x, y int32 }
type rect struct{ left, top, right, bottom int32 }
type monitorInfo struct {
	cbSize  uint32
	monitor rect
	work    rect
	flags   uint32
}
type msg struct {
	hwnd           uintptr
	message        uint32
	wParam, lParam uintptr
	time           uint32
	pt             point
	lPrivate       uint32
}
type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}
type paintStruct struct {
	hdc         uintptr
	erase       int32
	rcPaint     rect
	restore     int32
	incUpdate   int32
	rgbReserved [32]byte
}
type drawItemStruct struct {
	ctlType, ctlID, itemID, itemAction, itemState uint32
	hwndItem, hdc                                 uintptr
	rcItem                                        rect
	itemData                                      uintptr
}
type minMaxInfo struct {
	reserved     point
	maxSize      point
	maxPosition  point
	minTrackSize point
	maxTrackSize point
}
type measureItemStruct struct {
	ctlType, ctlID, itemID, itemWidth, itemHeight uint32
	itemData                                      uintptr
}
type kbdLLHookStruct struct {
	vkCode, scanCode, flags, time uint32
	extraInfo                     uintptr
}
type msLLHookStruct struct {
	pt                     point
	mouseData, flags, time uint32
	extraInfo              uintptr
}

func utf16Ptr(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func loword(v uintptr) uint16   { return uint16(v & 0xffff) }
func hiword(v uintptr) uint16   { return uint16((v >> 16) & 0xffff) }
func rgb(r, g, b byte) uintptr  { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

func setProcessDPIAware() {
	if procSetProcessDpiAwarenessContext.Find() == nil {
		if ok, _, _ := procSetProcessDpiAwarenessContext.Call(^uintptr(3)); ok != 0 {
			return
		}
	}
	procSetProcessDPIAware.Call()
}
func messageBox(hwnd uintptr, text, title string, flags uintptr) {
	procMessageBox.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(title))), flags)
}
func sendMessage(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	r, _, _ := procSendMessage.Call(hwnd, uintptr(message), wparam, lparam)
	return r
}
func postMessage(hwnd uintptr, message uint32, wparam, lparam uintptr) bool {
	r, _, _ := procPostMessage.Call(hwnd, uintptr(message), wparam, lparam)
	return r != 0
}
func setWindowText(hwnd uintptr, text string) {
	procSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(text))))
}
func isWindow(hwnd uintptr) bool { r, _, _ := procIsWindow.Call(hwnd); return r != 0 }
func getClientRect(hwnd uintptr, out *rect) bool {
	r, _, _ := procGetClientRectRaw.Call(hwnd, uintptr(unsafe.Pointer(out)))
	return r != 0
}

func createWindow(exStyle uint32, class, title string, style uint32, x, y, width, height int32, parent, menu, instance uintptr) uintptr {
	r, _, _ := procCreateWindowEx.Call(uintptr(exStyle), uintptr(unsafe.Pointer(utf16Ptr(class))), uintptr(unsafe.Pointer(utf16Ptr(title))), uintptr(style), uintptr(x), uintptr(y), uintptr(width), uintptr(height), parent, menu, instance, 0)
	return r
}

func defaultWindowProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	r, _, _ := procDefWindowProc.Call(hwnd, uintptr(message), wparam, lparam)
	return r
}
