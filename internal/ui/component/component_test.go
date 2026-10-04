package component_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/ui/component"
)

// TestConvertKeyProbesBubbleTeaBehaviour documents what the framework actually does,
// rather than what it is documented to do.
//
// Every case here is one that was verified against Bubble Tea's source at the version
// this module pins. They are written as probes rather than as assumptions because the
// assumption is what broke: parsing KeyPressMsg.String() yields a key with no modifier
// for every printable key, and every ctrl binding then silently fails to match.
func TestConvertKeyProbesBubbleTeaBehaviour(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyPressMsg
		want keybindings.Key
	}{
		{
			// String() reports "j" for ctrl+j, because ctrl+j and j arrive as the
			// same byte. Reading the fields avoids the question entirely.
			name: "ctrl+j keeps its modifier",
			msg:  tea.KeyPressMsg{Code: 'j', Text: "j", Mod: tea.ModCtrl},
			want: keybindings.Key{Rune: 'j', Mod: keybindings.ModCtrl},
		},
		{
			name: "ctrl+q keeps its modifier",
			msg:  tea.KeyPressMsg{Code: 'q', Text: "q", Mod: tea.ModCtrl},
			want: keybindings.Key{Rune: 'q', Mod: keybindings.ModCtrl},
		},
		{
			name: "a bare rune has no modifier",
			msg:  tea.KeyPressMsg{Code: 'j', Text: "j"},
			want: keybindings.Key{Rune: 'j'},
		},
		{
			// With shift held, Text holds the shifted character, which is what a
			// binding should match.
			name: "shift produces the shifted rune",
			msg:  tea.KeyPressMsg{Code: 'J', Text: "J", Mod: tea.ModShift},
			want: keybindings.Key{Rune: 'J', Mod: keybindings.ModShift},
		},
		{
			// Enter is ASCII 13 but is still a named key.
			name: "enter is named, not a rune",
			msg:  tea.KeyPressMsg{Code: tea.KeyEnter, Text: "\r"},
			want: keybindings.Key{Name: keybindings.KeyEnter},
		},
		{
			name: "tab is named, not a rune",
			msg:  tea.KeyPressMsg{Code: '\t', Text: "\t"},
			want: keybindings.Key{Name: keybindings.KeyTab},
		},
		{
			name: "escape is named",
			msg:  tea.KeyPressMsg{Code: tea.KeyEscape},
			want: keybindings.Key{Name: keybindings.KeyEscape},
		},
		{
			// Space is printable ASCII but is named anyway, because "space" is what a
			// user writes in a configuration file and no binding uses a bare " ".
			name: "space is named",
			msg:  tea.KeyPressMsg{Code: ' ', Text: " "},
			want: keybindings.Key{Name: keybindings.KeySpace},
		},
		{
			name: "backspace is named",
			msg:  tea.KeyPressMsg{Code: tea.KeyBackspace},
			want: keybindings.Key{Name: keybindings.KeyBackspace},
		},
		{
			// The arrows live above unicode.MaxRune. A range test would catch these
			// and miss the ones above, which is why the table is explicit.
			name: "down is named",
			msg:  tea.KeyPressMsg{Code: tea.KeyDown},
			want: keybindings.Key{Name: keybindings.KeyDown},
		},
		{
			name: "page up is named",
			msg:  tea.KeyPressMsg{Code: tea.KeyPgUp},
			want: keybindings.Key{Name: keybindings.KeyPageUp},
		},
		{
			// An unknown sentinel maps to the empty name, which matches no binding. A
			// key we cannot name must not trigger an action the user never asked for.
			name: "an unknown sentinel names nothing",
			msg:  tea.KeyPressMsg{Code: tea.KeyExtended + 999},
			want: keybindings.Key{},
		},
		{
			name: "a multi-byte rune survives",
			msg:  tea.KeyPressMsg{Code: 'ñ', Text: "ñ"},
			want: keybindings.Key{Rune: 'ñ'},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := component.ConvertKey(tc.msg); got != tc.want {
				t.Errorf("ConvertKey(%+v) = %+v, want %+v", tc.msg, got, tc.want)
			}
		})
	}
}

// TestConvertKeyResolvesTheDefaultBindings is the assertion that matters: every
// default binding must survive the conversion. A single unmapped key here is a
// shortcut that does nothing for every user who did not rebind it.
func TestConvertKeyResolvesTheDefaultBindings(t *testing.T) {
	m := keybindings.DefaultMap()

	for _, b := range m.Bindings() {
		for _, k := range b.Keys {
			msg := msgFor(t, k)
			if got := m.ResolveGlobal(k); got != keybindings.ActionNone {
				continue // already global; the panel lookup is not needed
			}
			got := m.Resolve(k, b.Panel)
			if got != b.Action {
				t.Errorf("key %v is declared for %s but resolves to %s",
					k, b.Action, got)
			}
			if msg.Code == 0 && msg.Text == "" {
				t.Errorf("key %v produced an empty message", k)
			}
		}
	}
}

// msgFor builds the Bubble Tea message a terminal would deliver for a binding key.
func msgFor(t *testing.T, k keybindings.Key) tea.KeyPressMsg {
	t.Helper()

	var mod tea.KeyMod
	if k.Mod.Has(keybindings.ModCtrl) {
		mod |= tea.ModCtrl
	}
	if k.Mod.Has(keybindings.ModAlt) {
		mod |= tea.ModAlt
	}
	if k.Mod.Has(keybindings.ModShift) {
		mod |= tea.ModShift
	}
	if k.Mod.Has(keybindings.ModSuper) {
		mod |= tea.ModSuper
	}

	if k.Rune != 0 {
		r := k.Rune
		return tea.KeyPressMsg{Code: r, Text: string(r), Mod: mod}
	}

	switch k.Name {
	case keybindings.KeyEnter:
		return tea.KeyPressMsg{Code: tea.KeyEnter, Text: "\r", Mod: mod}
	case keybindings.KeyEscape:
		return tea.KeyPressMsg{Code: tea.KeyEscape, Mod: mod}
	case keybindings.KeyTab:
		return tea.KeyPressMsg{Code: '\t', Text: "\t", Mod: mod}
	case keybindings.KeySpace:
		return tea.KeyPressMsg{Code: ' ', Text: " ", Mod: mod}
	case keybindings.KeyBackspace:
		return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: mod}
	case keybindings.KeyDelete:
		return tea.KeyPressMsg{Code: tea.KeyDelete, Mod: mod}
	case keybindings.KeyUp:
		return tea.KeyPressMsg{Code: tea.KeyUp, Mod: mod}
	case keybindings.KeyDown:
		return tea.KeyPressMsg{Code: tea.KeyDown, Mod: mod}
	case keybindings.KeyLeft:
		return tea.KeyPressMsg{Code: tea.KeyLeft, Mod: mod}
	case keybindings.KeyRight:
		return tea.KeyPressMsg{Code: tea.KeyRight, Mod: mod}
	case keybindings.KeyHome:
		return tea.KeyPressMsg{Code: tea.KeyHome, Mod: mod}
	case keybindings.KeyEnd:
		return tea.KeyPressMsg{Code: tea.KeyEnd, Mod: mod}
	case keybindings.KeyPageUp:
		return tea.KeyPressMsg{Code: tea.KeyPgUp, Mod: mod}
	case keybindings.KeyPageDown:
		return tea.KeyPressMsg{Code: tea.KeyPgDown, Mod: mod}
	}

	t.Fatalf("the test does not know how to build a message for the key %q", k.Name)
	return tea.KeyPressMsg{}
}

func TestIsPrintable(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyPressMsg
		want bool
	}{
		{"a letter", tea.KeyPressMsg{Code: 'a', Text: "a"}, true},
		{"a multi-byte letter", tea.KeyPressMsg{Code: 'ñ', Text: "ñ"}, true},
		{"an emoji", tea.KeyPressMsg{Code: '👍', Text: "👍"}, true},
		// Shift is allowed through: shift+a still produces a letter, and rejecting it
		// would make capitals impossible to type.
		{"shift+a", tea.KeyPressMsg{Code: 'A', Text: "A", Mod: tea.ModShift}, true},
		// Everything else is a binding, not text. This is the rule that keeps ctrl+q
		// from being typed as a "q".
		{"ctrl+a", tea.KeyPressMsg{Code: 'a', Text: "a", Mod: tea.ModCtrl}, false},
		{"alt+a", tea.KeyPressMsg{Code: 'a', Text: "a", Mod: tea.ModAlt}, false},
		{"super+a", tea.KeyPressMsg{Code: 'a', Text: "a", Mod: tea.ModSuper}, false},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter, Text: "\r"}, false},
		{"tab", tea.KeyPressMsg{Code: '\t', Text: "\t"}, false},
		{"space", tea.KeyPressMsg{Code: ' ', Text: " "}, false},
		{"backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}, false},
		{"a named key with no text", tea.KeyPressMsg{Code: tea.KeyUp}, false},
		{"a control byte", tea.KeyPressMsg{Code: 1, Text: "\x01"}, false},
		{"delete", tea.KeyPressMsg{Code: tea.KeyDelete}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := component.IsPrintable(tc.msg); got != tc.want {
				t.Errorf("IsPrintable(%+v) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

// TestNamedIsStableAcrossTextAndCode is the reason [component.Named] exists.
//
// String() is inconsistent about whether a key has text, which means a menu activated
// on "enter" works on one terminal and does nothing on the next. Keystroke is stable.
func TestNamedIsStableAcrossTextAndCode(t *testing.T) {
	pairs := []struct {
		withText tea.KeyPressMsg
		without  tea.KeyPressMsg
		want     string
	}{
		{
			withText: tea.KeyPressMsg{Code: tea.KeyEnter, Text: "\r"},
			without:  tea.KeyPressMsg{Code: tea.KeyEnter},
			want:     "enter",
		},
		{
			withText: tea.KeyPressMsg{Code: '\t', Text: "\t"},
			without:  tea.KeyPressMsg{Code: '\t'},
			want:     "tab",
		},
		{
			withText: tea.KeyPressMsg{Code: ' ', Text: " "},
			without:  tea.KeyPressMsg{Code: ' '},
			want:     "space",
		},
	}

	for _, p := range pairs {
		if got := component.Named(p.withText); got != p.want {
			t.Errorf("Named(%+v) = %q, want %q", p.withText, got, p.want)
		}
		if got := component.Named(p.without); got != p.want {
			t.Errorf("Named(%+v) = %q, want %q", p.without, got, p.want)
		}
		if !component.IsNamed(p.withText, p.want) {
			t.Errorf("IsNamed(%+v, %q) = false", p.withText, p.want)
		}
	}
}

func TestEventCmdIsNilForNoEvent(t *testing.T) {
	// Returning a command for a non-event would schedule a message the model would
	// then have to recognise and ignore, once per keystroke.
	if component.EventCmd(component.Event{}) != nil {
		t.Error("the zero event should produce no command")
	}
	if component.EventCmd(component.Event{Kind: component.KindFocusChat}) == nil {
		t.Error("a real event should produce a command")
	}
}

func TestEventStringNamesEveryKind(t *testing.T) {
	// A kind missing from the name table renders as "unknown" in a log, which is the
	// one moment the name is actually needed.
	kinds := []component.Kind{
		component.KindFocusChat, component.KindFocusRegion,
		component.KindSearchChanged, component.KindSearchDismissed,
		component.KindSubmit, component.KindInsertNewline, component.KindCancel,
		component.KindScroll, component.KindScrollTo,
		component.KindMessageSelected, component.KindMessageActivated,
		component.KindSelectionToggled, component.KindCommandChosen,
		component.KindDismissOverlay, component.KindSubmitOverlay, component.KindQuit,
	}

	for _, k := range kinds {
		e := component.Event{Kind: k}
		if got := e.String(); got == "unknown" || got == "none" {
			t.Errorf("kind %d renders as %q", k, got)
		}
		if !e.Is(k) {
			t.Errorf("Is(%d) should be true for an event of that kind", k)
		}
	}
}
