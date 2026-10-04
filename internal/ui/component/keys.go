package component

import (
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/keybindings"
)

// ConvertKey translates a Bubble Tea key press into a [keybindings.Key].
//
// # Why the fields are read and String is not parsed
//
// KeyPressMsg.String() is lossy for printable keys: it reports "j" for ctrl+j,
// because ctrl+j and j arrive as the same byte. Parsing that string produces a key
// with no modifier, and every ctrl binding then silently fails to match. The
// symptom looks like "my shortcuts do nothing" rather than like a defect.
//
// Named keys are recognised through a table rather than a numeric range, because
// Bubble Tea is inconsistent: the arrows and page keys live above unicode.MaxRune,
// while tab, enter, backspace, delete and space are plain ASCII codes. A range
// test catches half of them and misses the rest.
func ConvertKey(msg tea.KeyPressMsg) keybindings.Key {
	mod := ConvertMod(msg.Mod)

	if canonical, ok := namedKeyCodes[msg.Code]; ok {
		return keybindings.Key{Name: canonical, Mod: mod}
	}

	// A sentinel this build does not know by name maps to the empty name, which
	// matches no binding: a key we cannot name must not trigger an action.
	if msg.Code >= tea.KeyExtended {
		return keybindings.Key{Mod: mod}
	}

	// Text is authoritative for a printable key: with shift held it holds the
	// shifted character, which is what a binding should match.
	if msg.Text != "" {
		if r, size := utf8.DecodeRuneInString(msg.Text); size == len(msg.Text) && size > 0 {
			return keybindings.Key{Rune: r, Mod: mod}
		}
	}
	return keybindings.Key{Rune: msg.Code, Mod: mod}
}

// namedKeyCodes maps Bubble Tea's key codes to wterm's canonical names.
//
// Space is included although its code is printable ASCII: "space" is the name a
// user writes in a configuration file, and no binding uses a bare " " rune.
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

// ConvertMod maps Bubble Tea's modifier set onto wterm's.
func ConvertMod(m tea.KeyMod) keybindings.Mod {
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

// IsPrintable reports whether a press should be offered to a text field.
//
// Shift is allowed through, because shift+v still produces a letter and
// rejecting it would make capitals impossible to type. Every other modifier is
// not: ctrl+w must be a binding, not an inserted "w".
func IsPrintable(msg tea.KeyPressMsg) bool {
	if msg.Text == "" {
		return false
	}
	if _, named := namedKeyCodes[msg.Code]; named {
		return false
	}
	if msg.Mod&^tea.ModShift != 0 {
		return false
	}
	for _, r := range msg.Text {
		if r == ' ' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// Named returns the canonical name of a key press: "enter", "esc", "tab", "down",
// "ctrl+q" and so on.
//
// # Why Keystroke and not String
//
// [tea.KeyPressMsg.String] is inconsistent about whether a key has text. Probed
// directly:
//
//	{Code: 13, Text: "\r"} → String() == "\r"       Keystroke() == "enter"
//	{Code: 13, Text: ""}    → String() == "enter"    Keystroke() == "enter"
//	{Code: 9,  Text: "\t"} → String() == "\t"       Keystroke() == "tab"
//	{Code: 27, Text: ""}    → String() == "esc"      Keystroke() == "esc"
//
// Whether Text is populated depends on the terminal and on the terminfo entry, not on
// the key. Matching on String therefore means a menu that activates on "enter" works
// on one terminal and does nothing on the next — the worst kind of bug, because it
// looks like the application is ignoring the key.
//
// Keystroke is stable across both. The two also disagree about modifiers, which is
// why [ConvertKey] reads the fields rather than parsing either string: for a printable
// key, String reports the character alone, so ctrl+q parses as q.
func Named(msg tea.KeyPressMsg) string { return msg.Keystroke() }

// IsNamed reports whether a key press is one of the given canonical names.
//
// Named keys are matched by name rather than by code so that a binding table, a
// terminal and this file all agree on what "enter" is.
func IsNamed(msg tea.KeyPressMsg, names ...string) bool {
	name := Named(msg)
	for _, n := range names {
		if name == n {
			return true
		}
	}
	return false
}
