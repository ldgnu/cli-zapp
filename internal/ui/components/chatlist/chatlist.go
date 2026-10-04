// Package chatlist renders the sidebar of conversations.
//
// # It is a pure function of state
//
// [View] takes a [State] and returns a string. It holds no cursor, no scroll
// offset and no selection of its own — all of that lives in the caller's state
// struct. That is what makes it renderable in a test without a terminal, and it
// is why the component has no Update method: key presses are resolved to
// actions by internal/keybindings and applied to the state before rendering.
package chatlist

import (
	"fmt"
	"strings"

	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/theme"
)

// RowHeight is the number of terminal lines one chat row occupies.
//
// Two lines: the name with its timestamp, then the preview with its badge.
// Constant height keeps scrolling arithmetic simple and predictable, which
// matters more than variable height here — WhatsApp Web's rows are fixed too.
const RowHeight = 2

// State is everything the sidebar needs in order to render.
type State struct {
	// Chats are the conversations to display, already sorted.
	Chats []models.Chat

	// Selected is the index into Chats of the highlighted row, or -1 when
	// nothing is selected.
	Selected int

	// Focused reports whether the sidebar has keyboard focus, which changes
	// the selected row's appearance.
	Focused bool

	// Width is the sidebar width in cells.
	Width int

	// Height is the number of rows that fit in the visible area, excluding the
	// header and the search box.
	Height int

	// Offset is the index of the first visible row. It is maintained by the
	// caller so that scroll position survives re-renders.
	Offset int

	// SearchActive and SearchQuery drive the filter field.
	SearchActive bool
	SearchQuery  string

	// Theme supplies colours, metrics and glyphs.
	Theme theme.Theme
}

// visibleRange returns the half-open range of rows to draw.
//
// The selected row is scrolled into view here rather than in the update path,
// which keeps scrolling correct even when the row count changes underneath an
// external update such as a sync bringing in new messages.
func (s State) visibleRange() (start, end int) {
	if s.Height <= 0 || len(s.Chats) == 0 {
		return 0, 0
	}

	rows := s.Height / RowHeight
	if rows < 1 {
		rows = 1
	}

	start = s.Offset
	if start < 0 {
		start = 0
	}
	if start > len(s.Chats)-1 {
		start = len(s.Chats) - 1
	}

	// Follow the selection when it has scrolled out of view.
	if s.Selected >= 0 && s.Selected < len(s.Chats) {
		if s.Selected < start {
			start = s.Selected
		}
		if s.Selected >= start+rows {
			start = s.Selected - rows + 1
		}
	}

	end = start + rows
	if end > len(s.Chats) {
		end = len(s.Chats)
	}
	return start, end
}

// OffsetForSelection returns the scroll offset that shows the selected row,
// for callers that maintain the offset themselves.
func (s State) OffsetForSelection() int {
	return OffsetForSelection(s.Selected, len(s.Chats), s.Height)
}

// OffsetForSelection computes the first visible row index for a selection.
//
// Exported because both the sidebar and the root model need to agree on the
// scroll position, and duplicating that arithmetic is how sidebars and message
// lists drift apart.
func OffsetForSelection(selected, total, height int) int {
	if selected < 0 || total == 0 || height <= 0 {
		return 0
	}
	rows := height / RowHeight
	if rows < 1 {
		rows = 1
	}
	offset := selected - rows + 1
	if offset < 0 {
		return 0
	}
	if offset > total-1 {
		return total - 1
	}
	return offset
}

// View renders the sidebar.
//
// A pane with no room returns nothing rather than a fragment. Emitting a header
// and a divider into a zero-height area would push the rest of the layout down,
// and the caller joins these blocks side by side.
func View(s State) string {
	if s.Width <= 0 || s.Height <= 0 {
		return ""
	}

	st := s.Theme.Styles
	g := s.Theme.Glyphs

	var b strings.Builder

	// Header.
	b.WriteString(st.SidebarHead.Width(s.Width).Render(" " + "Chats"))
	b.WriteByte('\n')

	// Search box. It is always shown so that the layout does not jump when the
	// user starts typing, which is the behaviour every messaging client has.
	b.WriteString(renderSearch(s, st, g))
	b.WriteByte('\n')

	// Divider under the search box.
	b.WriteString(st.Divider.Render(strings.Repeat(g.Divider, s.Width)))
	b.WriteByte('\n')

	start, end := s.visibleRange()
	visible := s.Chats[start:end]

	if len(s.Chats) == 0 {
		b.WriteString(renderEmpty(s, st))
	} else if len(visible) == 0 {
		b.WriteString(st.EmptyState.Render(truncate("no chats in view", s.Width-2)))
	} else {
		for i, chat := range visible {
			row := renderRow(s, chat, start+i, st, g)
			b.WriteString(row)
			b.WriteByte('\n')
		}
	}

	// Pad to the full height so the sidebar's background extends to the
	// bottom of the screen; without this the conversation pane shows through.
	pad := s.Height - len(visible)*RowHeight
	for range max(pad, 0) {
		b.WriteString(strings.Repeat(" ", s.Width))
		b.WriteByte('\n')
	}

	// Drop the trailing newline: the caller composes rows side by side.
	return strings.TrimRight(b.String(), "\n")
}

func renderSearch(s State, st theme.Styles, g theme.Glyphs) string {
	query := s.SearchQuery
	if query == "" && !s.SearchActive {
		// Placeholder text, dimmed so it is not mistaken for a real query.
		ph := st.Muted.Render("Search chats")
		prompt := st.StatusLabel.Render(g.Search + " ")
		return padRight(prompt+ph, s.Width)
	}

	prompt := st.SearchPrompt.Render(g.Search + " ")
	text := query
	if s.SearchActive && query == "" {
		text = st.Muted.Render("type to search")
	}
	return padRight(prompt+text, s.Width)
}

// renderEmpty distinguishes "no chats at all" from "nothing matched the
// search", because the two call for different user actions.
func renderEmpty(s State, st theme.Styles) string {
	if s.SearchQuery != "" {
		return st.EmptyState.Render(truncate("no matches", s.Width-2))
	}
	return st.EmptyState.Render(truncate("no conversations yet", s.Width-2))
}

// renderRow draws one chat entry.
func renderRow(s State, chat models.Chat, idx int, st theme.Styles, g theme.Glyphs) string {
	selected := idx == s.Selected

	// Line 1: name, unread badge, timestamp.
	var title string
	switch {
	case chat.HasUnread && chat.UnreadCount > 0:
		title = st.ChatTitleUnread.Render(chat.FallbackName())
	default:
		title = st.ChatTitle.Render(chat.FallbackName())
	}

	// Decorate the title with the state flags a user scans for: pin, mute.
	var decor strings.Builder
	if chat.Pinned {
		decor.WriteString(g.Pinned)
	}
	if chat.Muted {
		decor.WriteString(g.Muted)
	}
	decorText := decor.String()
	if decorText != "" {
		title += " " + st.ChatTime.Render(decorText)
	}

	timeText := models.FormatChatTimestamp(chat.Timestamp)
	unreadText := ""
	if chat.HasUnread && chat.UnreadCount > 0 {
		unreadText = st.ChatBadge.Render(fmt.Sprintf("%d", chat.UnreadCount))
	}

	line1 := composeLine(title, unreadText, timeText, s.Width)

	// Line 2: preview, or the typing indicator.
	preview := chat.PreviewString()
	var line2 string
	switch {
	case chat.Typing:
		line2 = st.StatusKey.Render(g.Typing + " typing…")
	case preview == "":
		line2 = st.ChatPreview.Render(truncate(g.Placeholder, s.Width-2))
	default:
		line2 = st.ChatPreview.Render(truncate(preview, s.Width-2))
	}

	// The selected row's highlight must cover both lines and the full width,
	// otherwise the sidebar looks ragged.
	//
	// The width is taken from the state rather than from the style. A style built
	// with a fixed width silently overflows whenever the pane is rendered at a
	// different size, which is exactly what happens on a terminal resize.
	selectedStyle := st.ChatRowSelected.Width(s.Width)

	if selected && s.Focused {
		line1 = selectedStyle.Render(padRight(line1, s.Width))
		line2 = selectedStyle.Render(padRight(line2, s.Width))
	} else if selected {
		// Focused elsewhere: mark the row without the heavy highlight so the
		// user can still tell which chat is open.
		mark := st.StatusKey.Render("›") + " "
		line1 = st.ChatRow.Render(padRight(mark+line1, s.Width))
		line2 = st.ChatRow.Render(padRight("  "+line2, s.Width))
	} else {
		line1 = st.ChatRow.Render(padRight(line1, s.Width))
		line2 = st.ChatRow.Render(padRight(line2, s.Width))
	}

	return line1 + "\n" + line2
}

// composeLine arranges left, middle and right segments on one line.
//
// The timestamp is right-aligned and the unread badge placed before it, so the
// two never collide the way they would if both were simply appended.
func composeLine(left, middle, right string, width int) string {
	rightW := lineWidth(right)
	middleW := lineWidth(middle)

	available := width - rightW - middleW
	if available < lineWidth(left) {
		left = truncate(left, available)
	}

	gap := available - lineWidth(left)
	if gap < 0 {
		gap = 0
	}
	return left + strings.Repeat(" ", gap) + middle + right
}

// Width, PadRight and Truncate are thin aliases over the text package.
//
// They keep the call sites below readable; measurement and truncation have one
// implementation, in internal/text, and are tested there.
func lineWidth(s string) int          { return text.VisibleWidth(s) }
func padRight(s string, w int) string { return text.PadRight(s, w) }
func truncate(s string, w int) string { return text.TruncateStyled(s, w) }
