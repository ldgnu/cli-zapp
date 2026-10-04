package palette_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/component"
	"github.com/wterm/wterm/internal/ui/components/palette"
	"github.com/wterm/wterm/internal/ui/layout"
	"github.com/wterm/wterm/internal/ui/theme"
)

// commands is a fixed table so the filtering assertions are about the filter rather
// than about whatever the application happens to declare this week.
var commands = []component.Command{
	{ID: "reply", Title: "Responder", Hint: "r", Section: "Mensaje", Available: true},
	{ID: "edit", Title: "Editar mensaje", Hint: "mod+e", Section: "Mensaje", Available: true},
	{ID: "copy", Title: "Copiar mensaje", Hint: "mod+c", Section: "Mensaje", Available: true},
	{ID: "react", Title: "Reaccionar", Hint: "R", Section: "Mensaje", Available: false},
	{ID: "delete", Title: "Eliminar mensaje", Section: "Mensaje", Available: true, Danger: true},
	{ID: "quit", Title: "Salir", Hint: "mod+q", Section: "Aplicación", Available: true},
	{ID: "help", Title: "Atajos de teclado", Hint: "mod+?", Section: "Aplicación", Available: true},
}

// open builds an open palette over the fixed table.
func open(t *testing.T, w, h int, query string) *palette.Model {
	t.Helper()
	p := palette.New(theme.Dark())
	p.Resize(layout.Rect{Width: w, Height: h})
	p.SetCommands(commands)
	p.Open(query)
	return p
}

// frame returns the palette's screen-sized frame with styling stripped.
func frame(t *testing.T, p *palette.Model, w, h int) string {
	t.Helper()
	lines := strings.Split(p.View(), "\n")
	if len(lines) != h {
		t.Fatalf("produced %d rows, screen is %d", len(lines), h)
	}
	for i, l := range lines {
		if got := text.VisibleWidth(text.StripANSI(l)); got != w {
			t.Errorf("row %d is %d cells, screen is %d", i, got, w)
		}
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, text.StripANSI(l))
	}
	return strings.Join(out, "\n")
}

// --- shape ---

func TestClosedPaletteRendersNothing(t *testing.T) {
	p := palette.New(theme.Dark())
	p.Resize(layout.Rect{Width: 80, Height: 24})
	p.SetCommands(commands)

	if p.IsOpen() {
		t.Error("a new palette should be closed")
	}
	if p.View() != "" {
		t.Error("a closed palette should render nothing at all")
	}
}

func TestOpenPaletteRendersAWholeScreenFrame(t *testing.T) {
	// Returning a full-screen frame is what lets the root layer the palette with the
	// same one-line merge it uses for dialogs. A palette that returned only its box
	// would force the root to know how to centre it.
	frame(t, open(t, 80, 24, ""), 80, 24)
}

func TestPaletteFitsASmallTerminal(t *testing.T) {
	for _, size := range [][2]int{{40, 10}, {60, 20}, {200, 50}} {
		frame(t, open(t, size[0], size[1], ""), size[0], size[1])
	}
}

func TestPromptIsAlwaysVisible(t *testing.T) {
	// The prompt is what the user is typing into. A long command list must not push it
	// off the screen, however many commands the application declares.
	many := make([]component.Command, 0, 200)
	for i := range 200 {
		many = append(many, component.Command{
			ID: "c", Title: "Comando " + itoa(i), Section: "S",
		})
	}

	p := palette.New(theme.Dark())
	p.Resize(layout.Rect{Width: 80, Height: 24})
	p.SetCommands(many)
	p.Open("")

	body := frame(t, p, 80, 24)
	if !strings.Contains(body, "buscar un comando") {
		t.Error("the prompt should be visible with a long command list")
	}
}

// --- filtering ---

func TestQueryFiltersTheList(t *testing.T) {
	body := frame(t, open(t, 80, 24, "respon"), 80, 24)

	if !strings.Contains(body, "Responder") {
		t.Errorf("the matching command should be listed:\n%s", body)
	}
	if strings.Contains(body, "Copiar mensaje") {
		t.Error("a non-matching command should be filtered out")
	}
}

func TestExactMatchOutranksPrefix(t *testing.T) {
	// "Responder" and "Responder rápido" both start with the query, and the exact one
	// is what the user meant. The order is asserted by position in the frame rather
	// than by reading the score, because the frame is what the user sees.
	p := palette.New(theme.Dark())
	p.Resize(layout.Rect{Width: 80, Height: 24})
	p.SetCommands([]component.Command{
		{ID: "a", Title: "Responder rápido", Section: "S"},
		{ID: "b", Title: "Responder", Section: "S"},
	})
	p.Open("responder")

	rows := strings.Split(frame(t, p, 80, 24), "\n")
	quick, exact := -1, -1
	for i, l := range rows {
		switch {
		case quick < 0 && strings.Contains(l, "Responder rápido"):
			quick = i
		case exact < 0 && strings.Contains(l, "Responder") && !strings.Contains(l, "rápido"):
			exact = i
		}
	}
	if quick < 0 || exact < 0 {
		t.Fatalf("both commands should be listed:\n%s", strings.Join(rows, "\n"))
	}
	if exact > quick {
		t.Errorf("the exact match should be listed first (exact on row %d, prefix on %d)",
			exact, quick)
	}
}

func TestUnavailableCommandsAreListedNotHidden(t *testing.T) {
	// "Reaccionar" needs a message under the cursor. Hiding it would make the user
	// think the feature does not exist; showing it greyed says "not now".
	body := frame(t, open(t, 80, 24, "reaccionar"), 80, 24)

	if !strings.Contains(body, "Reaccionar") {
		t.Errorf("an unavailable command should still be listed:\n%s", body)
	}
}

func TestNoMatchesSaysSo(t *testing.T) {
	body := frame(t, open(t, 80, 24, "zzzzz"), 80, 24)

	if !strings.Contains(body, "sin coincidencias") {
		t.Errorf("a query matching nothing should say so:\n%s", body)
	}
	// The prompt survives an empty result, because that is the line the user is typing
	// into; losing it would leave them with a dead box and no way to clear the query.
	if !strings.Contains(body, "zzzzz") {
		t.Errorf("the prompt should still show the query:\n%s", body)
	}
}

func TestShortcutIsSearchable(t *testing.T) {
	// A user who remembers the key rather than the name should be able to type it.
	body := frame(t, open(t, 80, 24, "mod+q"), 80, 24)

	if !strings.Contains(body, "Salir") {
		t.Errorf("a shortcut should be searchable:\n%s", body)
	}
}

func TestFilteringNeverPanics(t *testing.T) {
	queries := make([]string, 0, 9)
	queries = append(queries, "", " ", "ñ", "\x1b", "\x00", "aá", "  a  ", "%s")
	queries = append(queries, strings.Repeat("x", 500))

	for _, q := range queries {
		p := open(t, 80, 24, q)
		if p.View() == "" {
			t.Errorf("query %q produced nothing", q)
		}
	}
}

// --- choosing ---

func TestEnterChoosesTheHighlightedCommand(t *testing.T) {
	p := open(t, 80, 24, "salir")

	_, cmd := p.Update(tea.KeyPressMsg{Code: '\r', Text: "\r"})
	if cmd == nil {
		t.Fatal("enter should choose the command")
	}
	msg, ok := cmd().(component.Event)
	if !ok || msg.Kind != component.KindCommandChosen {
		t.Fatalf("expected a command-chosen event, got %T", cmd())
	}
	if msg.Command.ID != "quit" {
		t.Errorf("chose %q, want %q", msg.Command.ID, "quit")
	}
	// The palette closes, or it would sit on top of whatever the command did.
	if p.IsOpen() {
		t.Error("the palette should close once a command is chosen")
	}
}

func TestAnUnavailableCommandDoesNothing(t *testing.T) {
	// "Reaccionar" is marked unavailable. Choosing it must do nothing rather than
	// something surprising, because the user can see it and select it.
	p := open(t, 80, 24, "reaccionar")
	before := frame(t, p, 80, 24)

	_, cmd := p.Update(tea.KeyPressMsg{Code: '\r', Text: "\r"})
	if cmd != nil {
		t.Error("choosing an unavailable command should emit nothing")
	}
	if !p.IsOpen() {
		t.Error("the palette should stay open so the user can pick something else")
	}
	if after := frame(t, p, 80, 24); after != before {
		t.Error("an unavailable command should leave the palette untouched")
	}
}

func TestEscapeClosesWithoutChoosing(t *testing.T) {
	p := open(t, 80, 24, "")

	_, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("escape should report the dismissal")
	}
	if msg, ok := cmd().(component.Event); !ok || msg.Kind != component.KindDismissOverlay {
		t.Errorf("expected a dismiss event, got %T", cmd())
	}
	if p.IsOpen() {
		t.Error("escape should close the palette")
	}
}

// --- navigation ---

func TestArrowsMoveTheCursor(t *testing.T) {
	p := open(t, 80, 24, "")

	// From the first entry, moving up must stay put rather than wrap to the end: a
	// menu that wraps on a single keypress makes it impossible to stop at the first
	// item with the arrow keys.
	for range 5 {
		if _, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyUp}); false {
			break
		}
	}
	if got := cursorTitle(frame(t, p, 80, 24)); got != "Responder" {
		t.Errorf("moving up past the first entry landed on %q, want %q", got, "Responder")
	}

	for range 5 {
		if _, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown}); false {
			break
		}
	}
	if got := cursorTitle(frame(t, p, 80, 24)); got == "Responder" {
		t.Error("moving down should have left the first entry")
	}
}

func TestCtrlJAndCtrlKNavigate(t *testing.T) {
	// The vim keys are what a keyboard-driven user reaches for, and this interface
	// follows that dialect everywhere else.
	p := open(t, 80, 24, "")

	if _, _ = p.Update(tea.KeyPressMsg{Code: 'j', Text: "j", Mod: tea.ModCtrl}); false {
		t.Fatal("unreachable")
	}
	if got := cursorTitle(frame(t, p, 80, 24)); got == "Responder" {
		t.Error("ctrl+j should move down")
	}

	if _, _ = p.Update(tea.KeyPressMsg{Code: 'k', Text: "k", Mod: tea.ModCtrl}); false {
		t.Fatal("unreachable")
	}
	if got := cursorTitle(frame(t, p, 80, 24)); got != "Responder" {
		t.Error("ctrl+k should move back up")
	}
}

func TestBackspaceErasesTheQuery(t *testing.T) {
	p := open(t, 80, 24, "ab")

	if _, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyBackspace}); false {
		t.Fatal("unreachable")
	}
	if p.Query() != "a" {
		t.Errorf("query = %q, want %q", p.Query(), "a")
	}
}

func TestBackspaceOnAnEmptyQueryIsHarmless(t *testing.T) {
	p := open(t, 80, 24, "")

	if _, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyBackspace}); false {
		t.Fatal("unreachable")
	}
	if p.Query() != "" {
		t.Errorf("query = %q, want it unchanged", p.Query())
	}
}

func TestReopeningRestoresTheLastPosition(t *testing.T) {
	// A palette that jumps back to the top every time it is dismissed makes repeated
	// use feel like it forgot what you were doing.
	p := open(t, 80, 24, "")
	for range 3 {
		if _, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown}); false {
			break
		}
	}
	want := cursorTitle(frame(t, p, 80, 24))
	if want == "" {
		t.Fatal("the cursor should be visible in the frame")
	}

	p.Close()
	p.Open("")

	if got := cursorTitle(frame(t, p, 80, 24)); got != want {
		t.Errorf("reopened on %q, want %q", got, want)
	}
}

func TestOpeningWithAQueryStartsAtTheTop(t *testing.T) {
	// A query changes what is listed, so the previous position is meaningless.
	p := open(t, 80, 24, "")
	for range 3 {
		if _, _ = p.Update(tea.KeyPressMsg{Code: tea.KeyDown}); false {
			break
		}
	}
	p.Close()
	p.Open("Copiar")

	if got := cursorTitle(frame(t, p, 80, 24)); got != "Copiar" {
		t.Errorf("a query opened the palette on %q, want the only match", got)
	}
}

// --- helpers ---

// cursorTitle returns the title of the highlighted entry.
//
// The renderer draws a marker glyph in the margin of the selected row, which survives
// the styling being stripped, so the cursor is observable without a colour-capable
// terminal.
func cursorTitle(body string) string {
	// The marker comes from the theme, so the helper does not hard-code a glyph that
	// changes with --glyphs=ascii — and so it cannot be confused with the palette's own
	// prompt glyph, which shares a frame with the list.
	marker := strings.TrimSpace(theme.Dark().Glyphs.Marker)

	for _, l := range strings.Split(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), marker) {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(l))
		if len(fields) > 1 {
			return fields[1]
		}
	}
	return ""
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
