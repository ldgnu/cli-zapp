// Package theme holds cli-zapp's visual vocabulary: colours, spacing and glyphs.
//
// # Why this is a package and not a file of constants
//
// Colour is the difference between a TUI that feels designed and one that looks
// like debug output. Centralising it means a palette changes in one place, and
// — because every colour flows through here — that a dark and a light variant
// differ only in the values below.
//
// # No global mutable state
//
// A [Theme] is a plain value. The UI holds one and threads it through rendering
// rather than reading a package-level singleton.
//
// The earlier alternative, a mutex-guarded global, was discarded because it
// makes styles untestable in isolation and forces every style lookup to take a
// lock on the render path. A value costs one struct copy and removes the
// locking question entirely.
//
// # Colour strategy
//
// Colours are [color.Color] rather than ANSI indices so lipgloss can downsample
// them to whatever the terminal actually supports. Hardcoded ANSI numbers
// produce unreadable output on limited palettes; letting lipgloss degrade is
// what keeps the UI legible everywhere.
//
// WhatsApp's palette is the reference: green accent, dark teal surfaces. The
// exact values are tuned for legibility on light and dark terminal themes
// rather than matching the web app pixel for pixel, since the terminal's
// background is outside cli-zapp's control.
package theme

import "image/color"

// Palette is a complete set of colours.
type Palette struct {
	// Accent is the brand colour, used for focus, links and highlights.
	Accent color.Color
	// AccentMuted is a dimmed accent for secondary emphasis.
	AccentMuted color.Color

	// Foreground is the base text colour.
	//
	// Background is deliberately absent. Overriding the terminal's own
	// background would fight the user's chosen colour scheme, and a TUI that
	// repaints the desktop's wallpaper colours is a nuisance rather than a
	// feature.
	Foreground color.Color

	// SidebarBackground is the chat list surface. WhatsApp Web draws a panel
	// distinct from the conversation area, and the same separation is what
	// makes the two-column layout readable.
	SidebarBackground color.Color
	// SidebarSelected is the focused chat row.
	SidebarSelected color.Color
	// SidebarSelectedFG is the text colour on [Palette.SidebarSelected].
	SidebarSelectedFG color.Color
	// SidebarTitle is the sidebar heading.
	SidebarTitle color.Color

	// HeaderBackground and HeaderForeground style the conversation header.
	HeaderBackground color.Color
	HeaderForeground color.Color
	// Muted is secondary text: timestamps, metadata, placeholders.
	Muted color.Color
	// Faint is the least prominent text, such as scrollbar troughs.
	Faint color.Color

	// OutgoingBG and OutgoingFG style the user's own message bubbles, and are
	// the values that most directly reproduce WhatsApp Web's look.
	OutgoingBG color.Color
	OutgoingFG color.Color
	// IncomingBG and IncomingFG style received message bubbles.
	IncomingBG color.Color
	IncomingFG color.Color

	// Border is the panel divider; BorderFocus highlights the divider next to
	// the focused panel.
	Border      color.Color
	BorderFocus color.Color

	// StatusBarFG styles the bottom status line.
	StatusBarFG color.Color

	// ModalBackground and ModalBorder frame overlays.
	ModalBackground color.Color
	ModalBorder     color.Color

	// Delivered, Read and Failed style message status marks.
	Delivered color.Color
	Read      color.Color
	Failed    color.Color

	// SystemText styles server-generated notices such as "joined the group".
	SystemText color.Color
	// Mention highlights an @-mention in the current chat.
	Mention color.Color

	// SearchMatch highlights search hits.
	SearchMatch color.Color

	// Online and Offline style presence indicators.
	Online  color.Color
	Offline color.Color
}

// hex builds an opaque colour from a 0xRRGGBB literal.
//
// Taking the literal form lets the palette definitions read as the design
// specification they come from rather than as bit arithmetic.
func hex(v uint32) color.RGBA {
	// Each component is masked to a byte. The literals in this file are all
	// 0xRRGGBB, so nothing is actually truncated; the masks say so explicitly
	// rather than leaving a conversion that looks like it could overflow.
	return color.RGBA{
		R: uint8((v >> 16) & 0xFF),
		G: uint8((v >> 8) & 0xFF),
		B: uint8(v & 0xFF),
		A: 0xFF,
	}
}

// DarkPalette returns the palette for dark terminal backgrounds.
//
// This is the default because terminals overwhelmingly default to dark, and a
// light palette on a dark terminal is worse than the reverse.
func DarkPalette() Palette {
	return Palette{
		Accent:      hex(0x25D366), // WhatsApp green
		AccentMuted: hex(0x1F9E55),

		Foreground: hex(0xE9EDEF),

		SidebarBackground: hex(0x111B21),
		SidebarSelected:   hex(0x2A3942),
		SidebarSelectedFG: hex(0xE9EDEF),
		SidebarTitle:      hex(0x8696A0),

		HeaderBackground: hex(0x1F2C34),
		HeaderForeground: hex(0xE9EDEF),

		Muted: hex(0x8696A0),
		Faint: hex(0x54656F),

		OutgoingBG: hex(0x005C4B),
		OutgoingFG: hex(0xE9EDEF),
		IncomingBG: hex(0x202C33),
		IncomingFG: hex(0xE9EDEF),

		Border:      hex(0x2A3942),
		BorderFocus: hex(0x25D366),

		StatusBarFG: hex(0x8696A0),

		ModalBackground: hex(0x1F2C34),
		ModalBorder:     hex(0x25D366),

		Delivered: hex(0x8696A0),
		Read:      hex(0x53BDEB),
		Failed:    hex(0xF15C6D),

		SystemText:  hex(0x8696A0),
		Mention:     hex(0x53BDEB),
		SearchMatch: hex(0x7A5C2E),

		Online:  hex(0x25D366),
		Offline: hex(0x8696A0),
	}
}

// LightPalette returns the palette for light terminal backgrounds.
//
// Selected explicitly via configuration. cli-zapp does not try to infer the
// terminal's background from escape sequences: doing so reliably is not
// possible without querying the terminal, which costs a round trip and is not
// worth it for a scheme that degrades gracefully either way.
func LightPalette() Palette {
	return Palette{
		Accent:      hex(0x128C7E),
		AccentMuted: hex(0x0E6E63),

		Foreground: hex(0x111B21),

		SidebarBackground: hex(0xF0F2F5),
		SidebarSelected:   hex(0xD9DEE4),
		SidebarSelectedFG: hex(0x111B21),
		SidebarTitle:      hex(0x667781),

		HeaderBackground: hex(0xF0F2F5),
		HeaderForeground: hex(0x111B21),

		Muted: hex(0x667781),
		Faint: hex(0x8696A0),

		OutgoingBG: hex(0xD9FAD3),
		OutgoingFG: hex(0x111B21),
		IncomingBG: hex(0xFFFFFF),
		IncomingFG: hex(0x111B21),

		Border:      hex(0xD9DEE4),
		BorderFocus: hex(0x128C7E),

		StatusBarFG: hex(0x667781),

		ModalBackground: hex(0xFFFFFF),
		ModalBorder:     hex(0x128C7E),

		Delivered: hex(0x667781),
		Read:      hex(0x027EB5),
		Failed:    hex(0xD93025),

		SystemText:  hex(0x667781),
		Mention:     hex(0x027EB5),
		SearchMatch: hex(0xFFF3CD),

		Online:  hex(0x128C7E),
		Offline: hex(0x8696A0),
	}
}

// Name identifies a palette.
type Name string

// Recognised palette names.
const (
	NameDark  Name = "dark"
	NameLight Name = "light"
)

// ParseName validates a palette name from configuration.
//
// The second return is false for an unknown name, letting the caller report a
// configuration error rather than silently substituting a default.
func ParseName(s string) (Name, bool) {
	switch Name(s) {
	case NameDark:
		return NameDark, true
	case NameLight:
		return NameLight, true
	default:
		return "", false
	}
}

// PaletteByName returns the named built-in palette.
func PaletteByName(n Name) (Palette, bool) {
	switch n {
	case NameDark:
		return DarkPalette(), true
	case NameLight:
		return LightPalette(), true
	default:
		return Palette{}, false
	}
}

// Metrics are the layout dimensions.
//
// They live beside the palette because spacing and colour together constitute
// the visual design; splitting them would invite the two to drift apart.
type Metrics struct {
	// SidebarWidth is the chat list width in cells.
	SidebarWidth int
	// HeaderHeight is the conversation header height in lines.
	HeaderHeight int
	// StatusHeight is the bottom bar height in lines.
	StatusHeight int
	// ComposerHeight is the message input height in lines.
	ComposerHeight int

	// MinWidth and MinHeight are the sizes below which the layout cannot be
	// rendered meaningfully.
	MinWidth  int
	MinHeight int
	// NarrowWidth is the width at or below which the sidebar collapses and
	// only the conversation is shown.
	NarrowWidth int
}

// DefaultMetrics returns the standard layout dimensions.
//
// The values are chosen for an 80x24 terminal, the smallest size where the
// two-column layout still carries real information: a 30-cell sidebar leaves a
// 50-cell conversation pane, enough for a short message line plus a timestamp.
func DefaultMetrics() Metrics {
	return Metrics{
		SidebarWidth:   30,
		HeaderHeight:   1,
		StatusHeight:   1,
		ComposerHeight: 3,
		MinWidth:       40,
		MinHeight:      10,
		NarrowWidth:    60,
	}
}

// Validate reports whether the metrics can produce a renderable layout.
//
// It is checked when loading configuration, because a bad width would otherwise
// surface as a mysteriously empty screen rather than as a clear error.
func (m Metrics) Validate() bool {
	if m.SidebarWidth <= 0 || m.HeaderHeight <= 0 || m.StatusHeight <= 0 || m.ComposerHeight <= 0 {
		return false
	}
	if m.MinWidth <= 0 || m.MinHeight <= 0 || m.NarrowWidth <= 0 {
		return false
	}
	// The fixed chrome must leave room for the conversation itself.
	return m.MinHeight > m.HeaderHeight+m.StatusHeight+m.ComposerHeight
}

// Glyphs are the decorative characters used across the UI.
//
// They are collected here so that a user on a font without emoji coverage can
// select [ASCIIGlyphs] in configuration, rather than individual widgets failing
// to draw.
type Glyphs struct {
	// Divider draws the horizontal rule under the search box.
	Divider string
	// Placeholder stands in for absent content, such as an empty preview.
	Placeholder string

	Bullet      string // list marker
	Pinned      string
	Muted       string // muted-chat indicator
	Online      string
	Offline     string
	Typing      string
	Reply       string
	Attachment  string
	Scrollbar   string
	ScrollThumb string
	Chevron     string
	Back        string
	Search      string
	Close       string
	Warn        string
	Error       string
	OK          string

	// Command is the prefix of the palette's query line, so a command reads as a
	// command rather than as a search result.
	Command string
	// Marker is the selection cursor in a list.
	//
	// It is deliberately not the same glyph as the palette's own prompt or the
	// sidebar's chevron: two different things on screen at once that look identical
	// is a question the interface should never ask.
	Marker string
	// KeyHint brackets a key in the hint bar.
	KeyHintOpen  string
	KeyHintClose string
	// Ellipsis marks truncated text.
	Ellipsis string
	// VBar is the vertical rule between the sidebar and the conversation.
	//
	// It is not the same character as Divider, which is horizontal: drawing a
	// horizontal rule in the divider column produced a frame that read as a table
	// header rather than as two panes.
	VBar string
	// Prompt is the composer marker.
	Prompt string
	// Pencil marks an edit in progress.
	Pencil string
}

// UnicodeGlyphs is the default glyph set.
func UnicodeGlyphs() Glyphs {
	return Glyphs{
		Divider: "─", Placeholder: "—",
		Bullet: "•", Pinned: "\U0001F4CC", Muted: "\U0001F507",
		Online: "●", Offline: "○",
		Typing:     "✍",
		Reply:      "↩",
		Attachment: "\U0001F4CE",
		Scrollbar:  "│", ScrollThumb: "┃",
		Chevron: "›", Back: "‹",
		Search: "⌕", Close: "✕",
		Warn: "⚠", Error: "✖", OK: "✔",
		Command: "›", Marker: "▸", KeyHintOpen: "[", KeyHintClose: "]", Ellipsis: "…", VBar: "│",
		Prompt: "›", Pencil: "✎",
	}
}

// ASCIIGlyphs is the fallback set for terminals with limited Unicode support.
func ASCIIGlyphs() Glyphs {
	return Glyphs{
		Divider: "-", Placeholder: "-",
		Bullet: "*", Pinned: "*", Muted: "-",
		Online: "o", Offline: "o",
		Typing: "...", Reply: "<", Attachment: "@",
		Scrollbar: "|", ScrollThumb: "#",
		Chevron: ">", Back: "<",
		Search: "/", Close: "x",
		Warn: "!", Error: "x", OK: "v",
		Command: ">", Marker: "*", KeyHintOpen: "[", KeyHintClose: "]", Ellipsis: "~", VBar: "|",
		Prompt: ">", Pencil: "*",
	}
}

// Theme is a palette, metrics and glyph set bundled with the styles derived
// from them.
//
// Styles are precomputed because deriving them per frame would both cost
// allocations and make the palette-to-style relationship impossible to test in
// isolation.
type Theme struct {
	// Name identifies the palette, not the application.
	Name    Name
	Palette Palette
	Metrics Metrics
	Glyphs  Glyphs
	Styles  Styles

	// Brand is the wordmark drawn in the sidebar's banner. It is part of the
	// theme rather than a constant in the sidebar because a terminal client with
	// no identity of its own looks like a demo.
	Brand string
}

// New builds a Theme from its parts and derives the styles.
//
// A palette name of "" selects dark. Invalid metrics fall back to the defaults
// so that a malformed configuration degrades to a working layout rather than an
// unrenderable one; callers that need to know should call [Metrics.Validate]
// themselves.
func New(p Palette, m Metrics, g Glyphs) Theme {
	if !m.Validate() {
		m = DefaultMetrics()
	}
	t := Theme{Name: NameDark, Palette: p, Metrics: m, Glyphs: g, Brand: "CLI-ZAPP"}
	t.Styles = buildStyles(t)
	return t
}

// Dark returns the default dark theme.
func Dark() Theme {
	return New(DarkPalette(), DefaultMetrics(), UnicodeGlyphs())
}

// Light returns the light theme.
func Light() Theme {
	return New(LightPalette(), DefaultMetrics(), UnicodeGlyphs())
}

// WithName returns a copy of the theme using the named built-in palette.
func (t Theme) WithName(n Name) (Theme, bool) {
	p, ok := PaletteByName(n)
	if !ok {
		return t, false
	}
	out := t
	out.Name, out.Palette = n, p
	out.Styles = buildStyles(out)
	return out, true
}

// WithMetrics returns a copy with new metrics.
func (t Theme) WithMetrics(m Metrics) Theme {
	if !m.Validate() {
		return t
	}
	out := t
	out.Metrics = m
	out.Styles = buildStyles(out)
	return out
}

// WithGlyphs returns a copy with a new glyph set.
func (t Theme) WithGlyphs(g Glyphs) Theme {
	out := t
	out.Glyphs = g
	out.Styles = buildStyles(out)
	return out
}

// Fits reports whether a terminal of the given size can render the layout.
func (t Theme) Fits(w, h int) bool {
	return w >= t.Metrics.MinWidth && h >= t.Metrics.MinHeight
}

// SidebarVisible reports whether the sidebar should be drawn at this width.
//
// Below [Metrics.NarrowWidth] the sidebar is dropped entirely rather than
// squeezed, because a three-cell sidebar is noise and the conversation is what
// the user is reading.
func (t Theme) SidebarVisible(w int) bool {
	return w > t.Metrics.NarrowWidth
}

// ContentWidth returns the conversation pane width for a terminal of width w,
// accounting for the sidebar and its divider.
func (t Theme) ContentWidth(w int) int {
	if !t.SidebarVisible(w) {
		return w
	}
	// One column for the vertical divider between the panels.
	return w - t.Metrics.SidebarWidth - 1
}

// ContentHeight returns the conversation height for a terminal of height h.
func (t Theme) ContentHeight(h int) int {
	// Header, composer and status bar are fixed; the divider above the
	// composer is included in the composer's own height budget.
	return h - t.Metrics.HeaderHeight - t.Metrics.ComposerHeight - t.Metrics.StatusHeight
}
