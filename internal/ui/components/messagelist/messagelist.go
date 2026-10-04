// Package messagelist renders a conversation transcript.
//
// # Layout model
//
// Messages have variable height, so the transcript cannot be indexed by row the
// way the chat list can. Instead [State] carries the laid-out rows: [Row] holds
// the rendered lines plus the metadata a click or a selection needs. That keeps
// wrapping, height computation and scrolling in one place, and lets scroll
// arithmetic work in terms of rows rather than pixels or lines.
//
// Rendering is a pure function of [State]. Cursor position, scroll offset and
// selection live in the caller's state, so the transcript is testable without a
// terminal.
package messagelist

import (
	"strings"
	"time"

	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/theme"
)

// Kind classifies a laid-out row, so that clicks and keys can act on the right
// thing without re-deriving it from geometry.
type Kind int

// Recognised row kinds.
const (
	KindMessage Kind = iota
	KindDaySeparator
	KindSystem
	KindUnreadMarker
	KindEmpty
	KindTyping
)

// Row is one laid-out element of the transcript.
type Row struct {
	Kind Kind

	// MessageID is set for [KindMessage] and identifies the message this row
	// belongs to. One message may span several rows when it is tall.
	MessageID models.MessageID

	// Lines are the rendered lines, without styling applied by the caller.
	Lines []string

	// Height is len(Lines).
	Height int

	// Outgoing reports bubble alignment.
	Outgoing bool

	// Selected marks a message included in a multi-select.
	Selected bool

	// Cursor marks the message with the keyboard cursor. At most one row has
	// it.
	Cursor bool
}

// State is everything the transcript needs in order to render.
type State struct {
	// Rows are the laid-out content, in chronological order.
	Rows []Row

	// Offset is the index of the first visible row.
	Offset int

	// Width and Height are the pane's content area in cells and lines. These
	// exclude the header, composer and status bar.
	Width, Height int

	// AtBottom reports whether the view is pinned to the newest message.
	//
	// It is tracked rather than inferred, because a short conversation can be
	// simultaneously scrolled to the top and to the bottom, and the two cases
	// want different behaviour on a new message.
	AtBottom bool

	// Theme supplies colours, metrics and glyphs.
	Theme theme.Theme
}

// Layout converts messages into rows at the given width.
//
// It inserts day separators and an unread marker, which is what makes a
// scrolled transcript navigable rather than an undifferentiated wall of text —
// the same device WhatsApp Web uses.
//
// cursorID, when non-empty, marks that message with the keyboard cursor.
// selectedIDs marks messages included in a multi-selection.
//
// The returned rows are the transcript's line index: scrolling is then a matter
// of slicing this slice, with no re-wrapping per frame.
func Layout(
	msgs []models.Message,
	width int,
	loc Loc,
	cursorID models.MessageID,
	selectedIDs map[models.MessageID]bool,
	t theme.Theme,
) []Row {
	if width <= 0 {
		return nil
	}

	st := t.Styles
	g := t.Glyphs
	rows := make([]Row, 0, len(msgs)*2)

	// Bubble geometry, mirroring WhatsApp Web: messages are inset from the
	// edges, and bubbles never exceed four fifths of the pane.
	const (
		edgePad    = 2
		minBubbles = 20
		maxBubbles = 4 // fraction of the width
	)
	avail := width - 2*edgePad
	if avail < minBubbles {
		avail = max(width, 1)
	}
	bubbleMax := avail * maxBubbles / 5
	if bubbleMax < minBubbles {
		bubbleMax = minBubbles
	}

	var lastDay string

	for _, m := range msgs {
		// Sanitize before rendering: message bodies come from the network.
		body := text.Sanitize(m.Text())

		// Date separator, whenever the calendar day changes.
		day := models.DayKey(m.Timestamp, loc.tz())
		if day != lastDay {
			rows = append(rows, Row{
				Kind:   KindDaySeparator,
				Height: 1,
				Lines:  []string{st.Muted.Render(dayLabel(m.Timestamp, loc.tz()))},
			})
			lastDay = day
		}

		// System notices are their own row type rather than bubbles: they are
		// centred and have no sender.
		if m.Kind == models.KindSystem {
			rows = append(rows, systemRow(m, width, st))
			continue
		}

		// A revoked message keeps a tombstone in place so the transcript does
		// not reflow, but shows no trace of the original content.
		if m.Revoked {
			label := "This message was deleted"
			rows = append(rows, Row{
				Kind:      KindMessage,
				MessageID: m.ID,
				Height:    1,
				Lines:     []string{st.Muted.Render(g.Close + " " + label)},
			})
			continue
		}

		lines := bubbleLines(m, body, bubbleMax, width, st, g)

		rows = append(rows, Row{
			Kind:      KindMessage,
			MessageID: m.ID,
			Height:    len(lines),
			Lines:     lines,
			Outgoing:  m.IsOutgoing(),
			Selected:  selectedIDs[m.ID],
			Cursor:    cursorID != "" && cursorID == m.ID,
		})
	}

	return rows
}

// loc resolves the viewer's time zone.
//
// The day separator must use the viewer's calendar, not UTC's, or messages sent
// near midnight appear under the wrong date.
// Loc resolves the viewer's time zone for day separators.
type Loc struct{ l *time.Location }

// NewLoc builds a location resolver for [Layout].
//
// A nil location is a programming error rather than a user-facing condition, so
// it falls back to the system zone rather than panicking.
func NewLoc(l *time.Location) Loc {
	if l == nil {
		l = time.Local
	}
	return Loc{l: l}
}

func (l Loc) tz() *time.Location { return l.l }

// bubbleLines renders one message as its lines of styled text.
func bubbleLines(
	m models.Message,
	body string,
	bubbleMax, paneWidth int,
	st theme.Styles,
	g theme.Glyphs,
) []string {
	var lines []string

	// Reply quote, indented above the body.
	if m.ReplyTo != nil {
		quote := text.Truncate(quoteText(m.ReplyTo), bubbleMax-2)
		lines = append(lines, st.ReplyQuote.Render(g.Reply+" "+quote))
	}

	// Media description for attachments, so a bubble is never empty.
	if m.Media != nil {
		desc := text.Truncate(m.Media.Description(), bubbleMax-2)
		lines = append(lines, st.Muted.Render(g.Attachment+" "+desc))
	}

	if body != "" {
		lines = append(lines, text.WrapStyled(body, bubbleMax)...)
	}

	// Reactions sit below the body, aggregated by glyph.
	for _, r := range m.ReactionSummary() {
		label := r.Glyph
		if r.Count > 1 {
			label += " " + text.Itoa(r.Count)
		}
		lines = append(lines, st.Reaction.Render(label))
	}

	if m.Forwarded {
		lines = append(lines, st.Muted.Render(g.Chevron+" forwarded"))
	}

	// The footer carries status for outgoing messages and the timestamp for
	// both, on the same line to save vertical space.
	footer := models.FormatMessageTimestamp(m.Timestamp)
	if m.IsOutgoing() {
		footer += " " + m.Status.Glyph()
	}
	if m.EditCount > 0 {
		footer += " " + g.Bullet + "edited"
	}

	style := st.BubbleTime
	switch {
	case m.SendError != "":
		footer = g.Error + " failed to send"
		style = st.BubbleFailed
	case m.Status == models.DeliveryRead:
		style = st.Reaction // read marks use the accent colour
	}
	lines = append(lines, style.Render(footer))

	// Bubble styling is applied per line and right-aligned for outgoing
	// messages, which is what produces the familiar left/right split.
	out := make([]string, 0, len(lines))
	bubble := st.Incoming
	if m.IsOutgoing() {
		bubble = st.Outgoing
	}
	for _, l := range lines {
		rendered := bubble.Render(l)
		if !m.IsOutgoing() {
			out = append(out, rendered)
			continue
		}
		pad := paneWidth - text.VisibleWidth(rendered)
		if pad < 0 {
			pad = 0
		}
		out = append(out, strings.Repeat(" ", pad)+rendered)
	}

	return out
}

// systemRow renders a server-generated notice.
func systemRow(m models.Message, width int, st theme.Styles) Row {
	label := m.SystemText
	if label == "" {
		label = m.Body
	}
	label = text.Truncate(text.Sanitize(label), width-8)
	return Row{
		Kind:      KindSystem,
		MessageID: m.ID,
		Height:    1,
		Lines:     []string{centre(st.System.Render(label), width)},
	}
}

// centre puts s on a line of exactly width cells, padded symmetrically.
//
// Centring is approximated by dividing the difference, which is what every
// terminal UI does; exact centring is not worth the complexity for one-cell
// precision.
func centre(s string, width int) string {
	pad := width - text.VisibleWidth(s)
	if pad <= 0 {
		return s
	}
	left := pad / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", pad-left)
}

// quoteText renders the quoted message of a reply.
func quoteText(r *models.MessageRef) string {
	who := r.SenderName
	if who == "" {
		who = "message"
	}
	if r.Revoked {
		return who + ": (deleted)"
	}
	return who + ": " + models.FirstLine(r.Body)
}

// dayLabel formats a date separator heading.
//
// "TODAY" is used rather than the date, matching the convention of every chat
// client: the date is redundant for the day the user is looking at.
func dayLabel(ts time.Time, loc *time.Location) string {
	now := time.Now()
	t := ts.In(loc)
	switch {
	case t.Year() == now.Year() && t.YearDay() == now.YearDay():
		return "TODAY"
	case t.Year() == now.Year():
		return strings.ToUpper(t.Format("Jan 2"))
	default:
		return strings.ToUpper(t.Format("Jan 2, 2006"))
	}
}

// View renders the transcript.
func View(s State) string {
	if s.Height <= 0 || s.Width <= 0 {
		return ""
	}
	if len(s.Rows) == 0 {
		return s.Theme.Styles.EmptyState.Render(centre("No messages yet", s.Width))
	}

	offset := s.Offset
	if offset < 0 {
		offset = 0
	}

	var (
		lines []string
		used  int
		i     = offset
	)
	for ; i < len(s.Rows) && used < s.Height; i++ {
		row := s.Rows[i]
		for _, l := range row.Lines {
			if used >= s.Height {
				break
			}
			lines = append(lines, s.decorate(l, row))
			used++
		}
	}

	// Pad so the composer's background reaches the bottom edge.
	for ; used < s.Height; used++ {
		lines = append(lines, strings.Repeat(" ", s.Width))
	}

	return strings.Join(lines, "\n")
}

// decorate applies selection and cursor treatment to a line.
func (s State) decorate(line string, row Row) string {
	st := s.Theme.Styles
	switch {
	case row.Selected:
		return st.Selection.Render(line)
	case row.Cursor:
		// The cursor line is marked rather than recoloured, so the bubble's own
		// direction stays legible.
		return st.Accent.Render(s.Theme.Glyphs.Chevron+" ") + line
	default:
		return line
	}
}

// VisibleRows returns the indices of rows intersecting the viewport, for
// mapping a mouse click to a message.
func (s State) VisibleRows() (first, last int) {
	offset := s.Offset
	if offset < 0 {
		offset = 0
	}
	first = offset
	last = offset
	used := 0
	for i := offset; i < len(s.Rows) && used < s.Height; i++ {
		used += s.Rows[i].Height
		last = i + 1
	}
	return first, last
}

// IndexAtY returns the index of the row occupying the given terminal line, or
// -1 when the line is past the end of the transcript.
//
// Mouse clicks are reported as absolute terminal coordinates, so this walks the
// row heights to find which message was hit.
func (s State) IndexAtY(y int) int {
	if y < 0 {
		return -1
	}
	offset := s.Offset
	if offset < 0 {
		offset = 0
	}

	rel := y
	for i := offset; i < len(s.Rows); i++ {
		if rel < s.Rows[i].Height {
			return i
		}
		rel -= s.Rows[i].Height
	}
	return -1
}

// TotalHeight returns the transcript's height in terminal lines.
func (s State) TotalHeight() int {
	total := 0
	for _, r := range s.Rows {
		total += r.Height
	}
	return total
}

// ScrollTarget returns the row index to scroll to, given a scroll position
// expressed in lines from the top.
//
// Scrolling by lines rather than by rows keeps wheel and page scrolling smooth,
// since a tall message should move by a fraction of itself rather than jumping
// its whole height.
func ScrollTarget(rows []Row, lineFromTop int) int {
	if lineFromTop <= 0 {
		return 0
	}
	acc := 0
	for i, r := range rows {
		acc += r.Height
		if acc > lineFromTop {
			return i
		}
	}
	return len(rows)
}

// MaxOffset returns the largest valid scroll offset for the given height.
func MaxOffset(rows []Row, height int) int {
	total := 0
	for _, r := range rows {
		total += r.Height
	}
	over := total - height
	if over < 0 {
		return 0
	}
	return ScrollTarget(rows, over)
}
