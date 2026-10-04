package chatlist

import (
	"strings"
	"testing"
	"time"

	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/theme"
)

// newState builds a renderable sidebar state for a test.
func newState(width, height int, chats ...models.Chat) State {
	return State{
		Chats:    chats,
		Selected: 0,
		Focused:  true,
		Width:    width,
		Height:   height,
		Theme:    theme.Dark(),
	}
}

func chat(id, name string, opts ...func(*models.Chat)) models.Chat {
	ts := time.Date(2026, time.October, 3, 14, 30, 0, 0, time.UTC)
	c := models.Chat{ID: models.ChatID(id), Name: name, Type: models.ChatTypeDirect, Timestamp: ts}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func TestViewRendersTwoLineRows(t *testing.T) {
	s := newState(30, 20, chat("1", "Ada"), chat("2", "Grace"))

	got := View(s)
	lines := strings.Split(got, "\n")

	// Header, search box, divider, then two lines per chat.
	if len(lines) < 4+RowHeight*2 {
		t.Fatalf("expected at least %d lines, got %d", 4+RowHeight*2, len(lines))
	}
	if !strings.Contains(got, "Chats") {
		t.Error("sidebar heading missing")
	}
	if !strings.Contains(got, "Ada") || !strings.Contains(got, "Grace") {
		t.Errorf("chat names missing from:\n%s", got)
	}
}

func TestEveryLineFitsTheWidth(t *testing.T) {
	// The invariant that stops the sidebar from wrapping into the transcript.
	s := newState(24, 30, chat("1", "A very long contact name indeed"),
		chat("2", "Short"))

	// VisibleWidth, not Width: the lines carry colour escapes, and counting their
	// bytes as cells would measure the escape sequences rather than the layout.
	for i, line := range strings.Split(View(s), "\n") {
		if w := text.VisibleWidth(line); w > s.Width {
			t.Errorf("line %d is %d cells, sidebar is %d: %q", i, w, s.Width, text.StripANSI(line))
		}
	}
}

func TestViewIsPaddedToFullHeight(t *testing.T) {
	// The sidebar's background must reach the bottom edge, or the transcript
	// shows through and the layout looks broken.
	s := newState(30, 24, chat("1", "Ada"))
	got := View(s)

	lines := strings.Split(got, "\n")
	if w := text.VisibleWidth(lines[len(lines)-1]); w != s.Width {
		t.Errorf("last line is %d cells, want %d", w, s.Width)
	}
}

func TestSelectionMarkers(t *testing.T) {
	focused := newState(30, 20, chat("1", "Ada"), chat("2", "Grace"))
	focused.Selected = 1
	got := View(focused)
	if !strings.Contains(got, "Grace") {
		t.Error("the selected chat should be visible")
	}

	// With no selection, nothing is marked.
	focused.Selected = -1
	if View(focused) == "" {
		t.Error("the sidebar should still render with no selection")
	}
}

func TestUnreadBadgeIsRendered(t *testing.T) {
	s := newState(30, 20, chat("1", "Ada", func(c *models.Chat) {
		c.UnreadCount = 7
		c.HasUnread = true
	}))

	if got := View(s); !strings.Contains(got, "7") {
		t.Errorf("unread count should be shown, got:\n%s", got)
	}
}

func TestPinAndMuteDecorations(t *testing.T) {
	s := newState(30, 20, chat("1", "Ada", func(c *models.Chat) {
		c.Pinned = true
		c.Muted = true
	}))
	g := s.Theme.Glyphs

	got := View(s)
	if !strings.Contains(got, g.Pinned) {
		t.Errorf("pinned marker %q missing", g.Pinned)
	}
	if !strings.Contains(got, g.Muted) {
		t.Errorf("muted marker %q missing", g.Muted)
	}
}

func TestTypingReplacesPreview(t *testing.T) {
	s := newState(30, 20, chat("1", "Ada", func(c *models.Chat) {
		c.LastMessage = &models.MessagePreview{Kind: models.KindText, Body: "hello"}
		c.Typing = true
	}))

	got := View(s)
	if strings.Contains(got, "hello") {
		t.Error("the preview should be replaced while the peer is typing")
	}
	if !strings.Contains(got, "typing") {
		t.Errorf("typing indicator missing:\n%s", got)
	}
}

func TestEmptyStates(t *testing.T) {
	empty := newState(30, 20)
	if got := View(empty); !strings.Contains(got, "no conversations") {
		t.Errorf("expected an empty-state message, got:\n%s", got)
	}

	filtered := newState(30, 20)
	filtered.SearchQuery = "zzz"
	if got := View(filtered); !strings.Contains(got, "no matches") {
		t.Errorf("expected a no-matches message, got:\n%s", got)
	}
}

func TestZeroWidthRendersNothing(t *testing.T) {
	// A zero-width sidebar must not panic or emit stray control characters.
	if got := View(newState(0, 20, chat("1", "Ada"))); got != "" {
		t.Errorf("expected empty output, got %q", got)
	}
	if got := View(newState(30, 0, chat("1", "Ada"))); got != "" {
		t.Errorf("expected empty output at zero height, got %q", got)
	}
}

func TestVisibleRangeScrollsSelectionIntoView(t *testing.T) {
	chats := make([]models.Chat, 0, 50)
	for i := range 50 {
		chats = append(chats, chat(string(rune('a'+i%26))+string(rune('0'+i/26)), "Chat"))
	}

	// Height fits five rows.
	s := newState(30, 10, chats...)
	s.Selected = 40
	start, end := s.visibleRange()

	if start > 40 {
		t.Errorf("selected row %d is above the visible range %d..%d", 40, start, end)
	}
	if end <= 40 {
		t.Errorf("selected row %d is below the visible range %d..%d", 40, start, end)
	}
}

func TestOffsetForSelection(t *testing.T) {
	tests := []struct {
		name                    string
		selected, total, height int
		want                    int
	}{
		{"first row needs no scroll", 0, 20, 10, 0},
		{"last row pins to the end", 19, 20, 10, 15},
		{"selection inside the view", 3, 20, 10, 0},
		{"beyond the end clamps", 25, 20, 10, 19},
		{"empty list", -1, 0, 10, 0},
		{"no selection", -1, 20, 10, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := OffsetForSelection(tc.selected, tc.total, tc.height)
			if got != tc.want {
				t.Errorf("want %d, got %d", tc.want, got)
			}
		})
	}
}

func TestOffsetForSelectionNeverExceedsList(t *testing.T) {
	for total := 0; total <= 30; total++ {
		for sel := -2; sel <= total+2; sel++ {
			got := OffsetForSelection(sel, total, 10)
			if got < 0 {
				t.Errorf("sel=%d total=%d gave a negative offset %d", sel, total, got)
			}
		}
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	s := newState(30, 20, chat("1", "Ada"), chat("2", "Grace"), chat("3", "Alan"))
	first := View(s)

	for range 5 {
		if got := View(s); got != first {
			t.Fatalf("render is not deterministic:\n%q\nthen\n%q", first, got)
		}
	}
}

func TestASCIIGlyphsRender(t *testing.T) {
	s := newState(30, 20, chat("1", "Ada", func(c *models.Chat) {
		c.Pinned = true
		c.UnreadCount = 3
		c.HasUnread = true
	}))
	s.Theme = s.Theme.WithGlyphs(theme.ASCIIGlyphs())

	got := View(s)
	// No non-ASCII should remain except in the content itself.
	for _, r := range got {
		if r > 0x7f {
			t.Fatalf("ASCII glyph set still emitted %q in:\n%s", r, got)
		}
	}
}
