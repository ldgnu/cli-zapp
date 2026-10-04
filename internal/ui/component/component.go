// Package component defines the contract every cli-zapp UI region follows.
//
// # Model / Update / View per region
//
// Bubble Tea's pattern is normally applied once, at the root. Applying it to each
// region as well is what keeps the components reusable: a sidebar that owns its own
// Update can be dropped into another application without that application
// reimplementing sidebar key handling.
//
// A region is:
//
//   - a Model: plain state, no behaviour, comparable where it should be
//   - an Update: receives messages, returns a new value and a command
//   - a View: renders into a rectangle it is handed
//
// # Regions emit events, they do not mutate siblings
//
// A region never reaches into another region. It returns a [Event] as a command
// result, and the root decides what it means. This is the whole of the
// architecture: the transcript cannot mark a chat read, and the sidebar cannot
// scroll the transcript, because neither has a reference to the other.
//
// # The WhatsApp boundary
//
// Regions receive [models] types and emit [Event] values. Neither mentions a
// protocol. A region that needed to know whether a JID was a group would be a
// region that had learned the protocol, which is precisely what must not happen.
package component

import (
	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/models"
	"github.com/cli-zapp/cli-zapp/internal/ui/layout"
)

// Region is a focusable, self-contained part of the interface.
//
// Regions are values. Update returns a modified copy, which is what lets the root
// model stay a plain value that Bubble Tea can snapshot, and what makes a region
// testable by driving its Update directly with no program and no terminal.
type Region interface {
	// Name identifies the region, for status text and diagnostics.
	Name() string

	// Resize gives the region its drawing rectangle.
	//
	// Called on every terminal resize and before every render, because the root
	// owns the layout and is the only thing that knows it. Regions never compute
	// their own geometry: two regions guessing at widths is how a sidebar and a
	// transcript end up disagreeing about where the divider is.
	Resize(layout.Rect)

	// Update handles a message and returns the region unchanged plus a command.
	Update(tea.Msg) (Region, tea.Cmd)

	// View renders the region. The returned string must occupy exactly the
	// rectangle given to Resize.
	View() string

	// Focus gives the region keyboard focus.
	Focus() tea.Cmd

	// Blur removes keyboard focus.
	Blur()

	// Focused reports whether the region currently has focus.
	Focused() bool
}

// Event is something a region asks the application to do.
//
// Events are the only channel between a region and the rest of the program. They
// describe *intent* — "the user chose this chat" — never "set this field", so the
// root remains free to reject an event it disagrees with and to own every
// consequence.
type Event struct {
	// Kind identifies the event.
	Kind Kind

	// ChatID is set for chat-scoped events.
	ChatID models.ChatID
	// MessageID is set for message-scoped events.
	MessageID models.MessageID
	// Query is set for search events.
	Query string
	// Command is set for palette events: the action the user chose.
	Command Command
	// Delta is set for scroll events.
	Delta int
	// Text carries free-form payloads: the composer's contents, a confirmation's
	// answer.
	Text string
	// Payload carries a message or a chat when the event is a request for one.
	Payload any
}

// Kind identifies an [Event].
type Kind int

// Recognised event kinds.
const (
	// KindNone is the zero value, meaning "nothing happened".
	KindNone Kind = iota

	// KindFocusChat asks the application to open a chat.
	KindFocusChat
	// KindFocusRegion asks the application to move keyboard focus.
	KindFocusRegion
	// KindSearchChanged reports a new search query.
	KindSearchChanged
	// KindSearchDismissed reports that search mode ended.
	KindSearchDismissed
	// KindSubmit reports that the composer's contents should be sent.
	KindSubmit
	// KindInsertNewline reports that the composer wants a newline rather than a
	// send.
	KindInsertNewline
	// KindCancel reports that the current interaction was abandoned.
	KindCancel
	// KindScroll requests transcript scrolling.
	KindScroll
	// KindScrollTo requests a jump to the newest or oldest message.
	KindScrollTo
	// KindMessageSelected reports a cursor move over the transcript.
	KindMessageSelected
	// KindMessageActivated reports that the cursor message should be acted on:
	// replied to, edited, deleted, reacted to.
	KindMessageActivated
	// KindSelectionToggled reports a multi-selection change.
	KindSelectionToggled
	// KindCommandChosen reports a command chosen from the palette.
	KindCommandChosen
	// KindDismissOverlay reports that an overlay should close.
	KindDismissOverlay
	// KindSubmitOverlay reports that an overlay's primary action was chosen.
	KindSubmitOverlay
	// KindQuit asks the application to exit.
	KindQuit
)

// EventCmd wraps an event as a Bubble Tea command.
//
// Every region's "I want this to happen" path goes through here, so that an event
// travels the same route whether it came from a key press, a mouse click or a
// timer.
func EventCmd(e Event) tea.Cmd {
	if e.Kind == KindNone {
		return nil
	}
	return func() tea.Msg { return e }
}

// Is reports whether the event matches a kind.
func (e Event) Is(k Kind) bool { return e.Kind == k }

// String implements fmt.Stringer, for logs and test failure messages.
func (e Event) String() string {
	if e.Kind == KindNone {
		return "none"
	}
	name := map[Kind]string{
		KindFocusChat:        "focus-chat",
		KindFocusRegion:      "focus-region",
		KindSearchChanged:    "search-changed",
		KindSearchDismissed:  "search-dismissed",
		KindSubmit:           "submit",
		KindInsertNewline:    "insert-newline",
		KindCancel:           "cancel",
		KindScroll:           "scroll",
		KindScrollTo:         "scroll-to",
		KindMessageSelected:  "message-selected",
		KindMessageActivated: "message-activated",
		KindSelectionToggled: "selection-toggled",
		KindCommandChosen:    "command-chosen",
		KindDismissOverlay:   "dismiss-overlay",
		KindSubmitOverlay:    "submit-overlay",
		KindQuit:             "quit",
	}[e.Kind]
	if name == "" {
		return "unknown"
	}
	return name
}

// RegionRef identifies a region by name, for focus events.
type RegionRef = string

// Recognised region names.
const (
	RegionSidebar    RegionRef = "sidebar"
	RegionTranscript RegionRef = "transcript"
	RegionComposer   RegionRef = "composer"
)

// Command is an application action the palette can offer.
//
// # The shape
//
//	ID          stable identifier, used in configuration and in tests
//	Title       the short label shown in the list
//	Description one sentence, shown for the highlighted command
//	Shortcut    the keys, shown right-aligned
//	Category    the heading it appears under
//	Available   whether it can run right now
//	Danger      whether it destroys something
//	Execute     what it does
//
// # Who runs it, and who does not
//
// Execute is a closure the application builds and the palette never calls. The palette
// filters a list of these and emits the chosen one; the application runs it.
//
// That split is the whole reason the component can be reused and the behaviour cannot
// leak into it. A palette that called Execute itself would need the application's state
// to pass in, which means it is no longer a component but a second copy of the
// application. Keeping the call on the application's side is also what lets a command
// be added in one place — see [Command.Execute] for why that matters.
//
// # Available is a bool, not a predicate
//
// It could be a func() bool evaluated at render time, and was. The problem is that the
// table is a value that the palette holds, so a closure would capture the state at the
// moment the table was built and then go stale — "Responder" would stay greyed after the
// user selected a message. Rebuilding the table whenever the state it depends on changes
// is one line at one place and cannot go stale, because there is nothing to capture.
//
// The same applies to Execute, and for the same reason: it is not a stored closure over
// an old state, it is a fresh one each time the table is rebuilt.
type Command struct {
	// ID is the stable identifier used in configuration and in tests.
	ID string
	// Title is the short label shown in the list.
	Title string
	// Description is one sentence about what the command does, shown for whichever
	// command is highlighted.
	//
	// It exists because the list is scannable by title but not always informative:
	// "Archivar" and "Marcar como no leída" are both clear, while "Sincronizar" is
	// three words that do not say it reconnects and refetches.
	Description string
	// Shortcut is the keyboard shortcut, shown right-aligned.
	Shortcut string
	// Category is the heading the command appears under.
	Category string
	// Danger marks a destructive command, so the palette can render it differently
	// from the reversible ones beside it.
	Danger bool
	// Available reports whether the command can run right now.
	//
	// A command that cannot run is shown greyed rather than hidden, so the user can
	// see that it exists. That is the whole reason this is a field rather than a filter
	// applied when the table is built: "Responder" with no message selected should look
	// unavailable, not missing.
	Available bool
	// Execute runs the command. The application sets it; the palette never calls it.
	//
	// A nil Execute means the command is declared but not implemented, which the
	// application reports rather than ignoring: a palette entry that looks right and
	// does nothing is the failure mode this whole design exists to make visible.
	Execute func() tea.Cmd
}

// Model is the shared state a region needs in order to render.
//
// It is passed by value on every render, so a region cannot mutate the
// application's state by accident.
type Model struct {
	// Chats is the conversation list, already sorted by the application.
	Chats []models.Chat
	// CurrentChatID is the open conversation.
	CurrentChatID models.ChatID
	// Messages is the open conversation's transcript, oldest first.
	Messages []models.Message
	// Presence is the open contact's availability.
	Presence models.Presence
	// TypingIn names the chat whose peer is composing, empty when nobody is.
	TypingIn string
	// UnreadTotal is the number of unread messages across all chats.
	UnreadTotal int
	// Selected marks the message identifiers in a multi-selection.
	Selected map[models.MessageID]bool
	// Search is the current search query.
	Search string
	// SearchActive reports whether the search field has focus.
	SearchActive bool
	// Hints are the key hints the sidebar's footer offers for the selected row.
	//
	// They arrive as data rather than being composed from the region's own key
	// knowledge, for the same reason every other binding does: a component that knew
	// what ctrl+n meant would have to be edited when the binding changed, and would
	// disagree with the help sheet the moment someone rebounded it.
	Hints []keybindings.HelpEntry
}
