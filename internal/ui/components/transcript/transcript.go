// Package transcript renders a conversation's messages.
//
// # Scroll is the region's own
//
// The offset lives here, not in the application. That is what makes scrolling
// independent: a user reading back through a long conversation keeps their place
// when the chat list scrolls, and vice versa, because neither knows about the
// other.
//
// # A transcript is laid out once and drawn many times
//
// Wrapping depends on the width, not on scroll position, so [Layout] runs when the
// width changes or the transcript changes — not every frame. A conversation of a
// thousand messages would otherwise re-wrap a thousand messages sixty times a
// second.
package transcript

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/models"
	"github.com/cli-zapp/cli-zapp/internal/text"
	"github.com/cli-zapp/cli-zapp/internal/ui/component"
	"github.com/cli-zapp/cli-zapp/internal/ui/layout"
	"github.com/cli-zapp/cli-zapp/internal/ui/theme"
)

// Kind classifies a laid-out row, so that a click or a key can act on the right
// thing without re-deriving it from geometry.
type Kind int

// Recognised row kinds.
const (
	KindMessage Kind = iota
	KindDaySeparator
	KindSystem
	KindEmpty
)

// Row is one laid-out element of the transcript.
type Row struct {
	Kind Kind
	// MessageID identifies the message; empty for separators.
	MessageID models.MessageID
	// Index is the message's position in the transcript, or -1.
	Index int
	// Lines are the rendered lines.
	Lines []string
	// Height is len(Lines).
	Height int
	// Outgoing reports bubble alignment.
	Outgoing bool
	// Cursor marks the message under the keyboard cursor.
	Cursor bool
	// Selected marks a multi-selection.
	Selected bool
}

// Model is the transcript's state.
type Model struct {
	rect   layout.Rect
	focus  bool
	keys   keybindings.Map
	theme  theme.Theme
	rows   []Row
	cursor int
	offset int
	// atBottom reports whether the view is pinned to the newest message.
	//
	// Tracked rather than inferred, because a short conversation is simultaneously
	// scrolled to the top and to the bottom, and the two want different behaviour
	// when a message arrives.
	atBottom bool
	// mode gates the day separators and the system rows, which minimal mode drops
	// to reclaim rows for messages.
	compact bool
	// showMeta adds a gutter carrying the sender's initial for group chats.
	showMeta bool
	// loc is the time zone used for day separators.
	loc *time.Location
	// msgs is the transcript the layout was built from.
	msgs []models.Message
	// selected is the multi-selection, read at render time rather than baked into
	// the rows so that selecting does not re-wrap anything.
	selected map[models.MessageID]bool
}

// New creates a transcript.
func New(keys keybindings.Map, t theme.Theme) *Model {
	return &Model{
		keys:     keys,
		theme:    t,
		cursor:   -1,
		atBottom: true,
		loc:      time.Local,
	}
}

// Name implements [component.Region].
func (m *Model) Name() string { return component.RegionTranscript }

// Resize implements [component.Region].
func (m *Model) Resize(r layout.Rect) {
	if r.Width != m.rect.Width {
		// Width changed, so every wrap point moved: the layout must be rebuilt.
		m.rect = r
		m.relayout()
		return
	}
	m.rect = r
	m.clampOffset()
}

// Focus implements [component.Region].
func (m *Model) Focus() tea.Cmd { m.focus = true; return nil }

// Blur implements [component.Region].
func (m *Model) Blur() { m.focus = false }

// Focused implements [component.Region].
func (m *Model) Focused() bool { return m.focus }

// SetMode applies the layout mode.
func (m *Model) SetMode(mode layout.Mode) {
	m.compact = mode == layout.ModeMinimal
}

// SetGroupChat enables the sender gutter.
func (m *Model) SetGroupChat(group bool) {
	if m.showMeta == group {
		return
	}
	m.showMeta = group
	m.relayout()
}

// Cursor returns the index of the selected message.
func (m *Model) Cursor() int { return m.cursor }

// AtBottom reports whether the view is pinned to the newest message.
func (m *Model) AtBottom() bool { return m.atBottom }

// SetModel supplies the application's shared state.
//
// The layout is rebuilt only when the messages themselves changed. Wrapping is the
// expensive part of a frame, and a thousand-message conversation must not re-wrap
// because the unread count changed.
func (m *Model) SetModel(s component.Model) {
	// The selection is held here rather than baked into the rows, because selecting
	// a message must not trigger a relayout: selection changes on every keystroke and
	// relayout re-wraps the whole transcript.
	m.selected = s.Selected

	if sameMessages(m.msgs, s.Messages) && m.rect.Width > 0 {
		return
	}
	m.msgs = s.Messages
	m.relayout()
}

// CursorMessageID returns the identifier of the message under the cursor.
//
// It is exported because the application acts on that message — copying it, replying
// to it — and the application must not have to reach into the region's internals to
// find out which one it means.
func (m *Model) CursorMessageID() models.MessageID { return m.cursorMessageID() }

// ScrollToNewest pins the view to the newest message.
//
// Opening a conversation always jumps to the bottom, because the message the user came
// to read is the most recent one; arriving in the middle of a history would be a
// strange default.
func (m *Model) ScrollToNewest() {
	m.atBottom = true
	m.offset = m.maxOffset()
}

// SelectedCount returns how many messages are selected.
func (m *Model) SelectedCount() int { return len(m.selected) }

// sameMessages reports whether two transcripts are identical, so the caller can
// skip a relayout.
// sameMessages reports whether two transcripts would lay out identically.
//
// Comparing only the fields a row actually draws is the point: a message's reaction
// set changing does alter the rendered output, so it is included; a message's
// bookkeeping field that is never drawn is not.
func sameMessages(a, b []models.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Status != b[i].Status ||
			a[i].Text() != b[i].Text() || a[i].Revoked != b[i].Revoked ||
			a[i].Kind != b[i].Kind || a[i].SenderID != b[i].SenderID ||
			a[i].SendError != b[i].SendError || a[i].EditCount != b[i].EditCount ||
			!a[i].Timestamp.Equal(b[i].Timestamp) || a[i].Forwarded != b[i].Forwarded ||
			a[i].ReplyTo != b[i].ReplyTo || a[i].Media != b[i].Media ||
			reactionKey(a[i]) != reactionKey(b[i]) {
			return false
		}
	}
	return true
}

// reactionKey fingerprints a message's reactions so two transcripts can be compared
// without walking the map.
//
// The reactions have to be in the comparison: they are drawn under the message, so
// leaving them out means adding a reaction changes nothing on screen until some other
// field happens to change — which reads as the feature doing nothing at all.
func reactionKey(m models.Message) string {
	if len(m.Reactions) == 0 {
		return ""
	}
	glyphs := make([]string, 0, len(m.Reactions))
	for g, who := range m.Reactions {
		glyphs = append(glyphs, g+":"+text.Itoa(len(who)))
	}
	// Map iteration order is random, so the fingerprint has to impose one.
	sort.Strings(glyphs)
	return strings.Join(glyphs, ",")
}

// Update implements [component.Region].
func (m *Model) Update(msg tea.Msg) (component.Region, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) (component.Region, tea.Cmd) {
	// The action is resolved once and named, rather than the key being compared again
	// inside each branch: the transcript's behaviour is a function of what the key
	// means, so a rebinding changes it with no code change.
	a := m.keys.Resolve(component.ConvertKey(msg), keybindings.PanelMessages)

	switch a {
	case keybindings.NavUp:
		return m.move(-1)
	case keybindings.NavDown:
		return m.move(1)

	case keybindings.ScrollUp:
		m.Scroll(-1)
		return m, m.reportCursor()
	case keybindings.ScrollDown:
		m.Scroll(1)
		return m, m.reportCursor()
	case keybindings.ScrollTop:
		m.scrollTo(0)
		return m, m.reportCursor()
	case keybindings.ScrollBottom:
		m.scrollTo(m.totalHeight())
		return m, m.reportCursor()
	case keybindings.PageUp:
		m.Scroll(-maxInt(m.visibleHeight(), 1))
		return m, m.reportCursor()
	case keybindings.PageDown:
		m.Scroll(maxInt(m.visibleHeight(), 1))
		return m, m.reportCursor()

	case keybindings.ActionSelect:
		return m.toggleSelect()
	case keybindings.ActionSelectAll:
		return m.selectAll()
	case keybindings.ActionReply, keybindings.ActionEdit, keybindings.ActionDelete,
		keybindings.ActionReact, keybindings.ActionCopy, keybindings.ActionDownload,
		keybindings.ActionOpen, keybindings.ActionForward:
		return m, m.reportCursorAnd(component.KindMessageActivated, string(a))

	default:
		// Not the transcript's bindings: another panel's, or the application's, which
		// were resolved before this region saw the key.
		return m, nil
	}
}

// move changes the cursor, which is the selection the user acts on.
func (m *Model) move(delta int) (component.Region, tea.Cmd) {
	n := len(m.msgs)
	if n == 0 {
		m.cursor = -1
		return m, nil
	}
	m.cursor = clamp(m.cursor+delta, 0, n-1)
	m.scrollCursorIntoView()
	return m, m.reportCursor()
}

// reportCursor tells the application which message the cursor is on.
func (m *Model) reportCursor() tea.Cmd {
	id := m.cursorMessageID()
	if id == "" {
		return nil
	}
	return component.EventCmd(component.Event{
		Kind: component.KindMessageSelected, MessageID: id,
	})
}

// reportCursorAnd tells the application the cursor moved and that an action was
// requested on it.
func (m *Model) reportCursorAnd(kind component.Kind, action string) tea.Cmd {
	id := m.cursorMessageID()
	if id == "" {
		return nil
	}
	return component.EventCmd(component.Event{
		Kind: kind, MessageID: id, Text: action,
	})
}

// toggleSelect adds or removes the cursor message from the selection.
func (m *Model) toggleSelect() (component.Region, tea.Cmd) {
	id := m.cursorMessageID()
	if id == "" {
		return m, nil
	}
	return m, component.EventCmd(component.Event{
		Kind: component.KindSelectionToggled, MessageID: id,
	})
}

// selectAll selects every message, or clears the selection.
func (m *Model) selectAll() (component.Region, tea.Cmd) {
	if len(m.msgs) == 0 {
		return m, nil
	}
	cmds := make([]tea.Cmd, 0, len(m.msgs))
	for _, msg := range m.msgs {
		cmds = append(cmds, component.EventCmd(component.Event{
			Kind: component.KindSelectionToggled, MessageID: msg.ID,
		}))
	}
	return m, tea.Batch(cmds...)
}

// Scroll moves the view without changing the cursor, which is what the wheel and
// the page keys do.
func (m *Model) Scroll(delta int) {
	m.scrollTo(m.offset + delta)
}

// scrollTo moves the view to a line offset, clamped to the content.
func (m *Model) scrollTo(line int) {
	maxOffset := m.maxOffset()
	m.offset = clamp(line, 0, maxOffset)
	m.atBottom = m.offset >= maxOffset
}

// maxOffset is the largest valid scroll offset.
func (m *Model) maxOffset() int {
	over := m.totalHeight() - m.visibleHeight()
	if over < 0 {
		return 0
	}
	return over
}

// totalHeight is the transcript's height in lines.
func (m *Model) totalHeight() int {
	total := 0
	for _, r := range m.rows {
		total += r.Height
	}
	return total
}

// visibleHeight is how many lines fit.
func (m *Model) visibleHeight() int {
	if m.rect.Height <= 0 {
		return 1
	}
	return m.rect.Height
}

// clampOffset keeps the scroll position inside the content after a resize.
func (m *Model) clampOffset() {
	if m.atBottom {
		m.offset = m.maxOffset()
		return
	}
	m.offset = clamp(m.offset, 0, m.maxOffset())
}

// scrollCursorIntoView adjusts the offset so the cursor's row is visible.
func (m *Model) scrollCursorIntoView() {
	if m.cursor < 0 {
		return
	}
	start, end := m.rowRangeFor(m.cursor)

	if start < m.offset {
		m.offset = start
	}
	if end > m.offset+m.visibleHeight() {
		m.offset = end - m.visibleHeight()
	}
	m.clampOffset()
}

// rowRangeFor returns the line range a message occupies.
func (m *Model) rowRangeFor(index int) (start, end int) {
	acc := 0
	for _, r := range m.rows {
		if r.Index == index {
			return acc, acc + r.Height
		}
		acc += r.Height
	}
	return acc, acc
}

// cursorMessageID returns the message under the cursor.
func (m *Model) cursorMessageID() models.MessageID {
	if m.cursor < 0 || m.cursor >= len(m.msgs) {
		return ""
	}
	return m.msgs[m.cursor].ID
}

// MessageAt returns a message by transcript index.
func (m *Model) MessageAt(i int) (models.Message, bool) {
	if i < 0 || i >= len(m.msgs) {
		return models.Message{}, false
	}
	return m.msgs[i], true
}

// ClickAt places the cursor on the message under a terminal cell.
func (m *Model) ClickAt(x, y int) tea.Cmd {
	if !m.rect.Contains(x, y) {
		return nil
	}
	line := y - m.rect.Y + m.offset
	acc := 0
	for _, r := range m.rows {
		if line < acc+r.Height {
			if r.Index < 0 {
				return nil
			}
			m.cursor = r.Index
			return m.reportCursor()
		}
		acc += r.Height
	}
	return nil
}

// relayout rebuilds the rows at the current width.
func (m *Model) relayout() {
	if m.rect.Width <= 0 {
		m.rows = nil
		return
	}
	m.rows = m.buildRows()
	m.clampCursor()
	m.clampOffset()
}

// clampCursor keeps the cursor inside the transcript.
func (m *Model) clampCursor() {
	switch {
	case len(m.msgs) == 0:
		m.cursor = -1
	case m.cursor < 0:
		m.cursor = len(m.msgs) - 1
	case m.cursor >= len(m.msgs):
		m.cursor = len(m.msgs) - 1
	}
}

// buildRows lays the transcript out at the current width.
func (m *Model) buildRows() []Row {
	g := m.theme.Glyphs
	st := m.theme.Styles

	width := m.rect.Width
	rows := make([]Row, 0, len(m.msgs)*2)

	// Geometry, following WhatsApp Web: messages are inset from the edges and
	// bubbles never exceed four fifths of the pane, so a long message reads as a
	// bubble rather than as a full-width block.
	const (
		gutter  = 2  // the left inset
		maxPct  = 4  // bubble width as a fraction of the pane, over 5
		minWide = 24 // below this the bubble is the full pane
	)
	avail := width - 2*gutter
	if avail < minWide {
		avail = maxInt(width, 1)
	}
	bubbleMax := avail * maxPct / 5
	if bubbleMax < minWide {
		bubbleMax = minWide
	}
	if m.showMeta {
		// The sender gutter takes a column from the bubble's budget.
		bubbleMax = maxInt(bubbleMax-1, 1)
	}

	var lastDay string

	for i, msg := range m.msgs {
		day := models.DayKey(msg.Timestamp, m.loc)
		if day != lastDay {
			if !m.compact {
				rows = append(rows, Row{
					Kind: KindDaySeparator, Index: -1, Height: 1,
					Lines: []string{centre(st.System.Render(dayLabel(day, msg.Timestamp)), width)},
				})
			}
			lastDay = day
		}

		if msg.Kind == models.KindSystem {
			rows = append(rows, Row{
				Kind: KindSystem, Index: i, Height: 1,
				Lines: []string{centre(
					st.System.Render(text.Truncate(text.Sanitize(msg.SystemText), width-4)),
					width,
				)},
			})
			continue
		}

		// A revoked message leaves a tombstone: the transcript must not reflow,
		// and the sender must not be able to erase the fact of having sent it.
		if msg.Revoked {
			rows = append(rows, Row{
				Kind: KindMessage, MessageID: msg.ID, Index: i, Height: 1,
				Lines:  []string{st.Muted.Render(g.Close + " mensaje eliminado")},
				Cursor: i == m.cursor,
			})
			continue
		}

		// Consecutive messages from one sender read as one utterance, so only the
		// last of a run carries a timestamp. Without this a three-line exchange
		// occupies nine rows at a font size of one row, which is how a chat becomes
		// unreadable on a small terminal.
		lastInRun := i+1 >= len(m.msgs) ||
			m.msgs[i+1].SenderID != msg.SenderID ||
			m.msgs[i+1].IsOutgoing() != msg.IsOutgoing() ||
			m.msgs[i+1].Kind == models.KindSystem ||
			m.msgs[i+1].Revoked ||
			models.DayKey(m.msgs[i+1].Timestamp, m.loc) != day

		lines := m.bubbleLines(msg, bubbleMax, width, lastInRun)

		rows = append(rows, Row{
			Kind:      KindMessage,
			MessageID: msg.ID,
			Index:     i,
			Lines:     lines,
			Height:    len(lines),
			Outgoing:  msg.IsOutgoing(),
			Cursor:    i == m.cursor,
		})
	}

	if m.atBottom {
		m.offset = m.maxOffset()
	}
	return rows
}

// bubbleLines renders one message's lines, styled and aligned.
func (m *Model) bubbleLines(
	msg models.Message, bubbleMax, paneWidth int, showTime bool,
) []string {
	st := m.theme.Styles
	g := m.theme.Glyphs

	var lines []string

	if msg.ReplyTo != nil {
		lines = append(lines, st.ReplyQuote.Render(
			g.Reply+" "+text.Truncate(quote(msg.ReplyTo), bubbleMax-2)))
	}
	if msg.Media != nil {
		lines = append(lines, st.Muted.Render(
			g.Attachment+" "+text.Truncate(msg.Media.Description(), bubbleMax-2)))
	}

	if body := text.Sanitize(msg.Text()); body != "" {
		lines = append(lines, text.Wrap(body, bubbleMax)...)
	}

	for _, r := range msg.ReactionSummary() {
		label := r.Glyph
		if r.Count > 1 {
			label += " " + text.Itoa(r.Count)
		}
		lines = append(lines, st.Reaction.Render(label))
	}

	if msg.Forwarded {
		lines = append(lines, st.Muted.Render(g.Chevron+" reenviado"))
	}

	if showTime {
		lines = append(lines, m.footer(msg, st, g))
	}

	// Style each line and align: incoming flush left, outgoing flush right.
	bubble := st.Incoming
	if msg.IsOutgoing() {
		bubble = st.Outgoing
	}

	out := make([]string, 0, len(lines))
	for _, l := range lines {
		rendered := bubble.Render(l)
		if msg.IsOutgoing() {
			out = append(out, alignRight(rendered, paneWidth))
			continue
		}
		out = append(out, rendered)
	}
	return out
}

// footer renders the timestamp and, for outgoing messages, the delivery mark.
func (m *Model) footer(msg models.Message, st theme.Styles, g theme.Glyphs) string {
	parts := []string{models.FormatMessageTimestamp(msg.Timestamp)}

	if msg.IsOutgoing() {
		parts = append(parts, msg.Status.Glyph())
	}
	if msg.EditCount > 0 {
		parts = append(parts, g.Bullet+"editado")
	}

	footer := strings.Join(parts, " ")

	style := st.BubbleTime
	switch {
	case msg.SendError != "":
		return st.BubbleFailed.Render(g.Error + " no se pudo enviar")
	case msg.Status == models.DeliveryRead:
		style = st.Reaction
	case msg.Status == models.DeliveryFailed:
		return st.BubbleFailed.Render(g.Error + " falló")
	}
	return style.Render(footer)
}

// quote renders a reply's quoted message.
func quote(r *models.MessageRef) string {
	who := r.SenderName
	if who == "" {
		who = "…"
	}
	if r.Revoked {
		return who + ": (eliminado)"
	}
	return who + ": " + models.FirstLine(r.Body)
}

// dayLabel formats a date separator heading.
func dayLabel(day string, ts time.Time) string {
	if day == models.DayKey(time.Now(), time.Local) {
		return "HOY"
	}
	return strings.ToUpper(ts.Format("2 Jan 2006"))
}

// alignRight pads a rendered line so its content sits at the pane's right edge.
func alignRight(s string, width int) string {
	pad := width - text.VisibleWidth(s)
	if pad <= 0 {
		return text.TruncateStyled(s, width)
	}
	return strings.Repeat(" ", pad) + s
}

// centre puts s on a line exactly width cells wide.
func centre(s string, width int) string {
	pad := width - text.VisibleWidth(s)
	if pad <= 0 {
		return s
	}
	left := pad / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", pad-left)
}

// View implements [component.Region].
func (m *Model) View() string {
	if m.rect.Empty() {
		return ""
	}
	st := m.theme.Styles

	if len(m.rows) == 0 {
		rows := make([]string, m.rect.Height)
		for i := range rows {
			rows[i] = strings.Repeat(" ", m.rect.Width)
		}
		// The notice sits on the middle row rather than the first, because the top of
		// an empty pane is where content would begin; writing there makes the pane look
		// like it has one line of content and a lot of trailing blank.
		if mid := m.rect.Height / 2; mid < len(rows) {
			rows[mid] = centre(st.EmptyState.Render("Sin mensajes todavía"), m.rect.Width)
		}
		return strings.Join(rows, "\n")
	}

	out := make([]string, 0, m.rect.Height)
	line := 0
	acc := 0

	for _, r := range m.rows {
		if line >= m.rect.Height {
			break
		}
		if acc+r.Height <= m.offset {
			acc += r.Height
			continue // above the viewport
		}
		for _, l := range r.Lines {
			if line >= m.rect.Height {
				break
			}
			if acc+line >= m.offset {
				out = append(out, m.pane(m.decorate(l, r)))
			}
			line++
		}
		acc += r.Height
	}

	for len(out) < m.rect.Height {
		out = append(out, strings.Repeat(" ", m.rect.Width))
	}
	return strings.Join(out[:m.rect.Height], "\n")
}

// pane forces a line to occupy the pane's full width.
//
// A bubble is only as wide as its longest line, and an outgoing one is pushed to the
// right edge. Neither fills the row, so without this the transcript renders a ragged
// right margin and lipgloss pads the block out to the widest line — which is the
// sidebar's width, not the pane's. The frame comes out a column short and the
// terminal wraps.
func (m *Model) pane(line string) string {
	switch w := text.VisibleWidth(line); {
	case w < m.rect.Width:
		return line + strings.Repeat(" ", m.rect.Width-w)
	case w > m.rect.Width:
		return text.TruncateStyled(line, m.rect.Width)
	default:
		return line
	}
}

// decorate applies the cursor and selection treatment to a line.
func (m *Model) decorate(line string, r Row) string {
	st := m.theme.Styles
	switch {
	case r.MessageID != "" && m.selected[r.MessageID]:
		return st.Selection.Render(line)
	case r.Cursor && m.focus:
		// The cursor is marked in the gutter rather than by recolouring the bubble, so
		// which side a message is on stays legible.
		//
		// It costs a column, so the line is clipped by one before the marker is
		// prepended. Clipped, not truncated: an ellipsis would say content was lost,
		// and what is actually being dropped is the last cell of the line's padding.
		// Every cursor row appearing to be truncated is worse than the column costs.
		inner := maxInt(m.rect.Width-1, 0)
		return st.Accent.Render(m.theme.Glyphs.Chevron) + text.PadRight(text.Clip(line, inner), inner)
	default:
		return line
	}
}

// compile-time proof that the transcript satisfies the region contract.
var _ component.Region = (*Model)(nil)

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return minInt(maxInt(v, lo), hi)
}

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
