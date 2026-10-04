package composer_test

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/component"
	"github.com/wterm/wterm/internal/ui/components/composer"
	"github.com/wterm/wterm/internal/ui/layout"
	"github.com/wterm/wterm/internal/ui/theme"
)

// rect builds the composer's rectangle.
func rect(w, h int) layout.Rect { return layout.Rect{Width: w, Height: h} }

// newComposer builds a focused composer of the given size.
func newComposer(t *testing.T, w, h int) *composer.Model {
	t.Helper()
	c := composer.New(keybindings.DefaultMap(), theme.Dark())
	c.SetMode(layout.ModeFull)
	c.Resize(rect(w, h))
	c.Focus()
	return c
}

// render returns the composer's frame with styling stripped.
//
// It panics rather than failing through *testing.T: it is called from inside table
// loops and from helpers that do not carry the testing handle, and a panic names the
// offending line directly.
func render(c *composer.Model, r layout.Rect) []string {
	lines := strings.Split(c.View(), "\n")
	if len(lines) != r.Height {
		panic("composer produced " + itoa(len(lines)) +
			" rows, rectangle is " + itoa(r.Height))
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = text.StripANSI(l)
		if got := text.VisibleWidth(out[i]); got != r.Width {
			panic("row " + itoa(i) + " is " + itoa(got) +
				" cells, rectangle is " + itoa(r.Width) + ": " + out[i])
		}
	}
	return out
}

// itoa keeps the frame assertions readable without threading a *testing.T through a
// helper that is only ever called from a test.
func itoa(n int) string { return strconv.Itoa(n) }

// typeText feeds a string as individual key presses.
func typeText(c *composer.Model, s string) {
	for _, r := range s {
		if _, _ = c.Update(tea.KeyPressMsg{Code: r, Text: string(r)}); false {
			break
		}
	}
}

// --- shape ---

func TestViewFillsItsRectangle(t *testing.T) {
	for _, size := range [][2]int{{40, 4}, {60, 3}, {80, 2}} {
		r := rect(size[0], size[1])
		c := newComposer(t, size[0], size[1])
		render(c, r)
	}
}

func TestPlaceholderIsVisibleWhenEmpty(t *testing.T) {
	r := rect(50, 4)
	body := strings.Join(render(newComposer(t, 50, 4), r), "\n")

	if !strings.Contains(body, "Escribir mensaje") {
		t.Errorf("an empty composer should show its placeholder:\n%s", body)
	}
}

func TestZeroSizedRectangleRendersNothing(t *testing.T) {
	c := newComposer(t, 0, 0)
	if c.View() != "" {
		t.Error("a region with no area should render nothing")
	}
}

// --- typing ---

func TestTypingAppearsInTheFrame(t *testing.T) {
	r := rect(50, 3)
	c := newComposer(t, 50, 3)
	typeText(c, "hola mundo")

	if c.Value() != "hola mundo" {
		t.Errorf("value = %q, want %q", c.Value(), "hola mundo")
	}
	if !strings.Contains(strings.Join(render(c, r), "\n"), "hola mundo") {
		t.Error("the draft should be visible in the frame")
	}
}

func TestBufferChangeIsReported(t *testing.T) {
	// The application needs to hear about buffer changes so it can redraw for derived
	// state such as an autocomplete hint; without an event, a change made through the
	// widget's own key handling would leave the frame one keystroke behind.
	c := newComposer(t, 50, 3)
	_, cmd := c.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})

	if cmd == nil {
		t.Fatal("a buffer change should emit an event")
	}
	if msg, ok := cmd().(component.Event); !ok || msg.Kind != component.KindSubmit {
		t.Errorf("expected a submit event, got %T", cmd())
	}
}

func TestGlobalShortcutsAreNotTyped(t *testing.T) {
	// ctrl+q must not insert a "q". This is the ordering guarantee the whole input
	// architecture exists to provide: the application resolves globals before the
	// focused region sees the key.
	c := newComposer(t, 50, 3)
	for _, r := range "qrpnef" {
		if _, _ = c.Update(tea.KeyPressMsg{Code: r, Text: string(r), Mod: tea.ModCtrl}); false {
			break
		}
	}
	if c.Value() != "" {
		t.Errorf("ctrl-modified keys were typed into the buffer: %q", c.Value())
	}
}

// --- submitting ---

func TestEnterSubmitsAndClears(t *testing.T) {
	c := newComposer(t, 50, 3)
	typeText(c, "hola")

	_, cmd := c.Update(tea.KeyPressMsg{Code: '\r', Text: "\r"})
	if cmd == nil {
		t.Fatal("enter should submit")
	}
	msg, ok := cmd().(component.Event)
	if !ok || msg.Kind != component.KindSubmit {
		t.Fatalf("expected a submit event, got %T", cmd())
	}
	if msg.Text != "hola" {
		t.Errorf("submitted %q, want %q", msg.Text, "hola")
	}
	if !c.Empty() {
		t.Error("the composer should be empty after submitting")
	}
}

func TestEnterOnAnEmptyComposerDoesNothing(t *testing.T) {
	// A submit event with no body would send an empty message, which the protocol
	// rejects and the user would see as an unexplained error toast.
	c := newComposer(t, 50, 3)
	if _, cmd := c.Update(tea.KeyPressMsg{Code: '\r', Text: "\r"}); cmd != nil {
		t.Error("submitting nothing should do nothing")
	}
}

func TestSubmitCarriesTheComposerMode(t *testing.T) {
	c := newComposer(t, 50, 3)
	c.SetReply("Ada")
	typeText(c, "respuesta")

	_, cmd := c.Update(tea.KeyPressMsg{Code: '\r', Text: "\r"})
	msg, ok := cmd().(component.Event)
	if !ok {
		t.Fatal("expected an event")
	}
	mode, ok := msg.Payload.(composer.Mode)
	if !ok {
		t.Fatalf("payload = %T, want a composer.Mode", msg.Payload)
	}
	if mode != composer.ModeReply {
		t.Errorf("mode = %v, want reply", mode)
	}
	// Submitting ends the mode: a second enter must not send another quote.
	if c.Mode() != composer.ModeSend {
		t.Error("submitting should return the composer to plain sending")
	}
}

func TestAltEnterInsertsANewlineInstead(t *testing.T) {
	// Multiline input is necessary — pasting a stack trace is normal — but enter must
	// still send, because a chat box where enter inserts a newline is unusable at a
	// prompt. The newline goes through alt+enter, and the hint line says so.
	c := newComposer(t, 50, 4)
	typeText(c, "uno")

	_, cmd := c.Update(tea.KeyPressMsg{Code: '\r', Text: "\r", Mod: tea.ModAlt})
	if cmd != nil {
		t.Error("alt+enter should not submit")
	}
	typeText(c, "dos")

	if !strings.Contains(c.Value(), "\n") {
		t.Errorf("alt+enter should insert a newline, value = %q", c.Value())
	}
	if !strings.Contains(c.Value(), "uno") || !strings.Contains(c.Value(), "dos") {
		t.Errorf("the newline should separate the two lines, value = %q", c.Value())
	}
}

func TestHintSaysHowToInsertANewline(t *testing.T) {
	r := rect(60, 4)
	body := strings.Join(render(newComposer(t, 60, 4), r), "\n")

	if !strings.Contains(body, "alt+enter") {
		t.Errorf("the hint should tell the user how to add a line:\n%s", body)
	}
}

// --- modes ---

func TestReplyBannerNamesTheQuotedMessage(t *testing.T) {
	r := rect(60, 5)
	c := newComposer(t, 60, 5)
	c.SetReply("Ada Lovelace")

	body := strings.Join(render(c, r), "\n")
	if !strings.Contains(body, "Ada Lovelace") {
		t.Errorf("the reply banner should name who is being replied to:\n%s", body)
	}
	if !strings.Contains(body, "respondiendo") {
		t.Errorf("the banner should say what will happen:\n%s", body)
	}
}

func TestEditPrefillsTheBody(t *testing.T) {
	c := newComposer(t, 60, 5)
	c.SetEdit("Ada Lovelace", "texto original")

	if !strings.Contains(c.Value(), "texto original") {
		t.Errorf("editing should prefill the existing body, got %q", c.Value())
	}
	if c.Mode() != composer.ModeEdit {
		t.Errorf("mode = %v, want edit", c.Mode())
	}
}

func TestReplyAndEditAreMutuallyExclusive(t *testing.T) {
	// One mode field, so the impossible state — quoting one message while editing
	// another — cannot be represented. That is the point of the type rather than two
	// booleans.
	c := newComposer(t, 60, 5)
	c.SetReply("Ada")
	c.SetEdit("Grace", "cuerpo")

	if c.Mode() != composer.ModeEdit {
		t.Errorf("edit should supersede reply, mode = %v", c.Mode())
	}
	body := strings.Join(render(c, rect(60, 5)), "\n")
	if strings.Contains(body, "respondiendo") {
		t.Error("the reply banner should be gone")
	}
}

func TestEscapeInTheComposerIsNotSwallowed(t *testing.T) {
	// Escape has to reach the application so a mode can be abandoned. A composer that
	// consumed it would leave the user stuck in a reply they cannot leave.
	c := newComposer(t, 60, 5)
	c.SetReply("Ada")

	if _, cmd := c.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil {
		t.Error("escape should be left for the application to handle")
	}
}

// --- caret ---

func TestCaretIsReportedWhenFocused(t *testing.T) {
	c := newComposer(t, 50, 3)
	typeText(c, "hola")

	x, y, ok := c.Cursor()
	if !ok {
		t.Fatal("a focused composer should report its caret")
	}
	if x <= 0 || y < 0 {
		t.Errorf("caret at (%d,%d) is outside the composer's interior", x, y)
	}
	if c.PromptX() < 2 {
		t.Errorf("the caret's column is %d; it must clear the border and the prompt", c.PromptX())
	}
}

// --- resize ---

func TestResizingReflowsTheDraft(t *testing.T) {
	c := newComposer(t, 60, 5)
	typeText(c, strings.Repeat("palabra ", 30))

	for _, size := range [][2]int{{30, 3}, {80, 5}, {20, 2}, {120, 6}} {
		c.Resize(rect(size[0], size[1]))
		for i, l := range render(c, rect(size[0], size[1])) {
			if text.VisibleWidth(l) != size[0] {
				t.Errorf("at %dx%d row %d is %d cells", size[0], size[1], i, text.VisibleWidth(l))
			}
		}
	}
}

func TestExtremeResizeDoesNotPanic(t *testing.T) {
	c := newComposer(t, 60, 5)
	typeText(c, "hola")

	for _, size := range [][2]int{{0, 0}, {1, 1}, {200, 1}, {3, 1}} {
		c.Resize(rect(size[0], size[1]))
		_ = c.View()
		_, _, _ = c.Cursor()
	}
}

// --- mode by layout ---

func TestMinimalModeDropsTheHint(t *testing.T) {
	// Both get the height the layout actually gives them: four rows in full mode and
	// two in minimal. The difference is what fits in them, not the height.
	full := newComposer(t, 60, 5)
	compact := newComposer(t, 60, 2)
	compact.SetMode(layout.ModeMinimal)

	if !strings.Contains(strings.Join(render(full, rect(60, 5)), "\n"), "alt+enter") {
		t.Error("full mode should show the hint")
	}
	if strings.Contains(strings.Join(render(compact, rect(60, 2)), "\n"), "alt+enter") {
		t.Error("minimal mode should reclaim the row the hint occupied")
	}
}

func TestDegradesRowByRowAsHeightShrinks(t *testing.T) {
	// What the composer gives up as rows are taken away, in order: the hint first,
	// then the bottom rule. The draft is the last thing to go, because a composer
	// that cannot be typed into is not a composer.
	tests := []struct {
		height         int
		wantHint       bool
		wantTopRule    bool
		wantBottomRule bool
	}{
		{height: 6, wantHint: true, wantTopRule: true, wantBottomRule: true},
		{height: 4, wantHint: true, wantTopRule: true, wantBottomRule: true},
		{height: 3, wantHint: true, wantTopRule: true, wantBottomRule: false},
		{height: 2, wantHint: true, wantTopRule: false, wantBottomRule: false},
	}

	for _, tc := range tests {
		rows := render(newComposer(t, 60, tc.height), rect(60, tc.height))
		body := strings.Join(rows, "\n")

		if got := strings.Contains(body, "alt+enter"); got != tc.wantHint {
			t.Errorf("at %d rows: hint present = %v, want %v\n%s",
				tc.height, got, tc.wantHint, body)
		}
		if !strings.Contains(body, "Escribir mensaje") {
			t.Errorf("at %d rows the draft must survive:\n%s", tc.height, body)
		}
		if got := strings.Contains(rows[0], "─"); got != tc.wantTopRule {
			t.Errorf("at %d rows: top rule present = %v, want %v\n%q",
				tc.height, got, tc.wantTopRule, rows[0])
		}
		if got := strings.Contains(rows[len(rows)-1], "─"); got != tc.wantBottomRule {
			t.Errorf("at %d rows: bottom rule present = %v, want %v\n%q",
				tc.height, got, tc.wantBottomRule, rows[len(rows)-1])
		}
	}
}

func TestMinimalModeKeepsOnlyTheDraft(t *testing.T) {
	// Two rows, no hint, no rules: everything that is not the draft goes. A user on a
	// small terminal gets a usable input, not a degraded one.
	c := newComposer(t, 60, 2)
	c.SetMode(layout.ModeMinimal)
	rows := render(c, rect(60, 2))
	body := strings.Join(rows, "\n")

	if strings.Contains(body, "alt+enter") {
		t.Errorf("minimal mode should drop the hint:\n%s", body)
	}
	if !strings.Contains(body, "Escribir mensaje") {
		t.Errorf("minimal mode must keep the draft:\n%s", body)
	}
}

// --- typing never crashes ---

func TestTypingNeverPanics(t *testing.T) {
	c := newComposer(t, 40, 3)

	keys := []tea.KeyPressMsg{
		{Code: 'ñ', Text: "ñ"},
		{Code: '👍', Text: "👍"},
		{Code: tea.KeyBackspace},
		{Code: tea.KeyDelete},
		{Code: tea.KeyTab},
		{Code: 'a', Text: "a", Mod: tea.ModCtrl},
		{Code: 'a', Text: "A", Mod: tea.ModShift},
		{Code: tea.KeyLeft},
		{Code: tea.KeyHome},
		{Code: tea.KeyEnd},
		{Code: '\r', Text: "\r", Mod: tea.ModAlt},
		{Code: tea.KeyEnter},
	}
	for _, k := range keys {
		if _, _ = c.Update(k); false {
			break
		}
		_ = c.View()
	}
}
