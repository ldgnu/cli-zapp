package keybindings

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// Binding associates an [Action] with the keys that trigger it.
type Binding struct {
	Action Action
	Keys   []Key

	// Short is the one-word label the status bar shows for this action.
	//
	// It is declared beside the sentence rather than derived from it, because a bar
	// that truncated the sentence would put "Cancelar la ac…" on screen. An action with
	// no short label simply does not appear in the bar.
	Short string

	// Panel restricts the binding to one focus region. A nil Panel means the
	// binding is global.
	//
	// Restricting a binding matters for keys that panels need for themselves:
	// "mod+j" is free to mean "next chat" globally, but if it also scrolled the
	// message list the two would fight.
	Panel Panel
	// Global forces the binding to fire regardless of focus.
	Global bool
	// Help is the label shown in the cheat sheet. When empty the action name
	// is humanised.
	Help string
}

// Map resolves key presses to actions.
//
// A Map is immutable once built. [NewMap] returns a value, and remapping
// produces a new one, which makes it safe to share a Map across the whole UI
// without locking.
type Map struct {
	// byKey is the reverse index, built once. Global bindings are checked
	// before panel bindings so that a global binding can shadow a panel one.
	byKey map[Key]Action
	// panelByKey holds panel-scoped bindings, keyed by panel then key.
	panelByKey map[Panel]map[Key]Action
	// bindings retains declaration order for help rendering.
	bindings []Binding
}

// NewMap builds a Map from bindings.
//
// The returned Map is not a pointer: copies are free, and sharing one value
// across goroutines is safe because nothing mutates it after construction.
func NewMap(bindings []Binding) Map {
	m := Map{
		byKey:      make(map[Key]Action, len(bindings)*2),
		panelByKey: make(map[Panel]map[Key]Action, 3),
		bindings:   append([]Binding(nil), bindings...),
	}
	for _, b := range bindings {
		if b.Global {
			for _, k := range b.Keys {
				m.byKey[k] = b.Action
			}
			continue
		}
		table, ok := m.panelByKey[b.Panel]
		if !ok {
			table = make(map[Key]Action, len(b.Keys))
			m.panelByKey[b.Panel] = table
		}
		for _, k := range b.Keys {
			table[k] = b.Action
		}
	}
	return m
}

// Resolve maps a key press to an action for the given focus.
//
// Resolution order is global bindings first, then the focused panel's bindings.
// That order lets a user bind the same key globally and per-panel with
// predictable results rather than depending on map iteration order.
//
// A printable key with no modifier returns [ActionNone] when unbound, which is
// the signal that a focused text field should consume it.
func (m Map) Resolve(k Key, focus Panel) Action {
	if a, ok := m.lookup(m.byKey, k); ok {
		return a
	}
	if table, ok := m.panelByKey[focus]; ok {
		if a, ok := m.lookup(table, k); ok {
			return a
		}
	}
	return ActionNone
}

// lookup finds k in a binding table, tolerating a redundant shift.
//
// # Why shift is optional for a rune
//
// Terminals disagree about how they report a punctuation key. A keyboard has no '?'
// key: the user presses shift and '/', and depending on the emulator and the
// terminfo entry the application receives either Code '?' with no modifier or
// Code '?' with ModShift. A binding table that insisted on the exact modifier set
// would therefore match ctrl+? on one terminal and not on the next — a bug that
// looks like the shortcut is broken rather than like the two sides disagree about
// what the key is.
//
// So a lookup on a rune key retries without ModShift, and only if the exact key
// missed. Named keys are unaffected: shift+tab is a genuinely different key from
// tab, and treating them as the same would make the two indistinguishable.
//
// The exact match is tried first so that a user who deliberately bound both
// ctrl+a and ctrl+shift+a gets what they wrote.
func (m Map) lookup(table map[Key]Action, k Key) (Action, bool) {
	if a, ok := table[k]; ok {
		return a, true
	}
	if k.Rune != 0 && k.Mod.Has(ModShift) && k.Rune < utf8.RuneSelf {
		if a, ok := table[Key{Rune: k.Rune, Mod: k.Mod &^ ModShift}]; ok {
			return a, true
		}
	}
	return ActionNone, false
}

// ResolveGlobal maps a key press to a globally bound action, ignoring focus.
//
// This exists because a keyboard-driven client must give its application-wide
// shortcuts — quit, search, resync — priority over whatever the focused widget
// would otherwise do with the key. A text field will happily report that it
// consumed ctrl+q, so the global bindings have to be consulted before the field
// sees the press at all.
func (m Map) ResolveGlobal(k Key) Action {
	a, _ := m.lookup(m.byKey, k)
	return a
}

// Has reports whether any binding in the map triggers a.
func (m Map) Has(a Action) bool {
	for _, b := range m.bindings {
		if b.Action == a {
			return true
		}
	}
	return false
}

// Bindings returns the bindings in declaration order.
//
// The returned slice is a copy; mutating it does not affect the Map.
func (m Map) Bindings() []Binding {
	out := make([]Binding, 0, len(m.bindings))
	for _, b := range m.bindings {
		b.Keys = append([]Key(nil), b.Keys...)
		out = append(out, b)
	}
	return out
}

// Keys returns the keys bound to a, or nil.
//
// The caller owns the returned slice. Mutating it is harmless but pointless;
// construct a new Map to change a binding.
func (m Map) Keys(a Action) []Key {
	for _, b := range m.bindings {
		if b.Action == a {
			return slices.Clone(b.Keys)
		}
	}
	return nil
}

// Conflict is one key bound to more than one action.
type Conflict struct {
	Key     Key
	Actions []Action
}

// Conflicts reports keys bound to more than one action.
//
// Global bindings are compared against each other and against all panel
// bindings, since a global binding shadows panel ones. Two panel bindings on
// different panels are not a conflict, because they are never live at the same
// time.
//
// Callers surface this at startup. A shadowed binding is the kind of defect
// that otherwise presents to the user as "this key does nothing", which is very
// hard to diagnose from inside the application.
func (m Map) Conflicts() []Conflict {
	type claim struct {
		action Action
		panel  Panel
		global bool
	}
	seen := make(map[Key][]claim)

	for _, b := range m.bindings {
		for _, k := range b.Keys {
			c := claim{action: b.Action, panel: b.Panel, global: b.Global}
			// The same action bound twice to the same scope is a duplicate, not
			// a conflict. The same action bound globally and per-panel is
			// merely redundant, not ambiguous.
			dup := false
			for _, existing := range seen[k] {
				if existing.action == c.action && existing.panel == c.panel && existing.global == c.global {
					dup = true
					break
				}
			}
			if !dup {
				seen[k] = append(seen[k], c)
			}
		}
	}

	var out []Conflict
	for k, claims := range seen {
		// Compared pairwise, not against the first claim.
		//
		// Comparing everything to claims[0] misses the case that matters most: a key
		// claimed once in the sidebar and twice in the message list is not ambiguous
		// because the sidebar's claim is in a different panel, but the two claims in
		// the same panel are — and the first-claim comparison calls all three
		// unambiguous. That is how ctrl+e was bound to both "edit message" and "scroll
		// down" with a conflict check that reported nothing.
		var live []Action
		for a := range claims {
			for _, y2 := range claims[a+1:] {
				// The second claim comes from ranging over the sub-slice, so it is the
				// value rather than an index. Taking an index here and using it against
				// claims compares the first claim with itself, which passes the check
				// vacuously — a conflict detector that can never report anything.
				x, y := claims[a], y2
				if x.action == y.action {
					continue
				}
				// Two claims can be live at the same moment when either is global, or
				// when they target the same panel.
				if !x.global && !y.global && x.panel != y.panel {
					continue
				}
				live = append(live, x.action, y.action)
			}
		}
		if len(live) == 0 {
			continue
		}

		slices.Sort(live)
		out = append(out, Conflict{Key: k, Actions: slices.Compact(live)})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Key.String() < out[j].Key.String() })
	return out
}

// Check reports whether the map is free of conflicts, returning a
// human-readable summary suitable for a startup error.
func (m Map) Check() error {
	conflicts := m.Conflicts()
	if len(conflicts) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d keybinding conflict(s):", len(conflicts))
	for _, c := range conflicts {
		fmt.Fprintf(&b, "\n  %s → %s", c.Key, joinActions(c.Actions))
	}
	return fmt.Errorf("keybindings: %s", b.String())
}

func joinActions(actions []Action) string {
	parts := make([]string, 0, len(actions))
	for _, a := range actions {
		parts = append(parts, string(a))
	}
	return strings.Join(parts, ", ")
}

// HelpEntry is one row of the cheat sheet.
type HelpEntry struct {
	Action Action
	Keys   []string
	// Help is the sentence the cheat sheet shows.
	Help string
	// Short is the one-word label the status bar shows.
	//
	// Two spellings of the same fact exist because the two surfaces have different
	// budgets: the sheet has a scrollback and the bar has one line it shares with the
	// connection state. Deriving the short form by truncating the sentence produces
	// "Cancelar la ac…" on screen, which is worse than no hint at all.
	Short  string
	Panel  Panel
	Global bool
}

// Help returns the cheat-sheet rows for the given focus, sorted by action name.
//
// Panel-scoped bindings for panels other than focus are excluded, because the
// cheat sheet documents what the user can do *now*.
func (m Map) Help(focus Panel) []HelpEntry {
	out := make([]HelpEntry, 0, len(m.bindings))
	for _, b := range m.bindings {
		if !b.Global && b.Panel != focus {
			continue
		}
		out = append(out, HelpEntry{
			Action: b.Action,
			Keys:   FormatKeys(b.Keys),
			Help:   helpText(b),
			Short:  b.Short,
			Panel:  b.Panel,
			Global: b.Global,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Action < out[j].Action })
	return out
}

// Hints returns the status bar's key hints for a focus, in the order they are shown.
//
// The order is a declaration-order preference rather than alphabetical: a hint bar that
// lists actions in the order of their identifier names is a bar nobody reads, because
// the useful ones are not adjacent. Panel bindings come first, since they are the ones
// that act on what is under the user's hands, and quit comes last because it is the
// thing a user already knows.
func (m Map) Hints(focus Panel) []HelpEntry {
	entries := m.Help(focus)
	rank := map[Action]int{}
	order := append(slices.Clone(hintOrder[focus]), alwaysLast)
	for i, a := range order {
		rank[a] = i + 1
	}

	out := make([]HelpEntry, 0, len(entries))
	for _, e := range entries {
		// A hint with no short label is one the bar has nothing useful to say about:
		// showing the key alone tells the user nothing they could not work out.
		if strings.TrimSpace(e.Short) == "" || len(e.Keys) == 0 {
			continue
		}
		out = append(out, e)
	}

	sort.SliceStable(out, func(i, j int) bool {
		ri, oki := rank[out[i].Action]
		rj, okj := rank[out[j].Action]
		switch {
		case oki && okj:
			return ri < rj
		case oki:
			return true
		case okj:
			return false
		default:
			return false
		}
	})
	return out
}

// hintOrder is the order the bar shows each panel's hints in.
//
// Only the actions worth a keystroke of attention are listed. A hint bar that tries to
// mention every binding shows none of them: at a hundred columns there is room for three
// short labels, and the three that matter are always the same.
var hintOrder = map[Panel][]Action{
	PanelSidebar: {
		ChatOpen, ActionToggleRead, ActionToggleMute, ActionNewChat, ActionPalette,
	},
	PanelMessages: {
		ActionReply, ActionReact, ActionCopy, ActionSelect, ActionInfo,
	},
	PanelComposer: {
		SendMessage, InsertNewline, ActionPalette,
	},
}

// alwaysLast is appended to every panel's order, so the bar ends the same way whatever
// is focused: quitting is the one action a user already knows, so it belongs last.
var alwaysLast = ActionQuit

// helpText returns the binding's help label, humanising the action name when
// no explicit label was supplied.
func helpText(b Binding) string {
	if b.Help != "" {
		return b.Help
	}
	return humanise(string(b.Action))
}

// humanise turns "chat.toggle_mute" into "chat: toggle mute".
func humanise(s string) string {
	prefix, rest, ok := strings.Cut(s, ".")
	if !ok {
		return s
	}
	return strings.ReplaceAll(prefix, "_", " ") + ": " + strings.ReplaceAll(rest, "_", " ")
}

// The pairwise comparison in [Map.Conflicts] is exercised directly by the tests with
// three claims on one key, which is the shape the defaults did not have. The regression
// it guards is subtle: a comparison that is subtly wrong in the direction of "no
// conflict" is invisible, because a table with no real conflicts looks identical to a
// checker that cannot find any.
