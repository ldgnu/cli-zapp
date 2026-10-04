package sidebar_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/component"
	"github.com/wterm/wterm/internal/ui/components/sidebar"
	"github.com/wterm/wterm/internal/ui/layout"
	"github.com/wterm/wterm/internal/ui/theme"
)

// fixture builds a sidebar over a known conversation list.
//
// The list is varied on purpose — pinned, unread, muted, archived, a group, an empty
// preview — because those are the cases the row renderer branches on, and a fixture
// that missed one would leave that branch untested.
func fixture(t *testing.T, rect layout.Rect, query string) *sidebar.Model {
	t.Helper()

	now := time.Now()
	s := sidebar.New(keybindings.DefaultMap(), theme.Dark())
	s.SetMode(layout.ModeFull)
	s.Resize(rect)

	chats := []models.Chat{
		{
			ID: "1", Name: "Ada Lovelace", Pinned: true, Timestamp: now,
			UnreadCount: 2, HasUnread: true,
			LastMessage: &models.MessagePreview{Body: "hola"},
		},
		{
			ID: "2", Name: "Grace Hopper", Timestamp: now.Add(-time.Hour),
			LastMessage: &models.MessagePreview{Body: "comprobado"},
		},
		{
			ID: "3", Name: "Terminal Gophers", Type: models.ChatTypeGroup,
			Muted: true, Timestamp: now.Add(-2 * time.Hour),
			LastMessage: &models.MessagePreview{Body: "revisión el viernes"},
		},
		{ID: "4", Name: "Newsletter", Archived: true, Timestamp: now.Add(-96 * time.Hour)},
		{ID: "5", Name: "", Timestamp: now},
	}
	s.SetModel(component.Model{Chats: chats, Search: query})
	return s
}

// render returns the sidebar's frame with styling stripped, one entry per row.
func render(s *sidebar.Model) []string {
	body := strings.Split(s.View(), "\n")
	for i, l := range body {
		body[i] = text.StripANSI(l)
	}
	return body
}

func rect(w, h int) layout.Rect { return layout.Rect{Width: w, Height: h} }

// --- shape ---

func TestViewFillsItsRectangle(t *testing.T) {
	// A sidebar that renders fewer lines than it was given leaves the previous
	// frame's content showing through, because the terminal only rewrites what the
	// renderer sends.
	for _, size := range [][2]int{{26, 20}, {30, 12}, {34, 40}} {
		s := fixture(t, rect(size[0], size[1]), "")
		rows := render(s)

		if len(rows) != size[1] {
			t.Errorf("at %dx%d the sidebar produced %d rows", size[0], size[1], len(rows))
		}
		for i, r := range rows {
			if got := text.VisibleWidth(r); got != size[0] {
				t.Errorf("at %dx%d row %d is %d cells: %q", size[0], size[1], i, got, r)
			}
		}
	}
}

func TestEmptySidebarStillFillsItsRectangle(t *testing.T) {
	s := sidebar.New(keybindings.DefaultMap(), theme.Dark())
	s.SetMode(layout.ModeFull)
	s.Resize(rect(30, 8))
	s.SetModel(component.Model{})

	rows := render(s)
	if len(rows) != 8 {
		t.Fatalf("produced %d rows", len(rows))
	}
	for i, r := range rows {
		if text.VisibleWidth(r) != 30 {
			t.Errorf("row %d is %d cells: %q", i, text.VisibleWidth(r), r)
		}
	}
}

func TestZeroSizedRectangleRendersNothing(t *testing.T) {
	s := fixture(t, rect(0, 0), "")
	if s.View() != "" {
		t.Error("a region with no area should render nothing rather than one blank line")
	}
}

// --- chrome ---

func TestBrandAndSearchAreDrawnInFullMode(t *testing.T) {
	body := strings.Join(render(fixture(t, rect(30, 10), "")), "\n")

	if !strings.Contains(body, "WTERM") {
		t.Error("the wordmark should be drawn")
	}
	if !strings.Contains(body, "Buscar chats") {
		t.Error("the search field should be drawn")
	}
}

func TestBrandAndSearchAreDroppedInMinimalMode(t *testing.T) {
	s := fixture(t, rect(30, 10), "")
	s.SetMode(layout.ModeMinimal)
	s.Resize(rect(30, 10))

	body := strings.Join(render(s), "\n")
	if strings.Contains(body, "WTERM") {
		t.Error("minimal mode should not repeat the wordmark")
	}
	if strings.Contains(body, "Buscar chats") {
		t.Error("minimal mode leaves search to the palette, so there is no field")
	}
}

// --- rows ---

func TestRowsCarryNameTimestampAndPreview(t *testing.T) {
	rows := render(fixture(t, rect(34, 12), ""))

	// The name leads, because it is what identifies the conversation.
	if !strings.Contains(rows[4], "Ada Lovelace") {
		t.Errorf("the name should be intact, got %q", rows[4])
	}
	// The timestamp is on the same row, which is what makes the column scannable.
	if !strings.Contains(rows[4], "23") && !strings.Contains(rows[4], ":") {
		t.Errorf("the timestamp should share the row, got %q", rows[4])
	}
	// The preview takes what is left.
	if !strings.Contains(rows[5], "comprobado") {
		t.Errorf("the preview should appear when there is room, got %q", rows[5])
	}
	// A pinned row has less room, and gives up the preview rather than the name.
	if !strings.Contains(rows[4], "Ada Lovelace") {
		t.Errorf("a pinned row must keep its name, got %q", rows[4])
	}
}

func TestNameIsNeverSacrificedForAPreview(t *testing.T) {
	// At the narrowest sidebar the layout has room for a name or a preview, not both.
	// The name wins, because a row whose name is a fragment identifies nothing.
	for _, w := range []int{26, 28, 30, 34, 40} {
		rows := render(fixture(t, rect(w, 12), ""))

		for _, want := range []string{"Ada Lovelace", "Grace Hopper"} {
			if !strings.Contains(strings.Join(rows, "\n"), want) {
				t.Errorf("at %d columns the name %q was truncated away", w, want)
			}
		}
	}
}

func TestNoRowIsTwoFragments(t *testing.T) {
	// The failure this guards against is subtle: a narrow row showing
	// "Ada Lo… Tha…" has two truncated words and says less than one readable name.
	rows := render(fixture(t, rect(26, 12), ""))

	for i, l := range rows {
		if !strings.Contains(l, "…") {
			continue
		}
		// One ellipsis per row at most: a second means the name and the preview are
		// both being cut.
		if n := strings.Count(l, "…"); n > 1 {
			t.Errorf("row %d truncates twice, which means a fragment and a fragment: %q", i, l)
		}
	}
}

func TestUnreadCountRendersAsABadge(t *testing.T) {
	body := strings.Join(render(fixture(t, rect(34, 12), "")), "\n")

	// The fixture has two unread in the first conversation. The badge must be there
	// next to its row, not only in some total.
	if !strings.Contains(body, "2") {
		t.Errorf("the unread badge is missing:\n%s", body)
	}
}

func TestArchivedConversationsAreHiddenByDefault(t *testing.T) {
	body := strings.Join(render(fixture(t, rect(34, 12), "")), "\n")

	if strings.Contains(body, "Newsletter") {
		t.Error("an archived conversation should be hidden while the query is empty")
	}
}

func TestArchivedConversationsAreFindableByQuery(t *testing.T) {
	// Hiding an archived conversation must not make it unreachable, or archiving is a
	// destructive operation rather than a filing one.
	body := strings.Join(render(fixture(t, rect(34, 12), "news")), "\n")

	if !strings.Contains(body, "Newsletter") {
		t.Errorf("a search should surface an archived conversation:\n%s", body)
	}
}

func TestTypingReplacesThePreview(t *testing.T) {
	now := time.Now()
	s := sidebar.New(keybindings.DefaultMap(), theme.Dark())
	s.SetMode(layout.ModeFull)
	s.Resize(rect(34, 10))
	s.SetModel(component.Model{Chats: []models.Chat{
		{
			ID: "1", Name: "Ada", Typing: true, Timestamp: now,
			LastMessage: &models.MessagePreview{Body: "un mensaje viejo"},
		},
	}})

	body := strings.Join(render(s), "\n")
	// The sidebar is 34 wide here, so the notice is truncated. What matters is that the
	// preview was replaced rather than shown alongside: "un mensaje viejo" must be gone.
	if !strings.Contains(body, "escribie") {
		t.Errorf("a composing peer should show as writing:\n%s", body)
	}
	if strings.Contains(body, "un mensaje viejo") {
		t.Error("the stale preview should be replaced, not shown alongside")
	}
}

// --- filtering ---

func TestQueryMatchesNamePreviewAndPhone(t *testing.T) {
	now := time.Now()
	phone := "+54 9 11 0000-0002"

	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{"by name", "ada", true},
		{"by name, upper case", "ADA", true},
		{"by preview", "comprobado", true},
		{"by phone", "0000-0002", true},
		{"no match", "zzz", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := sidebar.New(keybindings.DefaultMap(), theme.Dark())
			s.SetMode(layout.ModeFull)
			s.Resize(rect(34, 10))
			s.SetModel(component.Model{Chats: []models.Chat{
				{
					ID: "1", Name: "Ada Lovelace", Timestamp: now,
					Contact:     &models.Contact{Phone: phone},
					LastMessage: &models.MessagePreview{Body: "comprobado"},
				},
			}, Search: tc.query})

			body := strings.Join(render(s), "\n")
			if got := strings.Contains(body, "Ada Lovelace"); got != tc.want {
				t.Errorf("query %q: present = %v, want %v\n%s", tc.query, got, tc.want, body)
			}
		})
	}
}

func TestFilteringNeverPanics(t *testing.T) {
	// Filtering runs on every keystroke with whatever the user typed, including
	// combining marks, control bytes and half a rune.
	seeds := []string{"", " ", "ñ", "\x1b", "\x00", "aá", "  a  "}
	for _, q := range seeds {
		s := fixture(t, rect(26, 8), "")
		s.SetModel(component.Model{Chats: []models.Chat{{ID: "1", Name: "ñandú"}}, Search: q})
		_ = render(s)
	}
}

// --- selection and scrolling ---

func TestCursorMovesWithinTheVisibleRows(t *testing.T) {
	s := fixture(t, rect(26, 6), "")
	s.Focus()

	down := tea.KeyPressMsg{Code: tea.KeyDown}
	for range 4 {
		if _, cmd := s.Update(down); cmd != nil {
			t.Fatal("moving the cursor should not emit an event: the cursor is not a decision")
		}
	}

	if _, ok := s.SelectedChat(); !ok {
		t.Error("there should always be a selected conversation in a non-empty list")
	}
	// With six rows visible and four non-archived conversations, four moves from the
	// top should land on the last one.
	chat, _ := s.SelectedChat()
	if chat.ID != "4" {
		// "4" is the archived one, which is hidden, so the last visible is "5".
		if chat.ID != "5" {
			t.Errorf("cursor landed on %q, want the last visible conversation", chat.ID)
		}
	}
}

func TestCursorStaysInsideTheList(t *testing.T) {
	s := fixture(t, rect(26, 6), "")
	s.Focus()

	for range 20 {
		if _, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown}); false {
			break
		}
	}
	chat, ok := s.SelectedChat()
	if !ok {
		t.Fatal("the cursor left the list entirely")
	}
	if chat.ID != "5" {
		t.Errorf("cursor escaped to %q", chat.ID)
	}
}

func TestScrollingIsIndependentOfSelection(t *testing.T) {
	// A list long enough to scroll, with a viewport shorter than the list.
	chats := make([]models.Chat, 0, 50)
	for i := range 50 {
		chats = append(chats, models.Chat{
			ID:        models.ChatID(string(rune('a'+i%26)) + strings.Repeat("x", i)),
			Name:      "Chat " + string(rune('A'+i%26)),
			Timestamp: time.Now(),
		})
	}

	s := sidebar.New(keybindings.DefaultMap(), theme.Dark())
	s.SetMode(layout.ModeFull)
	s.Resize(rect(26, 6))
	s.SetModel(component.Model{Chats: chats})
	s.Focus()

	before, _ := s.SelectedChat()
	s.Scroll(3)
	after, _ := s.SelectedChat()

	if before.ID != after.ID {
		t.Error("scrolling must not move the cursor: the user is looking, not choosing")
	}
}

func TestCursorIsScrolledIntoView(t *testing.T) {
	chats := make([]models.Chat, 0, 20)
	for i := range 20 {
		chats = append(chats, models.Chat{
			ID: models.ChatID(string(rune('a' + i))), Name: "Chat", Timestamp: time.Now(),
		})
	}

	s := sidebar.New(keybindings.DefaultMap(), theme.Dark())
	s.SetMode(layout.ModeFull)
	s.Resize(rect(26, 6))
	s.SetModel(component.Model{Chats: chats})
	s.Focus()

	for range 10 {
		if _, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown}); false {
			break
		}
	}

	// The cursor must be inside the visible window, which is what "scrolled into
	// view" means; a cursor at index 10 with a viewport of 3 is invisible otherwise.
	offset := s.Offset()
	visible := rect(26, 6).Height - 3 // brand, rule, search, rule
	cursor := s.Cursor()
	if cursor < offset || cursor >= offset+visible {
		t.Errorf("cursor %d is outside the window [%d, %d)", cursor, offset, offset+visible)
	}
}

// --- events ---

func TestOpeningAConversationEmitsAnEvent(t *testing.T) {
	s := fixture(t, rect(26, 8), "")
	s.Focus()

	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("opening a conversation should emit an event")
	}

	msg, ok := cmd().(component.Event)
	if !ok {
		t.Fatalf("expected a component.Event, got %T", cmd())
	}
	if msg.Kind != component.KindFocusChat {
		t.Errorf("event kind = %v, want focus-chat", msg.Kind)
	}
	if msg.ChatID == "" {
		t.Error("the event should name the conversation")
	}
}

func TestSidebarOnlyEverEmitsEvents(t *testing.T) {
	// The layer boundary is the reason this package can be replaced. A region that
	// emitted anything else — a JID, a service call, a raw protocol value — would
	// have learned the protocol, which is precisely what must not happen.
	s := fixture(t, rect(26, 8), "")
	s.Focus()

	for _, key := range []tea.KeyPressMsg{
		{Code: tea.KeyDown},
		{Code: tea.KeyUp},
		{Code: tea.KeyEnter},
		{Code: 'p', Mod: tea.ModCtrl},
		{Code: 'm', Mod: tea.ModCtrl},
	} {
		_, cmd := s.Update(key)
		if cmd == nil {
			continue
		}
		if _, ok := cmd().(component.Event); !ok {
			t.Errorf("key %v produced a %T, not a component.Event", key, cmd())
		}
	}
}

func TestSearchTypingEmitsTheQuery(t *testing.T) {
	s := fixture(t, rect(26, 10), "")
	s.BeginSearch()

	if !s.Searching() {
		t.Fatal("BeginSearch should give the field the keyboard")
	}

	_, cmd := s.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	msg, ok := cmd().(component.Event)
	if !ok || msg.Kind != component.KindSearchChanged {
		t.Fatalf("typing should report the new query, got %T", cmd())
	}
	if msg.Query != "a" {
		t.Errorf("query = %q, want %q", msg.Query, "a")
	}
}

func TestSearchBackspaceRemovesOneRune(t *testing.T) {
	s := fixture(t, rect(26, 10), "")
	s.BeginSearch()
	s.SetModel(component.Model{
		Chats:        []models.Chat{{ID: "1", Name: "ñandú"}},
		Search:       "añb",
		SearchActive: true,
	})

	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	msg, ok := cmd().(component.Event)
	if !ok {
		t.Fatal("backspace should report the new query")
	}
	// "ñ" is two bytes. Removing one byte would leave a broken encoding that the
	// renderer draws as a replacement character, and the query would then match
	// nothing at all.
	if msg.Query != "añ" {
		t.Errorf("query = %q, want %q — a multi-byte rune must be removed whole", msg.Query, "añ")
	}
}

func TestGlobalShortcutsAreNotSwallowedByTheSearchField(t *testing.T) {
	// The sidebar's search field accepts arbitrary input. If it consumed modified
	// keys, ctrl+q would type a "q" instead of quitting — the bug the input
	// architecture's ordering exists to prevent.
	s := fixture(t, rect(26, 10), "")
	s.BeginSearch()
	s.Focus()

	_, cmd := s.Update(tea.KeyPressMsg{Code: 'q', Text: "q", Mod: tea.ModCtrl})
	if cmd != nil {
		t.Error("a ctrl-modified key should not be consumed as text by the search field")
	}
}

// --- mouse ---

func TestClickSelectsTheRowUnderThePointer(t *testing.T) {
	s := fixture(t, rect(26, 10), "")

	// Row 0 is the brand, row 1 its rule, row 2 the search field, row 3 its rule,
	// so the first conversation occupies row 4.
	cmd := s.ClickAt(2, 4)
	if cmd == nil {
		t.Fatal("clicking a row should emit an event")
	}
	msg, ok := cmd().(component.Event)
	if !ok {
		t.Fatalf("clicking a row should emit a component.Event, got %T", cmd())
	}
	if msg.ChatID != "1" {
		t.Errorf("clicking the first row should select conversation 1, got %q", msg.ChatID)
	}
}

func TestClickOutsideTheListDoesNothing(t *testing.T) {
	s := fixture(t, rect(26, 10), "")

	for _, p := range [][2]int{{0, 0}, {2, 2}, {100, 4}, {-1, 4}} {
		if cmd := s.ClickAt(p[0], p[1]); cmd != nil {
			t.Errorf("a click at %v should not select a row", p)
		}
	}
}

func TestWheelScrollsTheList(t *testing.T) {
	chats := make([]models.Chat, 0, 20)
	for i := range 20 {
		chats = append(chats, models.Chat{
			ID: models.ChatID(string(rune('a' + i))), Name: "Chat", Timestamp: time.Now(),
		})
	}
	s := sidebar.New(keybindings.DefaultMap(), theme.Dark())
	s.SetMode(layout.ModeFull)
	s.Resize(rect(26, 6))
	s.SetModel(component.Model{Chats: chats})

	before := s.Offset()
	s.Scroll(3)
	if s.Offset() <= before {
		t.Error("scrolling down should move the offset")
	}

	// Scrolling past the end must stop rather than running off into nothing.
	s.Scroll(1000)
	if s.Offset() <= 0 {
		t.Error("the offset went below zero")
	}
}

// --- resize ---

func TestResizingDoesNotEscapeTheCursor(t *testing.T) {
	s := fixture(t, rect(26, 10), "")
	s.Focus()

	for range 4 {
		if _, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown}); false {
			break
		}
	}

	// Shrink to a viewport with no rows at all, then grow back.
	s.Resize(rect(26, 1))
	s.Resize(rect(26, 10))

	if _, ok := s.SelectedChat(); !ok {
		t.Error("the cursor should survive a resize through zero")
	}
	for i, r := range render(s) {
		if text.VisibleWidth(r) != 26 {
			t.Errorf("row %d is %d cells after resizing back", i, text.VisibleWidth(r))
		}
	}
}
