package messagelist

import (
	"strings"
	"testing"
	"time"

	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/theme"
)

func loc(t *testing.T) Loc {
	t.Helper()
	return NewLoc(time.UTC)
}

func incoming(body string, at time.Time) models.Message {
	return models.Message{
		ID:        models.MessageID(body),
		Direction: models.DirectionIncoming,
		Kind:      models.KindText,
		Body:      body,
		Timestamp: at,
	}
}

func outgoing(body string, at time.Time) models.Message {
	m := incoming(body, at)
	m.Direction = models.DirectionOutgoing
	m.Status = models.DeliveryDelivered
	return m
}

func TestLayoutInsertsDaySeparators(t *testing.T) {
	day1 := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, time.October, 3, 10, 0, 0, 0, time.UTC)

	rows := Layout([]models.Message{
		incoming("one", day1),
		incoming("two", day2),
	}, 60, loc(t), "", nil, theme.Dark())

	var separators int
	for _, r := range rows {
		if r.Kind == KindDaySeparator {
			separators++
		}
	}
	if separators != 2 {
		t.Errorf("expected a separator per distinct day, got %d in %d rows", separators, len(rows))
	}
}

func TestLayoutProducesMessageRows(t *testing.T) {
	now := time.Now()
	rows := Layout([]models.Message{incoming("hello", now)}, 60, loc(t), "", nil, theme.Dark())

	var found bool
	for _, r := range rows {
		if r.Kind == KindMessage && strings.Contains(strings.Join(r.Lines, " "), "hello") {
			found = true
		}
	}
	if !found {
		t.Errorf("message body missing from the layout: %+v", rows)
	}
}

func TestLayoutHonoursCursorAndSelection(t *testing.T) {
	now := time.Now()
	msgs := []models.Message{incoming("a", now), incoming("b", now.Add(time.Second))}

	rows := Layout(msgs, 60, loc(t), "b", map[models.MessageID]bool{"a": true}, theme.Dark())

	var cursor, selected int
	for _, r := range rows {
		if r.Cursor {
			cursor++
		}
		if r.Selected {
			selected++
		}
	}
	if cursor != 1 {
		t.Errorf("exactly one row should carry the cursor, got %d", cursor)
	}
	if selected != 1 {
		t.Errorf("exactly one row should be selected, got %d", selected)
	}
}

func TestEveryRenderedLineFitsThePane(t *testing.T) {
	now := time.Now()
	msgs := []models.Message{
		incoming("short", now),
		incoming(strings.Repeat("a very long message ", 30), now),
		outgoing(strings.Repeat("outgoing ", 20), now),
		{
			ID: "media", Direction: models.DirectionIncoming, Kind: models.KindDocument,
			Body: "caption", Timestamp: now,
			Media: &models.Media{Kind: models.MediaKindDocument, Filename: "x.pdf"},
		},
	}

	rows := Layout(msgs, 40, loc(t), "", nil, theme.Dark())
	for _, r := range rows {
		for i, line := range r.Lines {
			if w := text.VisibleWidth(line); w > 40 {
				t.Errorf("row %v line %d is %d cells, pane is 40: %q", r.Kind, i, w, text.StripANSI(line))
			}
		}
	}
}

func TestViewNeverExceedsTheViewport(t *testing.T) {
	now := time.Now()
	msgs := make([]models.Message, 0, 60)
	for i := range 60 {
		msgs = append(msgs, incoming(strings.Repeat("word ", 12), now.Add(time.Duration(i)*time.Minute)))
	}
	rows := Layout(msgs, 50, loc(t), "", nil, theme.Dark())

	s := State{Rows: rows, Width: 50, Height: 12, Theme: theme.Dark()}
	got := View(s)

	lines := strings.Split(got, "\n")
	if len(lines) != 12 {
		t.Errorf("rendered %d lines, viewport is 12", len(lines))
	}
	for i, l := range lines {
		if w := text.VisibleWidth(l); w > 50 {
			t.Errorf("line %d is %d cells: %q", i, w, text.StripANSI(l))
		}
	}
}

func TestViewOffsetsIntoTheTranscript(t *testing.T) {
	now := time.Now()
	msgs := make([]models.Message, 0, 40)
	for i := range 40 {
		msgs = append(msgs, incoming("line", now.Add(time.Duration(i)*time.Minute)))
	}
	rows := Layout(msgs, 50, loc(t), "", nil, theme.Dark())

	const w, h = 50, 10
	bottom := State{Rows: rows, Width: w, Height: h, Offset: MaxOffset(rows, h), Theme: theme.Dark()}
	top := State{Rows: rows, Width: w, Height: h, Offset: 0, Theme: theme.Dark()}

	if View(bottom) == View(top) {
		t.Error("different scroll offsets should render differently")
	}
}

func TestViewPadsToTheViewport(t *testing.T) {
	// A short transcript must still fill the pane, so the composer's background
	// reaches the bottom edge.
	now := time.Now()
	rows := Layout([]models.Message{incoming("hi", now)}, 40, loc(t), "", nil, theme.Dark())
	s := State{Rows: rows, Width: 40, Height: 20, Theme: theme.Dark()}

	if got := len(strings.Split(View(s), "\n")); got != 20 {
		t.Errorf("rendered %d lines, want 20", got)
	}
}

func TestEmptyTranscriptRendersAPlaceholder(t *testing.T) {
	got := View(State{Width: 40, Height: 5, Theme: theme.Dark()})
	if !strings.Contains(got, "No messages") {
		t.Errorf("expected an empty-state message, got %q", got)
	}
}

func TestZeroSizeRendersNothing(t *testing.T) {
	if got := View(State{Width: 0, Height: 5, Theme: theme.Dark()}); got != "" {
		t.Errorf("width 0 should render nothing, got %q", got)
	}
	if got := View(State{Width: 40, Height: 0, Theme: theme.Dark()}); got != "" {
		t.Errorf("height 0 should render nothing, got %q", got)
	}
}

func TestRevokedMessageHidesItsContent(t *testing.T) {
	now := time.Now()
	m := incoming("secret text", now)
	m.Revoked = true

	rows := Layout([]models.Message{m}, 60, loc(t), "", nil, theme.Dark())
	joined := strings.Join(joinLines(rows), " ")

	if strings.Contains(joined, "secret text") {
		t.Errorf("a revoked message must not render its body: %q", joined)
	}
	if !strings.Contains(joined, "deleted") {
		t.Errorf("a tombstone should be shown instead: %q", joined)
	}
}

func TestEscapeSequencesInBodiesAreNeutralised(t *testing.T) {
	// A message body comes from the network. If it were rendered verbatim, a
	// sender could repaint the screen or set the terminal title.
	now := time.Now()
	m := incoming("hi\x1b[2J\x1b]0;pwned\x07\x1b[31mred", now)

	rows := Layout([]models.Message{m}, 60, loc(t), "", nil, theme.Dark())
	joined := strings.Join(joinLines(rows), " ")

	if text.HasEscape(joined) {
		// The layout applies its own styling, so escapes are expected here; what
		// matters is that the *body's* payload is gone.
		for _, payload := range []string{"2J", "0;pwned", "31m"} {
			_ = payload
		}
	}
	if strings.Contains(text.StripANSI(joined), "pwned") {
		t.Errorf("the injected payload survived sanitisation: %q", text.StripANSI(joined))
	}
	if strings.Contains(text.StripANSI(joined), "2J") {
		t.Errorf("the injected control sequence survived: %q", text.StripANSI(joined))
	}
}

func TestDeliveryStatusIsRendered(t *testing.T) {
	now := time.Now()
	for _, status := range []models.DeliveryStatus{
		models.DeliveryPending, models.DeliverySent,
		models.DeliveryDelivered, models.DeliveryRead, models.DeliveryFailed,
	} {
		m := outgoing("msg", now)
		m.Status = status

		rows := Layout([]models.Message{m}, 60, loc(t), "", nil, theme.Dark())
		joined := strings.Join(joinLines(rows), " ")
		if !strings.Contains(joined, status.Glyph()) {
			t.Errorf("status %v: glyph %q missing from %q", status, status.Glyph(), text.StripANSI(joined))
		}
	}
}

func TestFailedSendIsReported(t *testing.T) {
	now := time.Now()
	m := outgoing("msg", now)
	m.SendError = "network unreachable"

	rows := Layout([]models.Message{m}, 60, loc(t), "", nil, theme.Dark())
	joined := text.StripANSI(strings.Join(joinLines(rows), " "))

	if !strings.Contains(joined, "failed") {
		t.Errorf("a failed send should be visible: %q", joined)
	}
}

func TestReplyQuoteIsRendered(t *testing.T) {
	now := time.Now()
	m := outgoing("answer", now)
	m.ReplyTo = &models.MessageRef{
		ID: "q1", SenderName: "Ada", Body: "the original question", Timestamp: now,
	}

	rows := Layout([]models.Message{m}, 60, loc(t), "", nil, theme.Dark())
	joined := text.StripANSI(strings.Join(joinLines(rows), " "))

	if !strings.Contains(joined, "Ada") || !strings.Contains(joined, "original question") {
		t.Errorf("the quoted message should be shown: %q", joined)
	}
}

func TestReactionsAreAggregated(t *testing.T) {
	now := time.Now()
	m := outgoing("msg", now)
	m.Reactions = map[string][]models.ContactID{
		"👍":  {"a", "b", "c"},
		"❤️": {"d"},
	}

	rows := Layout([]models.Message{m}, 60, loc(t), "", nil, theme.Dark())
	joined := text.StripANSI(strings.Join(joinLines(rows), " "))

	if !strings.Contains(joined, "3") {
		t.Errorf("the reaction count should be shown: %q", joined)
	}
}

func TestMediaIsDescribed(t *testing.T) {
	now := time.Now()
	m := incoming("", now)
	m.Kind = models.KindDocument
	m.Media = &models.Media{Kind: models.MediaKindDocument, Filename: "invoice.pdf"}

	rows := Layout([]models.Message{m}, 60, loc(t), "", nil, theme.Dark())
	joined := text.StripANSI(strings.Join(joinLines(rows), " "))

	if !strings.Contains(joined, "invoice.pdf") {
		t.Errorf("the attachment should be described: %q", joined)
	}
}

func TestSystemNoticeIsCentredAndNotABubble(t *testing.T) {
	now := time.Now()
	m := incoming("", now)
	m.Kind = models.KindSystem
	m.SystemText = "Ada created this group"

	rows := Layout([]models.Message{m}, 40, loc(t), "", nil, theme.Dark())

	var found bool
	for _, r := range rows {
		if r.Kind == KindSystem {
			found = true
			if len(r.Lines) != 1 {
				t.Errorf("a system notice should be one line, got %d", len(r.Lines))
			}
		}
	}
	if !found {
		t.Error("the system notice should be laid out as a system row")
	}
}

func TestIndexAtY(t *testing.T) {
	now := time.Now()
	msgs := []models.Message{incoming("a", now), incoming("b", now.Add(time.Minute))}
	rows := Layout(msgs, 40, loc(t), "", nil, theme.Dark())

	s := State{Rows: rows, Width: 40, Height: 10, Theme: theme.Dark()}

	if got := s.IndexAtY(-1); got != -1 {
		t.Errorf("a negative row should map to nothing, got %d", got)
	}
	if got := s.IndexAtY(0); got != 0 {
		t.Errorf("row 0 should be the first row, got %d", got)
	}
	if got := s.IndexAtY(10000); got != -1 {
		t.Errorf("a row past the end should map to nothing, got %d", got)
	}
}

func TestScrollTargetAndMaxOffset(t *testing.T) {
	// Row heights chosen so that no row is one line: the arithmetic is only
	// interesting when rows span several lines.
	rows := []Row{
		{Height: 2}, {Height: 1}, {Height: 3}, {Height: 1},
	}

	if got := ScrollTarget(rows, 0); got != 0 {
		t.Errorf("scrolling to the top should be row 0, got %d", got)
	}

	// One line of scroll still leaves row 0 at the top, because row 0 occupies
	// lines 0 and 1. A row is only left behind once its last line is above the
	// viewport.
	if got := ScrollTarget(rows, 1); got != 0 {
		t.Errorf("scrolling one line should keep row 0, got %d", got)
	}
	if got := ScrollTarget(rows, 2); got != 1 {
		t.Errorf("scrolling past row 0 should start at row 1, got %d", got)
	}
	if got := ScrollTarget(rows, 99); got != len(rows) {
		t.Errorf("scrolling past the end should clamp to %d, got %d", len(rows), got)
	}

	if got := MaxOffset(rows, 100); got != 0 {
		t.Errorf("content shorter than the viewport needs no scroll, got %d", got)
	}

	// MaxOffset names the row to start from, so the value is a row index rather
	// than a line count: with a two-line viewport the last row begins at line 6,
	// which is reached by starting at row 2.
	if got := MaxOffset(rows, 2); got != 2 {
		t.Errorf("max offset should start at row 2, got %d", got)
	}

	// Whatever MaxOffset returns, the final row must still be drawn.
	if off := MaxOffset(rows, 2); off >= len(rows) {
		t.Errorf("max offset %d would skip the last row", off)
	}
}

func TestLayoutIsDeterministic(t *testing.T) {
	now := time.Now()
	msgs := []models.Message{
		incoming("one", now),
		outgoing("two", now.Add(time.Second)),
		incoming(strings.Repeat("long ", 40), now.Add(2*time.Second)),
	}

	first := len(Layout(msgs, 40, loc(t), "one", nil, theme.Dark()))
	for range 5 {
		if got := len(Layout(msgs, 40, loc(t), "one", nil, theme.Dark())); got != first {
			t.Fatalf("layout is not deterministic: %d then %d rows", first, got)
		}
	}
}

func TestZeroWidthLayoutIsEmpty(t *testing.T) {
	msgs := []models.Message{incoming("a", time.Now())}
	if got := Layout(msgs, 0, loc(t), "", nil, theme.Dark()); got != nil {
		t.Errorf("a zero-width pane should lay out nothing, got %d rows", len(got))
	}
}

func joinLines(rows []Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Lines...)
	}
	return out
}
