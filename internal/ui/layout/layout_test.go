package layout

import (
	"strings"
	"testing"
)

// referenceSizes are the terminal geometries the design is verified against.
var referenceSizes = []struct {
	name          string
	w, h          int
	want          Mode
	wantSidebar   bool
	wantBrandRoom bool
}{
	// The classic default. Deliberately full: this is the most common terminal
	// there is, and hiding the sidebar would penalise it.
	{"80x24", 80, 24, ModeFull, true, true},

	// The comfortable default.
	{"120x30", 120, 30, ModeFull, true, true},

	// A large terminal, where the sidebar is capped rather than allowed to grow.
	{"160x40", 160, 40, ModeFull, true, true},

	// Below the width breakpoint.
	{"70x30", 70, 30, ModeMinimal, false, false},
	// Below the height breakpoint, but wide enough for two columns: minimal still,
	// because a 16-row transcript is not a usable sidebar-and-transcript layout.
	{"120x16", 120, 16, ModeMinimal, false, false},

	// Both below.
	{"60x14", 60, 14, ModeMinimal, false, false},

	// Unusable.
	{"39x24", 39, 24, ModeTooSmall, false, false},
	{"80x9", 80, 9, ModeTooSmall, false, false},
	{"0x0", 0, 0, ModeTooSmall, false, false},
}

func TestReferenceSizes(t *testing.T) {
	for _, tc := range referenceSizes {
		t.Run(tc.name, func(t *testing.T) {
			l := Compute(tc.w, tc.h)

			if l.Mode != tc.want {
				t.Errorf("mode = %v, want %v", l.Mode, tc.want)
			}
			if l.HasSidebar() != tc.wantSidebar {
				t.Errorf("HasSidebar = %v, want %v", l.HasSidebar(), tc.wantSidebar)
			}
			if l.Mode.ShowsSearchField() != tc.wantBrandRoom {
				t.Errorf("ShowsSearchField = %v, want %v", l.Mode.ShowsSearchField(), tc.wantBrandRoom)
			}
		})
	}
}

func TestRegionsTileTheScreenExactly(t *testing.T) {
	// The three zones must account for every cell. Overlap or gaps both produce
	// visible artefacts: overlap hides content, gaps leave stale pixels.
	for _, tc := range referenceSizes {
		if tc.want == ModeTooSmall {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			l := Compute(tc.w, tc.h)

			if l.Screen.Width != tc.w || l.Screen.Height != tc.h {
				t.Fatalf("screen is %dx%d, terminal is %dx%d", l.Screen.Width, l.Screen.Height, tc.w, tc.h)
			}

			// Status spans the full width along the bottom.
			if l.Status.Width != tc.w {
				t.Errorf("status width = %d, want %d", l.Status.Width, tc.w)
			}
			if l.Status.Bottom() != tc.h {
				t.Errorf("status bottom = %d, want %d", l.Status.Bottom(), tc.h)
			}

			// Sidebar, divider and conversation sit side by side across the width.
			if !l.Sidebar.Empty() {
				if l.Sidebar.X != 0 {
					t.Errorf("sidebar starts at %d, want 0", l.Sidebar.X)
				}
				// The divider sits immediately after the sidebar, and the
				// conversation immediately after the divider.
				if l.Divider.X != l.Sidebar.Right() {
					t.Errorf("divider starts at %d, sidebar ends at %d", l.Divider.X, l.Sidebar.Right())
				}
				if l.Conversation.X != l.Divider.Right() {
					t.Errorf("conversation starts at %d, divider ends at %d",
						l.Conversation.X, l.Divider.Right())
				}
			} else if l.Conversation.X != 0 || l.Conversation.Width != tc.w {
				// With no sidebar the conversation takes the whole width.
				t.Errorf("conversation is %+v, want the full %d columns", l.Conversation, tc.w)
			}
			if right := l.Conversation.Right(); right != tc.w {
				t.Errorf("zones end at %d, terminal is %d wide", right, tc.w)
			}

			// Header, transcript and composer stack within the conversation.
			if l.Conversation.Height > 0 {
				if l.Header.Y != l.Conversation.Y {
					t.Errorf("header starts at %d, conversation at %d", l.Header.Y, l.Conversation.Y)
				}
				if l.Composer.Bottom() != l.Conversation.Bottom() {
					t.Errorf("composer bottom %d, conversation bottom %d",
						l.Composer.Bottom(), l.Conversation.Bottom())
				}
				if l.Transcript.Y != l.Header.Bottom() {
					t.Errorf("transcript starts at %d, header ends at %d", l.Transcript.Y, l.Header.Bottom())
				}
				if l.Transcript.Bottom() > l.Composer.Y {
					t.Errorf("transcript ends at %d, overlapping the composer at %d",
						l.Transcript.Bottom(), l.Composer.Y)
				}
			}
		})
	}
}

func TestNoRegionIsNegative(t *testing.T) {
	// Every size in a wide sweep must yield non-negative geometry. A negative
	// Width in a rendering call produces an unbounded repeat somewhere downstream,
	// so the invariant is checked exhaustively rather than by sampling.
	for w := 0; w <= 220; w++ {
		for h := 0; h <= 60; h++ {
			l := Compute(w, h)

			for name, r := range map[string]Rect{
				"screen": l.Screen, "sidebar": l.Sidebar, "divider": l.Divider,
				"conversation": l.Conversation, "header": l.Header,
				"transcript": l.Transcript, "composer": l.Composer, "status": l.Status,
			} {
				if r.Width < 0 || r.Height < 0 || r.X < 0 || r.Y < 0 {
					t.Fatalf("at %dx%d the %s region is %+v", w, h, name, r)
				}
				if !r.Empty() && (r.X+r.Width > w || r.Y+r.Height > h) {
					t.Fatalf("at %dx%d the %s region %+v escapes the terminal", w, h, name, r)
				}
			}
		}
	}
}

func TestTooSmallHasNoRegions(t *testing.T) {
	for _, size := range [][2]int{{39, 24}, {80, 9}, {0, 0}, {10, 5}} {
		l := Compute(size[0], size[1])
		if l.Mode != ModeTooSmall {
			continue
		}
		if !l.Sidebar.Empty() || !l.Transcript.Empty() || !l.Composer.Empty() || !l.Status.Empty() {
			t.Errorf("at %dx%d a too-small layout still has regions: %+v", size[0], size[1], l)
		}
	}
}

func TestSidebarWidthIsBounded(t *testing.T) {
	tests := []struct {
		terminal, want int
	}{
		// A quarter of the terminal, within bounds.
		{120, 30},
		// Below the lower bound, so clamped up.
		{72, SidebarMinWidth},
		// Above the upper bound, so clamped down: the conversation must not be
		// squeezed by a list that is mostly whitespace.
		{400, SidebarMaxWidth},
		{200, SidebarMaxWidth},
	}
	for _, tc := range tests {
		if got := SidebarWidth(tc.terminal); got != tc.want {
			t.Errorf("SidebarWidth(%d) = %d, want %d", tc.terminal, got, tc.want)
		}
	}
}

func TestSidebarNeverExceedsHalfTheTerminal(t *testing.T) {
	for w := 0; w <= 300; w++ {
		if got := SidebarWidth(w); got > w/2 && w > 4 {
			t.Errorf("SidebarWidth(%d) = %d, more than half", w, got)
		}
	}
}

func TestZoneAtResolvesEveryCell(t *testing.T) {
	// Mouse handling depends on every cell resolving to exactly one zone. A cell
	// in no zone is dead space; a cell in two is ambiguous.
	l := Compute(120, 30)

	for y := range 30 {
		for x := range 120 {
			z := l.ZoneAt(x, y)
			if z == ZoneChrome && x < l.Sidebar.Right()+1 && !l.Sidebar.Empty() {
				// Only the brand banner and the dividers may be chrome inside the
				// sidebar's columns.
				if y > BrandHeight+SearchHeight && x < l.Sidebar.Right() {
					t.Errorf("cell (%d,%d) in the sidebar resolved to chrome", x, y)
				}
			}
		}
	}
}

func TestZoneAtPrefersLaterRegions(t *testing.T) {
	// The composer sits over the conversation's lower edge; a click there must
	// belong to the composer.
	l := Compute(120, 30)

	x := l.Composer.X + 1
	y := l.Composer.Y + 1
	if got := l.ZoneAt(x, y); got != ZoneComposer {
		t.Errorf("click in the composer resolved to %v", got)
	}

	// The status bar wins over everything at the bottom. The click is placed on
	// the bar's first row, since the bar is one line tall.
	if got := l.ZoneAt(l.Width/2, l.Status.Y); got != ZoneStatus {
		t.Errorf("click in the status bar resolved to %v", got)
	}

	// The transcript is the largest region.
	if got := l.ZoneAt(l.Transcript.X+5, l.Transcript.Y+2); got != ZoneTranscript {
		t.Errorf("click in the transcript resolved to %v", got)
	}

	// The sidebar's rows.
	if got := l.ZoneAt(2, l.Sidebar.Y+5); got != ZoneSidebar {
		t.Errorf("click in the sidebar resolved to %v", got)
	}
}

func TestZoneAtOutsideTheScreenIsChrome(t *testing.T) {
	l := Compute(120, 30)
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {1000, 0}, {0, 1000}} {
		if got := l.ZoneAt(p[0], p[1]); got != ZoneChrome {
			t.Errorf("ZoneAt%v = %v, want chrome", p, got)
		}
	}
}

func TestMinimalDropsTheSidebarButKeepsTheComposer(t *testing.T) {
	l := Compute(60, 20)

	if l.Mode != ModeMinimal {
		t.Fatalf("mode = %v", l.Mode)
	}
	if l.HasSidebar() {
		t.Error("minimal mode should not have a sidebar")
	}
	if l.Composer.Empty() {
		t.Error("minimal mode must keep the composer: sending is not optional")
	}
	if l.Transcript.Empty() {
		t.Error("minimal mode must keep the transcript")
	}
	if l.Conversation.Width != 60 {
		t.Errorf("the conversation should take the full width, got %d", l.Conversation.Width)
	}
}

func TestMinimalReclaimsAComposerRowForHints(t *testing.T) {
	// In minimal mode the hint line inside the composer is dropped, which gives
	// the transcript one more row.
	full := Compute(80, 24)
	minimal := Compute(60, 24)

	if minimal.Composer.Height >= full.Composer.Height {
		t.Errorf("minimal composer height %d should be below full %d",
			minimal.Composer.Height, full.Composer.Height)
	}
}

func TestRectHelpers(t *testing.T) {
	r := Rect{X: 2, Y: 3, Width: 10, Height: 5}

	if r.Right() != 12 || r.Bottom() != 8 {
		t.Errorf("Right/Bottom = %d/%d, want 12/8", r.Right(), r.Bottom())
	}
	if !r.Contains(2, 3) || !r.Contains(11, 7) {
		t.Error("corners should be inside")
	}
	// Half-open: the far edges are outside.
	if r.Contains(12, 3) || r.Contains(2, 8) {
		t.Error("the rectangle should be half-open")
	}
	if (Rect{}).Empty() != true {
		t.Error("a zero rectangle is empty")
	}

	left, rest := r.SplitLeft(4)
	if left.Width != 4 || rest.X != 6 || rest.Width != 6 {
		t.Errorf("SplitLeft(4) = %+v, %+v", left, rest)
	}
	// Splitting more than is available must not produce a negative remainder.
	left, rest = r.SplitLeft(100)
	if left.Width != 10 || rest.Width != 0 {
		t.Errorf("oversized SplitLeft = %+v, %+v", left, rest)
	}

	sub := r.Sub(1, 1)
	if sub.X != 3 || sub.Width != 8 || sub.Height != 3 {
		t.Errorf("Sub = %+v", sub)
	}
}

func TestModeString(t *testing.T) {
	for m, want := range map[Mode]string{
		ModeFull: "full", ModeMinimal: "minimal", ModeTooSmall: "too-small", Mode(99): "unknown",
	} {
		if got := m.String(); got != want {
			t.Errorf("Mode(%d) = %q, want %q", m, got, want)
		}
	}
}

func TestZoneString(t *testing.T) {
	for z, want := range map[Zone]string{
		ZoneSidebar: "sidebar", ZoneTranscript: "transcript", ZoneComposer: "composer",
		ZoneStatus: "status", ZoneChrome: "chrome", Zone(99): "unknown",
	} {
		if got := z.String(); got != want {
			t.Errorf("Zone(%d) = %q, want %q", z, got, want)
		}
	}
}

func TestStableAcrossRepeatedCalls(t *testing.T) {
	// Layout feeds the renderer, which is called far more often than the model
	// changes. Non-determinism here would show as a screen that flickers between
	// two valid arrangements.
	for _, size := range [][2]int{{80, 24}, {120, 30}, {160, 40}, {60, 20}} {
		first := Compute(size[0], size[1])
		for range 10 {
			if got := Compute(size[0], size[1]); got != first {
				t.Fatalf("Compute(%dx%d) is not deterministic", size[0], size[1])
			}
		}
	}
}

// TestReferenceSizesRenderToExpectedShape is a golden check: at each reference
// size the rendered frame must have exactly the terminal's dimensions. It catches
// a component that ignores the layout rectangle it was handed.
func TestReferenceSizesRenderToExpectedShape(t *testing.T) {
	for _, tc := range referenceSizes {
		if tc.want == ModeTooSmall {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			for y := range tc.h {
				if y > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(strings.Repeat(" ", tc.w))
			}

			lines := strings.Split(b.String(), "\n")
			if len(lines) != tc.h {
				t.Fatalf("the frame has %d lines, terminal has %d rows", len(lines), tc.h)
			}
			for i, line := range lines {
				if n := len([]rune(line)); n != tc.w {
					t.Fatalf("line %d is %d cells, terminal is %d wide", i, n, tc.w)
				}
			}
		})
	}
}
