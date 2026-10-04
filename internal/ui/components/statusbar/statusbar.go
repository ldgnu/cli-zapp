// Package statusbar renders the bottom status line.
//
// The status bar is wterm's answer to a real usability problem: a keyboard-driven
// client is unusable unless the bindings are discoverable. The bar shows the
// current context, the connection state and the keys that work right now, so the
// user never has to remember a table they cannot see.
package statusbar

import (
	"strings"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/theme"
)

// Connection describes the account's link state, as shown in the bar.
type Connection int

// Recognised connection states.
const (
	// Disconnected means no session. Nothing can be sent or received.
	Disconnected Connection = iota
	// Connecting means a session is being established.
	Connecting
	// Syncing means connected, with history still arriving.
	Syncing
	// Online means connected and up to date.
	Online
)

// String implements fmt.Stringer.
func (c Connection) String() string {
	switch c {
	case Connecting:
		return "connecting"
	case Syncing:
		return "syncing"
	case Online:
		return "online"
	case Disconnected:
		return "offline"
	default:
		return "unknown"
	}
}

// State is everything the status bar needs in order to render.
type State struct {
	// Connection is the account's link state.
	Connection Connection
	// AccountName identifies the linked account, for example a phone number.
	AccountName string

	// Focus names the focused panel.
	Focus keybindings.Panel
	// FocusVisible reports whether the focused panel is the sidebar, which
	// determines whether the chat list or the transcript gets the hints.
	ChatName string

	// ChatCount and UnreadTotal drive the chat-list section.
	ChatCount   int
	UnreadTotal int

	// ComposerHint is contextual help for the current input state, such as
	// "enter to send".
	ComposerHint string

	// TypingIn names the chat whose peer is composing, empty when nobody is.
	TypingIn string

	// Width is the terminal width in cells.
	Width int

	// Help supplies the context-sensitive key hints.
	Help []keybindings.HelpEntry

	// Theme supplies colours and glyphs.
	Theme theme.Theme
}

// maxHints bounds how many key hints fit, so that the right-hand side degrades
// gracefully on a narrow terminal instead of overflowing and being clipped
// mid-escape-sequence.
const maxHints = 6

// View renders the status bar.
func View(s State) string {
	if s.Width <= 0 {
		return ""
	}
	st := s.Theme.Styles

	left := renderLeft(s, st)

	// Drop hints from the right until the two halves fit. This is a loop rather
	// than a calculation because hint widths vary with the labels involved.
	hints := s.Help
	for {
		gap := s.Width - text.VisibleWidth(left) - text.VisibleWidth(renderRight(hints, st))
		if gap >= 1 || len(hints) == 0 {
			break
		}
		hints = hints[:len(hints)-1]
	}

	right := renderRight(hints, st)

	gap := s.Width - text.VisibleWidth(left) - text.VisibleWidth(right)
	if gap < 1 {
		// The hints did not fit after trimming; prioritise the context and let
		// the bar be short rather than overflowing.
		return pad(left, s.Width)
	}

	return text.PadRight(left+strings.Repeat(" ", gap)+right, s.Width)
}

// pad is a local alias keeping the layout arithmetic above readable.
func pad(s string, width int) string { return text.PadRight(s, width) }

// renderLeft builds the context section: connection, focus and chat name.
func renderLeft(s State, st theme.Styles) string {
	var parts []string

	conn := st.StatusLabel.Render(s.Connection.String())
	switch s.Connection {
	case Online:
		conn = st.Success.Render(s.Theme.Glyphs.OK + " " + s.Connection.String())
	case Connecting, Syncing:
		conn = st.Warning.Render(s.Connection.String() + "…")
	case Disconnected:
		conn = st.Error.Render(s.Connection.String())
	}
	parts = append(parts, conn)

	if s.ChatName != "" {
		parts = append(parts, st.StatusBar.Render(text.Truncate(s.ChatName, 24)))
	}

	if s.TypingIn != "" {
		parts = append(parts, st.Accent.Render("typing…"))
	}

	if s.ChatCount > 0 {
		badge := text.Truncate("chats", 24)
		if s.UnreadTotal > 0 {
			badge = text.Truncate("chats", 24) + " " + st.ChatBadge.Render(text.Itoa(s.UnreadTotal))
		}
		parts = append(parts, st.StatusLabel.Render(badge))
	}

	return strings.Join(parts, st.StatusLabel.Render(" · "))
}

// renderRight builds the key hints.
//
// Hints are rendered as "key label" pairs joined by spaces, which reads more
// quietly than a bulleted list and fits the single-line budget.
func renderRight(hints []keybindings.HelpEntry, st theme.Styles) string {
	if len(hints) > maxHints {
		hints = hints[:maxHints]
	}

	var parts []string
	for _, h := range hints {
		if len(h.Keys) == 0 {
			continue
		}
		parts = append(parts,
			st.StatusKey.Render(h.Keys[0])+" "+st.StatusLabel.Render(h.Help))
	}
	return strings.Join(parts, st.StatusLabel.Render("  "))
}
