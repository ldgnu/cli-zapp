package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/models"
)

// Composition modes: replying and editing.
//
// The two are mutually exclusive by construction. A single mode field would allow
// the impossible state of editing one message while quoting another, which is
// precisely the kind of bug that reaches production because nothing tests for it.

// beginReply starts a reply to the cursor message.
//
// A revoked message cannot be replied to: its content is gone, so a quote of it
// would be empty and confusing.
func (m *Model) beginReply() tea.Cmd {
	msg, ok := m.messageAt(m.msgSel)
	if !ok || msg.Revoked {
		return nil
	}

	m.editing = "" // replying supersedes an edit in progress
	m.replyTo = &models.MessageRef{
		ID:         msg.ID,
		SenderID:   msg.SenderID,
		SenderName: m.senderName(msg),
		Body:       msg.Body,
		Kind:       msg.Kind,
		Timestamp:  msg.Timestamp,
		Revoked:    msg.Revoked,
	}
	m.setFocus(keybindings.PanelComposer)
	return nil
}

// beginEdit starts editing the cursor message.
//
// Only the user's own messages can be edited. Anything else is refused with an
// explanation rather than silently doing nothing, because a key that appears dead
// is indistinguishable from a broken binding.
func (m *Model) beginEdit() tea.Cmd {
	msg, ok := m.messageAt(m.msgSel)
	switch {
	case !ok:
		return nil
	case !msg.IsOutgoing():
		return m.toast("Only your own messages can be edited", false)
	case msg.Revoked:
		return m.toast("That message was deleted", false)
	}

	m.replyTo = nil // editing supersedes a reply in progress
	m.editing = msg.ID
	m.composer.SetValue(msg.Body)
	m.setFocus(keybindings.PanelComposer)
	return nil
}
