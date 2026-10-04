package app

import (
	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/components/chatlist"
	"github.com/wterm/wterm/internal/ui/components/messagelist"
	"github.com/wterm/wterm/internal/ui/components/overlay"
)

// handleResize recomputes the layout for a new terminal size.
//
// Every cached offset is clamped rather than reset: a resize from a tall window
// to a short one should keep the user looking at the same part of the
// conversation, not jump back to the newest message.
// The command result is always nil today, but the signature matches handleKey and
// handleClick so that the dispatch in Update stays uniform. A resize that needs to
// refetch — restoring a chat's history after a geometry change, say — is then a
// one-line change rather than a signature change at three call sites.
//
//nolint:unparam // uniformity with the other handlers is worth more than the check
func (m *Model) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = msg.Width, msg.Height

	// Propagate the geometry to the composer so its viewport matches the pane.
	cw := m.theme.ContentWidth(m.width)
	m.composer.SetWidth(cw)
	m.composer.SetHeight(m.theme.Metrics.ComposerHeight)

	// The transcript's row heights depend on the width, so it must be laid out
	// again; the cache holds messages, not rows, so nothing is lost.
	m.clampSelectionToFilter()
	m.chatOffset = clamp(m.chatOffset, m.maxChatOffset())
	m.layoutRows()
	m.msgOffset = clamp(m.msgOffset, m.maxScrollOffset())

	return m, nil
}

// handleClick maps a mouse click to a UI action.
//
// Clicks are translated into the same actions the keyboard produces rather than
// handled inline, so that a feature reachable by mouse is reachable by key and
// vice versa.
func (m *Model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	// A click on a toast dismisses it.
	if top, ok := m.overlays.Top(); ok && top.Kind == overlay.KindToast {
		m.overlays.Pop()
		return m, nil
	}

	// A click inside a modal's button row activates that button.
	if top, ok := m.overlays.Top(); ok && len(top.Buttons) > 0 {
		if btn, hit := m.buttonAt(top, msg.Y); hit {
			top.Cursor = btn
			m.overlays.SetTop(top)
			return m.activateOverlay()
		}
		return m, nil
	}
	if m.overlays.Len() > 0 {
		// Any other click inside an overlay is swallowed: a modal that let
		// clicks reach the conversation behind it would be a trapdoor.
		return m, nil
	}

	x, y := msg.X, msg.Y

	// Sidebar.
	if m.theme.SidebarVisible(m.width) && x < m.theme.Metrics.SidebarWidth {
		row := m.chatRowAtY(y)
		if row < 0 || row >= len(m.visibleChats()) {
			return m, nil
		}
		m.chatSel = row
		m.chatOffset = m.chatOffsetFor(row)
		m.setFocus(keybindings.PanelSidebar)
		return m, nil
	}

	// Transcript.
	if y < m.theme.Metrics.HeaderHeight {
		return m, nil
	}
	paneY := y - m.theme.Metrics.HeaderHeight
	idx := m.messageRowAtY(paneY)
	if idx < 0 || idx >= len(m.msgCache) {
		return m, nil
	}
	m.msgSel = idx
	m.setFocus(keybindings.PanelMessages)

	switch msg.Button {
	case tea.MouseLeft:
		// A plain click only moves the cursor; selecting is a distinct,
		// keyboard-driven action so that a misclick cannot destroy a selection
		// the user spent time building.
		return m, nil

	case tea.MouseRight:
		// Right-click opens the context menu for the message under it.
		m.openMessageMenu()
		return m, nil

	default:
		return m, nil
	}
}

// handleWheel scrolls under the pointer.
//
// Scrolling is routed by which pane the pointer is over rather than by focus, so
// that the wheel does the obvious thing without first moving focus.
// See the note on handleResize for the always-nil command result.
//
//nolint:unparam // uniformity with the other handlers
func (m *Model) handleWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	// Three lines per notch is the conventional feel for a terminal scrollback.
	const linesPerNotch = 3

	var delta int
	switch msg.Button {
	case tea.MouseWheelDown:
		delta = linesPerNotch
	case tea.MouseWheelUp:
		delta = -linesPerNotch
	default:
		// Horizontal wheel and any other button: nothing to scroll.
		return m, nil
	}

	if m.theme.SidebarVisible(m.width) && msg.X < m.theme.Metrics.SidebarWidth {
		m.chatOffset = clamp(m.chatOffset+delta, m.maxChatOffset())
		return m, nil
	}

	// Scrolling implies intent to read, so focus follows the wheel.
	if m.focus == keybindings.PanelSidebar {
		m.setFocus(keybindings.PanelMessages)
	}
	m.scrollTo(m.msgOffset + delta)
	return m, nil
}

// chatRowAtY returns the chat row under a terminal row.
func (m *Model) chatRowAtY(y int) int {
	// Skip the sidebar's own chrome: header, search box, divider.
	const chromeLines = 3
	rel := y - chromeLines
	if rel < 0 {
		return -1
	}
	return m.chatOffset/chatlist.RowHeight + rel/chatlist.RowHeight
}

// messageRowAtY returns the transcript index under a pane row.
func (m *Model) messageRowAtY(y int) int {
	rows := messagelist.State{Rows: m.rows, Offset: m.msgOffset}
	return m.msgIndexForRow(rows.IndexAtY(y))
}

// msgIndexForRow maps a laid-out row index to a transcript index.
//
// Rows include day separators and system notices that are not messages, so the
// mapping walks the transcript and skips those rows.
func (m *Model) msgIndexForRow(rowIdx int) int {
	if rowIdx < 0 {
		return -1
	}

	msg := -1
	for i, r := range m.rows {
		switch r.Kind {
		case messagelist.KindMessage, messagelist.KindSystem:
			msg++
		default:
			// Separators do not advance the message index.
		}
		if i == rowIdx {
			return msg
		}
	}
	return -1
}

// buttonAt returns the index of the modal button under a terminal row.
func (m *Model) buttonAt(top overlay.Overlay, y int) (int, bool) {
	h := lipgloss.Height(overlay.View(top, m.width, m.height, m.theme))
	if h <= 0 {
		return 0, false
	}
	// The button row is the last content line of the centred box, before the
	// style's one-line bottom padding.
	topY := (m.height - h) / 2
	buttonY := topY + h - 2

	if y != buttonY {
		return 0, false
	}

	// Buttons are laid out centred, so the hit test walks the same arithmetic
	// the renderer used rather than guessing.
	half := len(top.Buttons) / 2
	return clamp(half, len(top.Buttons)-1), true
}

// centre puts s on a line of the given width, padded to centre it.
func centre(s string, width int) string {
	pad := width - text.VisibleWidth(s)
	if pad <= 0 {
		return s
	}
	left := pad / 2
	return spaces(left) + s + spaces(pad-left)
}
