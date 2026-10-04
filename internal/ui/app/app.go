// Package app is cli-zapp's root Bubble Tea model.
//
// # The root owns decisions, regions own pixels
//
// Every region implements [component.Region]: its own model, its own Update, its
// own View. The root composes them into a frame and turns the events they emit
// into consequences. It never draws a region itself, and a region never reaches
// outside itself.
//
// That routing is the whole design. A keystroke in the transcript becomes a
// [component.Event]; the root decides that "reply" means "put a quote in the
// composer and move focus there". Neither half knows about the other, so either
// can be replaced — the transcript by a different renderer, the root by a
// different application — without touching the other.
//
// # Input order
//
// One path, and the order is load-bearing:
//
//  1. Overlays consume input, so a dialog cannot act on the conversation behind it.
//  2. Escape is handled next: the user must always be able to abandon a mode.
//  3. Global bindings resolve before any text field, because a text field accepts
//     arbitrary input and would otherwise swallow every modified key.
//  4. The palette takes printable input as its query, while it is open.
//  5. The focused region gets the remainder.
//
// Step 3 before step 4 is the counter-intuitive one, and the reason is worth
// stating because getting it backwards produces a bug that reads as "my shortcuts
// are broken": a textarea consumes arbitrary input, so if it saw the key first it
// would eat ctrl+q, ctrl+r and ctrl+p — and the user spends most of their time
// with the composer focused.
package app

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/models"
	"github.com/cli-zapp/cli-zapp/internal/notifications"
	"github.com/cli-zapp/cli-zapp/internal/text"
	"github.com/cli-zapp/cli-zapp/internal/ui/component"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/composer"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/overlay"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/palette"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/sidebar"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/statusbar"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/transcript"
	"github.com/cli-zapp/cli-zapp/internal/ui/layout"
	"github.com/cli-zapp/cli-zapp/internal/ui/theme"
	"github.com/cli-zapp/cli-zapp/internal/whatsapp"
)

// Model is the root model.
//
// It is a pointer, and it mutates in place. Bubble Tea's own guidance is that
// copying a model on every key press is wasteful, and the regions are values
// precisely so that the expensive part of a frame — re-wrapping a transcript —
// only happens when the width or the messages change.
type Model struct {
	// services are the application's dependencies. They are interfaces so that
	// the UI can be exercised against the in-memory fake.
	services whatsapp.Services

	theme theme.Theme
	keys  keybindings.Map

	sidebar    *sidebar.Model
	transcript *transcript.Model
	composer   *composer.Model
	status     *statusbar.Model
	palette    *palette.Model

	// layout is the geometry of the last frame; mode is its tier, cached so the
	// regions can be told about it.
	layout layout.Layout
	mode   layout.Mode

	width, height int

	// focus is the region with keyboard focus. It is a region name rather than a
	// pointer so that it can be compared and logged without touching the regions.
	focus     component.RegionRef
	prevFocus component.RegionRef

	// Data, owned by the application and handed to the regions each frame.
	chats     []models.Chat
	messages  []models.Message
	selected  map[models.MessageID]bool
	search    string
	searching bool

	// opened is the conversation whose transcript is cached in messages.
	//
	// It is separate from the sidebar's cursor on purpose: the cursor says which row
	// is highlighted, this says which transcript has been fetched. Confusing the two
	// would re-fetch on every cursor move, which on a slow connection feels like the
	// list has frozen.
	opened models.ChatID

	// senderNames maps a contact to a display name, built once from the cached
	// conversations so that quoting a message does not need a service call.
	senderNames map[models.ContactID]string

	// replyTo is the message being quoted, and editing is the one being replaced.
	//
	// They are mutually exclusive by construction: the composer holds a single
	// mode, so the impossible state of quoting one message while editing another
	// cannot be represented. That is the point of the type.
	replyTo *models.MessageRef
	editing models.MessageID

	// overlays is the dialog stack above the interface.
	overlays overlay.Stack

	// connection is the account's link state.
	connection statusbar.Connection

	// commands is the palette's command table, rebuilt whenever the set of
	// available actions changes.
	commands []component.Command

	// notifier raises alerts for background messages; pendingNotices is the queue
	// drained on the UI goroutine so the notifier's exec calls cannot stall the
	// event stream.
	notifier       *notifications.Notifier
	pendingNotices []notifications.Event

	// pending carries commands the model produced for itself — a fetch triggered by
	// a state change, say — as opposed to the command that is the direct reply to
	// the current message.
	//
	// The distinction is the point. A command that is the reply must be *returned*
	// from Update, because Bubble Tea only schedules what Update hands back;
	// queueing it would strand it and the key press would appear to do nothing. A
	// follow-up has no such caller, so queueing it until the next tick is both
	// correct and testable: a test drains the queue with no running program.
	pending []tea.Cmd

	// DisableTimers suppresses sleeping commands. It is set by tests, which would
	// otherwise block for the duration of a toast's lifetime.
	DisableTimers bool

	// quitting ends the program. Set before the last frame so Update can keep
	// processing messages without re-rendering a dead screen.
	quitting bool

	// loc is the location used for day separators. It is captured once at startup
	// so that every frame agrees which calendar day a message belongs to, even
	// across a midnight boundary.
	loc *time.Location
}

// New creates the root model.
func New(services whatsapp.Services, t theme.Theme, keys keybindings.Map) *Model {
	m := &Model{
		services:    services,
		theme:       t,
		keys:        keys,
		sidebar:     sidebar.New(keys, t),
		transcript:  transcript.New(keys, t),
		composer:    composer.New(keys, t),
		status:      statusbar.New(t),
		palette:     palette.New(t),
		focus:       component.RegionSidebar,
		selected:    make(map[models.MessageID]bool),
		senderNames: make(map[models.ContactID]string),
		loc:         time.Local,
	}

	// The sidebar starts focused but is not told so through Focus: the regions are
	// handed their rectangles below, and the focus flag is set directly. Calling
	// Focus here would run before any layout exists and leave the region thinking
	// it had a zero-sized rectangle.
	m.sidebar.Focus()

	// The palette is built once here and rebuilt whenever the state its availability
	// depends on changes, so a command that has become possible appears without the
	// user having to reopen the application.
	m.rebuildCommands()
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	m.queue(m.syncNow())
	return m.reply(nil)
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.applyLayout()
		m.syncRegions()
		return m, m.reply(nil)

	case tea.KeyPressMsg:
		return m, m.reply(m.onKey(msg))

	case tea.MouseClickMsg:
		return m, m.reply(m.onClick(msg))

	case tea.MouseWheelMsg:
		return m, m.reply(m.onWheel(msg))

	case component.Event:
		cmd := m.onEvent(msg)
		m.rebuildCommands()
		return m, m.reply(cmd)

	case chatsReady:
		m.chats = msg.chats
		m.indexSenders()
		m.syncRegions()
		m.rebuildCommands()
		return m, m.reply(nil)

	case transcriptReady:
		// Tagged with the chat it belongs to: a slow response for a conversation
		// the user has already left must not overwrite the one they are reading.
		if msg.forChat != m.currentChatID() {
			return m, m.reply(nil)
		}
		m.messages = msg.messages
		m.syncTranscript()
		m.rebuildCommands()
		return m, m.reply(nil)

	case statusReady:
		m.connection = msg.conn
		return m, m.reply(nil)

	case failure:
		return m, m.reply(m.toast(msg.err.Error(), true))

	case eventsReady:
		return m, m.reply(m.setEvents(msg.events))

	case noticesReady:
		return m, m.reply(m.notifyAll(msg.events))

	case toastPushed:
		return m, m.reply(m.toast(msg.msg, false))

	case toastExpiredMsg:
		m.expireToasts(msg.at)
		return m, m.scheduleExpiry()

	case tea.QuitMsg:
		m.quitting = true
		return m, tea.Quit
	}

	return m, m.reply(nil)
}

// View implements tea.View.
func (m *Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	if m.mode == layout.ModeTooSmall {
		return m.viewTooSmall()
	}

	screen := m.compose()

	v := tea.NewView(screen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = m.theme.Brand

	// The caret is placed here rather than by the composer's widget because a
	// widget draws a virtual caret in its own coordinates, which cannot be
	// reconciled with the rest of the frame.
	if m.focus == component.RegionComposer && m.overlays.Empty() && !m.palette.IsOpen() {
		if x, y, ok := m.composer.Cursor(); ok {
			c := tea.NewCursor(m.composer.PromptX()+x, m.layout.Composer.Y+1+y)
			c.Shape = tea.CursorBlock
			c.Blink = true
			v.Cursor = c
		}
	}

	return v
}

// compose assembles the frame.
//
// Width and height are enforced here rather than trusted from the regions: lipgloss
// pads a short block out to the tallest one's width, and neither the sidebar nor the
// transcript knows the terminal's dimensions. Without this the frame is exactly as
// wide as the widest line any region produced, which is how a TUI overflows its
// terminal and starts wrapping.
func (m *Model) compose() string {
	l := m.layout
	base := m.composeBase(l)

	// The palette is drawn first and the overlays over it, because the palette is the
	// outermost layer: it is what the user opened most recently, and a dialog sitting
	// above it would be unreachable.
	if p := m.palette.View(); p != "" {
		base = mergeFrames(base, p)
	}
	if o := m.overlays.View(l.Width, l.Height, m.theme); o != "" {
		base = mergeFrames(base, o)
	}

	return m.paintBlock(base, l.Screen)
}

// mergeFrames composites a full-screen layer over a full-screen base.
//
// Both frames are the terminal's size and both are blank where they have nothing to
// draw, so the merge rule is one line: a non-blank cell in the layer wins, a blank one
// shows the base through. Treating the layer as a canvas rather than as a replacement
// is what lets a dialog cover the conversation without repainting the status bar it
// happens to overlap.
func mergeFrames(base, layer string) string {
	baseRows := strings.Split(base, "\n")
	layerRows := strings.Split(layer, "\n")

	out := make([]string, maxInt(len(baseRows), len(layerRows)))
	for i := range out {
		var b, l string
		if i < len(baseRows) {
			b = baseRows[i]
		}
		if i < len(layerRows) {
			l = layerRows[i]
		}
		out[i] = mergeRow(b, l)
	}
	return strings.Join(out, "\n")
}

// mergeRow composites one line of a layer over one line of a base.
func mergeRow(base, layer string) string {
	if strings.TrimSpace(text.StripANSI(layer)) == "" {
		// A blank layer row shows the base through. Returning the base unchanged rather
		// than repainting it keeps the base's styling, which matters because the base
		// is where all the colour lives.
		return base
	}
	return text.OverlayCells(layer, base)
}

// composeBase paints the three zones with no layer above them.
func (m *Model) composeBase(l layout.Layout) string {
	// The status bar spans the full width underneath both columns, so it is joined
	// vertically rather than being part of either one.
	if l.Mode == layout.ModeMinimal {
		// Minimal mode drops the sidebar, so the conversation takes the whole width.
		// The contact header goes with it: at this size the name would cost a row the
		// transcript needs, and the conversation is still identifiable from what is
		// said in it.
		return lipgloss.JoinVertical(lipgloss.Top,
			m.paintStack([]string{
				m.transcript.View(),
				m.composer.View(),
			}, []layout.Rect{l.Transcript, l.Composer}),
			m.viewStatusbar(),
		)
	}

	// The divider is one column wide and one character per row. Building it as a
	// single string of repeated glyphs and joining it horizontally would instead
	// produce one very wide row, which shifts every column to its right.
	divider := m.theme.Styles.Divider.Render(
		strings.Join(repeatLines(m.theme.Glyphs.VBar, l.Conversation.Height), "\n"))

	// The conversation's rows are stacked first so the block handed to the horizontal
	// join is already the right height; joining a short transcript against a tall
	// composer would leave a gap that lipgloss fills with the wider block's padding.
	conversation := m.paintStack([]string{
		m.viewHeader(),
		m.viewHeaderRule(),
		m.transcript.View(),
		m.composer.View(),
	}, []layout.Rect{l.Header, l.HeaderRule, l.Transcript, l.Composer})

	return lipgloss.JoinVertical(lipgloss.Top,
		lipgloss.JoinHorizontal(lipgloss.Top,
			m.paintBlock(m.sidebar.View(), l.Sidebar),
			divider,
			m.paintBlock(conversation, l.Conversation),
		),
		m.viewStatusbar(),
	)
}

// paintStack joins blocks vertically, forcing each one into its own rectangle.
//
// The rectangles must be contiguous and cover the parent's height; a gap would
// leave stale cells on screen and an overlap would hide the last row of the
// transcript.
func (m *Model) paintStack(blocks []string, rects []layout.Rect) string {
	painted := make([]string, 0, len(blocks))
	for i, b := range blocks {
		r := layout.Rect{}
		if i < len(rects) {
			r = rects[i]
		}
		painted = append(painted, m.paintBlock(b, r))
	}
	return lipgloss.JoinVertical(lipgloss.Top, painted...)
}

// paintBlock forces a block to occupy exactly its rectangle.
//
// Every line is padded or truncated to the width and the line count is adjusted.
// This is the single place where geometry is reconciled with what a region
// produced, which is what keeps the three zones from disagreeing about where their
// edges are.
func (m *Model) paintBlock(s string, r layout.Rect) string {
	if r.Empty() {
		return ""
	}

	lines := strings.Split(s, "\n")
	for len(lines) < r.Height {
		lines = append(lines, strings.Repeat(" ", r.Width))
	}
	if len(lines) > r.Height {
		lines = lines[:r.Height]
	}
	for i, l := range lines {
		switch w := text.VisibleWidth(l); {
		case w < r.Width:
			lines[i] = l + strings.Repeat(" ", r.Width-w)
		case w > r.Width:
			lines[i] = text.TruncateStyled(l, r.Width)
		}
	}
	return strings.Join(lines, "\n")
}

// viewHeader renders the contact name and presence.
//
// This is the detail that most distinguishes the interface from a generic terminal
// client: the top of the conversation says who it is with and whether they are
// there, the way WhatsApp Web does.
func (m *Model) viewHeader() string {
	r := m.layout.Header
	if r.Empty() {
		return ""
	}

	chat, ok := m.currentChat()
	if !ok {
		return strings.Repeat(" ", r.Width)
	}

	st := m.theme.Styles
	name := st.HeaderTitle.Render(text.Truncate(chat.FallbackName(), maxInt(r.Width/2, 1)))
	right := m.headerRight(chat)

	gap := r.Width - text.VisibleWidth(name) - text.VisibleWidth(right)
	if gap < 1 {
		// Too narrow for both. The name is what identifies the conversation, so
		// presence gives way rather than the name.
		return text.PadRight(
			st.HeaderTitle.Render(text.Truncate(chat.FallbackName(), r.Width)), r.Width)
	}

	return text.PadRight(name+strings.Repeat(" ", gap)+right, r.Width)
}

// viewHeaderRule draws the separator between the header and the transcript.
//
// It belongs to the root rather than to a region because it spans the conversation
// column as a whole: the header's identity and the transcript's content are drawn by
// different code and only the root knows both, which makes the root the only place the
// boundary between them can live.
func (m *Model) viewHeaderRule() string {
	r := m.layout.HeaderRule
	if r.Empty() {
		return ""
	}
	return m.theme.Styles.Divider.Render(
		strings.Repeat(m.theme.Glyphs.Divider, maxInt(r.Width, 0)))
}

// headerRight renders the presence or typing notice.
func (m *Model) headerRight(chat models.Chat) string {
	st := m.theme.Styles
	g := m.theme.Glyphs

	switch {
	case chat.Typing:
		return st.Accent.Render(g.Typing + " escribiendo…")
	case chat.Type.IsGroup():
		return st.HeaderSubtitle.Render("grupo")
	case chat.Contact == nil:
		return ""
	}

	switch chat.Contact.Presence.State {
	case models.PresenceAvailable:
		return st.PresenceOnline.Render(g.Online + " en línea")
	case models.PresenceUnavailable:
		return st.PresenceOffline.Render(g.Offline + " desconectado")
	default:
		// Unknown presence renders nothing rather than guessing. Telling a user
		// someone is offline when the information was merely withheld — WhatsApp
		// hides presence by default for some accounts — is worse than saying
		// nothing, because the user will act on it.
		return ""
	}
}

// viewStatusbar fills the bar's state and renders it.
func (m *Model) viewStatusbar() string {
	m.status.SetState(statusbar.State{
		Connection: m.connection,
		ChatName:   m.currentChatName(),
		Unread:     m.unreadTotal(),
		Typing:     m.typingIn(),
		Hints:      m.keys.Hints(m.focusPanel()),
	})
	return m.status.View()
}

// viewTooSmall explains why nothing is being drawn.
//
// Explaining is better than drawing: at this size the interface would be a
// scattering of truncated fragments, and a sentence saying what is needed is more
// use than a broken grid.
func (m *Model) viewTooSmall() tea.View {
	msg := "terminal demasiado pequeña — se necesita " +
		text.Itoa(layout.MinWidth) + "x" + text.Itoa(layout.MinHeight)

	v := tea.NewView(text.PadRight(centre(msg, m.width), m.width))
	v.AltScreen = true
	return v
}

// Shutdown releases resources the model holds.
//
// Bubble Tea's Run has returned by the time this is called, so the terminal has
// already been restored. That makes it the right place to stop background work: a
// goroutine still writing to a now-dead terminal corrupts the shell's prompt.
func (m *Model) Shutdown() error {
	if m.services.Sync == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return m.services.Sync.Stop(ctx)
}

// SetNotifier attaches the notifier used for background messages.
//
// It is a setter rather than a constructor parameter because the notifier depends on
// the environment — whether notify-send exists — which is resolved in main after the
// model would otherwise already exist.
func (m *Model) SetNotifier(n *notifications.Notifier) { m.notifier = n }
