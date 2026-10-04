package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wterm/wterm/internal/models"
)

// Errors returned by [Fake].
var (
	// ErrNotFound is returned when an identifier does not exist.
	ErrNotFound = errors.New("whatsapp: not found")
	// ErrNotEditable is returned when editing a message the account does not own.
	ErrNotEditable = errors.New("whatsapp: message cannot be edited")
	// ErrNoAttachment is returned when downloading a message with no media.
	ErrNoAttachment = errors.New("whatsapp: message has no attachment")
)

// Fake is an in-memory implementation of every service interface.
//
// It exists so that the whole UI can be built and tested before the protocol
// adapter exists, and so that component tests need no network. Every method is
// safe for concurrent use, because a sync goroutine writes while the UI reads.
//
// # Not a mock
//
// [Fake] keeps real state and enforces real invariants: a send appends to the
// transcript, updates the chat's preview and increments the unread count for
// incoming messages. A hand-written mock returning canned values would let the
// UI be built against behaviour the real adapter never produces, which is the
// failure mode this type exists to avoid.
//
// # Six views of one store
//
// Several services need differently-shaped methods of the same name — Get for a
// chat, a contact and a group; Delete for a chat and a message; List for a chat,
// a contact and a group. Go cannot overload, so [Fake] exposes one view per
// interface, each an embedding shim that shadows only the conflicting methods.
// The receiver stays a single [Fake] and the state stays single-sourced.
type Fake struct {
	mu sync.RWMutex

	self      models.Contact
	contacts  map[models.ContactID]models.Contact
	chatOrder []models.ChatID
	chats     map[models.ChatID]models.Chat
	messages  map[models.ChatID][]models.Message

	nextID int

	conn        ConnectionState
	events      chan Event
	started     bool
	stopped     bool
	downloadDir string
}

// Compile-time proof that the six views between them satisfy every interface.
// If a signature changes, this block stops compiling.
var (
	_ ChatService    = fakeChat{}
	_ MessageService = fakeMessage{}
	_ ContactService = fakeContact{}
	_ GroupService   = fakeGroup{}
	_ MediaService   = fakeMedia{}
	_ SyncService    = (*Fake)(nil)
)

// The shims embed *Fake and shadow only the methods whose signature conflicts.

// fakeChat provides the chat-scoped Get, List and Delete.
type fakeChat struct{ *Fake }

// fakeMessage provides the message-scoped Delete.
type fakeMessage struct{ *Fake }

// fakeContact provides the contact-scoped Get and List.
type fakeContact struct{ *Fake }

// fakeGroup provides the group-scoped Get and List.
type fakeGroup struct{ *Fake }

// fakeMedia names do not conflict, so the shim only exists for symmetry.
type fakeMedia struct{ *Fake }

// NewFake builds a fake with the given account.
func NewFake(self models.Contact) *Fake {
	if self.ID == "" {
		self.ID = "self"
	}
	self.IsSelf = true
	return &Fake{
		self:        self,
		contacts:    map[models.ContactID]models.Contact{self.ID: self},
		chats:       make(map[models.ChatID]models.Chat),
		messages:    make(map[models.ChatID][]models.Message),
		events:      make(chan Event, 64),
		conn:        ConnectionOffline,
		downloadDir: filepath.Join(os.TempDir(), "wterm-fake-media"),
	}
}

// Services bundles the fake as a full [Services] value.
func (f *Fake) Services() Services {
	return Services{
		Chat:    fakeChat{f},
		Message: fakeMessage{f},
		Contact: fakeContact{f},
		Group:   fakeGroup{f},
		Media:   fakeMedia{f},
		Sync:    f,
	}
}

// SetDownloadDir changes where Download writes files.
func (f *Fake) SetDownloadDir(dir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloadDir = dir
}

// --- ChatService ---

func (f fakeChat) List(ctx context.Context) ([]models.Chat, error) { return f.listChats(ctx) }

func (f fakeChat) Get(ctx context.Context, id models.ChatID) (models.Chat, error) {
	return f.getChat(ctx, id)
}

func (f fakeChat) SetPinned(ctx context.Context, id models.ChatID, v bool) error {
	return f.mutateChat(id, func(c *models.Chat) { c.Pinned = v })
}

func (f fakeChat) SetMuted(ctx context.Context, id models.ChatID, v bool) error {
	return f.mutateChat(id, func(c *models.Chat) { c.Muted = v })
}

func (f fakeChat) SetArchived(ctx context.Context, id models.ChatID, v bool) error {
	return f.mutateChat(id, func(c *models.Chat) { c.Archived = v })
}

func (f fakeChat) SetRead(ctx context.Context, id models.ChatID, read bool) error {
	return f.mutateChat(id, func(c *models.Chat) { f.setRead(c, read) })
}

func (f fakeChat) Delete(ctx context.Context, id models.ChatID) error {
	return f.deleteChat(ctx, id)
}

// --- MessageService ---

func (f fakeMessage) Send(
	ctx context.Context, chat models.ChatID, body string,
) (models.Message, error) {
	return f.send(ctx, chat, body)
}

func (f fakeMessage) History(
	ctx context.Context, chat models.ChatID, limit int,
) ([]models.Message, error) {
	return f.history(ctx, chat, limit)
}

func (f fakeMessage) Edit(
	ctx context.Context, chat models.ChatID, id models.MessageID, body string,
) (models.Message, error) {
	return f.edit(ctx, chat, id, body)
}

func (f fakeMessage) Delete(ctx context.Context, chat models.ChatID, id models.MessageID) error {
	return f.deleteMessage(ctx, chat, id)
}

func (f fakeMessage) React(
	ctx context.Context, chat models.ChatID, id models.MessageID, glyph string,
) error {
	return f.react(ctx, chat, id, glyph)
}

func (f fakeMessage) Forward(
	ctx context.Context, from models.ChatID, id models.MessageID, to models.ChatID,
) (models.Message, error) {
	return f.forward(ctx, from, id, to)
}

// --- ContactService ---

func (f fakeContact) Self(ctx context.Context) (models.Contact, error) {
	return f.selfContact(ctx)
}

func (f fakeContact) Get(ctx context.Context, id models.ContactID) (models.Contact, error) {
	return f.getContact(ctx, id)
}

func (f fakeContact) List(ctx context.Context) ([]models.Contact, error) {
	return f.listContacts(ctx)
}

func (f fakeContact) Presence(ctx context.Context, id models.ContactID) (models.Presence, error) {
	return f.getPresence(ctx, id)
}

func (f fakeContact) SetAvailable(ctx context.Context, available bool) error {
	return f.setAvailable(ctx, available)
}

// --- GroupService ---

func (f fakeGroup) Get(ctx context.Context, id models.ChatID) (models.Group, error) {
	return f.getGroup(ctx, id)
}
func (f fakeGroup) List(ctx context.Context) ([]models.Group, error) { return f.listGroups(ctx) }
func (f fakeGroup) Leave(ctx context.Context, id models.ChatID) error {
	return f.deleteChat(ctx, id)
}

// --- MediaService ---

func (f fakeMedia) Download(
	ctx context.Context, chat models.ChatID, id models.MessageID,
) (string, error) {
	return f.download(ctx, chat, id)
}
func (f fakeMedia) Open(ctx context.Context, path string) error { return f.open(ctx, path) }

// --- SyncService ---

// Start begins the event stream.
//
// It emits connecting then online synchronously, so that a caller which starts
// the sync before the UI is ready still observes the ordered transitions.
func (f *Fake) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.started {
		return nil
	}
	f.started = true
	f.stopped = false

	f.conn = ConnectionConnecting
	f.emitLocked(Event{Kind: EventConnection, Connection: ConnectionConnecting})

	f.conn = ConnectionOnline
	f.emitLocked(Event{Kind: EventConnection, Connection: ConnectionOnline})
	return nil
}

// Stop ends the stream.
//
// The event channel is closed, which is how a consumer learns the stream ended
// rather than treating silence as a healthy connection. A fresh channel replaces
// it so that a subsequent Start is usable.
func (f *Fake) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.started || f.stopped {
		return nil
	}
	f.stopped = true
	f.conn = ConnectionOffline
	close(f.events)
	f.events = make(chan Event, 64)
	return nil
}

// Connected reports whether the session is live.
func (f *Fake) Connected() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.conn == ConnectionOnline
}

// Events returns the stream of account events.
func (f *Fake) Events() <-chan Event {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.events
}

// --- State helpers, all requiring the appropriate lock ---

func (f *Fake) listChats(context.Context) ([]models.Chat, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]models.Chat, 0, len(f.chatOrder))
	for _, id := range f.chatOrder {
		if c, ok := f.chats[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *Fake) getChat(_ context.Context, id models.ChatID) (models.Chat, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	c, ok := f.chats[id]
	if !ok {
		return models.Chat{}, fmt.Errorf("%w: chat %s", ErrNotFound, id)
	}
	return c, nil
}

// setRead applies the read/unread transition. The caller must hold the lock.
func (f *Fake) setRead(c *models.Chat, read bool) {
	if read {
		c.UnreadCount = 0
		c.HasUnread = false
		return
	}
	// Marking unread must produce a badge. One is used rather than a computed
	// count because the protocol cannot always say how many are unseen when
	// history has not been fetched.
	c.UnreadCount = max(c.UnreadCount, 1)
	c.HasUnread = true
}

func (f *Fake) mutateChat(id models.ChatID, fn func(*models.Chat)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.chats[id]
	if !ok {
		return fmt.Errorf("%w: chat %s", ErrNotFound, id)
	}
	fn(&c)
	f.chats[id] = c
	return nil
}

func (f *Fake) deleteChat(_ context.Context, id models.ChatID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.chats[id]; !ok {
		return fmt.Errorf("%w: chat %s", ErrNotFound, id)
	}
	delete(f.chats, id)
	delete(f.messages, id)
	for i, oid := range f.chatOrder {
		if oid == id {
			f.chatOrder = append(f.chatOrder[:i], f.chatOrder[i+1:]...)
			break
		}
	}
	return nil
}

func (f *Fake) selfContact(context.Context) (models.Contact, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.self, nil
}

func (f *Fake) getContact(_ context.Context, id models.ContactID) (models.Contact, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	c, ok := f.contacts[id]
	if !ok {
		return models.Contact{}, fmt.Errorf("%w: contact %s", ErrNotFound, id)
	}
	return c, nil
}

func (f *Fake) listContacts(context.Context) ([]models.Contact, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]models.Contact, 0, len(f.contacts))
	for _, c := range f.contacts {
		out = append(out, c)
	}
	return out, nil
}

func (f *Fake) getPresence(_ context.Context, id models.ContactID) (models.Presence, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	c, ok := f.contacts[id]
	if !ok {
		return models.Presence{}, fmt.Errorf("%w: contact %s", ErrNotFound, id)
	}
	return c.Presence, nil
}

func (f *Fake) setAvailable(_ context.Context, available bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := models.PresenceUnavailable
	if available {
		state = models.PresenceAvailable
	}
	self := f.self
	self.Presence = models.Presence{ContactID: self.ID, State: state}
	f.self = self
	f.contacts[self.ID] = self
	return nil
}

func (f *Fake) getGroup(_ context.Context, id models.ChatID) (models.Group, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	c, ok := f.chats[id]
	if !ok {
		return models.Group{}, fmt.Errorf("%w: group %s", ErrNotFound, id)
	}
	return models.Group{ID: id, Name: c.Name, AvatarURL: c.AvatarURL}, nil
}

func (f *Fake) listGroups(context.Context) ([]models.Group, error) {
	chats, err := f.listChats(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]models.Group, 0, len(chats))
	for _, c := range chats {
		if !c.Type.IsGroup() {
			continue
		}
		g, err := f.getGroup(context.Background(), c.ID)
		if err != nil {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (f *Fake) download(
	_ context.Context, chatID models.ChatID, id models.MessageID,
) (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	m := f.findMessageLocked(chatID, id)
	if m.ID == "" {
		return "", fmt.Errorf("%w: message %s", ErrNotFound, id)
	}
	if m.Media == nil {
		return "", ErrNoAttachment
	}

	name := m.Media.Filename
	if name == "" {
		name = m.ID.String()
	}
	path := filepath.Join(f.downloadDir, name)

	if err := os.MkdirAll(f.downloadDir, 0o700); err != nil {
		return "", fmt.Errorf("creating download directory: %w", err)
	}
	// 0o600 rather than 0o644: attachments are private correspondence.
	if err := os.WriteFile(path, []byte("wterm fake attachment"), 0o600); err != nil {
		return "", fmt.Errorf("writing attachment: %w", err)
	}
	return path, nil
}

func (f *Fake) open(_ context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("whatsapp: opening %s: %w", path, err)
	}
	return nil
}

// --- Messages ---

func (f *Fake) send(_ context.Context, chatID models.ChatID, body string) (models.Message, error) {
	f.mu.Lock()
	if _, ok := f.chats[chatID]; !ok {
		f.mu.Unlock()
		return models.Message{}, fmt.Errorf("%w: chat %s", ErrNotFound, chatID)
	}
	f.nextID++
	m := models.Message{
		ID:        models.MessageID(fmt.Sprintf("wterm-fake-%06d", f.nextID)),
		ChatID:    chatID,
		SenderID:  f.self.ID,
		Direction: models.DirectionOutgoing,
		Kind:      models.KindText,
		Body:      body,
		Timestamp: time.Now(),
		// Pending, because a real send is confirmed asynchronously. Showing it
		// as sent immediately would make the pending state untestable.
		Status: models.DeliveryPending,
		Local:  true,
	}
	f.appendMessageLocked(m)
	f.mu.Unlock()

	go f.confirmDelivery(chatID, m.ID)
	return m, nil
}

// confirmDelivery advances a pending message to delivered.
//
// The delay models network latency. It is short enough to keep tests fast and
// long enough that the pending state is observable.
func (f *Fake) confirmDelivery(chatID models.ChatID, id models.MessageID) {
	const latency = 40 * time.Millisecond
	time.Sleep(latency)

	f.mu.Lock()
	msgs, ok := f.messages[chatID]
	if !ok {
		f.mu.Unlock()
		return
	}
	var updated models.Message
	for i := range msgs {
		if msgs[i].ID != id {
			continue
		}
		msgs[i].Status = models.DeliveryDelivered
		msgs[i].Local = false
		msgs[i].Timestamp = time.Now()
		updated = msgs[i]
	}
	f.messages[chatID] = msgs
	f.updatePreviewLocked(chatID)
	f.mu.Unlock()

	if updated.ID != "" {
		f.emit(Event{Kind: EventMessage, ChatID: chatID, Message: updated})
	}
}

func (f *Fake) history(
	_ context.Context, chatID models.ChatID, limit int,
) ([]models.Message, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	msgs, ok := f.messages[chatID]
	if !ok {
		// An unfetched chat has an empty history rather than an error: the UI
		// distinguishes "no messages" from "failed to load".
		return nil, nil
	}
	if limit > 0 && len(msgs) > limit {
		// The newest limit messages, still oldest first.
		msgs = msgs[len(msgs)-limit:]
	}
	return append([]models.Message(nil), msgs...), nil
}

func (f *Fake) edit(
	_ context.Context, chatID models.ChatID, id models.MessageID, body string,
) (models.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	msgs, ok := f.messages[chatID]
	if !ok {
		return models.Message{}, fmt.Errorf("%w: message %s", ErrNotFound, id)
	}
	for i := range msgs {
		m := &msgs[i]
		if m.ID != id {
			continue
		}
		if !m.IsOutgoing() {
			return models.Message{}, fmt.Errorf("%w: message %s", ErrNotEditable, id)
		}
		m.Body = body
		m.EditCount++
		m.Revoked = false
		f.messages[chatID] = msgs
		f.updatePreviewLocked(chatID)
		return *m, nil
	}
	return models.Message{}, fmt.Errorf("%w: message %s", ErrNotFound, id)
}

// deleteMessage revokes a message for everyone.
//
// A revoke leaves a tombstone rather than removing the row: the transcript must
// not reflow, and a sender must not be able to erase the fact that a message
// was sent at all.
func (f *Fake) deleteMessage(_ context.Context, chatID models.ChatID, id models.MessageID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	msgs, ok := f.messages[chatID]
	if !ok {
		return fmt.Errorf("%w: message %s", ErrNotFound, id)
	}
	for i := range msgs {
		m := &msgs[i]
		if m.ID != id {
			continue
		}
		m.Revoked = true
		m.Body = ""
		m.Media = nil
		m.Reactions = nil
		f.messages[chatID] = msgs
		f.updatePreviewLocked(chatID)
		return nil
	}
	return fmt.Errorf("%w: message %s", ErrNotFound, id)
}

// react toggles a reaction.
//
// Reacting with the glyph already present removes it, which is what WhatsApp
// does and what users expect; an empty glyph is treated as a retraction.
func (f *Fake) react(
	_ context.Context, chatID models.ChatID, id models.MessageID, glyph string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	msgs, ok := f.messages[chatID]
	if !ok {
		return fmt.Errorf("%w: message %s", ErrNotFound, id)
	}
	for i := range msgs {
		m := &msgs[i]
		if m.ID != id {
			continue
		}
		if m.Reactions == nil {
			m.Reactions = make(map[string][]models.ContactID)
		}

		// An empty glyph retracts the local user's own reaction, whatever it
		// was, since the protocol does not tell us which one it was.
		if glyph == "" {
			for g, who := range m.Reactions {
				if slicesContains(who, f.self.ID) {
					m.Reactions[g] = removeContact(who, f.self.ID)
					if len(m.Reactions[g]) == 0 {
						delete(m.Reactions, g)
					}
				}
			}
			f.messages[chatID] = msgs
			return nil
		}

		for j, c := range m.Reactions[glyph] {
			if c == f.self.ID {
				m.Reactions[glyph] = append(m.Reactions[glyph][:j], m.Reactions[glyph][j+1:]...)
				if len(m.Reactions[glyph]) == 0 {
					delete(m.Reactions, glyph)
				}
				f.messages[chatID] = msgs
				return nil
			}
		}
		m.Reactions[glyph] = append(m.Reactions[glyph], f.self.ID)
		f.messages[chatID] = msgs
		return nil
	}
	return fmt.Errorf("%w: message %s", ErrNotFound, id)
}

func slicesContains(s []models.ContactID, v models.ContactID) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func removeContact(s []models.ContactID, v models.ContactID) []models.ContactID {
	out := make([]models.ContactID, 0, len(s))
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func (f *Fake) forward(
	_ context.Context, from models.ChatID, id models.MessageID, to models.ChatID,
) (models.Message, error) {
	f.mu.RLock()
	orig := f.findMessageLocked(from, id)
	_, toExists := f.chats[to]
	f.mu.RUnlock()

	if orig.ID == "" {
		return models.Message{}, fmt.Errorf("%w: message %s", ErrNotFound, id)
	}
	if !toExists {
		return models.Message{}, fmt.Errorf("%w: chat %s", ErrNotFound, to)
	}

	sent, err := f.send(context.Background(), to, orig.Body)
	if err != nil {
		return models.Message{}, err
	}

	f.mu.Lock()
	msgs := f.messages[to]
	for i := range msgs {
		if msgs[i].ID == sent.ID {
			msgs[i].Forwarded = true
			break
		}
	}
	f.messages[to] = msgs
	f.mu.Unlock()

	return sent, nil
}

// appendMessageLocked adds a message and refreshes the chat's derived state.
func (f *Fake) appendMessageLocked(m models.Message) {
	msgs := append(f.messages[m.ChatID], m)
	models.SortMessages(msgs)
	f.messages[m.ChatID] = msgs

	c := f.chats[m.ChatID]
	c.Timestamp = m.Timestamp
	c.LastMessage = previewOf(m)
	if !m.IsOutgoing() {
		c.UnreadCount++
		c.HasUnread = true
	}
	f.chats[m.ChatID] = c
}

func previewOf(m models.Message) *models.MessagePreview {
	return &models.MessagePreview{
		Kind:       m.Kind,
		Body:       m.Body,
		IsOutgoing: m.IsOutgoing(),
		Timestamp:  m.Timestamp,
		Media:      m.Media,
	}
}

// updatePreviewLocked recomputes a chat's preview from its newest message.
func (f *Fake) updatePreviewLocked(chatID models.ChatID) {
	msgs := f.messages[chatID]
	if len(msgs) == 0 {
		return
	}
	c := f.chats[chatID]
	c.LastMessage = previewOf(msgs[len(msgs)-1])
	f.chats[chatID] = c
}

func (f *Fake) findMessageLocked(chatID models.ChatID, id models.MessageID) models.Message {
	for _, m := range f.messages[chatID] {
		if m.ID == id {
			return m
		}
	}
	return models.Message{}
}

// --- Fixtures, for tests and the Phase 1 demo ---

// PutChat adds or replaces a chat, preserving insertion order.
func (f *Fake) PutChat(c models.Chat) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.chats[c.ID]; !exists {
		f.chatOrder = append(f.chatOrder, c.ID)
	}
	f.chats[c.ID] = c
}

// PutContact adds or replaces a contact.
func (f *Fake) PutContact(c models.Contact) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contacts[c.ID] = c
}

// Receive records an incoming message and emits an event.
func (f *Fake) Receive(chatID models.ChatID, sender models.ContactID, body string) models.Message {
	f.mu.Lock()
	f.nextID++
	m := models.Message{
		ID:        models.MessageID(fmt.Sprintf("wterm-fake-in-%06d", f.nextID)),
		ChatID:    chatID,
		SenderID:  sender,
		Direction: models.DirectionIncoming,
		Kind:      models.KindText,
		Body:      body,
		Timestamp: time.Now(),
	}
	f.appendMessageLocked(m)
	f.mu.Unlock()

	f.emit(Event{Kind: EventMessage, ChatID: chatID, Message: m})
	return m
}

// SetPresence updates a contact's availability and emits the events a UI needs
// to update both the presence dot and the chat row.
func (f *Fake) SetPresence(id models.ContactID, state models.PresenceState) {
	f.mu.Lock()
	c, ok := f.contacts[id]
	if !ok {
		f.mu.Unlock()
		return
	}
	c.Presence = models.Presence{ContactID: id, State: state, LastSeen: time.Now()}
	f.contacts[id] = c
	chatID, found := f.chatForContactLocked(id)
	f.mu.Unlock()

	f.emit(Event{Kind: EventPresence, ChatID: chatID, Presence: c.Presence})
	if found {
		f.emit(Event{Kind: EventChatUpdate, ChatID: chatID})
	}
}

func (f *Fake) chatForContactLocked(id models.ContactID) (models.ChatID, bool) {
	for _, cid := range f.chatOrder {
		if c := f.chats[cid]; c.Contact != nil && c.Contact.ID == id {
			return cid, true
		}
	}
	return "", false
}

// SetTyping updates a chat's composing indicator and emits an event.
func (f *Fake) SetTyping(chatID models.ChatID, typing bool) {
	f.mu.Lock()
	c, ok := f.chats[chatID]
	if !ok {
		f.mu.Unlock()
		return
	}
	c.Typing = typing
	f.chats[chatID] = c
	f.mu.Unlock()

	f.emit(Event{Kind: EventTyping, ChatID: chatID, Typing: typing})
}

// emit publishes an event without blocking.
//
// A full channel means the consumer is not keeping up. Dropping the event is
// the right failure: blocking would stall the producer, and a stale presence
// update is not worth a deadlock.
func (f *Fake) emit(e Event) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	f.emitLocked(e)
}

func (f *Fake) emitLocked(e Event) {
	if f.stopped {
		return
	}
	select {
	case f.events <- e:
	default:
	}
}
