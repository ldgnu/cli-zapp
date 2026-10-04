// Package whatsapp declares the service interfaces the application depends on,
// plus the in-memory fake used before the protocol adapter exists.
//
// # The boundary
//
// These interfaces are the entire surface the UI is allowed to see. They are
// expressed in terms of [github.com/wterm/wterm/internal/models] types, never in
// terms of protocol types, which is what guarantees that a WhatsApp protocol
// change is absorbed by one package rather than rippling through the app.
//
// # Why the fake lives here
//
// [Fake] is in the same package as the interfaces rather than in a testing
// subpackage so that tests, and the Phase 1 binary, can both use it. That is a
// deliberate trade: a mock is useful to exactly one consumer — the code under
// test — so keeping it beside the contract it implements makes the contract
// harder to get wrong.
package whatsapp

import (
	"context"

	"github.com/wterm/wterm/internal/models"
)

// ChatService reads and modifies conversations.
type ChatService interface {
	// List returns every known chat, in no particular order.
	List(ctx context.Context) ([]models.Chat, error)

	// Get returns a single chat.
	Get(ctx context.Context, id models.ChatID) (models.Chat, error)

	// SetPinned pins or unpins a chat.
	SetPinned(ctx context.Context, id models.ChatID, pinned bool) error

	// SetMuted mutes or unmutes a chat.
	SetMuted(ctx context.Context, id models.ChatID, muted bool) error

	// SetArchived archives or unarchives a chat.
	SetArchived(ctx context.Context, id models.ChatID, archived bool) error

	// SetRead marks a chat read or unread.
	//
	// The distinction from unread count matters: marking read must clear the
	// badge, whereas marking unread must set it, and neither is the other's
	// inverse once history has been fetched.
	SetRead(ctx context.Context, id models.ChatID, read bool) error

	// Delete removes a chat from the local list.
	Delete(ctx context.Context, id models.ChatID) error
}

// MessageService reads and sends messages.
type MessageService interface {
	// Send delivers a text message.
	//
	// It returns the stored message immediately, before the network confirms
	// it, so that the UI can show it as pending. Delivery status arrives later
	// through [SyncService.Events].
	Send(ctx context.Context, chat models.ChatID, body string) (models.Message, error)

	// History returns messages for a chat, oldest first.
	History(ctx context.Context, chat models.ChatID, limit int) ([]models.Message, error)

	// Edit replaces the body of a previously sent message.
	Edit(
		ctx context.Context,
		chat models.ChatID,
		id models.MessageID,
		body string,
	) (models.Message, error)

	// Delete revokes a message for everyone.
	Delete(ctx context.Context, chat models.ChatID, id models.MessageID) error

	// React adds or removes a reaction. An empty glyph removes the local
	// user's existing reaction.
	React(ctx context.Context, chat models.ChatID, id models.MessageID, glyph string) error

	// Forward sends a message to another chat.
	Forward(
		ctx context.Context,
		from models.ChatID,
		id models.MessageID,
		to models.ChatID,
	) (models.Message, error)
}

// ContactService reads contact and presence information.
type ContactService interface {
	// Self returns the logged-in account.
	Self(ctx context.Context) (models.Contact, error)

	// Get returns a contact.
	Get(ctx context.Context, id models.ContactID) (models.Contact, error)

	// List returns every known contact.
	List(ctx context.Context) ([]models.Contact, error)

	// Presence returns the current availability of a contact.
	Presence(ctx context.Context, id models.ContactID) (models.Presence, error)

	// SetAvailable publishes the logged-in user's own presence.
	SetAvailable(ctx context.Context, available bool) error
}

// GroupService reads group metadata.
type GroupService interface {
	// Get returns a group's metadata and roster.
	Get(ctx context.Context, id models.ChatID) (models.Group, error)

	// List returns every joined group.
	List(ctx context.Context) ([]models.Group, error)

	// Leave removes the logged-in user from a group.
	Leave(ctx context.Context, id models.ChatID) error
}

// MediaService downloads attachments and hands them to external programs.
type MediaService interface {
	// Download fetches a message's attachment to the configured directory and
	// returns the path written.
	Download(ctx context.Context, chat models.ChatID, id models.MessageID) (string, error)

	// Open hands a local file to the user's configured viewer.
	Open(ctx context.Context, path string) error
}

// SyncService streams account-level changes.
type SyncService interface {
	// Start begins receiving events. It returns immediately; events arrive on
	// the channel returned by [SyncService.Events].
	Start(ctx context.Context) error

	// Stop ends the stream and closes the event channel.
	Stop(ctx context.Context) error

	// Events returns the stream of account events.
	//
	// The channel is closed on [SyncService.Stop], which is how the consumer
	// learns the stream ended rather than treating silence as a healthy
	// connection.
	Events() <-chan Event

	// Connected reports whether the session is live.
	Connected() bool
}

// Event is an account-level change.
//
// It is a tagged union rather than separate channels per event type: a single
// ordered stream avoids the interleaving bugs that appear when, say, a presence
// update races a message on two channels.
type Event struct {
	// Kind identifies the payload.
	Kind EventKind

	// ChatID is set for chat-scoped events.
	ChatID models.ChatID

	// Message is set for [EventMessage].
	Message models.Message

	// Presence is set for [EventPresence].
	Presence models.Presence

	// Typing is set for [EventTyping]; Typing is false when the peer stopped.
	Typing bool

	// Connection is set for [EventConnection].
	Connection ConnectionState

	// Err is set for [EventError].
	Err error
}

// EventKind identifies an [Event]'s payload.
type EventKind int

// Recognised event kinds.
const (
	EventUnknown EventKind = iota
	// EventMessage carries a new or updated message.
	EventMessage
	// EventMessageDeleted carries the ID of a revoked message.
	EventMessageDeleted
	// EventReaction carries an updated reaction set.
	EventReaction
	// EventPresence carries availability.
	EventPresence
	// EventTyping carries composing state.
	EventTyping
	// EventChatUpdate carries changed chat metadata such as pin or mute.
	EventChatUpdate
	// EventConnection carries the session state.
	EventConnection
	// EventError carries a non-fatal failure.
	EventError
)

// ConnectionState is the session's link state.
type ConnectionState int

// Recognised connection states.
const (
	ConnectionOffline ConnectionState = iota
	ConnectionConnecting
	ConnectionSyncing
	ConnectionOnline
)

// String implements fmt.Stringer.
func (c ConnectionState) String() string {
	switch c {
	case ConnectionConnecting:
		return "connecting"
	case ConnectionSyncing:
		return "syncing"
	case ConnectionOnline:
		return "online"
	case ConnectionOffline:
		return "offline"
	default:
		return "unknown"
	}
}

// Services bundles every dependency the UI needs.
//
// It is passed as one value rather than as six parameters so that adding a
// service later does not change every call site, and so that a test can supply
// a struct of fakes in a single expression.
type Services struct {
	Chat    ChatService
	Message MessageService
	Contact ContactService
	Group   GroupService
	Media   MediaService
	Sync    SyncService
}

// Self returns the logged-in account.
//
// A nil Contact service or a lookup failure yields a zero contact, so the UI
// renders an unlinked state rather than panicking. Losing the account name in a
// status bar is a far better failure mode than a crash at startup.
func (s Services) Self() models.Contact {
	if s.Contact == nil {
		return models.Contact{}
	}
	c, err := s.Contact.Self(context.Background())
	if err != nil {
		return models.Contact{}
	}
	return c
}
