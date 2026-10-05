package adapter

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/cli-zapp/cli-zapp/internal/models"
	"github.com/cli-zapp/cli-zapp/internal/whatsapp"
)

// core is the state every service shares: the caches, the lock and the event channel.
//
// # Why it is not one type per interface
//
// The interfaces in [whatsapp] collide on names — ChatService, GroupService and
// ContactService each have List and Get, and ChatService.Delete takes a chat while
// MessageService.Delete takes a message. Go cannot put two methods of the same name on
// one type, so a single Services type cannot satisfy the set.
//
// Splitting into six thin types over a shared core is what makes it work, and it is
// also why the bundle in [whatsapp.Services] has named fields rather than being one
// interface. The alternative — renaming the interface methods — would push a Go
// naming constraint into the application's contract.
//
// The translation lives here and nowhere else. The UI receives [models] types and has
// no way to ask what a JID is; when WhatsApp changes how it identifies something, the
// change is confined to this package.
type core struct {
	client *Client

	// mu guards the caches below.
	//
	// The event handler is called from whatsmeow's read loop while a command may be
	// reading the same caches, so this is not optional. -race found it: the caches
	// were plain maps for the first version, and a presence update arriving while a
	// chat list was being rendered was reported as a concurrent map read and write.
	mu sync.RWMutex

	// chats is the conversation list, keyed by JID string.
	chats map[string]*models.Chat
	// order preserves the sort the UI asked for; a map cannot be ranged in order and
	// the sidebar's ordering is its whole point.
	order []string
	// senders maps a JID to the display name to use when rendering one.
	senders map[string]string
	// messages is the transcript cache, keyed by chat and kept in arrival order.
	messages map[models.ChatID][]models.Message
	// rawMessages holds the protocol messages behind the cache, because downloading an
	// attachment is authenticated by keys that live in the message itself and cannot be
	// reconstructed from the descriptor.
	rawMessages map[models.ChatID]map[string]whatsmeow.DownloadableMessage

	// handler is whatsmeow's event handler id, so the handler can be detached on Stop.
	handler uint32
	// closeOnce guards closing the events channel exactly once.
	closeOnce sync.Once

	// events is the single stream the UI drains.
	//
	// One channel rather than one per event kind, and that is deliberate: presence,
	// receipts and messages arriving on separate channels interleave in whatever order
	// the library produces them, and the UI folds a message and a presence update into
	// one model consistently only if it sees them in one order.
	events chan whatsapp.Event

	// connected tracks the link, so the status bar does not have to infer it.
	connected bool
	// bannedUntil is set when the account is temporarily banned, and is what makes the
	// UI show a countdown instead of a spinner.
	bannedUntil time.Time
	// banReason explains a ban in the user's terms.
	banReason string
}

// newCore builds the shared state. The service bundle is assembled over it by
// NewServices, in serviceset.go.
func newCore(c *Client) *core {
	return &core{
		client:      c,
		chats:       make(map[string]*models.Chat),
		senders:     make(map[string]string),
		messages:    make(map[models.ChatID][]models.Message),
		rawMessages: make(map[models.ChatID]map[string]whatsmeow.DownloadableMessage),
		events:      make(chan whatsapp.Event, 256),
	}
}

// AttachHandler registers the event handler with whatsmeow.
//
// It returns the handler id, which is what RemoveEventHandler takes. Losing it would
// make the handler impossible to detach, so it is returned rather than discarded.
func (s *core) AttachHandler() uint32 {
	return s.client.Raw().AddEventHandler(s.onEvent)
}

// onEvent folds one protocol event into the model.
//
// This is the seam where the protocol stops. Everything above receives
// [whatsapp.Event]; nothing above this line imports whatsmeow.
func (s *core) onEvent(rawEvt any) {
	switch evt := rawEvt.(type) {
	case *events.Connected:
		s.setConnected(true)

	case *events.Disconnected:
		s.setConnected(false)

	case *events.LoggedOut:
		// The phone forgot this device. Every subsequent command will fail in a way
		// the user cannot act on, so the UI is told to return to pairing.
		s.setConnected(false)
		s.emit(whatsapp.Event{Kind: whatsapp.EventUnpaired, Reason: connectReason(evt.Reason)})

	case *events.TemporaryBan:
		// Rendered as a countdown, deliberately.
		//
		// The alternative is a spinner, and a spinner is what makes a user assume the
		// program has hung and restart it — which turns a temporary ban into a
		// permanent one. whatsmeow hands us the expiry; using it is the difference
		// between "come back in 20 minutes" and "my account is gone".
		s.bannedUntil = time.Now().Add(evt.Expire)
		s.banReason = banReason(evt.Code)
		s.setConnected(false)
		s.emit(whatsapp.Event{Kind: whatsapp.EventBanned, Until: evt.Expire, Reason: banReason(evt.Code)})

	case *events.Message:
		msg := convertMessage(evt.Info, evt.Message)
		s.indexSender(evt.Info.Sender)
		s.cacheRaw(evt.Info, evt.Message)
		s.cacheMessage(msg)
		s.emit(whatsapp.Event{Kind: whatsapp.EventMessage, Message: msg})

	case *events.Presence:
		s.mu.Lock()
		s.senders[evt.From.String()] = "set"
		s.mu.Unlock()

	case *events.ChatPresence:
		s.emit(whatsapp.Event{
			Kind:   whatsapp.EventTyping,
			ChatID: models.ChatID(evt.Chat.String()),
			Typing: evt.State == types.ChatPresenceComposing,
		})

	case *events.Receipt:
		// A receipt is an update to a message, not a separate event: the interface has
		// one message kind, and the fake reports a status change the same way. Making it
		// a distinct kind here would mean the UI grew a path the fake cannot exercise.
		//
		// Read subsumes delivered in WhatsApp's model — a read receipt is also a
		// delivery receipt — so a read also reports delivered. Reporting them
		// independently would let the transcript move a message back from read to
		// delivered, which is the kind of bug that only shows up on a slow connection.
		if len(evt.MessageIDs) == 0 {
			break
		}
		s.applyReceipt(models.ChatID(evt.Chat.String()),
			models.MessageID(evt.MessageIDs[0]),
			evt.Type == types.ReceiptTypeDelivered || evt.Type == types.ReceiptTypeRead,
			evt.Type == types.ReceiptTypeRead)

	case *events.PairSuccess:
		s.setConnected(true)
		s.emit(whatsapp.Event{Kind: whatsapp.EventPaired})

	case *events.PairError:
		s.emit(whatsapp.Event{
			Kind: whatsapp.EventPairFailed,
			Err:  fmt.Errorf("pairing rejected: %w", evt.Error),
		})

	case *events.GroupInfo:
		// Name arrives as a typed optional, and only when it changed. A nil here is
		// the common case and means "leave the name alone", not "the group has no
		// name" — conflating the two would blank every group on any unrelated
		// participant change.
		if evt.Name != nil {
			s.chatFlag(evt.JID, func(c *models.Chat) { c.Name = evt.Name.Name })
		}

	case *events.Archive:
		s.chatFlag(evt.JID, func(c *models.Chat) {
			c.Archived = evt.Action != nil && evt.Action.Archived != nil && *evt.Action.Archived
		})

	case *events.Mute:
		s.chatFlag(evt.JID, func(c *models.Chat) {
			c.Muted = evt.Action != nil && evt.Action.Muted != nil && *evt.Action.Muted
		})

	case *events.Pin:
		s.chatFlag(evt.JID, func(c *models.Chat) {
			c.Pinned = evt.Action != nil && evt.Action.Pinned != nil && *evt.Action.Pinned
		})

	case *events.StreamReplaced:
		// Another client connected with the same keys. That is a real operational
		// situation — usually the same session started twice — and it looks identical
		// to being logged out unless it is named.
		s.setConnected(false)
		s.emit(whatsapp.Event{
			Kind:   whatsapp.EventReplaced,
			Reason: "otra sesión se conectó con esta misma cuenta",
		})
	}
}

// cacheRaw keeps the protocol message so an attachment can be downloaded later.
func (s *core) cacheRaw(info types.MessageInfo, raw *waE2E.Message) {
	// Only media is kept, and only as the narrow interface DownloadToFile accepts.
	// Holding every message twice would double the cache for no benefit, and the
	// download is the only thing that needs the protocol shape.
	dl, ok := downloadableFrom(raw)
	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	chat := models.ChatID(info.Chat.String())
	if s.rawMessages[chat] == nil {
		s.rawMessages[chat] = make(map[string]whatsmeow.DownloadableMessage)
	}
	s.rawMessages[chat][info.ID] = dl
}

// cacheMessage appends a message to its chat's transcript.
//
// Appended rather than keyed, because History is served newest-last and the sidebar's
// preview needs the most recent without a scan. Bounded per chat, because an account
// with years of history would otherwise grow this without limit; the protocol's own
// cache is the durable copy and this is a display window.
func (s *core) cacheMessage(m models.Message) {
	const perChat = 500

	s.mu.Lock()
	defer s.mu.Unlock()

	list := append(s.messages[m.ChatID], m)
	if len(list) > perChat {
		list = list[len(list)-perChat:]
	}
	s.messages[m.ChatID] = list

	if chat, ok := s.chats[string(m.ChatID)]; ok {
		chat.Timestamp = m.Timestamp
		chat.LastMessage = &models.MessagePreview{
			Kind:       m.Kind,
			Body:       m.Body,
			IsOutgoing: m.Direction == models.DirectionOutgoing,
			Media:      m.Media,
		}
	}
}

// emit hands an event to the UI.
//
// The channel is buffered, and a full one drops the event rather than blocking. A
// blocked emit would stop draining the protocol's read loop, and the consequence would
// be the library closing the connection — so a slow UI would disconnect the account.
// Losing one presence update is recoverable; losing the session is not.
func (s *core) emit(e whatsapp.Event) {
	select {
	case s.events <- e:
	default:
	}
}

// indexSender remembers a display name so a quote has something to show.
func (s *core) indexSender(jid types.JID) {
	if jid.User == "" || jid.Server == types.BroadcastServer {
		return
	}
	name := jid.User
	if s.client.device != nil && jid.User == s.client.device.ID.User {
		return // ourselves: quoting yourself needs no name
	}
	s.mu.Lock()
	if _, ok := s.senders[jid.String()]; !ok {
		s.senders[jid.String()] = name
	}
	s.mu.Unlock()
}

func (s *core) setConnected(v bool) {
	s.mu.Lock()
	s.connected = v
	if v {
		s.bannedUntil = time.Time{}
		s.banReason = ""
	}
	s.mu.Unlock()
}

// chatFlag applies a mutation to one chat and tells the UI about it.
//
// The notification is not optional. The sidebar renders from this cache, so mutating it
// without emitting leaves the row showing the old flag until something else happens to
// redraw — and for a pin, nothing else changes.
func (s *core) chatFlag(jid types.JID, f func(*models.Chat)) {
	s.mu.Lock()
	_, known := s.chats[jid.String()]
	if known {
		f(s.chats[jid.String()])
	}
	s.mu.Unlock()

	s.emit(whatsapp.Event{Kind: whatsapp.EventChatUpdate, ChatID: models.ChatID(jid.String())})
	if !known {
		s.emit(whatsapp.Event{
			Kind:   whatsapp.EventError,
			ChatID: models.ChatID(jid.String()),
			Err:    fmt.Errorf("flag for a chat not in the cache: %w", ErrUnpaired),
		})
	}
}

// ErrUnpaired means the account is no longer linked and the UI must return to pairing.
//
// Distinct from a generic failure because the recovery differs: retrying is pointless,
// and advising someone to "try again" for something that needs a new QR code costs an
// hour of their time.
var ErrUnpaired = errors.New("not paired")

// Connected reports whether the account is linked and online.
func (s *core) Connected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

// Banned reports the temporary-ban expiry, and whether one is in force.
func (s *core) Banned() (time.Time, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.bannedUntil.IsZero() || time.Now().After(s.bannedUntil) {
		return time.Time{}, "", false
	}
	return s.bannedUntil, s.banReason, true
}

// DisplayName returns the best name for a peer.
func (s *core) DisplayName(jid types.JID) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if name, ok := s.senders[jid.String()]; ok && name != "" {
		return name
	}
	return jid.User
}

// resolveJID parses a [models.ChatID] back into a protocol JID.
//
// It returns an error rather than a zero JID, because a zero JID silently addresses
// nothing: the call appears to work and the message disappears, which is the worst
// possible failure for a message send.
func resolveJID(id models.ChatID) (types.JID, error) {
	jid, err := types.ParseJID(string(id))
	if err != nil {
		return types.JID{}, fmt.Errorf("bad chat id %q: %w", id, err)
	}
	return jid, nil
}

// trimJIDSuffix shortens a JID for display: "1234@s.whatsapp.net" to "1234".
func trimJIDSuffix(s string) string {
	if i := strings.IndexByte(s, '@'); i > 0 {
		return s[:i]
	}
	return s
}

// ErrUnsupported is returned by capabilities the protocol cannot provide.
//
// It exists so that a caller can distinguish "this cannot be done" from "this failed":
// a feature that is impossible should be reported as impossible rather than as an
// error the user retries forever.
var ErrUnsupported = errors.New("unsupported by the WhatsApp protocol")

// applyReceipt updates the cached status of one message and re-emits it.
//
// The cache is updated first so that a transcript rendered between the update and the
// redraw is not stale, and the message is emitted afterwards so the UI redraws.
func (s *core) applyReceipt(chat models.ChatID, id models.MessageID, delivered, read bool) {
	s.mu.Lock()
	list := s.messages[chat]
	updated := models.Message{}
	found := false
	for i := range list {
		if list[i].ID != id {
			continue
		}
		list[i].Status = receiptStatus(list[i].Status, delivered, read)
		updated = list[i]
		found = true
		break
	}
	s.mu.Unlock()

	if !found {
		// A receipt for a message not held in the cache. Not an error: the transcript
		// window is bounded, so a receipt for something older is simply nothing to
		// update, and reporting it as a failure would fill the log with noise.
		return
	}
	s.emit(whatsapp.Event{Kind: whatsapp.EventMessage, ChatID: chat, Message: updated})
}

// receiptStatus advances a message's delivery status.
//
// Statuses only ever move forward. A delivered receipt arriving after a read receipt
// must not demote the message, which is what a plain overwrite would do — and out-of-
// order receipts are normal, because they arrive over one connection that a reconnect
// can replay from the start of the stream.
func receiptStatus(current models.DeliveryStatus, delivered, read bool) models.DeliveryStatus {
	if read && current < models.DeliveryRead {
		return models.DeliveryRead
	}
	if delivered && current < models.DeliveryDelivered {
		return models.DeliveryDelivered
	}
	return current
}

// mustBeConnected returns the error to report when the account is not linked.
func (s *core) mustBeConnected() error {
	if !s.Connected() {
		return ErrNotPaired
	}
	return nil
}

// selfJID returns this account's own JID as a string, or "" when not paired.
func (s *core) selfJID() string {
	if jid := s.client.JID(); jid.User != "" {
		return jid.String()
	}
	if id := s.client.Raw().Store.ID; id != nil {
		return id.String()
	}
	return ""
}
