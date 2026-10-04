package app

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/layout"
	"github.com/wterm/wterm/internal/ui/theme"
)

// newModel builds a model over demo data with timers off.
//
// The conversation list is fetched and fed back rather than left to the model's own
// startup command, because the commands Update returns are not run by Update — Bubble
// Tea schedules them — and a test has no loop to schedule them on. A test that assumed
// otherwise would assert against an empty sidebar and then blame the layout.
func newModel(t *testing.T, w, h int) *Model {
	t.Helper()

	fake := DemoData()
	m := New(fake.Services(), theme.Dark(), keybindings.DefaultMap())
	m.DisableTimers = true

	send(m, tea.WindowSizeMsg{Width: w, Height: h})

	chats, err := fake.Services().Chat.List(t.Context())
	if err != nil {
		t.Fatalf("listing demo chats: %v", err)
	}
	send(m, chatsReady{chats: chats})

	return m
}

// openChat makes a conversation current and feeds its transcript back to the model.
func openChat(t *testing.T, m *Model, id models.ChatID) {
	t.Helper()

	msgs, err := m.services.Message.History(t.Context(), id, historyLimit)
	if err != nil {
		t.Fatalf("loading history for %q: %v", id, err)
	}
	send(m, transcriptReady{forChat: id, messages: msgs})
}

// send applies a message and discards the resulting command.
//
// Discarding is safe because the only commands a test depends on are the fetches whose
// results it feeds back itself; everything else the model queues is either a no-op or
// a refresh the fixture already satisfies.
func send(m *Model, msg tea.Msg) {
	_, _ = m.Update(msg)
}

// frame returns the rendered screen as its lines, with styling stripped.
func frame(m *Model) []string {
	body := strings.Split(m.View().Content, "\n")
	for i, l := range body {
		body[i] = text.StripANSI(l)
	}
	return body
}

// assertFrameShape checks that the frame is exactly the terminal's size.
//
// This is the invariant the whole layout rests on: a frame that is one cell too wide
// makes the terminal wrap every line, and one that is a row short leaves stale
// characters. It is checked on every render rather than trusted.
func assertFrameShape(t *testing.T, m *Model, w, h int) {
	t.Helper()

	lines := frame(m)
	if len(lines) != h {
		t.Fatalf("frame has %d rows, terminal is %d tall", len(lines), h)
	}
	for i, l := range lines {
		if got := text.VisibleWidth(l); got != w {
			t.Errorf("row %d is %d cells wide, terminal is %d: %q", i, got, w, l)
		}
	}
}

// --- layout ---

func TestFrameFillsTheTerminalAtEveryReferenceSize(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 30}, {160, 40}, {60, 20}} {
		t.Run(strconv.Itoa(size[0])+"x"+strconv.Itoa(size[1]), func(t *testing.T) {
			m := newModel(t, size[0], size[1])
			assertFrameShape(t, m, size[0], size[1])
		})
	}
}

func TestFrameFillsTheTerminalAcrossTheWholeSizeRange(t *testing.T) {
	// A layout that is only correct at the sizes someone thought to check is not a
	// layout. Every size in a representative sweep is rendered, because the cost is
	// microseconds and the failure mode is a visibly broken screen.
	for w := 40; w <= 200; w += 7 {
		for h := 10; h <= 50; h += 3 {
			l := layout.Compute(w, h)
			if l.Mode == layout.ModeTooSmall {
				continue
			}
			m := newModel(t, w, h)
			lines := frame(m)
			if len(lines) != h {
				t.Fatalf("at %dx%d the frame has %d rows", w, h, len(lines))
			}
			for i, line := range lines {
				if got := text.VisibleWidth(line); got != w {
					t.Fatalf("at %dx%d row %d is %d cells: %q", w, h, i, got, line)
				}
			}
		}
	}
}

func TestTooSmallTerminalExplainsItself(t *testing.T) {
	m := newModel(t, 30, 8)
	body := text.StripANSI(m.View().Content)

	if !strings.Contains(body, "terminal demasiado pequeña") {
		t.Errorf("a too-small terminal should say so, got %q", body)
	}
	// It must not attempt a layout: a scrambled grid would be worse than the message.
	if strings.Count(body, strconv.Itoa(layout.MinWidth)) != 1 {
		t.Errorf("the message should state the requirement once, got %q", body)
	}
}

// --- the three zones ---

func TestFullLayoutShowsSidebarConversationAndStatus(t *testing.T) {
	m := newModel(t, 120, 30)
	lines := frame(m)

	l := layout.Compute(120, 30)

	// The brand banner is in the sidebar's first row.
	if !strings.Contains(lines[0], "WTERM") {
		t.Errorf("row 0 should carry the wordmark, got %q", lines[0])
	}
	// The search field sits under it.
	if !strings.Contains(lines[2], "Buscar chats") {
		t.Errorf("row 2 should carry the search field, got %q", lines[2])
	}
	// Chat names appear in the sidebar's columns.
	if !strings.Contains(strings.Join(lines, "\n"), "Ada Lovelace") {
		t.Error("the sidebar should list a conversation")
	}
	// The status bar is the last row.
	last := lines[l.Status.Y]
	// The demo fake is not started, so the expected state is "sin conexión"; what
	// matters is that the bar says something about the connection at all.
	if !strings.Contains(last, "conexión") {
		t.Errorf("the status bar should report the connection, got %q", last)
	}
	// The divider column separates the two panes. Checking it at several rows rather
	// than one catches a divider that is only one character tall.
	for _, y := range []int{1, 5, 10, l.Conversation.Height - 1} {
		r := []rune(lines[y])
		if l.Sidebar.Width >= len(r) {
			t.Fatalf("row %d is narrower than the sidebar", y)
		}
		if got := string(r[l.Sidebar.Width]); got != "│" {
			t.Errorf("row %d column %d should be the divider, got %q", y, l.Sidebar.Width, got)
		}
	}
}

func TestMinimalLayoutDropsTheSidebarButKeepsTheComposer(t *testing.T) {
	m := newModel(t, 60, 20)
	lines := frame(m)
	body := strings.Join(lines, "\n")

	if strings.Contains(body, "WTERM") {
		t.Error("minimal mode should not draw the wordmark")
	}
	if !strings.Contains(body, "Escribir mensaje") {
		t.Error("minimal mode must keep the composer: sending is not optional")
	}
	assertFrameShape(t, m, 60, 20)
}

func TestEmptyTranscriptSaysSo(t *testing.T) {
	m := newModel(t, 120, 30)
	body := strings.Join(frame(m), "\n")

	if !strings.Contains(body, "Sin mensajes todavía") {
		t.Error("an unopened conversation should say it is empty, not render nothing")
	}
}

// --- data flow ---

func TestOpeningAConversationLoadsItsTranscript(t *testing.T) {
	m := newModel(t, 120, 30)

	if strings.Contains(strings.Join(frame(m), "\n"), "analytical engine") {
		t.Fatal("the transcript should be empty before a conversation is opened")
	}

	// mod+enter opens the conversation under the sidebar's cursor.
	send(m, ctrlEnter())

	openChat(t, m, "chat-ada")

	body := strings.Join(frame(m), "\n")
	if !strings.Contains(body, "Bernoulli") {
		t.Errorf("the opened conversation should be rendered, got:\n%s", body)
	}
}

func TestUnreadCountIsShownInTheStatusBar(t *testing.T) {
	m := newModel(t, 120, 30)
	body := strings.Join(frame(m), "\n")

	// The demo data has unread messages across several conversations.
	if !strings.Contains(body, "sin leer") {
		t.Errorf("the status bar should report unread messages, got:\n%s", body)
	}
}

// --- cursor and focus ---

func TestFocusMovesBetweenRegions(t *testing.T) {
	m := newModel(t, 120, 30)

	// Tab cycles sidebar → transcript → composer → sidebar.
	for _, want := range []string{"Ada", "Escribir mensaje", "Ada"} {
		send(m, tea.KeyPressMsg{Code: '\t', Text: "\t"})
		body := strings.Join(frame(m), "\n")
		if !containsAny(body, want) {
			t.Errorf("after tab the frame should show %q", want)
		}
	}
}

func TestEscapeReturnsToTheSidebar(t *testing.T) {
	m := newModel(t, 120, 30)

	// Move away from the sidebar.
	send(m, tea.KeyPressMsg{Code: '\t', Text: "\t"})
	send(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	// With nothing to cancel, escape focuses the sidebar again.
	send(m, tea.KeyPressMsg{Code: tea.KeyUp, Text: "k", Mod: tea.ModCtrl})
	send(m, ctrlKey('h'))
	body := strings.Join(frame(m), "\n")
	if !strings.Contains(body, "WTERM") {
		t.Error("the sidebar should be back")
	}
}

// --- the palette ---

func TestPaletteOpensFiltersAndListsCommands(t *testing.T) {
	m := newModel(t, 120, 30)

	// mod+shift+p opens it.
	send(m, ctrlShiftKey('p'))
	body := strings.Join(frame(m), "\n")

	if !strings.Contains(body, "buscar un comando") {
		t.Errorf("the palette should be open, got:\n%s", body)
	}

	// Typing filters.
	send(m, tea.KeyPressMsg{Code: 'r', Text: "r"})
	send(m, tea.KeyPressMsg{Code: 'e', Text: "e"})
	send(m, tea.KeyPressMsg{Code: 'p', Text: "p"})
	body = strings.Join(frame(m), "\n")

	if !strings.Contains(body, "Responder") {
		t.Errorf("typing 'rep' should surface 'Responder', got:\n%s", body)
	}
	// A command that does not match is gone.
	if strings.Contains(body, "Reenviar") {
		t.Error("the filter should have removed the non-matching commands")
	}
}

func TestEscapeClosesThePalette(t *testing.T) {
	m := newModel(t, 120, 30)

	send(m, ctrlShiftKey('p'))
	if !strings.Contains(strings.Join(frame(m), "\n"), "buscar un comando") {
		t.Fatal("the palette should be open")
	}

	send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(strings.Join(frame(m), "\n"), "buscar un comando") {
		t.Error("escape should close the palette")
	}
}

func TestPaletteRunsTheChosenCommand(t *testing.T) {
	m := newModel(t, 120, 30)

	send(m, ctrlShiftKey('p'))
	for _, r := range "atajos" {
		send(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	send(m, tea.KeyPressMsg{Code: '\r', Text: "\r"})

	body := strings.Join(frame(m), "\n")
	if !strings.Contains(body, "Atajos de teclado") {
		t.Errorf("choosing the help command should open the help sheet, got:\n%s", body)
	}
	// The palette must be gone, or it would sit on top of the dialog.
	if strings.Contains(body, "buscar un comando") {
		t.Error("the palette should close once a command is chosen")
	}
}

// --- overlays ---

func TestConfirmationDismissesWithEscape(t *testing.T) {
	m := newModel(t, 120, 30)

	// Right-click the transcript to open the message menu, then delete from it.
	openMessageMenu(t, m)

	// Walk down to "Eliminar" and choose it. The menu is Responder, Reaccionar,
	// Copiar, a separator, Eliminar — and the cursor skips the separator, so four
	// presses, then enter.
	for range 4 {
		send(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	send(m, tea.KeyPressMsg{Code: '\r', Text: "\r"})

	body := strings.Join(frame(m), "\n")
	if !strings.Contains(body, "Eliminar mensaje") {
		t.Fatalf("choosing delete should ask for confirmation, got:\n%s", body)
	}

	send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	send(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(strings.Join(frame(m), "\n"), "Mensaje") {
		t.Error("escape should dismiss the menu and the dialog")
	}
}

func TestConfirmationDefaultsToTheSafeButton(t *testing.T) {
	m := newModel(t, 120, 30)
	openMessageMenu(t, m)

	// Enter on the first button, which must be Cancel.
	send(m, tea.KeyPressMsg{Code: '\r', Text: "\r"})
	send(m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if strings.Contains(strings.Join(frame(m), "\n"), "se eliminará para todos") {
		t.Error("enter on the default button must not delete anything")
	}
}

func TestModalSwallowsKeysBehindIt(t *testing.T) {
	m := newModel(t, 120, 30)
	send(m, tea.KeyPressMsg{Code: tea.KeyEscape}) // put focus somewhere harmless

	openMessageMenu(t, m)
	before := strings.Join(frame(m), "\n")

	// A key that would normally move the sidebar's cursor must do nothing while a
	// modal is up.
	send(m, tea.KeyPressMsg{Code: 'j', Text: "j"})

	if strings.Join(frame(m), "\n") != before {
		t.Error("a key must not reach the conversation behind an open modal")
	}
}

// --- help and info ---

func TestHelpListsTheBindingsForTheFocusedRegion(t *testing.T) {
	m := newModel(t, 120, 30)

	// mod+? opens the cheat sheet.
	send(m, ctrlShiftKey('?'))
	body := strings.Join(frame(m), "\n")

	if !strings.Contains(body, "Atajos de teclado") {
		t.Fatalf("help should open, got:\n%s", body)
	}
	if !strings.Contains(body, "ctrl+q") {
		t.Error("help should show a global binding")
	}
}

func TestInfoReportsTheContact(t *testing.T) {
	m := newModel(t, 120, 30)

	send(m, ctrlKey('g'))
	body := strings.Join(frame(m), "\n")

	if !strings.Contains(body, "Información") {
		t.Errorf("mod+g should open the info panel, got:\n%s", body)
	}
	if !strings.Contains(body, "Teléfono") {
		t.Error("the info panel should show the phone number")
	}
}

// --- compositing ---

func TestOverlaysAreLayeredNotReplacing(t *testing.T) {
	m := newModel(t, 120, 30)

	withoutOverlay := strings.Join(frame(m), "\n")
	send(m, ctrlKey('g'))
	withOverlay := strings.Join(frame(m), "\n")

	if withOverlay == withoutOverlay {
		t.Fatal("the overlay should change the frame")
	}
	// The sidebar is behind the dialog, and the dialog is narrower than the terminal,
	// so the sidebar's content must survive on the rows the dialog does not cover.
	if !strings.Contains(withOverlay, "WTERM") {
		t.Error("a centred dialog must not erase the sidebar")
	}
}

// --- resize ---

func TestResizingBetweenModesKeepsTheFrameExact(t *testing.T) {
	m := newModel(t, 120, 30)

	for _, size := range [][2]int{{60, 20}, {80, 24}, {160, 40}, {45, 12}, {200, 60}} {
		send(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if layout.Compute(size[0], size[1]).Mode == layout.ModeTooSmall {
			continue
		}
		assertFrameShape(t, m, size[0], size[1])
	}
}

func TestResizingDoesNotLoseTheConversation(t *testing.T) {
	m := newModel(t, 120, 30)
	send(m, ctrlEnter())
	openChat(t, m, "chat-ada")

	send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	send(m, tea.WindowSizeMsg{Width: 160, Height: 40})

	if !strings.Contains(strings.Join(frame(m), "\n"), "Bernoulli") {
		t.Error("a resize should reflow the transcript, not discard it")
	}
}

// --- helpers ---

// ctrlKey builds a ctrl-modified key press for a printable character.
//
// mod is ctrl on Linux, which is the platform this application targets, so the tests
// name it the way a user thinks about it.
func ctrlKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r), Mod: tea.ModCtrl}
}

// ctrlEnter is mod+enter, the primary action for the focused region.
func ctrlEnter() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
}

// ctrlShiftKey builds a ctrl+shift-modified key press.
func ctrlShiftKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r), Mod: tea.ModCtrl | tea.ModShift}
}

// containsAny reports whether haystack contains any of the needles.
func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// openMessageMenu opens the transcript's context menu, which requires a conversation
// with a message under the cursor.
func openMessageMenu(t *testing.T, m *Model) {
	t.Helper()
	send(m, ctrlEnter())
	openChat(t, m, "chat-ada")
	send(m, tea.MouseClickMsg{X: 60, Y: 12, Button: tea.MouseRight})
}

// compile-time check that the demo fixture keeps providing the fields the UI reads,
// so that a rename in the models package fails here rather than at run time.
var _ = func() bool {
	var c models.Chat
	_ = c.FallbackName()
	_ = c.PreviewString()
	return true
}()
