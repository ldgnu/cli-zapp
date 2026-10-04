package whatsapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wterm/wterm/internal/models"
)

// env bundles the fake with its service views and the ids the tests refer to.
//
// The service views are separate objects rather than the fake itself, so tests go
// through the same interfaces the application uses. A test that reached into the
// fake's fields directly would keep compiling after an interface changed, and
// would stop testing anything that matters.
type env struct {
	fake *Fake
	svc  Services
	ctx  context.Context
}

const (
	chatID  models.ChatID    = "direct"
	groupID models.ChatID    = "group"
	peerID  models.ContactID = "peer"
)

// fixture builds a fake with a self contact, a direct chat and a group.
func fixture(t *testing.T) env {
	t.Helper()

	self := models.Contact{ID: "self", Name: "You", Phone: "+540000000001"}
	f := NewFake(self)
	f.SetDownloadDir(t.TempDir())

	f.PutContact(models.Contact{ID: peerID, Name: "Ada", Phone: "+540000000002"})
	f.PutChat(models.Chat{ID: chatID, Type: models.ChatTypeDirect, Name: "Ada"})
	f.PutChat(models.Chat{ID: groupID, Type: models.ChatTypeGroup, Name: "Gophers"})

	return env{fake: f, svc: f.Services(), ctx: context.Background()}
}

func TestServicesSatisfyEveryInterface(t *testing.T) {
	// The shim split is easy to break by adding a method; this is the assertion
	// that catches it.
	e := fixture(t)
	s := e.svc

	if s.Chat == nil || s.Message == nil || s.Contact == nil ||
		s.Group == nil || s.Media == nil || s.Sync == nil {
		t.Fatal("Services must populate every field")
	}
}

func TestSelfSurvivesANilService(t *testing.T) {
	// A missing service is a wiring error, not a runtime condition; the UI must
	// show an unlinked account rather than panic at startup.
	var s Services
	if got := s.Self(); got.ID != "" {
		t.Errorf("expected an empty contact, got %+v", got)
	}
}

func TestChatListPreservesInsertionOrder(t *testing.T) {
	e := fixture(t)

	chats, err := e.svc.Chat.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 2 {
		t.Fatalf("expected 2 chats, got %d", len(chats))
	}
	if chats[0].ID != "direct" || chats[1].ID != "group" {
		t.Errorf("unexpected order: %q then %q", chats[0].ID, chats[1].ID)
	}
}

func TestUnknownIdentifiersReportNotFound(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	if _, err := e.svc.Chat.Get(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("chat: got %v, want ErrNotFound", err)
	}
	if _, err := e.svc.Contact.Get(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("contact: got %v, want ErrNotFound", err)
	}
	if err := e.svc.Chat.SetPinned(ctx, "nope", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("pin: got %v, want ErrNotFound", err)
	}
}

func TestSendAppendsAndUpdatesTheChat(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	sent, err := e.svc.Message.Send(ctx, chatID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !sent.IsOutgoing() {
		t.Error("a sent message must be outgoing")
	}
	if sent.Status != models.DeliveryPending {
		t.Errorf("status = %v, want pending: a send is confirmed asynchronously", sent.Status)
	}
	if !sent.Local {
		t.Error("a message that has not been acknowledged must be marked local")
	}

	history, err := e.svc.Message.History(ctx, chatID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Body != "hello" {
		t.Fatalf("unexpected history: %+v", history)
	}

	c, err := e.svc.Chat.Get(ctx, chatID)
	if err != nil {
		t.Fatal(err)
	}
	if c.LastMessage == nil || c.LastMessage.Body != "hello" {
		t.Errorf("the chatID preview should update, got %+v", c.LastMessage)
	}
}

func TestIncomingMessageIncrementsUnread(t *testing.T) {
	e := fixture(t)

	e.fake.Receive(chatID, peerID, "hi there")

	c, _ := e.svc.Chat.Get(context.Background(), chatID)
	if c.UnreadCount != 1 || !c.HasUnread {
		t.Errorf("an incoming message should be unread, got %d/%v", c.UnreadCount, c.HasUnread)
	}
}

func TestOutgoingMessageDoesNotIncrementUnread(t *testing.T) {
	e := fixture(t)

	if _, err := e.svc.Message.Send(context.Background(), chatID, "mine"); err != nil {
		t.Fatal(err)
	}

	c, _ := e.svc.Chat.Get(context.Background(), chatID)
	if c.UnreadCount != 0 || c.HasUnread {
		t.Error("one's own message should not count as unread")
	}
}

func TestMarkReadAndUnreadAreDistinct(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	e.fake.Receive(chatID, peerID, "one")
	e.fake.Receive(chatID, peerID, "two")

	if err := e.svc.Chat.SetRead(ctx, chatID, true); err != nil {
		t.Fatal(err)
	}
	c, _ := e.svc.Chat.Get(ctx, chatID)
	if c.UnreadCount != 0 || c.HasUnread {
		t.Errorf("marking read should clear the badge, got %d/%v", c.UnreadCount, c.HasUnread)
	}

	if err := e.svc.Chat.SetRead(ctx, chatID, false); err != nil {
		t.Fatal(err)
	}
	c, _ = e.svc.Chat.Get(ctx, chatID)
	if !c.HasUnread || c.UnreadCount < 1 {
		t.Errorf("marking unread should set a badge, got %d/%v", c.UnreadCount, c.HasUnread)
	}
}

func TestDeliveryIsConfirmedAsynchronously(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	sent, err := e.svc.Message.Send(ctx, chatID, "hello")
	if err != nil {
		t.Fatal(err)
	}

	// The fake confirms after a short delay; poll rather than sleeping a fixed
	// amount, which would be slow and still flaky.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		history, _ := e.svc.Message.History(ctx, chatID, 0)
		for _, m := range history {
			if m.ID == sent.ID && m.Status == models.DeliveryDelivered && !m.Local {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("the message was never confirmed as delivered")
}

func TestHistoryRespectsTheLimit(t *testing.T) {
	e := fixture(t)

	for i := range 10 {
		e.fake.Receive(chatID, peerID, string(rune('a'+i)))
	}

	history, err := e.svc.Message.History(context.Background(), chatID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(history))
	}
	// Ten messages a..j, so the last three are h, i, j — still oldest first.
	if history[0].Body != "h" || history[2].Body != "j" {
		t.Errorf("expected the last three in order, got %q..%q", history[0].Body, history[2].Body)
	}
}

func TestHistoryOfAnUnknownChatIsEmptyNotAnError(t *testing.T) {
	// The UI distinguishes "no messages" from "failed to load", so an unfetched
	// chatID must be the former.
	e := fixture(t)

	history, err := e.svc.Message.History(context.Background(), "never-seen", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("expected an empty history, got %d", len(history))
	}
}

func TestEditOnlyAppliesToOwnMessages(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	incoming := e.fake.Receive(chatID, peerID, "theirs")
	_, err := e.svc.Message.Edit(ctx, chatID, incoming.ID, "mine now")
	if !errors.Is(err, ErrNotEditable) {
		t.Errorf("editing an incoming message: got %v, want ErrNotEditable", err)
	}

	sent, _ := e.svc.Message.Send(ctx, chatID, "mine")
	edited, err := e.svc.Message.Edit(ctx, chatID, sent.ID, "mine, edited")
	if err != nil {
		t.Fatal(err)
	}
	if edited.Body != "mine, edited" {
		t.Errorf("body = %q", edited.Body)
	}
	if edited.EditCount != 1 {
		t.Errorf("EditCount = %d, want 1", edited.EditCount)
	}
}

func TestDeleteLeavesATombstone(t *testing.T) {
	// The transcript must not reflow, and a sender must not be able to erase the
	// fact that a message was sent at all.
	e := fixture(t)
	ctx := e.ctx

	sent, _ := e.svc.Message.Send(ctx, chatID, "oops")
	if err := e.svc.Message.Delete(ctx, chatID, sent.ID); err != nil {
		t.Fatal(err)
	}

	history, _ := e.svc.Message.History(ctx, chatID, 0)
	if len(history) != 1 {
		t.Fatalf("the tombstone should remain, got %d messages", len(history))
	}
	if !history[0].Revoked {
		t.Error("the message should be marked revoked")
	}
	if history[0].Body != "" {
		t.Errorf("the body must be cleared, got %q", history[0].Body)
	}
}

func TestReactToggles(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	sent, _ := e.svc.Message.Send(ctx, chatID, "hi")

	if err := e.svc.Message.React(ctx, chatID, sent.ID, "👍"); err != nil {
		t.Fatal(err)
	}
	history, _ := e.svc.Message.History(ctx, chatID, 0)
	if len(history[0].Reactions["👍"]) != 1 {
		t.Fatalf("expected one reaction, got %+v", history[0].Reactions)
	}

	// Reacting again with the same glyph removes it, which is what WhatsApp does.
	if err := e.svc.Message.React(ctx, chatID, sent.ID, "👍"); err != nil {
		t.Fatal(err)
	}
	history, _ = e.svc.Message.History(ctx, chatID, 0)
	if len(history[0].Reactions) != 0 {
		t.Errorf("the reaction should have been toggled off, got %+v", history[0].Reactions)
	}

	// An empty glyph retracts whatever the local user reacted with.
	_ = e.svc.Message.React(ctx, chatID, sent.ID, "❤️")
	if err := e.svc.Message.React(ctx, chatID, sent.ID, ""); err != nil {
		t.Fatal(err)
	}
	history, _ = e.svc.Message.History(ctx, chatID, 0)
	if len(history[0].Reactions) != 0 {
		t.Errorf("retraction should clear every local reaction, got %+v", history[0].Reactions)
	}
}

func TestForwardCopiesTheBodyAndMarksItForwarded(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	orig, _ := e.svc.Message.Send(ctx, chatID, "worth sharing")
	sent, err := e.svc.Message.Forward(ctx, chatID, orig.ID, groupID)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Body != "worth sharing" {
		t.Errorf("body = %q", sent.Body)
	}

	history, _ := e.svc.Message.History(ctx, groupID, 0)
	if len(history) != 1 || !history[0].Forwarded {
		t.Errorf("the forwarded copy should be marked, got %+v", history)
	}
}

func TestForwardRejectsAnUnknownSource(t *testing.T) {
	e := fixture(t)

	ctx := context.Background()

	if _, err := e.svc.Message.Forward(ctx, chatID, "nope", groupID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown source: got %v, want ErrNotFound", err)
	}
	if _, err := e.svc.Message.Forward(ctx, chatID, "x", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown destination: got %v, want ErrNotFound", err)
	}
}

func TestPinMuteArchive(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	for name, fn := range map[string]func(context.Context, models.ChatID, bool) error{
		"pin":     e.svc.Chat.SetPinned,
		"mute":    e.svc.Chat.SetMuted,
		"archive": e.svc.Chat.SetArchived,
	} {
		if err := fn(ctx, chatID, true); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	c, _ := e.svc.Chat.Get(ctx, chatID)
	if !c.Pinned || !c.Muted || !c.Archived {
		t.Errorf("flags not applied: %+v", c)
	}

	for name, fn := range map[string]func(context.Context, models.ChatID, bool) error{
		"pin":     e.svc.Chat.SetPinned,
		"mute":    e.svc.Chat.SetMuted,
		"archive": e.svc.Chat.SetArchived,
	} {
		if err := fn(ctx, chatID, false); err != nil {
			t.Fatalf("%s off: %v", name, err)
		}
	}
	c, _ = e.svc.Chat.Get(ctx, chatID)
	if c.Pinned || c.Muted || c.Archived {
		t.Errorf("flags not cleared: %+v", c)
	}
}

func TestDeleteChatRemovesItAndItsMessages(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	e.fake.Receive(chatID, peerID, "hi")
	if err := e.svc.Chat.Delete(ctx, chatID); err != nil {
		t.Fatal(err)
	}

	if _, err := e.svc.Chat.Get(ctx, chatID); !errors.Is(err, ErrNotFound) {
		t.Errorf("chat should be gone, got %v", err)
	}
	history, err := e.svc.Message.History(ctx, chatID, 0)
	if err != nil {
		t.Fatalf("history of a deleted chatID should be empty, got %v", err)
	}
	if len(history) != 0 {
		t.Errorf("messages should be gone, got %d", len(history))
	}

	chats, _ := e.svc.Chat.List(ctx)
	for _, c := range chats {
		if c.ID == chatID {
			t.Error("the deleted chatID is still listed")
		}
	}
}

func TestSyncStartEmitsOrderedConnectionStates(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	if err := e.svc.Sync.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer e.svc.Sync.Stop(ctx)

	var states []ConnectionState
	for range 2 {
		select {
		case e := <-e.svc.Sync.Events():
			if e.Kind == EventConnection {
				states = append(states, e.Connection)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for connection events")
		}
	}

	if len(states) != 2 || states[0] != ConnectionConnecting || states[1] != ConnectionOnline {
		t.Errorf("expected connecting then online, got %v", states)
	}
	if !e.svc.Sync.Connected() {
		t.Error("the fake should report connected")
	}
}

func TestSyncStopClosesTheChannel(t *testing.T) {
	// A closed channel is how the consumer learns the stream ended; treating
	// silence as a healthy connection would hide a dead link.
	e := fixture(t)
	ctx := e.ctx

	if err := e.svc.Sync.Start(ctx); err != nil {
		t.Fatal(err)
	}
	events := e.svc.Sync.Events()
	if err := e.svc.Sync.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	// Drain anything already buffered, then the channel must close.
	for range events {
	}
	if _, ok := <-events; ok {
		t.Error("the channel should be closed after Stop")
	}
	if e.svc.Sync.Connected() {
		t.Error("the fake should report disconnected")
	}
}

func TestSyncStopIsIdempotent(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	if err := e.svc.Sync.Stop(ctx); err != nil {
		t.Errorf("stopping an unstarted sync should be a no-op, got %v", err)
	}
	_ = e.svc.Sync.Start(ctx)
	_ = e.svc.Sync.Stop(ctx)
	if err := e.svc.Sync.Stop(ctx); err != nil {
		t.Errorf("a second stop should be a no-op, got %v", err)
	}
}

func TestReceiveEmitsAnEvent(t *testing.T) {
	e := fixture(t)
	_ = e.svc.Sync.Start(e.ctx)
	defer e.svc.Sync.Stop(e.ctx)

	// Drain the connection transitions that Start emits, so the assertion below
	// is about the message rather than about whichever event happened to be first.
	for range 2 {
		<-e.svc.Sync.Events()
	}

	e.fake.Receive(chatID, peerID, "hello")

	select {
	case ev := <-e.svc.Sync.Events():
		if ev.Kind != EventMessage {
			t.Fatalf("kind = %v, want a message", ev.Kind)
		}
		if ev.ChatID != chatID {
			t.Errorf("chat = %q, want %q", ev.ChatID, chatID)
		}
		if ev.Message.Body != "hello" {
			t.Errorf("body = %q", ev.Message.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("no event was emitted")
	}
}

func TestSetPresenceAndTypingEmit(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx
	_ = e.svc.Sync.Start(ctx)
	defer e.svc.Sync.Stop(ctx)

	e.fake.SetPresence(peerID, models.PresenceAvailable)
	e.fake.SetTyping(chatID, true)

	var sawPresence, sawTyping bool
	for range 8 {
		select {
		case e := <-e.svc.Sync.Events():
			switch e.Kind {
			case EventPresence:
				sawPresence = true
				if e.Presence.State != models.PresenceAvailable {
					t.Errorf("presence state = %v", e.Presence.State)
				}
			case EventTyping:
				sawTyping = true
				if !e.Typing {
					t.Error("the typing event should report typing")
				}
			default:
				// Connection events precede these; nothing to assert.
			}
		case <-time.After(time.Second):
			t.Fatal("timed out")
		}
		if sawPresence && sawTyping {
			return
		}
	}
	t.Errorf("presence=%v typing=%v", sawPresence, sawTyping)
}

func TestTypingIsStoredOnTheChat(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	e.fake.SetTyping(chatID, true)
	c, _ := e.svc.Chat.Get(ctx, chatID)
	if !c.Typing {
		t.Error("the chatID should record that the peer is typing")
	}

	e.fake.SetTyping(chatID, false)
	c, _ = e.svc.Chat.Get(ctx, chatID)
	if c.Typing {
		t.Error("the flag should clear when typing stops")
	}
}

func TestGroupListingOnlyIncludesGroups(t *testing.T) {
	e := fixture(t)

	groups, err := e.svc.Group.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ID != groupID {
		t.Errorf("expected only the groupID, got %+v", groups)
	}
}

func TestLeaveGroupRemovesTheChat(t *testing.T) {
	e := fixture(t)
	ctx := context.Background()

	if err := e.svc.Group.Leave(ctx, groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Chat.Get(ctx, groupID); !errors.Is(err, ErrNotFound) {
		t.Errorf("the groupID should be gone, got %v", err)
	}
}

func TestDownloadWritesAFileWithRestrictivePermissions(t *testing.T) {
	// Attachments are private correspondence, so the file must not be world
	// readable even on a shared machine.
	e := fixture(t)
	ctx := e.ctx

	sent, err := e.svc.Message.Send(ctx, chatID, "")
	if err != nil {
		t.Fatal(err)
	}
	sent.Kind = models.KindDocument
	sent.Media = &models.Media{Kind: models.MediaKindDocument, Filename: "invoice.pdf"}

	// Inject the media directly, since Send does not carry attachments.
	if err := attachMedia(e.fake, chatID, sent.ID, sent.Media); err != nil {
		t.Fatal(err)
	}

	path, err := e.svc.Media.Download(ctx, chatID, sent.ID)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
	if filepath.Base(path) != "invoice.pdf" {
		t.Errorf("path = %q, want the sender's filename", path)
	}
}

func TestDownloadRejectsAMessageWithNoAttachment(t *testing.T) {
	e := fixture(t)
	ctx := e.ctx

	sent, _ := e.svc.Message.Send(ctx, chatID, "no attachment")
	if _, err := e.svc.Media.Download(ctx, chatID, sent.ID); !errors.Is(err, ErrNoAttachment) {
		t.Errorf("got %v, want ErrNoAttachment", err)
	}
}

func TestOpenChecksTheFileExists(t *testing.T) {
	e := fixture(t)
	missing := filepath.Join(t.TempDir(), "absent")
	if err := e.svc.Media.Open(context.Background(), missing); err == nil {
		t.Error("opening a missing file should fail")
	}
}

func TestConcurrentAccessIsSafe(t *testing.T) {
	// Run with -race. The sync goroutine writes while the UI reads, so the fake
	// must hold its lock for every access.
	e := fixture(t)
	ctx := e.ctx
	_ = e.svc.Sync.Start(ctx)
	defer e.svc.Sync.Stop(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 50 {
			e.fake.Receive(chatID, peerID, "concurrent")
		}
	}()

	for range 50 {
		_, _ = e.svc.Chat.List(ctx)
		_, _ = e.svc.Message.History(ctx, chatID, 0)
		e.fake.SetTyping(chatID, true)
	}
	<-done
}

// attachMedia injects media onto an existing message.
func attachMedia(f *Fake, chatID models.ChatID, id models.MessageID, media *models.Media) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	msgs, ok := f.messages[chatID]
	if !ok {
		return ErrNotFound
	}
	for i := range msgs {
		if msgs[i].ID == id {
			msgs[i].Media = media
			msgs[i].Kind = models.KindDocument
			f.messages[chatID] = msgs
			return nil
		}
	}
	return ErrNotFound
}
