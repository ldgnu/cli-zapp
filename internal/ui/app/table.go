package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/ui/component"
)

// Categories the palette groups commands under.
//
// The grouping is by what a command acts on rather than by where it lives in the code,
// because that is how someone looking for "how do I mute this?" thinks about it.
const (
	categoryChat   = "Conversación"
	categoryMsg    = "Mensaje"
	categoryWindow = "Aplicación"
)

// rebuildCommands regenerates the palette's command table.
//
// It is rebuilt whenever the state changes rather than held constant, because several
// commands are only meaningful in context: "Responder" needs a message under the cursor,
// "Eliminar conversación" needs an open conversation.
//
// A command that cannot run right now is listed and greyed rather than hidden. That is
// the difference between a feature the user has not found yet and a feature that is
// missing.
//
// # One line per command
//
// Every entry is built by [Model.command], which wires the description, the shortcut and
// the action to execute. Adding a command is one line here and nowhere else — there is no
// dispatch table to update in parallel, and no way for the two to disagree about what a
// command does. That is the extensibility requirement: the palette is a list of values
// and the application decides what running one means.
func (m *Model) rebuildCommands() {
	chat := m.hasOpenChat()
	msg := m.hasCursorMessage()
	media := m.hasCursorMedia()
	outgoing := m.cursorIsOutgoing()

	m.commands = []component.Command{
		// Conversations.
		m.command("open", "Abrir conversación", "Abre la conversación resaltada", "mod+enter",
			categoryChat, chat, false, keybindings.ChatOpen),
		m.command("search", "Buscar", "Filtra las conversaciones por nombre o mensaje", "mod+f",
			categoryChat, true, false, keybindings.ActionSearch),
		m.command("info", "Información", "Teléfono, nota y presencia del contacto", "mod+g",
			categoryChat, chat, false, keybindings.ActionInfo),
		m.command("new_chat", "Nueva conversación", "Abre el menú de conversaciones", "mod+n",
			categoryChat, true, false, keybindings.ActionNewChat),
		// Pin and mark-read are here rather than on a key because their keys were worth
		// less than the palette's. Leaving a flag out of the table entirely would make
		// it unreachable, which is the one outcome worse than giving up the shortcut.
		m.command("pin", "Anclar", "Mantiene la conversación al principio de la lista", "",
			categoryChat, chat, false, keybindings.ActionTogglePin),
		m.command("mark_read", "Marcar como leída", "Quita la insignia de no leídas", "mod+u",
			categoryChat, chat && m.chatHasUnread(), false, keybindings.ActionToggleRead),
		m.command("archive", "Archivar", "Mueve la conversación fuera de la lista", "",
			categoryChat, chat, false, keybindings.ActionToggleArchive),
		m.command("delete_chat", "Eliminar conversación",
			"Borra la conversación y sus mensajes en este dispositivo", "",
			categoryChat, chat, true, keybindings.ActionDeleteChat),

		// Messages.
		m.command("reply", "Responder", "Responde citando el mensaje", "r",
			categoryMsg, msg, false, keybindings.ActionReply),
		m.command("edit", "Editar mensaje", "Reemplaza el cuerpo del mensaje", "",
			categoryMsg, msg && outgoing, false, keybindings.ActionEdit),
		m.command("react", "Reaccionar", "Añade una reacción al mensaje", "R",
			categoryMsg, msg, false, keybindings.ActionReact),
		m.command("forward", "Reenviar", "Envía el mensaje a otra conversación", "",
			categoryMsg, msg, false, keybindings.ActionForward),
		m.command("copy", "Copiar", "Pone el mensaje en el portapapeles", "",
			categoryMsg, msg, false, keybindings.ActionCopy),
		m.command("select", "Seleccionar mensaje", "Marca el mensaje para una acción", "x",
			categoryMsg, msg, false, keybindings.ActionSelect),
		m.command("select_all", "Seleccionar todos", "Marca todos los mensajes", "mod+a",
			categoryMsg, msg, false, keybindings.ActionSelectAll),
		m.command("download", "Descargar adjunto", "Guarda el archivo en disco", "",
			categoryMsg, media, false, keybindings.ActionDownload),
		m.command("open_attachment", "Abrir adjunto", "Abre el archivo fuera del terminal", "",
			categoryMsg, media, false, keybindings.ActionOpen),
		m.command("delete_message", "Eliminar mensaje", "Lo elimina para todos", "",
			categoryMsg, msg, true, keybindings.ActionDelete),

		// Application.
		m.command("sync", "Sincronizar", "Reconecta y recupera el historial perdido", "mod+r",
			categoryWindow, true, false, keybindings.ActionSync),
		m.command("help", "Keybindings", "Muestra esta application's atajos", "mod+?",
			categoryWindow, true, false, keybindings.ActionHelp),
		m.command("cancel", "Cancelar", "Abandona el modo actual", "esc",
			categoryWindow, m.hasAnythingToCancel(), false, keybindings.ActionCancel),
		m.command("quit", "Salir", "Cierra la aplicación", "mod+q",
			categoryWindow, true, false, keybindings.ActionQuit),
	}

	m.palette.SetCommands(m.commands)
}

// command builds one palette entry, wiring the action it runs.
//
// The parameters are positional because the alternative is a struct literal per
// command, and twenty of those with four booleans among the fields is exactly the shape
// in which "dangerous" ends up next to "unavailable" by accident.
func (m *Model) command(
	id, title, description, shortcut, category string,
	available, danger bool,
	action keybindings.Action,
) component.Command {
	return component.Command{
		ID:          id,
		Title:       title,
		Description: description,
		Shortcut:    shortcut,
		Category:    category,
		Available:   available,
		Danger:      danger,
		// The closure is built here and now, capturing the model as it is at this
		// moment. That is why the table is rebuilt on every change rather than held:
		// a closure captured once would run against a state the user has left behind.
		Execute: func() tea.Cmd { return m.run(action) },
	}
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
