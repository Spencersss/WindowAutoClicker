package main

import (
	"fmt"
	"sort"
	"strings"
)

type hotkeyKind uint8

const (
	keyboardHotkey hotkeyKind = iota
	mouseHotkey
)

type hotkey struct {
	kind hotkeyKind
	code uint32
}

func (h hotkey) name() string {
	if h.kind == mouseHotkey {
		switch h.code {
		case mouseRight:
			return "RIGHT MOUSE"
		case mouseMiddle:
			return "MIDDLE MOUSE"
		case mouseX1:
			return "MOUSE 4"
		case mouseX2:
			return "MOUSE 5"
		}
	}

	if h.code >= vkF1 && h.code <= vkF24 {
		return fmt.Sprintf("F%d", h.code-vkF1+1)
	}
	if h.code >= 'A' && h.code <= 'Z' || h.code >= '0' && h.code <= '9' {
		return string(rune(h.code))
	}
	if h.code >= vkNumpad0 && h.code <= vkNumpad9 {
		return fmt.Sprintf("NUM %d", h.code-vkNumpad0)
	}

	names := map[uint32]string{
		vkBack: "BACKSPACE", vkTab: "TAB", vkReturn: "ENTER", vkShift: "SHIFT",
		vkControl: "CTRL", vkMenu: "ALT", vkPause: "PAUSE", vkCapital: "CAPS LOCK",
		vkEscape: "ESC", vkSpace: "SPACE", vkPrior: "PAGE UP", vkNext: "PAGE DOWN",
		vkEnd: "END", vkHome: "HOME", vkLeft: "LEFT", vkUp: "UP", vkRight: "RIGHT",
		vkDown: "DOWN", vkInsert: "INSERT", vkDelete: "DELETE", vkLWin: "LEFT WIN",
		vkRWin: "RIGHT WIN", vkMultiply: "NUM *", vkAdd: "NUM +", vkSubtract: "NUM -",
		vkDecimal: "NUM .", vkDivide: "NUM /", vkNumlock: "NUM LOCK", vkScroll: "SCROLL LOCK",
		vkLShift: "LEFT SHIFT", vkRShift: "RIGHT SHIFT", vkLControl: "LEFT CTRL",
		vkRControl: "RIGHT CTRL", vkLMenu: "LEFT ALT", vkRMenu: "RIGHT ALT",
		vkOem1: ";", vkOemPlus: "=", vkOemComma: ",", vkOemMinus: "-",
		vkOemPeriod: ".", vkOem2: "/", vkOem3: "`", vkOem4: "[",
		vkOem5: "\\", vkOem6: "]", vkOem7: "'",
	}
	if name, ok := names[h.code]; ok {
		return name
	}
	return fmt.Sprintf("KEY 0x%02X", h.code)
}

func normalizeInterval(value int) int {
	if value < minIntervalMS {
		return minIntervalMS
	}
	if value > maxIntervalMS {
		return maxIntervalMS
	}
	return value
}

type targetWindow struct {
	hwnd  uintptr
	title string
	pid   uint32
}

func sortTargets(targets []targetWindow) {
	sort.Slice(targets, func(i, j int) bool {
		return strings.ToLower(targets[i].title) < strings.ToLower(targets[j].title)
	})
}
