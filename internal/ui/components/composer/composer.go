// Package composer renders the message input.
//
// # Multiline, and why enter still sends
//
// The input holds multiple lines, because pasting a stack trace into a chat is
// normal. It does not follow: enter sends, because every terminal messenger works
// that way and a chat box where enter inserts a newline is unusable at a prompt.
// A newline goes through alt+enter or shift+enter, and the hint line says so.
//
// # It owns its buffer
//
// The caret, the buffer and the scroll offset have to survive across frames, and
// keeping them in the caller's state would mean copying the whole buffer on every
// keystroke. The composer therefore holds the value and reports it through an
// event, rather than the application reaching in to read it.
package composer

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/text"
	"github.com/cli-zapp/cli-zapp/internal/ui/component"
	"github.com/cli-zapp/cli-zapp/internal/ui/layout"
	"github.com/cli-zapp/cli-zapp/internal/ui/theme"
)

// MaxHintLines bounds how many lines of hint the composer shows.
const MaxHintLines = 1

// Model is the composer's state.
type Model struct {
	rect   layout.Rect
	focus  bool
	keys   keybindings.Map
	theme  theme.Theme
	ta     textarea.Model
	width  int
	height int

	// mode describes what the composer will do with the next send.
	mode Mode
	// replyTo names the message being replied to.
	replyTo string
	// editing names the message being edited.
	editing string
	// showHint renders the contextual hint inside the empty composer.
	showHint bool
}

// Mode is what the composer is currently set up to do.
type Mode int

// Recognised composer modes.
const (
	// ModeSend is the ordinary case.
	ModeSend Mode = iota
	// ModeReply sends with a quote.
	ModeReply
	// ModeEdit replaces an existing message.
	ModeEdit
)

// String implements fmt.Stringer.
func (m Mode) String() string {
	switch m {
	case ModeReply:
		return "reply"
	case ModeEdit:
		return "edit"
	default:
		return "send"
	}
}

// New creates a composer.
func New(keys keybindings.Map, t theme.Theme) *Model {
	m := &Model{keys: keys, theme: t}
	m.ta = m.newTextarea()
	return m
}

// newTextarea builds the underlying input widget.
func (m *Model) newTextarea() textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // the protocol caps message length, not the input
	ta.Placeholder = "Escribir mensaje…"

	// A real terminal caret rather than a virtual one: the virtual block is drawn
	// inside the widget in its own coordinates, which cannot be positioned against
	// the rest of the frame. The root places the real cursor.
	ta.SetVirtualCursor(false)
	ta.SetPromptFunc(0, func(textarea.PromptInfo) string { return "" })

	st := m.theme.Styles
	isDark := m.theme.Name != theme.NameLight

	styles := textarea.DefaultStyles(isDark)
	// The cursor line is not highlighted: a full-width band behind the editing line
	// looks nothing like the rest of the interface and fights the border.
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Blurred.CursorLine = lipgloss.NewStyle()
	styles.Focused.Placeholder = st.ComposerHint
	styles.Blurred.Placeholder = st.ComposerHint
	styles.Focused.Text = st.Composer
	styles.Blurred.Text = st.ComposerHint
	styles.Focused.Selection = st.Selection
	styles.Blurred.Selection = st.Selection
	ta.SetStyles(styles)

	return ta
}

// Name implements [component.Region].
func (m *Model) Name() string { return component.RegionComposer }

// Resize implements [component.Region].
func (m *Model) Resize(r layout.Rect) {
	if r.Width == m.width && r.Height == m.height {
		return
	}
	m.rect = r
	m.width, m.height = r.Width, r.Height

	// The border consumes two columns and two rows; the prompt consumes one more.
	m.ta.SetWidth(maxInt(r.Width-3, 1))
	m.ta.SetHeight(maxInt(r.Height-2, 1))
}

// Focus implements [component.Region].
func (m *Model) Focus() tea.Cmd {
	m.focus = true
	m.ta.Focus()
	return nil
}

// Blur implements [component.Region].
func (m *Model) Blur() {
	m.focus = false
	m.ta.Blur()
}

// Focused implements [component.Region].
func (m *Model) Focused() bool { return m.focus }

// SetMode applies the layout mode, which decides whether the hint line is drawn.
func (m *Model) SetMode(mode layout.Mode) {
	m.showHint = mode == layout.ModeFull
}

// Mode returns what the composer will do with the next send.
func (m *Model) Mode() Mode { return m.mode }

// SetReply begins a reply to the named message.
func (m *Model) SetReply(name string) {
	m.mode = ModeReply
	m.replyTo = name
	m.editing = ""
}

// SetEdit begins editing the named message, pre-filling its body.
func (m *Model) SetEdit(name, body string) {
	m.mode = ModeEdit
	m.editing = name
	m.replyTo = ""
	m.ta.SetValue(body)
}

// ClearMode returns the composer to plain sending.
func (m *Model) ClearMode() {
	m.mode = ModeSend
	m.replyTo = ""
	m.editing = ""
}

// Value returns the current input text.
func (m *Model) Value() string { return m.ta.Value() }

// Empty reports whether there is nothing to send.
func (m *Model) Empty() bool { return strings.TrimSpace(m.ta.Value()) == "" }

// Clear empties the input.
func (m *Model) Clear() {
	m.ta.Reset()
	m.ta.MoveToEnd()
	m.ClearMode()
}

// Restore puts text and a mode back after the input was cleared.
//
// It exists because the composer clears its own buffer *before* emitting the submit
// event, which is the right order — the widget should not be holding a draft the user
// has already sent. But it means a send refused after that point, by the rate limiter,
// would silently eat what the user typed.
//
// Restoring is therefore not optional politeness: without it, hitting the limit
// deletes the message. The user would retype it, press enter again out of
// frustration, and be refused again — and would conclude the composer is broken
// rather than that they are sending too fast.
func (m *Model) Restore(body string, mode Mode) {
	m.ta.Reset()
	m.ta.InsertString(body)
	m.ta.MoveToEnd()

	// The mode is restored but not the reply/edit banners: those are held by the root,
	// which clears them on a successful send and leaves them alone on a refusal, so
	// re-setting the composer to reply-without-a-quote would show a mode the transcript
	// does not agree with.
	m.mode = mode
}

// Cursor exposes the caret position for the root to place the terminal cursor.
func (m *Model) Cursor() (x, y int, visible bool) {
	cur := m.ta.Cursor()
	if cur == nil {
		return 0, 0, false
	}
	return cur.X, cur.Y, true
}

// PromptX is the column at which the caret sits, accounting for the border and
// the prompt marker.
func (m *Model) PromptX() int { return m.rect.X + 2 }

// Update implements [component.Region].
func (m *Model) Update(msg tea.Msg) (component.Region, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) (component.Region, tea.Cmd) {
	// The application's own bindings win inside the composer.
	//
	// The switch is deliberately partial: the cases the composer does not claim fall
	// through to the text widget below, which is what turns an unbound printable key
	// into a character. A default that returned would make the composer swallow
	// everything, and a switch listing every action would say nothing about that.
	//
	//nolint:exhaustive // the fall-through is the behaviour, not an oversight
	switch m.keys.Resolve(component.ConvertKey(msg), keybindings.PanelComposer) {
	case keybindings.SendMessage:
		return m.submit()
	case keybindings.InsertNewline:
		m.ta.InsertString("\n")
		return m, nil
	case keybindings.ActionCancel:
		// Escape abandons the mode, not the draft. Clearing the draft would be the
		// more literal reading of "cancel" and the more annoying one: throwing away
		// what someone typed because they reached for the wrong key loses work they
		// have to retype, while leaving it costs nothing.
		if m.mode != ModeSend {
			m.ClearMode()
		}
		return m, nil
	}

	// Every action the composer does not claim falls through to the widget below,
	// which is where unbound printable keys become text. A default that returned would
	// make the composer swallow everything the application has not bound, which is the
	// opposite of what a text field is for.
	if !m.focus || !m.ta.Focused() {
		return m, nil
	}

	// Nothing with a control, alt or meta modifier reaches the text field.
	//
	// This is the composer's own guard rather than the application's, because the
	// composer is the region that owns a text field and so is the only place that
	// knows these keys are not text. Shift is allowed through, or capitals would be
	// impossible to type. The application's ordering already resolves global bindings
	// first, so this is a second line of defence rather than the only one — a key
	// that reaches here with ctrl held is a key the application did not bind, and
	// inserting a literal "q" for ctrl+q is never the right answer.
	if msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) != 0 {
		return m, nil
	}

	before := m.ta.Value()
	m.ta, _ = m.ta.Update(msg)
	if m.ta.Value() != before {
		return m, component.EventCmd(component.Event{
			Kind: component.KindSubmit, Text: "", Payload: m.ta.Value(),
		})
	}
	return m, nil
}

// submit hands the draft to the application.
func (m *Model) submit() (component.Region, tea.Cmd) {
	if m.Empty() {
		return m, nil
	}
	body := m.ta.Value()
	mode := m.mode
	m.Clear()

	return m, component.EventCmd(component.Event{
		Kind:    component.KindSubmit,
		Text:    body,
		Payload: mode,
	})
}

// View implements [component.Region].
//
// # There is no border, and that is the design
//
// An earlier version drew the composer inside a box. Three things argued against it, and
// all three are about cells in a terminal that does not have many:
//
//   - The border cost two of the three rows the composer was given. At 24 rows total
//     that is 8% of the screen spent saying "this is the input".
//   - At two rows — which minimal mode allocates — there was no room for a border and
//     content at once, so the region either overflowed its rectangle or dropped the
//     box depending on how the arithmetic came out.
//   - lipgloss's Width counts the border and its Height does not. A trap worth not
//     walking into when the frame's width has to be exact.
//
// What replaces the border is the prompt, the placeholder and the real terminal caret
// the root places. Between the three, a user can tell an active input from an inactive
// one without spending a single cell on a frame.
//
// # The prompt follows the caret
//
// Putting the marker on the caret's line rather than always on the first one is what
// makes a multi-line draft read as one input being typed into, rather than as a list of
// lines with a glyph beside the first. It is why a shell moves its continuation prompt
// down as a multi-line command grows.
func (m *Model) View() string {
	if m.rect.Empty() {
		return ""
	}

	st := m.theme.Styles
	g := m.theme.Glyphs

	// The prompt is a glyph and the space after it, reserved on the caret's line.
	const promptW = 2

	// The band of rows the text may occupy, and whether the framing rules are drawn.
	//
	// The rules are only drawn when there is a row to spare on both sides, and the hint
	// only when there is a row spare on top of that. A rule that costs a row of the
	// draft is a bad trade, and a hint drawn on top of a rule is unreadable.
	top, bottom, rules := m.band()

	body := m.innerLines(bottom-top, maxInt(m.rect.Width-promptW, 1), st, g)
	body = m.placePrompt(body, bottom-top, st, g)

	lines := make([]string, m.rect.Height)
	if rules {
		rule := st.Divider.Render(strings.Repeat(g.Divider, m.rect.Width))
		lines[0] = rule
		lines[m.rect.Height-1] = rule
	}

	for i, l := range body {
		row := top + i
		if row >= m.rect.Height {
			break
		}
		// Every row is padded to the full width: the composer's background has to reach
		// the right edge, or the frame shows a stripe of the terminal's own colour down
		// the side of the input.
		lines[row] = text.PadRight(l, m.rect.Width)
	}

	return strings.Join(lines, "\n")
}

// band returns the rows the text may occupy and whether the rules are drawn.
//
// The thresholds come from the layout: full mode gives four rows (rule, draft, hint,
// rule), minimal gives two (draft and nothing else), and anything in between degrades
// by dropping the hint first and the rules second, because a rule is what still
// separates the input from the conversation above it.
func (m *Model) band() (top, bottom int, rules bool) {
	h := m.rect.Height
	switch {
	case h >= 4:
		return 1, h - 1, true
	case h == 3:
		// A top rule and the text below it. The bottom rule is what gives way: it is
		// the edge that meets the status bar, which is visually separated anyway.
		return 1, h, true
	default:
		// Two rows is the draft and one spare. A rule here would cost the draft its own
		// row, and the draft is the whole point of the region.
		return 0, h, false
	}
}

// innerLines builds the composer's text: the mode banner, the draft, and the hint.
func (m *Model) innerLines(height, textW int, st theme.Styles, g theme.Glyphs) []string {
	lines := make([]string, 0, height)

	if banner := m.banner(st, g, textW); banner != "" {
		lines = append(lines, banner)
	}

	if m.ta.Value() == "" {
		placeholder := m.ta.Placeholder
		if !m.focus {
			// Dimmed while blurred, so an inactive composer reads as inactive rather than
			// as an editable one.
			placeholder = st.ComposerHint.Render(placeholder)
		}
		lines = append(lines, text.Truncate(placeholder, textW))
	} else {
		// Sanitised: a draft can carry text that arrived in a quoted message, and the
		// draft is drawn exactly as it will be sent.
		for _, l := range strings.Split(text.Sanitize(m.ta.Value()), "\n") {
			lines = append(lines, text.Truncate(l, textW))
		}
	}

	// The hint sits at the bottom of the band rather than the middle, so a growing
	// draft pushes it out of the way rather than being pushed by it. It needs a row of
	// its own; without one it is dropped rather than squeezed, because half a hint is
	// worse than none.
	if m.showHint && height >= 2 {
		for len(lines) < height-1 {
			lines = append(lines, "")
		}
		lines = append(lines, text.Truncate(m.hint(), textW))
	}

	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

// placePrompt puts the marker on the caret's line and shifts that line past it.
//
// The lines are already truncated to the band minus the prompt, so the shift cannot
// make any of them too wide. Doing the truncation here instead would cut the visible
// text by two characters — which is how an earlier version rendered every placeholder
// as "Escribir mensa…".
//
// The caret's row is taken relative to the banner, because the banner occupies the
// first row of the band when a reply or an edit is in progress.
func (m *Model) placePrompt(lines []string, band int, st theme.Styles, g theme.Glyphs) []string {
	row := 0
	if _, y, ok := m.Cursor(); ok {
		row = y + m.bannerRows()
	}
	// The hint sits below the draft, so it can never be the caret's line.
	if row > band-1 || row < 0 {
		row = 0
	}

	marker := st.ComposerPrompt.Render(g.Prompt + " ")
	if !m.focus {
		marker = st.Muted.Render(g.Prompt + " ")
	}

	lines[row] = marker + lines[row]
	return lines
}

// bannerRows is how many rows the mode banner occupies above the draft.
func (m *Model) bannerRows() int {
	if m.mode == ModeSend {
		return 0
	}
	return 1
}

// banner renders the reply or edit indicator.
func (m *Model) banner(st theme.Styles, g theme.Glyphs, width int) string {
	switch m.mode {
	case ModeReply:
		return st.Accent.Render(
			text.Truncate(g.Reply+" respondiendo a "+m.replyTo, maxInt(width, 8)))
	case ModeEdit:
		return st.Warning.Render(g.Pencil + " editando — enter guarda, esc cancela")
	default:
		return ""
	}
}

// hint returns the contextual key hint.
//
// It is also the answer to the question a user has when they press alt+enter for the
// first time. The alternative is a key that works and is mentioned nowhere.
func (m *Model) hint() string {
	switch m.mode {
	case ModeEdit:
		return "enter guardar · esc cancelar"
	case ModeReply:
		return "enter enviar · esc cancelar"
	default:
		return "enter enviar · alt+enter línea nueva"
	}
}

// compile-time proof that the composer satisfies the region contract.
var _ component.Region = (*Model)(nil)

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
