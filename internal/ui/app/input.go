package app

import (
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/models"
	"github.com/cli-zapp/cli-zapp/internal/ui/component"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/overlay"
	"github.com/cli-zapp/cli-zapp/internal/ui/layout"
)

// --- layout ---

// applyLayout recomputes the geometry and tells every region about it.
//
// The regions are handed rectangles rather than being asked to compute their own: two
// regions independently guessing at widths is how a sidebar and a transcript end up
// disagreeing about where the divider is, and the disagreement only shows up as a
// visibly crooked frame.
func (m *Model) applyLayout() {
	m.layout = layout.Compute(m.width, m.height)
	m.mode = m.layout.Mode

	// The mode is applied before the rectangles, because it decides whether a region
	// draws a brand banner, a search field or key hints — all of which change the
	// space the region has left for its own content.
	m.sidebar.SetMode(m.mode)
	m.transcript.SetMode(m.mode)
	m.composer.SetMode(m.mode)
	m.status.SetMode(m.mode)

	m.sidebar.Resize(m.layout.Sidebar)
	m.transcript.Resize(m.layout.Transcript)
	m.composer.Resize(m.layout.Composer)
	m.status.Resize(m.layout.Status)

	// The palette floats over the whole screen rather than inside a zone.
	m.palette.Resize(m.layout.Screen)
}

// --- keyboard ---

// onKey routes a key press.
//
// The five steps below are the whole input architecture and each one is
// load-bearing. The order is documented in the package comment; what follows is why
// each step is where it is.
func (m *Model) onKey(msg tea.KeyPressMsg) tea.Cmd {
	// 1. Overlays consume input. A confirmation dialog that let a key through would
	//    act on the conversation behind it — a dialog asking "delete this message?"
	//    that then deleted a different one.
	if top, ok := m.overlays.Top(); ok {
		return m.onOverlayKey(top, msg)
	}

	// 2. Escape always works. The user must be able to abandon a mode they are stuck
	//    in, and an escape that is itself bound to something else is a trap.
	if component.IsNamed(msg, "esc") {
		return m.onEscape()
	}

	// 3. Global bindings, before any text field. If the composer went first it would
	//    swallow every modified key, because a text field accepts arbitrary input and
	//    reports that it consumed the press.
	if a := m.keys.ResolveGlobal(component.ConvertKey(msg)); a != keybindings.ActionNone {
		return m.run(a)
	}

	// 4. The palette, while open, takes printable input as its query. It must come
	//    after the globals so that ctrl+q still quits and esc still closes it.
	if m.palette.IsOpen() {
		if component.IsPrintable(msg) {
			return m.palette.Open(m.palette.Query() + msg.Text)
		}
		return nil
	}

	// 5. The focused region gets what is left.
	region := m.focusedRegion()
	if _, cmd := region.Update(msg); cmd != nil {
		return cmd
	}
	return nil
}

// onEscape implements cancel, which is contextual rather than uniform.
//
// "Cancel" means something different depending on what is in progress, and doing the
// same thing every time would leave the user stuck in a mode they cannot leave. The
// order is outermost-first, so the most recent thing they started is the first thing
// that gets undone.
func (m *Model) onEscape() tea.Cmd {
	switch {
	case m.palette.IsOpen():
		m.palette.Close()
		return nil

	case m.searching:
		return m.dismissSearch()

	case m.composer.Mode() != composerModeSend:
		// Escape abandons the mode, not the draft. Throwing away what someone typed
		// because they pressed the wrong key is the more annoying of the two.
		m.composer.ClearMode()
		m.replyTo, m.editing = nil, ""
		return nil

	case len(m.selected) > 0:
		clear(m.selected)
		m.syncTranscript()
		return nil

	case m.focus != component.RegionSidebar:
		// Nothing to cancel, so return to the list: a predictable way back for
		// someone using only the keyboard, and the same key as i3 uses to leave a
		// nested container.
		return m.setFocus(component.RegionSidebar)
	}

	return nil
}

// --- mouse ---

// onClick maps a click to a zone and a region.
func (m *Model) onClick(msg tea.MouseClickMsg) tea.Cmd {
	if m.mode == layout.ModeTooSmall || m.palette.IsOpen() {
		return nil
	}

	// A click dismisses a toast, because a notification that cannot be dismissed by
	// clicking it is a decoration the user has to wait out.
	if top, ok := m.overlays.Top(); ok {
		if top.Kind == overlay.KindToast {
			m.overlays.Pop()
			return nil
		}
		// Any other click inside a modal is swallowed. A dialog that passed clicks
		// through would be a trapdoor: the user clicks away from the prompt and
		// destroys the conversation behind it.
		return nil
	}

	switch m.layout.ZoneAt(msg.X, msg.Y) {
	case layout.ZoneSidebar:
		return m.sidebar.ClickAt(msg.X, msg.Y)

	case layout.ZoneTranscript:
		// A right-click opens the context menu for the message under the pointer,
		// which is what every other messenger does and what the user's hand expects.
		if msg.Button == tea.MouseRight {
			cmd := m.transcript.ClickAt(msg.X, msg.Y)
			return tea.Batch(cmd, m.openMessageMenu())
		}
		return tea.Batch(
			m.transcript.ClickAt(msg.X, msg.Y),
			m.setFocus(component.RegionTranscript),
		)

	case layout.ZoneComposer:
		return m.setFocus(component.RegionComposer)

	default:
		// The status bar and the chrome: decorative, so a click there does nothing
		// rather than focusing something the user did not point at.
		return nil
	}
}

// onWheel scrolls the pane under the pointer.
//
// Routing by pointer rather than by focus is what makes the wheel do the obvious thing
// without first moving focus, which is how every other scrollable interface behaves.
func (m *Model) onWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if m.mode == layout.ModeTooSmall || m.palette.IsOpen() || !m.overlays.Empty() {
		return nil
	}

	// Three lines per notch is the conventional feel for terminal scrollback: one
	// feels stuck, ten overshoots everything worth reading.
	const linesPerNotch = 3

	var delta int
	switch msg.Button {
	case tea.MouseWheelDown:
		delta = linesPerNotch
	case tea.MouseWheelUp:
		delta = -linesPerNotch
	default:
		return nil
	}

	switch m.layout.ZoneAt(msg.X, msg.Y) {
	case layout.ZoneSidebar:
		m.sidebar.Scroll(delta)
		return nil

	case layout.ZoneTranscript, layout.ZoneComposer:
		m.transcript.Scroll(delta)
		// Scrolling implies intent to read, so focus follows the wheel — otherwise the
		// next keystroke would act on a pane the user has visibly moved away from.
		if m.focus != component.RegionTranscript {
			return m.setFocus(component.RegionTranscript)
		}

	default:
		// The status bar and the chrome do not scroll.
		return nil
	}

	return nil
}

// --- focus ---

// setFocus moves keyboard focus.
func (m *Model) setFocus(r component.RegionRef) tea.Cmd {
	if r == m.focus {
		return nil
	}
	m.prevFocus = m.focus
	m.focus = r

	switch r {
	case component.RegionTranscript:
		m.transcript.Focus()
		m.composer.Blur()
		m.sidebar.Blur()
	case component.RegionComposer:
		return m.composer.Focus()
	case component.RegionSidebar:
		m.sidebar.Focus()
		m.transcript.Blur()
		m.composer.Blur()
	}

	return nil
}

// focusedRegion returns the region that currently owns the keyboard.
func (m *Model) focusedRegion() component.Region {
	switch m.focus {
	case component.RegionTranscript:
		return m.transcript
	case component.RegionComposer:
		return m.composer
	default:
		return m.sidebar
	}
}

// focusPanel maps the focused region to the panel the binding map is scoped by.
//
// The mapping is many-to-one on purpose: the sidebar and the transcript are separate
// regions but share a binding panel, because "which region is this" is an
// implementation detail a configuration file has no business knowing.
func (m *Model) focusPanel() keybindings.Panel {
	switch m.focus {
	case component.RegionTranscript:
		return keybindings.PanelMessages
	case component.RegionComposer:
		return keybindings.PanelComposer
	default:
		return keybindings.PanelSidebar
	}
}

// nextRegion cycles focus forward, wrapping around.
func nextRegion(r component.RegionRef) component.RegionRef {
	switch r {
	case component.RegionSidebar:
		return component.RegionTranscript
	case component.RegionTranscript:
		return component.RegionComposer
	default:
		return component.RegionSidebar
	}
}

// prevRegion cycles focus backward, wrapping around.
func prevRegion(r component.RegionRef) component.RegionRef {
	switch r {
	case component.RegionSidebar:
		return component.RegionComposer
	case component.RegionTranscript:
		return component.RegionSidebar
	default:
		return component.RegionTranscript
	}
}

// --- dispatch ---

// run executes an application action from the binding table or the palette.
//
// It is the single place where a key press becomes a consequence, which is what makes
// the set of things a keystroke can do auditable by reading one switch.
func (m *Model) run(a keybindings.Action) tea.Cmd {
	chat, hasChat := m.currentChat()

	switch a {
	// Application.
	case keybindings.ActionQuit:
		return tea.Quit
	case keybindings.ActionSearch:
		return m.beginSearch("")
	case keybindings.ActionPalette:
		return m.palette.Open("")
	case keybindings.ActionSync:
		return m.syncNow()
	case keybindings.ActionNewChat:
		return m.openNewChat()
	case keybindings.ActionInfo:
		return m.openInfo()
	case keybindings.ActionHelp:
		return m.openHelp()
	case keybindings.ActionCancel:
		return m.onEscape()
	case keybindings.ActionConfirm:
		return m.confirm()

	// Focus.
	case keybindings.PanelNext:
		return m.setFocus(nextRegion(m.focus))
	case keybindings.PanelPrev:
		return m.setFocus(prevRegion(m.focus))
	case keybindings.NavLeft:
		return m.setFocus(component.RegionSidebar)
	case keybindings.NavRight:
		return m.setFocus(component.RegionComposer)
	case keybindings.ChatOpen:
		return m.focusTranscript()

	// Conversation flags. Each needs a conversation; without one the key does
	// nothing, which is better than an error the user cannot act on.
	case keybindings.ActionToggleRead:
		if !hasChat {
			return nil
		}
		return m.setRead(chat.ID, chat.HasUnread)
	case keybindings.ActionTogglePin:
		if !hasChat {
			return nil
		}
		return m.chatFlag(chat.ID,
			func(c models.Chat) bool { return c.Pinned }, setPinned)
	case keybindings.ActionToggleMute:
		if !hasChat {
			return nil
		}
		return m.chatFlag(chat.ID,
			func(c models.Chat) bool { return c.Muted }, setMuted)
	case keybindings.ActionToggleArchive:
		if !hasChat {
			return nil
		}
		return m.chatFlag(chat.ID,
			func(c models.Chat) bool { return c.Archived }, setArchived)
	case keybindings.ActionDeleteChat:
		return m.confirmDeleteChat()

	// Message actions. These are dispatched here rather than in the transcript
	// because they change application state — focus, the composer, the service — and
	// the transcript has no business doing any of that.
	case keybindings.ActionReply:
		return m.beginReply()
	case keybindings.ActionEdit:
		return m.beginEdit()
	case keybindings.ActionDelete:
		return m.confirmDeleteMessage()
	case keybindings.ActionReact:
		return m.openReactionPicker()
	case keybindings.ActionForward:
		return m.openMessageMenu()
	case keybindings.ActionCopy:
		return m.copySelected(m.selectionOrCursor())

	// The composer's own submit.
	case keybindings.SendMessage:
		return m.submitDraft()

	default:
		// Anything the binding table names but this model does not handle is a no-op
		// rather than a panic.
		//
		// A configuration file may legitimately bind an action that a later phase
		// adds, and a client that panics on an unrecognised action cannot be configured
		// ahead of itself. The same is true of an action that belongs to a panel other
		// than the focused one: the region that owns it handles it there, and it is only
		// the application's own bindings that reach this switch.
		return nil
	}
}

// confirm performs the primary action for the focused region.
//
// mod+enter means "do the main thing here", which is what makes the modifier hierarchy
// worth having: the same key is useful in every region and does the locally obvious
// thing.
func (m *Model) confirm() tea.Cmd {
	switch m.focus {
	case component.RegionSidebar:
		return m.focusTranscript()
	case component.RegionTranscript:
		return m.beginReply()
	default:
		return m.submitDraft()
	}
}

// focusTranscript moves focus to the transcript, opening the conversation if needed.
func (m *Model) focusTranscript() tea.Cmd {
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}

	// The transcript is only fetched once per conversation: pressing mod+enter twice
	// must not re-fetch, or a slow connection would make the second press feel broken.
	if m.opened != chat.ID {
		m.opened = chat.ID
		m.queue(m.reload(chat.ID))
	}
	return m.setFocus(component.RegionTranscript)
}

// submitDraft sends the composer's contents.
func (m *Model) submitDraft() tea.Cmd {
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}

	body := m.composer.Value()
	if body == "" {
		return nil
	}

	// The mode is read before the composer is cleared, because clearing resets it.
	mode := m.composer.Mode()

	// The command is built before the fields are cleared, because the command reads
	// them. Clearing first and building afterwards sends an edit against an empty
	// message identifier, which the service rejects — and the symptom is a toast
	// saying "not found" for a message the user can plainly see.
	cmd := m.send(chat.ID, body, mode)

	m.composer.Clear()
	m.replyTo, m.editing = nil, ""

	return cmd
}

// selectionOrCursor returns the selection, falling back to the cursor message.
//
// The fallback is what makes "copy" work without a selection step: a user who has
// navigated to a message and pressed the key plainly meant that message.
func (m *Model) selectionOrCursor() []models.MessageID {
	if len(m.selected) > 0 {
		ids := make([]models.MessageID, 0, len(m.selected))
		for id := range m.selected {
			ids = append(ids, id)
		}
		// Map iteration order is random, so the order has to be imposed or the copied
		// text would come out shuffled between two runs of the same keystroke.
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		return ids
	}
	if id := m.transcript.CursorMessageID(); id != "" {
		return []models.MessageID{id}
	}
	return nil
}

// --- search ---

// beginSearch enters search mode.
//
// In minimal mode there is no search field, so search runs through the palette
// instead, which filters conversations as well as commands. The feature does not
// disappear with the layout; only the affordance for it does.
func (m *Model) beginSearch(seed string) tea.Cmd {
	if m.mode != layout.ModeFull {
		return m.palette.Open(seed)
	}
	m.searching = true
	m.setFocus(component.RegionSidebar)
	m.sidebar.BeginSearch()
	return nil
}

// dismissSearch leaves search mode and clears the query.
func (m *Model) dismissSearch() tea.Cmd {
	if !m.searching {
		return nil
	}
	m.searching = false
	m.search = ""
	m.syncSidebar()
	return m.listChats()
}

// --- toasts ---

// toast pushes a transient notification and schedules its expiry.
func (m *Model) toast(msg string, isError bool) tea.Cmd {
	m.overlays.Push(overlay.Toast(msg, isError))
	return m.scheduleExpiry()
}

// scheduleExpiry returns a command that fires when the next toast should go.
//
// A timer rather than a frame counter: the toast's lifetime is wall-clock, and tying
// it to redraws would make a toast vanish instantly during a sync, which is exactly
// when the user most wants to read it.
func (m *Model) scheduleExpiry() tea.Cmd {
	if m.DisableTimers {
		// A tea.Cmd is a func rather than an interface, so a test cannot type-assert
		// to recognise a sleeping command. The model is asked directly instead of
		// blocking on the timer.
		return nil
	}

	at, ok := m.nextExpiry()
	if !ok {
		return nil
	}

	d := time.Until(at)
	if d <= 0 {
		return func() tea.Msg { return toastExpiredMsg{at: at} }
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return toastExpiredMsg{at: at} })
}

// nextExpiry returns the earliest expiry among the toasts on the stack.
func (m *Model) nextExpiry() (time.Time, bool) {
	var (
		earliest time.Time
		found    bool
	)
	for _, o := range m.overlays.All() {
		if o.Kind != overlay.KindToast {
			continue
		}
		if !found || o.ExpiresAt.Before(earliest) {
			earliest, found = o.ExpiresAt, true
		}
	}
	return earliest, found
}

// expireToasts pops toasts that have outlived their display window.
//
// It walks from the top down and stops at the first non-toast, because the stack is
// ordered: a modal opened after a toast must stay up until it is dismissed
// deliberately, even once the toast behind it has expired.
func (m *Model) expireToasts(now time.Time) {
	for m.overlays.Len() > 0 {
		top, ok := m.overlays.Top()
		if !ok || top.Kind != overlay.KindToast || now.Before(top.ExpiresAt) {
			return
		}
		m.overlays.Pop()
	}
}
