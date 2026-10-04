package app

import (
	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/ui/components/chatlist"
)

// Navigation and selection.
//
// Selection and scrolling are deliberately different operations. mod+j moves a
// highlight from message to message, while j scrolls the view. Conflating them —
// which is the usual shortcut in a file browser — makes it impossible to read
// back through a long conversation without losing your place.

// move changes the selection within the focused panel.
func (m *Model) move(delta int) {
	switch m.focus {
	case keybindings.PanelSidebar:
		n := len(m.visibleChats())
		if n == 0 {
			m.chatSel = -1
			return
		}
		m.chatSel = clamp(m.chatSel+delta, n-1)
		m.chatOffset = m.chatOffsetFor(m.chatSel)

	case keybindings.PanelMessages, keybindings.PanelComposer:
		// In the transcript, moving selects a message rather than scrolling it.
		n := m.messageCount()
		if n == 0 {
			m.msgSel = -1
			return
		}
		m.msgSel = clamp(m.msgSel+delta, n-1)
		m.atBottom = m.msgSel >= n-1
	}
}

// scroll moves the view without changing the selection.
//
// This is what the wheel, the page keys and the arrow-less scroll keys do: a
// reader who has scrolled up to find something has not thereby chosen a message.
func (m *Model) scroll(delta int) {
	if m.focus == keybindings.PanelSidebar {
		m.chatOffset = clamp(m.chatOffset+delta*chatlist.RowHeight, m.maxChatOffset())
		return
	}
	m.scrollTo(m.msgOffset + delta)
}

// page scrolls by a viewport's worth of lines.
func (m *Model) page(delta int) {
	m.scrollTo(m.msgOffset + delta*viewportHeight(m))
}

// scrollTo moves the transcript to a line offset, clamped to the content.
func (m *Model) scrollTo(line int) {
	maxOffset := m.maxScrollOffset()
	m.msgOffset = clamp(line, maxOffset)
	m.atBottom = m.msgOffset >= maxOffset
}

// maxScrollOffset returns the largest valid transcript scroll offset.
func (m *Model) maxScrollOffset() int {
	total := 0
	for _, r := range m.rows {
		total += r.Height
	}
	return maxInt(total-viewportHeight(m), 0)
}

// toggleSelect adds or removes the cursor message from the selection.
func (m *Model) toggleSelect() {
	if m.msgSel < 0 || m.msgSel >= len(m.rowMessages) {
		return
	}
	id := m.rowMessages[m.msgSel]
	if m.selected[id] {
		delete(m.selected, id)
		return
	}
	m.selected[id] = true

	// Advance so that a run of selections needs one key press per message rather
	// than two.
	m.move(1)
}

// selectAll selects every message, or clears the selection if everything is
// already selected.
//
// Toggling rather than only selecting matches what the key means in every other
// application, and it is the only way back out without pressing space once per
// message.
func (m *Model) selectAll() {
	if len(m.rowMessages) == 0 {
		return
	}
	if len(m.selected) >= len(m.rowMessages) {
		clear(m.selected)
		return
	}
	for _, id := range m.rowMessages {
		m.selected[id] = true
	}
}
