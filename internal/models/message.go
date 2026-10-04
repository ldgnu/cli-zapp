package models

import (
	"strings"
	"time"
	"unicode"
)

// MessageDirection says who sent a message relative to the logged-in account.
type MessageDirection int

// Recognised directions.
const (
	// DirectionUnknown is the zero value. A message whose direction has not
	// been resolved must still render; treat it as incoming.
	DirectionUnknown MessageDirection = iota
	DirectionIncoming
	DirectionOutgoing
)

// String implements fmt.Stringer.
func (d MessageDirection) String() string {
	switch d {
	case DirectionIncoming:
		return "incoming"
	case DirectionOutgoing:
		return "outgoing"
	case DirectionUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// DeliveryStatus is the lifecycle state of an outgoing message.
//
// The ordering of the constants is meaningful: a message advances through them
// monotonically, so [DeliveryStatus.AtLeast] is a valid comparison.
type DeliveryStatus int

// Recognised delivery states, in advancement order.
const (
	// DeliveryPending covers everything from "handed to the sync engine" up to
	// the server's first acknowledgement. It is deliberately a single bucket:
	// the intermediate stages are not information a chat user acts on.
	DeliveryPending DeliveryStatus = iota
	DeliverySent
	DeliveryDelivered
	DeliveryRead
	DeliveryFailed
)

// String implements fmt.Stringer.
func (d DeliveryStatus) String() string {
	switch d {
	case DeliveryPending:
		return "pending"
	case DeliverySent:
		return "sent"
	case DeliveryDelivered:
		return "delivered"
	case DeliveryRead:
		return "read"
	case DeliveryFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Terminal reports whether no further status transition is expected.
func (d DeliveryStatus) Terminal() bool {
	return d == DeliveryRead || d == DeliveryFailed
}

// Glyph returns the single-rune indicator shown next to an outgoing message.
//
// These are ASCII-adjacent Unicode marks chosen for being legible in most
// terminal fonts; see docs/DESIGN.md for the reasoning behind the shapes.
func (d DeliveryStatus) Glyph() string {
	switch d {
	case DeliveryPending:
		return "◌"
	case DeliverySent:
		return "✓"
	case DeliveryDelivered:
		return "✓✓"
	case DeliveryRead:
		return "✓✓" // rendered in an accent colour instead
	case DeliveryFailed:
		return "!"
	default:
		return "·"
	}
}

// MessageKind is the content type of a message.
type MessageKind int

// Recognised message kinds.
const (
	KindUnknown MessageKind = iota
	KindText
	KindImage
	KindVideo
	KindAudio
	KindVoice
	KindDocument
	KindSticker
	KindLocation
	KindContactCard
	KindPoll
	KindSystem
	KindDeleted
	KindRevoked
	KindUnsupported
)

// String implements fmt.Stringer.
func (k MessageKind) String() string {
	switch k {
	case KindText:
		return "text"
	case KindImage:
		return "image"
	case KindVideo:
		return "video"
	case KindAudio:
		return "audio"
	case KindVoice:
		return "voice"
	case KindDocument:
		return "document"
	case KindSticker:
		return "sticker"
	case KindLocation:
		return "location"
	case KindContactCard:
		return "contact"
	case KindPoll:
		return "poll"
	case KindSystem:
		return "system"
	case KindDeleted:
		return "deleted"
	case KindRevoked:
		return "revoked"
	case KindUnsupported:
		return "unsupported"
	case KindUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// HasMedia reports whether the message carries an attachment.
func (k MessageKind) HasMedia() bool {
	switch k {
	case KindImage, KindVideo, KindAudio, KindVoice, KindDocument, KindSticker:
		return true
	default:
		return false
	}
}

// IsRemovable reports whether the message can be deleted for everyone.
func (k MessageKind) IsRemovable() bool {
	switch k {
	case KindText, KindImage, KindVideo, KindAudio, KindVoice, KindDocument, KindSticker:
		return true
	default:
		return false
	}
}

// Message is a single item in a conversation.
//
// The UI reads this type; it never constructs protocol payloads. A message
// that failed to send is still a Message, carrying [Message.SendError], so
// that a failed send stays visible in the transcript instead of vanishing.
type Message struct {
	ID        MessageID
	ChatID    ChatID
	SenderID  ContactID
	Direction MessageDirection

	Kind MessageKind

	// Body is the plain text of the message. For media messages it is the
	// caption, which may be empty. Never contains protocol markup.
	Body string

	// Timestamp is when the message was sent by its author.
	Timestamp time.Time

	// Status applies to outgoing messages only.
	Status DeliveryStatus

	// SendError is set when an outgoing message could not be delivered. The UI
	// surfaces it and offers a retry; the storage layer persists it so that a
	// failure survives a restart.
	SendError string

	// ReplyTo references the message being quoted, if any.
	ReplyTo *MessageRef

	// Forwarded is true when the message was forwarded from another chat.
	Forwarded bool

	// EditCount tracks how many times the message has been edited.
	EditCount int

	// Revoked marks a message that was deleted for everyone. Its body is
	// cleared; the message itself remains in the transcript as a tombstone so
	// that the conversation flow does not jump.
	Revoked bool

	// Media describes the attachment, nil for text messages.
	Media *Media

	// Reactions maps a reaction glyph to the contacts that used it, and
	// includes the empty string as the key for "reacted with nothing", which
	// is how WhatsApp represents a reaction retraction.
	Reactions map[string][]ContactID

	// Mentions lists contacts mentioned in the body via @-syntax.
	Mentions []ContactID

	// SystemText is the human-readable text of a KindSystem message, which is
	// server-formatted rather than authored by a user.
	SystemText string

	// Local marks a message that exists only in this client and has not been
	// acknowledged by the server yet.
	Local bool
}

// IsOutgoing reports whether the logged-in account sent the message.
func (m Message) IsOutgoing() bool { return m.Direction == DirectionOutgoing }

// Text returns the string to render as the message body, taking revocation
// into account so that a revoked message never leaks its original content.
func (m Message) Text() string {
	if m.Revoked {
		return ""
	}
	return m.Body
}

// ReactionSummary returns the distinct reactions in a deterministic order, so
// that the rendered bar does not reshuffle between frames.
//
// Sorted by glyph using the same fold rules as chat names, which keeps
// ordering stable and locale-independent.
func (m Message) ReactionSummary() []Reaction {
	if len(m.Reactions) == 0 {
		return nil
	}
	out := make([]Reaction, 0, len(m.Reactions))
	for glyph, who := range m.Reactions {
		out = append(out, Reaction{
			Glyph:    glyph,
			Count:    len(who),
			Contacts: append([]ContactID(nil), who...),
		})
	}
	for i := 1; i < len(out); i++ {
		cur := out[i]
		j := i - 1
		for j >= 0 && lessFold(out[j].Glyph, cur.Glyph) > 0 {
			out[j+1] = out[j]
			j--
		}
		out[j+1] = cur
	}
	return out
}

// Reaction is one aggregated reaction bar.
type Reaction struct {
	Glyph    string
	Count    int
	Contacts []ContactID
}

// MessageRef is a lightweight reference to another message, used for replies
// and for edit provenance. It intentionally carries only the fields needed to
// render a quote, not a nested Message.
type MessageRef struct {
	ID         MessageID
	SenderID   ContactID
	SenderName string
	Body       string
	Kind       MessageKind
	Timestamp  time.Time
	Revoked    bool
}

// PreviewString returns a one-line summary suitable for the chat list.
//
// The result never contains a newline, so it is safe to place in a single-row
// widget without further processing.
func (p MessagePreview) String() string {
	var b strings.Builder
	if p.IsOutgoing {
		b.WriteString("You: ")
	}
	b.WriteString(previewBody(p.Kind, p.Body, p.Media))
	return strings.TrimSpace(collapseSpaces(b.String()))
}

// previewBody renders the body of a preview, substituting a human description
// when the message has no text.
func previewBody(kind MessageKind, body string, media *Media) string {
	if body = strings.TrimSpace(body); body != "" {
		return body
	}
	switch {
	case kind.HasMedia() && media != nil:
		if media.Filename != "" {
			return kind.String() + ": " + media.Filename
		}
		return kind.String()
	case kind.HasMedia():
		return kind.String()
	case kind == KindSystem:
		return "system message"
	case kind == KindLocation:
		return "location"
	case kind == KindPoll:
		return "poll"
	case kind == KindContactCard:
		return "contact"
	default:
		return ""
	}
}

// MessagePreview is the compact form of a Message used by the chat list.
type MessagePreview struct {
	Kind       MessageKind
	Body       string
	IsOutgoing bool
	SenderName string
	Media      *Media
	Timestamp  time.Time
	Revoked    bool
}

// collapseSpaces folds runs of whitespace, including newlines, into single
// spaces. Used wherever text moves from a multi-line context into a single-line
// widget.
func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

// FirstLine returns the first non-empty line of a message body, collapsed to
// single spaces. Used by the chat list, which has one line to work with.
func FirstLine(s string) string {
	for line := range strings.Lines(s) {
		if t := strings.TrimSpace(line); t != "" {
			return collapseSpaces(t)
		}
	}
	return ""
}

// SortMessages orders a transcript chronologically, breaking ties on
// MessageID so the order is total and stable across a restart.
//
// Messages sharing a timestamp are common: history sync delivers batches whose
// members all carry the server's batch timestamp.
func SortMessages(msgs []Message) {
	for i := 1; i < len(msgs); i++ {
		cur := msgs[i]
		j := i - 1
		for j >= 0 && messageLess(cur, msgs[j]) {
			msgs[j+1] = msgs[j]
			j--
		}
		msgs[j+1] = cur
	}
}

func messageLess(a, b Message) bool {
	if !a.Timestamp.Equal(b.Timestamp) {
		return a.Timestamp.Before(b.Timestamp)
	}
	return a.ID < b.ID
}
