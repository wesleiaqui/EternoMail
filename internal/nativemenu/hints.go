package nativemenu

import (
	"strings"
	"unicode"

	"github.com/wailsapp/wails/v2/pkg/menu"
)

// These are AppKit's device-independent modifier bits, not platform key handlers.
const (
	hintShift   = 1 << 17
	hintControl = 1 << 18
	hintOption  = 1 << 19
	hintCommand = 1 << 20
)

type menuHint struct {
	Title     string     `json:"title"`
	Key       string     `json:"key,omitempty"`
	Modifiers uint       `json:"modifiers,omitempty"`
	Children  []menuHint `json:"children,omitempty"`
}

// shortcutHint translates catalog glyphs into NSMenuItem's native display data.
// It does not install an accelerator or change keyboard ownership.
func shortcutHint(shortcut string) (string, uint) {
	var modifiers uint
	for _, prefix := range []struct {
		glyph string
		mask  uint
	}{
		{"⌘", hintCommand}, {"⇧", hintShift}, {"⌥", hintOption}, {"⌃", hintControl},
	} {
		if strings.Contains(shortcut, prefix.glyph) {
			modifiers |= prefix.mask
			shortcut = strings.ReplaceAll(shortcut, prefix.glyph, "")
		}
	}
	switch shortcut {
	case "Space":
		return " ", modifiers
	case "Esc":
		return "\x1b", modifiers
	case "↩":
		return "\r", modifiers
	case "⇥":
		return "\t", modifiers
	case "⌦":
		return "\uf728", modifiers
	case "⌫":
		return "\x7f", modifiers
	case "↑":
		return "\uf700", modifiers
	case "↓":
		return "\uf701", modifiers
	case "←":
		return "\uf702", modifiers
	case "→":
		return "\uf703", modifiers
	}
	if len([]rune(shortcut)) != 1 {
		return "", 0
	}
	return strings.Map(unicode.ToLower, shortcut), modifiers
}

func (c *Controller) hintTree(items []*menu.MenuItem) []menuHint {
	result := make([]menuHint, 0, len(items))
	for _, item := range items {
		hint := menuHint{Title: item.Label}
		// True clipboard accelerators keep AppKit's normal responder-chain behavior.
		if item.Accelerator == nil {
			hint.Key, hint.Modifiers = shortcutHint(c.hints[item])
		}
		if item.SubMenu != nil {
			hint.Children = c.hintTree(item.SubMenu.Items)
		}
		if hint.Key != "" || len(hint.Children) > 0 {
			result = append(result, hint)
		}
	}
	return result
}
