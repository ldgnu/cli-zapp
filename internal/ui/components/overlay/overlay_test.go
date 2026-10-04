package overlay

import (
	"strings"
	"testing"
	"time"

	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/theme"
)

func renderLines(o Overlay, w, h int) []string {
	return strings.Split(View(o, w, h, theme.Dark()), "\n")
}

func TestMenuRendersItems(t *testing.T) {
	o := Menu("Message", []MenuItem{
		{Label: "Reply", Hint: "r"},
		{Label: "Delete", Hint: "mod+d", Danger: true},
	}, 0)

	got := text.StripANSI(View(o, 60, 20, theme.Dark()))

	for _, want := range []string{"Message", "Reply", "Delete", "r", "mod+d"} {
		if !strings.Contains(got, want) {
			t.Errorf("menu should contain %q, got:\n%s", want, got)
		}
	}
}

func TestMenuMarksTheCursor(t *testing.T) {
	withCursor := Menu("Message", []MenuItem{{Label: "First"}, {Label: "Second"}}, 1)
	got := View(withCursor, 60, 20, theme.Dark())

	// The selected row carries a background colour the others lack.
	if !strings.Contains(got, "48;2;") {
		t.Errorf("the selected row should be highlighted:\n%q", got)
	}
}

func TestMenuSeparatorRenders(t *testing.T) {
	o := Menu("Message", []MenuItem{
		{Label: "Reply"},
		Separator(),
		{Label: "Delete", Danger: true},
	}, 0)

	got := text.StripANSI(View(o, 60, 20, theme.Dark()))
	if strings.Count(got, "\n") < 4 {
		t.Errorf("a separator should add a row, got:\n%s", got)
	}
	if strings.Contains(got, "  \n") && !strings.Contains(got, "Reply") {
		t.Error("separator swallowed the menu content")
	}
}

func TestConfirmDefaultsToCancel(t *testing.T) {
	// A destructive dialog must open on the safe button, so that a stray enter
	// cannot destroy data.
	o := Confirm("Delete chat", "This cannot be undone.")
	got := text.StripANSI(View(o, 60, 20, theme.Dark()))

	if !strings.Contains(got, "Cancel") || !strings.Contains(got, "Confirm") {
		t.Errorf("both buttons should be offered, got:\n%s", got)
	}
	if !o.CancelDefault {
		t.Error("a confirmation should record that cancel is the default")
	}
	if !strings.Contains(got, "cannot be undone") {
		t.Errorf("the message body should be shown, got:\n%s", got)
	}
}

func TestInfoRendersBody(t *testing.T) {
	o := Info("Chat info", []string{"Type: direct", "Phone: +54 9 11"})
	got := text.StripANSI(View(o, 60, 20, theme.Dark()))

	for _, want := range []string{"Chat info", "Type: direct", "Phone: +54 9 11"} {
		if !strings.Contains(got, want) {
			t.Errorf("info overlay should contain %q, got:\n%s", want, got)
		}
	}
}

func TestToastCarriesAnExpiry(t *testing.T) {
	o := Toast("Copied", false)
	if o.ExpiresAt.IsZero() {
		t.Error("a toast must expire, or it would sit on screen for the session")
	}
	if d := time.Until(o.ExpiresAt); d <= 0 || d > DefaultToastTTL {
		t.Errorf("unexpected toast lifetime %v", d)
	}
}

func TestOverlayFitsTheTerminal(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {40, 10}, {120, 40}, {20, 6}} {
		o := Info("Title", []string{strings.Repeat("word ", 40), "short"})
		lines := renderLines(o, size[0], size[1])

		if len(lines) > size[1] {
			t.Errorf("at %dx%d the overlay is %d lines tall", size[0], size[1], len(lines))
		}
		for i, l := range lines {
			if w := text.VisibleWidth(l); w > size[0] {
				t.Errorf("at %dx%d line %d is %d cells", size[0], size[1], i, w)
			}
		}
	}
}

func TestZeroSizeRendersNothing(t *testing.T) {
	o := Info("Title", []string{"body"})
	if got := View(o, 0, 20, theme.Dark()); got != "" {
		t.Errorf("width 0 should render nothing, got %q", got)
	}
	if got := View(o, 60, 0, theme.Dark()); got != "" {
		t.Errorf("height 0 should render nothing, got %q", got)
	}
}

func TestBodyIsSanitised(t *testing.T) {
	// Overlay content can come from a contact's "about" text, so it gets the
	// same treatment as a message body.
	o := Info("Info", []string{"hello\x1b[2J\x1b]0;pwned\x07"})
	got := text.StripANSI(View(o, 60, 20, theme.Dark()))

	if strings.Contains(got, "pwned") {
		t.Errorf("an injected payload survived: %q", got)
	}
}

func TestStackPushPop(t *testing.T) {
	var s Stack

	if !s.Empty() || s.Len() != 0 {
		t.Fatal("a new stack should be empty")
	}
	if _, ok := s.Top(); ok {
		t.Error("Top on an empty stack should report false")
	}
	if _, ok := s.Pop(); ok {
		t.Error("Pop on an empty stack should report false")
	}

	s.Push(Menu("first", []MenuItem{{Label: "a"}}, 0))
	s.Push(Menu("second", []MenuItem{{Label: "b"}}, 0))

	if s.Len() != 2 {
		t.Errorf("expected 2 overlays, got %d", s.Len())
	}
	top, ok := s.Top()
	if !ok || top.Title != "second" {
		t.Errorf("Top should be the most recently pushed, got %+v", top)
	}

	popped, ok := s.Pop()
	if !ok || popped.Title != "second" {
		t.Errorf("Pop should remove the topmost, got %+v", popped)
	}
	if s.Len() != 1 {
		t.Errorf("after popping, expected 1 overlay, got %d", s.Len())
	}
}

func TestStackNests(t *testing.T) {
	// A context menu over the conversation, with a confirmation over the menu, is
	// the case a boolean "modal open" cannot express.
	var s Stack
	s.Push(overlayFor("menu"))
	s.Push(overlayFor("confirm"))
	s.Push(overlayFor("toast"))

	if s.Len() != 3 {
		t.Fatalf("expected 3 nested overlays, got %d", s.Len())
	}
	if top, _ := s.Top(); top.Kind != KindToast {
		t.Errorf("the last pushed should be on top, got kind %v", top.Kind)
	}
}

func overlayFor(title string) Overlay {
	switch title {
	case "toast":
		return Toast("hi", false)
	default:
		return Menu(title, []MenuItem{{Label: "item"}}, 0)
	}
}

func TestStackViewOverBackground(t *testing.T) {
	var s Stack
	s.Push(Info("Title", []string{"body"}))

	got := text.StripANSI(s.View(60, 20, theme.Dark()))
	if !strings.Contains(got, "Title") {
		t.Errorf("the overlay should be rendered over the background:\n%s", got)
	}
}

func TestEmptyStackViewIsEmpty(t *testing.T) {
	var s Stack
	if got := s.View(60, 20, theme.Dark()); got != "" {
		t.Errorf("an empty stack should render nothing, got %q", got)
	}
}

func TestStackViewMatchesTheTerminalSize(t *testing.T) {
	var s Stack
	s.Push(Info("Title", []string{"a fairly long body line of text here"}))

	const w, h = 70, 22
	lines := strings.Split(s.View(w, h, theme.Dark()), "\n")

	if len(lines) != h {
		t.Errorf("stack view is %d lines, terminal is %d", len(lines), h)
	}
	for i, l := range lines {
		if width := text.VisibleWidth(l); width > w {
			t.Errorf("line %d is %d cells, terminal is %d", i, width, w)
		}
	}
}

func TestSetTopReplacesTheTopmost(t *testing.T) {
	var s Stack
	s.Push(Menu("m", []MenuItem{{Label: "a"}, {Label: "b"}}, 0))
	s.Push(Menu("m2", []MenuItem{{Label: "c"}, {Label: "d"}}, 0))

	top, _ := s.Top()
	top.Cursor = 1
	s.SetTop(top)

	after, _ := s.Top()
	if after.Cursor != 1 {
		t.Errorf("SetTop should update the topmost overlay, cursor = %d", after.Cursor)
	}
	if s.Len() != 2 {
		t.Errorf("SetTop must not change the stack depth, got %d", s.Len())
	}
}

func TestSetTopOnEmptyStackIsSafe(t *testing.T) {
	var s Stack
	s.SetTop(Menu("m", nil, 0)) // must not panic
	if !s.Empty() {
		t.Error("SetTop on an empty stack should do nothing")
	}
}

func TestFitRowPreservesTheTail(t *testing.T) {
	// The overlay must not erase what it does not cover, or the conversation
	// behind a dialog would be destroyed.
	dst := strings.Repeat("x", 20)
	got := text.StripANSI(fitRow("HELLO", dst))

	if !strings.HasPrefix(got, "HELLO") {
		t.Errorf("the overlay text should be written first, got %q", got)
	}
	if len(got) != 20 {
		t.Errorf("the row should keep its full width, got %d cells: %q", len(got), got)
	}
	if !strings.HasSuffix(got, "xxxxxxxxxxx") {
		t.Errorf("the uncovered tail should survive, got %q", got)
	}
}

func TestFitRowWithNarrowOverlay(t *testing.T) {
	dst := "abcdefghij"
	got := text.StripANSI(fitRow("AB", dst))

	if got != "ABcdefghij" {
		t.Errorf("only the covered cells should change, got %q", got)
	}
}

func TestMaxWidthCapsTheBoxNotTheCanvas(t *testing.T) {
	// A very long line must not stretch the dialog across the terminal. The
	// canvas itself is always full-width by contract, so the box is what is
	// measured here.
	o := Info("Title", []string{strings.Repeat("word ", 200)})

	const termWidth = 200
	for i, l := range strings.Split(viewBox(o, termWidth, theme.Dark()), "\n") {
		if w := text.VisibleWidth(l); w > MaxWidth+6 {
			t.Errorf("box line %d is %d cells, MaxWidth is %d", i, w, MaxWidth)
		}
	}
}

func TestViewAlwaysFillsTheCanvas(t *testing.T) {
	// The contract the caller relies on when compositing: exactly height lines of
	// exactly width cells, whatever the overlay contains.
	for _, size := range [][2]int{{80, 24}, {200, 50}, {40, 12}} {
		for name, o := range map[string]Overlay{
			"menu":  Menu("t", []MenuItem{{Label: "a"}, {Label: "b"}}, 0),
			"modal": Info("t", []string{"body"}),
			"toast": Toast("hello", false),
		} {
			lines := strings.Split(View(o, size[0], size[1], theme.Dark()), "\n")
			if len(lines) != size[1] {
				t.Errorf("%s at %dx%d: %d lines, want %d", name, size[0], size[1], len(lines), size[1])
			}
			for i, l := range lines {
				if w := text.VisibleWidth(l); w != size[0] {
					t.Errorf("%s at %dx%d line %d: %d cells, want %d", name, size[0], size[1], i, w, size[0])
				}
			}
		}
	}
}

func TestViewPreservesBoxStyling(t *testing.T) {
	// The canvas is built from the styled box rows, so a modal's background must
	// survive; a composition that stripped the escapes would render unstyled text.
	got := View(Info("Title", []string{"body"}), 60, 12, theme.Dark())

	if !text.HasEscape(got) {
		t.Errorf("the overlay should be styled, got a plain string:\n%q", got)
	}
}
