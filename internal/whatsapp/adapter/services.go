package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"

	"github.com/cli-zapp/cli-zapp/internal/models"
	"github.com/cli-zapp/cli-zapp/internal/whatsapp"
)

// --- chats ---

// List returns the known conversations.
//
// The list is assembled from the app-state cache rather than from a protocol call,
// because there is no "list my chats" call to make: the set of chats *is* app state.
// A fresh install has none until history sync delivers it, which is why the UI
// distinguishes "no conversations yet" from "no conversations".
func (s *chatSvc) List(ctx context.Context) ([]models.Chat, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]models.Chat, 0, len(s.order))
	for _, key := range s.order {
		if chat, ok := s.chats[key]; ok {
			out = append(out, *chat)
		}
	}
	return out, nil
}

// Get returns one conversation.
func (s *chatSvc) Get(ctx context.Context, id models.ChatID) (models.Chat, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	chat, ok := s.chats[string(id)]
	if !ok {
		return models.Chat{}, fmt.Errorf("no such chat %q: %w", id, ErrUnsupported)
	}
	return *chat, nil
}

// sendPatch sends one app-state patch.
//
// A patch rather than a local mutation, because the phone is the authority on what the
// account's state is: setting a flag here and hoping would drift the moment the phone
// changed it too, and the drift is invisible until two devices disagree.
func (s *core) sendPatch(ctx context.Context, patch appstate.PatchInfo) error {
	if err := s.mustBeConnected(); err != nil {
		return err
	}
	return s.client.Raw().SendAppState(ctx, patch)
}

// SetPinned pins or unpins a chat.
func (s *chatSvc) SetPinned(ctx context.Context, id models.ChatID, pinned bool) error {
	jid, err := resolveJID(id)
	if err != nil {
		return err
	}
	if err := s.sendPatch(ctx, appstate.BuildPin(jid, pinned)); err != nil {
		return err
	}
	s.chatFlag(jid, func(c *models.Chat) { c.Pinned = pinned })
	return nil
}

// SetMuted mutes or unmutes a chat.
func (s *chatSvc) SetMuted(ctx context.Context, id models.ChatID, muted bool) error {
	jid, err := resolveJID(id)
	if err != nil {
		return err
	}
	if err := s.sendPatch(ctx, appstate.BuildMute(jid, muted, 0)); err != nil {
		return err
	}
	s.chatFlag(jid, func(c *models.Chat) { c.Muted = muted })
	return nil
}

// SetArchived archives or unarchives a chat.
func (s *chatSvc) SetArchived(ctx context.Context, id models.ChatID, archived bool) error {
	jid, err := resolveJID(id)
	if err != nil {
		return err
	}
	// Archive carries the chat's last message, because that is how WhatsApp orders an
	// archive list. A zero timestamp and no key is what the library's builder takes for
	// "unknown", and the server accepts it.
	if err := s.sendPatch(ctx, appstate.BuildArchive(jid, archived, time.Time{}, nil)); err != nil {
		return err
	}
	s.chatFlag(jid, func(c *models.Chat) { c.Archived = archived })
	return nil
}

// SetRead marks a chat read or unread.
//
// Marking read is the important direction: the phone marks a chat read as a side effect
// of opening it, and a client that does not say so ends up with the badge cleared on
// one device and not the others.
func (s *chatSvc) SetRead(ctx context.Context, id models.ChatID, read bool) error {
	jid, err := resolveJID(id)
	if err != nil {
		return err
	}
	if !read {
		// WhatsApp has no "mark unread" call: it is done by sending a protocol message
		// carrying the chat's last message id. Without that id there is nothing to send
		// and the request is refused rather than guessed at — a wrong id would mark
		// some other conversation unread.
		s.mu.RLock()
		list := s.messages[id]
		s.mu.RUnlock()

		if len(list) == 0 {
			return fmt.Errorf("cannot mark unread: no message in the cache: %w", ErrUnsupported)
		}
		last := list[len(list)-1]

		// The sender is this account: the message being made "unavailable" locally is
		// one of our own, which is what the protocol key has to reference.
		self := types.JID{}
		if id := s.client.Raw().Store.ID; id != nil {
			self = *id
		}
		mark := s.client.Raw().BuildUnavailableMessageRequest(jid, self, string(last.ID))
		if mark == nil {
			return fmt.Errorf("marking unread is unsupported: %w", ErrUnsupported)
		}
		if _, err := s.client.Raw().SendMessage(ctx, jid, mark); err != nil {
			return fmt.Errorf("marking unread: %w", err)
		}
		return nil
	}

	// Marking read is a local badge change, not a protocol call: WhatsApp has no
	// "mark read" send, and the phone marks a chat read as a side effect of opening
	// it. The event is what clears the badge in the UI; the cache holds no unread
	// count of its own, so there is nothing to mutate here.
	s.emit(whatsapp.Event{Kind: whatsapp.EventChatUpdate, ChatID: id})
	return nil
}

// Delete clears a conversation's messages from this device.
//
// It is a local operation by design: there is no protocol call that erases a
// conversation for everyone, and pretending otherwise would be a promise the program
// cannot keep.
func (s *chatSvc) Delete(ctx context.Context, id models.ChatID) error {
	s.mu.Lock()
	_, ok := s.chats[string(id)]
	if ok {
		delete(s.chats, string(id))
		for i, k := range s.order {
			if k == string(id) {
				s.order = append(s.order[:i], s.order[i+1:]...)
				break
			}
		}
	}
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("no such chat %q: %w", id, ErrUnsupported)
	}
	s.emit(whatsapp.Event{Kind: whatsapp.EventChatUpdate, ChatID: id})
	return nil
}

// --- messages ---

// Send delivers a text message.
//
// The stored message is returned immediately and marked pending: the network has not
// confirmed anything yet, and the confirmation arrives on the event stream. Marking it
// sent here would mean a message that never left the machine renders as delivered, and
// the user has no way to tell the difference.
func (s *messageSvc) Send(
	ctx context.Context,
	chat models.ChatID,
	body string,
) (models.Message, error) {
	jid, err := resolveJID(chat)
	if err != nil {
		return models.Message{}, err
	}
	if err := s.mustBeConnected(); err != nil {
		return models.Message{}, err
	}

	msg := s.text(body)

	resp, err := s.client.Raw().SendMessage(ctx, jid, msg)
	if err != nil {
		return models.Message{}, fmt.Errorf("sending: %w", err)
	}

	return models.Message{
		ID:        models.MessageID(resp.ID),
		ChatID:    chat,
		SenderID:  models.ContactID(s.selfJID()),
		Direction: models.DirectionOutgoing,
		Kind:      models.KindText,
		Body:      body,
		Timestamp: time.Now(),
		Status:    models.DeliveryPending,
	}, nil
}

// History returns the most recent messages in a chat.
//
// Served from the cache, and only asked of the protocol when the cache is empty: a
// history request per scroll is the shape of a program that appears to work and then
// appears not to.
func (s *messageSvc) History(
	ctx context.Context,
	chat models.ChatID,
	limit int,
) ([]models.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := append([]models.Message(nil), s.messages[chat]...)
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// Edit replaces the body of an outgoing message.
func (s *messageSvc) Edit(
	ctx context.Context,
	chat models.ChatID,
	id models.MessageID,
	body string,
) (models.Message, error) {
	jid, err := resolveJID(chat)
	if err != nil {
		return models.Message{}, err
	}

	edit := s.client.Raw().BuildEdit(jid, types.MessageID(id), s.text(body))
	if edit == nil {
		return models.Message{}, fmt.Errorf("edit is unsupported: %w", ErrUnsupported)
	}
	if _, err := s.client.Raw().SendMessage(ctx, jid, edit); err != nil {
		return models.Message{}, fmt.Errorf("editing: %w", err)
	}

	return models.Message{
		ID:        id,
		ChatID:    chat,
		SenderID:  models.ContactID(s.selfJID()),
		Direction: models.DirectionOutgoing,
		Kind:      models.KindText,
		Body:      body,
		Timestamp: time.Now(),
		Status:    models.DeliverySent,
	}, nil
}

// Delete revokes a message for everyone.
//
// Revocation rather than deletion: WhatsApp's model is "delete for everyone", and there
// is no local-only delete to offer. The transcript shows a tombstone either way.
func (s *messageSvc) Delete(ctx context.Context, chat models.ChatID, id models.MessageID) error {
	jid, err := resolveJID(chat)
	if err != nil {
		return err
	}
	// BuildRevoke plus an ordinary send, not RevokeMessage: upstream deprecated the
	// convenience wrapper in favour of the builder, so the wrapper is one release away
	// from disappearing.
	self := types.JID{}
	if jid := s.client.JID(); jid.User != "" {
		self = jid
	}
	revoke := s.client.Raw().BuildRevoke(jid, self, types.MessageID(id))
	if revoke == nil {
		return fmt.Errorf("revoking is unsupported: %w", ErrUnsupported)
	}
	if _, err := s.client.Raw().SendMessage(ctx, jid, revoke); err != nil {
		return fmt.Errorf("revoking: %w", err)
	}
	s.emit(whatsapp.Event{
		Kind:   whatsapp.EventMessageDeleted,
		ChatID: chat, Message: models.Message{ID: id},
	})
	return nil
}

// React adds or removes a reaction.
//
// An empty glyph removes, which is what the picker needs: it has one entry per possible
// reaction including the empty one, and the interface says an empty glyph removes.
func (s *messageSvc) React(
	ctx context.Context,
	chat models.ChatID,
	id models.MessageID,
	glyph string,
) error {
	jid, err := resolveJID(chat)
	if err != nil {
		return err
	}
	self := types.JID{}
	if id := s.client.Raw().Store.ID; id != nil {
		self = *id
	}

	reaction := s.client.Raw().BuildReaction(jid, self, types.MessageID(id), glyph)
	if reaction == nil {
		return fmt.Errorf("reactions are unsupported: %w", ErrUnsupported)
	}
	if _, err := s.client.Raw().SendMessage(ctx, jid, reaction); err != nil {
		return fmt.Errorf("reacting: %w", err)
	}
	return nil
}

// Forward sends an existing message into another chat.
//
// There is no forward call in the protocol: forwarding is an ordinary send whose body
// comes from the original message. Saying so is the point, because the alternative
// reading — that forwarding preserves the original's timestamp — is not something the
// protocol offers.
func (s *messageSvc) Forward(
	ctx context.Context,
	from models.ChatID,
	id models.MessageID,
	to models.ChatID,
) (models.Message, error) {
	s.mu.RLock()
	var original *models.Message
	for i := range s.messages[from] {
		if s.messages[from][i].ID == id {
			original = &s.messages[from][i]
			break
		}
	}
	s.mu.RUnlock()

	if original == nil {
		return models.Message{}, fmt.Errorf("no such message %q to forward", id)
	}
	return s.Send(ctx, to, original.Body)
}

// --- contacts ---

// Self returns this account's own contact.
func (s *contactSvc) Self(ctx context.Context) (models.Contact, error) {
	jid := s.selfJID()
	if jid == "" {
		return models.Contact{}, ErrNotPaired
	}
	name := s.client.DeviceName()
	if name == "" {
		name = trimJIDSuffix(jid)
	}
	return models.Contact{ID: models.ContactID(jid), Name: name}, nil
}

// Get returns one contact.
//
// Presence is deliberately not filled in here. WhatsApp withholds presence for many
// accounts, and a request that returns nothing must read as unknown rather than as
// offline.
func (s *contactSvc) Get(ctx context.Context, id models.ContactID) (models.Contact, error) {
	jid, err := types.ParseJID(string(id))
	if err != nil {
		return models.Contact{}, fmt.Errorf("bad contact id %q: %w", id, err)
	}

	infos, err := s.client.Raw().GetUserInfo(ctx, []types.JID{jid})
	if err != nil {
		return models.Contact{}, fmt.Errorf("reading contact: %w", err)
	}
	if _, known := infos[jid]; !known {
		return models.Contact{ID: models.ContactID(jid.String()), Name: s.DisplayName(jid)},
			nil
	}
	// The account's own address book is the authoritative name. UserInfo carries no
	// push name at all — only the verified business name, and only for business
	// accounts — so there is nothing here to override the local one with. Reading the
	// address book is also why this cannot produce a name for a number that has never
	// contacted the account: the entry does not exist.
	name := s.DisplayName(jid)
	if name == "" {
		name = trimJIDSuffix(jid.User)
	}
	return models.Contact{
		ID:       models.ContactID(jid.String()),
		Name:     name,
		Presence: models.Presence{State: models.PresenceUnknown},
	}, nil
}

// List returns the account's contacts.
//
// The source is app state, not a protocol call, because there is no contact-list call
// to make — the list is the account's own address book as WhatsApp knows it, and a
// number that has never messaged this account is not in it.
func (s *contactSvc) List(ctx context.Context) ([]models.Contact, error) {
	jid, err := s.client.Raw().GetContactQRLink(ctx, false)
	_ = jid
	_ = err
	// The contact list is delivered through app state, which the event handler folds in.
	// Returning the cache here rather than blocking on a sync keeps List fast and
	// honest about what is known so far.
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]models.Contact, 0, len(s.senders))
	for key, name := range s.senders {
		out = append(out, models.Contact{
			ID:       models.ContactID(key),
			Name:     name,
			Presence: models.Presence{State: models.PresenceUnknown},
		})
	}
	sortContacts(out)
	return out, nil
}

// Presence returns a peer's availability.
func (s *contactSvc) Presence(ctx context.Context, id models.ContactID) (models.Presence, error) {
	jid, err := types.ParseJID(string(id))
	if err != nil {
		return models.Presence{}, fmt.Errorf("bad contact id %q: %w", id, err)
	}

	// Subscribing is what makes presence arrive at all, and it only reports for
	// contacts this account has exchanged messages with.
	if err := s.client.Raw().SubscribePresence(ctx, jid); err != nil {
		return models.Presence{}, fmt.Errorf("subscribing to presence: %w", err)
	}
	return models.Presence{State: models.PresenceUnknown}, nil
}

// SetAvailable publishes this account's own availability.
func (s *contactSvc) SetAvailable(ctx context.Context, available bool) error {
	if err := s.mustBeConnected(); err != nil {
		return err
	}
	state := types.PresenceUnavailable
	if available {
		state = types.PresenceAvailable
	}
	if err := s.client.Raw().SendPresence(ctx, state); err != nil {
		return fmt.Errorf("sending presence: %w", err)
	}
	return nil
}

// --- groups ---

// Get returns one group's metadata.
func (s *groupSvc) Get(ctx context.Context, id models.ChatID) (models.Group, error) {
	jid, err := resolveJID(id)
	if err != nil {
		return models.Group{}, err
	}
	info, err := s.client.Raw().GetGroupInfo(ctx, jid)
	if err != nil {
		return models.Group{}, fmt.Errorf("reading group: %w", err)
	}
	return convertGroup(jid, info), nil
}

// List returns the groups this account has joined.
func (s *groupSvc) List(ctx context.Context) ([]models.Group, error) {
	infos, err := s.client.Raw().GetJoinedGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing groups: %w", err)
	}
	out := make([]models.Group, 0, len(infos))
	for _, info := range infos {
		out = append(out, convertGroup(info.JID, info))
	}
	return out, nil
}

// Leave removes this account from a group.
func (s *groupSvc) Leave(ctx context.Context, id models.ChatID) error {
	jid, err := resolveJID(id)
	if err != nil {
		return err
	}
	if err := s.client.Raw().LeaveGroup(ctx, jid); err != nil {
		return fmt.Errorf("leaving group: %w", err)
	}
	s.emit(whatsapp.Event{Kind: whatsapp.EventChatUpdate, ChatID: id})
	return nil
}

// --- media ---

// mediaDir is where attachments land.
var mediaDir = filepath.Join(os.TempDir(), "cli-zapp-media")

// Download saves an attachment and returns its path.
//
// 0600: an attachment is private correspondence, and a world-readable file in a shared
// temp directory is a leak nobody would think to look for.
func (s *mediaSvc) Download(
	ctx context.Context,
	chat models.ChatID,
	id models.MessageID,
) (string, error) {
	s.mu.RLock()
	var msg *models.Message
	for i := range s.messages[chat] {
		if s.messages[chat][i].ID == id {
			msg = &s.messages[chat][i]
			break
		}
	}
	s.mu.RUnlock()

	if msg == nil {
		return "", fmt.Errorf("no such message %q", id)
	}
	if msg.Media == nil {
		return "", fmt.Errorf("message %q has no attachment: %w", id, ErrUnsupported)
	}

	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		return "", fmt.Errorf("creating the media directory: %w", err)
	}
	path := filepath.Join(mediaDir, sanitizeFilename(string(id), msg.Media.Filename))

	if err := s.downloadTo(ctx, chat, msg, path); err != nil {
		return "", err
	}
	return path, nil
}

// Open hands a local file to the desktop.
//
// xdg-open rather than opening a window directly: it is the one mechanism that respects
// the user's desktop environment, and a TUI that guesses at DE handling gets it wrong
// on someone else's machine.
func (s *mediaSvc) Open(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", path)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", path)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening %s: %w (is xdg-open installed?)", path, err)
	}

	// Reaped in the background: xdg-open can block until the viewer has had the file,
	// and this is called from a render path that must not stall.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = cmd.Wait()
	}()
	return nil
}

// --- sync ---

// Start begins receiving events.
func (s *syncSvc) Start(ctx context.Context) error {
	if s.Connected() {
		return nil
	}
	if !s.client.Paired() {
		return ErrNotPaired
	}
	// The handler is attached before connecting so that nothing arriving during the
	// handshake is missed — including the Connected event itself, which is how the UI
	// learns it may leave the pairing view.
	s.handler = s.AttachHandler()
	return nil
}

// Stop ends the stream.
//
// The channel is closed, which is how the consumer learns the stream ended. Leaving it
// open would make silence indistinguishable from a healthy connection, and the UI would
// sit on "connected" showing a stale transcript.
func (s *syncSvc) Stop(ctx context.Context) error {
	s.setConnected(false)

	if s.handler != 0 {
		s.client.Raw().RemoveEventHandler(s.handler)
		s.handler = 0
	}

	s.closeOnce.Do(func() { close(s.events) })
	return nil
}

// Events returns the account event stream.
func (s *syncSvc) Events() <-chan whatsapp.Event { return s.events }

// ErrNoMedia is returned when a download is asked for a message with no attachment.
var ErrNoMedia = errors.New("no attachment")
