package keybindings

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Binding associates an [Action] with the keys that trigger it.
type Binding struct {
	Action Action
	Keys   []Key
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
	if a, ok := m.byKey[k]; ok {
		return a
	}
	if table, ok := m.panelByKey[focus]; ok {
		if a, ok := table[k]; ok {
			return a
		}
	}
	return ActionNone
}

// ResolveGlobal maps a key press to a globally bound action, ignoring focus.
//
// This exists because a keyboard-driven client must give its application-wide
// shortcuts — quit, search, resync — priority over whatever the focused widget
// would otherwise do with the key. A text field will happily report that it
// consumed ctrl+q, so the global bindings have to be consulted before the field
// sees the press at all.
func (m Map) ResolveGlobal(k Key) Action {
	return m.byKey[k]
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
		// Two claims conflict only if both can be live at the same moment:
		// either one is global, or they target the same panel.
		var live []Action
		for _, a := range claims[:1] {
			live = append(live, a.action)
		}
		for _, b := range claims[1:] {
			if b.global || claims[0].global || b.panel == claims[0].panel {
				live = append(live, b.action)
			}
		}
		if len(live) < 2 {
			continue
		}
		sort.Slice(live, func(i, j int) bool { return live[i] < live[j] })
		out = append(out, Conflict{Key: k, Actions: live})
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
	Help   string
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
			Panel:  b.Panel,
			Global: b.Global,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Action < out[j].Action })
	return out
}

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
