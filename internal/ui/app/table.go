package app

import (
	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/ui/component"
)

// Sections the palette groups commands under.
//
// The grouping is by what the command acts on rather than by where it lives in the
// code, because that is how a user looking for "how do I mute this?" thinks about it.
const (
	sectionChat   = "Conversación"
	sectionMsg    = "Mensaje"
	sectionWindow = "Aplicación"
)

// rebuildCommands regenerates the palette's command table.
//
// It is rebuilt whenever the state changes rather than held constant, because several
// commands are only meaningful in context: "responder" needs a message under the
// cursor, "eliminar conversación" needs an open conversation.
//
// A command that cannot run right now is listed and greyed rather than hidden. That is
// the whole reason for the Available field: it is the difference between a feature the
// user has not found yet and a feature that is missing.
//
// # The table is the only place a command is named
//
// The palette does not dispatch; it emits the [component.Command] it was configured
// with and the root resolves it by action through [commandAction]. A command in this
// table and an action in [keybindings] are the same thing seen from two directions,
// and keeping the pairing in one map is what stops the two lists from drifting.
func (m *Model) rebuildCommands() {
	chat := m.hasOpenChat()
	msg := m.hasCursorMessage()
	media := m.hasCursorMedia()
	outgoing := m.cursorIsOutgoing()

	m.commands = []component.Command{
		// Conversations.
		entry("open", "Abrir conversación", "mod+enter", sectionChat, chat, false),
		entry("search", "Buscar", "mod+f", sectionChat, true, false),
		entry("info", "Información del contacto", "mod+g", sectionChat, chat, false),
		entry("new_chat", "Ir a otra conversación", "mod+n", sectionChat, true, false),

		// Conversation flags. Read and unread are two entries rather than one toggling
		// command because the user knows which state they want, not which key flips it.
		entry("toggle_read", "Marcar como leída", "mod+u", sectionChat, chat, false),
		entry("toggle_unread", "Marcar como no leída", "", sectionChat,
			chat && m.chatHasUnread(), false),
		entry("toggle_pin", "Anclar conversación", "mod+p", sectionChat, chat, false),
		entry("toggle_mute", "Silenciar conversación", "mod+m", sectionChat, chat, false),
		entry("toggle_archive", "Archivar conversación", "mod+e", sectionChat, chat, false),
		entry("delete_chat", "Eliminar conversación", "mod+backspace", sectionChat, chat, true),

		// Messages.
		entry("reply", "Responder", "r", sectionMsg, msg, false),
		entry("edit", "Editar mensaje", "mod+e", sectionMsg, msg && outgoing, false),
		entry("react", "Reaccionar", "R", sectionMsg, msg, false),
		entry("forward", "Reenviar", "", sectionMsg, msg, false),
		entry("copy", "Copiar mensaje", "mod+c", sectionMsg, msg, false),
		entry("select", "Seleccionar mensaje", "space", sectionMsg, msg, false),
		entry("select_all", "Seleccionar todos", "mod+a", sectionMsg, msg, false),
		entry("download", "Descargar adjunto", "mod+s", sectionMsg, media, false),
		entry("open_attachment", "Abrir adjunto", "mod+o", sectionMsg, media, false),
		entry("delete_message", "Eliminar mensaje", "mod+d", sectionMsg, msg, true),

		// Application.
		entry("sync", "Reconectar y sincronizar", "mod+r", sectionWindow, true, false),
		entry("help", "Atajos de teclado", "mod+?", sectionWindow, true, false),
		entry("cancel", "Cancelar", "esc", sectionWindow,
			m.hasAnythingToCancel(), false),
		entry("quit", "Salir", "mod+q", sectionWindow, true, false),
	}

	m.palette.SetCommands(m.commands)
}

// entry builds one palette command.
//
// The parameters are positional because the alternative is a struct literal per
// command, and twenty of those with four booleans among the fields is exactly the shape
// in which "dangerous" ends up next to "unavailable" by accident.
func entry(
	id, title, hint, section string, available, danger bool,
) component.Command {
	return component.Command{
		ID:        id,
		Title:     title,
		Hint:      hint,
		Section:   section,
		Available: available,
		Danger:    danger,
	}
}

// commandAction maps a palette command identifier to the action that runs it.
//
// A map rather than a positional lookup, so that adding a command to the table cannot
// silently shift the dispatch of every command after it.
var commandAction = map[string]keybindings.Action{
	"open":            keybindings.ChatOpen,
	"search":          keybindings.ActionSearch,
	"info":            keybindings.ActionInfo,
	"new_chat":        keybindings.ActionNewChat,
	"toggle_read":     keybindings.ActionToggleRead,
	"toggle_unread":   keybindings.ActionToggleRead,
	"toggle_pin":      keybindings.ActionTogglePin,
	"toggle_mute":     keybindings.ActionToggleMute,
	"toggle_archive":  keybindings.ActionToggleArchive,
	"delete_chat":     keybindings.ActionDeleteChat,
	"reply":           keybindings.ActionReply,
	"edit":            keybindings.ActionEdit,
	"react":           keybindings.ActionReact,
	"forward":         keybindings.ActionForward,
	"copy":            keybindings.ActionCopy,
	"select":          keybindings.ActionSelect,
	"select_all":      keybindings.ActionSelectAll,
	"download":        keybindings.ActionDownload,
	"open_attachment": keybindings.ActionOpen,
	"delete_message":  keybindings.ActionDelete,
	"sync":            keybindings.ActionSync,
	"help":            keybindings.ActionHelp,
	"cancel":          keybindings.ActionCancel,
	"quit":            keybindings.ActionQuit,
}

// --- context predicates ---
//
// These exist so the table above reads as a list of features rather than as a thicket of
// nil checks, and so the same question is never asked two different ways in two places.

func (m *Model) hasOpenChat() bool {
	_, ok := m.currentChat()
	return ok
}

func (m *Model) hasCursorMessage() bool {
	_, ok := m.transcript.MessageAt(m.transcript.Cursor())
	return ok
}

func (m *Model) hasCursorMedia() bool {
	msg, ok := m.transcript.MessageAt(m.transcript.Cursor())
	return ok && msg.Media != nil
}

func (m *Model) cursorIsOutgoing() bool {
	msg, ok := m.transcript.MessageAt(m.transcript.Cursor())
	return ok && msg.IsOutgoing()
}

func (m *Model) chatHasUnread() bool {
	chat, ok := m.currentChat()
	return ok && chat.HasUnread
}

// hasAnythingToCancel reports whether escape would do something.
//
// Offering "cancelar" when there is nothing to cancel would mean the user presses it,
// nothing happens, and they conclude the key is broken.
func (m *Model) hasAnythingToCancel() bool {
	return m.searching || m.palette.IsOpen() || len(m.selected) > 0 ||
		m.composer.Mode() != composerModeSend || m.focus != component.RegionSidebar
}
