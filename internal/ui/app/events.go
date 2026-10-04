package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/models"
	"github.com/cli-zapp/cli-zapp/internal/notifications"
	"github.com/cli-zapp/cli-zapp/internal/ui/component"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/composer"
	"github.com/cli-zapp/cli-zapp/internal/whatsapp"
)

// onEvent routes an event emitted by a region.
//
// This is where the architecture pays off: every consequence of a region's intent is
// decided here, in one function, with no region code involved. The transcript emitting
// "the user wants to reply" is a fact about the user; what that means for the
// composer, the focus and the services is this switch.
//
// The cases that merely re-fetch are grouped, because a region reporting a selection
// and a service reporting a changed message both mean the same thing: this region
// needs to know.
func (m *Model) onEvent(e component.Event) tea.Cmd {
	switch e.Kind {
	case component.KindFocusChat:
		return m.openConversation(e.ChatID)

	case component.KindFocusRegion:
		return m.setFocus(e.Command.ID)

	case component.KindSearchChanged:
		m.search = e.Query
		m.syncSidebar()
		return nil

	case component.KindSearchDismissed:
		return m.dismissSearch()

	case component.KindSubmit:
		// A submission carrying a mode is the composer pressing enter; one without is
		// the composer reporting that its buffer changed, which the application only
		// needs so that a redraw happens for the autocomplete state.
		if mode, ok := e.Payload.(composer.Mode); ok {
			return m.sendDraft(e.Text, mode)
		}
		return nil

	case component.KindCancel:
		return m.onEscape()

	case component.KindScroll:
		m.transcript.Scroll(e.Delta)
		return nil

	case component.KindScrollTo:
		m.transcript.ScrollToNewest()
		return nil

	case component.KindMessageSelected:
		// The transcript told us where its cursor is. Nothing to do: the region owns
		// that state and the application reads it when it needs it. The event exists
		// so that a future root could react — a status line showing the selected
		// message, say — without the transcript knowing.
		return nil

	case component.KindMessageActivated:
		return m.activateMessage(e)

	case component.KindSelectionToggled:
		if m.selected[e.MessageID] {
			delete(m.selected, e.MessageID)
		} else {
			m.selected[e.MessageID] = true
		}
		m.syncTranscript()
		return nil

	case component.KindCommandChosen:
		// The command carries what it does. The palette chose it; the application runs
		// it, which is the whole of the split between the two.
		if e.Command.Execute == nil {
			// Declared but not implemented, which is a table bug. Saying so beats a
			// palette entry that looks right and does nothing.
			return m.toast("comando no implementado: "+e.Command.ID, true)
		}
		return e.Command.Execute()

	case component.KindDismissOverlay:
		m.palette.Close()
		return nil

	case component.KindQuit:
		return tea.Quit

	default:
		// A kind this model does not act on. Spelling the default out rather than
		// listing every kind keeps the switch honest when one is added: the new case
		// lands here and is silently ignored, which the compiler cannot catch but a
		// reader can see.
		return nil
	}
}

// activateMessage runs an action a region requested on a message or a conversation.
//
// The region names the action by the string form of its keybinding action rather than
// interpreting it, so that adding a key never requires touching region code and a
// typo in a region resolves to a no-op rather than to the wrong effect.
func (m *Model) activateMessage(e component.Event) tea.Cmd {
	a, ok := keybindings.ParseAction(e.Text)
	if !ok {
		return nil
	}
	return m.run(a)
}

// openConversation makes a conversation current and fetches its transcript.
func (m *Model) openConversation(id models.ChatID) tea.Cmd {
	if id == "" {
		return nil
	}

	// Move the sidebar's cursor so that "open" and "select" agree on what is open.
	// The sidebar owns that state, so it is asked rather than set from outside.
	m.sidebar.SelectChat(id)

	if m.opened == id {
		// Already open: the fetch is a no-op and the conversation is simply brought
		// into view. Re-fetching on every press would make pressing enter twice feel
		// like a hang on a slow connection.
		m.transcript.ScrollToNewest()
		return nil
	}

	m.opened = id
	m.messages = nil
	m.syncTranscript()
	m.queue(m.reload(id))
	return nil
}

// sendDraft delivers a body the composer submitted.
//
// The command is built before the compose-mode fields are cleared, for the same reason
// as in [Model.submitDraft]: the command reads them.
func (m *Model) sendDraft(body string, mode composer.Mode) tea.Cmd {
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}

	// The limiter is checked here, on the event path, rather than in submitDraft.
	//
	// Those are two different send routes: submitDraft is mod+enter on the focused
	// region, while this is the composer consuming enter itself and emitting
	// KindSubmit. Wiring the limit into only one of them leaves the other unbounded,
	// and enter is the key a runaway repeat actually presses.
	//
	// The draft is restored on refusal. The composer has already cleared its buffer
	// by the time the event arrives — it clears before emitting — so returning a
	// refusal here without putting the text back means the user's message vanishes.
	// A limit that eats what someone wrote is worse than no limit: they retype it,
	// press enter again out of frustration, and are refused again.
	if allowed, reason := m.limiter.allow(time.Now()); !allowed {
		m.composer.Restore(body, mode)
		return m.toast(reason, true)
	}

	cmd := m.send(chat.ID, body, mode)
	m.replyTo, m.editing = nil, ""
	return cmd
}

// --- account events ---

// setEvents folds a batch of account events into the model.
//
// Batching is what keeps a resync usable: two thousand history messages become a
// handful of redraws rather than two thousand, because the model decides once what
// needs refetching rather than once per message.
func (m *Model) setEvents(events []whatsapp.Event) tea.Cmd {
	if len(events) == 0 {
		// The loop continues even when a batch is empty: returning nil would stop the
		// stream from being drained, and the next batch would never arrive.
		return m.waitForEvent()
	}

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
			// Presence touches the header and the sidebar badge only. The transcript is
			// untouched, so no re-fetch is needed.
			chatsDirty = true

		case whatsapp.EventConnection:
			m.connection = connectionFor(e.Connection)

		case whatsapp.EventError:
			if e.Err != nil {
				cmds = append(cmds, m.toast(e.Err.Error(), true))
			}

		case whatsapp.EventUnknown:
			// An event with no payload is nothing to act on. Listed explicitly so that
			// adding a kind to the interface is a compile-time decision rather than a
			// silently unhandled branch that ships.
		}
	}

	if openChanged {
		if id := m.currentChatID(); id != "" {
			cmds = append(cmds, m.reload(id))
		}
	}
	if chatsDirty {
		cmds = append(cmds, m.listChats())
	}

	// Keep the loop running. This is the one command that must always be re-armed:
	// forgetting it here silently ends message reception with nothing on screen
	// indicating why.
	cmds = append(cmds, m.waitForEvent())

	return tea.Batch(cmds...)
}

// raiseNotification queues a desktop notification for an incoming message.
//
// A message in the conversation on screen does not notify: the user is already looking
// at it, and a notification for it is pure noise. Neither does a muted conversation —
// muting is a request not to be interrupted, and ignoring it is worse than the
// feature not existing.
func (m *Model) raiseNotification(e whatsapp.Event) {
	if e.Message.Direction != models.DirectionIncoming {
		return
	}
	if e.ChatID == m.currentChatID() {
		return
	}
	if chat, ok := m.chatByID(e.ChatID); ok && chat.Muted {
		return
	}

	name := "Mensaje nuevo"
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
