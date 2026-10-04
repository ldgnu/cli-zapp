package transcript_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/component"
	"github.com/wterm/wterm/internal/ui/components/transcript"
	"github.com/wterm/wterm/internal/ui/layout"
	"github.com/wterm/wterm/internal/ui/theme"
)

// rect builds a transcript's rectangle.
func rect(w, h int) layout.Rect { return layout.Rect{Width: w, Height: h} }

// render returns the transcript's frame with styling stripped.
func render(t *testing.T, tr *transcript.Model, r layout.Rect) []string {
	t.Helper()
	lines := strings.Split(tr.View(), "\n")
	if len(lines) != r.Height {
		t.Fatalf("produced %d rows, rectangle is %d", len(lines), r.Height)
	}
	for i, l := range lines {
		lines[i] = text.StripANSI(l)
		if got := text.VisibleWidth(lines[i]); got != r.Width {
			t.Errorf("row %d is %d cells, rectangle is %d: %q", i, got, r.Width, lines[i])
		}
	}
	return lines
}

// conversation builds a transcript from a list of bodies, alternating direction.
func conversation(t *testing.T, r layout.Rect, bodies ...string) *transcript.Model {
	t.Helper()

	now := time.Now()
	msgs := make([]models.Message, 0, len(bodies))
	for i, body := range bodies {
		outgoing := i%2 == 1
		sender := models.ContactID("them")
		if outgoing {
			sender = "me"
		}
		msgs = append(msgs, models.Message{
			ID:        models.MessageID("m" + itoa(i)),
			SenderID:  sender,
			Direction: direction(outgoing),
			Body:      body,
			Kind:      models.KindText,
			Timestamp: now.Add(time.Duration(i) * time.Minute),
			Status:    status(outgoing),
		})
	}

	tr := transcript.New(keybindings.DefaultMap(), theme.Dark())
	tr.SetMode(layout.ModeFull)
	tr.Resize(r)
	tr.SetModel(component.Model{Messages: msgs})
	return tr
}

func direction(outgoing bool) models.MessageDirection {
	if outgoing {
		return models.DirectionOutgoing
	}
	return models.DirectionIncoming
}

func status(outgoing bool) models.DeliveryStatus {
	if outgoing {
		return models.DeliveryRead
	}
	return models.DeliveryPending
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// --- shape ---

func TestViewFillsItsRectangle(t *testing.T) {
	for _, size := range [][2]int{{40, 10}, {60, 20}, {90, 24}} {
		tr := conversation(t, rect(size[0], size[1]), "hola", "buenos días", "¿cómo estás?")
		render(t, tr, rect(size[0], size[1]))
	}
}

func TestEmptyTranscriptFillsItsRectangle(t *testing.T) {
	r := rect(50, 12)
	tr := transcript.New(keybindings.DefaultMap(), theme.Dark())
	tr.SetMode(layout.ModeFull)
	tr.Resize(r)
	tr.SetModel(component.Model{})

	lines := render(t, tr, r)
	if !strings.Contains(strings.Join(lines, "\n"), "Sin mensajes") {
		t.Error("an empty transcript should say so")
	}
	// Exactly one line of it, centred — not one per row.
	if n := strings.Count(strings.Join(lines, "\n"), "Sin mensajes"); n != 1 {
		t.Errorf("the notice appears on %d rows", n)
	}
}

func TestZeroSizedRectangleRendersNothing(t *testing.T) {
	tr := conversation(t, rect(0, 0), "hola")
	if tr.View() != "" {
		t.Error("a region with no area should render nothing")
	}
}

// --- rendering ---

func TestBodiesAreRendered(t *testing.T) {
	tr := conversation(t, rect(60, 20), "hola", "buenos días")
	body := strings.Join(render(t, tr, rect(60, 20)), "\n")

	for _, want := range []string{"hola", "buenos días"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q is missing from:\n%s", want, body)
		}
	}
}

func TestOutgoingMessagesAreRightAligned(t *testing.T) {
	tr := conversation(t, rect(60, 20), "entrante", "saliente")
	lines := render(t, tr, rect(60, 20))

	var incoming, outgoing string
	for _, l := range lines {
		if strings.Contains(l, "entrante") {
			incoming = l
		}
		if strings.Contains(l, "saliente") {
			outgoing = l
		}
	}
	if incoming == "" || outgoing == "" {
		t.Fatalf("both messages should be visible:\n%s", strings.Join(lines, "\n"))
	}

	// A bubble's text starts where its content starts; an outgoing one is pushed
	// towards the right edge. Comparing the start columns is the direct assertion.
	in := text.VisibleWidth(incoming) - text.VisibleWidth(strings.TrimLeft(incoming, " "))
	_ = in
	if strings.Index(incoming, "entrante") >= strings.Index(outgoing, "saliente") {
		t.Errorf("the outgoing message should start further right:\n%q\n%q", incoming, outgoing)
	}
}

func TestTimestampAppearsOncePerRun(t *testing.T) {
	// Three consecutive messages from one sender is one utterance. Stamping each of
	// them would triple the height of the exchange, which on a 24-row terminal is the
	// difference between reading a conversation and scrolling one.
	now := time.Now()
	msgs := make([]models.Message, 0, 3)
	for i := range 3 {
		msgs = append(msgs, models.Message{
			ID:        models.MessageID("m" + itoa(i)),
			SenderID:  "them",
			Direction: models.DirectionIncoming,
			Body:      "linea " + itoa(i),
			Kind:      models.KindText,
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}

	r := rect(60, 20)
	tr := transcript.New(keybindings.DefaultMap(), theme.Dark())
	tr.SetMode(layout.ModeFull)
	tr.Resize(r)
	tr.SetModel(component.Model{Messages: msgs})

	// The stamp belongs to the last message of the run — that is the one that closes
	// the utterance.
	stamp := models.FormatMessageTimestamp(now.Add(2 * time.Minute))
	body := strings.Join(render(t, tr, r), "\n")
	if n := strings.Count(body, stamp); n != 1 {
		t.Errorf("the timestamp %q appears %d times, want 1:\n%s", stamp, n, body)
	}
}

func TestLongMessagesWrapInsideTheBubble(t *testing.T) {
	body := strings.Repeat("palabra ", 60)
	tr := conversation(t, rect(50, 20), body)
	for i, l := range render(t, tr, rect(50, 20)) {
		if strings.Contains(l, "palabra") && text.VisibleWidth(l) > 50 {
			t.Errorf("row %d overflows the pane at %d cells", i, text.VisibleWidth(l))
		}
	}
}

func TestRevokedMessageLeavesATombstone(t *testing.T) {
	now := time.Now()
	r := rect(60, 12)
	tr := transcript.New(keybindings.DefaultMap(), theme.Dark())
	tr.SetMode(layout.ModeFull)
	tr.Resize(r)
	tr.SetModel(component.Model{Messages: []models.Message{
		{
			ID: "a", SenderID: "them", Direction: models.DirectionIncoming,
			Body: "secreto", Kind: models.KindText, Timestamp: now,
		},
		{
			ID: "b", SenderID: "them", Direction: models.DirectionIncoming,
			Body: "secreto", Kind: models.KindText, Timestamp: now, Revoked: true,
		},
	}})

	body := strings.Join(render(t, tr, r), "\n")
	if strings.Contains(body, "secreto") && strings.Count(body, "secreto") > 1 {
		t.Error("a revoked message must not still be readable")
	}
	if !strings.Contains(body, "eliminado") {
		t.Errorf("a revoked message should leave a tombstone:\n%s", body)
	}
}

func TestEscapeSequencesInABodyNeverReachTheScreen(t *testing.T) {
	// A message body arrives over the network and is drawn directly. One containing
	// an escape sequence could repaint the screen, move the cursor, or set the
	// terminal title — so sanitising is a security boundary, not cosmetics.
	r := rect(60, 12)
	tr := transcript.New(keybindings.DefaultMap(), theme.Dark())
	tr.SetMode(layout.ModeFull)
	tr.Resize(r)
	tr.SetModel(component.Model{Messages: []models.Message{
		{
			ID: "a", SenderID: "them", Direction: models.DirectionIncoming,
			Body: "hola\x1b[31mROJO\x1b]0;título\x07", Kind: models.KindText,
			Timestamp: time.Now(),
		},
	}})

	out := tr.View()
	if strings.Contains(out, "\x1b]0;") {
		t.Error("an OSC sequence from a message body reached the renderer")
	}
	// The escape bytes themselves must be gone; the visible text must remain.
	if !strings.Contains(text.StripANSI(out), "hola") {
		t.Error("sanitising should preserve the readable text")
	}
}

func TestDaySeparatorsAreDroppedInMinimalMode(t *testing.T) {
	// Minimal mode trades chrome for rows, so the separators go.
	yesterday := time.Now().Add(-48 * time.Hour)

	msgs := []models.Message{
		{
			ID: "a", SenderID: "them", Direction: models.DirectionIncoming,
			Body: "ayer", Kind: models.KindText, Timestamp: yesterday,
		},
		{
			ID: "b", SenderID: "them", Direction: models.DirectionIncoming,
			Body: "hoy", Kind: models.KindText, Timestamp: time.Now(),
		},
	}

	full := transcript.New(keybindings.DefaultMap(), theme.Dark())
	full.SetMode(layout.ModeFull)
	full.Resize(rect(60, 20))
	full.SetModel(component.Model{Messages: msgs})

	compact := transcript.New(keybindings.DefaultMap(), theme.Dark())
	compact.SetMode(layout.ModeMinimal)
	compact.Resize(rect(60, 20))
	compact.SetModel(component.Model{Messages: msgs})

	a := len(strings.Split(strings.TrimRight(full.View(), "\n "), "\n"))
	b := len(strings.Split(strings.TrimRight(compact.View(), "\n "), "\n"))
	if b >= a {
		t.Errorf("minimal mode should be no taller: %d rows against %d", b, a)
	}
}

// --- scrolling ---

func TestOpeningAConversationStartsAtTheNewest(t *testing.T) {
	r := rect(60, 8)
	msgs := make([]models.Message, 0, 30)
	for i := range 30 {
		msgs = append(msgs, models.Message{
			ID: models.MessageID("m" + itoa(i)), SenderID: "them",
			Direction: models.DirectionIncoming, Body: "mensaje " + itoa(i),
			Kind: models.KindText, Timestamp: time.Now().Add(time.Duration(i) * time.Minute),
		})
	}

	tr := transcript.New(keybindings.DefaultMap(), theme.Dark())
	tr.SetMode(layout.ModeFull)
	tr.Resize(r)
	tr.SetModel(component.Model{Messages: msgs})

	if !tr.AtBottom() {
		t.Error("a transcript should open pinned to the newest message")
	}
	// The newest message is the one on screen.
	body := strings.Join(render(t, tr, r), "\n")
	if !strings.Contains(body, "mensaje 29") {
		t.Errorf("the newest message should be visible:\n%s", body)
	}
}

func TestScrollingUpStopsAtTheOldest(t *testing.T) {
	tr := conversation(t, rect(60, 8), "uno", "dos", "tres", "cuatro", "cinco")

	for range 50 {
		tr.Scroll(-1)
	}

	// Scrolling past the top must clamp rather than run off into nothing, and must
	// report that it is no longer at the bottom.
	if tr.AtBottom() {
		t.Error("after scrolling up the view is not at the bottom")
	}
	tr.ScrollToNewest()
	if !tr.AtBottom() {
		t.Error("jumping to the newest should re-pin the view")
	}
}

func TestScrollingDoesNotMoveTheCursor(t *testing.T) {
	tr := conversation(t, rect(60, 8), "uno", "dos", "tres")
	before := tr.Cursor()
	tr.Scroll(-3)
	if tr.Cursor() != before {
		t.Error("scrolling is looking, not choosing: the cursor must not move")
	}
}

func TestCursorMovementEmitsTheSelectedMessage(t *testing.T) {
	tr := conversation(t, rect(60, 20), "uno", "dos", "tres")
	tr.Focus()

	_, cmd := tr.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if cmd == nil {
		t.Fatal("moving the cursor should report the message under it")
	}
	msg, ok := cmd().(component.Event)
	if !ok || msg.Kind != component.KindMessageSelected {
		t.Fatalf("expected a message-selected event, got %T", cmd())
	}
	if msg.MessageID == "" {
		t.Error("the event should name the message")
	}
}

func TestCursorMessageIDIsExported(t *testing.T) {
	tr := conversation(t, rect(60, 20), "uno", "dos")
	tr.Focus()

	if _, _ = tr.Update(tea.KeyPressMsg{Code: tea.KeyUp}); false {
		t.Fatal("unreachable")
	}
	if tr.CursorMessageID() == "" {
		t.Error("the application needs to know which message the cursor is on")
	}
	if _, ok := tr.MessageAt(tr.Cursor()); !ok {
		t.Error("the cursor should be on a real message")
	}
}

// --- selection ---

func TestSelectionDoesNotRelayout(t *testing.T) {
	// Selecting happens on every keystroke; re-wrapping a long transcript each time
	// would make navigation stutter.
	msgs := make([]models.Message, 0, 60)
	for i := range 60 {
		msgs = append(msgs, models.Message{
			ID: models.MessageID("m" + itoa(i)), SenderID: "them",
			Direction: models.DirectionIncoming,
			Body:      strings.Repeat("x", 200) + " " + itoa(i),
			Kind:      models.KindText, Timestamp: time.Now(),
		})
	}

	r := rect(60, 12)
	tr := transcript.New(keybindings.DefaultMap(), theme.Dark())
	tr.SetMode(layout.ModeFull)
	tr.Resize(r)
	tr.SetModel(component.Model{Messages: msgs})
	before := tr.View()

	// The newest message is the one on screen: a transcript opens pinned to the bottom,
	// so selecting an early one would change nothing visible.
	tr.SetModel(component.Model{Messages: msgs, Selected: map[models.MessageID]bool{"m59": true}})

	if tr.View() == before {
		t.Error("a selected message should look different from an unselected one")
	}
}

func TestToggleSelectEmitsAnEventPerMessage(t *testing.T) {
	tr := conversation(t, rect(60, 20), "uno", "dos")
	tr.Focus()

	_, cmd := tr.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if cmd == nil {
		t.Fatal("selecting should emit an event")
	}
	if msg, ok := cmd().(component.Event); !ok || msg.Kind != component.KindSelectionToggled {
		t.Errorf("expected a selection-toggled event, got %T", cmd())
	}
}

// --- resize ---

func TestReflowOnResizeKeepsTheCursorMessage(t *testing.T) {
	tr := conversation(t, rect(80, 20), "uno", "dos", "tres", "cuatro")
	tr.Focus()
	if _, _ = tr.Update(tea.KeyPressMsg{Code: tea.KeyUp}); false {
		t.Fatal("unreachable")
	}
	id := tr.CursorMessageID()

	tr.Resize(rect(40, 10))

	if tr.CursorMessageID() != id {
		t.Errorf("the cursor moved from %q to %q when the width changed", id, tr.CursorMessageID())
	}
	for i, l := range render(t, tr, rect(40, 10)) {
		if text.VisibleWidth(l) != 40 {
			t.Errorf("row %d is %d cells after the resize", i, text.VisibleWidth(l))
		}
	}
}

func TestExtremeResizeDoesNotPanic(t *testing.T) {
	tr := conversation(t, rect(60, 20), "uno", "dos", "tres")

	for _, size := range [][2]int{{1, 1}, {0, 0}, {200, 1}, {10, 100}, {5, 2}} {
		tr.Resize(layout.Rect{Width: size[0], Height: size[1]})
		_ = tr.View()
		tr.Scroll(5)
		tr.Scroll(-5)
	}
}
