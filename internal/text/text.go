// Package text provides terminal-aware string measurement, wrapping and
// sanitisation.
//
// # Why not just len()
//
// A Go string counts bytes. A terminal counts cells. For "ñ" those are 2 and 1;
// for an emoji they are 4 and 2; for a CJK ideograph they are 3 and 2. Using byte
// length to lay out a TUI produces text that overflows its column, which is the
// most common way a terminal UI looks broken.
//
// # Three concerns, one scanner
//
// The package is organised around three jobs that all need to know where the
// escape sequences in a string are:
//
//   - measurement, in [Width], [VisibleWidth] and [TruncateStyled]
//   - layout, in [Wrap] and [WrapStyled]
//   - safety, in [Sanitize] and [StripANSI]
//
// Each needs its own answer to "is this byte visible?", and a bug in any one of
// them is a layout or security defect. So they all walk the single scanner in
// ansi.go rather than each rolling its own.
//
// # East Asian width and grapheme clusters
//
// Wide characters occupy two cells, and a combining accent belongs to the
// character before it. Both matter: measuring per rune makes a wrapped CJK line
// overflow, and cutting per rune splits an emoji into a replacement box.
package text

import (
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// OverlayCells paints src over the start of dst, preserving both their styling.
//
// src's escape sequences are emitted as-is; only the visible cells src covers are
// taken from it, and dst supplies everything beyond. It is the primitive behind
// drawing a dialog on top of a conversation without erasing the parts of the
// conversation that stick out around it.
//
// Cells beyond dst's width are dropped, so the result never grows the line.
func OverlayCells(src, dst string) string {
	srcCells := VisibleWidth(src)
	dstCells := VisibleWidth(dst)
	if srcCells == 0 {
		return dst
	}

	var (
		b    strings.Builder
		cell int
	)
	b.Grow(len(src) + len(dst))

	for _, seg := range scanAll(src) {
		if seg.escape {
			b.WriteString(seg.text)
			continue
		}
		for _, r := range seg.text {
			if cell >= dstCells {
				// Past the end of the destination; nothing more of src is shown.
				b.WriteString(trimCells(StripANSI(dst), srcCells))
				return b.String()
			}
			b.WriteRune(r)
			cell++
		}
	}

	if srcCells < dstCells {
		b.WriteString(trimCells(StripANSI(dst), srcCells))
	}
	return b.String()
}

// trimCells drops the first n visible cells from s, never splitting a rune.
func trimCells(s string, n int) string {
	if n <= 0 {
		return s
	}
	w := 0
	for i, r := range s {
		if w >= n {
			return s[i:]
		}
		w += RuneWidth(r)
	}
	return ""
}

// PadRightStyled extends a styled string with spaces to exactly width visible
// cells.
func PadRightStyled(s string, width int) string {
	pad := width - VisibleWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// Width returns the number of terminal cells s occupies, counting escape
// sequences as visible.
//
// For plain text this is the right measure. For styled text use [VisibleWidth],
// which ignores the escapes.
func Width(s string) int {
	return uniseg.StringWidth(s)
}

// RuneWidth returns the cells occupied by a single rune.
func RuneWidth(r rune) int { return runewidth.RuneWidth(r) }

// Truncate shortens s to at most width cells, appending an ellipsis when it cut.
//
// A result exactly width cells long is returned unmodified: reserving a cell for
// the ellipsis would make the function lossy for text that already fits.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return TruncateNoEllipsis(s, width-1) + "…"
}

// TruncateNoEllipsis cuts s to at most width cells without adding an ellipsis.
//
// Used where the caller places its own marker, so that adding one here would
// double it up.
func TruncateNoEllipsis(s string, width int) string {
	if width <= 0 {
		return ""
	}

	var (
		b strings.Builder
		w int
	)
	b.Grow(len(s))

	iter(s, func(cluster string) bool {
		cw := clusterWidth(cluster)
		if w+cw > width {
			return false
		}
		w += cw
		b.WriteString(cluster)
		return true
	})
	return b.String()
}

// PadRight extends s with spaces to exactly width cells.
func PadRight(s string, width int) string {
	pad := width - Width(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// PadLeft prefixes s with spaces to exactly width cells.
func PadLeft(s string, width int) string {
	pad := width - Width(s)
	if pad <= 0 {
		return s
	}
	return strings.Repeat(" ", pad) + s
}

// Wrap breaks s into lines of at most width cells, breaking on word boundaries
// where possible.
//
// Word wrapping matters for message bodies: cutting mid-word looks broken to a
// reader, and hard-wrapping never looks intentional. A word wider than the line —
// a URL, typically — is broken mid-word, because leaving it overlong would overflow
// the bubble and corrupt the layout.
func Wrap(s string, width int) []string {
	if width <= 0 {
		return nil
	}

	var lines []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, wrapWords(para, width)...)
	}
	return lines
}

func wrapWords(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}

	var (
		lines []string
		cur   strings.Builder
		vis   int
	)

	flush := func() {
		if vis > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			vis = 0
		}
	}

	for _, word := range words {
		ww := Width(word)
		switch {
		case ww > width:
			flush()
			lines = append(lines, hardWrap(word, width)...)

		case vis == 0:
			cur.WriteString(word)
			vis = ww

		case vis+1+ww <= width:
			cur.WriteByte(' ')
			cur.WriteString(word)
			vis += 1 + ww

		default:
			flush()
			cur.WriteString(word)
			vis = ww
		}
	}
	flush()

	return lines
}

// hardWrap breaks an overlong word into width-sized chunks.
func hardWrap(s string, width int) []string {
	var out []string
	rest := s

	for Width(rest) > width {
		cut := TruncateNoEllipsis(rest, width)
		// No progress means even the first cluster is wider than the line. One
		// cluster per line is the only remaining option: the line will exceed the
		// requested width, but a wide character cannot be halved without producing
		// mojibake, and continuing to loop would never terminate.
		if cut == "" || Width(cut) == Width(rest) {
			return append(out, splitByCluster(rest)...)
		}
		out = append(out, cut)
		rest = strings.TrimPrefix(rest, cut)
	}
	if rest != "" {
		out = append(out, rest)
	}
	return out
}

// VisibleWidth measures s as a terminal would render it, ignoring ANSI escape
// sequences entirely.
//
// Components that join independently styled segments use this to align columns;
// without it every escape sequence would count toward the width and no two columns
// would line up.
func VisibleWidth(s string) int {
	return Width(StripANSI(s))
}

// Sanitize makes s safe to render in a terminal, preserving newlines and tabs.
//
// Message bodies arrive over the network and are drawn directly, so a body
// containing escape sequences would let a sender repaint the screen, move the
// cursor, or set the terminal title and clipboard on the user's machine. This is
// the boundary that prevents that.
//
// Unlike [StripANSI] it also drops other C0 control characters, because a message
// body has no legitimate use for them and several — BEL, BS, DEL — have terminal
// side effects. Unterminated sequences consume the remainder of the string, which
// is the safe direction to fail in: a truncated escape is never useful to display,
// and emitting its payload is the vulnerability.
func Sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, seg := range scanAll(s) {
		if seg.escape {
			continue
		}
		for _, r := range seg.text {
			switch {
			case r == '\n' || r == '\t':
				b.WriteRune(r)
			case r < 0x20 || r == 0x7f:
				// Other C0 controls and DEL are dropped.
			default:
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
