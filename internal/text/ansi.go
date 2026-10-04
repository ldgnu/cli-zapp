package text

import (
	"strings"
	"unicode/utf8"
)

// finalByte reports whether b terminates a CSI sequence.
//
// The terminator is a single byte in U+0040–U+007E.
func finalByte(b byte) bool { return b >= 0x40 && b <= 0x7e }

// introducerKind classifies the byte following ESC.
type introducerKind int

const (
	// introCSI is ESC '[', a control sequence.
	introCSI introducerKind = iota
	// introString is ESC ']', 'P', 'X', '^' or '_': a payload terminated by BEL
	// or ESC '\'.
	introString
	// introSingle is a two-byte escape such as ESC 'c'.
	introSingle
	// introNone means the byte is not an introducer at all.
	introNone
)

// classifyIntroducer names the kind of sequence a byte introduces.
func classifyIntroducer(b byte) introducerKind {
	switch b {
	case '[':
		return introCSI
	case ']', 'P', 'X', '^', '_':
		return introString
	default:
		// ESC Fe is a two-byte escape when Fe is in the range U+0040–U+005F.
		if b >= 0x40 && b <= 0x5f {
			return introSingle
		}
		return introNone
	}
}

// is8BitIntro reports whether b begins an 8-bit control sequence.
//
// Terminals set these with SM/RM rather than ESC '['; a scanner that ignores them
// renders the payload as visible text.
func is8BitIntro(b byte) bool {
	switch b {
	case 0x9b: // CSI
		return true
	case 0x9d: // OSC
		return true
	case 0x90, 0x98, 0x9e, 0x9f: // DCS, SOS, PM, APC
		return true
	default:
		return false
	}
}

// scanEscape consumes one escape sequence whose introducer is at s[i],
// returning the index just past it.
//
// An unterminated sequence consumes the remainder of the string. That is the safe
// direction to fail: a truncated escape is never useful to display, and emitting
// its payload is how terminal injection happens.
func scanEscape(s string, i int) int {
	if i >= len(s) {
		return i
	}

	switch {
	case is8BitIntro(s[i]):
		// An 8-bit introducer stands alone; the payload follows directly.
		return scanEscapeBody(s, i+1, eightBitKind(s[i]))

	case s[i] == 0x1b:
		if i+1 >= len(s) {
			return len(s)
		}
		return scanEscapeBody(s, i+2, classifyIntroducer(s[i+1]))

	default:
		// Not a sequence at all.
		return i
	}
}

// scanEscapeBody consumes a sequence body starting at i.
func scanEscapeBody(s string, i int, kind introducerKind) int {
	switch kind {
	case introCSI:
		for ; i < len(s); i++ {
			if finalByte(s[i]) {
				return i + 1
			}
		}
		return len(s)

	case introString:
		return scanStringBody(s, i)

	case introSingle:
		return minInt(i+1, len(s))

	default:
		// Not a recognised introducer; the byte is not a sequence at all.
		return i - 1
	}
}

// eightBitKind classifies the 8-bit form of an introducer.
func eightBitKind(b byte) introducerKind {
	if b == 0x9b {
		return introCSI
	}
	return introString
}

// scanStringBody consumes a BEL- or ESC-'\'-terminated payload starting at i.
func scanStringBody(s string, i int) int {
	for ; i < len(s); i++ {
		switch s[i] {
		case 0x07: // BEL
			return i + 1

		case 0x1b:
			// ESC '\' terminates; a bare ESC does not, so keep looking.
			if i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}

		default:
			// Payload byte.
		}
	}
	return len(s)
}

// segment is one run of either visible text or an escape sequence.
type segment struct {
	text   string
	escape bool
}

// scanAll splits s into alternating visible and escape segments.
//
// Every function in this package that needs to know where the sequences are walks
// this once, rather than each maintaining its own scanner — which is how a
// measure function and a strip function end up disagreeing about where a sequence
// ends, and every column calculation slowly goes wrong.
func scanAll(s string) []segment {
	if s == "" {
		return nil
	}

	segs := make([]segment, 0, 8)
	var plain strings.Builder

	flush := func() {
		if plain.Len() > 0 {
			segs = append(segs, segment{text: plain.String()})
			plain.Reset()
		}
	}

	for i := 0; i < len(s); {
		// A multi-byte rune is text, full stop.
		//
		// This check must come first. The 8-bit C1 control codes (0x90, 0x9b,
		// 0x9d, 0x9f …) share their numeric range with UTF-8 continuation bytes,
		// so a byte-at-a-time scan reports the second byte of an emoji as an APC
		// introducer and silently eats the character. Decoding runes first is what
		// keeps non-ASCII text intact.
		if _, size := utf8.DecodeRuneInString(s[i:]); size > 1 {
			plain.WriteString(s[i : i+size])
			i += size
			continue
		}

		n := scanEscape(s, i)
		if n <= i {
			// Not an escape; copy one byte as text.
			plain.WriteByte(s[i])
			i++
			continue
		}
		flush()
		segs = append(segs, segment{text: s[i:n], escape: true})
		i = n
	}

	flush()
	return segs
}

// StripANSI removes escape sequences from s.
//
// Used before measuring styled content, and before handing text to an external
// program such as a clipboard helper.
func StripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, seg := range scanAll(s) {
		if !seg.escape {
			b.WriteString(seg.text)
		}
	}
	return b.String()
}

// HasEscape reports whether s contains any escape sequence.
func HasEscape(s string) bool {
	for _, seg := range scanAll(s) {
		if seg.escape {
			return true
		}
	}
	return false
}

// TruncateStyled shortens a styled string to at most width *visible* cells,
// preserving the escape sequences that fall before the cut.
//
// [Truncate] is for plain text. A styled string must be measured with
// [VisibleWidth]: every "\x1b[38;2;17;27;33m" occupies zero visible cells, not the
// nineteen bytes it is made of. Sequences entirely past the cut are dropped, so
// the result carries no dangling styling.
func TruncateStyled(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if VisibleWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}

	var (
		b   strings.Builder
		vis int
	)
	b.Grow(len(s))

	for _, seg := range scanAll(s) {
		if seg.escape {
			b.WriteString(seg.text)
			continue
		}

		var cut bool
		iter(seg.text, func(cluster string) bool {
			cw := clusterWidth(cluster)
			if vis+cw > width-1 {
				cut = true
				return false
			}
			vis += cw
			b.WriteString(cluster)
			return true
		})
		if cut {
			b.WriteString("…")
			return b.String()
		}
	}
	return b.String()
}

// WrapStyled breaks a styled string into lines of at most width visible cells.
//
// Escape sequences are carried through rather than counted, so a coloured
// paragraph wraps on its visible width. Styling is not rebalanced across a break:
// a colour opened on one line stays open for the rest of that line, which is
// correct, and each returned line is self-contained for the sequences it contains.
func WrapStyled(s string, width int) []string {
	if width <= 0 {
		return nil
	}

	var (
		lines []string
		cur   strings.Builder
		vis   int
		word  strings.Builder
		wordW int
	)

	// flushWord appends the pending word to the current line, wrapping first if
	// it does not fit.
	flushWord := func() {
		if wordW == 0 {
			return
		}
		if vis > 0 && vis+wordW > width {
			lines = append(lines, cur.String())
			cur.Reset()
			vis = 0
		} else if vis > 0 {
			cur.WriteByte(' ')
			vis++
		}
		cur.WriteString(word.String())
		vis += wordW
		word.Reset()
		wordW = 0
	}

	flushLine := func() {
		flushWord()
		if vis > 0 || cur.Len() > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			vis = 0
		}
	}

	for _, seg := range scanAll(s) {
		if seg.escape {
			// Styling belongs to the word it opens on.
			cur.WriteString(seg.text)
			word.WriteString(seg.text)
			continue
		}

		for _, r := range seg.text {
			switch r {
			case '\n':
				flushLine()
			case ' ':
				// flushWord writes the separating space itself when the word is
				// appended to a line that already has content; adding one here as
				// well is what produces a double space between words.
				flushWord()
			default:
				rw := RuneWidth(r)
				// An overlong word is emitted on its own lines rather than
				// allowed to overflow the pane.
				if wordW+rw > width {
					flushWord()
					flushLine()
					lines = append(lines, hardWrap(word.String(), width)...)
					continue
				}
				word.WriteRune(r)
				wordW += rw
			}
		}
	}
	flushLine()

	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// minInt is a local helper, kept so this package needs no imports beyond the
// width tables.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
