package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/ui/components/statusbar"
	"github.com/wterm/wterm/internal/whatsapp"
)

// Messages the model consumes, carrying results back from commands.
//
// Each command runs on Bubble Tea's goroutine and returns one of these; Update
// then folds the result into the state. This is what keeps every service call
// off the render path.
type (
	// chatsLoadedMsg carries the chat list.
	chatsLoadedMsg struct{ chats []models.Chat }

	// messagesLoadedMsg carries a transcript.
	messagesLoadedMsg struct {
		chat     models.ChatID
		messages []models.Message
	}

	// statusUpdatedMsg carries a connection state transition.
	statusUpdatedMsg struct{ conn statusbar.Connection }

	// errMsg carries a non-fatal failure worth surfacing to the user.
	errMsg struct{ err error }
)

// eventsMsg carries a batch of sync events drained from the service.
//
// Draining in batches rather than one message per event is what keeps a busy
// sync from flooding Bubble Tea's message loop with a redraw per event.
type eventsMsg struct{ events []whatsapp.Event }

// handleEscape implements cancel, which is contextual rather than uniform.
//
// "Cancel" means different things depending on what is in progress, and always
// doing the same thing would leave the user stuck in a mode they cannot leave.
// handleKey routes a key press.
//
// The order below is the entire input architecture, and each step is load-bearing:
//
//  1. Overlays consume input first, because a modal must not let a key fall
//     through to the conversation behind it.
//  2. Escape is handled next, since the user must always be able to abandon a mode
//     they are stuck in.
//  3. Global bindings are resolved before any text field sees the key.
//  4. The search field, then the composer, get what is left.
//  5. Panel bindings handle the remainder.
//
// Step 3 before step 4 is the counter-intuitive one. If the composer went first it
// would swallow every modified key — including ctrl+q, ctrl+f and ctrl+r — because
// a text area accepts arbitrary input and reports that it consumed the press. The
// user would find that no global shortcut works while the composer has focus,
// which is where they spend most of their time.
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if top, ok := m.overlays.Top(); ok {
		return m.handleOverlayKey(top, msg)
	}

	if msg.String() == "esc" {
		return m.handleEscape()
	}

	if action := m.keys.ResolveGlobal(convertKey(msg)); action != keybindings.ActionNone {
		return m.dispatch(action)
	}

	if m.searchActive {
		if handled, cmd := m.handleSearchKey(msg); handled {
			return m, cmd
		}
	}

	if m.focus == keybindings.PanelComposer && m.composer.Update(msg) {
		return m, nil
	}

	action := m.keys.Resolve(convertKey(msg), m.focus)
	if action == keybindings.ActionNone {
		return m, nil
	}
	return m.dispatch(action)
}

func (m *Model) handleEscape() (tea.Model, tea.Cmd) {
	switch {
	case m.searchActive:
		// Leave search mode but keep the query visible, so the user can see what
		// they were looking at before starting over.
		m.searchActive = false
		m.searchQuery = ""
		m.chatSel = -1
		m.chatOffset = 0
		return m, m.queue(m.listChats())

	case m.editing != "":
		m.editing = ""
		m.composer.Reset()
		return m, nil

	case m.replyTo != nil:
		m.replyTo = nil
		m.composer.Reset()
		return m, nil

	case len(m.selected) > 0:
		clear(m.selected)
		return m, nil

	default:
		// Nothing to cancel, so move to the sidebar: a predictable way back to the
		// chat list for someone using only the keyboard.
		m.setFocus(keybindings.PanelSidebar)
		return m, nil
	}
}

// dispatch executes an action.
func (m *Model) dispatch(a keybindings.Action) (tea.Model, tea.Cmd) {
	switch a {
	// Application.
	case keybindings.ActionQuit:
		return m, tea.Quit
	case keybindings.ActionSearch:
		return m, m.beginSearch()
	case keybindings.ActionSync:
		return m, m.resync()
	case keybindings.ActionNewChat:
		return m, m.openNewChat()
	case keybindings.ActionInfo:
		return m.openInfo(), nil
	case keybindings.ActionHelp:
		return m.openHelp(), nil
	case keybindings.ActionCancel:
		return m.handleEscape()
	case keybindings.ActionConfirm:
		return m.confirm()

	// Panels.
	case keybindings.PanelNext:
		m.setFocus(m.focus.Next())
		return m, nil
	case keybindings.PanelPrev:
		m.setFocus(m.focus.Prev())
		return m, nil

	// Navigation.
	case keybindings.NavUp:
		m.move(-1)
		return m, nil
	case keybindings.NavDown:
		m.move(1)
		return m, nil
	case keybindings.NavLeft:
		m.setFocus(keybindings.PanelSidebar)
		return m, nil
	case keybindings.NavRight:
		m.setFocus(keybindings.PanelComposer)
		return m, nil

	// Scrolling.
	case keybindings.ScrollUp:
		m.scroll(-1)
		return m, nil
	case keybindings.ScrollDown:
		m.scroll(1)
		return m, nil
	case keybindings.ScrollTop:
		m.scrollTo(0)
		return m, nil
	case keybindings.ScrollBottom:
		m.scrollTo(m.maxScrollOffset())
		return m, nil
	case keybindings.PageUp:
		m.page(-1)
		return m, nil
	case keybindings.PageDown:
		m.page(1)
		return m, nil

	// Sidebar.
	case keybindings.ChatOpen:
		return m, m.openChat()
	case keybindings.ActionToggleRead:
		return m.toggleRead()
	case keybindings.ActionTogglePin:
		return m.togglePin()
	case keybindings.ActionToggleMute:
		return m.toggleMute()
	case keybindings.ActionToggleArchive:
		return m.toggleArchive()
	case keybindings.ActionDeleteChat:
		return m.confirmDeleteChat(), nil

	// Messages.
	case keybindings.ActionReply:
		return m, m.beginReply()
	case keybindings.ActionEdit:
		return m, m.beginEdit()
	case keybindings.ActionDelete:
		return m.confirmDeleteMessage(), nil
	case keybindings.ActionReact:
		return m.openReactionPicker(), nil
	case keybindings.ActionSelect:
		m.toggleSelect()
		return m, nil
	case keybindings.ActionSelectAll:
		m.selectAll()
		return m, nil
	case keybindings.ActionCopy:
		return m, m.copySelected()

	// Composer.
	case keybindings.SendMessage:
		return m.send()
	case keybindings.InsertNewline:
		m.composer.Insert("\n")
		return m, nil

	default:
		// An unhandled action is a no-op rather than a panic: a binding table may
		// legitimately name an action that a later phase implements.
		return m, nil
	}
}

// confirm performs the primary action for the focused panel.
//
// mod+enter means "do the main thing here", which is what makes the modifier
// hierarchy worth having: the same key is useful in every panel and does the
// locally obvious thing.
func (m *Model) confirm() (tea.Model, tea.Cmd) {
	switch m.focus {
	case keybindings.PanelSidebar:
		return m, m.openChat()
	case keybindings.PanelMessages:
		if m.msgSel >= 0 {
			return m, m.beginReply()
		}
		return m, nil
	case keybindings.PanelComposer:
		return m.send()
	default:
		return m, nil
	}
}

// setFocus moves keyboard focus and keeps the composer's caret in sync.
func (m *Model) setFocus(p keybindings.Panel) {
	if p == m.focus {
		return
	}
	m.prevFocus = m.focus
	m.focus = p

	if p == keybindings.PanelComposer {
		m.composer.Focus()
		return
	}
	m.composer.Blur()
}

// clamp limits v to [lo, hi].
//
// The two-argument form clamps to a zero lower bound, which is every call site's
// need: a selection index is never meaningfully negative in this model, and the
// empty-list case is handled explicitly by setting -1.
func clamp(v, hi int) int {
	if hi < 0 {
		return 0
	}
	return minInt(maxInt(v, 0), hi)
}

// minInt returns the smaller of two ints.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// maxInt returns the larger of two ints.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
