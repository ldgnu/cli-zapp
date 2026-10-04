// Package layout computes the geometry of wterm's interface.
//
// # Why this is a separate package
//
// A layout that is computed inside the render function cannot be tested without
// rendering, and a layout that is wrong is visible only as a broken screen. Here
// the geometry is a pure function of (width, height) returning rectangles, so it
// can be asserted exhaustively — every terminal size, every breakpoint — in a unit
// test that runs in microseconds.
//
// # The three zones
//
// The interface is three conceptual areas, always in this order:
//
//	┌──────────────────────────────────────────────────────────┐
//	│  status    context, unread, key hints                     │
//	├──────────────┬───────────────────────────────────────────┤
//	│              │  header   contact name, presence           │
//	│   sidebar    ├───────────────────────────────────────────┤
//	│              │  transcript, independently scrollable       │
//	│  brand       ├───────────────────────────────────────────┤
//	│  search      │  composer, multiline                       │
//	│  rows        │                                           │
//	├──────────────┴───────────────────────────────────────────┤
//	│  status/action bar                                         │
//	└──────────────────────────────────────────────────────────┘
//
// The sidebar is dropped rather than squeezed when the terminal is too narrow for
// it to carry information. A four-column sidebar showing truncated names and
// clipped timestamps is worse than no sidebar: the user reads neither, and the
// conversation — the thing they came for — gets less room.
package layout

// Mode is a layout tier. The tiers are ordered from most to least capable.
type Mode int

// Recognised layout modes.
const (
	// ModeTooSmall cannot render anything usable. The application explains this
	// rather than drawing a scrambled interface.
	ModeTooSmall Mode = iota
	// ModeMinimal is a single column: conversation only, with a compact header
	// and no key hints.
	ModeMinimal
	// ModeFull is the two-column layout with the sidebar, the brand header, the
	// search field and the full status bar.
	ModeFull
)

// String implements fmt.Stringer.
func (m Mode) String() string {
	switch m {
	case ModeFull:
		return "full"
	case ModeMinimal:
		return "minimal"
	case ModeTooSmall:
		return "too-small"
	default:
		return "unknown"
	}
}

// HasSidebar reports whether the mode draws the sidebar.
func (m Mode) HasSidebar() bool { return m == ModeFull }

// ShowsKeyHints reports whether the mode shows the key-hint area.
//
// Hints are the first thing to go on a small terminal: the interface stays
// operable without them, and the user's own memory of the bindings is not worth
// the rows they cost.
func (m Mode) ShowsKeyHints() bool { return m == ModeFull }

// ShowsSearchField reports whether the mode draws the persistent search field.
//
// In minimal mode search is still reachable, through the command palette, rather
// than through a field that would consume a row the conversation needs.
func (m Mode) ShowsSearchField() bool { return m == ModeFull }

// Breakpoints. These are the numbers the whole design turns on, so they are named
// and documented rather than inlined at their use sites.
const (
	// MinWidth and MinHeight are the smallest terminal that can show a
	// conversation with a composer.
	MinWidth  = 40
	MinHeight = 10

	// FullWidth is the width at or above which the sidebar earns its column.
	// Below it, the sidebar is dropped.
	//
	// 80 columns is deliberately inside the full layout: it is the classic default
	// terminal size, and a 26-column sidebar beside a 53-column conversation is
	// still readable. Hiding the sidebar at 80 would penalise the most common
	// terminal there is.
	FullWidth = 72

	// FullHeight is the height at or above which the full layout fits without
	// squeezing the transcript. Below it the brand header and the search field are
	// the first to go.
	FullHeight = 18

	// SidebarMinWidth and SidebarMaxWidth bound the chat list column.
	SidebarMinWidth = 26
	SidebarMaxWidth = 34

	// HeaderHeight is the contact header band.
	HeaderHeight = 1
	// HeaderRuleHeight is the rule between the header and the transcript.
	//
	// One row, spent on separating *who* you are talking to from *what* was said.
	// Without it the contact's name sits directly on top of the first message, and
	// the two read as one block — which is exactly the ambiguity the header exists to
	// remove. On a narrow terminal it is the first thing to go, after the header itself.
	HeaderRuleHeight = 1
	// SidebarFooterHeight is the action row at the bottom of the sidebar.
	//
	// It is not decoration. The status bar along the bottom says what *is*; this says
	// what you can *do* where the cursor is, which is a different question and needs a
	// different place. Showing "enter abrir" beside the list, rather than at the far end
	// of a bar shared with the connection state, puts the answer next to the thing it
	// applies to.
	SidebarFooterHeight = 1
	// ComposerHeight is the multiline input area in the full layout: a rule, the draft,
	// the contextual hint and a rule.
	//
	// Four rows rather than one is the honest cost of telling the user that
	// alt+enter inserts a newline. Without the hint, enter sending is something they
	// have to discover by trying to send a two-line message and watching the first line
	// go out on its own.
	ComposerHeight = 4
	// StatusHeight is the context line and the key-hint bar.
	StatusHeight = 1
	// BrandHeight is the WTERM banner above the sidebar's search field.
	BrandHeight = 1
	// SearchHeight is the search field in the sidebar.
	SearchHeight = 1
)

// Rect is a region of the terminal, in cells.
//
// The rectangle is half-open in the usual Go sense: Width and Height are counts,
// not exclusive coordinates, and X+Width is the first cell past the right edge.
type Rect struct {
	X, Y          int
	Width, Height int
}

// Empty reports whether the rectangle has no area.
func (r Rect) Empty() bool { return r.Width <= 0 || r.Height <= 0 }

// Right returns the first column past the rectangle's right edge.
func (r Rect) Right() int { return r.X + r.Width }

// Bottom returns the first row past the rectangle's bottom edge.
func (r Rect) Bottom() int { return r.Y + r.Height }

// Contains reports whether a terminal cell falls inside the rectangle.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.Right() && y >= r.Y && y < r.Bottom()
}

// SplitLeft carves width columns off the left of the rectangle.
func (r Rect) SplitLeft(width int) (left, rest Rect) {
	if width < 0 {
		width = 0
	}
	if width > r.Width {
		width = r.Width
	}
	return Rect{X: r.X, Y: r.Y, Width: width, Height: r.Height},
		Rect{X: r.X + width, Y: r.Y, Width: r.Width - width, Height: r.Height}
}

// Sub returns the rectangle inset by the given amounts.
func (r Rect) Sub(dx, dy int) Rect {
	return Rect{
		X:      r.X + dx,
		Y:      r.Y + dy,
		Width:  max(r.Width-dx*2, 0),
		Height: max(r.Height-dy*2, 0),
	}
}

// Layout is the computed geometry of one frame.
type Layout struct {
	Mode Mode
	// Size is the terminal size this layout was computed for.
	Width, Height int

	// Screen is the whole terminal.
	Screen Rect

	// Sidebar is the chat list column. Empty in minimal mode.
	Sidebar Rect
	// Divider is the one column between the sidebar and the conversation.
	Divider Rect
	// Conversation is everything right of the divider, or the whole screen.
	Conversation Rect

	// Header is the contact name band inside Conversation.
	Header Rect
	// HeaderRule separates the header from the transcript. Empty when there is no room
	// for it.
	HeaderRule Rect
	// Transcript is the scrollable message area inside Conversation.
	Transcript Rect
	// Composer is the multiline input inside Conversation.
	Composer Rect

	// Status is the key-hint and context bar across the bottom.
	Status Rect
}

// HasSidebar reports whether this layout draws the sidebar.
func (l Layout) HasSidebar() bool { return !l.Sidebar.Empty() }

// Compute returns the layout for a terminal of the given size.
//
// It is total: every size yields a valid layout, and sizes too small to render
// produce ModeTooSmall rather than negative dimensions. Callers never have to
// defend against it.
func Compute(width, height int) Layout {
	l := Layout{Width: width, Height: height, Screen: Rect{Width: width, Height: height}}

	mode := ModeFor(width, height)
	l.Mode = mode

	if mode == ModeTooSmall {
		return l
	}

	l.Status = Rect{X: 0, Y: height - StatusHeight, Width: width, Height: StatusHeight}

	// The conversation column, then the sidebar carved out of what is left.
	conv := Rect{
		X:      0,
		Y:      0,
		Width:  width,
		Height: height - StatusHeight,
	}

	if mode == ModeFull {
		side := SidebarWidth(width)
		l.Sidebar = Rect{X: 0, Y: 0, Width: side, Height: conv.Height}
		// The divider occupies the single column immediately after the sidebar.
		l.Divider = Rect{X: side, Y: 0, Width: 1, Height: conv.Height}
		conv = Rect{X: side + 1, Width: width - side - 1, Height: conv.Height}
	}

	l.Conversation = conv

	// Inside the conversation: header, transcript, composer.
	composerH := ComposerHeight
	if !mode.ShowsKeyHints() {
		// Minimal mode keeps the composer but drops the hint line inside it,
		// reclaiming a row for the transcript.
		composerH = ComposerHeight - 1
	}

	// The rule goes before the transcript, and both go before the header does.
	head, rule := HeaderHeight, HeaderRuleHeight
	body := conv.Height - head - rule - composerH

	switch {
	case body < 1 && rule > 0:
		rule, body = 0, max(conv.Height-head-composerH, 0)
	case body < 1 && head > 0:
		// Extremely short terminal: sacrifice the header rather than leaving no room to
		// read. The conversation is still identifiable from the sidebar.
		head, body = 0, max(conv.Height-rule-composerH, 0)
	}

	l.Header = Rect{X: conv.X, Y: conv.Y, Width: conv.Width, Height: head}
	l.HeaderRule = Rect{X: conv.X, Y: conv.Y + head, Width: conv.Width, Height: rule}

	transcriptY := conv.Y + head + rule
	l.Transcript = Rect{X: conv.X, Y: transcriptY, Width: conv.Width, Height: max(body, 0)}
	l.Composer = Rect{
		X:      conv.X,
		Y:      transcriptY + max(body, 0),
		Width:  conv.Width,
		Height: min(composerH, max(conv.Height-head-rule, 0)),
	}

	return l
}

// ModeFor classifies a terminal size into a layout mode.
func ModeFor(width, height int) Mode {
	switch {
	case width < MinWidth || height < MinHeight:
		return ModeTooSmall
	case width < FullWidth || height < FullHeight:
		return ModeMinimal
	default:
		return ModeFull
	}
}

// SidebarWidth returns the chat list width for a terminal width.
//
// A quarter of the terminal, bounded. The lower bound keeps names and timestamps
// readable; the upper bound stops the list from eating the conversation on a very
// wide terminal, where a 40-column sidebar is mostly whitespace.
func SidebarWidth(width int) int {
	w := width / 4
	if w < SidebarMinWidth {
		w = SidebarMinWidth
	}
	if w > SidebarMaxWidth {
		w = SidebarMaxWidth
	}
	// Never take more than half the terminal, whatever the bounds say.
	if half := width / 2; w > half {
		w = half
	}
	return max(w, 1)
}

// Zone identifies a conceptual area, for mouse hit-testing and for routing.
type Zone int

// Recognised zones.
const (
	ZoneSidebar Zone = iota
	ZoneTranscript
	ZoneComposer
	ZoneStatus
	ZoneChrome // the brand header, dividers and other non-interactive surface
)

// String implements fmt.Stringer.
func (z Zone) String() string {
	switch z {
	case ZoneSidebar:
		return "sidebar"
	case ZoneTranscript:
		return "transcript"
	case ZoneComposer:
		return "composer"
	case ZoneStatus:
		return "status"
	case ZoneChrome:
		return "chrome"
	default:
		return "unknown"
	}
}

// ZoneAt reports which zone a terminal cell belongs to.
//
// Regions are tested in painting order, so an overlapping later region wins: the
// composer sits over the conversation's lower edge, and a click there belongs to
// the composer.
func (l Layout) ZoneAt(x, y int) Zone {
	// Off-screen clicks belong to nothing; they arrive from terminal emulators
	// that report a stale position after a resize.
	if x < 0 || y < 0 || x >= l.Width || y >= l.Height {
		return ZoneChrome
	}

	switch {
	case l.Status.Contains(x, y):
		return ZoneStatus
	case l.Composer.Contains(x, y):
		return ZoneComposer
	case l.Transcript.Contains(x, y):
		return ZoneTranscript
	case !l.Sidebar.Empty() && l.Sidebar.Contains(x, y):
		return ZoneSidebar
	default:
		return ZoneChrome
	}
}
