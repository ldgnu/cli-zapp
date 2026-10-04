package text

import "testing"

func TestWidthCountsCellsNotBytes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"ascii", "hello", 5},
		{"empty", "", 0},
		// The whole point of the package: byte length lies.
		{"latin1 accent", "ñ", 1},
		{"two accents", "ñó", 2},
		// Wide characters occupy two cells.
		{"cjk", "日本語", 6},
		{"hangul", "한글", 4},
		// Emoji are wide and may be multi-codepoint.
		{"emoji", "\U0001F600", 2},
		{"emoji with modifier", "\U0001F44D\U0001F3FB", 2},
		// A combining mark adds no width of its own.
		{"combining acute", "é", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Width(tc.in); got != tc.want {
				t.Errorf("Width(%q) = %d, want %d (len=%d)", tc.in, got, tc.want, len(tc.in))
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		w    int
		want string
	}{
		{"shorter than width", "hi", 10, "hi"},
		{"exactly width", "hello", 5, "hello"},
		{"one over", "hellos", 5, "hell…"},
		{"five over four", "hello", 4, "hel…"},
		{"much longer", "hello world", 8, "hello w…"},
		{"zero width", "hello", 0, ""},
		{"negative width", "hello", -1, ""},
		{"width one", "hello", 1, "…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Truncate(tc.in, tc.w)
			if got != tc.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
			}
			if w := Width(got); w > tc.w && tc.w > 0 {
				t.Errorf("result %q is %d cells, exceeding the %d requested", got, w, tc.w)
			}
		})
	}
}

func TestTruncateRespectsWideCharacters(t *testing.T) {
	// Truncating to 5 cells must not emit 6 cells of CJK.
	got := Truncate("日本語日本語", 5)
	if w := Width(got); w > 5 {
		t.Errorf("got %q at %d cells, want at most 5", got, w)
	}
}

func TestTruncateNoEllipsis(t *testing.T) {
	if got := TruncateNoEllipsis("hello world", 5); got != "hello" {
		t.Errorf("want %q, got %q", "hello", got)
	}
	if got := TruncateNoEllipsis("hi", 5); got != "hi" {
		t.Errorf("want unchanged, got %q", got)
	}
}

func TestTruncateNoEllipsisSplitsGraphemeClusters(t *testing.T) {
	// A combining mark must not be separated from its base character, or the
	// output renders as a replacement box.
	got := TruncateNoEllipsis("ééé", 2)
	if got != "éé" {
		t.Errorf("want %q, got %q", "éé", got)
	}
}

func TestPadRightAndLeft(t *testing.T) {
	if got := PadRight("ab", 5); got != "ab   " {
		t.Errorf("PadRight = %q", got)
	}
	if got := PadLeft("ab", 5); got != "   ab" {
		t.Errorf("PadLeft = %q", got)
	}
	// Never truncates.
	if got := PadRight("abcdef", 3); got != "abcdef" {
		t.Errorf("PadRight should not truncate, got %q", got)
	}
	// Counts cells, not bytes.
	if w := Width(PadRight("ñ", 4)); w != 4 {
		t.Errorf("padded width = %d, want 4", w)
	}
}

func TestWrap(t *testing.T) {
	tests := []struct {
		name string
		in   string
		w    int
		want []string
	}{
		{"fits", "hi there", 20, []string{"hi there"}},
		{"wraps on word", "hi there friend", 8, []string{"hi there", "friend"}},
		{"preserves paragraph break", "a\n\nb", 10, []string{"a", "", "b"}},
		{"empty line", "", 10, []string{""}},
		{"zero width", "hello", 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Wrap(tc.in, tc.w)
			if len(got) != len(tc.want) {
				t.Fatalf("Wrap(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("line %d: want %q, got %q", i, tc.want[i], got[i])
				}
			}
		})
	}
}

func TestWrapNeverExceedsWidth(t *testing.T) {
	// The invariant that keeps bubbles from overflowing the layout.
	//
	// Widths start at 2 because a single cell cannot hold a double-width
	// character: for narrow panes the correct behaviour is one character per
	// line, which necessarily exceeds the requested width. That case is
	// covered by TestWrapNarrowerThanDoubleWidth instead.
	for _, width := range []int{2, 5, 10, 20, 40} {
		for _, in := range []string{
			"the quick brown fox jumps over the lazy dog",
			"supercalifragilisticexpialidocious",
			"日本語日本語日本語日本語",
			"mixed 日本語 and english text that goes on",
		} {
			for _, line := range Wrap(in, width) {
				if w := Width(line); w > width {
					t.Errorf("Wrap(%q, %d) produced %q at %d cells", in, width, line, w)
				}
			}
		}
	}
}

func TestWrapNarrowerThanDoubleWidth(t *testing.T) {
	// A pane narrower than a wide character must still terminate and must not
	// lose text, even though the lines cannot honour the requested width.
	got := Wrap("日本語", 1)
	if len(got) != 3 {
		t.Fatalf("expected one character per line, got %q", got)
	}
	joined := ""
	for _, l := range got {
		joined += l
	}
	if joined != "日本語" {
		t.Errorf("characters were lost: got %q", joined)
	}
}

func TestWrapBreaksOverlongWord(t *testing.T) {
	// A URL is longer than any pane; it must be broken, not left overlong.
	got := Wrap("https://example.com/a/very/long/path", 10)
	for _, line := range got {
		if Width(line) > 10 {
			t.Errorf("line %q exceeds 10 cells", line)
		}
	}
	if len(got) < 2 {
		t.Errorf("an overlong word should be split, got %q", got)
	}
}

func TestWrapStyledMeasuresVisibleWidth(t *testing.T) {
	// A styled line must wrap on its visible width, not on its byte length.
	styled := "\x1b[31mred text that is quite long indeed\x1b[0m"
	got := WrapStyled(styled, 10)
	if len(got) < 2 {
		t.Fatalf("expected wrapping, got %q", got)
	}
	for _, line := range got {
		if w := VisibleWidth(line); w > 10 {
			t.Errorf("line %q measures %d visible cells, want at most 10", line, w)
		}
	}
}

func TestWrapStyledPreservesEscapes(t *testing.T) {
	styled := "\x1b[32mgreen words here\x1b[0m"
	got := WrapStyled(styled, 8)
	if len(got) == 0 {
		t.Fatal("expected output")
	}
	if StripANSI(got[0]) == "" {
		t.Errorf("escape sequences were lost: %q", got[0])
	}
}

func TestVisibleWidth(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"plain", 5},
		{"", 0},
		{"\x1b[1mbold\x1b[0m", 4},
		{"\x1b[38;2;255;0;0mcolour\x1b[0m", 6},
		{"a\x1b[1mb\x1b[0mc", 3},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := VisibleWidth(tc.in); got != tc.want {
				t.Errorf("VisibleWidth(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"plain", "plain"},
		{"\x1b[1mbold\x1b[0m", "bold"},
		{"\x1b[38;5;196mred\x1b[0m", "red"},
		{"mixed \x1b[1mtext\x1b[0m here", "mixed text here"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := StripANSI(tc.in); got != tc.want {
			t.Errorf("StripANSI(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeRemovesEscapeSequences(t *testing.T) {
	// The security-relevant case: a message body must not be able to repaint
	// the terminal. A peer who sends raw escape codes must not be able to move
	// the cursor, change colours, or clear the screen.
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text", "hello", "hello"},
		{"keeps newlines", "a\nb", "a\nb"},
		{"keeps tabs", "a\tb", "a\tb"},
		{"keeps accents", "ñó", "ñó"},
		{"keeps emoji", "\U0001F600", "\U0001F600"},
		{"strips colour", "a\x1b[31mred\x1b[0mb", "aredb"},
		{"strips cursor move", "a\x1b[2Jb", "ab"},
		{"strips bold", "\x1b[1mbold", "bold"},
		{"strips bell", "a\x07b", "ab"},
		{"strips backspace", "a\x08b", "ab"},
		{"strips delete", "a\x7fb", "ab"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Sanitize(tc.in)
			if got != tc.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeIsIdempotent(t *testing.T) {
	// Running it twice must not mangle already-clean text, since a message may
	// pass through more than one rendering path.
	in := "hello \x1b[31mworld\x1b[0m\nsecond line\twith tab"
	once := Sanitize(in)
	if twice := Sanitize(once); twice != once {
		t.Errorf("not idempotent: %q then %q", once, twice)
	}
}

func TestSanitizeRemovesNoVisibleText(t *testing.T) {
	// Sanitising must never delete ordinary printable characters, including
	// brackets, which are common in message text.
	in := "array[0] = {a, b} (c) [d] 50% #tag @mention"
	if got := Sanitize(in); got != in {
		t.Errorf("Sanitize altered printable text:\n got %q\nwant %q", got, in)
	}
}

func TestSanitizeOutputIsAlwaysSafeToRender(t *testing.T) {
	// Property check: nothing that survives Sanitize can still change terminal
	// state.
	inputs := []string{
		"\x1b]0;title\x07",
		"\x1b[?1049h",
		"\x1bP1$r\x1b\\",
		"\x9b31m",
		"\x1b[1;2;3;4;5m",
	}
	for _, in := range inputs {
		out := Sanitize(in)
		for _, r := range out {
			if r == 0x1b || r == 0x9b || r == 0x07 {
				t.Errorf("Sanitize(%q) = %q still contains %q", in, out, r)
			}
		}
		if StripANSI(out) != out {
			t.Errorf("Sanitize(%q) = %q still contains escapes", in, out)
		}
	}
}

func TestWidthOfTruncatedNeverExceedsTarget(t *testing.T) {
	// A property test over the range of widths and inputs the UI can produce.
	inputs := []string{"", "a", "hello world", "ñó", "日本語", "\U0001F600\U0001F601", "a b c d e f g"}
	for _, in := range inputs {
		for w := 0; w <= 20; w++ {
			got := Truncate(in, w)
			if w == 0 {
				if got != "" {
					t.Errorf("Truncate(%q, 0) = %q, want empty", in, got)
				}
				continue
			}
			if n := Width(got); n > w {
				t.Errorf("Truncate(%q, %d) = %q at %d cells", in, w, got, n)
			}
		}
	}
}
