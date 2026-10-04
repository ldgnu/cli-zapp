package models

import (
	"fmt"
	"strings"
	"time"
)

// MediaKind is the attachment format family.
type MediaKind int

// Recognised media kinds.
const (
	MediaKindUnknown MediaKind = iota
	MediaKindImage
	MediaKindVideo
	MediaKindAudio
	MediaKindVoice
	MediaKindDocument
	MediaKindSticker
)

// String implements fmt.Stringer.
func (m MediaKind) String() string {
	switch m {
	case MediaKindImage:
		return "image"
	case MediaKindVideo:
		return "video"
	case MediaKindAudio:
		return "audio"
	case MediaKindVoice:
		return "voice"
	case MediaKindDocument:
		return "document"
	case MediaKindSticker:
		return "sticker"
	case MediaKindUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// IsRenderable reports whether cli-zapp can attempt to draw this media as an
// image in the terminal.
//
// Terminal graphics require either a Sixel-, Kitty- or iTerm-compatible
// emulator, or an external renderer such as chafa or viu writing to the
// terminal. Where none is available the UI falls back to a text description;
// see [Media.Description].
func (m MediaKind) IsRenderable() bool {
	return m == MediaKindImage || m == MediaKindSticker
}

// Media describes an attachment.
//
// No media bytes are held here. Media is downloaded on demand into a
// user-configured directory and referenced by path, which keeps the resident
// set of the UI independent of conversation history size.
type Media struct {
	Kind MediaKind

	// Filename is the sender-supplied name, present for documents and
	// sometimes for images.
	Filename string

	// MimeType is the declared content type. It is advisory: cli-zapp trusts it
	// for choosing a renderer but falls back to [Media.Kind] when it is empty
	// or unrecognised.
	MimeType string

	// Size is the plaintext size in bytes, as reported by the sender.
	Size int64

	// Width and Height are the pixel dimensions of image and video media.
	// Zero for non-visual media.
	Width, Height int

	// Duration is the playback length of audio and video. Zero otherwise.
	Duration time.Duration

	// LocalPath is where the media was written after download, empty until
	// then.
	LocalPath string

	// PageCount is the number of pages in a document, zero when not applicable.
	PageCount int
}

// Description returns a one-line human description of the media, used as the
// fallback rendering on terminals without graphics support and as the
// chat list preview text.
func (m Media) Description() string {
	var b strings.Builder
	b.WriteString(m.Kind.String())
	if m.Duration > 0 {
		b.WriteString(" ")
		b.WriteString(FormatDuration(m.Duration))
	}
	if m.Width > 0 && m.Height > 0 {
		fmt.Fprintf(&b, " %dx%d", m.Width, m.Height)
	}
	if name := strings.TrimSpace(m.Filename); name != "" {
		b.WriteString(" ")
		b.WriteString(name)
	} else if m.Size > 0 {
		fmt.Fprintf(&b, " %s", FormatBytes(m.Size))
	}
	return strings.TrimSpace(b.String())
}

// FormatBytes renders a byte count using binary (1024-based) units, which is
// what operating systems report and therefore what users expect.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	// One decimal place is enough to distinguish adjacent units without
	// implying a precision the sender never provided.
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

// FormatDuration renders a duration as m:ss, or h:mm:ss past an hour, matching
// how media players label length.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d / time.Second)
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
