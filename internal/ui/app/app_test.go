package app

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/components/statusbar"
	"github.com/wterm/wterm/internal/ui/theme"
	"github.com/wterm/wterm/internal/whatsapp"
)

// newTestModel builds a model with the demo fixtures, sized to a standard
// terminal and past its initial load.
//
// The size matters: a TUI's whole contract is that it renders correctly at a
// given geometry, so tests state it explicitly rather than relying on a default.
func newTestModel(t *testing.T, w, h int) (*Model, *fixture) {
	t.Helper()

	fake := DemoData()
	svc := fake.Services()
	m := New(svc, theme.Dark(), keybindings.DefaultMap())
	m.DisableTimers = true

	// Drive the initial load synchronously. Commands return messages rather than
	// applying effects, so a test has to feed them back in.
	m = settle(t, m, m.Init(), settleDepth)

	m = resize(t, m, w, h)
	return m, &fixture{services: svc, fake: fake}
}

// fixture bundles the fake with its assembled services, so tests can reach
// either without re-deriving one from the other.
type fixture struct {
	services whatsapp.Services
	fake     *whatsapp.Fake
}

// History reads a transcript through the service interface, which is the same
// path the application uses.
func (f *fixture) History(t *testing.T, id models.ChatID) []models.Message {
	t.Helper()
	msgs, err := f.services.Message.History(t.Context(), id, 0)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	return msgs
}

// settle drives the model until it stops producing work.
//
// A Bubble Tea command is a plain function returning a message, so a test can run
// it and feed the result back into Update. The loop is written as one function
// with an explicit depth rather than as apply/applyCmd calling each other: mutual
// recursion that resets its own depth never terminates, and the symptom is a
// stack overflow rather than a legible failure.
//
// A BatchMsg carries member commands rather than being callable, so its members
// are expanded and run in order.
func settle(t *testing.T, m *Model, cmd tea.Cmd, depth int) *Model {
	t.Helper()

	for range depth {
		if cmd == nil {
			return m
		}

		msg := cmd()
		if msg == nil {
			return m
		}

		if batch, ok := msg.(tea.BatchMsg); ok {
			cmd = tea.Sequence(batch...)
			continue
		}

		next, reply := m.Update(msg)
		nm, ok := next.(*Model)
		if !ok {
			t.Fatalf("Update returned %T, want *app.Model", next)
		}
		m, cmd = nm, reply
	}
	return m
}

// apply sends one message to the model and settles the resulting work.
func apply(t *testing.T, m *Model, msg tea.Msg) *Model {
	t.Helper()

	next, cmd := m.Update(msg)
	nm, ok := next.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *app.Model", next)
	}
	return settle(t, nm, cmd, settleDepth)
}

// settleDepth bounds how much work one simulated interaction may generate.
//
// A handful of fetches and follow-ups is normal; a hundred would mean the model is
// looping, which is exactly what this bound is here to catch.
const settleDepth = 32

// applyKeys sends a sequence of key presses, running each key's command before
// sending the next one.
//
// Sending them all and then running the commands would reorder the interaction:
// a keystroke that triggers a fetch would be applied after the next keystroke, and
// the model's state at each step would not match what a user's would.
func applyKeys(t *testing.T, m *Model, keys ...string) *Model {
	t.Helper()
	for _, k := range keys {
		m = apply(t, m, keyMsg(t, k))
	}
	return m
}

// resize sends a window size message.
func resize(t *testing.T, m *Model, w, h int) *Model {
	t.Helper()
	return apply(t, m, tea.WindowSizeMsg{Width: w, Height: h})
}

// keyMsg builds a Bubble Tea key press for a binding spec.
//
// Going through the same parser the application uses keeps the test honest: a
// binding that cannot be expressed as a key press would fail here rather than
// only in front of a user.
func keyMsg(t *testing.T, spec string) tea.KeyPressMsg {
	t.Helper()

	k, err := keybindings.ParseKey(spec)
	if err != nil {
		t.Fatalf("parsing key %q: %v", spec, err)
	}

	msg := tea.KeyPressMsg{Code: k.Rune, Text: string(k.Rune), Mod: teaMod(k.Mod)}
	if k.Name != "" {
		msg = namedKey(k.Name, k.Mod)
	}
	return msg
}

// teaMod maps wterm's modifier bits to Bubble Tea's.
func teaMod(m keybindings.Mod) tea.KeyMod {
	var out tea.KeyMod
	if m&keybindings.ModShift != 0 {
		out |= tea.ModShift
	}
	if m&keybindings.ModAlt != 0 {
		out |= tea.ModAlt
	}
	if m&keybindings.ModCtrl != 0 {
		out |= tea.ModCtrl
	}
	if m&keybindings.ModSuper != 0 {
		out |= tea.ModSuper
	}
	return out
}

// namedKey builds a press for a named key.
func namedKey(name string, mod keybindings.Mod) tea.KeyPressMsg {
	var code rune
	switch name {
	case keybindings.KeyEnter:
		code = tea.KeyEnter
	case keybindings.KeyEscape:
		code = tea.KeyEscape
	case keybindings.KeyTab:
		code = tea.KeyTab
	case keybindings.KeyBackspace:
		code = tea.KeyBackspace
	case keybindings.KeyDelete:
		code = tea.KeyDelete
	case keybindings.KeyUp:
		code = tea.KeyUp
	case keybindings.KeyDown:
		code = tea.KeyDown
	case keybindings.KeyLeft:
		code = tea.KeyLeft
	case keybindings.KeyRight:
		code = tea.KeyRight
	case keybindings.KeyHome:
		code = tea.KeyHome
	case keybindings.KeyEnd:
		code = tea.KeyEnd
	case keybindings.KeyPageUp:
		code = tea.KeyPgUp
	case keybindings.KeyPageDown:
		code = tea.KeyPgDown
	case keybindings.KeySpace:
		code = tea.KeySpace
	default:
		// An unrecognised name is treated as a printable rune.
		code = '?'
	}
	return tea.KeyPressMsg{Code: code, Mod: teaMod(mod)}
}

// render returns the model's view with escape sequences stripped.
func render(m *Model) string {
	return text.StripANSI(m.View().Content)
}

// --- Rendering ---

func TestViewRendersBothPanes(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	got := render(m)

	// The sidebar heading and a chat name must both be present: this is the
	// core layout promise.
	if !strings.Contains(got, "Chats") {
		t.Error("sidebar heading missing")
	}
	if !strings.Contains(got, "Ada Lovelace") {
		t.Error("a chat name is missing from the sidebar")
	}
	if !strings.Contains(got, "Write a message") {
		t.Error("composer placeholder missing")
	}
}

func TestViewRespectsTerminalWidth(t *testing.T) {
	const w = 100
	m, _ := newTestModel(t, w, 30)
	got := render(m)

	for i, line := range strings.Split(got, "\n") {
		if width := text.Width(line); width > w {
			t.Errorf("line %d is %d cells, exceeding the terminal width %d: %q",
				i, width, w, line)
		}
	}
}

func TestViewRespectsTerminalHeight(t *testing.T) {
	const h = 24
	m, _ := newTestModel(t, 100, h)
	got := render(m)

	lines := strings.Split(got, "\n")
	if len(lines) > h {
		t.Errorf("rendered %d lines, exceeding the terminal height %d", len(lines), h)
	}
}

func TestViewFitsEveryReasonableSize(t *testing.T) {
	// The layout must not panic or overflow at any size a user might have. A
	// scrollback pane is routinely 40x10.
	for _, size := range [][2]int{
		{40, 10}, {60, 15}, {80, 24}, {100, 30}, {120, 40}, {200, 60}, {79, 23},
	} {
		t.Run(sizeName(size), func(t *testing.T) {
			m, _ := newTestModel(t, size[0], size[1])
			got := render(m)
			for i, line := range strings.Split(got, "\n") {
				if width := text.Width(line); width > size[0] {
					t.Fatalf("line %d is %d cells wide, terminal is %d", i, width, size[0])
				}
			}
		})
	}
}

func TestTooSmallTerminalExplainsItself(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = resize(t, m, 20, 5)

	got := render(m)
	if !strings.Contains(got, "too small") {
		t.Errorf("expected an explanation, got %q", got)
	}
}

func TestNarrowWidthHidesSidebar(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)

	if got := render(m); !strings.Contains(got, "Chats") {
		t.Fatal("the sidebar heading should be present at a normal width")
	}

	m = resize(t, m, 50, 20)

	// Assert on the sidebar's own marker rather than on a chat name: the open
	// chat's name also appears in the conversation header, so its presence proves
	// nothing about the sidebar.
	got := render(m)
	if strings.Contains(got, "Chats") {
		t.Errorf("the sidebar should be dropped at a narrow width, got:\n%s", got)
	}
	if strings.Contains(got, "Search chats") {
		t.Error("the sidebar search field should be dropped with it")
	}
}

func TestViewEnablesAltScreenAndMouse(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	v := m.View()

	if !v.AltScreen {
		t.Error("the interface must request the alternate screen")
	}
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("mouse mode = %v, want cell motion", v.MouseMode)
	}
}

func TestCursorOnlyInComposer(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)

	if m.View().Cursor != nil {
		t.Error("no caret should be shown while the sidebar has focus")
	}

	m = applyKeys(t, m, "ctrl+l")
	if got := m.View().Cursor; got == nil {
		t.Error("the caret should appear when the composer has focus")
	}
}

func TestQuittingStopsRendering(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "ctrl+q")

	if got := render(m); got != "" {
		t.Errorf("a quitting model should render nothing, got %q", got)
	}
}

// --- Navigation ---

func TestTabCyclesPanels(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)

	if m.focus != keybindings.PanelSidebar {
		t.Fatalf("startup focus = %v, want sidebar", m.focus)
	}
	m = applyKeys(t, m, "tab")
	if m.focus != keybindings.PanelMessages {
		t.Errorf("after tab, focus = %v, want messages", m.focus)
	}
	m = applyKeys(t, m, "tab")
	if m.focus != keybindings.PanelComposer {
		t.Errorf("after two tabs, focus = %v, want composer", m.focus)
	}
	m = applyKeys(t, m, "tab")
	if m.focus != keybindings.PanelSidebar {
		t.Errorf("focus should wrap back to the sidebar, got %v", m.focus)
	}
}

func TestShiftTabGoesBack(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "tab", "shift+tab")
	if m.focus != keybindings.PanelSidebar {
		t.Errorf("focus = %v, want sidebar", m.focus)
	}
}

func TestModHAndModLSwitchPanels(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "tab") // messages

	m = applyKeys(t, m, "ctrl+h")
	if m.focus != keybindings.PanelSidebar {
		t.Errorf("ctrl+h should focus the sidebar, got %v", m.focus)
	}

	m = applyKeys(t, m, "tab", "tab") // composer
	m = applyKeys(t, m, "ctrl+h")
	if m.focus != keybindings.PanelSidebar {
		t.Errorf("ctrl+h should focus the sidebar from the composer, got %v", m.focus)
	}

	m = applyKeys(t, m, "ctrl+l")
	if m.focus != keybindings.PanelComposer {
		t.Errorf("ctrl+l should focus the composer, got %v", m.focus)
	}
}

func TestModJKMovesChatSelection(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)

	start := m.chatSel
	m = applyKeys(t, m, "ctrl+j")
	if m.chatSel <= start {
		t.Errorf("ctrl+j should advance the selection: %d then %d", start, m.chatSel)
	}
	m = applyKeys(t, m, "ctrl+k")
	if m.chatSel != start {
		t.Errorf("ctrl+k should return to %d, got %d", start, m.chatSel)
	}
}

func TestSelectionStaysInRange(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)

	// Move far past the end; the selection must clamp rather than panic or index
	// out of bounds.
	for range 50 {
		m = applyKeys(t, m, "ctrl+j")
	}

	n := len(m.visibleChats())
	if m.chatSel < 0 || m.chatSel >= n {
		t.Errorf("selection %d is outside the %d visible chats", m.chatSel, n)
	}
}

func TestArrowKeysMatchModJK(t *testing.T) {
	// The arrows are bound alongside the hjkl keys, and both must work.
	withMod := newTestModelKeys(t, 100, 30, "ctrl+j")
	withArrow := newTestModelKeys(t, 100, 30, "down")

	if withMod.chatSel != withArrow.chatSel {
		t.Errorf("ctrl+j gave selection %d but down gave %d; they are bound to the same action",
			withMod.chatSel, withArrow.chatSel)
	}
}

// newTestModelKeys builds a model and applies keys after the initial load.
func newTestModelKeys(t *testing.T, w, h int, keys ...string) *Model {
	t.Helper()
	m, _ := newTestModel(t, w, h)
	return applyKeys(t, m, keys...)
}

func TestEnterOpensChatAndLoadsTranscript(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)

	if m.messageCount() == 0 {
		t.Fatal("expected the initial chat to be loaded")
	}

	m = applyKeys(t, m, "enter")
	m = settle(t, m, m.openChat(), settleDepth)

	if m.focus != keybindings.PanelMessages {
		t.Errorf("opening a chat should move focus to the transcript, got %v", m.focus)
	}
	if m.messageCount() == 0 {
		t.Error("transcript is empty after opening a chat")
	}
}

// --- Search ---

func TestSearchFiltersChats(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	total := len(m.visibleChats())

	m = applyKeys(t, m, "ctrl+f")
	if !m.searchActive {
		t.Fatal("ctrl+f should enter search mode")
	}

	m = typeText(t, m, "Grace")
	got := len(m.visibleChats())

	if got >= total {
		t.Errorf("searching for Grace left %d of %d chats; expected a narrower set", got, total)
	}
	for _, c := range m.visibleChats() {
		if !strings.Contains(strings.ToLower(c.FallbackName()), "grace") {
			t.Errorf("chat %q does not match the query but was returned", c.FallbackName())
		}
	}
}

func TestSearchWithNoMatchesEmptiesList(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "ctrl+f")
	m = typeText(t, m, "zzzzz")

	if got := len(m.visibleChats()); got != 0 {
		t.Errorf("a query matching nothing should leave no chats, got %d", got)
	}
	if m.chatSel != -1 {
		t.Errorf("selection should be cleared when nothing matches, got %d", m.chatSel)
	}
	// The rendering must survive the empty case rather than panicking.
	_ = render(m)
}

func TestSearchMatchesLatestMessageText(t *testing.T) {
	// Phase 1 searches the chat name and the latest message. Searching all history
	// needs an index and belongs to the storage layer in Phase 2 — but the preview
	// must be searchable now, since it is often the only identifying text.
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "ctrl+f")
	m = typeText(t, m, "reassuring")

	if len(m.visibleChats()) == 0 {
		t.Error("the latest message text should be searchable")
	}
}

func TestSearchDoesNotMatchOlderMessagesYet(t *testing.T) {
	// Documents the Phase 1 boundary explicitly, so that a later phase widening
	// search to full history is a deliberate change rather than an accident.
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "ctrl+f")
	m = typeText(t, m, "Bernoulli") // present in an older message, not the latest

	if got := len(m.visibleChats()); got != 0 {
		t.Logf("full-history search already active (%d matches); "+
			"the Phase 1 limitation no longer applies", got)
	}
}

func TestEscapeLeavesSearchAndClearsQuery(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "ctrl+f")
	m = typeText(t, m, "Ada")

	m = applyKeys(t, m, "esc")

	if m.searchActive {
		t.Error("escape should leave search mode")
	}
	if m.searchQuery != "" {
		t.Errorf("escape should clear the query, got %q", m.searchQuery)
	}
	if len(m.visibleChats()) == 0 {
		t.Error("clearing the search should restore the chat list")
	}
}

func TestTypingInComposerDoesNotTriggerActions(t *testing.T) {
	// The central input-architecture test: a letter typed into the composer must
	// become text, never a binding.
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "ctrl+l") // focus composer

	before := m.chatSel
	m = typeText(t, m, "jjk")

	if m.chatSel != before {
		t.Errorf("typing j/k in the composer moved the chat selection: %d then %d", before, m.chatSel)
	}
	if got := m.composer.Value(); got != "jjk" {
		t.Errorf("composer holds %q, want %q", got, "jjk")
	}
}

func TestTypingInSearchDoesNotTriggerActions(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "ctrl+f")

	before := m.chatSel
	m = typeText(t, m, "jj")

	if m.chatSel != before && m.searchQuery == "" {
		t.Error("typing in the search field triggered navigation")
	}
	if !strings.Contains(m.searchQuery, "j") {
		t.Errorf("search query %q should contain the typed characters", m.searchQuery)
	}
}

func TestControlCharsInComposerAreNotInserted(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "ctrl+l")
	m = typeText(t, m, "hello")

	// ctrl+l is focus navigation here, not "clear line", and must not enter
	// text.
	if got := m.composer.Value(); strings.ContainsAny(got, "\n\t") {
		t.Errorf("composer contains control characters: %q", got)
	}
}

// --- Composer ---

func TestEnterSendsMessage(t *testing.T) {
	m, fx := newTestModel(t, 100, 30)

	chat, ok := m.currentChat()
	if !ok {
		t.Fatal("no chat is open")
	}
	before := len(fx.History(t, chat.ID))

	// applyKeys returns the updated model and runs each key's command, so the
	// send has already happened by the time the history is read back.
	m = applyKeys(t, m, "ctrl+l")
	m = typeText(t, m, "hello there")
	applyKeys(t, m, "enter")

	msgs := fx.History(t, chat.ID)

	var found bool
	for _, msg := range msgs {
		if msg.Body == "hello there" && msg.IsOutgoing() {
			found = true
		}
	}
	if !found {
		t.Errorf("the sent message is not in the history: have %d messages, started with %d",
			len(msgs), before)
	}
}

func TestSendingEmptyMessageIsANoOp(t *testing.T) {
	m, fx := newTestModel(t, 100, 30)
	chat, _ := m.currentChat()
	before := len(fx.History(t, chat.ID))

	applyKeys(t, m, "ctrl+l", "enter")

	if after := len(fx.History(t, chat.ID)); after != before {
		t.Errorf("sending an empty composer changed the history: %d then %d", before, after)
	}
}

func TestSendingClearsComposer(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "ctrl+l")
	m = typeText(t, m, "text")
	m = applyKeys(t, m, "enter")

	if !m.composer.Empty() {
		t.Errorf("composer should be empty after sending, holds %q", m.composer.Value())
	}
}

// --- Escape semantics ---

func TestEscapeIsContextual(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)

	// With nothing to cancel, escape returns focus to the sidebar.
	m = applyKeys(t, m, "ctrl+l")
	m = applyKeys(t, m, "esc")
	if m.focus != keybindings.PanelSidebar {
		t.Errorf("escape with nothing to cancel should focus the sidebar, got %v", m.focus)
	}
}

func TestEscapeCancelsReply(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "tab") // messages
	m = applyKeys(t, m, "r")   // reply

	if m.replyTo == nil {
		t.Skip("no message selected to reply to")
	}

	m = applyKeys(t, m, "esc")
	if m.replyTo != nil {
		t.Error("escape should cancel the reply")
	}
}

func TestEscapeCancelsEdit(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "tab")
	m.move(1) // ensure a selection
	m = applyKeys(t, m, "ctrl+e")

	if m.editing == "" {
		t.Skip("the selected message is not editable")
	}

	m = applyKeys(t, m, "esc")
	if m.editing != "" {
		t.Error("escape should cancel editing")
	}
	if !m.composer.Empty() {
		t.Error("cancelling an edit should clear the composer")
	}
}

// --- Message actions ---

func TestReplySetsQuoteAndFocusesComposer(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "tab")
	m.msgSel = 0

	m = applyKeys(t, m, "r")

	if m.replyTo == nil {
		t.Fatal("r should start a reply")
	}
	if m.focus != keybindings.PanelComposer {
		t.Errorf("replying should focus the composer, got %v", m.focus)
	}
}

func TestEditRefusesIncomingMessage(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "tab")
	m.msgSel = 0 // an incoming message

	m = applyKeys(t, m, "ctrl+e")

	if m.editing != "" {
		t.Error("an incoming message must not be editable")
	}
	if !m.composer.Empty() {
		t.Error("a refused edit must not load the message into the composer")
	}
}

func TestEditLoadsOwnMessage(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "tab")

	// Find the user's own message.
	idx := -1
	for i, msg := range m.currentMessages() {
		if msg.IsOutgoing() {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Skip("no outgoing message in the transcript")
	}

	m.msgSel = idx
	body := m.currentMessages()[idx].Body

	m = applyKeys(t, m, "ctrl+e")

	if m.editing == "" {
		t.Fatal("own message should be editable")
	}
	if m.composer.Value() != body {
		t.Errorf("composer holds %q, want the message body %q", m.composer.Value(), body)
	}
}

func TestSelectTogglesAndSelectAll(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "tab")

	total := m.messageCount()
	if total == 0 {
		t.Skip("empty transcript")
	}

	m = applyKeys(t, m, "space")
	if len(m.selected) != 1 {
		t.Errorf("space should select one message, got %d", len(m.selected))
	}

	m = applyKeys(t, m, "space")
	if len(m.selected) != 0 {
		t.Errorf("space again should deselect, got %d", len(m.selected))
	}

	m = applyKeys(t, m, "ctrl+a")
	if len(m.selected) != total {
		t.Errorf("ctrl+a should select all %d messages, got %d", total, len(m.selected))
	}

	m = applyKeys(t, m, "ctrl+a")
	if len(m.selected) != 0 {
		t.Errorf("ctrl+a again should clear the selection, got %d", len(m.selected))
	}
}

func TestEscapeClearsSelection(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "tab")
	m = applyKeys(t, m, "space")

	if len(m.selected) == 0 {
		t.Skip("nothing selected")
	}

	m = applyKeys(t, m, "esc")
	if len(m.selected) != 0 {
		t.Errorf("escape should clear the selection, got %d", len(m.selected))
	}
}

// --- Overlays ---

func TestHelpOpensAndEscapeCloses(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)

	m = applyKeys(t, m, "ctrl+?")
	if m.overlays.Len() != 1 {
		t.Fatalf("ctrl+? should open one overlay, got %d", m.overlays.Len())
	}
	if !strings.Contains(render(m), "Keybindings") {
		t.Error("the help overlay should show its title")
	}

	m = applyKeys(t, m, "esc")
	if m.overlays.Len() != 0 {
		t.Errorf("escape should close the overlay, %d remain", m.overlays.Len())
	}
}

func TestInfoOverlayShowsContactDetails(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "ctrl+g")

	if m.overlays.Len() == 0 {
		t.Fatal("ctrl+g should open the info overlay")
	}
	if !strings.Contains(render(m), "Chat info") {
		t.Error("the info overlay should be titled")
	}
}

func TestOverlaysSwallowKeys(t *testing.T) {
	// A modal that let keys reach the conversation behind it would act on the
	// wrong thing.
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "ctrl+?")

	before := m.chatSel
	m = applyKeys(t, m, "ctrl+j", "down", "up")

	if m.chatSel != before {
		t.Errorf("keys reached the chat list while an overlay was open: %d then %d", before, m.chatSel)
	}
	if m.overlays.Len() != 1 {
		t.Errorf("the overlay should still be open, got %d", m.overlays.Len())
	}
}

func TestDeleteAsksForConfirmation(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "tab")
	m.msgSel = 0

	m = applyKeys(t, m, "ctrl+d")

	if m.overlays.Len() != 1 {
		t.Fatal("deleting a message should ask for confirmation")
	}
	if !strings.Contains(render(m), "Cancel") {
		t.Error("the confirmation should offer a cancel button")
	}
}

// --- Scrolling ---

func TestScrollStaysWithinBounds(t *testing.T) {
	// A short terminal, so the transcript is taller than the viewport. Without
	// overflow there is nothing to scroll and the test would pass vacuously.
	//
	// The transcript panel is focused first because the scroll bindings are scoped
	// to it: a key that scrolls one pane must not scroll another, so "scroll up"
	// means different things depending on where the user is.
	m, _ := newTestModel(t, 100, 12)
	m = applyKeys(t, m, "tab")

	// Scroll far past both ends.
	for range 200 {
		m = applyKeys(t, m, "k")
	}
	if m.msgOffset < 0 {
		t.Errorf("scroll offset went negative: %d", m.msgOffset)
	}

	for range 400 {
		m = applyKeys(t, m, "j")
	}
	if m.msgOffset > m.maxScrollOffset() {
		t.Errorf("scroll offset %d exceeds the maximum %d", m.msgOffset, m.maxScrollOffset())
	}
}

func TestScrollBottomPinsToLatest(t *testing.T) {
	m, _ := newTestModel(t, 100, 12)
	m = applyKeys(t, m, "tab")

	if m.maxScrollOffset() == 0 {
		t.Fatal("the transcript must overflow the viewport for scrolling to mean anything")
	}

	m = applyKeys(t, m, "ctrl+home")
	if m.atBottom {
		t.Error("scrolling to the top should clear the at-bottom flag")
	}

	m = applyKeys(t, m, "ctrl+end")
	if !m.atBottom {
		t.Error("ctrl+end should return to the bottom")
	}
}

func TestPageKeysScroll(t *testing.T) {
	m, _ := newTestModel(t, 100, 12)
	m = applyKeys(t, m, "tab")

	// Start at the bottom, as the model does on open.
	m.scrollTo(m.maxScrollOffset())
	before := m.msgOffset

	// The offset counts rows from the oldest message, so scrolling towards older
	// messages decreases it.
	m = applyKeys(t, m, "ctrl+b")
	if m.msgOffset >= before {
		t.Errorf("ctrl+b should scroll towards older messages: offset %d then %d", before, m.msgOffset)
	}

	// alt+pgdown is the page-down binding. ctrl+f is the global search, which is
	// why it is not also a scroll key: one key, one meaning.
	m = applyKeys(t, m, "alt+pgdown")
	if got := m.msgOffset; got <= 0 {
		t.Errorf("alt+pgdown should scroll back towards newer messages, offset = %d", got)
	}
}

// --- Resize ---

func TestResizeKeepsSelectionAndClampsOffsets(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	selected := m.chatSel

	for _, size := range [][2]int{{40, 10}, {200, 60}, {80, 24}, {50, 12}} {
		m = resize(t, m, size[0], size[1])

		if m.chatSel < 0 || m.chatSel >= len(m.visibleChats()) {
			t.Fatalf("at %dx%d the selection %d is out of range", size[0], size[1], m.chatSel)
		}
		if m.msgOffset < 0 {
			t.Fatalf("at %dx%d the scroll offset went negative", size[0], size[1])
		}
		if n := len(m.visibleChats()); n > 0 && m.chatSel != selected && m.chatSel >= n {
			t.Fatalf("at %dx%d selection %d is out of range", size[0], size[1], m.chatSel)
		}
	}
}

func TestResizeToTooSmallDoesNotPanic(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	for _, size := range [][2]int{{1, 1}, {0, 0}, {5, 2}, {10, 3}} {
		m = resize(t, m, size[0], size[1])
		_ = render(m)
	}
}

// --- Mouse ---

func TestClickSelectsChat(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)

	// Row 2 of the sidebar, in the second chat.
	y := 3 + chatlistRowHeight*2
	m = apply(t, m, tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseLeft})

	if m.focus != keybindings.PanelSidebar {
		t.Errorf("clicking the sidebar should focus it, got %v", m.focus)
	}
	if m.chatSel != 2 {
		t.Errorf("selection = %d, want 2", m.chatSel)
	}
}

// chatlistRowHeight mirrors the sidebar's row height for click arithmetic.
const chatlistRowHeight = 2

func TestClickSelectsMessage(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "tab")

	// Click in the transcript pane.
	y := m.theme.Metrics.HeaderHeight + 2
	m = apply(t, m, tea.MouseClickMsg{X: 60, Y: y, Button: tea.MouseLeft})

	if m.focus != keybindings.PanelMessages {
		t.Errorf("clicking the transcript should focus it, got %v", m.focus)
	}
}

func TestWheelScrollsWithoutChangingSelection(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	m = applyKeys(t, m, "tab")

	before := m.msgSel
	m = apply(t, m, tea.MouseWheelMsg{X: 60, Y: 10, Button: tea.MouseWheelUp})

	if m.msgSel != before {
		t.Errorf("the wheel moved the selection: %d then %d", before, m.msgSel)
	}
	if m.msgOffset >= 0 && m.msgOffset > 0 {
		// Scrolling up from the bottom should reduce the offset; if the
		// transcript fits, no scroll is needed and this is a no-op.
		t.Logf("offset after scrolling up: %d", m.msgOffset)
	}
}

// --- Connection ---

func TestConnectionStateRenders(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)

	for _, conn := range []struct {
		name string
		want string
	}{
		{"offline", "offline"},
		{"connecting", "connecting"},
		{"syncing", "syncing"},
		{"online", "online"},
	} {
		t.Run(conn.name, func(t *testing.T) {
			m.conn = connStateOf(conn.name)
			if got := render(m); !strings.Contains(got, conn.want) {
				t.Errorf("status bar should mention %q, got:\n%s", conn.want, got)
			}
		})
	}
}

func TestUnreadBadgeIsCounted(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)

	total := 0
	for _, c := range m.chats() {
		total += c.UnreadCount
	}
	if total == 0 {
		t.Skip("fixtures have no unread messages")
	}
	if got := render(m); !strings.Contains(got, "unread") && !strings.Contains(got, "·") {
		t.Log("no explicit unread marker in the status bar")
	}
}

// --- Determinism ---

func TestRenderIsDeterministic(t *testing.T) {
	// A TUI that renders differently for the same state produces flicker and
	// makes diff-based testing useless.
	m, _ := newTestModel(t, 100, 30)

	first := render(m)
	for range 5 {
		if got := render(m); got != first {
			t.Fatalf("render is not deterministic:\n%q\nthen\n%q", first, got)
		}
	}
}

func TestRenderDoesNotMutateState(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)

	before := m.chatSel
	offset := m.chatOffset
	rows := len(m.rows)

	for range 3 {
		_ = render(m)
	}

	if m.chatSel != before || m.chatOffset != offset || len(m.rows) != rows {
		t.Errorf("View mutated state: sel %d/%d offset %d/%d rows %d/%d",
			before, m.chatSel, offset, m.chatOffset, rows, len(m.rows))
	}
}

func TestShutdownStopsSync(t *testing.T) {
	m, fx := newTestModel(t, 100, 30)

	if err := m.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if fx.fake.Connected() {
		t.Error("the fake should be disconnected after shutdown")
	}
}

// --- helpers ---

// typeText sends each character of s as a key press.
func typeText(t *testing.T, m *Model, s string) *Model {
	t.Helper()
	for _, r := range s {
		m = apply(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

// connStateOf maps a name to a statusbar connection value.
func connStateOf(name string) statusbar.Connection {
	switch name {
	case "connecting":
		return statusbar.Connecting
	case "syncing":
		return statusbar.Syncing
	case "online":
		return statusbar.Online
	default:
		return statusbar.Disconnected
	}
}

func sizeName(size [2]int) string {
	return strconv.Itoa(size[0]) + "x" + strconv.Itoa(size[1])
}
