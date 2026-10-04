// Package composer renders the message input area.
//
// # It owns the text buffer, deliberately
//
// Unlike the other components, the composer holds its own value and exposes it,
// because a text input needs a caret, a buffer and a scroll offset that must
// persist across frames and survive re-renders. Keeping that in the caller's
// state struct would mean every keystroke copies the entire buffer.
//
// # It wraps bubbles, deliberately
//
// The underlying widget is [textarea.Model] from charm.land/bubbles. Caret
// movement, word wrapping, selection and grapheme-cluster handling are all
// subtle; a hand-rolled text area would be fewer lines and far more bugs.
//
// # Bubble Tea is not imported here
//
// The package accepts key messages through a local interface, so components stay
// renderable and testable without constructing Bubble Tea messages.
package composer

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/theme"
)

// Composer is the stateful input widget.
type Composer struct {
	ta textarea.Model

	// width and height track the last applied geometry. The textarea's
	// viewport is only recomputed when the geometry actually changes, because
	// SetWidth resets its internal line index and would otherwise scroll a
	// multi-line draft back to the top on every frame.
	width, height int
}

// New creates a composer for the given geometry.
//
// The composer does not assume focus; call [Composer.Focus] when the user moves
// to it, so that a program starting with the sidebar focused does not have its
// cursor appear in the input.
func New(width, height int, t theme.Theme) *Composer {
	c := &Composer{width: width, height: height}
	c.ta = newTextarea(width, height, t)
	return c
}

// newTextarea builds the underlying textarea with wterm's palette applied.
func newTextarea(width, height int, t theme.Theme) textarea.Model {
	st := t.Styles
	isDark := t.Name != theme.NameLight

	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // the protocol caps message length, not the input
	ta.Placeholder = PlaceholderFor(false, "")

	// Use the terminal's own caret rather than a virtual one.
	//
	// The virtual cursor draws a fake block inside the widget, which cannot be
	// positioned relative to the rest of the frame and is drawn in the widget's
	// own coordinates. The root model already knows where the composer sits on
	// screen, so a real terminal cursor placed there is both simpler and correct.
	ta.SetVirtualCursor(false)

	// wterm draws its own border, so the textarea contributes no prompt. An
	// empty prompt keeps the text flush against the border.
	ta.SetPromptFunc(0, func(textarea.PromptInfo) string { return "" })

	styles := textarea.DefaultStyles(isDark)
	// The cursor line must not be highlighted: a full-width band behind the
	// editing line looks nothing like the rest of the app and fights the
	// composer border.
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Blurred.CursorLine = lipgloss.NewStyle()
	styles.Focused.Placeholder = st.ComposerHint
	styles.Blurred.Placeholder = st.ComposerHint
	styles.Focused.Text = st.Composer
	styles.Blurred.Text = st.ComposerHint
	styles.Focused.Selection = st.Selection
	styles.Blurred.Selection = st.Selection
	ta.SetStyles(styles)

	applySize(&ta, width, height)
	return ta
}

// applySize sets the textarea's viewport from the outer geometry.
//
// The outer size includes the border the composer draws around it, so two lines
// and two cells are subtracted.
func applySize(ta *textarea.Model, width, height int) {
	ta.SetWidth(max(width-2, 1))
	ta.SetHeight(max(height-2, 1))
}

// SetWidth resizes the composer.
func (c *Composer) SetWidth(width int) {
	if width == c.width {
		return
	}
	c.width = width
	applySize(&c.ta, width, c.height)
}

// SetHeight resizes the composer.
func (c *Composer) SetHeight(height int) {
	if height == c.height {
		return
	}
	c.height = height
	applySize(&c.ta, c.width, height)
}

// Size returns the current geometry.
func (c *Composer) Size() (width, height int) { return c.width, c.height }

// Value returns the current input text.
func (c *Composer) Value() string { return c.ta.Value() }

// SetValue replaces the input text, used when starting an edit.
func (c *Composer) SetValue(s string) { c.ta.SetValue(s) }

// Reset clears the input.
func (c *Composer) Reset() {
	c.ta.Reset()
	c.ta.MoveToEnd()
}

// Empty reports whether there is nothing to send.
func (c *Composer) Empty() bool { return strings.TrimSpace(c.ta.Value()) == "" }

// Insert adds text at the caret, used to pre-fill a reply.
func (c *Composer) Insert(s string) {
	c.ta.InsertString(s)
	c.ta.MoveToEnd()
}

// Focus gives the composer keyboard focus.
func (c *Composer) Focus() { c.ta.Focus() }

// Blur removes keyboard focus.
func (c *Composer) Blur() { c.ta.Blur() }

// Focused reports whether the composer has focus.
func (c *Composer) Focused() bool { return c.ta.Focused() }

// Cursor exposes the textarea's cursor so the root model can position the
// terminal cursor correctly.
func (c *Composer) Cursor() (x, y int, visible bool) {
	cur := c.ta.Cursor()
	if cur == nil {
		return 0, 0, false
	}
	return cur.X, cur.Y, true
}

// Update handles a key press and reports whether the composer consumed it.
//
// The composer receives keys directly rather than through the binding map,
// because a text field must be able to receive every printable rune and every
// editing key. The caller consults the map only for keys this returns false for,
// and that is what lets a user type "j" without scrolling the transcript.
//
// The parameter is a concrete [tea.KeyPressMsg] rather than a local interface.
// An earlier version declared an interface with a String method to avoid importing
// Bubble Tea here, and it silently accepted every key while inserting nothing:
// the textarea matches on the concrete type in a type switch, so a wrapper struct
// never reached the code that edits the buffer. The decoupling was not worth a
// silent failure to accept typing.
func (c *Composer) Update(msg tea.KeyPressMsg) bool {
	if !c.ta.Focused() {
		return false
	}
	// Enter, tab and the arrow keys are not the composer's to handle.
	//
	// A textarea would treat enter as a newline and tab as an indent. Both belong
	// to the binding map, where they mean "send the message" and "change panel" —
	// the conventions every terminal chat client follows. Letting the widget take
	// them is how a compose box ends up where enter inserts a blank line and
	// nothing is ever sent.
	if isReservedKey(msg) {
		return false
	}

	before := c.ta.Value()
	c.ta, _ = c.ta.Update(msg)
	return c.ta.Value() != before
}

// isReservedKey reports whether a key belongs to the binding map rather than to
// the text field.
func isReservedKey(msg tea.KeyPressMsg) bool {
	switch msg.Code {
	case tea.KeyEnter, tea.KeyTab, tea.KeyEscape, tea.KeyUp, tea.KeyDown, tea.KeyLeft, tea.KeyRight:
		return true
	}
	// Any modified key is a shortcut, not text: ctrl+w must delete a word by
	// binding, not be inserted.
	return msg.Mod != 0
}

// State is everything the composer needs in order to render its chrome.
//
// The text itself comes from the [Composer]; this struct covers the frame
// around it, which is a pure function of state and therefore testable on its own.
type State struct {
	// Placeholder is the prompt shown when the input is empty.
	Placeholder string

	// ReplyTo names the message being replied to, empty when not replying.
	ReplyTo string

	// EditTo names the message being edited, empty when not editing.
	EditTo string

	// Focused reports whether the composer has keyboard focus. Only then does
	// the border highlight.
	Focused bool

	// Width and Height are the composer's outer dimensions in cells and lines,
	// including the border.
	Width, Height int

	// ShowHint renders the contextual hint inside the empty composer.
	ShowHint bool

	// Editing changes the border colour to signal a destructive-adjacent mode.
	Editing bool

	// Theme supplies colours and glyphs.
	Theme theme.Theme
}

// View renders the composer's frame.
//
// Text is passed in rather than read from the model so that this function stays
// pure; [Composer.View] is the convenience wrapper that supplies it.
func View(s State, value string) string {
	if s.Width <= 2 || s.Height <= 2 {
		return ""
	}
	st := s.Theme.Styles

	// The border colour carries the mode: focused, or editing an existing
	// message, which is a state the user must be able to see.
	border := st.Composer
	switch {
	case s.Editing:
		border = st.ComposerFocused.
			BorderForeground(st.Warning.GetForeground()).
			BorderStyle(lipgloss.RoundedBorder())
	case s.Focused:
		border = st.ComposerFocused
	}

	inner := max(s.Height-2, 1)

	var body []string

	switch {
	case s.EditTo != "":
		body = append(body, st.Warning.Render("Editing — enter saves, esc cancels"))
	case s.ReplyTo != "":
		body = append(body, st.Accent.Render("Replying to "+text.Truncate(s.ReplyTo, s.Width-6)))
	}

	if text.Trim(value) != "" {
		body = append(body, strings.Split(text.Sanitize(value), "\n")...)
	} else {
		body = append(body, st.ComposerHint.Render(s.Placeholder))
	}

	if s.ShowHint && len(body) < inner {
		body = append(body, st.ComposerHint.Render(hintFor(s)))
	}

	for len(body) < inner {
		body = append(body, "")
	}
	body = body[:inner]

	return border.
		Width(s.Width - 2).
		Height(s.Height - 2).
		Render(strings.Join(body, "\n"))
}

// hintFor returns the contextual hint for the current mode.
func hintFor(s State) string {
	switch {
	case s.EditTo != "":
		return "enter save · esc cancel"
	case s.ReplyTo != "":
		return "enter send · esc cancel reply"
	default:
		return "enter send · alt+enter newline"
	}
}

// View renders the composer including its text.
//
// The offset returned is where the textarea's content starts, so that a caller
// rendering its own frame can position the terminal cursor.
func (c *Composer) View(t theme.Theme) (string, int) {
	s := State{
		Placeholder: PlaceholderFor(false, ""),
		Focused:     c.ta.Focused(),
		Width:       c.width,
		Height:      c.height,
		ShowHint:    true,
		Theme:       t,
	}
	// Delegate to the textarea for the text itself so that caret-aware
	// rendering, soft wrapping and selection highlighting all come from the
	// widget that owns them.
	inner := c.ta.View()
	return border(inner, s), 1
}

// border wraps already-rendered inner content in the composer's frame.
func border(inner string, s State) string {
	st := s.Theme.Styles
	b := st.Composer
	switch {
	case s.Editing:
		b = st.ComposerFocused
	case s.Focused:
		b = st.ComposerFocused
	}
	lines := strings.Split(inner, "\n")
	for len(lines) < max(s.Height-2, 1) {
		lines = append(lines, "")
	}
	lines = lines[:max(s.Height-2, 1)]
	return b.Width(s.Width - 2).Render(strings.Join(lines, "\n"))
}

// PlaceholderFor returns the input prompt for the current mode.
func PlaceholderFor(editing bool, replyTo string) string {
	switch {
	case editing:
		return "Edit your message"
	case replyTo != "":
		return "Reply…"
	default:
		return "Write a message"
	}
}
