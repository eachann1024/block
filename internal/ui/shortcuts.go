package ui

import (
	"fmt"
	"strings"

	"block/internal/theme"
	"github.com/egoist/mygo/ui"
)

// ParseShortcut converts stored strings like "cmd-k", "cmd-shift-m", "alt-n", "ctrl-tab"
// into MyGo ui.Modifiers + ui.Key.
func ParseShortcut(spec string) (ui.Modifiers, ui.Key, bool) {
	s := strings.ToLower(strings.TrimSpace(spec))
	if s == "" {
		return 0, ui.KeyUnknown, false
	}
	trailingMinus := strings.HasSuffix(s, "--") || strings.HasSuffix(s, "+-")
	s = strings.ReplaceAll(s, "+", "-")
	parts := strings.Split(s, "-")
	var mods ui.Modifiers
	var key ui.Key = ui.KeyUnknown
	for _, p := range parts {
		if p == "" {
			continue
		}
		switch p {
		case "cmd", "cmdorctrl", "meta", "super", "command":
			mods |= ui.Cmd
		case "ctrl", "control":
			mods |= ui.Ctrl
		case "alt", "opt", "option":
			mods |= ui.Alt
		case "shift":
			mods |= ui.Shift
		case "tab":
			key = ui.KeyTab
		case "enter", "return":
			key = ui.KeyEnter
		case "esc", "escape":
			key = ui.KeyEscape
		case "space":
			key = ui.KeySpace
		case "backspace":
			key = ui.KeyBackspace
		case "delete", "del":
			key = ui.KeyDelete
		case "left", "arrowleft":
			key = ui.KeyLeft
		case "right", "arrowright":
			key = ui.KeyRight
		case "up", "arrowup":
			key = ui.KeyUp
		case "down", "arrowdown":
			key = ui.KeyDown
		case ",", "comma":
			key = ui.KeyComma
		case ".", "period":
			key = ui.KeyPeriod
		case "/", "slash":
			key = ui.KeySlash
		case "\\", "backslash":
			key = ui.KeyBackslash
		case ";", "semicolon":
			key = ui.KeySemicolon
		case "'", "quote", "apostrophe":
			key = ui.KeyQuote
		case "`", "backquote", "grave":
			key = ui.KeyBackquote
		case "-", "minus":
			key = ui.KeyMinus
		case "=", "equal", "plus":
			key = ui.KeyEqual
		case "[", "bracketleft":
			key = ui.KeyBracketLeft
		case "]", "bracketright":
			key = ui.KeyBracketRight
		default:
			if len(p) >= 2 && p[0] == 'f' {
				var fn int
				if _, err := fmt.Sscanf(p[1:], "%d", &fn); err == nil && fn >= 1 && fn <= 12 {
					key = ui.KeyF1 + ui.Key(fn-1)
					continue
				}
			}
			if len(p) == 1 {
				ch := p[0]
				switch {
				case ch >= 'a' && ch <= 'z':
					key = ui.KeyA + ui.Key(ch-'a')
				case ch >= '0' && ch <= '9':
					key = ui.Key0 + ui.Key(ch-'0')
				}
			}
		}
	}
	if trailingMinus && key == ui.KeyUnknown {
		key = ui.KeyMinus
	}
	return mods, key, key != ui.KeyUnknown
}

// FormatShortcutDisplay formats "cmd-shift-k" into "⌘⇧K" on macOS or "Ctrl+Shift+K" elsewhere.
func FormatShortcutDisplay(spec string) string {
	mods, key, ok := ParseShortcut(spec)
	if !ok {
		return spec
	}
	mac := theme.IsMacOS()
	var parts []string
	if mac {
		if mods&ui.Ctrl != 0 {
			parts = append(parts, "⌃")
		}
		if mods&ui.Alt != 0 {
			parts = append(parts, "⌥")
		}
		if mods&ui.Shift != 0 {
			parts = append(parts, "⇧")
		}
		if mods&ui.Super != 0 {
			parts = append(parts, "⌘")
		}
		return strings.Join(parts, "") + keyDisplayName(key)
	}
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&ui.Super != 0 {
		parts = append(parts, "Super")
	}
	parts = append(parts, keyDisplayName(key))
	return strings.Join(parts, "+")
}

// ToMyGoAccelerator converts internal shortcut strings ("alt-n", "cmd-k") to MyGo Menu/GlobalShortcut syntax.
func ToMyGoAccelerator(spec string) string {
	mods, key, ok := ParseShortcut(spec)
	if !ok {
		return ""
	}
	var parts []string
	if mods&ui.Cmd != 0 {
		parts = append(parts, "CmdOrCtrl")
	} else if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	keyStr := keyDisplayName(key)
	if key == ui.KeyBackspace {
		keyStr = "Backspace"
	} else if key == ui.KeyEscape {
		keyStr = "Escape"
	}
	if keyStr == "" {
		return ""
	}
	parts = append(parts, keyStr)
	return strings.Join(parts, "+")
}

func keyDisplayName(k ui.Key) string {
	switch k {
	case ui.KeyComma:
		return ","
	case ui.KeyPeriod:
		return "."
	case ui.KeySlash:
		return "/"
	case ui.KeyBackslash:
		return "\\"
	case ui.KeySemicolon:
		return ";"
	case ui.KeyQuote:
		return "'"
	case ui.KeyBackquote:
		return "`"
	case ui.KeyMinus:
		return "-"
	case ui.KeyEqual:
		return "="
	case ui.KeyBracketLeft:
		return "["
	case ui.KeyBracketRight:
		return "]"
	case ui.KeyTab:
		return "Tab"
	case ui.KeyEnter:
		return "Enter"
	case ui.KeyEscape:
		return "Esc"
	case ui.KeySpace:
		return "Space"
	case ui.KeyBackspace:
		return "⌫"
	case ui.KeyDelete:
		return "Del"
	case ui.KeyLeft:
		return "←"
	case ui.KeyRight:
		return "→"
	case ui.KeyUp:
		return "↑"
	case ui.KeyDown:
		return "↓"
	default:
		if k >= ui.KeyF1 && k <= ui.KeyF12 {
			return fmt.Sprintf("F%d", 1+int(k-ui.KeyF1))
		}
		if k >= ui.KeyA && k <= ui.KeyZ {
			return string(rune('A' + (k - ui.KeyA)))
		}
		if k >= ui.Key0 && k <= ui.Key9 {
			return string(rune('0' + (k - ui.Key0)))
		}
		return ""
	}
}

// DefaultCommandPaletteShortcut is the default shortcut chord for the command palette.
const DefaultCommandPaletteShortcut = "cmd-shift-p"

// IsHostReservedShortcut reports chords the editor or app handles after plugin
// shortcuts run (find, formatting, clipboard, zoom, tab navigation). Plugins
// cannot bind them, so a plugin never shadows a built-in action.
func IsHostReservedShortcut(mods ui.Modifiers, key ui.Key) bool {
	switch mods {
	case ui.Cmd:
		switch key {
		case ui.KeyA, ui.KeyB, ui.KeyC, ui.KeyD, ui.KeyE, ui.KeyF, ui.KeyI,
			ui.KeyS, ui.KeyU, ui.KeyV, ui.KeyX, ui.KeyY, ui.KeyZ,
			ui.KeyEqual, ui.KeyMinus, ui.Key0, ui.KeyBackslash,
			ui.KeyBracketLeft, ui.KeyBracketRight:
			return true
		}
		return key >= ui.Key1 && key <= ui.Key9
	case ui.Cmd | ui.Shift:
		switch key {
		case ui.KeyS, ui.KeyV, ui.KeyX, ui.KeyZ, ui.KeyEqual, ui.KeyMinus,
			ui.KeyBracketLeft, ui.KeyBracketRight:
			return true
		}
	case ui.Cmd | ui.Shift | ui.Alt:
		return key == ui.KeyV
	case ui.Cmd | ui.Alt:
		return key == ui.KeyC || key == ui.KeyLeft || key == ui.KeyRight
	}
	return false
}

// MatchShortcut reports whether the current frame's key press matches spec (e.g. "cmd-shift-t").
func MatchShortcut(c *ui.Context, spec string) bool {
	if c == nil {
		return false
	}
	mods, key, ok := ParseShortcut(spec)
	if !ok {
		return false
	}
	return c.Shortcut(mods, key)
}

// TreeKeyAction identifies keyboard actions scoped to a focused file tree.
type TreeKeyAction uint8

const (
	TreeKeyNone TreeKeyAction = iota
	TreeKeyRename
	TreeKeyTrash
)

// MatchTreeShortcut routes file-tree keyboard bindings (Enter / F2 rename,
// Cmd+Backspace / Delete trash) only while el or one of its descendants holds focus.
func MatchTreeShortcut(el ui.Element) TreeKeyAction {
	if el.Shortcut(0, ui.KeyEnter) || el.Shortcut(0, ui.KeyF2) {
		return TreeKeyRename
	}
	if el.Shortcut(ui.Cmd, ui.KeyBackspace) || el.Shortcut(0, ui.KeyDelete) {
		return TreeKeyTrash
	}
	return TreeKeyNone
}
