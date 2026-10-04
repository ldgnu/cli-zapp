package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"

	"github.com/cli-zapp/cli-zapp/internal/models"
	"github.com/cli-zapp/cli-zapp/internal/text"
	"github.com/cli-zapp/cli-zapp/internal/ui/component"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/composer"
)

// --- the open conversation ---

// currentChat returns the conversation the sidebar's cursor is on.
func (m *Model) currentChat() (models.Chat, bool) {
	chat, ok := m.sidebar.SelectedChat()
	if !ok {
		return models.Chat{}, false
	}
	return chat, true
}

// currentChatID returns the open conversation's identifier, empty when none is open.
func (m *Model) currentChatID() models.ChatID {
	chat, ok := m.currentChat()
	if !ok {
		return ""
	}
	return chat.ID
}

// currentChatName returns the open conversation's display name.
func (m *Model) currentChatName() string {
	if chat, ok := m.currentChat(); ok {
		return chat.FallbackName()
	}
	return ""
}

// chatByID looks up a conversation in the cached list.
func (m *Model) chatByID(id models.ChatID) (models.Chat, bool) {
	for _, c := range m.chats {
		if c.ID == id {
			return c, true
		}
	}
	return models.Chat{}, false
}

// --- derived sidebar state ---

// visibleChats returns the conversations passing the current filter.
//
// Sorting happens here rather than in the sidebar so that the order is a property of
// the application's data, not of one renderer. A different sidebar would then sort the
// same way without being asked.
func (m *Model) visibleChats() []models.Chat {
	out := make([]models.Chat, 0, len(m.chats))
	for _, c := range m.chats {
		// Archived conversations are hidden unless they match a query: the list
		// should be short by default, but an archived conversation the user
		// remembers must still be findable.
		if c.Archived && m.search == "" {
			continue
		}
		out = append(out, c)
	}
	models.SortChats(out)
	return out
}

// unreadTotal counts unread messages across every conversation.
//
// Muted conversations are excluded. A notification client that counts messages the
// user chose not to be notified about is nagging them about their own settings.
func (m *Model) unreadTotal() int {
	total := 0
	for _, c := range m.chats {
		if c.Muted {
			continue
		}
		total += c.UnreadCount
	}
	return total
}

// typingIn names the conversation whose peer is composing, or "".
func (m *Model) typingIn() string {
	for _, c := range m.chats {
		if c.Typing {
			return c.FallbackName()
		}
	}
	return ""
}

// --- region synchronisation ---

// sharedState builds the value handed to the regions each frame.
//
// It is passed by value so that a region cannot mutate the application's state by
// reaching into the slice it was given.
func (m *Model) sharedState() component.Model {
	return component.Model{
		Chats:         m.visibleChats(),
		CurrentChatID: m.currentChatID(),
		Messages:      m.messages,
		TypingIn:      m.typingIn(),
		UnreadTotal:   m.unreadTotal(),
		Selected:      m.selected,
		Search:        m.search,
		SearchActive:  m.searching,
		// The sidebar's footer is scoped to its own panel: the actions that apply to the
		// highlighted conversation, not the ones that apply wherever the keyboard happens
		// to be focused. It is the same list the status bar draws from, and the two can
		// never disagree because there is only one of it.
		Hints: m.keys.Hints(keybindings.PanelSidebar),
	}
}

// syncRegions pushes the application's state into every region.
//
// It is called after anything that could change what a region draws. Doing it here
// rather than in each region's own Update is what keeps the call sites from drifting:
// there is one place that knows a frame needs its inputs refreshed.
func (m *Model) syncRegions() {
	m.syncSidebar()
	m.syncTranscript()
}

// syncSidebar pushes the state the sidebar draws from.
func (m *Model) syncSidebar() { m.sidebar.SetModel(m.sharedState()) }

// syncTranscript pushes the state the transcript draws from.
func (m *Model) syncTranscript() {
	m.transcript.SetModel(m.sharedState())
	if chat, ok := m.currentChat(); ok {
		m.transcript.SetGroupChat(chat.Type.IsGroup())
	}
}

// --- composition modes ---

// beginReply starts a reply to the cursor message.
//
// A revoked message cannot be replied to: its content is gone, so a quote of it would
// be an empty line saying nothing about nothing.
func (m *Model) beginReply() tea.Cmd {
	msg, ok := m.transcript.MessageAt(m.transcript.Cursor())
	if !ok || msg.Revoked {
		return nil
	}

	// Replying supersedes an edit in progress. The two are mutually exclusive by
	// construction — the composer holds one mode — so the impossible state of quoting
	// one message while editing another cannot be represented.
	m.editing = ""
	name := m.senderName(msg)
	m.replyTo = &models.MessageRef{
		ID:         msg.ID,
		SenderID:   msg.SenderID,
		SenderName: name,
		Body:       msg.Text(),
		Kind:       msg.Kind,
		Timestamp:  msg.Timestamp,
		Revoked:    msg.Revoked,
	}
	m.composer.SetReply(name)
	return m.setFocus(component.RegionComposer)
}

// beginEdit starts editing the cursor message.
//
// Only the account's own messages can be edited. Refusing with an explanation rather
// than silently doing nothing matters: a key that appears dead is indistinguishable
// from a broken binding, and the user will go looking for the wrong bug.
func (m *Model) beginEdit() tea.Cmd {
	msg, ok := m.transcript.MessageAt(m.transcript.Cursor())
	switch {
	case !ok:
		return nil
	case msg.Revoked:
		return m.toast("ese mensaje fue eliminado", false)
	case !msg.IsOutgoing():
		return m.toast("solo se pueden editar tus mensajes", false)
	}

	m.replyTo = nil
	m.editing = msg.ID
	m.composer.SetEdit(m.senderName(msg), msg.Text())
	return m.setFocus(component.RegionComposer)
}

// senderName returns who sent a message, in a form suitable for quoting.
func (m *Model) senderName(msg models.Message) string {
	switch {
	case msg.IsOutgoing():
		return "tú"
	case m.senderNames[msg.SenderID] != "":
		return m.senderNames[msg.SenderID]
	default:
		// "alguien" rather than a blank: a quote whose author is missing reads as a
		// rendering failure, whereas one that says "someone" reads as what it is.
		return "alguien"
	}
}

// --- helpers ---

// centre puts s on a line exactly width cells wide.
func centre(s string, width int) string {
	pad := width - text.VisibleWidth(s)
	if pad <= 0 {
		return s
	}
	left := pad / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", pad-left)
}

// plural renders a count with a correctly pluralised noun.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return text.Itoa(n) + " " + noun + "s"
}

// orDash returns s, or an em dash when empty, for info panels.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// composerModeSend is the composer's ordinary mode, aliased so the comparison in
// onEscape reads as a mode check rather than as a magic number.
const composerModeSend = composer.ModeSend

// indexSenders builds the contact-name lookup used when quoting a message.
//
// It is built from the cached conversations rather than queried per message because a
// quote is rendered on every frame of a reply, and a service call per frame would make
// scrolling a conversation stutter.
func (m *Model) indexSenders() {
	for _, c := range m.chats {
		if c.Contact != nil && c.Contact.Name != "" {
			m.senderNames[c.Contact.ID] = c.Contact.Name
		}
	}
}

// maxInt returns the larger of two ints.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// repeatLines returns n copies of s as separate lines.
//
// The lines are joined without a trailing newline by the caller. A trailing one would
// be counted as an extra empty row by every join that follows, adding exactly one row
// to the frame — which is how a status bar ends up pushed off the bottom of the screen
// by a one-character divider.
func repeatLines(s string, n int) []string {
	if n <= 0 {
		return nil
	}
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}
