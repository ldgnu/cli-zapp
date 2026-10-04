// Package palette implements the command palette.
//
// # Why it exists
//
// A keyboard-driven client accumulates actions faster than any key table can hold
// them. Bindings cover the frequent; the palette covers everything. It is also the
// only discoverable interface for the actions that have no binding at all, which
// is why it is reachable in one keystroke rather than hidden behind a menu.
//
// # It names commands, it does not run them
//
// The palette filters a list of [component.Command] values and emits the chosen
// one. It has no idea what "toggle mute" means. A palette that dispatched actions
// itself would be a second application, and every command added would be a branch
// in two places.
package palette

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/text"
	"github.com/cli-zapp/cli-zapp/internal/ui/component"
	"github.com/cli-zapp/cli-zapp/internal/ui/layout"
	"github.com/cli-zapp/cli-zapp/internal/ui/theme"
)

// entry is one filtered row.
type entry struct {
	cmd   component.Command
	score int
}

// Model is the palette's state.
type Model struct {
	rect  layout.Rect
	theme theme.Theme

	open   bool
	query  string
	cursor int
	// all is the full command list; visible is what the query filters to.
	all     []component.Command
	visible []entry
	// geom is the last geometry the caller computed, so the palette can draw
	// itself centred without knowing the terminal size.
	geom layout.Rect
}

// New creates a closed palette over the given commands.
func New(t theme.Theme) *Model {
	return &Model{theme: t}
}

// SetCommands replaces the command list.
func (m *Model) SetCommands(cmds []component.Command) {
	m.all = cmds
	m.refilter()
}

// Open shows the palette, optionally with a prefilled query.
//
// A prefilled query is how the palette is used as a search: pressing a key that
// opens it with that key's character as the query turns "find the command for mute"
// into one keystroke.
func (m *Model) Open(query string) tea.Cmd {
	previous := m.cursor

	m.open = true
	m.query = query
	m.refilter()

	if query != "" {
		// A query changes what is listed, so the previous position means nothing.
		m.cursor = 0
		return nil
	}

	// Reopening with no query puts the cursor back where it was.
	//
	// Opening and dismissing the palette repeatedly is the common way it is used, and
	// a palette that jumps back to the top every time reads as though it forgot what
	// the user was doing.
	m.cursor = clamp(previous, 0, maxInt(len(m.visible)-1, 0))
	return nil
}

// Close hides the palette.
func (m *Model) Close() {
	m.open = false
	m.query = ""
	m.visible = nil
}

// IsOpen reports whether the palette is showing.
func (m *Model) IsOpen() bool { return m.open }

// Query returns the current filter text.
func (m *Model) Query() string { return m.query }

// Resize implements the geometry the caller wants the palette drawn into.
func (m *Model) Resize(r layout.Rect) {
	m.rect = r
	m.geom = r
}

// Name implements [component.Region].
//
// The palette is a region so it can be driven through the same Update path as the
// others, even though it draws over everything rather than inside a zone.
func (m *Model) Name() string { return "palette" }

// Focused reports whether the palette owns the keyboard.
func (m *Model) Focused() bool { return m.open }

// Focus and Blur are no-ops: the palette's open state is the focus.
func (m *Model) Focus() tea.Cmd { return nil }
func (m *Model) Blur()          {}

// Update handles a key press while the palette is open.
func (m *Model) Update(msg tea.Msg) (component.Region, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || !m.open {
		return m, nil
	}

	switch component.Named(key) {
	case "esc":
		m.Close()
		return m, component.EventCmd(component.Event{Kind: component.KindDismissOverlay})
	case "enter":
		return m.choose()
	case "up", "ctrl+k":
		m.move(-1)
		return m, nil
	case "down", "ctrl+j":
		m.move(1)
		return m, nil
	case "backspace":
		if m.query != "" {
			r := []rune(m.query)
			m.query = string(r[:len(r)-1])
			m.refilter()
		}
		return m, nil
	}

	if component.IsPrintable(key) {
		m.query += key.Text
		m.refilter()
	}
	return m, nil
}

// move changes the highlighted entry.
func (m *Model) move(delta int) {
	if len(m.visible) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = clamp(m.cursor+delta, 0, len(m.visible)-1)
}

// choose emits the highlighted command.
func (m *Model) choose() (component.Region, tea.Cmd) {
	if len(m.visible) == 0 {
		m.Close()
		return m, component.EventCmd(component.Event{Kind: component.KindDismissOverlay})
	}

	cmd := m.visible[m.cursor].cmd
	if !cmd.Available {
		// A disabled command is shown rather than hidden, so the user can see it
		// exists. Choosing it must therefore do nothing rather than something
		// surprising.
		return m, nil
	}

	m.Close()
	return m, component.EventCmd(component.Event{Kind: component.KindCommandChosen, Command: cmd})
}

// refilter recomputes the visible entries from the query.
func (m *Model) refilter() {
	if !m.open {
		m.visible = nil
		return
	}

	q := strings.ToLower(strings.TrimSpace(m.query))
	out := make([]entry, 0, len(m.all))

	for _, c := range m.all {
		score, ok := match(c, q)
		if !ok {
			continue
		}
		out = append(out, entry{cmd: c, score: score})
	}

	// Sorted by score, descending, and stably — so with an empty query, where every
	// score is zero, the list is exactly the order the application declared. A palette
	// that reordered its own commands between openings would be impossible to learn.
	slices.SortStableFunc(out, func(a, b entry) int { return b.score - a.score })

	m.visible = out
	if m.cursor >= len(out) {
		m.cursor = maxInt(0, len(out)-1)
	}
}

// match scores a command against a lowercased query.
//
// The scoring is deliberately simple and explainable: an exact title match beats a
// prefix, which beats a word-start, which beats a substring. A fuzzy matcher with
// transposition handling scores better on paper and is worse in practice, because
// it ranks "mute" above "unmute" for the query "mute" — which is the opposite of
// what the user meant.
func match(c component.Command, q string) (int, bool) {
	if q == "" {
		return 0, true
	}

	title := strings.ToLower(c.Title)
	switch {
	case title == q:
		return 100, true
	case strings.HasPrefix(title, q):
		return 80, true
	}

	if strings.Contains(title, " "+q) {
		return 60, true
	}
	if strings.Contains(title, q) {
		return 40, true
	}

	// Fall back to the identifier and the shortcut, so the user can type either.
	if strings.Contains(strings.ToLower(c.ID), q) {
		return 30, true
	}
	for _, k := range shortcutKeys(c) {
		if strings.Contains(strings.ToLower(k), q) {
			return 20, true
		}
	}
	return 0, false
}

func shortcutKeys(c component.Command) []string {
	if c.Hint == "" {
		return nil
	}
	return strings.Fields(c.Hint)
}

// View renders the palette as a full-screen frame, with the box centred.
//
// Returning a frame the same size as the terminal — rather than just the box — is what
// lets the root layer it with the same one-line merge it uses for dialogs. A palette
// that returned only its own box would force the root to know how to centre it, which
// is a rendering decision that belongs here.
func (m *Model) View() string {
	if !m.open {
		return ""
	}
	box := m.viewBox()
	if box == "" {
		return ""
	}
	return centreFrame(box, m.geom)
}

// viewBox draws the palette's own bordered box, without the surrounding frame.
func (m *Model) viewBox() string {
	st := m.theme.Styles
	g := m.theme.Glyphs

	w := m.geom.Width
	if w <= 8 {
		return ""
	}

	// The palette is narrower than the terminal so that it reads as a layer rather
	// than as a page. On a narrow terminal it fills the width instead, because a
	// twenty-column dialog is worse than an edge-to-edge one.
	box := minInt(w-8, 76)
	if box < 24 {
		box = maxInt(w-8, 24)
	}
	inner := box - 2

	lines := make([]string, 0, 16)

	// The query line, which doubles as the prompt.
	prompt := st.PalettePrompt.Render(g.Command + " ")
	query := m.query
	if query == "" {
		query = st.Muted.Render("buscar un comando…")
	}
	lines = append(lines, text.PadRight(prompt+query, inner))
	lines = append(lines, st.Divider.Render(strings.Repeat(g.Divider, inner)))

	if len(m.visible) == 0 {
		lines = append(lines, st.EmptyState.Render(text.PadRight("sin coincidencias", inner)))
	}

	// The list is bounded so that a long command list cannot push the prompt off the
	// screen: the prompt is what the user is typing into, so it is the one row that
	// must always be visible.
	const maxListRows = 11
	section := ""
	for i, e := range m.visible {
		if len(lines)-2 >= maxListRows {
			break
		}
		if e.cmd.Section != section {
			section = e.cmd.Section
			lines = append(lines,
				st.PaletteSection.Render(text.PadRight(strings.ToUpper(section), inner)))
		}
		lines = append(lines, m.renderEntry(e.cmd, i == m.cursor, inner, st, g))
	}

	return st.Palette.Width(inner).Render(strings.Join(lines, "\n"))
}

// centreFrame places box in the middle of a blank frame of the given size.
//
// The box is returned blank-padded rather than transparent, so the caller merges it
// with the same rule it uses for dialogs: a non-blank cell wins.
func centreFrame(box string, frame layout.Rect) string {
	rows := strings.Split(box, "\n")

	canvas := make([]string, frame.Height)
	for i := range canvas {
		canvas[i] = strings.Repeat(" ", frame.Width)
	}

	// The box sits above centre: a palette pinned to the middle of the screen is half
	// obscured by the conversation it is filtering, and the command the user picked is
	// usually near the end of the list, which is below the middle.
	top := maxInt((frame.Height-len(rows))/3, 0)
	if top+len(rows) > frame.Height {
		top = maxInt(frame.Height-len(rows), 0)
	}

	for i, row := range rows {
		y := top + i
		if y < 0 || y >= frame.Height {
			continue
		}
		canvas[y] = centrePlain(row, frame.Width)
	}

	return strings.Join(canvas, "\n")
}

// centrePlain centres a possibly styled line within width cells.
func centrePlain(s string, width int) string {
	pad := width - text.VisibleWidth(s)
	if pad <= 0 {
		return text.TruncateStyled(s, width)
	}
	left := pad / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", pad-left)
}

// cursorW is the width of the selection marker in the left margin.
//
// The marker is drawn as well as coloured. A palette whose only indication of the
// cursor is a background colour is unusable on a monochrome terminal and ambiguous for
// anyone who cannot rely on colour, and the marker costs one column of a list that has
// room for it.
const cursorW = 2

// renderEntry draws one command row.
func (m *Model) renderEntry(
	c component.Command, selected bool, inner int, st theme.Styles, g theme.Glyphs,
) string {
	label := c.Title
	switch {
	case !c.Available:
		label = st.Muted.Render(label)
	case c.Danger:
		label = st.Error.Render(label)
	}

	// The shortcut is right-aligned within the title's own width, so the titles of the
	// rows above it stay flush left however long a shortcut is.
	titleW := maxInt(inner-text.VisibleWidth(c.Hint)-1, 0)
	row := text.PadRight(text.Truncate(label, titleW), titleW)
	if c.Hint != "" {
		row += " " + st.StatusLabel.Render(c.Hint)
	}

	marker := strings.Repeat(" ", cursorW)
	if selected {
		marker = st.Accent.Render(g.Marker + " ")
	}

	if selected {
		return marker + st.PaletteCursor.Render(text.PadRight(row, inner-cursorW))
	}
	return marker + st.PaletteItem.Render(text.PadRight(row, inner-cursorW))
}

// Cursor returns the index of the highlighted command.
//
// It is exported so that a caller can label the palette — a status line saying which
// command is selected, say — without the palette having to draw it.
func (m *Model) Cursor() int { return m.cursor }

// Matched returns how many commands pass the current query.
func (m *Model) Matched() int { return len(m.visible) }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return minInt(maxInt(v, lo), hi)
}
