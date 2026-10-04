// Package app is wterm's root Bubble Tea model.
//
// # The layer boundary, enforced by construction
//
// This package imports internal/models, internal/keybindings, internal/ui/theme
// and the ui/components packages. It does not import the protocol, the database,
// or any networking package. Everything it needs from those arrives through the
// narrow interfaces in internal/whatsapp, which in Phase 1 is backed by an
// in-memory fake.
//
// That is not a stylistic preference: it is what allows the entire UI to be
// built, exercised and tested before any of it can be broken by an upstream
// protocol change.
//
// # State ownership
//
// The model is a value. Update returns a copy, and every mutation happens in
// place on that copy before it is returned. Bubble Tea's own guidance is that
// copying a model on every key press is wasteful, but it buys two properties
// this design needs: the previous frame can never be mutated by a later one, and
// the model can be snapshotted in a test with no synchronisation.
package app

import (
	"context"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/notifications"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/components/chatlist"
	"github.com/wterm/wterm/internal/ui/components/composer"
	"github.com/wterm/wterm/internal/ui/components/messagelist"
	"github.com/wterm/wterm/internal/ui/components/overlay"
	"github.com/wterm/wterm/internal/ui/components/statusbar"
	"github.com/wterm/wterm/internal/ui/theme"
	"github.com/wterm/wterm/internal/whatsapp"
)

// Model is the root Bubble Tea model.
type Model struct {
	// deps holds the service interfaces. They are interfaces so that the UI can
	// be tested against the in-memory fake.
	deps whatsapp.Services

	theme    theme.Theme
	keys     keybindings.Map
	composer *composer.Composer

	// Layout.
	width, height int

	// loc is the location used for day separators. It is captured at startup so
	// that every frame agrees on which calendar day a message belongs to.
	loc *time.Location

	// Data caches, replaced wholesale by service results.
	chatCache []models.Chat
	msgCache  []models.Message

	// rowMessages maps transcript indices to message identifiers, so that
	// selection survives a relayout at a different width.
	rowMessages []models.MessageID

	// openedChat is the chat whose transcript has been fetched, so that a
	// background refresh of the chat list does not re-fetch it.
	openedChat models.ChatID

	// Focus and selection.
	focus     keybindings.Panel
	prevFocus keybindings.Panel
	chatSel   int
	msgSel    int

	// Scroll offsets.
	chatOffset int
	msgOffset  int
	rows       []messagelist.Row
	atBottom   bool

	// Search.
	searchActive bool
	searchQuery  string

	// Compose mode.
	replyTo  *models.MessageRef
	editing  models.MessageID
	selected map[models.MessageID]bool

	// Overlay stack.
	overlays overlay.Stack

	// notifier raises alerts for messages arriving in background chats, and
	// pendingNotices is the queue drained on the UI goroutine.
	notifier       *notifications.Notifier
	pendingNotices []notifications.Event

	// Event pump. The sync stream is drained on its own goroutine and posted to
	// Bubble Tea, so the model itself is only ever touched from the UI goroutine.
	eventMu     sync.Mutex
	eventCancel context.CancelFunc
	program     *tea.Program

	// Connection.
	conn statusbar.Connection

	// quitting is set once the program should exit, so that Update can keep
	// processing messages without re-rendering a dead screen.
	quitting bool

	// DisableTimers suppresses sleeping commands. It is set by tests, which would
	// otherwise block for the duration of a toast's lifetime.
	DisableTimers bool

	// pending carries commands the model produced for itself — a fetch triggered
	// by a state change, say — as opposed to the command that is the direct reply
	// to the current message.
	//
	// The distinction is the point. A command that is the reply must be *returned*
	// from Update, because Bubble Tea only schedules what Update hands back; queueing
	// it would strand it and the key press would appear to do nothing. A
	// follow-up has no such caller, so queueing it until the next tick is both
	// correct and testable: a test can drain the queue without a running program.
	pending []tea.Cmd
}

// queue records follow-up commands for the next tick.
//
// It returns nil so that it composes cleanly as "return m, m.queue(...)" without
// the risk of it being mistaken for a command to schedule.
func (m *Model) queue(cmds ...tea.Cmd) tea.Cmd {
	for _, c := range cmds {
		if c != nil {
			m.pending = append(m.pending, c)
		}
	}
	return nil
}

// runPending drains the queued follow-ups.
func (m *Model) runPending() tea.Cmd {
	if len(m.pending) == 0 {
		return nil
	}
	cmds := m.pending
	m.pending = nil
	return tea.Batch(cmds...)
}

// reply combines the command produced by handling a message with any queued
// follow-ups.
//
// Returning both matters: returning only the follow-ups would drop the reply, and
// returning only the reply would drop the follow-ups. This is the single place
// the two are joined, so the omission cannot happen per call site.
func (m *Model) reply(cmd tea.Cmd) tea.Cmd {
	queued := m.runPending()
	switch {
	case queued == nil:
		return cmd
	case cmd == nil:
		return queued
	default:
		// A batch, not a sequence.
		//
		// The queued commands are independent fetches — refreshing the chat list
		// while opening a conversation — so there is nothing to order between them.
		// Sequence would express a dependency that does not exist, and it hides its
		// members behind a message the caller has to expand.
		return tea.Batch(queued, cmd)
	}
}

// New creates the root model.
func New(deps whatsapp.Services, t theme.Theme, keys keybindings.Map) *Model {
	return &Model{
		deps:     deps,
		theme:    t,
		keys:     keys,
		composer: composer.New(t.Metrics.SidebarWidth, t.Metrics.ComposerHeight, t),
		// The sidebar is focused at startup, matching the web client, where the
		// conversation list is the entry point.
		focus:    keybindings.PanelSidebar,
		selected: make(map[models.MessageID]bool),
		loc:      time.Local,
		conn:     statusbar.Disconnected,
		atBottom: true,
		chatSel:  -1,
		msgSel:   -1,
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	// The queue is primed here rather than returned directly, so that the
	// commands run through the same path as every later one.
	//
	// Loading the chat list comes first: openChat needs the list in hand to know
	// which chat is selected, and a command issued before the list arrives would
	// have nothing to open.
	m.queue(
		m.listChats(),
		m.scheduleExpiry(time.Now().Add(noticeTTL)),
	)

	// Returning the loader's own command chains the transcript fetch onto it, so
	// the first conversation is open by the time the first frame is drawn. That
	// matches WhatsApp Web, where the last conversation is always on screen, and
	// it means a user who only ever reads never has to make a selection.
	return m.runPending()
}

// noticeTTL is how long a transient message stays on screen.
const noticeTTL = overlay.DefaultToastTTL

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		next, cmd := m.handleResize(msg)
		return next, m.reply(cmd)

	case tea.KeyPressMsg:
		next, cmd := m.handleKey(msg)
		return next, m.reply(cmd)

	case tea.MouseClickMsg:
		next, cmd := m.handleClick(msg)
		return next, m.reply(cmd)

	case tea.MouseWheelMsg:
		next, cmd := m.handleWheel(msg)
		return next, m.reply(cmd)

	case chatsLoadedMsg:
		return m, m.reply(m.setChats(msg.chats))

	case messagesLoadedMsg:
		m.setMessages(string(msg.chat), msg.messages)
		return m, m.reply(nil)

	case eventsMsg:
		return m, m.reply(tea.Batch(m.setEvents(msg.events), m.drainNotices()))

	case statusUpdatedMsg:
		m.conn = msg.conn
		return m, m.reply(nil)

	case errMsg:
		return m, m.reply(m.toast(msg.err.Error(), true))

	case toastExpiredMsg:
		m.expireToasts(time.Now())
		return m, m.reply(m.scheduleExpiry(time.Now().Add(noticeTTL)))

	case toastRaisedMsg:
		return m, m.reply(m.scheduleExpiry(time.Now().Add(noticeTTL)))

	case tea.QuitMsg:
		m.quitting = true
		return m, tea.Quit
	}

	return m, m.reply(nil)
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	// A terminal too small to lay out gets an explanation rather than a
	// scrambled render.
	if !m.theme.Fits(m.width, m.height) {
		return m.viewTooSmall()
	}

	screen := m.compose()

	v := tea.NewView(screen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "wterm"

	// The caret is only meaningful in the composer, and only while it has focus.
	if m.focus == keybindings.PanelComposer && m.overlays.Empty() {
		if x, y, ok := m.composer.Cursor(); ok {
			c := tea.NewCursor(m.composerX()+x, m.composerY()+y)
			c.Shape = tea.CursorBlock
			c.Blink = true
			v.Cursor = c
		}
	}

	return v
}

// compose assembles the full screen.
//
// Width and height are enforced here rather than trusted from the components:
// lipgloss's horizontal join pads the shorter block to the taller one's width,
// and neither the sidebar nor the transcript knows the terminal's dimensions.
// Without this the frame is exactly as wide as the tallest line of the composer
// or the sidebar's border, which is how a TUI ends up overflowing.
func (m *Model) compose() string {
	t := m.theme

	showSidebar := t.SidebarVisible(m.width)
	contentW := t.ContentWidth(m.width)
	contentH := t.ContentHeight(m.height)

	var body string
	if showSidebar {
		sidebar := m.viewSidebar()
		bodyH := max(m.height-t.Metrics.StatusHeight, 1)
		divider := t.Styles.Divider.Render(strings.Repeat("│", bodyH))

		left := fitBlock(sidebar, t.Metrics.SidebarWidth, bodyH)
		conversation := m.viewConversation(contentW, contentH)
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			left, divider, fitBlock(conversation, contentW, bodyH))
	} else {
		bodyH := max(m.height-t.Metrics.StatusHeight, 1)
		body = fitBlock(m.viewConversation(contentW, contentH), contentW, bodyH)
	}

	status := fitBlock(m.viewStatusBar(), m.width, m.theme.Metrics.StatusHeight)
	screen := lipgloss.JoinVertical(lipgloss.Top, body, status)

	if !m.overlays.Empty() {
		screen = m.overlays.View(m.width, m.height, t)
	}

	return clampBlock(screen, m.width, m.height)
}

// fitBlock forces a rendered block to exactly the given size.
//
// Every line is padded or truncated to the width and the line count is adjusted,
// which is what keeps the join below from producing a frame of the wrong size.
func fitBlock(s string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	lines := strings.Split(s, "\n")

	// Pad or trim the line count.
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	if len(lines) > height {
		lines = lines[:height]
	}

	for i, l := range lines {
		switch w := text.VisibleWidth(l); {
		case w < width:
			lines[i] = l + strings.Repeat(" ", width-w)
		case w > width:
			lines[i] = text.TruncateStyled(l, width)
		}
	}

	return strings.Join(lines, "\n")
}

// clampBlock enforces the terminal size on a fully composed frame.
//
// This is the last line of defence. A component that miscounts a border or a
// padding cell would otherwise paint outside the screen and corrupt the scrollback
// the user sees after quitting.
func clampBlock(s string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	return fitBlock(s, width, height)
}

// viewSidebar renders the chat list column.
func (m *Model) viewSidebar() string {
	t := m.theme

	// Height excludes the header, search box, divider and status bar.
	h := t.Metrics.MinHeight
	if m.height > 0 {
		h = m.height - t.Metrics.StatusHeight - 3
	}

	return chatlist.View(chatlist.State{
		Chats:        m.chats(),
		Selected:     m.chatSel,
		Focused:      m.focus == keybindings.PanelSidebar,
		Width:        t.Metrics.SidebarWidth,
		Height:       h,
		Offset:       m.chatOffset,
		SearchActive: m.searchActive,
		SearchQuery:  m.searchQuery,
		Theme:        t,
	})
}

// viewConversation renders the header, transcript and composer.
func (m *Model) viewConversation(w, h int) string {
	t := m.theme

	header := m.viewHeader(w)
	msgs := m.viewMessages(w, max(h-t.Metrics.ComposerHeight, 1))
	comp := composer.View(composer.State{
		Placeholder: composer.PlaceholderFor(m.editing != "", m.replyName()),
		ReplyTo:     m.replyName(),
		EditTo:      string(m.editing),
		Focused:     m.focus == keybindings.PanelComposer,
		Width:       w,
		Height:      t.Metrics.ComposerHeight,
		ShowHint:    true,
		Editing:     m.editing != "",
		Theme:       t,
	}, m.composer.Value())

	return lipgloss.JoinVertical(lipgloss.Top, header, msgs, comp)
}

// viewHeader renders the conversation header.
func (m *Model) viewHeader(w int) string {
	st := m.theme.Styles

	chat, ok := m.currentChat()
	if !ok {
		return st.Header.Width(w).Render(text.PadRight("", w))
	}

	left := st.HeaderTitle.Render(text.Truncate(chat.FallbackName(), w/2))
	right := st.HeaderSubtitle.Render(m.headerRight(chat))

	gap := w - text.VisibleWidth(left) - text.VisibleWidth(right)
	if gap < 1 {
		return st.Header.Width(w).Render(text.Truncate(chat.FallbackName(), w))
	}

	return st.Header.Width(w).Render(
		left + strings.Repeat(" ", gap) + right)
}

// headerRight renders the presence indicator, which is the WhatsApp detail that
// most distinguishes its header from a generic terminal client.
func (m *Model) headerRight(chat models.Chat) string {
	st := m.theme.Styles
	g := m.theme.Glyphs

	if chat.Typing {
		return st.Accent.Render(g.Typing + " typing…")
	}
	if chat.Type.IsGroup() {
		return st.HeaderSubtitle.Render("group")
	}
	if chat.Contact == nil {
		return ""
	}

	switch chat.Contact.Presence.State {
	case models.PresenceAvailable:
		return st.PresenceOnline.Render(g.Online + " online")
	case models.PresenceUnavailable:
		return st.PresenceOffline.Render(g.Offline + " offline")
	default:
		// Unknown presence renders nothing rather than guessing, because telling
		// a user someone is offline when the information was merely withheld is
		// worse than saying nothing.
		return ""
	}
}

// viewMessages renders the transcript.
func (m *Model) viewMessages(w, h int) string {
	return messagelist.View(messagelist.State{
		Rows:     m.rows,
		Offset:   m.msgOffset,
		Width:    w,
		Height:   h,
		AtBottom: m.atBottom,
		Theme:    m.theme,
	})
}

// viewStatusBar renders the bottom line.
func (m *Model) viewStatusBar() string {
	t := m.theme

	unread := 0
	for _, c := range m.chats() {
		unread += c.UnreadCount
	}

	name := ""
	if chat, ok := m.currentChat(); ok {
		name = chat.FallbackName()
	}

	// Report typing for the open chat only: a notification about a background
	// conversation belongs in the sidebar badge, not in the status bar of the
	// one being read.
	typing := ""
	if current, ok := m.currentChat(); ok && current.Typing {
		typing = current.FallbackName()
	}

	return statusbar.View(statusbar.State{
		Connection:  m.conn,
		AccountName: m.deps.Self().Phone,
		Focus:       m.focus,
		ChatName:    name,
		ChatCount:   len(m.chats()),
		UnreadTotal: unread,
		TypingIn:    typing,
		Width:       m.width,
		Help:        m.keys.Help(m.focus),
		Theme:       t,
	})
}

// viewTooSmall explains why nothing is being drawn.
func (m *Model) viewTooSmall() tea.View {
	st := m.theme.Styles
	msg := "terminal too small — need at least " +
		text.Itoa(m.theme.Metrics.MinWidth) + "x" +
		text.Itoa(m.theme.Metrics.MinHeight)
	v := tea.NewView(st.EmptyState.Render(centre(msg, m.width)))
	v.AltScreen = true
	return v
}

// composerX returns the composer's left edge in terminal coordinates.
//
// Only the narrow layout places the composer elsewhere; in the wide layout it
// follows the sidebar, so the offset is constant.
func (m *Model) composerX() int {
	if !m.theme.SidebarVisible(m.width) {
		return 0
	}
	return m.theme.Metrics.SidebarWidth + 1
}

// composerY returns the composer's top edge in terminal coordinates.
func (m *Model) composerY() int {
	return max(m.height-m.theme.Metrics.ComposerHeight-m.theme.Metrics.StatusHeight, 0)
}

// replyName returns the display name of the message being replied to.
func (m *Model) replyName() string {
	if m.replyTo == nil {
		return ""
	}
	if n := strings.TrimSpace(m.replyTo.SenderName); n != "" {
		return n
	}
	return string(m.replyTo.ID)
}
