package app

import (
	"context"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/ui/components/chatlist"
)

// visibleChats returns the chats passing the current search filter.
//
// Archived chats are excluded unless the query mentions them, which keeps the
// sidebar usable once a list accumulates dozens of archived conversations.
func (m *Model) visibleChats() []models.Chat {
	chats := m.chats()

	q := strings.ToLower(strings.TrimSpace(m.searchQuery))
	if q == "" {
		return m.unarchived(chats)
	}

	out := make([]models.Chat, 0, len(chats))
	for _, c := range chats {
		if !matches(c, q) {
			continue
		}
		// A search should reach archived chats; otherwise "find that old group"
		// is impossible.
		out = append(out, c)
	}
	return out
}

func (m *Model) unarchived(chats []models.Chat) []models.Chat {
	out := make([]models.Chat, 0, len(chats))
	for _, c := range chats {
		if c.Archived {
			continue
		}
		out = append(out, c)
	}
	return out
}

// matches reports whether a chat matches a lowercased query.
func matches(c models.Chat, q string) bool {
	if strings.Contains(strings.ToLower(c.FallbackName()), q) {
		return true
	}
	if c.Contact != nil && strings.Contains(strings.ToLower(c.Contact.Phone), q) {
		return true
	}
	// Searching the preview makes it possible to find a conversation by what
	// was said in it, which is what people actually try to do.
	return strings.Contains(strings.ToLower(c.PreviewString()), q)
}

// chats returns the cached chat list.
//
// The slice is sorted on read rather than stored sorted, because a background
// refresh replaces the cache wholesale and re-sorting there would make the
// selected row jump whenever a new message arrived.
func (m *Model) chats() []models.Chat {
	if len(m.chatCache) == 0 {
		return nil
	}
	out := make([]models.Chat, len(m.chatCache))
	copy(out, m.chatCache)
	models.SortChats(out)
	return out
}

// setChats replaces the cached chat list, preserving the selection where
// possible.
//
// Preserving the selection by identifier rather than by index is what keeps the
// highlighted chat stable when a message arrives and reorders the list.
func (m *Model) setChats(chats []models.Chat) tea.Cmd {
	previous := m.selectedChatID()

	m.chatCache = chats

	if previous == "" {
		// No prior selection: land on the first chat, or nothing if the list is
		// empty.
		if len(chats) > 0 {
			m.chatSel = 0
		} else {
			m.chatSel = -1
		}
	} else {
		m.chatSel = m.indexOfChat(previous)
	}

	m.clampSelectionToFilter()
	m.chatOffset = m.chatOffsetFor(m.chatSel)

	// Open the first conversation as soon as the list arrives. Doing it here
	// rather than in Init is what makes it work: the selection is only known once
	// the list has been loaded, so a fetch issued from Init would have nothing to
	// open yet.
	if m.openedChat == "" && m.chatSel >= 0 {
		if chat, ok := m.currentChat(); ok {
			m.openedChat = chat.ID
			return m.queue(m.fetchTranscript(chat))
		}
	}
	return nil
}

// fetchTranscript loads a chat's messages without moving focus.
//
// Separated from openChat because showing a conversation and fetching its history
// are different concerns: the first changes where typing goes, the second does
// not.
func (m *Model) fetchTranscript(chat models.Chat) tea.Cmd {
	svc := m.deps.Message
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		msgs, err := svc.History(ctx, chat.ID, historyLimit)
		if err != nil {
			return errMsg{err: err}
		}
		return messagesLoadedMsg{chat: chat.ID, messages: msgs}
	}
}

// selectedChatID returns the identifier of the selected chat.
func (m *Model) selectedChatID() models.ChatID {
	visible := m.visibleChats()
	if m.chatSel < 0 || m.chatSel >= len(visible) {
		return ""
	}
	return visible[m.chatSel].ID
}

// indexOfChat finds a chat's position in the filtered list.
func (m *Model) indexOfChat(id models.ChatID) int {
	for i, c := range m.visibleChats() {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// currentChat returns the open chat.
func (m *Model) currentChat() (models.Chat, bool) {
	visible := m.visibleChats()
	if m.chatSel < 0 || m.chatSel >= len(visible) {
		return models.Chat{}, false
	}
	return visible[m.chatSel], true
}

// clampSelectionToFilter keeps the selection inside the filtered list.
//
// Needed because the filter can shrink under the selection: typing a query that
// matches nothing must not leave a highlight pointing at an index that no longer
// exists.
func (m *Model) clampSelectionToFilter() {
	n := len(m.visibleChats())
	switch {
	case n == 0:
		m.chatSel = -1
	case m.chatSel < 0:
		m.chatSel = 0
	case m.chatSel >= n:
		m.chatSel = n - 1
	}
}

// chatOffsetFor returns the sidebar scroll offset for a selection.
func (m *Model) chatOffsetFor(sel int) int {
	return chatlist.OffsetForSelection(sel, len(m.visibleChats()), m.sidebarRows())
}

// maxChatOffset returns the largest valid sidebar scroll offset.
func (m *Model) maxChatOffset() int {
	rows := m.sidebarRows()
	return maxInt(0, len(m.visibleChats())-rows)
}

// sidebarRows returns how many chat rows fit in the sidebar.
//
// The sidebar's header, search box, divider and the status bar all consume
// space, and getting this wrong is what makes the scrollbar overshoot.
func (m *Model) sidebarRows() int {
	// 1 status bar + 3 chrome lines (header, search, divider).
	h := m.height - m.theme.Metrics.StatusHeight - 3
	if h < 0 {
		h = 0
	}
	return h / chatlist.RowHeight
}

// currentMessages returns the open chat's transcript.
func (m *Model) currentMessages() []models.Message {
	if len(m.msgCache) == 0 {
		return nil
	}
	return m.msgCache
}

// setMessages replaces the cached transcript for a chat.
//
// The chat check matters: a slow history response for a chat the user has since
// left must not overwrite the transcript they are now reading.
func (m *Model) setMessages(chatID string, msgs []models.Message) {
	if id := models.ChatID(chatID); id != m.currentChatID() {
		return
	}

	wasAtBottom := m.atBottom
	previous := m.cursorID()

	m.msgCache = msgs

	// The transcript may have been re-fetched without messages, in which case
	// there is nothing to select.
	if len(msgs) == 0 {
		m.msgSel = -1
		m.rows = nil
		m.msgOffset = 0
		return
	}

	m.msgSel = m.indexOfMessage(previous)
	if m.msgSel < 0 {
		// Follow the newest message, which is what a chat client does when a
		// conversation is opened.
		m.msgSel = len(msgs) - 1
	}
	m.atBottom = wasAtBottom || m.msgSel >= len(msgs)-1

	m.layoutRows()
}

// indexOfMessage finds a message's position in the transcript.
func (m *Model) indexOfMessage(id models.MessageID) int {
	if id == "" {
		return -1
	}
	for i, msg := range m.currentMessages() {
		if msg.ID == id {
			return i
		}
	}
	return -1
}

// messageAt returns the transcript entry at an index.
func (m *Model) messageAt(i int) (models.Message, bool) {
	msgs := m.currentMessages()
	if i < 0 || i >= len(msgs) {
		return models.Message{}, false
	}
	return msgs[i], true
}

// messageCount returns the transcript length.
func (m *Model) messageCount() int { return len(m.currentMessages()) }

// cursorID returns the message under the keyboard cursor.
func (m *Model) cursorID() models.MessageID {
	msg, ok := m.messageAt(m.msgSel)
	if !ok {
		return ""
	}
	return msg.ID
}

// senderName resolves a message's display name.
//
// For a group, the push name is needed because a group transcript is
// meaningless without knowing who said what.
func (m *Model) senderName(msg models.Message) string {
	chat, ok := m.currentChat()
	if !ok {
		return string(msg.SenderID)
	}
	if chat.Contact != nil && chat.Contact.ID == msg.SenderID {
		return chat.Contact.DisplayName()
	}

	// A cached roster is out of scope for Phase 1; the identifier stands in, and
	// the group roster replaces it in Phase 6.
	if chat.Type.IsGroup() {
		return string(msg.SenderID)
	}
	return string(msg.SenderID)
}

// viewportHeight returns the number of transcript lines visible.
func viewportHeight(m *Model) int {
	if m.height <= 0 {
		return 1
	}
	// Header, composer and status bar.
	mt := m.theme.Metrics
	h := m.height - mt.HeaderHeight - mt.ComposerHeight - mt.StatusHeight
	return maxInt(h, 1)
}

// sortedSelectedIDs returns the selected identifiers in a stable order, so that
// operations over a selection do not depend on map iteration.
func sortedSelectedIDs(sel map[models.MessageID]bool) []models.MessageID {
	out := make([]models.MessageID, 0, len(sel))
	for id := range sel {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

var _ = keybindings.ActionNone
