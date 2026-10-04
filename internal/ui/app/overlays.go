package app

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/ui/components/overlay"
)

// reactionShortcuts is the palette offered by the reaction picker.
//
// A fixed palette rather than free emoji entry, because a picker needs to be
// operable in a terminal and a full keyboard is far too slow.
var reactionShortcuts = []string{"👍", "❤️", "😂", "😮", "😢", "🙏"}

// handleOverlayKey routes a key press to the topmost overlay.
//
// Overlays consume input before anything else does. A confirmation dialog that
// let "mod+d" through to the conversation behind it would delete a message
// while the user was trying to dismiss a prompt.
func (m *Model) handleOverlayKey(top overlay.Overlay, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Modal buttons navigate horizontally; menus navigate vertically.
	switch top.Kind {
	case overlay.KindMenu:
		switch key {
		case "up", "ctrl+k":
			m.moveOverlayCursor(-1)
			return m, nil
		case "down", "ctrl+j":
			m.moveOverlayCursor(1)
			return m, nil
		case "esc", "q":
			m.overlays.Pop()
			return m, nil
		case "enter":
			return m.activateOverlay()
		}
		// Anything else is ignored rather than passed through.
		return m, nil

	case overlay.KindModal:
		switch key {
		case "left", "right", "tab", "shift+tab", "h", "l":
			m.moveModalCursor(1)
			return m, nil
		case "esc":
			m.overlays.Pop()
			return m, nil
		case "enter":
			return m.activateOverlay()
		}
		return m, nil

	case overlay.KindToast:
		// Toasts dismiss on any key, so a notification cannot trap the user.
		m.overlays.Pop()
		return m, nil

	default:
		return m, nil
	}
}

// moveOverlayCursor moves a menu's selection, skipping separators and disabled
// entries so that the cursor always lands somewhere actionable.
func (m *Model) moveOverlayCursor(delta int) {
	top, ok := m.overlays.Top()
	if !ok {
		return
	}
	n := len(top.Items)
	if n == 0 {
		return
	}

	cursor := clamp(top.Cursor+delta, n-1)
	for range n {
		if !top.Items[cursor].Separator && !top.Items[cursor].Disabled {
			break
		}
		cursor = clamp(cursor+delta, n-1)
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

// activateOverlay runs the selected overlay action.
func (m *Model) activateOverlay() (tea.Model, tea.Cmd) {
	top, ok := m.overlays.Top()
	if !ok {
		return m, nil
	}

	switch top.Kind {
	case overlay.KindMenu:
		if top.Cursor < 0 || top.Cursor >= len(top.Items) {
			return m, nil
		}
		item := top.Items[top.Cursor]
		m.overlays.Pop()

		if action, isDelete := deleteHandlers[menuActionID(top.Title, item.Label)]; isDelete {
			return m, action(m)
		}
		return m, m.runMenuAction(top.Title, item.Label)

	case overlay.KindModal:
		if len(top.Buttons) == 0 {
			m.overlays.Pop()
			return m, nil
		}
		label := top.Buttons[top.Cursor].Label
		m.overlays.Pop()
		return m, m.runModalAction(top, label)

	default:
		m.overlays.Pop()
		return m, nil
	}
}

// deleteHandlers maps a menu entry to the command it runs.
//
// The destructive actions are held separately from the label-dispatch in
// [runMenuAction] so that a deletion is one obvious place in the code, which is
// what makes the set of irreversible operations auditable.
var deleteHandlers = map[string]func(*Model) tea.Cmd{
	"delete-chat":    (*Model).deleteChatCmd,
	"delete-message": (*Model).deleteMessageCmd,
}

// menuActionID builds the identifier for a menu entry.
func menuActionID(title, label string) string {
	switch {
	case label == "Delete chat" && title == "Delete chat":
		return "delete-chat"
	case label == "Delete" && title == "Delete message":
		return "delete-message"
	default:
		return ""
	}
}

// runMenuAction dispatches a menu entry that is not a confirmed deletion.
//
// The switch is keyed on the menu title rather than the label, because a menu
// titled "React" whose entries are all emoji has no other way to be identified.
func (m *Model) runMenuAction(title, label string) tea.Cmd {
	switch title {
	case "React":
		return m.react(label)

	case "Message":
		switch label {
		case "Reply":
			return m.beginReply()
		case "Edit":
			return m.beginEdit()
		case "Download":
			return m.downloadAttachment()
		case "Open externally":
			return m.openAttachment()
		case "Copy":
			return m.copySelected()
		default:
			return nil
		}

	default:
		return nil
	}
}

// downloadAttachment fetches the cursor message's attachment.
func (m *Model) downloadAttachment() tea.Cmd {
	msg, ok := m.messageAt(m.msgSel)
	if !ok || msg.Media == nil {
		return m.toast("No attachment to download", false)
	}
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}
	svc := m.deps.Media

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		path, err := svc.Download(ctx, chat.ID, msg.ID)
		if err != nil {
			return errMsg{err: err}
		}
		m.overlays.Push(overlay.Toast("Saved to "+path, false))
		return toastRaisedMsg{}
	}
}

// openAttachment downloads if needed, then hands the file to the desktop.
func (m *Model) openAttachment() tea.Cmd {
	msg, ok := m.messageAt(m.msgSel)
	if !ok || msg.Media == nil {
		return m.toast("No attachment to open", false)
	}
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}
	svc := m.deps.Media

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		path := msg.Media.LocalPath
		if path == "" {
			p, err := svc.Download(ctx, chat.ID, msg.ID)
			if err != nil {
				return errMsg{err: err}
			}
			path = p
		}
		if err := svc.Open(ctx, path); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}

// runModalAction dispatches a modal's button press.
func (m *Model) runModalAction(top overlay.Overlay, label string) tea.Cmd {
	switch top.Title {
	case "Delete chat":
		if label == "Confirm" {
			return m.deleteChatCmd()
		}
	case "Delete message":
		if label == "Confirm" {
			return m.deleteMessageCmd()
		}
	}
	return nil
}

// deleteMessageCmd revokes the selected message.
func (m *Model) deleteMessageCmd() tea.Cmd {
	msg, ok := m.messageAt(m.msgSel)
	if !ok {
		return nil
	}
	chat, ok := m.currentChat()
	if !ok {
		return nil
	}
	svc := m.deps.Message

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := svc.Delete(ctx, chat.ID, msg.ID); err != nil {
			return errMsg{err: err}
		}
		return messagesLoadedMsg{chat: chat.ID}
	}
}

// confirmDeleteChat opens the deletion confirmation for the selected chat.
func (m *Model) confirmDeleteChat() *Model {
	chat, ok := m.currentChat()
	if !ok {
		return m
	}
	m.overlays.Push(overlay.Confirm(
		"Delete chat",
		"Delete the conversation with "+chat.FallbackName()+
			"? Messages will be removed from this device.",
	))
	return m
}

// confirmDeleteMessage opens the deletion confirmation for the cursor message.
func (m *Model) confirmDeleteMessage() *Model {
	msg, ok := m.messageAt(m.msgSel)
	if !ok || msg.Revoked {
		return m
	}
	m.overlays.Push(overlay.Confirm(
		"Delete message",
		"This message will be deleted for everyone in the conversation.",
	))
	return m
}

// openReactionPicker opens the reaction palette.
func (m *Model) openReactionPicker() *Model {
	if _, ok := m.messageAt(m.msgSel); !ok {
		return m
	}
	items := make([]overlay.MenuItem, 0, len(reactionShortcuts))
	for _, r := range reactionShortcuts {
		items = append(items, overlay.MenuItem{Label: r})
	}
	m.overlays.Push(overlay.Menu("React", items, 0))
	return m
}

// openMessageMenu opens the right-click context menu for the cursor message.
//
// Which entries appear depends on the message: only the user's own messages can
// be edited or deleted-for-everyone, so offering those for a received message
// would promise something the protocol will refuse.
func (m *Model) openMessageMenu() *Model {
	msg, ok := m.messageAt(m.msgSel)
	if !ok {
		return m
	}

	items := []overlay.MenuItem{
		{Label: "Reply", Hint: "r"},
		{Label: "React", Hint: "R"},
	}
	if msg.Media != nil {
		items = append(items,
			overlay.MenuItem{Label: "Download", Hint: "mod+s"},
			overlay.MenuItem{Label: "Open externally", Hint: "mod+o"},
		)
	}
	items = append(items, overlay.MenuItem{Label: "Copy", Hint: "mod+c"},
		overlay.Separator(),
	)

	if msg.IsOutgoing() && !msg.Revoked {
		items = append(items, overlay.MenuItem{Label: "Edit", Hint: "mod+e"})
	}
	if !msg.Revoked {
		items = append(items,
			overlay.MenuItem{Label: "Delete", Hint: "mod+d", Danger: true})
	}

	m.overlays.Push(overlay.Menu("Message", items, 0))
	return m
}

// openNewChat opens a menu of candidate chats to start.
func (m *Model) openNewChat() tea.Cmd {
	return m.listChats()
}

// openInfo shows the contact or group information.
func (m *Model) openInfo() *Model {
	chat, ok := m.currentChat()
	if !ok {
		return m
	}

	lines := []string{"Type: " + chat.Type.String()}
	if chat.Type.IsGroup() {
		lines = append(lines, "Members: see group roster")
	}
	if chat.Contact != nil {
		c := chat.Contact
		lines = append(lines, "Phone: "+orDash(c.Phone))
		lines = append(lines, "About: "+orDash(c.About))
		if p := c.Presence.Label(); p != "" {
			lines = append(lines, "Presence: "+p)
		}
	}

	m.overlays.Push(overlay.Info("Chat info", lines))
	return m
}

// openHelp opens the keybinding cheat sheet.
func (m *Model) openHelp() *Model {
	entries := m.keys.Help(m.focus)

	// Group by the action's prefix so the sheet is navigable rather than a wall.
	var sections []string
	seen := make(map[string]bool)
	for _, e := range entries {
		section := cutAction(string(e.Action))
		if seen[section] {
			continue
		}
		seen[section] = true

		sections = append(sections, strings.ToUpper(section))
		for _, other := range entries {
			if s := cutAction(string(other.Action)); s != section {
				continue
			}
			key := ""
			if len(other.Keys) > 0 {
				key = other.Keys[0]
			}
			sections = append(sections, "  "+padKey(key)+"  "+other.Help)
		}
	}

	m.overlays.Push(overlay.Info("Keybindings", sections))
	return m
}

// toast pushes a transient notification and schedules its expiry.
//
// The command is returned rather than run, so that the timer is registered with
// Bubble Tea's loop: a raw time.After would fire on a goroutine nothing is
// listening to.
func (m *Model) toast(msg string, isError bool) tea.Cmd {
	o := overlay.Toast(msg, isError)
	m.overlays.Push(o)
	return m.scheduleExpiry(o.ExpiresAt)
}

// orDash returns s, or an em dash when empty, for info panels.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// padKey right-pads a key label to a fixed width for the help sheet.
func padKey(k string) string {
	const width = 12
	if len(k) >= width {
		return k
	}
	return k + spaces(width-len(k))
}

func spaces(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

// cutAction returns the section part of an action name, e.g. "chat" for
// "chat.toggle_mute". An action with no dot is its own section.
func cutAction(a string) string {
	for i := range len(a) {
		if a[i] == '.' {
			return a[:i]
		}
	}
	return a
}

// currentChatID returns the open chat's identifier, empty when none is open.
func (m *Model) currentChatID() models.ChatID {
	chat, ok := m.currentChat()
	if !ok {
		return ""
	}
	return chat.ID
}
