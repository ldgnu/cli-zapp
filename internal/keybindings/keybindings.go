// Package keybindings is wterm's single source of truth for keyboard input.
//
// # Why this package exists
//
// Components never compare key strings. A widget that wants to know "did the
// user ask to scroll up?" is handed an [Action] that has already been resolved
// from a key press by [Map.Resolve]. This buys three things:
//
//   - Remapping is a configuration concern. A user who prefers "mod+left" over
//     "mod+h" changes a TOML file and nothing recompiles.
//   - The whole input layer is testable without a terminal. [Map.Resolve] is a
//     pure function of a key descriptor, so navigation logic can be asserted
//     in unit tests.
//   - Conflicts are detectable. [Map.Conflicts] reports duplicate bindings at
//     startup rather than leaving the user with a key that silently does the
//     wrong thing.
//
// # Vocabulary
//
// An [Action] is a verb, not a key. A [Key] is what the user pressed. A
// [Map] relates them. Configuration supplies both.
package keybindings

import (
	"fmt"
	"strings"
)

// Action is a semantic intent, independent of how it was triggered.
//
// Actions are what components receive. Naming them after intent rather than
// after keys ("scroll up") is what allows a binding to be remapped without
// touching the component.
type Action string

// Application-wide actions.
//
// These are handled by the root model regardless of which panel has focus,
// except where a text input consumes the key first.
const (
	ActionNone Action = ""

	// ActionQuit closes the application, or dismisses the topmost overlay if
	// one is open.
	ActionQuit Action = "app.quit"
	// ActionCancel aborts the current interaction without performing it.
	ActionCancel Action = "app.cancel"
	// ActionConfirm performs the primary action of the focused context.
	ActionConfirm Action = "app.confirm"
	// ActionSearch opens the search palette.
	ActionSearch Action = "app.search"
	// ActionSync forces a reconnection or history sync.
	ActionSync Action = "app.sync"
	// ActionNewChat starts a new conversation.
	ActionNewChat Action = "app.new_chat"
	// ActionInfo opens the contact or group information overlay.
	ActionInfo Action = "app.info"
	// ActionHelp toggles the keybinding cheat sheet.
	ActionHelp Action = "app.help"
	// ActionToggleMute mutes or unmutes the focused chat.
	ActionToggleMute Action = "chat.toggle_mute"
	// ActionTogglePin pins or unpins the focused chat.
	ActionTogglePin Action = "chat.toggle_pin"
	// ActionToggleArchive archives or unarchives the focused chat.
	ActionToggleArchive Action = "chat.toggle_archive"
	// ActionToggleRead marks the focused chat read or unread.
	ActionToggleRead Action = "chat.toggle_read"
	// ActionDeleteChat deletes the focused chat.
	ActionDeleteChat Action = "chat.delete"

	// Message-level actions, valid when the message list has selection.
	ActionReply       Action = "msg.reply"
	ActionEdit        Action = "msg.edit"
	ActionDelete      Action = "msg.delete"
	ActionReact       Action = "msg.react"
	ActionForward     Action = "msg.forward"
	ActionCopy        Action = "msg.copy"
	ActionSelect      Action = "msg.select"
	ActionSelectAll   Action = "msg.select_all"
	ActionDownload    Action = "msg.download"
	ActionOpen        Action = "msg.open"
	ActionMarkStarred Action = "msg.star"
)

// Message-level actions require a selected message; the others do not. This
// distinction drives context-sensitive help and validation.
func (a Action) IsMessageScoped() bool {
	switch a {
	case ActionReply, ActionEdit, ActionDelete, ActionReact, ActionForward,
		ActionCopy, ActionSelect, ActionSelectAll, ActionDownload, ActionOpen, ActionMarkStarred:
		return true
	default:
		return false
	}
}

// Panel identifies a focusable region of the UI.
type Panel int

// Recognised panels.
const (
	PanelSidebar Panel = iota
	PanelMessages
	PanelComposer
)

// String implements fmt.Stringer.
func (p Panel) String() string {
	switch p {
	case PanelSidebar:
		return "sidebar"
	case PanelMessages:
		return "messages"
	case PanelComposer:
		return "composer"
	default:
		return "unknown"
	}
}

// Next returns the panel to the right of p, wrapping around.
//
// Tab cycles forward through this; Shift+Tab uses [Panel.Prev].
func (p Panel) Next() Panel {
	if !p.valid() {
		return PanelSidebar
	}
	return Panel((int(p) + 1) % len(panels))
}

// Prev returns the panel to the left of p, wrapping around.
func (p Panel) Prev() Panel {
	if !p.valid() {
		return PanelSidebar
	}
	return Panel((int(p) - 1 + len(panels)) % len(panels))
}

var panels = [...]Panel{PanelSidebar, PanelMessages, PanelComposer}

func (p Panel) valid() bool { return p >= 0 && int(p) < len(panels) }

// Mod is a keyboard modifier, mirroring the terminal's own notion of one.
type Mod uint8

// Recognised modifiers, as distinct bits.
//
// The values are spelled out rather than derived from iota because a shifted
// iota block placed after a zero-valued constant silently starts at 2,
// producing correct-but-surprising values that are easy to misread later.
const (
	ModNone  Mod = 0
	ModShift Mod = 1 << 0
	ModAlt   Mod = 1 << 1
	ModCtrl  Mod = 1 << 2
	ModSuper Mod = 1 << 3
)

// modNames gives each modifier a canonical spelling and a stable render order.
var modNames = []struct {
	bit  Mod
	name string
}{
	{ModShift, "shift"},
	{ModAlt, "alt"},
	{ModCtrl, "ctrl"},
	{ModSuper, "super"},
}

// Has reports whether m contains every bit in other.
func (m Mod) Has(other Mod) bool { return m&other == other }

// String renders the modifiers in a canonical order, joined by "+".
func (m Mod) String() string {
	if m == ModNone {
		return ""
	}
	var parts []string
	for _, mn := range modNames {
		if m&mn.bit != 0 {
			parts = append(parts, mn.name)
		}
	}
	return strings.Join(parts, "+")
}

// ModSpec is a parsed modifier specification such as "ctrl+shift".
//
// The parser keeps modifiers as a parsed set rather than as text so that
// combinations may be written in any order and with any alias: "mod+shift+f",
// "shift+ctrl+f" and "M-S-f" all denote the same key.
type ModSpec struct {
	bits Mod
}

// ParseModSpec parses one or more modifier tokens joined by "+", such as
// "ctrl+shift" or "mod+alt".
func ParseModSpec(s string) (ModSpec, error) {
	var bits Mod
	for _, tok := range strings.Split(s, "+") {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if tok == "" {
			continue
		}
		m, ok := parseMod(tok)
		if !ok {
			return ModSpec{}, fmt.Errorf("keybindings: unknown modifier %q", tok)
		}
		bits |= m
	}
	if bits == ModNone {
		return ModSpec{}, fmt.Errorf("keybindings: %q names no modifier", s)
	}
	return ModSpec{bits: bits}, nil
}

// Mod returns the parsed modifier bits.
func (m ModSpec) Mod() Mod { return m.bits }

// String renders the modifiers in canonical order.
func (m ModSpec) String() string { return m.bits.String() }

// Key is a single physical key press: a base rune plus modifiers.
//
// It is deliberately independent of any terminal library so that the whole
// binding system can be unit tested without constructing a tea.KeyPressMsg.
// The UI adapter in internal/ui converts between the two.
type Key struct {
	// Rune is the character produced by the key. For named keys such as
	// "enter" or "f1" this holds the library's sentinel rune and Name is set.
	Rune rune
	// Name is a lowercase key name for keys with no meaningful rune, such as
	// "enter", "tab", "up", "pgup". Empty for ordinary character keys.
	Name string
	Mod  Mod
}

// String renders the key in canonical "mod+name" form, which is both the
// configuration syntax and the help-sheet display form.
//
// Canonical form is what makes [Key.String] usable as a map key, and what lets
// a binding be written in the config file exactly as it is displayed to the
// user.
func (k Key) String() string {
	var b strings.Builder
	b.WriteString(k.Mod.String())
	if b.Len() > 0 {
		b.WriteByte('+')
	}
	if k.Name != "" {
		b.WriteString(k.Name)
	} else {
		b.WriteRune(k.Rune)
	}
	return b.String()
}

// IsPrintable reports whether the key produces text and should therefore be
// offered to a focused text input before any binding is consulted.
//
// A modified printable key is not: Ctrl+A must never insert a letter.
func (k Key) IsPrintable() bool {
	return k.Mod == ModNone && k.Name == "" && k.Rune >= 0x20 && k.Rune != 0x7f
}

// Named keys, matching the names accepted in configuration.
//
// These mirror the terminal's own vocabulary rather than any library's, so
// that the configuration file stays readable and stable across upgrades.
const (
	KeyEnter     = "enter"
	KeyEscape    = "esc"
	KeyTab       = "tab"
	KeyBackspace = "backspace"
	KeyDelete    = "delete"
	KeyUp        = "up"
	KeyDown      = "down"
	KeyLeft      = "left"
	KeyRight     = "right"
	KeyHome      = "home"
	KeyEnd       = "end"
	KeyPageUp    = "pgup"
	KeyPageDown  = "pgdown"
	KeySpace     = "space"
)

// keyNameAliases maps every accepted spelling of a named key to its canonical
// name.
//
// "esc" and "escape", or "pgup" and "pageup", are the same physical key;
// insisting on one spelling would reject configurations that are perfectly
// clear to their author.
var keyNameAliases = map[string]string{
	KeyEnter: KeyEnter, "return": KeyEnter, "cr": KeyEnter,
	KeyEscape: KeyEscape, "escape": KeyEscape,
	KeyTab:       KeyTab,
	KeyBackspace: KeyBackspace, "bs": KeyBackspace,
	KeyDelete: KeyDelete, "del": KeyDelete,
	KeyUp:     KeyUp,
	KeyDown:   KeyDown,
	KeyLeft:   KeyLeft,
	KeyRight:  KeyRight,
	KeyHome:   KeyHome,
	KeyEnd:    KeyEnd,
	KeyPageUp: KeyPageUp, "pageup": KeyPageUp, "prior": KeyPageUp,
	KeyPageDown: KeyPageDown, "pagedown": KeyPageDown, "next": KeyPageDown,
	KeySpace: KeySpace, "spc": KeySpace,
}

// keyNames is the set of canonical names a Key may carry.
var keyNames = func() map[string]bool {
	m := make(map[string]bool, len(keyNameAliases))
	for _, canonical := range keyNameAliases {
		m[canonical] = true
	}
	return m
}()

// canonicalKeyName resolves an alias to its canonical spelling, reporting
// whether the name is recognised at all.
func canonicalKeyName(name string) (string, bool) {
	c, ok := keyNameAliases[strings.ToLower(name)]
	return c, ok
}

// IsNamedKey reports whether name is a recognised key name.
func IsNamedKey(name string) bool { return keyNames[name] }
