package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/notifications"
	"github.com/wterm/wterm/internal/ui/components/overlay"
	"github.com/wterm/wterm/internal/whatsapp"
)

// setEvents folds a batch of sync events into the model.
//
// Batching matters: a resync delivers thousands of events, and applying them one
// redraw at a time would make the interface unusable for the duration.
func (m *Model) setEvents(events []whatsapp.Event) tea.Cmd {
	if len(events) == 0 {
		return nil
	}

	// Track whether the open chat changed, so the transcript is only re-laid out
	// once at the end rather than per event.
	var (
		openChanged bool
		chatsDirty  bool
		cmds        []tea.Cmd
	)

	for _, e := range events {
		switch e.Kind {
		case whatsapp.EventMessage:
			if e.ChatID == m.currentChatID() {
				openChanged = true
			}
			chatsDirty = true
			m.raiseNotification(e)

		case whatsapp.EventMessageDeleted, whatsapp.EventReaction, whatsapp.EventChatUpdate:
			if e.ChatID == m.currentChatID() {
				openChanged = true
			}
			chatsDirty = true

		case whatsapp.EventPresence, whatsapp.EventTyping:
			// Presence changes only affect the header and the sidebar badge; the
			// transcript itself is untouched.
			chatsDirty = true

		case whatsapp.EventConnection:
			m.conn = displayConn(e.Connection)

		case whatsapp.EventError:
			if e.Err != nil {
				cmds = append(cmds, m.toast(e.Err.Error(), true))
			}

		case whatsapp.EventUnknown:
			// An event with no payload is nothing to act on. Listed explicitly so
			// that adding a kind to the interface is a compile-time decision rather
			// than a silently unhandled branch.
		}
	}

	// A change in the open conversation needs both a re-fetch and a fresh layout:
	// the cached transcript is only updated by the service call.
	if openChanged {
		cmds = append(cmds, m.reloadOpenChat())
	}

	if chatsDirty {
		cmds = append(cmds, m.listChats())
	}
	// Keep the wait loop running: a stream that stops being drained would stall
	// the connection.
	cmds = append(cmds, m.waitForEvent())
	return tea.Batch(cmds...)
}

// raiseNotification alerts the user about an incoming message.
//
// A message in the chat currently on screen does not notify: the user is already
// looking at it, and a notification for it is pure noise.
func (m *Model) raiseNotification(e whatsapp.Event) {
	if e.Message.Direction != models.DirectionIncoming {
		return
	}
	if e.ChatID == m.currentChatID() && m.focus != keybindings.PanelSidebar {
		return
	}

	name := "New message"
	if chat, ok := m.chatByID(e.ChatID); ok {
		name = chat.FallbackName()
	}
	body := e.Message.Text()
	if body == "" {
		body = e.Message.Kind.String()
	}

	m.pendingNotices = append(m.pendingNotices, notifications.Event{
		Title:  name,
		Body:   body,
		ChatID: string(e.ChatID),
	})
}

// chatByID looks up a chat by identifier in the cache.
func (m *Model) chatByID(id models.ChatID) (models.Chat, bool) {
	for _, c := range m.chatCache {
		if c.ID == id {
			return c, true
		}
	}
	return models.Chat{}, false
}

// reloadOpenChat re-fetches the transcript of the chat currently on screen.
func (m *Model) reloadOpenChat() tea.Cmd {
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}
	return m.fetchTranscript(chat)
}

// drainNotices delivers queued notifications.
//
// Notifications are raised on the UI goroutine rather than from the sync reader, so
// that the notifier's exec calls cannot stall the event stream.
func (m *Model) drainNotices() tea.Cmd {
	if len(m.pendingNotices) == 0 {
		return nil
	}

	notices := m.pendingNotices
	m.pendingNotices = nil

	notifier := m.notifier
	return func() tea.Msg {
		for _, e := range notices {
			notifier.Notify(e)
		}
		return nil
	}
}

// toastRaisedMsg is returned when a command pushes a toast from its own
// goroutine. It exists purely to give Bubble Tea something to redraw on.
type toastRaisedMsg struct{}

// toastTimeout removes an expired toast.
type toastExpiredMsg struct{ at time.Time }

// scheduleExpiry returns a command that fires when the next toast should go.
//
// A timer rather than a frame counter: the toast lifetime is wall-clock, and
// tying it to redraws would make it vanish immediately during a sync.
func (m *Model) scheduleExpiry(at time.Time) tea.Cmd {
	if m.DisableTimers {
		// Set by tests. A tea.Cmd is a func rather than an interface, so a test
		// cannot type-assert to recognise a sleeping command; the model is asked
		// directly instead of blocking on the timer.
		return nil
	}

	d := time.Until(at)
	if d <= 0 {
		return func() tea.Msg { return toastExpiredMsg{} }
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return toastExpiredMsg{at: at} })
}

// expireToasts pops toasts that have outlived their display window.
func (m *Model) expireToasts(now time.Time) {
	for m.overlays.Len() > 0 {
		top, ok := m.overlays.Top()
		if !ok || top.Kind != overlay.KindToast {
			return
		}
		if now.Before(top.ExpiresAt) {
			return
		}
		m.overlays.Pop()
	}
}

// PumpEvents starts the goroutine that drains the sync event stream.
//
// It is called once from main before Bubble Tea starts, so that events arriving
// during startup are buffered rather than dropped. The returned cancel function
// stops the goroutine; [Model.Shutdown] calls it.
func (m *Model) PumpEvents() {
	if m.deps.Sync == nil {
		return
	}

	m.eventMu.Lock()
	if m.eventCancel != nil {
		// Already pumping.
		m.eventMu.Unlock()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.eventCancel = cancel
	events := m.deps.Sync.Events()
	m.eventMu.Unlock()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-events:
				if !ok {
					return
				}
				// Send to the program rather than mutating the model: the model
				// belongs to the UI goroutine, and Bubble Tea's Send is the only
				// safe way in from elsewhere.
				if m.program != nil {
					m.program.Send(eventsMsg{events: []whatsapp.Event{e}})
				}
			}
		}
	}()
}

// AttachProgram records the running program so the event pump can post to it.
//
// It must be called after NewProgram and before Run, which is the only window in
// which the program exists but has not yet started its loop.
func (m *Model) AttachProgram(p *tea.Program) {
	m.eventMu.Lock()
	m.program = p
	m.eventMu.Unlock()
}
