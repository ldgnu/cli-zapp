// Package sidebar renders the conversation list: the brand banner, the search
// field, the chat rows and the action hints.
//
// # It emits, it does not act
//
// Selecting a row produces a [component.Event]. The application decides what that
// means — which chat is open, where focus goes, what is marked read. The sidebar
// has no reference to the transcript and cannot scroll it.
//
// # Scroll is its own
//
// The sidebar scrolls independently of the transcript. A user reading back through
// a long conversation must be able to find another chat without losing their
// place, which is the reason the two offsets are separate state.
package sidebar

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/component"
	"github.com/wterm/wterm/internal/ui/layout"
	"github.com/wterm/wterm/internal/ui/theme"
)

// MinPreviewWidth is the narrowest preview worth drawing.
//
// Below this the truncated text carries no information: eight cells is about one short
// word, which is enough for the user to recognise what a conversation is about without
// opening it. See [Model.viewRow] for why the alternative is worse.
const MinPreviewWidth = 8

// RowHeight is the number of terminal lines one chat row occupies.
//
// One line. WhatsApp Web uses two, but a terminal cannot afford the luxury: at
// 24 rows a two-line row list holds five chats, which is not a list. One line
// shows a name, a timestamp and an unread badge, which is what the sidebar is for.
const RowHeight = 1

// Model is the sidebar's state.
type Model struct {
	rect   layout.Rect
	focus  bool
	keys   keybindings.Map
	theme  theme.Theme
	offset int
	cursor int
	// hints are the action labels for the footer, supplied by the application.
	hints []keybindings.HelpEntry
	// chats is the application's list; rows is what survives the query.
	chats []models.Chat
	// rows is the filtered, visible chat list.
	rows []models.Chat
	// search is the local copy of the query, so typing does not round-trip
	// through the application on every keystroke.
	search string
	// searchFocused reports whether the search field owns the keyboard.
	searchFocused bool
	// showSearchField is false in minimal mode, where the field is replaced by
	// the command palette.
	showSearchField bool
	// showBrand is false when the conversation occupies the full width, so the
	// wordmark is not repeated in a single-column interface.
	showBrand bool
}

// New creates a sidebar.
func New(keys keybindings.Map, t theme.Theme) *Model {
	return &Model{
		keys:            keys,
		theme:           t,
		cursor:          -1,
		showSearchField: true,
		showBrand:       true,
	}
}

// Name implements [component.Region].
func (m *Model) Name() string { return component.RegionSidebar }

// Resize implements [component.Region]. It also recomputes the visible rows,
// because the filtering depends on the height and a resize can change it.
func (m *Model) Resize(r layout.Rect) {
	m.rect = r
	m.scrollCursorIntoView()
}

// Focus implements [component.Region].
func (m *Model) Focus() tea.Cmd {
	m.focus = true
	return nil
}

// Blur implements [component.Region].
func (m *Model) Blur() { m.focus = false }

// Focused implements [component.Region].
func (m *Model) Focused() bool { return m.focus }

// SetModel supplies the application's shared state.
func (m *Model) SetModel(s component.Model) {
	m.hints = s.Hints

	// The selected conversation is remembered by identifier, not by index.
	//
	// The list re-sorts whenever a conversation is pinned, so the row that was at index
	// 3 is at index 0 a moment later. Keeping the cursor as an index would silently
	// change which conversation is open — and the user would type a reply into the
	// wrong one. Restoring by identity is what keeps the highlight and the open
	// conversation the same thing.
	selected, hadSelection := m.selectedID()

	m.search = s.Search
	m.searchFocused = s.SearchActive && m.showSearchField
	m.chats = s.Chats
	m.rows = m.filtered()

	if hadSelection {
		m.SelectChat(selected)
	}
	m.clampCursor()
}

// selectedID returns the conversation under the cursor.
func (m *Model) selectedID() (models.ChatID, bool) {
	chat, ok := m.SelectedChat()
	return chat.ID, ok
}

// SetMode applies the layout mode, which decides whether the brand banner and the
// persistent search field are drawn.
func (m *Model) SetMode(mode layout.Mode) {
	m.showBrand = mode.ShowsSearchField()
	m.showSearchField = mode.ShowsSearchField()
}

// searchFieldHeight is how many rows the search field and its rule occupy.
func (m *Model) searchFieldHeight() int {
	if !m.showSearchField {
		return 0
	}
	return layout.SearchHeight + 1 // the field plus its underline
}

// listRect returns the rectangle available for chat rows.
//
// The footer is excluded because it is pinned to the bottom: the rows fill whatever is
// left between the search field and it, so the action hint does not move when the list
// grows or scrolls.
func (m *Model) listRect() layout.Rect {
	return layout.Rect{
		X:      m.rect.X,
		Y:      m.rect.Y + brandRows(m) + searchRows(m),
		Width:  m.rect.Width,
		Height: maxInt(m.rect.Height-brandRows(m)-searchRows(m)-m.footerHeight(), 0),
	}
}

// footerHeight is how many rows the action footer takes, or none when there is nothing
// to say or no room to say it.
//
// A footer that exists but is empty is worse than no footer: the action it appears to
// offer turns out not to work, which is the specific confusion it was added to prevent.
func (m *Model) footerHeight() int {
	// Minimal mode has no sidebar, so it has no footer either. Gating on the hints alone
	// left the action row drawn over a column that is not supposed to exist.
	if !m.showSearchField || len(m.hints) == 0 {
		return 0
	}
	// The brand, its rule, the search field, its rule and at least one row: below that
	// the footer would leave the list with nothing to show.
	if m.rect.Height < layout.BrandHeight+layout.SearchHeight+3 {
		return 0
	}
	return layout.SidebarFooterHeight
}

// footerRect returns the action row's rectangle, empty when there is no footer.
func (m *Model) footerRect() layout.Rect {
	h := m.footerHeight()
	if h == 0 {
		return layout.Rect{}
	}
	return layout.Rect{
		X:      m.rect.X,
		Y:      m.rect.Bottom() - h,
		Width:  m.rect.Width,
		Height: h,
	}
}

// Offset returns the first visible row, for the application's status line.
func (m *Model) Offset() int { return m.offset }

// Cursor returns the selected row index.
func (m *Model) Cursor() int { return m.cursor }

// SelectedChat returns the highlighted chat.
func (m *Model) SelectedChat() (models.Chat, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return models.Chat{}, false
	}
	return m.rows[m.cursor], true
}

// Update implements [component.Region].
//
// It returns an event rather than performing the action. "The user chose this
// chat" is a fact; "open this chat" is a decision, and it belongs to the
// application.
func (m *Model) Update(msg tea.Msg) (component.Region, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) (component.Region, tea.Cmd) {
	// The search field consumes printable input while it has the keyboard. Everything
	// else falls through to the bindings below, so a global shortcut still works
	// while the user is typing a query — the root resolves those first.
	if m.searchFocused {
		switch {
		case component.IsNamed(msg, "enter"):
			m.searchFocused = false
			if chat, ok := m.SelectedChat(); ok {
				return m, component.EventCmd(component.Event{
					Kind: component.KindFocusChat, ChatID: chat.ID,
				})
			}
			return m, component.EventCmd(component.Event{Kind: component.KindSearchDismissed})

		case component.IsNamed(msg, "backspace"):
			if m.search != "" {
				m.search = truncateLastRune(m.search)
				return m, m.searchChanged()
			}

		case component.IsPrintable(msg):
			m.search += msg.Text
			return m, m.searchChanged()
		}
	}

	switch m.keys.Resolve(component.ConvertKey(msg), keybindings.PanelSidebar) {
	case keybindings.NavUp:
		return m.move(-1)
	case keybindings.NavDown:
		return m.move(1)
	case keybindings.ChatOpen:
		if chat, ok := m.SelectedChat(); ok {
			return m, component.EventCmd(component.Event{
				Kind: component.KindFocusChat, ChatID: chat.ID,
			})
		}
		return m, nil
	case keybindings.ActionToggleRead, keybindings.ActionTogglePin,
		keybindings.ActionToggleMute, keybindings.ActionToggleArchive,
		keybindings.ActionDeleteChat:
		// The action is named, not interpreted. The sidebar knows which row is
		// selected and which key was pressed; what "toggle mute" means is the
		// application's business, because only it knows what muting costs.
		action := m.keys.Resolve(component.ConvertKey(msg), keybindings.PanelSidebar)
		if chat, ok := m.SelectedChat(); ok && action != keybindings.ActionNone {
			return m, component.EventCmd(component.Event{
				Kind: component.KindMessageActivated, ChatID: chat.ID, Text: string(action),
			})
		}
		return m, nil

	default:
		// Not the sidebar's bindings. The application resolved the globals first, so
		// reaching here means the key belongs to another panel or to nothing at all,
		// and either way the sidebar has no business acting on it.
		return m, nil
	}
}

// searchChanged recomputes the filtered rows and reports the new query.
//
// The rows are recomputed from the cached list rather than deferred: filtering four
// hundred conversations is a microsecond, whereas a frame drawn with a stale filter is
// visibly wrong for as long as the next command takes to arrive.
func (m *Model) searchChanged() tea.Cmd {
	m.rows = m.filtered()
	m.clampCursor()
	return component.EventCmd(component.Event{
		Kind: component.KindSearchChanged, Query: m.search,
	})
}

// filtered applies the query to the last supplied conversation list.
func (m *Model) filtered() []models.Chat {
	out := make([]models.Chat, 0, len(m.chats))
	for _, c := range m.chats {
		if m.matches(c) {
			out = append(out, c)
		}
	}
	return out
}

// matches reports whether a conversation satisfies the current query.
//
// Archived conversations are included when they match, so the default filter keeps the
// list short without making an old conversation unfindable.
func (m *Model) matches(c models.Chat) bool {
	q := strings.ToLower(strings.TrimSpace(m.search))
	if q == "" {
		return !c.Archived
	}
	if strings.Contains(strings.ToLower(c.FallbackName()), q) {
		return true
	}
	if c.Contact != nil && strings.Contains(strings.ToLower(c.Contact.Phone), q) {
		return true
	}
	return strings.Contains(strings.ToLower(c.PreviewString()), q)
}

// move changes the cursor and scrolls it into view.
func (m *Model) move(delta int) (component.Region, tea.Cmd) {
	if len(m.rows) == 0 {
		m.cursor = -1
		return m, nil
	}
	m.cursor = clamp(m.cursor+delta, 0, len(m.rows)-1)
	m.scrollCursorIntoView()
	return m, nil
}

// scrollCursorIntoView adjusts the offset so the cursor is visible.
func (m *Model) scrollCursorIntoView() {
	visible := m.visibleRows()
	if visible <= 0 {
		m.offset = 0
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visible {
		m.offset = m.cursor - visible + 1
	}
	m.offset = clamp(m.offset, 0, maxInt(0, len(m.rows)-visible))
}

// visibleRows is how many chat rows fit.
func (m *Model) visibleRows() int {
	h := m.listRect().Height
	if h <= 0 {
		return 0
	}
	return h / RowHeight
}

// clampCursor keeps the cursor inside the row list.
func (m *Model) clampCursor() {
	switch {
	case len(m.rows) == 0:
		m.cursor = -1
	case m.cursor < 0:
		m.cursor = 0
	case m.cursor >= len(m.rows):
		m.cursor = len(m.rows) - 1
	}
	m.scrollCursorIntoView()
}

// BeginSearch gives the search field the keyboard.
//
// The field is owned by the sidebar rather than by the application because it lives in
// the sidebar's own rectangle: a field the sidebar does not draw is a field the sidebar
// has nowhere to put the caret.
func (m *Model) BeginSearch() { m.searchFocused = m.showSearchField }

// Searching reports whether the search field owns the keyboard.
func (m *Model) Searching() bool { return m.searchFocused }

// SelectChat moves the cursor to a conversation by identifier.
//
// It is how the application makes a chat current without reaching into the cursor
// index, so that the highlight and the open conversation cannot drift apart. An unknown
// identifier is a no-op rather than an error: the identifier may name a conversation
// that has since been archived out of the visible list.
func (m *Model) SelectChat(id models.ChatID) {
	for i, c := range m.rows {
		if c.ID != id {
			continue
		}
		if i == m.cursor {
			return
		}
		m.cursor = i
		m.scrollCursorIntoView()
		return
	}
}

// Scroll moves the list without moving the cursor, for the mouse wheel.
func (m *Model) Scroll(delta int) {
	visible := m.visibleRows()
	m.offset = clamp(m.offset+delta, 0, maxInt(0, len(m.rows)-visible))
}

// ClickAt selects the row under a terminal cell.
func (m *Model) ClickAt(x, y int) tea.Cmd {
	list := m.listRect()
	if !list.Contains(x, y) {
		return nil
	}
	// The row is the pointer's distance from the top of the list, plus the scroll
	// offset. Adding the list's own Y to that would count the band above the list
	// twice and select a row several positions below the one under the pointer.
	row := (y - list.Y) + m.offset
	if row < 0 || row >= len(m.rows) {
		return nil
	}
	m.cursor = row
	return component.EventCmd(component.Event{Kind: component.KindFocusChat, ChatID: m.rows[row].ID})
}

// View implements [component.Region].
//
// # The frame is exact by construction
//
// The rows are assembled into a slice of exactly [Model.rect]'s height and the footer is
// placed at the last index, rather than being appended after whatever the rows happened
// to produce. Counting on the append to land right is how the action hint ended up one
// cell short and one row too tall: both were invisible until a test compared the frame
// against the rectangle it was given, which is the only thing that can catch it.
func (m *Model) View() string {
	if m.rect.Empty() {
		return ""
	}
	st := m.theme.Styles
	g := m.theme.Glyphs

	lines := make([]string, m.rect.Height)
	blank := strings.Repeat(" ", m.rect.Width)
	for i := range lines {
		lines[i] = blank
	}

	row := 0
	put := func(s string, rows int) {
		for r := range rows {
			if row+r >= len(lines) {
				return
			}
			lines[row+r] = s
		}
		row += rows
	}

	if m.showBrand {
		put(text.PadRight(m.viewBrandMark(st), m.rect.Width), 1)
		put(m.rule(st, g), 1)
	}
	if m.showSearchField {
		put(m.viewSearch(st, g), 1)
		put(m.rule(st, g), 1)
	}

	// The rows fill whatever is between the search field and the footer. Padding is not
	// written: the slice is already blank, and writing blanks over blanks is how the
	// arithmetic used to go wrong.
	list := m.listRect()
	for i := range list.Height {
		idx := m.offset + i
		if idx >= len(m.rows) {
			continue
		}
		if r := list.Y + i; r < len(lines) {
			lines[r] = m.viewRow(m.rows[idx], idx == m.cursor, st, g)
		}
	}

	if footer := m.footerRect(); !footer.Empty() {
		if footer.Y < len(lines) {
			lines[footer.Y] = m.viewFooter(st, g)
		}
	}

	return strings.Join(lines, "\n")
}

// viewBrandMark renders the wordmark on its own, without its rule.
func (m *Model) viewBrandMark(st theme.Styles) string {
	if m.focus {
		return st.Accent.Bold(true).Render(" " + m.theme.Brand)
	}
	return st.Muted.Render(" " + m.theme.Brand)
}

// viewFooter renders the action hint for the selected conversation.
//
// Two entries at most, and trimmed from the right: a footer listing every binding is a
// footer listing none, because the one the user needs is as likely to be the fourth as
// the first.
func (m *Model) viewFooter(st theme.Styles, g theme.Glyphs) string {
	const maxHints = 2

	shown := m.hints
	if len(shown) > maxHints {
		shown = shown[:maxHints]
	}

	// Entries are dropped from the right until the row fits. A footer that wrapped
	// would push the list up and the whole column would jitter, so trimming is the
	// only acceptable way to be too wide.
	line := m.renderHints(st, g, shown)
	for line != "" && text.VisibleWidth(line)+1 > m.rect.Width {
		shown = shown[:len(shown)-1]
		line = m.renderHints(st, g, shown)
	}

	// Padded rather than truncated: the row is the full width of the column, and a
	// short one leaves the sidebar's background stopping short of the divider.
	return text.PadRight(text.TruncateStyled(line, maxInt(m.rect.Width, 0)), m.rect.Width)
}

// renderHints builds a hint row from a slice of entries.
func (m *Model) renderHints(st theme.Styles, g theme.Glyphs, hints []keybindings.HelpEntry) string {
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		if len(h.Keys) == 0 {
			continue
		}
		label := h.Short
		if label == "" {
			label = h.Help
		}
		key := st.StatusKey.Render(g.KeyHintOpen + h.Keys[0] + g.KeyHintClose)
		parts = append(parts, key+" "+st.StatusLabel.Render(label))
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, "  ")
}

func brandRows(m *Model) int {
	if m.showBrand {
		return layout.BrandHeight + 1
	}
	return 0
}

func searchRows(m *Model) int { return m.searchFieldHeight() }

// viewBrand renders the wordmark and its rule.
// rule renders the horizontal rule that separates the sidebar's bands.
func (m *Model) rule(st theme.Styles, g theme.Glyphs) string {
	return st.Divider.Render(strings.Repeat(g.Divider, maxInt(m.rect.Width, 0)))
}

// viewSearch renders the search field on its own, without its rule.
//
// Padded to the column's width rather than left short: the sidebar's background has to
// reach the divider, or the frame shows a stripe of the terminal's own colour down the
// side of the column.
func (m *Model) viewSearch(st theme.Styles, g theme.Glyphs) string {
	var b strings.Builder

	prompt := st.StatusLabel.Render(g.Search + " ")
	if m.searchFocused {
		prompt = st.Accent.Render(g.Search + " ")
	}

	// The field's contents, truncated to what is left after the prompt. Truncating
	// the contents rather than the prompt is what keeps the prompt visible however
	// long the query gets — a search field whose glyph scrolls away is a field the
	// user cannot tell apart from a label.
	room := maxInt(m.rect.Width-text.VisibleWidth(prompt), 0)
	body := text.PadRight(text.Truncate(m.search, room), room)

	switch {
	case m.search != "":
		b.WriteString(prompt + st.ChatTitle.Render(body))
	case m.searchFocused:
		// The placeholder is styled like real text while the field has focus: it is
		// what is being overwritten, and dimming it would look like the field is empty
		// and disabled.
		b.WriteString(prompt + st.ChatTitle.Render(text.PadRight("Buscar chats…", room)))
	default:
		b.WriteString(st.Muted.Render(prompt + text.PadRight("Buscar chats…", room)))
	}

	return b.String()
}

// viewRow renders one chat line.
//
// # The layout of one line
//
//	▾ Ada Lovelace              2  22:37 📌
//	  Hola, ¿llegaron las notas?      🔇
//
// The name leads because it is what identifies the conversation; the timestamp sits on
// the right because it is what the eye scans a list for; the preview takes what is
// left, dimmed, because it is the first thing dropped when the column narrows and the
// least useful when scanning.
//
// The order of sacrifice is preview, then timestamp, then name. A row with no name is
// an anonymous row, and a row with no time gives no clue about whether the
// conversation is current — which is the whole reason to keep a chat list at all.
func (m *Model) viewRow(chat models.Chat, selected bool, st theme.Styles, g theme.Glyphs) string {
	w := m.rect.Width
	if w <= 0 {
		return ""
	}

	// The cursor marker takes the first column when the list has focus, so the
	// selection is identifiable without relying on colour — which is what a monochrome
	// terminal, or a colour-blind reader, actually sees.
	marker := " "
	if selected && m.focus {
		marker = st.Accent.Render(g.Chevron)
	}

	timestamp := models.FormatChatTimestamp(chat.Timestamp)
	badge := ""
	if chat.HasUnread && chat.UnreadCount > 0 {
		badge = st.ChatBadge.Render(text.Itoa(chat.UnreadCount))
	}

	// The pinned and muted marks sit after the timestamp so they cannot push the times
	// out of alignment down the column. Every row's time lining up is what makes a
	// time column scannable.
	flags := ""
	if chat.Pinned {
		flags += g.Pinned
	}
	if chat.Muted {
		flags += g.Muted
	}
	if flags != "" {
		flags = " " + flags
	}

	// The right-hand block is built once, because its width is the anchor the whole
	// row is laid out against: everything to its left is what is left of the column.
	right := st.ChatTime.Render(timestamp)
	if badge != "" {
		right = badge + " " + right
	}
	right += flags
	rightW := text.VisibleWidth(right)

	// Everything to the left of the right-hand block is shared between the name and
	// the preview.
	left := w - 1 - rightW
	if left < 1 {
		// Too narrow for a name and a time side by side. The name still wins, because
		// it is the only part that identifies the conversation.
		return text.PadRight(
			marker+st.ChatTitle.Render(text.Truncate(chat.FallbackName(), maxInt(w-1, 0))), w)
	}

	// The name's width and the preview's are decided together, not in sequence.
	//
	// Sizing the name first and then measuring what is left for the preview truncates
	// both when the column is narrow: at 26 columns a pinned conversation with unread
	// messages has fourteen cells left, three fifths of which is eight — enough for
	// "Ada Lo…" and too little for a preview worth drawing. Deciding the preview first
	// means the name either gets a readable width or gets all of it, and the two are
	// never both fragments.
	const readableName = 12 // about "Ada Lovelace"

	nameW, previewW := left, 0
	if room := left - minInt(readableName, left) - 1; room >= MinPreviewWidth {
		// Both fit. The name takes the larger share and is capped, because a name past
		// twenty characters is a group and stops benefiting at the preview's expense.
		nameW = minInt(left*3/5, 20)
		previewW = left - nameW - 1
	}

	preview := chat.PreviewString()
	switch {
	case chat.Typing:
		// Typing replaces the preview: it is the more urgent fact, and a stale preview
		// beside a live indicator reads as though the preview were the new message.
		preview = g.Typing + " escribiendo…"
	case preview == "":
		preview = g.Placeholder
	}

	name := chat.FallbackName()
	if chat.HasUnread && chat.UnreadCount > 0 {
		name = st.ChatTitleUnread.Render(text.Truncate(name, nameW))
	} else {
		name = st.ChatTitle.Render(text.Truncate(name, nameW))
	}

	left_ := marker + name
	if previewW > 0 {
		left_ += " " + st.ChatPreview.Render(text.Truncate(preview, previewW))
	}

	// left_ begins with the cursor marker, so it is padded to the full left budget
	// including that marker rather than to `left` alone — otherwise every row comes
	// out one cell short and the column's background stops at the divider.
	row := text.PadRight(left_, maxInt(w-rightW, 0)) + right

	// The highlight covers the whole line, so a selected row reads as one object
	// rather than as a coloured name in a column of plain ones.
	if selected && m.focus {
		return st.ChatRowSelected.Render(row)
	}
	return row
}

// compile-time proof that the sidebar satisfies the region contract.
var _ component.Region = (*Model)(nil)

// truncateLastRune removes the final rune, for backspace.
//
// Runes rather than bytes, because a byte-at-a-time removal from a multi-byte
// character leaves a broken encoding behind, which the renderer then draws as a
// replacement character in the search field.
func truncateLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[:len(r)-1])
}

// clamp limits v to [lo, hi].
func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RowCount returns how many conversations pass the current filter.
//
// It is exported for the application's diagnostics, and it is the honest way to ask
// "is this conversation visible": a rendered name is truncated to the column's width,
// so grepping the frame for it tests the name's length rather than the list's contents.
func (m *Model) RowCount() int { return len(m.rows) }
