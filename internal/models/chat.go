package models

import (
	"strings"
	"time"
	"unicode"
)

// ChatType distinguishes direct conversations from groups and broadcasts.
type ChatType int

// Recognised chat types.
const (
	// ChatTypeUnknown is the zero value, used when the type has not yet been
	// resolved. UI code must render it gracefully rather than assume otherwise.
	ChatTypeUnknown ChatType = iota
	ChatTypeDirect
	ChatTypeGroup
	ChatTypeBroadcast
	ChatTypeNewsletter
	ChatTypeStatus
)

// String implements fmt.Stringer.
func (c ChatType) String() string {
	switch c {
	case ChatTypeDirect:
		return "direct"
	case ChatTypeGroup:
		return "group"
	case ChatTypeBroadcast:
		return "broadcast"
	case ChatTypeNewsletter:
		return "newsletter"
	case ChatTypeStatus:
		return "status"
	case ChatTypeUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// IsGroup reports whether the chat has group semantics (members, admins, etc).
func (c ChatType) IsGroup() bool {
	return c == ChatTypeGroup || c == ChatTypeNewsletter
}

// Chat is a conversation as the rest of the application sees it.
//
// Field naming is deliberately protocol-neutral: there is no "jid", no
// "unread_ack", no "archived_flag" — those belong to the adapter and the
// storage layer respectively.
type Chat struct {
	ID   ChatID
	Type ChatType

	// Name is the display name: the contact's push name for direct chats, the
	// subject for groups. May be empty; callers should fall back to
	// [Chat.FallbackName].
	Name string

	// AvatarURL is an address from which the avatar bytes can be fetched. It is
	// a URL rather than decoded image bytes so that wterm does not hold images
	// in memory; the media service resolves it on demand.
	AvatarURL string

	// LastMessage is a preview of the most recent message, used for the
	// second line of the chat list entry. A nil value means the history has
	// not been fetched yet, which the UI renders differently from "this chat
	// has no messages".
	LastMessage *MessagePreview

	// Timestamp is when the chat last had activity, used for ordering.
	Timestamp time.Time

	UnreadCount int
	HasUnread   bool

	Pinned   bool
	Muted    bool
	Archived bool

	// Typing reports whether the peer is currently composing. This is
	// ephemeral presence information and is never persisted.
	Typing bool

	// Contact is populated for direct chats, nil for groups.
	Contact *Contact
}

// FallbackName returns a name suitable for display, never the empty string.
func (c Chat) FallbackName() string {
	if n := strings.TrimSpace(c.Name); n != "" {
		return n
	}
	// A chat always has an identifier; showing a truncated form of it is more
	// useful to the user than a blank row in the sidebar.
	id := string(c.ID)
	if id == "" {
		return "(unknown chat)"
	}
	if len(id) > 12 {
		return "…" + id[len(id)-12:]
	}
	return id
}

// PreviewString returns the chat list's secondary line, collapsing the
// never-fetched case to the empty string.
func (c Chat) PreviewString() string {
	if c.LastMessage == nil {
		return ""
	}
	return c.LastMessage.String()
}

// SortChats orders chats the way the sidebar displays them: pinned first, then
// most recent activity, then by name so the order stays deterministic when
// timestamps tie.
//
// The sort is stable for full ties, which keeps the sidebar from reshuffling
// rows during an incremental sync.
func SortChats(chats []Chat) {
	// Insertion sort keeps the dependency surface at zero and is more than
	// fast enough: a sidebar holds tens of rows, not thousands. It is also
	// stable, which sorting slice values by comparison is not without extra
	// bookkeeping.
	for i := 1; i < len(chats); i++ {
		cur := chats[i]
		j := i - 1
		for j >= 0 && chatLess(cur, chats[j]) {
			chats[j+1] = chats[j]
			j--
		}
		chats[j+1] = cur
	}
}

// chatLess reports whether a should be displayed before b.
func chatLess(a, b Chat) bool {
	if a.Pinned != b.Pinned {
		return a.Pinned
	}
	if !a.Timestamp.Equal(b.Timestamp) {
		return a.Timestamp.After(b.Timestamp)
	}
	return lessFold(a.FallbackName(), b.FallbackName()) < 0
}

// lessFold compares two strings the way a human sorts them: case-insensitively,
// and with runs of whitespace collapsed so that "Ana  Lopez" and "Ana Lopez"
// do not order differently.
func lessFold(a, b string) int {
	ar, br := foldRunes(a), foldRunes(b)
	for i := 0; i < len(ar) && i < len(br); i++ {
		if ar[i] != br[i] {
			if ar[i] < br[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ar) < len(br):
		return -1
	case len(ar) > len(br):
		return 1
	default:
		return 0
	}
}

func foldRunes(s string) []rune {
	out := make([]rune, 0, len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if prevSpace {
				continue
			}
			r = ' '
			prevSpace = true
		} else {
			prevSpace = false
			r = unicode.ToLower(r)
		}
		out = append(out, r)
	}
	return out
}
