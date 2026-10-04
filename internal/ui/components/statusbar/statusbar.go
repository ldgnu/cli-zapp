// Package statusbar renders the bottom bar: connection state, unread count,
// typing notice and the context-sensitive key hints.
//
// # Why the hints live here
//
// A keyboard-driven client is unusable unless its bindings are discoverable, and a
// help overlay that must be opened to answer "what does ctrl+k do" is one keystroke
// too many. The bar shows the keys that work right now, so the interface teaches
// itself while it is being used.
//
// # What is dropped first
//
// Context on the left, hints on the right. When the terminal is too narrow for
// both, the hints go: they are the more expendable of the two, because the same
// keys are also in the help overlay and in the README.
package statusbar

import (
	"strings"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/text"
	"github.com/cli-zapp/cli-zapp/internal/ui/layout"
	"github.com/cli-zapp/cli-zapp/internal/ui/theme"
)

// Connection is the account's link state.
type Connection int

// Recognised connection states.
const (
	// Offline means no session: nothing can be sent or received.
	Offline Connection = iota
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
		return "conectando"
	case Syncing:
		return "sincronizando"
	case Online:
		return "en línea"
	case Offline:
		return "sin conexión"
	default:
		return "desconocido"
	}
}

// Model is the status bar's state.
type Model struct {
	rect  layout.Rect
	theme theme.Theme

	// mode decides what the bar is willing to show. It is consulted at render time
	// rather than acted on when set, because the application calls SetState on every
	// frame and SetMode only when the terminal is resized: a mode applied by clearing
	// a field would be undone by the very next frame.
	mode layout.Mode

	connection Connection
	chatName   string
	unread     int
	typing     string
	account    string
	hints      []keybindings.HelpEntry
}

// New creates a status bar.
func New(t theme.Theme) *Model { return &Model{theme: t} }

// Resize gives the bar its rectangle.
func (m *Model) Resize(r layout.Rect) { m.rect = r }

// SetMode applies the layout mode: minimal drops the hints entirely.
func (m *Model) SetMode(mode layout.Mode) { m.mode = mode }

// SetState supplies everything the bar shows.
func (m *Model) SetState(s State) {
	m.connection = s.Connection
	m.chatName = s.ChatName
	m.unread = s.Unread
	m.typing = s.Typing
	m.account = s.Account
	if s.Hints != nil {
		m.hints = s.Hints
	}
}

// State is the data the bar renders.
type State struct {
	Connection Connection
	ChatName   string
	Unread     int
	Typing     string
	Account    string
	Hints      []keybindings.HelpEntry
}

// View renders the bar into exactly its rectangle.
func (m *Model) View() string {
	if m.rect.Empty() {
		return ""
	}
	w := m.rect.Width
	st := m.theme.Styles
	g := m.theme.Glyphs

	left := m.renderLeft(st, g)

	// Drop hints from the right until both halves fit. The context is never dropped: a
	// user who cannot see whether they are connected cannot tell a silent client from a
	// working one.
	//
	// The count is a local rather than a truncation of m.hints, because View runs far
	// more often than SetState: shrinking the stored slice here would make the bar
	// permanently lose its hints after the user widened the terminal once and narrowed
	// it back.
	shown := m.hints
	right := m.renderHints(st, g, shown)
	for right != "" && text.VisibleWidth(left)+text.VisibleWidth(right)+1 > w {
		shown = shown[:len(shown)-1]
		right = m.renderHints(st, g, shown)
	}

	if text.VisibleWidth(left) > w {
		left = text.TruncateStyled(left, w)
	}

	gap := w - text.VisibleWidth(left) - text.VisibleWidth(right)
	if gap < 1 {
		if text.VisibleWidth(left) >= w {
			return text.TruncateStyled(left, w)
		}
		return left + strings.Repeat(" ", w-text.VisibleWidth(left))
	}
	return left + strings.Repeat(" ", gap) + right
}

// renderLeft builds the context: connection, chat, typing and unread count.
func (m *Model) renderLeft(st theme.Styles, g theme.Glyphs) string {
	parts := make([]string, 0, 5)

	switch m.connection {
	case Online:
		parts = append(parts, st.Success.Render(g.Online+" en línea"))
	case Connecting, Syncing:
		parts = append(parts, st.Warning.Render(m.connection.String()+"…"))
	case Offline:
		parts = append(parts, st.Error.Render(g.Offline+" "+Offline.String()))
	}

	if m.chatName != "" {
		parts = append(parts, st.ChatTitle.Render(text.Truncate(m.chatName, 28)))
	}

	if m.typing != "" {
		parts = append(parts, st.Accent.Render(g.Typing+" escribiendo…"))
	}

	if m.unread > 0 {
		parts = append(parts,
			st.ChatBadge.Render(text.Itoa(m.unread))+" "+st.Muted.Render("sin leer"))
	}

	return strings.Join(parts, st.Muted.Render(" · "))
}

// renderHints builds the right-hand key hints.
//
// They are rendered as "[key] label" pairs, which reads quietly and survives a
// narrow terminal by being trimmed from the right rather than wrapped.
func (m *Model) renderHints(st theme.Styles, g theme.Glyphs, hints []keybindings.HelpEntry) string {
	// Hints are dropped in minimal mode. They are in the help sheet and in the README,
	// so nothing is lost but the rows, which the transcript needs more.
	if !m.mode.ShowsKeyHints() || len(m.hints) == 0 {
		return ""
	}

	// Four is what fits beside the context at a hundred columns; the bar's trimming
	// loop removes the rest on narrower terminals.
	const maxHints = 4
	if len(hints) > maxHints {
		hints = hints[:maxHints]
	}

	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		if len(h.Keys) == 0 {
			continue
		}

		// The short label, not the sentence.
		//
		// A bar that shares one line with the connection state has room for about
		// three labels, and the sentences are twenty-odd cells each. Truncating them
		// produces "Cancelar la ac…", which tells the user less than the key alone does,
		// so the short form is declared beside the sentence instead of derived from it.
		label := h.Short
		if label == "" {
			label = h.Help
		}

		key := st.StatusKey.Render(g.KeyHintOpen + h.Keys[0] + g.KeyHintClose)
		parts = append(parts, key+" "+st.StatusLabel.Render(label))
	}
	return strings.Join(parts, "  ")
}
