package keybindings

import (
	"errors"
	"slices"
	"testing"
)

func TestParseKeyNamedKeys(t *testing.T) {
	tests := []struct {
		spec string
		want Key
	}{
		{"enter", Key{Name: "enter"}},
		{"ENTER", Key{Name: "enter"}},
		{"  enter  ", Key{Name: "enter"}},
		{"up", Key{Name: "up"}},
		{"pgdown", Key{Name: "pgdown"}},
		// "escape" and "esc" both canonicalise to "esc".
		{"esc", Key{Name: "esc"}},
		{"escape", Key{Name: "esc"}},
		{"pageup", Key{Name: "pgup"}},
		{"pagedown", Key{Name: "pgdown"}},
		{"del", Key{Name: "delete"}},
		{"return", Key{Name: "enter"}},
		// Modifiers combine in any order and with any alias.
		{"mod+shift+f", Key{Rune: 'f', Mod: ModCtrl | ModShift}},
		{"shift+ctrl+f", Key{Rune: 'f', Mod: ModCtrl | ModShift}},
		{"ctrl+shift+f", Key{Rune: 'f', Mod: ModCtrl | ModShift}},
		{"mod+enter", Key{Name: "enter", Mod: ModCtrl}},
		{"ctrl+enter", Key{Name: "enter", Mod: ModCtrl}},
		{"control+enter", Key{Name: "enter", Mod: ModCtrl}},
		{"^enter", Key{Name: "enter", Mod: ModCtrl}},
		{"c+enter", Key{Name: "enter", Mod: ModCtrl}},
		{"shift+tab", Key{Name: "tab", Mod: ModShift}},
		{"alt+left", Key{Name: "left", Mod: ModAlt}},
		{"cmd+k", Key{Rune: 'k', Mod: ModSuper}},
		{"super+j", Key{Rune: 'j', Mod: ModSuper}},
	}
	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := ParseKey(tc.spec)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("want %+v, got %+v", tc.want, got)
			}
		})
	}
}

func TestParseKeyRunes(t *testing.T) {
	tests := []struct {
		spec string
		want Key
	}{
		{"j", Key{Rune: 'j'}},
		{"J", Key{Rune: 'J'}},
		{"G", Key{Rune: 'G'}},
		{"?", Key{Rune: '?'}},
		{"/", Key{Rune: '/'}},
		{"ñ", Key{Rune: 'ñ'}},
		{"mod+j", Key{Rune: 'j', Mod: ModCtrl}},
		{"mod+J", Key{Rune: 'J', Mod: ModCtrl}},
		{"alt+G", Key{Rune: 'G', Mod: ModAlt}},
		// A literal plus, bare and modified.
		{"+", Key{Rune: '+'}},
		{"ctrl++", Key{Rune: '+', Mod: ModCtrl}},
	}
	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := ParseKey(tc.spec)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("want %+v, got %+v", tc.want, got)
			}
		})
	}
}

func TestParseKeyErrors(t *testing.T) {
	tests := []struct {
		name string
		spec string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"unknown modifier", "hyper+j"},
		{"unknown name", "foo"},
		{"multi character", "abc"},
		{"modifier with no key", "ctrl+"},
		{"control character", "\x01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseKey(tc.spec); err == nil {
				t.Errorf("ParseKey(%q) should have failed", tc.spec)
			}
		})
	}
}

func TestParseKeyEmptySpecSentinel(t *testing.T) {
	// Callers may want to test for the sentinel specifically.
	if _, err := ParseKey(""); !errors.Is(err, ErrEmptySpec) {
		t.Errorf("want ErrEmptySpec, got %v", err)
	}
	if _, err := ParseKeys(nil); !errors.Is(err, ErrEmptyBinding) {
		t.Errorf("want ErrEmptyBinding, got %v", err)
	}
}

func TestParseKeysRejectsDuplicateWithinOneAction(t *testing.T) {
	if _, err := ParseKeys([]string{"mod+j", "ctrl+j"}); err == nil {
		t.Error("the same key spelled two ways should be rejected as a duplicate")
	}
	if _, err := ParseKeys([]string{"mod+j", "mod+k"}); err != nil {
		t.Errorf("distinct keys should be accepted: %v", err)
	}
}

func TestKeyStringRoundTrip(t *testing.T) {
	// The canonical form is what appears in both the config file and the help
	// sheet, so it must parse back to an identical Key.
	specs := []string{
		"ctrl+q", "ctrl+j", "alt+enter", "shift+tab", "enter", "esc", "up",
		"space", "G", "j", "?", "+", "ctrl++", "ctrl+backspace", "ctrl+home",
		"alt+pgup", "super+k",
	}
	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			k, err := ParseKey(spec)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			rendered := k.String()
			again, err := ParseKey(rendered)
			if err != nil {
				t.Fatalf("re-parse of %q: %v", rendered, err)
			}
			if again != k {
				t.Errorf("round trip changed the key: %+v → %q → %+v", k, rendered, again)
			}
		})
	}
}

func TestKeyStringCanonicalModOrder(t *testing.T) {
	// Modifier order must not depend on how the user spelled the binding, or
	// the help sheet would render the same key two different ways.
	k := Key{Rune: 'k', Mod: ModCtrl | ModShift | ModAlt}
	if got, want := k.String(), "shift+alt+ctrl+k"; got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestKeyIsPrintable(t *testing.T) {
	tests := []struct {
		key  Key
		want bool
	}{
		{Key{Rune: 'a'}, true},
		{Key{Rune: 'A'}, true},
		{Key{Rune: ' '}, true},
		{Key{Rune: 'ñ'}, true},
		{Key{Rune: 'a', Mod: ModCtrl}, false},
		{Key{Rune: 'a', Mod: ModAlt}, false},
		{Key{Name: "enter"}, false},
		{Key{Name: "tab"}, false},
		{Key{Rune: 0x7f}, false}, // DEL
	}
	for _, tc := range tests {
		if got := tc.key.IsPrintable(); got != tc.want {
			t.Errorf("Key%+v.IsPrintable() = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestResolvePanelAndGlobal(t *testing.T) {
	m := NewMap([]Binding{
		{Action: ActionQuit, Global: true, Keys: []Key{{Name: "q", Mod: ModCtrl}}},
		{Action: NavUp, Panel: PanelSidebar, Keys: []Key{{Rune: 'k'}}},
		{Action: NavUp, Panel: PanelMessages, Keys: []Key{{Rune: 'k'}}},
	})

	if got := m.Resolve(Key{Rune: 'k'}, PanelSidebar); got != NavUp {
		t.Errorf("sidebar: want %v, got %v", NavUp, got)
	}
	// The same key means the same action here, but a per-panel override must be
	// honoured, so check a key that differs by panel.
	if got := m.Resolve(Key{Rune: 'k'}, PanelComposer); got != ActionNone {
		t.Errorf("composer: unbound key should resolve to none, got %v", got)
	}
	if got := m.Resolve(Key{Name: "q", Mod: ModCtrl}, PanelComposer); got != ActionQuit {
		t.Errorf("global binding must resolve from any panel, got %v", got)
	}
}

func TestResolvePerPanelOverride(t *testing.T) {
	// The same key bound to different actions per panel: resolution must follow
	// focus.
	m := NewMap([]Binding{
		{Action: ActionDelete, Panel: PanelSidebar, Keys: []Key{{Name: "delete"}}},
		{Action: ActionOpen, Panel: PanelMessages, Keys: []Key{{Name: "delete"}}},
	})
	if got := m.Resolve(Key{Name: "delete"}, PanelSidebar); got != ActionDelete {
		t.Errorf("sidebar: want %v, got %v", ActionDelete, got)
	}
	if got := m.Resolve(Key{Name: "delete"}, PanelMessages); got != ActionOpen {
		t.Errorf("messages: want %v, got %v", ActionOpen, got)
	}
}

func TestResolveGlobalShadowsPanel(t *testing.T) {
	// Documented precedence: globals are checked first, so a global binding can
	// deliberately override a panel one.
	m := NewMap([]Binding{
		{Action: ActionQuit, Global: true, Keys: []Key{{Rune: 'd'}}},
		{Action: ActionDelete, Panel: PanelMessages, Keys: []Key{{Rune: 'd'}}},
	})
	if got := m.Resolve(Key{Rune: 'd'}, PanelMessages); got != ActionQuit {
		t.Errorf("global should win, got %v", got)
	}
}

func TestConflictsDetected(t *testing.T) {
	m := NewMap([]Binding{
		{Action: ActionDelete, Panel: PanelMessages, Keys: []Key{{Rune: 'd'}}},
		{Action: ActionCopy, Global: true, Keys: []Key{{Rune: 'd'}}},
	})
	c := m.Conflicts()
	if len(c) != 1 {
		t.Fatalf("want 1 conflict, got %d: %+v", len(c), c)
	}
	if c[0].Key.String() != "d" {
		t.Errorf("unexpected key: %q", c[0].Key)
	}
	if !slices.Equal(c[0].Actions, []Action{ActionCopy, ActionDelete}) {
		t.Errorf("actions should be sorted, got %v", c[0].Actions)
	}
	if err := m.Check(); err == nil {
		t.Error("Check should report the conflict")
	}
}

func TestConflictsIgnoresSameActionTwice(t *testing.T) {
	m := NewMap([]Binding{
		{Action: ActionQuit, Global: true, Keys: []Key{{Rune: 'q', Mod: ModCtrl}}},
		{Action: ActionQuit, Global: true, Keys: []Key{{Name: "q", Mod: ModCtrl}}},
	})
	if got := m.Conflicts(); len(got) != 0 {
		t.Errorf("one action bound twice is not a conflict, got %+v", got)
	}
}

func TestConflictsSameKeyDifferentPanelsIsNotAConflict(t *testing.T) {
	// Two panel-scoped bindings can never be live simultaneously.
	m := NewMap([]Binding{
		{Action: ActionReply, Panel: PanelMessages, Keys: []Key{{Rune: 'r'}}},
		{Action: ActionToggleRead, Panel: PanelSidebar, Keys: []Key{{Rune: 'r'}}},
	})
	if got := m.Conflicts(); len(got) != 0 {
		t.Errorf("panel-scoped reuse should not be a conflict, got %+v", got)
	}
}

func TestDefaultsHaveNoConflicts(t *testing.T) {
	m := DefaultMap()
	if err := m.Check(); err != nil {
		t.Errorf("default bindings conflict: %v", err)
	}
}

func TestDefaultsCoverRequiredShortcuts(t *testing.T) {
	// These come straight from the design brief. Asserting them here means a
	// refactor that drops one fails the build rather than shipping silently.
	required := map[Action][]string{
		ActionQuit:    {"ctrl+q"},
		ActionSearch:  {"ctrl+f"},
		ActionSync:    {"ctrl+r"},
		ActionNewChat: {"ctrl+n"},
		ActionInfo:    {"ctrl+g"},
		ActionEdit:    {"ctrl+e"},
		ActionDelete:  {"ctrl+d"},
		ActionCancel:  {"esc"},
		ActionConfirm: {"ctrl+enter"},
		PanelNext:     {"tab"},
		PanelPrev:     {"shift+tab"},
		PageUp:        {"ctrl+b"},
		PageDown:      {"alt+pgdown"},
		ScrollTop:     {"ctrl+home"},
		ScrollBottom:  {"ctrl+end"},
		SendMessage:   {"enter"},
		NavLeft:       {"ctrl+h"},
		NavRight:      {"ctrl+l"},
		ScrollUp:      {"pgup"},
		ScrollDown:    {"pgdown"},
	}
	m := DefaultMap()

	for action, want := range required {
		keys := m.Keys(action)
		if len(keys) == 0 {
			t.Errorf("action %s has no binding", action)
			continue
		}
		got := FormatKeys(keys)
		if !slices.Contains(got, want[0]) {
			t.Errorf("action %s: want it bound to %q, got %v", action, want[0], got)
		}
	}
}

func TestDefaultsResolveAsDesigned(t *testing.T) {
	// Walk through the navigation paths from the brief using real key presses,
	// to prove the table behaves as intended rather than merely existing.
	m := DefaultMap()

	tests := []struct {
		name  string
		key   Key
		focus Panel
		want  Action
	}{
		{"mod+h from messages focuses sidebar", Key{Rune: 'h', Mod: ModCtrl}, PanelMessages, NavLeft},
		{"mod+l from messages focuses composer", Key{Rune: 'l', Mod: ModCtrl}, PanelMessages, NavRight},
		{"mod+j in sidebar is next chat", Key{Rune: 'j', Mod: ModCtrl}, PanelSidebar, NavDown},
		{"mod+k in sidebar is previous chat", Key{Rune: 'k', Mod: ModCtrl}, PanelSidebar, NavUp},
		{"mod+j in messages is next message", Key{Rune: 'j', Mod: ModCtrl}, PanelMessages, NavDown},
		{"mod+q quits from anywhere", Key{Rune: 'q', Mod: ModCtrl}, PanelMessages, ActionQuit},
		{"tab switches panel", Key{Name: "tab"}, PanelMessages, PanelNext},
		{"shift+tab goes back", Key{Name: "tab", Mod: ModShift}, PanelMessages, PanelPrev},
		{"enter in sidebar opens the chat", Key{Name: "enter"}, PanelSidebar, ChatOpen},
		{"enter in composer sends", Key{Name: "enter"}, PanelComposer, SendMessage},
		// Bare j and k must scroll, not navigate chats, while messages have
		// focus. This is the distinction that makes the modifier hierarchy
		// useful rather than confusing.
		{"j in messages scrolls", Key{Rune: 'j'}, PanelMessages, ScrollDown},
		{"k in messages scrolls", Key{Rune: 'k'}, PanelMessages, ScrollUp},
		{"pgup scrolls up", Key{Name: "pgup"}, PanelMessages, ScrollUp},
		{"pgdown scrolls down", Key{Name: "pgdown"}, PanelMessages, ScrollDown},
		// alt+pgup pages a full screen, distinct from the bare arrow.
		{"alt+pgup pages up", Key{Name: "pgup", Mod: ModAlt}, PanelMessages, PageUp},
		{"alt+pgdown pages down", Key{Name: "pgdown", Mod: ModAlt}, PanelMessages, PageDown},
		{"ctrl+d deletes the message", Key{Rune: 'd', Mod: ModCtrl}, PanelMessages, ActionDelete},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.Resolve(tc.key, tc.focus); got != tc.want {
				t.Errorf("%s in %v: want %v, got %v", tc.key, tc.focus, tc.want, got)
			}
		})
	}
}

func TestDefaultsDoNotStealPrintableKeysFromComposer(t *testing.T) {
	// A user typing "j" in the composer must get the letter j. The binding
	// system resolves actions first, so this is enforced by the UI handing
	// printable keys to the text area before consulting the map; assert here
	// only that no default binds a bare printable rune in the composer, which
	// would make that fallback unreachable.
	m := DefaultMap()
	for _, b := range m.Bindings() {
		if b.Panel != PanelComposer {
			continue
		}
		for _, k := range b.Keys {
			if k.IsPrintable() {
				t.Errorf("action %s binds bare printable key %q in the composer, "+
					"which would make typed text unreachable", b.Action, k)
			}
		}
	}
}

func TestPanelCycling(t *testing.T) {
	if got := PanelSidebar.Next(); got != PanelMessages {
		t.Errorf("sidebar → messages, got %v", got)
	}
	if got := PanelComposer.Next(); got != PanelSidebar {
		t.Errorf("composer wraps to sidebar, got %v", got)
	}
	if got := PanelSidebar.Prev(); got != PanelComposer {
		t.Errorf("sidebar wraps back to composer, got %v", got)
	}
	// An out-of-range panel must not panic.
	if got := Panel(99).Next(); got != PanelSidebar {
		t.Errorf("invalid panel should fall back to sidebar, got %v", got)
	}
}

func TestHelpFiltersByFocusAndSorts(t *testing.T) {
	m := DefaultMap()
	help := m.Help(PanelMessages)

	if len(help) == 0 {
		t.Fatal("expected help entries")
	}
	for i := 1; i < len(help); i++ {
		if help[i-1].Action > help[i].Action {
			t.Errorf("help not sorted at %d: %v then %v", i, help[i-1].Action, help[i].Action)
		}
	}
	// Panel-scoped entries for other panels must not be offered.
	for _, e := range help {
		if !e.Global && e.Panel != PanelMessages {
			t.Errorf("entry %s is scoped to %v but shown while %v is focused",
				e.Action, e.Panel, PanelMessages)
		}
	}
	// Global entries are always present.
	var sawQuit bool
	for _, e := range help {
		if e.Action == ActionQuit {
			sawQuit = true
			if len(e.Keys) == 0 || e.Help == "" {
				t.Errorf("quit entry missing keys or help: %+v", e)
			}
		}
	}
	if !sawQuit {
		t.Error("global quit binding missing from help")
	}
}

func TestBindingsAndKeysReturnCopies(t *testing.T) {
	m := NewMap([]Binding{
		{Action: ActionQuit, Global: true, Keys: []Key{{Rune: 'q', Mod: ModCtrl}}},
	})
	bs := m.Bindings()
	bs[0].Keys[0] = Key{Rune: 'z'}
	// Re-read through the map: the caller's copy must not alias it.
	if again := m.Bindings(); again[0].Keys[0].Rune == 'z' {
		t.Fatal("Bindings must return copies of the key slices")
	}

	ks := m.Keys(ActionQuit)
	ks[0] = Key{Rune: 'z'}
	if fresh := m.Keys(ActionQuit); fresh[0].Rune != 'q' {
		t.Fatalf("Keys must not expose the map's own slice, got %q", fresh[0].Rune)
	}
}

func TestModString(t *testing.T) {
	tests := []struct {
		mod  Mod
		want string
	}{
		{ModNone, ""},
		{ModShift, "shift"},
		{ModAlt, "alt"},
		{ModCtrl, "ctrl"},
		{ModSuper, "super"},
		{ModCtrl | ModShift, "shift+ctrl"},
		{ModCtrl | ModAlt | ModShift | ModSuper, "shift+alt+ctrl+super"},
	}
	for _, tc := range tests {
		if got := tc.mod.String(); got != tc.want {
			t.Errorf("Mod(%d).String() = %q, want %q", tc.mod, got, tc.want)
		}
	}
}

func TestModHas(t *testing.T) {
	m := ModCtrl | ModShift
	if !m.Has(ModCtrl) {
		t.Error("should contain ctrl")
	}
	if !m.Has(ModShift) {
		t.Error("should contain shift")
	}
	if m.Has(ModCtrl | ModAlt) {
		t.Error("should not report alt")
	}
	if !ModNone.Has(ModNone) {
		t.Error("no modifiers trivially contains no modifiers")
	}
}

func TestModBitValuesAreDistinct(t *testing.T) {
	// These constants were spelled out precisely because an iota block placed
	// after a zero value silently starts at 2. Guard the invariant.
	mods := []Mod{ModShift, ModAlt, ModCtrl, ModSuper}
	for i := range mods {
		if mods[i] == ModNone {
			t.Errorf("modifier %d collides with ModNone", i)
		}
		for j := i + 1; j < len(mods); j++ {
			if mods[i] == mods[j] {
				t.Errorf("modifiers %d and %d have the same value %d", i, j, mods[i])
			}
			if mods[i]&mods[j] != 0 {
				t.Errorf("modifiers %d and %d share bits", i, j)
			}
		}
	}
}

func TestIsNamedKey(t *testing.T) {
	if !IsNamedKey("enter") || !IsNamedKey("pgup") {
		t.Error("expected names should be recognised")
	}
	if IsNamedKey("nope") {
		t.Error("unknown name should be rejected")
	}
}

func TestFormatKeysRoundTrip(t *testing.T) {
	specs := []string{"ctrl+q", "enter", "shift+tab", "alt+pgup"}
	keys, err := ParseKeys(specs)
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatKeys(keys); !slices.Equal(got, specs) {
		t.Errorf("round trip: want %v, got %v", specs, got)
	}
}

func TestHumanise(t *testing.T) {
	tests := []struct {
		action Action
		want   string
	}{
		{ActionToggleMute, "chat: toggle mute"},
		{NavUp, "nav: up"},
		{SendMessage, "composer: send"},
		{"bare", "bare"},
	}
	for _, tc := range tests {
		if got := helpText(Binding{Action: tc.action}); got != tc.want {
			t.Errorf("%s: want %q, got %q", tc.action, tc.want, got)
		}
	}
}

func TestExplicitHelpWinsOverHumanised(t *testing.T) {
	b := Binding{Action: ActionQuit, Help: "Quit, or close the top overlay"}
	if got := helpText(b); got != "Quit, or close the top overlay" {
		t.Errorf("explicit help should win, got %q", got)
	}
}

// TestConflictsCompareEveryPairNotJustTheFirst is the regression test for the
// detector's original defect.
//
// A key claimed once in the sidebar and twice in the message list is a conflict between
// the two message-list claims. Comparing every claim against only the first would call
// all three unambiguous, because the first is in a different panel — which is how
// ctrl+e was bound to both "edit message" and "scroll down" while the startup check
// reported nothing.
func TestConflictsCompareEveryPairNotJustTheFirst(t *testing.T) {
	m := NewMap([]Binding{
		{Action: ActionToggleArchive, Panel: PanelSidebar, Keys: []Key{{Rune: 'e'}}},
		{Action: ActionEdit, Panel: PanelMessages, Keys: []Key{{Rune: 'e'}}},
		{Action: ScrollDown, Panel: PanelMessages, Keys: []Key{{Rune: 'e'}}},
	})

	c := m.Conflicts()
	if len(c) != 1 {
		t.Fatalf("want 1 conflict, got %d: %+v", len(c), c)
	}
	if !slices.Equal(c[0].Actions, []Action{ActionEdit, ScrollDown}) {
		t.Errorf("the conflicting pair is the two in the same panel, got %v", c[0].Actions)
	}
}

// TestConflictsIgnoreDifferentPanels is the other half: two panel bindings on different
// panels are never live at the same time and must not be reported.
func TestConflictsIgnoreDifferentPanels(t *testing.T) {
	m := NewMap([]Binding{
		{Action: ActionToggleArchive, Panel: PanelSidebar, Keys: []Key{{Rune: 'e'}}},
		{Action: ActionEdit, Panel: PanelMessages, Keys: []Key{{Rune: 'e'}}},
	})
	if c := m.Conflicts(); len(c) != 0 {
		t.Errorf("bindings in different panels are not simultaneous: %+v", c)
	}
}

// TestTheSameActionTwiceIsNotAConflict covers the duplicate case, which the defaults
// rely on: one action may legitimately carry several keys.
func TestTheSameActionTwiceIsNotAConflict(t *testing.T) {
	m := NewMap([]Binding{
		{Action: ActionQuit, Global: true, Keys: []Key{{Rune: 'q'}, {Rune: 'x'}}},
	})
	if c := m.Conflicts(); len(c) != 0 {
		t.Errorf("one action with several keys is not a conflict: %+v", c)
	}
}
