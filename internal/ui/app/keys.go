package app

import (
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/keybindings"
)

// convertKey translates a Bubble Tea key press into a [keybindings.Key].
//
// This is the only place the two representations meet, which is what allows the
// binding system to be tested with plain structs and no terminal and keeps
// internal/keybindings free of any Bubble Tea dependency.
//
// # Why the fields are read rather than String() parsed
//
// KeyPressMsg.String() is lossy in a way that is easy to miss. Probing it:
//
//	ctrl+j    → "j"
//	ctrl+h    → "h"
//	ctrl+q    → "q"
//	tab       → "tab"
//	shift+tab → "shift+tab"
//
// For a printable key it reports only the character and drops the modifier, because
// ctrl+j and j arrive as the same byte. Parsing that string therefore yields a key
// with no modifier, and every ctrl binding fails to match — a bug that looks like
// "none of my shortcuts work" rather than like anything wrong in the UI.
//
// The named keys do keep their modifiers, which makes the inconsistency worse:
// ctrl+tab works while ctrl+j does not. Reading [tea.KeyPressMsg.Mod] and
// [tea.KeyPressMsg.Code] directly avoids the question.
func convertKey(msg tea.KeyPressMsg) keybindings.Key {
	mod := convertMod(msg.Mod)

	// A named key is recognised by an explicit table, not by a numeric range.
	//
	// Bubble Tea is not consistent about how it encodes them: the arrows and the
	// page keys live above unicode.MaxRune (KeyExtended is MaxRune+1), but tab,
	// enter, backspace, delete and space are their plain ASCII codes — 9, 13, 8,
	// 127 and 32. A single range test therefore catches half of them and misses
	// the rest, and the ones it misses arrive as ordinary characters.
	if name, ok := namedKeyCodes[msg.Code]; ok {
		return keybindings.Key{Name: name, Mod: mod}
	}

	// Anything at or above KeyExtended is a sentinel this build does not know by
	// name. Mapping it to the empty name means it matches no binding, which is the
	// safe outcome: a key we cannot name must not trigger an action the user never
	// asked for.
	if msg.Code >= tea.KeyExtended {
		return keybindings.Key{Mod: mod}
	}

	// For a printable key, Text is authoritative: with shift held it holds the
	// shifted character ("G" rather than "g"), which is what a binding should
	// match.
	if msg.Text != "" {
		if r, size := utf8.DecodeRuneInString(msg.Text); size == len(msg.Text) && size > 0 {
			return keybindings.Key{Rune: r, Mod: mod}
		}
	}
	return keybindings.Key{Rune: msg.Code, Mod: mod}
}

// namedKeyCodes maps Bubble Tea's key codes to wterm's canonical names.
//
// Space is included even though its code is the printable ASCII space: "space" is
// the name a user writes in a configuration file, and no binding uses a bare " "
// rune, so mapping it here loses nothing.
var namedKeyCodes = map[rune]string{
	tea.KeyEnter:     keybindings.KeyEnter,
	tea.KeyEscape:    keybindings.KeyEscape,
	tea.KeyTab:       keybindings.KeyTab,
	tea.KeyBackspace: keybindings.KeyBackspace,
	tea.KeyDelete:    keybindings.KeyDelete,
	tea.KeySpace:     keybindings.KeySpace,
	tea.KeyUp:        keybindings.KeyUp,
	tea.KeyDown:      keybindings.KeyDown,
	tea.KeyLeft:      keybindings.KeyLeft,
	tea.KeyRight:     keybindings.KeyRight,
	tea.KeyHome:      keybindings.KeyHome,
	tea.KeyEnd:       keybindings.KeyEnd,
	tea.KeyPgUp:      keybindings.KeyPageUp,
	tea.KeyPgDown:    keybindings.KeyPageDown,
}

// convertMod maps Bubble Tea's modifier set onto wterm's.
func convertMod(m tea.KeyMod) keybindings.Mod {
	var out keybindings.Mod
	if m&tea.ModShift != 0 {
		out |= keybindings.ModShift
	}
	if m&tea.ModAlt != 0 {
		out |= keybindings.ModAlt
	}
	if m&tea.ModCtrl != 0 {
		out |= keybindings.ModCtrl
	}
	if m&tea.ModSuper != 0 {
		out |= keybindings.ModSuper
	}
	return out
}
