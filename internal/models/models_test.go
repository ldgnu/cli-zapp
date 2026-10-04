package models

import (
	"testing"
	"time"
)

func TestSortChatsPinnedFirst(t *testing.T) {
	now := time.Now()
	chats := []Chat{
		{ID: "a", Name: "Alice", Timestamp: now},
		{ID: "b", Name: "Bob", Timestamp: now.Add(time.Hour), Pinned: true},
		{ID: "c", Name: "Carol", Timestamp: now.Add(time.Minute)},
	}
	SortChats(chats)

	if chats[0].ID != "b" {
		t.Errorf("pinned chat should sort first, got %q", chats[0].ID)
	}
	if chats[1].ID != "c" || chats[2].ID != "a" {
		t.Errorf("remaining chats should sort by recency, got %q then %q", chats[1].ID, chats[2].ID)
	}
}

func TestSortChatsStableOnFullTie(t *testing.T) {
	// Two chats that compare equal on every key must retain their input order,
	// otherwise the sidebar reshuffles during an incremental sync.
	ts := time.Now()
	chats := []Chat{
		{ID: "a", Name: "Same", Timestamp: ts},
		{ID: "b", Name: "Same", Timestamp: ts},
		{ID: "c", Name: "Same", Timestamp: ts},
	}
	SortChats(chats)

	for i, want := range []ChatID{"a", "b", "c"} {
		if chats[i].ID != want {
			t.Errorf("position %d: want %q, got %q", i, want, chats[i].ID)
		}
	}
}

func TestSortChatsNameTiebreakIsCaseAndSpaceInsensitive(t *testing.T) {
	ts := time.Now()
	chats := []Chat{
		{ID: "a", Name: "bob", Timestamp: ts},
		{ID: "b", Name: "  Ana   Lopez ", Timestamp: ts},
		{ID: "c", Name: "ANA LOPEZ", Timestamp: ts},
	}
	SortChats(chats)

	// "  Ana   Lopez " and "ANA LOPEZ" fold to the same string and tie, so the
	// stable sort keeps them in input order; "bob" sorts last.
	if chats[0].ID != "b" {
		t.Errorf("first chat should be the Ana Lopez variant, got %q", chats[0].ID)
	}
	if chats[2].ID != "a" {
		t.Errorf("last chat should be bob, got %q", chats[2].ID)
	}
}

func TestSortMessagesTimestampThenID(t *testing.T) {
	base := time.Now().Truncate(time.Second)
	msgs := []Message{
		{ID: "z", Timestamp: base},
		{ID: "a", Timestamp: base},
		{ID: "m", Timestamp: base.Add(-time.Second)},
	}
	SortMessages(msgs)

	want := []MessageID{"m", "a", "z"}
	for i, w := range want {
		if msgs[i].ID != w {
			t.Errorf("position %d: want %q, got %q", i, w, msgs[i].ID)
		}
	}
}

func TestMessageTextHidesRevokedBody(t *testing.T) {
	m := Message{Body: "secret", Revoked: true}
	if got := m.Text(); got != "" {
		t.Errorf("a revoked message must not expose its body, got %q", got)
	}
}

func TestMessageReactionSummaryDeterministic(t *testing.T) {
	m := Message{Reactions: map[string][]ContactID{
		"❤️": {"a", "b"},
		"👍":  {"c"},
		"🎉":  {"d"},
	}}

	// Map iteration order is randomised by the runtime; the rendered bar must
	// not reshuffle between frames, so require a stable order across calls.
	first := m.ReactionSummary()
	for range 20 {
		got := m.ReactionSummary()
		if len(got) != len(first) {
			t.Fatalf("reaction count changed between calls: %d then %d", len(first), len(got))
		}
		for i := range got {
			if got[i].Glyph != first[i].Glyph {
				t.Fatalf("reaction order not stable at %d: %q then %q", i, first[i].Glyph, got[i].Glyph)
			}
		}
	}
}

func TestMessagePreviewNeverContainsNewline(t *testing.T) {
	p := MessagePreview{Kind: KindText, Body: "line one\nline two\n\nline three"}
	got := p.String()
	for i := range len(got) {
		if got[i] == '\n' || got[i] == '\t' {
			t.Fatalf("preview must be single-line, got %q", got)
		}
	}
	if got != "line one line two line three" {
		t.Errorf("unexpected preview: %q", got)
	}
}

func TestMessagePreviewPrefixesOutgoing(t *testing.T) {
	in := MessagePreview{Kind: KindText, Body: "hi"}
	out := MessagePreview{Kind: KindText, Body: "hi", IsOutgoing: true}

	if in.String() != "hi" {
		t.Errorf("incoming preview should not be prefixed, got %q", in.String())
	}
	if out.String() != "You: hi" {
		t.Errorf("outgoing preview should be prefixed, got %q", out.String())
	}
}

func TestMessagePreviewMediaFallback(t *testing.T) {
	tests := []struct {
		name string
		p    MessagePreview
		want string
	}{
		{
			name: "document with filename",
			p: MessagePreview{
				Kind:  KindDocument,
				Media: &Media{Kind: MediaKindDocument, Filename: "invoice.pdf"},
			},
			want: "document: invoice.pdf",
		},
		{
			name: "image without filename",
			p:    MessagePreview{Kind: KindImage, Media: &Media{Kind: MediaKindImage}},
			want: "image",
		},
		{
			name: "media with no Media at all",
			p:    MessagePreview{Kind: KindVideo},
			want: "video",
		},
		{
			name: "location",
			p:    MessagePreview{Kind: KindLocation},
			want: "location",
		},
		{
			name: "empty text",
			p:    MessagePreview{Kind: KindText},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.String(); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestFallbackNameNeverEmpty(t *testing.T) {
	tests := []struct {
		name string
		chat Chat
		want string
	}{
		{"uses name", Chat{Name: "  Ada  "}, "Ada"},
		{"short id", Chat{ID: "abc"}, "abc"},
		{"long id truncated", Chat{ID: "12345678901234567890"}, "…901234567890"},
		{"no id", Chat{}, "(unknown chat)"},
		{"blank name falls back", Chat{ID: "x", Name: "   "}, "x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.chat.FallbackName()
			if got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestPresenceLabelDistinguishesUnknownFromOffline(t *testing.T) {
	// Conflating these two would tell the user a falsehood, so assert they
	// render differently.
	unknown := Presence{State: PresenceUnknown}
	offline := Presence{State: PresenceUnavailable}

	if unknown.Label() != "" {
		t.Errorf("unknown presence should render empty, got %q", unknown.Label())
	}
	if offline.Label() != "offline" {
		t.Errorf("unavailable presence should render offline, got %q", offline.Label())
	}
	if unknown.IsOnline() || offline.IsOnline() {
		t.Error("neither unknown nor unavailable presence means online")
	}
	if !(Presence{State: PresenceAvailable}).IsOnline() {
		t.Error("available presence means online")
	}
}

func TestInitials(t *testing.T) {
	tests := []struct {
		contact Contact
		want    string
	}{
		{Contact{Name: "Ada Lovelace"}, "AL"},
		{Contact{Name: "cher"}, "C"},
		{Contact{Name: "  spaced  out  "}, "SO"},
		{Contact{Name: "", Phone: "5491123456789"}, "5"},
		// A push name of pure punctuation is still the name; initials fall back
		// to the placeholder rather than reaching for the phone number.
		{Contact{Name: "!!!", Phone: "+54 9 11"}, "?"},
		{Contact{}, "?"},
	}
	for _, tc := range tests {
		t.Run(tc.contact.DisplayName(), func(t *testing.T) {
			if got := tc.contact.Initials(); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestDeliveryStatusGlyphAndTerminal(t *testing.T) {
	if !DeliveryRead.Terminal() {
		t.Error("read is terminal")
	}
	if !DeliveryFailed.Terminal() {
		t.Error("failed is terminal")
	}
	if DeliveryDelivered.Terminal() {
		t.Error("delivered can still advance to read")
	}
	// Read and delivered share a glyph and are distinguished by colour; assert
	// that intent explicitly so a future divergence is a deliberate change.
	if DeliveryRead.Glyph() != DeliveryDelivered.Glyph() {
		t.Error("read and delivered are expected to share a glyph, differing in colour")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{3 * 1024 * 1024 * 1024, "3.0 GiB"},
	}
	for _, tc := range tests {
		if got := FormatBytes(tc.in); got != tc.want {
			t.Errorf("FormatBytes(%d): want %q, got %q", tc.in, tc.want, got)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0:00"},
		{-5 * time.Second, "0:00"},
		{45 * time.Second, "0:45"},
		{90 * time.Second, "1:30"},
		{time.Hour, "1:00:00"},
		{3661 * time.Second, "1:01:01"},
	}
	for _, tc := range tests {
		if got := FormatDuration(tc.in); got != tc.want {
			t.Errorf("FormatDuration(%v): want %q, got %q", tc.in, tc.want, got)
		}
	}
}

func TestFormatRelativeTime(t *testing.T) {
	// Explicit dates rather than hour offsets: the boundaries this function
	// cares about are calendar boundaries, and hour arithmetic obscures them.
	loc := time.UTC
	at := func(month time.Month, d, hour, minute int) time.Time {
		return time.Date(2026, month, d, hour, minute, 0, 0, loc)
	}
	now := at(time.October, 3, 15, 30)

	tests := []struct {
		name string
		ts   time.Time
		want string
	}{
		{"zero", time.Time{}, ""},
		{"just now", now.Add(-10 * time.Second), "just now"},
		{"one minute", now.Add(-time.Minute), "1 minute ago"},
		{"many minutes", now.Add(-5 * time.Minute), "5 minutes ago"},
		{"same day shows clock", now.Add(-3 * time.Hour), "12:30"},
		{"same year shows date", at(time.September, 30, 8, 0), "Sep 30"},
		{"previous year shows year", time.Date(2025, time.December, 6, 8, 0, 0, 0, loc), "Dec 6, 2025"},
		// A peer with a skewed clock must not produce a negative age.
		{"future timestamp", now.Add(time.Hour), "just now"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatRelativeTime(tc.ts, now); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestFormatChatTimestamp(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, time.October, 3, 15, 30, 0, 0, loc)

	tests := []struct {
		name string
		ts   time.Time
		want string
	}{
		{"zero", time.Time{}, ""},
		{"today", time.Date(2026, time.October, 3, 9, 5, 0, 0, loc), "09:05"},
		{"this year", time.Date(2026, time.May, 17, 9, 5, 0, 0, loc), "May 17"},
		{"last year", time.Date(2024, time.May, 17, 9, 5, 0, 0, loc), "May 17, 2024"},
		{"future", now.Add(time.Hour), "16:30"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatChatTimestampAt(tc.ts, now); got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestDayKey(t *testing.T) {
	// A zone ahead of UTC crosses the date line where UTC does not, so the two
	// expectations differ and the test would catch a location being ignored.
	ts := time.Date(2026, time.October, 3, 20, 0, 0, 0, time.UTC)
	tokyo := time.FixedZone("JST", 9*3600)

	if got := DayKey(ts, time.UTC); got != "2026-10-03" {
		t.Errorf("day key in UTC, got %q", got)
	}
	if got := DayKey(ts, tokyo); got != "2026-10-04" {
		t.Errorf("day key must use the given location, got %q", got)
	}
}

func TestIDConstructorsRejectBlank(t *testing.T) {
	if _, err := NewChatID("   "); err == nil {
		t.Error("blank chat ID should be rejected")
	}
	if _, err := NewMessageID(""); err == nil {
		t.Error("empty message ID should be rejected")
	}
	if _, err := NewContactID("\t"); err == nil {
		t.Error("whitespace contact ID should be rejected")
	}
	if _, err := NewChatID("ok"); err != nil {
		t.Errorf("valid chat ID rejected: %v", err)
	}
}

func TestGroupAdmins(t *testing.T) {
	g := Group{Participants: []GroupParticipant{
		{ID: "1", IsSuperAdmin: true},
		{ID: "2"},
		{ID: "3", IsAdmin: true},
	}}
	admins := g.Admins()
	if len(admins) != 2 {
		t.Fatalf("want 2 admins, got %d", len(admins))
	}
	if admins[0].ID != "1" || admins[1].ID != "3" {
		t.Errorf("admins should preserve roster order, got %q then %q", admins[0].ID, admins[1].ID)
	}
}
