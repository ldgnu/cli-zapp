package keybindings

// Defaults returns wterm's standard bindings.
//
// The layout follows the i3wm vocabulary the application is designed around:
// "mod" means ctrl, hjkl navigate, and the modifier-free keys stay free for
// scrolling and text entry.
//
// # Why these are data and not code
//
// Every entry here is overridable from configuration. Defaults live in one
// place precisely so that they *can* be replaced wholesale: a user who wants
// emacs navigation supplies a different table rather than patching behaviour
// out of the components.
//
// # Reserved keys
//
// These deliberately have no default binding, because the components need them
// or they are handled outside the binding system:
//
//   - printable runes while the composer has focus: consumed as text
//   - enter while the composer has focus: send the message
//   - esc: handled directly, since it must always abandon the current mode
func Defaults() []Binding {
	return []Binding{
		// Global.
		{
			Action: ActionQuit, Global: true,
			Keys: []Key{key("mod+q")}, Help: "Quit, or close the top overlay",
		},
		{
			Action: ActionCancel, Global: true,
			Keys: []Key{key(KeyEscape)}, Help: "Cancel the current action",
		},
		{
			Action: ActionSearch, Global: true,
			Keys: []Key{key("mod+f")}, Help: "Search chats and messages",
		},
		{
			Action: ActionSync, Global: true,
			Keys: []Key{key("mod+r")}, Help: "Reconnect and resynchronise",
		},
		{
			Action: ActionNewChat, Global: true,
			Keys: []Key{key("mod+n")}, Help: "Start a new chat",
		},
		{
			Action: ActionInfo, Global: true,
			Keys: []Key{key("mod+g")}, Help: "Contact or group information",
		},
		{
			Action: ActionHelp, Global: true,
			Keys: []Key{key("mod+?")}, Help: "Show this help",
		},
		{
			// mod+enter is the primary action, per the i3wm convention where
			// "enter" is bound to the focused container's default action.
			Action: ActionConfirm, Global: true,
			Keys: []Key{key("mod+enter")}, Help: "Primary action for the focused panel",
		},
		{
			Action: ActionSelectAll, Global: true,
			Keys: []Key{key("mod+a")}, Help: "Select all messages",
		},
		{
			// Tab / Shift+Tab switch panels. They are bound globally because a
			// panel switch is meaningful from anywhere, and are also listed as
			// navigation below so the help groups them sensibly.
			Action: PanelNext, Global: true,
			Keys: []Key{key(KeyTab)}, Help: "Focus the next panel",
		},
		{
			Action: PanelPrev, Global: true,
			Keys: []Key{key("shift+" + KeyTab)}, Help: "Focus the previous panel",
		},

		// Sidebar.
		{
			Action: NavUp, Panel: PanelSidebar,
			Keys: []Key{key("mod+k"), key(KeyUp)},
			Help: "Previous chat",
		},
		{
			Action: NavDown, Panel: PanelSidebar,
			Keys: []Key{key("mod+j"), key(KeyDown)},
			Help: "Next chat",
		},
		{
			Action: ChatOpen, Panel: PanelSidebar,
			Keys: []Key{key(KeyEnter)}, Help: "Open the selected chat",
		},
		{
			Action: ActionToggleRead, Panel: PanelSidebar,
			Keys: []Key{key("mod+u")}, Help: "Mark the chat read or unread",
		},
		{
			Action: ActionTogglePin, Panel: PanelSidebar,
			Keys: []Key{key("mod+p")}, Help: "Pin or unpin the chat",
		},
		{
			Action: ActionToggleMute, Panel: PanelSidebar,
			Keys: []Key{key("mod+m")}, Help: "Mute or unmute the chat",
		},
		{
			Action: ActionToggleArchive, Panel: PanelSidebar,
			Keys: []Key{key("mod+e")}, Help: "Archive or unarchive the chat",
		},
		{
			Action: ActionDeleteChat, Panel: PanelSidebar,
			Keys: []Key{key("mod+backspace")}, Help: "Delete the chat",
		},

		// Message list.
		{
			Action: NavUp, Panel: PanelMessages,
			Keys: []Key{key("mod+k"), key(KeyUp)},
			Help: "Previous message",
		},
		{
			Action: NavDown, Panel: PanelMessages,
			Keys: []Key{key("mod+j"), key(KeyDown)},
			Help: "Next message",
		},
		{
			// Panel focus is bound globally rather than per panel. The point of an
			// i3-style hierarchy is that mod+h reaches the sidebar from wherever the
			// user is; scoping it to the message list would leave the key silently
			// doing nothing from the composer, which is where typing usually happens.
			Action: NavLeft, Global: true,
			Keys: []Key{key("mod+h")},
			Help: "Focus the sidebar",
		},
		{
			Action: NavRight, Global: true,
			Keys: []Key{key("mod+l")},
			Help: "Focus the composer",
		},
		{
			// A bare page key scrolls a page, and ctrl+b / alt+pgup is the
			// dedicated "page" action. Both are offered because a user who
			// thinks of PageUp as a page should not have to discover the
			// distinction.
			Action: ScrollUp, Panel: PanelMessages,
			Keys: []Key{key("k"), key("ctrl+u"), key(KeyPageUp), key("shift+" + KeyUp)},
			Help: "Scroll up",
		},
		{
			// ctrl+d is reserved for deleting the selected message, so
			// half-page scrolling uses ctrl+y / ctrl+e, the pair a
			// terminal-emulator user already has in their fingers.
			Action: ScrollDown, Panel: PanelMessages,
			Keys: []Key{key("j"), key("ctrl+e"), key(KeyPageDown), key("shift+" + KeyDown)},
			Help: "Scroll down",
		},
		{
			Action: ScrollTop, Panel: PanelMessages,
			Keys: []Key{key("ctrl+home"), key("g")}, Help: "Jump to the oldest message",
		},
		{
			Action: ScrollBottom, Panel: PanelMessages,
			Keys: []Key{key("ctrl+end"), key("G")}, Help: "Jump to the newest message",
		},
		{
			Action: PageUp, Panel: PanelMessages,
			Keys: []Key{key("ctrl+b"), key("alt+" + KeyPageUp)}, Help: "Scroll up one page",
		},
		{
			Action: PageDown, Panel: PanelMessages,
			Keys: []Key{key("alt+" + KeyPageDown)}, Help: "Scroll down one page",
		},
		{
			Action: ActionReply, Panel: PanelMessages,
			Keys: []Key{key("r")}, Help: "Reply to the selected message",
		},
		{
			Action: ActionEdit, Panel: PanelMessages,
			Keys: []Key{key("mod+e")}, Help: "Edit the selected message",
		},
		{
			Action: ActionDelete, Panel: PanelMessages,
			Keys: []Key{key("mod+d")}, Help: "Delete the selected message",
		},
		{
			Action: ActionReact, Panel: PanelMessages,
			Keys: []Key{key("R")}, Help: "React to the selected message",
		},
		{
			Action: ActionForward, Panel: PanelMessages,
			Keys: []Key{key("mod+shift+f")}, Help: "Forward the selected message",
		},
		{
			Action: ActionCopy, Panel: PanelMessages,
			Keys: []Key{key("mod+c")}, Help: "Copy the selected message",
		},
		{
			Action: ActionSelect, Panel: PanelMessages,
			Keys: []Key{key("space")}, Help: "Select or deselect the message",
		},
		{
			Action: ActionDownload, Panel: PanelMessages,
			Keys: []Key{key("mod+s")}, Help: "Download the message attachment",
		},
		{
			Action: ActionOpen, Panel: PanelMessages,
			Keys: []Key{key("mod+o")}, Help: "Open the attachment externally",
		},

		// Composer.
		{
			// ctrl+n is reserved for starting a new chat, so switching chats from
			// the composer uses alt+arrow, leaving the plain arrows available for
			// moving the caret in multi-line text.
			Action: NavUp, Panel: PanelComposer,
			Keys: []Key{key("alt+" + KeyUp)},
			Help: "Previous chat",
		},
		{
			Action: NavDown, Panel: PanelComposer,
			Keys: []Key{key("alt+" + KeyDown)},
			Help: "Next chat",
		},
		{
			Action: ScrollUp, Panel: PanelComposer,
			Keys: []Key{key("alt+k"), key("alt+" + KeyPageUp)}, Help: "Scroll up",
		},
		{
			Action: ScrollDown, Panel: PanelComposer,
			Keys: []Key{key("alt+j"), key("alt+" + KeyPageDown)}, Help: "Scroll down",
		},
		{
			Action: SendMessage, Panel: PanelComposer,
			Keys: []Key{key(KeyEnter)}, Help: "Send the message",
		},
		{
			Action: InsertNewline, Panel: PanelComposer,
			Keys: []Key{key("alt+enter"), key("shift+enter")}, Help: "Insert a newline",
		},
	}
}

// Navigation and panel actions, defined separately from the application actions
// above because they are structural rather than domain-specific.
const (
	NavUp    Action = "nav.up"
	NavDown  Action = "nav.down"
	NavLeft  Action = "nav.left"
	NavRight Action = "nav.right"

	ScrollUp     Action = "scroll.up"
	ScrollDown   Action = "scroll.down"
	ScrollTop    Action = "scroll.top"
	ScrollBottom Action = "scroll.bottom"
	PageUp       Action = "scroll.page_up"
	PageDown     Action = "scroll.page_down"

	PanelNext Action = "panel.next"
	PanelPrev Action = "panel.prev"

	SendMessage   Action = "composer.send"
	InsertNewline Action = "composer.newline"

	ChatOpen Action = "sidebar.open"
)

// key is a helper for declaring defaults.
//
// Panicking here is deliberate: a malformed default is a programming error
// caught by the package's own tests at startup, never a runtime condition a
// user could trigger.
func key(spec string) Key {
	k, err := ParseKey(spec)
	if err != nil {
		panic("keybindings: invalid default binding " + spec + ": " + err.Error())
	}
	return k
}

// DefaultMap returns the default binding map, validated.
//
// It panics if the defaults conflict, which makes a bad default a failing test
// rather than a broken installation.
func DefaultMap() Map {
	m := NewMap(Defaults())
	if err := m.Check(); err != nil {
		panic(err)
	}
	return m
}
