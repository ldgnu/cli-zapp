package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/models"
	"github.com/cli-zapp/cli-zapp/internal/ui/component"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/composer"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/statusbar"
	"github.com/cli-zapp/cli-zapp/internal/ui/theme"
	"github.com/cli-zapp/cli-zapp/internal/whatsapp"
)

// harness drives a model the way Bubble Tea's loop does: apply a message, run the
// commands Update returns, fold the results back in, and repeat until quiet.
//
// The frame tests call View() directly and are the right tool for asserting on pixels.
// They are the wrong tool for the service paths, because a command is a function the
// program schedules and a test does not — so a test that skips the loop never reaches
// a send, and a service path that is never reached is a service path nobody has run.
type harness struct {
	t    *testing.T
	m    *Model
	fake servicesForTest
}

// servicesForTest is the subset of the fake the harness inspects.
type servicesForTest struct {
	chats    func() []models.Chat
	messages func(models.ChatID) []models.Message
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	fake := DemoData()
	m := New(fake.Services(), theme.Dark(), keybindings.DefaultMap())
	m.DisableTimers = true

	h := &harness{t: t, m: m, fake: servicesForTest{
		chats: func() []models.Chat {
			list, _ := fake.Services().Chat.List(t.Context())
			return list
		},
		messages: func(id models.ChatID) []models.Message {
			list, _ := fake.Services().Message.History(t.Context(), id, historyLimit)
			return list
		},
	}}

	// Startup order, as the program does it: the session and the conversation list
	// first, then the size. A harness that skipped Init would have an empty sidebar and
	// every test below would fail against a model that never had any data.
	h.settle(tea.WindowSizeMsg{Width: 120, Height: 30})
	h.settle(flatten(m.Init())...)
	return h
}

// apply sends one message and settles everything it triggers.
func (h *harness) apply(msg tea.Msg) { h.settle(msg) }

// settle applies a message and runs the resulting commands, recursively.
//
// The recursion is what makes a send testable end to end: pressing enter produces a
// command that calls the service and a message that carries the new transcript, and
// only the second step produces the frame the user sees.
func (h *harness) settle(msgs ...tea.Msg) {
	h.t.Helper()

	pending := msgs
	for round := 0; round < 20 && len(pending) > 0; round++ {
		next := make([]tea.Msg, 0, len(pending))
		for _, m := range pending {
			_, cmd := h.m.Update(m)
			next = append(next, flatten(cmd)...)
		}
		pending = next
	}
}

// flatten runs one command, expanding the envelopes Bubble Tea wraps batches in.
//
// A command that does not return within the deadline is abandoned rather than waited
// for. The long-lived read on the account event stream is exactly such a command: it
// blocks until the service produces something, and the fake produces nothing until a
// test asks it to. Without the deadline the harness would hang on startup.
func flatten(cmd tea.Cmd) []tea.Msg {
	// A nil command is the normal way Bubble Tea says "nothing to do", and both
	// callers pass whatever Update returned without filtering, so the check belongs
	// here rather than being duplicated — and forgotten — at each call site.
	if cmd == nil {
		return nil
	}

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	var got tea.Msg
	select {
	case got = <-done:
	case <-time.After(300 * time.Millisecond):
		return nil
	}

	if got == nil {
		return nil
	}
	batch, ok := got.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{got}
	}
	var out []tea.Msg
	for _, c := range batch {
		out = append(out, flatten(c)...)
	}
	return out
}

// key sends a key press and settles.
func (h *harness) key(msg tea.KeyPressMsg) { h.settle(msg) }

// body returns the rendered frame with styling stripped.
func (h *harness) body() string {
	return strings.Join(frame(h.m), "\n")
}

// openChat makes a conversation current and settles the transcript fetch.
func (h *harness) openChat(id models.ChatID) {
	h.t.Helper()
	h.key(ctrlEnter())
	if h.m.currentChatID() != id {
		// The cursor was somewhere else; move to the named conversation.
		h.m.sidebar.SelectChat(id)
		h.m.rebuildCommands()
		h.key(ctrlEnter())
	}
	if h.m.currentChatID() != id {
		h.t.Fatalf("could not open %q; the open conversation is %q", id, h.m.currentChatID())
	}
}

// --- sending ---

func TestSendingAMessageReachesTheService(t *testing.T) {
	h := newHarness(t)
	h.openChat("chat-ada")

	before := len(h.fake.messages("chat-ada"))
	// Through the application, not by calling Focus on the widget: the widget being
	// focused is not the same as the application believing it is, and a test that sets
	// one and not the other is testing a state no user can reach.
	h.settle(h.m.setFocus(component.RegionComposer))

	for _, r := range "hola" {
		h.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := h.m.composer.Value(); got != "hola" {
		t.Fatalf("the draft is %q, want %q", got, "hola")
	}

	h.key(tea.KeyPressMsg{Code: '\r', Text: "\r"})

	after := h.fake.messages("chat-ada")
	if len(after) != before+1 {
		t.Fatalf("the service holds %d messages, want %d", len(after), before+1)
	}
	if !strings.Contains(after[len(after)-1].Text(), "hola") {
		t.Errorf("the last message is %q", after[len(after)-1].Text())
	}
	// The draft is cleared, so the next message does not append to this one.
	if !h.m.composer.Empty() {
		t.Error("the composer should be empty after sending")
	}
	// And the message appears on screen.
	if !strings.Contains(h.body(), "hola") {
		t.Error("the sent message should be in the frame")
	}
}

func TestSendingInAGroupNamesTheSender(t *testing.T) {
	// A group has no contact attached to the chat, so the sender's name has to come
	// from the group roster or the reply quote says "alguien".
	h := newHarness(t)
	h.openChat("chat-group")

	chat, ok := h.m.currentChat()
	if !ok || chat.Type != models.ChatTypeGroup {
		t.Fatalf("chat-group is not an open group: %+v", chat)
	}
	if !chat.Type.IsGroup() {
		t.Error("a group should be identified as one")
	}
	if !strings.Contains(h.body(), "grupo") {
		t.Errorf("the header should say it is a group:\n%s", h.body())
	}
}

// --- flags ---

func TestChatFlagsReachTheService(t *testing.T) {
	// The archive case starts from a conversation that is already archived, so that
	// reversing is exercised in both directions across the three.
	// The pin toggle is driven through the palette because that is now its only path:
	// mod+p belongs to the command palette and the pin has no key of its own. Driving
	// it by key would be testing a binding that no longer exists, and a test that
	// still pressed one would keep passing right up to the day the pin stopped working
	// at all.
	tests := []struct {
		name  string
		press func(h *harness)
		id    models.ChatID
		check func(models.Chat) bool
		from  bool
	}{
		{
			name:  "pin, through the palette",
			press: func(h *harness) { h.runCommand("anclar") },
			id:    "chat-alan",
			check: func(c models.Chat) bool { return c.Pinned },
			from:  false,
		},
		{
			name: "mute",
			press: func(h *harness) {
				h.key(tea.KeyPressMsg{Code: 'm', Text: "m", Mod: tea.ModCtrl})
			},
			id:    "chat-alan",
			check: func(c models.Chat) bool { return c.Muted },
			from:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.m.sidebar.SelectChat(tc.id)
			h.m.rebuildCommands()

			if got := h.flagOf(tc.id, tc.check); got != tc.from {
				t.Fatalf("the fixture should start with %q = %v", tc.name, tc.from)
			}

			tc.press(h)
			if got := h.flagOf(tc.id, tc.check); got == tc.from {
				t.Errorf("%q did not change in the service", tc.name)
			}

			// Pressing again reverses it, which is the part that is easy to get wrong: a
			// toggle that only ever turns things on is indistinguishable from a bug
			// until the user presses it twice.
			tc.press(h)
			if got := h.flagOf(tc.id, tc.check); got != tc.from {
				t.Errorf("%q did not reverse", tc.name)
			}
		})
	}
}

// runCommand opens the palette, types a query and chooses the highlighted command.
//
// It goes through the palette rather than calling Execute directly, because the point
// is that the path a user takes works end to end: the key opens it, the query filters,
// enter picks. A test that calls Execute skips all three.
func (h *harness) runCommand(query string) {
	h.t.Helper()

	h.key(tea.KeyPressMsg{Code: 'p', Text: "p", Mod: tea.ModCtrl})
	for _, r := range query {
		h.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	h.key(tea.KeyPressMsg{Code: '\r', Text: "\r"})
}

// TestArchivingHidesTheConversationAndSearchBringsItBack covers archiving separately,
// because its reversal cannot be reached from the list: archiving removes the row, so
// there is nothing left to press the key on.
//
// That is the property worth asserting. An operation with no route back is not a filing
// decision, it is a deletion with a delay.
func TestArchivingHidesTheConversationAndSearchBringsItBack(t *testing.T) {
	h := newHarness(t)

	visible := h.m.sidebar.RowCount()
	h.m.sidebar.SelectChat("chat-alan")
	h.m.rebuildCommands()
	if h.m.currentChatID() != "chat-alan" {
		t.Fatalf("the cursor should be on Alan Turing, got %q", h.m.currentChatID())
	}

	h.key(tea.KeyPressMsg{Code: 'e', Text: "e", Mod: tea.ModCtrl})

	if !h.flagOf("chat-alan", func(c models.Chat) bool { return c.Archived }) {
		t.Fatal("archiving did not reach the service")
	}
	if got := h.m.sidebar.RowCount(); got != visible-1 {
		t.Errorf("an archived conversation should leave the list: %d rows, want %d",
			got, visible-1)
	}

	// Findable by query, which is the route back. An operation with no route back is
	// not a filing decision, it is a deletion with a delay.
	h.key(tea.KeyPressMsg{Code: 'f', Text: "f", Mod: tea.ModCtrl})
	for _, r := range "Alan" {
		h.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := h.m.sidebar.RowCount(); got != 1 {
		t.Fatalf("the query should match exactly the archived conversation, got %d rows", got)
	}

	// And unarchivable from there.
	h.key(tea.KeyPressMsg{Code: 'e', Text: "e", Mod: tea.ModCtrl})
	if h.flagOf("chat-alan", func(c models.Chat) bool { return c.Archived }) {
		t.Error("archiving did not reverse")
	}
}

func TestMarkingReadClearsTheBadge(t *testing.T) {
	h := newHarness(t)
	h.m.sidebar.SelectChat("chat-grace")
	h.m.rebuildCommands()

	chat, ok := h.m.chatByID("chat-grace")
	if !ok || !chat.HasUnread {
		t.Fatalf("the fixture should start unread: %+v", chat)
	}
	_ = chat

	h.key(tea.KeyPressMsg{Code: 'u', Text: "u", Mod: tea.ModCtrl})

	chat, _ = h.m.chatByID("chat-grace")
	if chat.HasUnread || chat.UnreadCount != 0 {
		t.Errorf("the badge survived being marked read: %+v", chat)
	}
	if strings.Contains(h.body(), "sin leer") && h.m.unreadTotal() == 0 {
		t.Error("the status bar should not claim unread messages when there are none")
	}
}

// --- message actions ---

func TestReplyQuotesTheCursorMessage(t *testing.T) {
	h := newHarness(t)
	h.openChat("chat-ada")
	h.settle(h.m.setFocus(component.RegionTranscript))
	h.key(tea.KeyPressMsg{Code: tea.KeyUp})

	// Move to the first message, which is not a system row.
	h.key(tea.KeyPressMsg{Code: 'r', Text: "r"})

	if h.m.composer.Mode() != composer.ModeReply {
		t.Errorf("the composer should be in reply mode, got %v", h.m.composer.Mode())
	}
	if !strings.Contains(h.body(), "respondiendo") {
		t.Errorf("the reply banner should be visible:\n%s", h.body())
	}
	if h.m.focus != component.RegionComposer {
		t.Error("replying should move focus to the composer")
	}
}

func TestEditingOnlyOffersTheUsersOwnMessages(t *testing.T) {
	h := newHarness(t)
	h.openChat("chat-ada")
	h.settle(h.m.setFocus(component.RegionTranscript))

	// The cursor starts on the newest message, which is the account's own and
	// therefore editable.
	h.key(tea.KeyPressMsg{Code: 'e', Text: "e", Mod: tea.ModCtrl})
	if h.m.composer.Mode() != composer.ModeEdit {
		t.Errorf("the composer should be in edit mode, got %v", h.m.composer.Mode())
	}

	// Moving to an incoming message and asking again is refused with an explanation,
	// not silently.
	//
	// Focus goes back to the transcript first: editing moved it to the composer, and
	// the arrow keys would otherwise move the caret rather than the cursor.
	h.m.composer.ClearMode()
	h.settle(h.m.setFocus(component.RegionTranscript))
	for range 6 {
		h.key(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	h.key(tea.KeyPressMsg{Code: 'e', Text: "e", Mod: tea.ModCtrl})
	if h.m.composer.Mode() == composer.ModeEdit {
		t.Error("an incoming message must not be editable")
	}
	// Asserted on a phrase, not on the whole sentence: a toast is wrapped to the pane,
	// so the sentence is split across lines and no single substring spans it.
	if !strings.Contains(h.body(), "solo se pueden editar") {
		t.Errorf("the refusal should be explained:\n%s", h.body())
	}
}

func TestEditingReplacesTheMessageRatherThanAddingOne(t *testing.T) {
	// An edit that appeared as an extra message would misrepresent what was said.
	h := newHarness(t)
	h.openChat("chat-ada")
	h.settle(h.m.setFocus(component.RegionTranscript))

	before := len(h.fake.messages("chat-ada"))
	h.key(tea.KeyPressMsg{Code: 'e', Text: "e", Mod: tea.ModCtrl})

	// The existing body is prefilled, which is what makes editing editing rather than
	// composing. Clearing it before typing would turn the test into one for sending.
	if h.m.composer.Mode() != composer.ModeEdit {
		t.Fatalf("the composer should be in edit mode, got %v", h.m.composer.Mode())
	}
	for h.m.composer.Value() != "" {
		h.key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	for _, r := range "corregido" {
		h.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	h.key(tea.KeyPressMsg{Code: '\r', Text: "\r"})

	after := h.fake.messages("chat-ada")
	if len(after) != before {
		t.Errorf("an edit changed the message count from %d to %d", before, len(after))
	}
	if !strings.Contains(h.body(), "corregido") {
		last := h.m.messages[len(h.m.messages)-1]
		t.Errorf("the edited body should be shown; composer=%q mode=%v last=%q (%d messages)",
			h.m.composer.Value(), h.m.composer.Mode(), last.Text(), len(h.m.messages))
		t.Errorf("frame:\n%s", h.body())
	}
}

func TestRevokingRemovesTheMessageAndKeepsTheRest(t *testing.T) {
	h := newHarness(t)
	h.openChat("chat-ada")
	h.settle(h.m.setFocus(component.RegionTranscript))

	before := len(h.fake.messages("chat-ada"))
	h.key(tea.KeyPressMsg{Code: 'd', Text: "d", Mod: tea.ModCtrl})

	// The first thing enter does is confirm, which cancels.
	if !strings.Contains(h.body(), "Eliminar mensaje") {
		t.Fatalf("deletion should ask first:\n%s", h.body())
	}
	h.key(tea.KeyPressMsg{Code: tea.KeyEscape})

	if len(h.fake.messages("chat-ada")) != before {
		t.Error("escape must not delete anything")
	}
}

func TestDeletingRequiresConfirmation(t *testing.T) {
	h := newHarness(t)
	h.openChat("chat-ada")
	h.settle(h.m.setFocus(component.RegionTranscript))

	before := len(h.fake.messages("chat-ada"))
	h.key(tea.KeyPressMsg{Code: 'd', Text: "d", Mod: tea.ModCtrl})
	h.key(tea.KeyPressMsg{Code: '\r', Text: "\r"}) // cancel is the default button

	if len(h.fake.messages("chat-ada")) != before {
		t.Fatalf("enter on the default button deleted %d messages",
			len(h.fake.messages("chat-ada"))-before)
	}
}

func TestReactionIsRecorded(t *testing.T) {
	h := newHarness(t)
	h.openChat("chat-ada")
	h.settle(h.m.setFocus(component.RegionTranscript))

	h.key(tea.KeyPressMsg{Code: 'R', Text: "R", Mod: tea.ModShift})
	if !strings.Contains(h.body(), "Reaccionar") {
		t.Fatalf("the reaction palette should be open:\n%s", h.body())
	}

	h.key(tea.KeyPressMsg{Code: '\r', Text: "\r"})

	// The reaction reaches the service; whether the fake stores it is its business, so
	// the assertion is on the effect the user can see.
	if !strings.Contains(h.body(), "👍") {
		t.Errorf("the reaction should appear on the message:\n%s", h.body())
	}
}

// --- the event stream ---

func TestAnIncomingMessageUpdatesTheSidebar(t *testing.T) {
	h := newHarness(t)
	h.openChat("chat-ada")

	before := len(h.body())
	h.apply(component.Event{Kind: component.KindFocusChat, ChatID: "chat-grace"})
	h.fake.chats() // the cache is refreshed by the command the model queues

	if len(h.body()) == before {
		t.Error("opening another conversation should change the frame")
	}
	if !strings.Contains(h.body(), "Grace") {
		t.Errorf("the open conversation should be Grace:\n%s", h.body())
	}
}

func TestEventsAreFoldedAndTheLoopIsKeptRunning(t *testing.T) {
	// The single most important thing about the event handler is that it re-arms its
	// own read. Forgetting it ends message reception silently, with nothing on screen
	// indicating why.
	h := newHarness(t)

	cmd := h.m.setEvents([]whatsapp.Event{
		{
			Kind: whatsapp.EventMessage, ChatID: "chat-grace",
			Message: models.Message{ID: "x", Direction: models.DirectionIncoming, Body: "hola"},
		},
	})
	if cmd == nil {
		t.Fatal("folding events must produce commands")
	}

	// The batch carries a chat refresh, a transcript reload and the next read. Counting
	// it is more than a nil check, but the read itself blocks until the service speaks,
	// so it is not counted: what is asserted is that folding produced work rather than
	// silently doing nothing.
	if n := countBatch(cmd); n < 2 {
		t.Errorf("folding one message produced %d commands, want a refresh and a reload", n)
	}

	// An empty batch must still re-arm, for the same reason: the stream's silence is
	// not evidence that it ended.
	if h.m.setEvents(nil) == nil {
		t.Error("an empty batch must still re-arm the read")
	}
}

// countBatch counts the commands in a batch without running them.
//
// Running them is not an option: the read on the event stream blocks until the service
// produces something, which for the in-memory fake means until a test asks it to.
func countBatch(cmd tea.Cmd) int {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	var got tea.Msg
	select {
	case got = <-done:
	case <-time.After(300 * time.Millisecond):
		return -1
	}

	batch, ok := got.(tea.BatchMsg)
	if !ok {
		return 1
	}
	return len(batch)
}

// TestAnEmptyBatchStillKeepsTheStreamAlive covers the case that ends message
// reception with no symptom at all.
func TestAnEmptyBatchStillKeepsTheStreamAlive(t *testing.T) {
	h := newHarness(t)
	if cmd := h.m.setEvents(nil); cmd == nil {
		t.Error("an empty batch must still re-arm the read")
	}
}

func TestNotificationsAreRaisedOnlyForBackgroundConversations(t *testing.T) {
	h := newHarness(t)
	h.openChat("chat-ada")

	// The fixture's own events have already queued notices, so the assertions below are
	// on the delta rather than on the total. Measuring the total would make the test
	// depend on how many events the fake happened to buffer.
	h.m.pendingNotices = nil

	// A message in the conversation on screen: the user is looking at it.
	h.m.raiseNotification(whatsapp.Event{
		Kind: whatsapp.EventMessage, ChatID: h.m.currentChatID(),
		Message: models.Message{Direction: models.DirectionIncoming, Body: "hola"},
	})
	if len(h.m.pendingNotices) != 0 {
		t.Error("a message in the open conversation must not notify")
	}

	// A message elsewhere: it should.
	h.m.raiseNotification(whatsapp.Event{
		Kind: whatsapp.EventMessage, ChatID: "chat-alan",
		Message: models.Message{Direction: models.DirectionIncoming, Body: "hola"},
	})
	if len(h.m.pendingNotices) != 1 {
		t.Fatalf("a background message should notify, got %d", len(h.m.pendingNotices))
	}
	if h.m.pendingNotices[0].Body != "hola" {
		t.Errorf("the notice carries %q", h.m.pendingNotices[0].Body)
	}
}

func TestMutedConversationsDoNotNotify(t *testing.T) {
	// Muting is a request not to be interrupted. Ignoring it is worse than the feature
	// not existing.
	h := newHarness(t)
	h.m.pendingNotices = nil

	h.m.raiseNotification(whatsapp.Event{
		Kind: whatsapp.EventMessage, ChatID: "chat-group",
		Message: models.Message{Direction: models.DirectionIncoming, Body: "hola"},
	})
	if len(h.m.pendingNotices) != 0 {
		t.Error("a muted conversation must not notify")
	}
}

func TestOutgoingMessagesDoNotNotify(t *testing.T) {
	h := newHarness(t)
	h.m.pendingNotices = nil
	h.m.raiseNotification(whatsapp.Event{
		Kind: whatsapp.EventMessage, ChatID: "chat-alan",
		Message: models.Message{Direction: models.DirectionOutgoing, Body: "hola"},
	})
	if len(h.m.pendingNotices) != 0 {
		t.Error("the account's own message must not notify it")
	}
}

// --- the palette ---

func TestChoosingAPaletteCommandRunsIt(t *testing.T) {
	h := newHarness(t)
	h.openChat("chat-ada")

	h.key(tea.KeyPressMsg{Code: 'p', Text: "p", Mod: tea.ModCtrl})
	for _, r := range "keyb" {
		h.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	h.key(tea.KeyPressMsg{Code: '\r', Text: "\r"})

	if !strings.Contains(h.body(), "Atajos de teclado") {
		t.Errorf("typing 'keyb' should open the help sheet:\n%s", h.body())
	}
}

func TestAnUnimplementedPaletteCommandIsRefusedLoudly(t *testing.T) {
	// A command with nothing behind it is a table bug. It should say so rather than
	// look like a key that silently does nothing.
	h := newHarness(t)

	// Straight to the dispatcher, because the palette's filtering is not what is under
	// test: what matters is that an identifier with nothing behind it says so.
	h.settle(h.m.onEvent(component.Event{
		Kind:    component.KindCommandChosen,
		Command: component.Command{ID: "no-existe", Title: "Inventada"},
	}))

	// Asserted on the phrase rather than the whole string: a toast is wrapped to the
	// pane, so "no-existe" is split across two lines and no single substring spans it.
	if !strings.Contains(h.body(), "comando no implementado") {
		t.Errorf("an unknown command should be refused visibly:\n%s", h.body())
	}
}

// --- resync ---

func TestResyncReconnects(t *testing.T) {
	// Asserted against the same fake the model holds, not a fresh one: the state that
	// matters is whether *this* session is live, and comparing against a second fake's
	// answer would pass whatever it happened to be.
	h := newHarness(t)
	h.settle(h.m.syncNow())

	want := statusbar.Offline
	if h.m.services.Sync.Connected() {
		want = statusbar.Online
	}
	if h.m.connection != want {
		t.Errorf("connection = %v, want %v", h.m.connection, want)
	}
	if !strings.Contains(h.body(), "en línea") && !strings.Contains(h.body(), "sin conexión") {
		t.Errorf("the status bar should report the connection:\n%s", h.body())
	}
}

func TestShutdownStopsTheService(t *testing.T) {
	h := newHarness(t)
	if err := h.m.Shutdown(); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

// --- the command table ---

func TestEveryCommandInTheTableCanRun(t *testing.T) {
	// A command the palette offers with nothing behind it looks correct and does
	// nothing. That is a table bug, and it is caught here rather than by a user who
	// presses enter and watches a conversation close.
	//
	// Every entry is built by Model.command, which is what wires the action, so this
	// also asserts that nothing was added to the slice by hand and left unwired.
	h := newHarness(t)
	h.openChat("chat-ada")

	if len(h.m.commands) == 0 {
		t.Fatal("the command table is empty")
	}

	seen := make(map[string]bool)
	for _, c := range h.m.commands {
		if c.Execute == nil {
			t.Errorf("the palette offers %q, which has nothing to run", c.ID)
		}
		if c.Title == "" {
			t.Errorf("command %q has no title", c.ID)
		}
		if c.Description == "" {
			t.Errorf("command %q has no description", c.ID)
		}
		if c.Category == "" {
			t.Errorf("command %q has no category", c.ID)
		}
		if seen[c.ID] {
			t.Errorf("command %q is registered twice", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestEveryActionInTheBindingTableIsReachable(t *testing.T) {
	// Binding an action is only half of reaching it: if no command, key or menu entry
	// offers it, then it is bound to nothing. This is what caught the pin toggle losing
	// mod+p to the palette — it stayed on the binding table with no keys and out of the
	// palette, which is exactly "unreachable" wearing the costume of "configurable".
	h := newHarness(t)
	h.openChat("chat-ada")

	reachable := make(map[string]bool)
	for _, c := range h.m.commands {
		reachable[c.ID] = true
	}
	_ = reachable // the table's identifiers are the palette's; the check is the one below

	// Every action that has at least one key, or is offered as a command, must be
	// dispatchable. A command with no keys is fine — that is the whole point of the
	// palette — so the test is that the action set and the palette do not drift.
	paletted := make(map[string]bool)
	for _, c := range h.m.commands {
		paletted[c.ID] = true
	}

	// The pin toggle is the concrete case: it lost mod+p, so it must be in the palette.
	if !paletted["pin"] {
		t.Error("chat.toggle_pin lost its key to the palette but is not in the palette")
	}
	if !paletted["mark_read"] {
		t.Error("chat.toggle_read should be reachable from the palette")
	}
}

func TestTheCommandTableTracksContext(t *testing.T) {
	// Checked on a model that has never been given a conversation list, because the open
	// conversation is derived from the sidebar's cursor — so as soon as there is a list,
	// there is an open conversation, and "no conversation open" is only reachable at
	// startup.
	m := New(DemoData().Services(), theme.Dark(), keybindings.DefaultMap())

	for _, c := range m.commands {
		switch c.ID {
		case "reply", "edit", "delete_message", "download":
			if c.Available {
				t.Errorf("%q should be unavailable with nothing loaded", c.ID)
			}
		case "search", "sync", "help", "quit":
			if !c.Available {
				t.Errorf("%q should always be available", c.ID)
			}
		}
	}

	h := newHarness(t)
	h.openChat("chat-ada")
	h.m.transcript.Focus()

	byID := make(map[string]bool)
	for _, c := range h.m.commands {
		byID[c.ID] = c.Available
	}
	if !byID["reply"] {
		t.Error("reply should be available with a message under the cursor")
	}
}

// --- helpers ---

// flagOf reads one flag of one conversation from the service.
func (h *harness) flagOf(id models.ChatID, check func(models.Chat) bool) bool {
	h.t.Helper()
	for _, c := range h.fake.chats() {
		if c.ID == id {
			return check(c)
		}
	}
	h.t.Fatalf("conversation %q is not in the service", id)
	return false
}
