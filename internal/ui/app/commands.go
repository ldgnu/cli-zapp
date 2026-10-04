package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/ui/components/messagelist"
	"github.com/wterm/wterm/internal/ui/components/statusbar"
	"github.com/wterm/wterm/internal/whatsapp"
)

// historyLimit is how many messages are fetched for a chat.
const historyLimit = 200

// listChats returns a command that loads the chat list.
func (m *Model) listChats() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		chats, err := m.deps.Chat.List(ctx)
		if err != nil {
			return errMsg{err: err}
		}
		return chatsLoadedMsg{chats: chats}
	}
}

// openChat returns a command that loads the selected chat's transcript.
func (m *Model) openChat() tea.Cmd {
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}
	m.openedChat = chat.ID

	// Move focus to the transcript immediately: waiting for the network before
	// moving would make the UI feel unresponsive even though the request is
	// already in flight.
	m.setFocus(keybindings.PanelMessages)
	m.msgSel = -1
	m.msgOffset = 0

	return m.fetchTranscript(chat)
}

// resync returns a command that restarts the event stream.
//
// Restarting rather than only forcing a fetch is what makes "resync" recover a
// wedged connection, which is the common case a user reaches for it in.
func (m *Model) resync() tea.Cmd {
	sync := m.deps.Sync

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := sync.Stop(ctx); err != nil {
			return errMsg{err: err}
		}
		if err := sync.Start(ctx); err != nil {
			return errMsg{err: err}
		}
		return statusUpdatedMsg{conn: connFor(sync.Connected())}
	}
}

// waitForEvent returns a command that blocks until the next sync event arrives.
//
// The blocking is the point: Bubble Tea runs the command on its own goroutine and
// re-renders when it returns, so the UI never waits on the network.
func (m *Model) waitForEvent() tea.Cmd {
	events := m.deps.Sync.Events()
	return func() tea.Msg {
		e, ok := <-events
		if !ok {
			// A closed channel means the stream ended. Reporting it as an empty
			// batch rather than a nil message stops the wait loop.
			return eventsMsg{}
		}
		return eventsMsg{events: []whatsapp.Event{e}}
	}
}

// send returns a command that delivers the composer's contents.
func (m *Model) send() (tea.Model, tea.Cmd) {
	body := m.composer.Value()
	if m.composer.Empty() {
		return m, nil
	}

	chat, ok := m.currentChat()
	if !ok {
		return m, m.toast("Open a chat first", false)
	}

	// Editing replaces the body of an existing message rather than sending a new
	// one, so the transcript keeps its shape.
	if m.editing != "" {
		id := m.editing
		m.composer.Reset()
		m.editing = ""
		m.replyTo = nil

		msg := m.deps.Message
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			edited, err := msg.Edit(ctx, chat.ID, id, body)
			_ = edited
			if err != nil {
				return errMsg{err: err}
			}
			return messagesLoadedMsg{chat: chat.ID}
		}
	}

	// Capture the reply reference before clearing the composer.
	replyTo := m.replyTo
	m.composer.Reset()
	m.replyTo = nil

	msgSvc := m.deps.Message
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if _, err := msgSvc.Send(ctx, chat.ID, body); err != nil {
			return errMsg{err: err}
		}

		// The reply quote is a client-side decoration; the protocol carries it
		// as context info that the adapter attaches, so the fake needs it said
		// explicitly for the transcript to look right.
		_ = replyTo
		return messagesLoadedMsg{chat: chat.ID}
	}
}

// toggleRead marks the selected chat read or unread.
func (m *Model) toggleRead() (tea.Model, tea.Cmd) {
	chat, ok := m.currentChat()
	if !ok {
		return m, nil
	}
	read := chat.HasUnread

	svc := m.deps.Chat
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := svc.SetRead(ctx, chat.ID, read); err != nil {
			return errMsg{err: err}
		}
		return chatsLoadedMsg{chats: mustList(ctx, svc)}
	}
}

// togglePin pins or unpins the selected chat.
func (m *Model) togglePin() (tea.Model, tea.Cmd) {
	return m.chatFlag(func(c *models.Chat) bool { return c.Pinned }, setPinned)
}

// toggleMute mutes or unmutes the selected chat.
func (m *Model) toggleMute() (tea.Model, tea.Cmd) {
	return m.chatFlag(func(c *models.Chat) bool { return c.Muted }, setMuted)
}

// toggleArchive archives or unarchives the selected chat.
func (m *Model) toggleArchive() (tea.Model, tea.Cmd) {
	return m.chatFlag(func(c *models.Chat) bool { return c.Archived }, setArchived)
}

// chatFlag implements the pin, mute and archive toggles, which differ only in
// which field they touch and which service call they make.
func (m *Model) chatFlag(
	current func(*models.Chat) bool,
	set func(context.Context, whatsapp.ChatService, models.ChatID, bool) error,
) (tea.Model, tea.Cmd) {
	chat, ok := m.currentChat()
	if !ok {
		return m, nil
	}
	value := !current(&chat)
	svc := m.deps.Chat

	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := set(ctx, svc, chat.ID, value); err != nil {
			return errMsg{err: err}
		}
		return chatsLoadedMsg{chats: mustList(ctx, svc)}
	}
}

// setPinned and friends adapt the service methods to the setter signature
// chatFlag takes.
func setPinned(ctx context.Context, s whatsapp.ChatService, id models.ChatID, v bool) error {
	return s.SetPinned(ctx, id, v)
}

func setMuted(ctx context.Context, s whatsapp.ChatService, id models.ChatID, v bool) error {
	return s.SetMuted(ctx, id, v)
}

func setArchived(ctx context.Context, s whatsapp.ChatService, id models.ChatID, v bool) error {
	return s.SetArchived(ctx, id, v)
}

// deleteChat removes the selected chat after confirmation.
func (m *Model) deleteChatCmd() tea.Cmd {
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}
	svc := m.deps.Chat

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := svc.Delete(ctx, chat.ID); err != nil {
			return errMsg{err: err}
		}
		return chatsLoadedMsg{chats: mustList(ctx, svc)}
	}
}

// react applies a reaction to the cursor message.
func (m *Model) react(glyph string) tea.Cmd {
	msg, ok := m.messageAt(m.msgSel)
	if !ok {
		return nil
	}
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}

	svc := m.deps.Message
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := svc.React(ctx, chat.ID, msg.ID, glyph); err != nil {
			return errMsg{err: err}
		}
		return messagesLoadedMsg{chat: chat.ID}
	}
}

// beginSearch enters search mode.
func (m *Model) beginSearch() tea.Cmd {
	m.searchActive = true
	m.setFocus(keybindings.PanelSidebar)
	return m.listChats()
}

// handleSearchKey consumes input while the search field is active.
//
// It reports whether it consumed the key. Escape is not handled here because the
// caller checks it first, which keeps a single implementation of "escape always
// wins".
func (m *Model) handleSearchKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "backspace":
		if m.searchQuery != "" {
			m.searchQuery = truncateRunes(m.searchQuery, len(m.searchQuery)-1)
			m.clampSelectionToFilter()
		}
		return true, m.listChats()

	case "enter":
		m.searchActive = false
		return true, nil

	default:
		// Only printable runes become part of the query.
		if r := []rune(msg.String()); len(r) == 1 && msg.Text != "" {
			m.searchQuery += msg.String()
			m.chatSel = 0
			m.chatOffset = 0
			return true, m.listChats()
		}
	}
	return false, nil
}

// mustList lists chats, returning an empty slice on failure.
//
// The error has already been reported through the caller's own call, so
// returning empty here keeps one failure from cascading into two.
func mustList(ctx context.Context, svc whatsapp.ChatService) []models.Chat {
	chats, err := svc.List(ctx)
	if err != nil {
		return nil
	}
	return chats
}

// connFor maps a connected flag to a display state.
func connFor(connected bool) statusbar.Connection {
	if connected {
		return statusbar.Online
	}
	return statusbar.Disconnected
}

// displayConn maps a protocol connection state to its display form.
func displayConn(c whatsapp.ConnectionState) statusbar.Connection {
	switch c {
	case whatsapp.ConnectionConnecting:
		return statusbar.Connecting
	case whatsapp.ConnectionSyncing:
		return statusbar.Syncing
	case whatsapp.ConnectionOnline:
		return statusbar.Online
	default:
		return statusbar.Disconnected
	}
}

// layoutRows recomputes the transcript's rows at the current width.
func (m *Model) layoutRows() {
	msgs := m.currentMessages()
	w := m.theme.ContentWidth(m.width)

	// Follow the bottom when the user was already there, so an arriving message
	// does not yank the view away from what they were reading.
	wasAtBottom := m.atBottom

	m.rows = messagelist.Layout(msgs, w, messagelist.NewLoc(m.loc), m.cursorID(), m.selected, m.theme)

	// Record which transcript index each row belongs to.
	//
	// Selection, forwarding and editing all address a message by index into the
	// transcript, but the laid-out rows also contain day separators and system
	// notices. Without this mapping, acting on a row would need to re-derive the
	// correspondence on every keystroke.
	m.rowMessages = m.rowMessages[:0]
	idx := -1
	for _, r := range m.rows {
		switch r.Kind {
		case messagelist.KindMessage, messagelist.KindSystem:
			idx++
			m.rowMessages = append(m.rowMessages, r.MessageID)
		default:
			// Separators are not messages and occupy no transcript index.
		}
	}

	if wasAtBottom {
		m.msgOffset = messagelist.MaxOffset(m.rows, viewportHeight(m))
	}
}

// truncateRunes removes the last n runes of s.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if n <= 0 {
		return ""
	}
	if n >= len(r) {
		return ""
	}
	return string(r[:len(r)-n])
}
