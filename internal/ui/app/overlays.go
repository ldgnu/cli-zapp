package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/models"
	"github.com/cli-zapp/cli-zapp/internal/text"
	"github.com/cli-zapp/cli-zapp/internal/ui/component"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/overlay"
)

// reactionGlyphs is the palette offered by the reaction picker.
//
// A fixed set rather than free emoji entry, because a picker has to be operable in a
// terminal and a full keyboard picker is far slower than just typing on a phone.
var reactionGlyphs = []string{"👍", "❤️", "😂", "😮", "😢", "🙏"}

// --- key routing ---

// onOverlayKey routes a key press to the topmost overlay.
//
// Overlays consume input before anything else does. A confirmation dialog that let
// mod+d through to the conversation behind it would delete a message while the user was
// trying to dismiss a prompt, which is the worst possible failure for a destructive
// action.
func (m *Model) onOverlayKey(top overlay.Overlay, msg tea.KeyPressMsg) tea.Cmd {
	key := component.Named(msg)

	switch top.Kind {
	case overlay.KindMenu:
		// A menu is a list, so it navigates vertically. ctrl+j and ctrl+k are
		// accepted alongside the arrows because a terminal user reaches for them.
		switch key {
		case "up", "ctrl+k":
			m.moveOverlayCursor(-1)
		case "down", "ctrl+j":
			m.moveOverlayCursor(1)
		case "esc", "q":
			m.overlays.Pop()
		case "enter":
			return m.activateOverlay()
		}
		return nil

	case overlay.KindModal:
		// A modal's buttons are a row, so it navigates horizontally. The letter
		// shortcuts are accepted because "h"/"l" is the i3 idiom for the same thing
		// and this interface follows it everywhere else.
		switch key {
		case "left", "right", "tab", "shift+tab", "h", "l":
			m.moveModalCursor(1)
		case "esc":
			m.overlays.Pop()
		case "enter":
			return m.activateOverlay()
		}
		return nil

	case overlay.KindToast:
		// A toast dismisses on any key, so a notification can never trap the user.
		m.overlays.Pop()
		return nil
	}

	return nil
}

// moveOverlayCursor moves a menu's selection.
func (m *Model) moveOverlayCursor(delta int) {
	top, ok := m.overlays.Top()
	if !ok || len(top.Items) == 0 {
		return
	}

	cursor := clamp(top.Cursor+delta, len(top.Items)-1)

	// Skip separators and disabled entries, so the cursor always lands somewhere
	// actionable. Without this, pressing enter on a separator looks like a dead key.
	for range len(top.Items) {
		if !top.Items[cursor].Separator && !top.Items[cursor].Disabled {
			break
		}
		cursor = clamp(cursor+delta, len(top.Items)-1)
	}

	top.Cursor = cursor
	m.overlays.SetTop(top)
}

// moveModalCursor cycles a modal's button focus.
func (m *Model) moveModalCursor(delta int) {
	top, ok := m.overlays.Top()
	if !ok || len(top.Buttons) == 0 {
		return
	}
	top.Cursor = clamp(top.Cursor+delta, len(top.Buttons)-1)
	m.overlays.SetTop(top)
}

// activateOverlay runs the selected overlay's action.
func (m *Model) activateOverlay() tea.Cmd {
	top, ok := m.overlays.Top()
	if !ok {
		return nil
	}

	switch top.Kind {
	case overlay.KindMenu:
		if top.Cursor < 0 || top.Cursor >= len(top.Items) {
			return nil
		}
		item := top.Items[top.Cursor]
		if item.Separator || item.Disabled {
			// The cursor cannot normally rest on one, but a mouse click or a
			// shrinking menu can. Refusing is the safe answer.
			return nil
		}
		m.overlays.Pop()
		return m.runMenuAction(top.Title, item.Label)

	case overlay.KindModal:
		if len(top.Buttons) == 0 {
			m.overlays.Pop()
			return nil
		}
		label := top.Buttons[top.Cursor].Label
		m.overlays.Pop()
		return m.runModalAction(top.Title, label)

	case overlay.KindToast:
		m.overlays.Pop()
		return nil
	}

	return nil
}

// runMenuAction dispatches a menu entry.
//
// It is keyed on the menu's title rather than the label, because a menu titled "React"
// whose entries are all emoji has no other way to be identified, and keying on the
// label would mean the dispatch depends on the translation of a user-visible string.
func (m *Model) runMenuAction(title, label string) tea.Cmd {
	switch title {
	case "Reaccionar":
		return m.react(m.currentChatID(), m.transcript.CursorMessageID(), label)

	case "Mensaje":
		msg, _ := m.transcript.MessageAt(m.transcript.Cursor())
		switch label {
		case "Responder":
			return m.beginReply()
		case "Editar":
			return m.beginEdit()
		case "Descargar":
			return m.downloadAttachment()
		case "Abrir":
			return m.openAttachment(msg)
		case "Copiar":
			return m.copySelected(m.selectionOrCursor())
		case "Eliminar":
			// Deletion goes through a confirmation from the menu too, not only from
			// the keyboard shortcut. Requiring the dialog on one path and not the
			// other would make the menu a way to skip it, which is exactly the kind of
			// inconsistency that makes people not trust a confirmation they have seen
			// bypassed once.
			return m.confirmDeleteMessage()
		}

	default:
		return nil
	}
	return nil
}

// runModalAction dispatches a modal's button press.
//
// The destructive handlers are held in one table rather than inline in the switch,
// so that the complete set of irreversible operations is readable in one place. That
// is what makes the list auditable when someone asks "what can this program destroy?"
// — which is the question a reviewer should never have to grep for.
var destructiveButtons = map[string]map[string]func(*Model) tea.Cmd{
	"Eliminar conversación": {"Confirmar": (*Model).doDeleteChat},
	"Eliminar mensaje":      {"Confirmar": (*Model).doDeleteMessage},
}

func (m *Model) runModalAction(title, label string) tea.Cmd {
	if byLabel, ok := destructiveButtons[title]; ok {
		if fn, ok := byLabel[label]; ok {
			return fn(m)
		}
	}
	return nil
}

// --- overlay actions ---

// confirmDeleteChat asks before deleting the open conversation.
func (m *Model) confirmDeleteChat() tea.Cmd {
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}
	m.overlays.Push(overlay.Confirm(
		"Eliminar conversación",
		"Se eliminará la conversación con "+chat.FallbackName()+
			" y sus mensajes en este dispositivo. No se puede deshacer.",
	))
	return nil
}

// confirmDeleteMessage asks before revoking the cursor message.
func (m *Model) confirmDeleteMessage() tea.Cmd {
	msg, ok := m.transcript.MessageAt(m.transcript.Cursor())
	if !ok || msg.Revoked {
		return nil
	}
	m.overlays.Push(overlay.Confirm(
		"Eliminar mensaje",
		"Este mensaje se eliminará para todos en la conversación.",
	))
	return nil
}

// doDeleteChat carries out a confirmed conversation deletion.
func (m *Model) doDeleteChat() tea.Cmd {
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}
	// The cached transcript belonged to the conversation that just went away; keeping
	// it would render another conversation's messages under this one's header.
	m.opened = ""
	m.messages = nil
	m.syncTranscript()
	return m.deleteChat(chat.ID)
}

// doDeleteMessage carries out a confirmed revocation.
func (m *Model) doDeleteMessage() tea.Cmd {
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}
	return m.revoke(chat.ID, m.transcript.CursorMessageID())
}

// openReactionPicker opens the fixed reaction palette.
func (m *Model) openReactionPicker() tea.Cmd {
	if _, ok := m.transcript.MessageAt(m.transcript.Cursor()); !ok {
		return nil
	}
	items := make([]overlay.MenuItem, 0, len(reactionGlyphs))
	for _, g := range reactionGlyphs {
		items = append(items, overlay.MenuItem{Label: g})
	}
	m.overlays.Push(overlay.Menu("Reaccionar", items, 0))
	return nil
}

// openMessageMenu opens the context menu for the cursor message.
//
// Which entries appear depends on the message: only the account's own messages can be
// edited or deleted for everyone, so offering those for a received message would
// promise something the protocol will refuse.
func (m *Model) openMessageMenu() tea.Cmd {
	msg, ok := m.transcript.MessageAt(m.transcript.Cursor())
	if !ok {
		return nil
	}

	items := []overlay.MenuItem{
		{Label: "Responder", Hint: "r"},
		{Label: "Reaccionar", Hint: "R"},
	}
	if msg.Media != nil {
		items = append(items,
			overlay.MenuItem{Label: "Descargar", Hint: "mod+s"},
			overlay.MenuItem{Label: "Abrir", Hint: "mod+o"},
		)
	}
	items = append(items, overlay.MenuItem{Label: "Copiar", Hint: "mod+c"}, overlay.Separator())

	if msg.IsOutgoing() && !msg.Revoked {
		items = append(items, overlay.MenuItem{Label: "Editar", Hint: "mod+e"})
	}
	if !msg.Revoked {
		items = append(items,
			overlay.MenuItem{Label: "Eliminar", Hint: "mod+d", Danger: true})
	}

	m.overlays.Push(overlay.Menu("Mensaje", items, 0))
	return nil
}

// downloadAttachment fetches the cursor message's attachment.
func (m *Model) downloadAttachment() tea.Cmd {
	msg, ok := m.transcript.MessageAt(m.transcript.Cursor())
	if !ok || msg.Media == nil {
		return m.toast("no hay ningún archivoadjunto", false)
	}
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}
	return m.download(chat.ID, msg.ID)
}

// openAttachment hands the cursor message's attachment to the desktop.
func (m *Model) openAttachment(msg models.Message) tea.Cmd {
	if msg.Media == nil {
		return m.toast("no hay ningún archivo adjunto", false)
	}
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}
	return m.openInDesktop(chat.ID, msg)
}

// openInfo shows the conversation's metadata.
func (m *Model) openInfo() tea.Cmd {
	chat, ok := m.currentChat()
	if !ok {
		return m.toast("no hay ninguna conversación abierta", false)
	}

	lines := []string{"Tipo: " + chat.Type.String()}
	if chat.Contact != nil {
		c := chat.Contact
		lines = append(lines,
			"Teléfono: "+orDash(c.Phone),
			"Nota: "+orDash(c.About),
		)
		if p := c.Presence.Label(); p != "" {
			lines = append(lines, "Presencia: "+p)
		}
		if c.Verified {
			lines = append(lines, "Verificado")
		}
	}
	if chat.Type.IsGroup() {
		lines = append(lines, "Miembros: "+text.Itoa(m.groupSize(chat)))
	}
	lines = append(lines,
		"Pinzado: "+yesNo(chat.Pinned),
		"Silenciado: "+yesNo(chat.Muted),
		"Archivado: "+yesNo(chat.Archived),
	)

	m.overlays.Push(overlay.Info("Información", lines))
	return nil
}

// groupSize returns a group's member count, or zero when it is unknown.
func (m *Model) groupSize(chat models.Chat) int {
	if m.services.Group == nil {
		return 0
	}
	g, err := m.services.Group.Get(m.ctx(), chat.ID)
	if err != nil {
		return 0
	}
	return len(g.Participants)
}

// openHelp shows the keybinding cheat sheet.
//
// It is grouped by the action's prefix so the sheet is navigable rather than a wall of
// forty lines, and it is scoped to the focused panel so it shows the keys that work
// where the user is standing rather than every key in the application.
func (m *Model) openHelp() tea.Cmd {
	entries := m.keys.Help(m.focusPanel())

	seen := make(map[string]bool)
	var lines []string
	for _, e := range entries {
		section := actionSection(string(e.Action))
		if seen[section] {
			continue
		}
		seen[section] = true

		lines = append(lines, strings.ToUpper(section))
		for _, other := range entries {
			if actionSection(string(other.Action)) != section {
				continue
			}
			k := ""
			if len(other.Keys) > 0 {
				k = other.Keys[0]
			}
			lines = append(lines, "  "+padKey(k)+"  "+other.Help)
		}
	}

	m.overlays.Push(overlay.Info("Atajos de teclado", lines))
	return nil
}

// openNewChat opens a conversation picker.
//
// Starting a chat needs a destination, and there is no destination to infer: unlike a
// web client there is no URL bar to paste a number into. The palette already lists
// every conversation, so it is reused rather than a second picker being written.
func (m *Model) openNewChat() tea.Cmd { return m.palette.Open("") }

// actionSection returns the part of an action before the dot, e.g. "chat" for
// "chat.toggle_mute". An action with no dot is its own section.
func actionSection(a string) string {
	if i := strings.IndexByte(a, '.'); i >= 0 {
		return a[:i]
	}
	return a
}

// padKey right-pads a key label to a fixed width, for the help sheet.
func padKey(k string) string {
	const width = 12
	if len(k) >= width {
		return k
	}
	return k + strings.Repeat(" ", width-len(k))
}

// yesNo renders a boolean as Spanish prose.
func yesNo(b bool) string {
	if b {
		return "sí"
	}
	return "no"
}

// clamp limits v to [0, hi].
//
// The lower bound of zero rather than -1 is what every call site needs: an index is
// never meaningfully negative once clamped, and the empty-list case is handled
// explicitly by refusing to index at all.
func clamp(v, hi int) int {
	if hi < 0 {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > hi {
		return hi
	}
	return v
}
