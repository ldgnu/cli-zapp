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

// The weights a fuzzy score is built from.
//
// They are package-level rather than local to [match] because [fuzzyScore] needs them
// too, and two copies of a scoring weight that have to agree are two copies that will
// eventually not.
const (
	// fuzzyBonusRun is added per character already matched, for continuing a run.
	// It is cumulative, so a long consecutive run is worth far more than a short one.
	fuzzyBonusRun = 120
	// fuzzyBonusWord is added for a match that begins a word.
	fuzzyBonusWord = 60
	// fuzzyBonusStart is added for a match at the very beginning of the title.
	fuzzyBonusStart = 40
	// fuzzyLengthPenalty is subtracted per rune of title the user skipped, which is
	// what makes a tight match rank above a scattered one.
	fuzzyLengthPenalty = 2
)

// match scores a command against a lowercased query, or reports that it does not match.
//
// # What fuzzy means here
//
// A subsequence match, scored by how tight the match is rather than merely whether one
// exists. Typing "ncr" finds "Marcar como no leída" because those letters appear in
// order across a word boundary, and it ranks that above a scattered match like "ntcar",
// which is what makes a fuzzy palette usable rather than a slot machine.
//
// Four things raise a score, in the order a user would want them to:
//
//   - consecutive runs, so "sc" beats a match with the same letters apart
//   - matches at a word boundary, so "s" finds "Marcar" before "Responder"
//   - matches at the start, so a prefix beats a match buried in the middle
//   - a shorter title, so "Sync" beats "Sync and refetch everything" for "sync"
//
// # What is deliberately not fuzzy
//
// The exact and prefix cases are checked before any subsequence, and score above every
// fuzzy result. A query of "mute" therefore ranks "Silenciar conversación" — which
// contains those letters in order across a boundary — below a command called exactly
// "Mute", and never ranks "Unmute" above either.
//
// That is the failure mode a naive fuzzy matcher has, and it is the reason a palette
// with one is unusable: the wrong item is highlighted, and pressing enter does the
// wrong thing to a conversation.
func match(c component.Command, q string) (int, bool) {
	if q == "" {
		return 0, true
	}

	// The score bands. The gaps between them are wider than any single bonus, so a
	// strong fuzzy match can never outrank a weak exact one.
	const (
		scoreExact    = 10000
		scorePrefix   = 9000
		scoreBoundary = 8000
		scoreSubstr   = 7000
		scoreFuzzy    = 1000
	)

	title := strings.ToLower(c.Title)

	switch {
	case title == q:
		return scoreExact, true
	case strings.HasPrefix(title, q):
		return scorePrefix, true
	case strings.Contains(title, " "+q):
		return scoreBoundary, true
	case strings.Contains(title, q):
		return scoreSubstr, true
	}

	// The identifier and the shortcut are searched too, so a user who remembers the
	// key rather than the name can type it. Scored below every title match, because a
	// title is what the user was looking at when they started typing.
	if strings.Contains(strings.ToLower(c.ID), q) {
		return scoreFuzzy, true
	}
	for _, k := range shortcutKeys(c) {
		if strings.Contains(strings.ToLower(k), q) {
			return scoreFuzzy, true
		}
	}

	score, ok := fuzzyScore(title, q)
	if !ok {
		return 0, false
	}

	// The penalty is per rune of title the user had to skip over, which is the fuzziest
	// part of "fuzzy" and the one that makes a result feel arbitrary without it.
	return scoreFuzzy + score - len([]rune(title))*fuzzyLengthPenalty, true
}

// fuzzyScore scores a subsequence match, or reports that the query is not a subsequence
// of the title.
//
// A subsequence match is the weakest possible guarantee — "abc" matches "a big cat" —
// so everything here is about making the good cases rank far above the bad ones rather
// than about whether they match at all.
func fuzzyScore(s, q string) (int, bool) {
	if q == "" {
		return 0, true
	}

	runes := []rune(s)
	qi := 0
	score := 0
	// run is how many consecutive characters have matched so far; a run that breaks
	// costs more than the run was worth, which is what separates "abc" from "a b c".
	run := 0
	lastMatch := -2 // so a match at index 0 does not look consecutive

	for i, r := range runes {
		if qi >= len(q) {
			break
		}
		if r != []rune(q)[qi] {
			if run > 0 {
				score -= run
				run = 0
			}
			continue
		}

		score++
		if i == lastMatch+1 {
			// Continuing a run is worth more than starting one, so "sc" outranks a
			// match with the same letters scattered.
			score += fuzzyBonusRun * run
			run++
		} else {
			run = 1
		}

		switch {
		case i == 0:
			score += fuzzyBonusStart
		case isWordBoundary(runes, i):
			// A match that starts a word is almost always what the user meant, which is
			// why "n" finds "Marcar" and not only "Responder".
			score += fuzzyBonusWord
		}

		lastMatch = i
		qi++
	}

	if qi < len(q) {
		return 0, false
	}
	return score, true
}

// isWordBoundary reports whether the rune at i starts a word.
//
// A boundary is the start of the string or a position after a space, a dash, a
// parenthesis or a slash. Punctuation counts because command titles are phrases, not
// single words, and the space is the boundary that matters.
func isWordBoundary(runes []rune, i int) bool {
	if i == 0 {
		return false
	}
	switch runes[i-1] {
	case ' ', '-', '_', '/', '(', '[', '.', ',', ':':
		return true
	default:
		return false
	}
}

func shortcutKeys(c component.Command) []string {
	if c.Shortcut == "" {
		return nil
	}
	return strings.Fields(c.Shortcut)
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

	// The list is bounded so that a long command list cannot push the prompt or the
	// description off the screen: both are rows the user cannot do without.
	const maxListRows = 13
	// The description is added after the list, so the budget leaves room for it.
	budget := maxListRows - 2

	section := ""
	for i, e := range m.visible {
		if len(lines)-2 >= budget {
			break
		}
		if e.cmd.Category != section {
			section = e.cmd.Category
			lines = append(lines,
				st.PaletteSection.Render(text.PadRight(strings.ToUpper(section), inner)))
		}
		lines = append(lines, m.renderEntry(e.cmd, i == m.cursor, inner, st, g))
	}

	// The highlighted command's description, below the list and behind a rule.
	//
	// One row for the whole palette rather than one per command: a description beside
	// every entry would double the list's height and turn a scannable menu into a wall,
	// while a description only for the highlighted entry is exactly what the user cannot
	// read without moving the cursor.
	//
	// Below the list, not above it: the list is the thing being scanned, and a row the
	// eye has to skip past on the way to the first command is in the wrong place.
	if desc := m.highlightedDescription(); desc != "" {
		lines = append(lines, st.Divider.Render(strings.Repeat(g.Divider, inner)))
		lines = append(lines, st.Muted.Render(text.Truncate(desc, inner)))
	}

	// Width includes the border, and the style also has a column of padding on each
	// side, so the visible content is four cells narrower than the width given here.
	//
	// Getting this wrong is invisible in the source and obvious on screen: every entry
	// wraps its shortcut onto a second line, and a list of twenty commands becomes forty
	// rows with the prompt pushed off the top.
	return st.Palette.Width(inner + 4).Render(strings.Join(lines, "\n"))
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

// highlightedDescription returns the highlighted command's description, or "".
func (m *Model) highlightedDescription() string {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return ""
	}
	return m.visible[m.cursor].cmd.Description
}

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

	// The shortcut is right-aligned within the row, so the titles of the rows above it
	// stay flush left however long a shortcut is.
	//
	// The second argument to maxInt is the value to use when the subtraction goes
	// negative, and it has to be zero rather than the row width: passing the width there
	// made every entry wrap its shortcut onto a second line, which doubled the list's
	// height and turned it into a wall.
	shortcutW := text.VisibleWidth(c.Shortcut)
	titleW := inner
	if shortcutW > 0 {
		titleW = maxInt(inner-shortcutW-1, 0)
	}

	row := text.PadRight(text.Truncate(label, titleW), titleW)
	if shortcutW > 0 && titleW > 0 {
		row += " " + st.StatusLabel.Render(c.Shortcut)
	} else {
		// Too narrow for both. The title is the command; a shortcut the user cannot
		// read is worse than none, and the palette is the place to look it up anyway.
		row = text.PadRight(text.Truncate(label, inner), inner)
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
