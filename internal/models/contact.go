package models

import (
	"strings"
	"time"
)

// PresenceState is what a contact's availability is currently known to be.
type PresenceState int

// Recognised presence states.
//
// [PresenceUnknown] is not an error state. It is the correct answer whenever
// the user has not granted presence visibility, or presence has not been
// requested yet, and the UI must render it as such rather than as "offline".
const (
	PresenceUnknown PresenceState = iota
	PresenceAvailable
	PresenceUnavailable
	PresenceLastSeen
)

// String implements fmt.Stringer.
func (p PresenceState) String() string {
	switch p {
	case PresenceAvailable:
		return "available"
	case PresenceUnavailable:
		return "unavailable"
	case PresenceLastSeen:
		return "last seen"
	case PresenceUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// Presence is a contact's availability.
//
// The distinction between [PresenceUnknown] and [PresenceUnavailable] matters:
// the first means wterm cannot know, the second means the contact is actually
// offline. Collapsing them would tell users something untrue.
type Presence struct {
	ContactID ContactID
	State     PresenceState
	LastSeen  time.Time
	// Typing is set while the contact is composing in a given chat.
	Typing bool
	// Recording is set while the contact is holding a voice message.
	Recording bool
}

// IsOnline reports whether the contact is known to be reachable right now.
func (p Presence) IsOnline() bool { return p.State == PresenceAvailable }

// Label returns the text shown next to a contact's name in the chat header.
func (p Presence) Label() string {
	switch p.State {
	case PresenceAvailable:
		return "online"
	case PresenceUnavailable:
		return "offline"
	case PresenceLastSeen:
		if p.LastSeen.IsZero() {
			return "last seen recently"
		}
		return "last seen " + FormatRelativeTime(p.LastSeen)
	case PresenceUnknown:
		return ""
	default:
		return ""
	}
}

// Contact is a WhatsApp account as wterm models it.
type Contact struct {
	ID ContactID

	// Name is the push name, the name the contact chose to show. It is not
	// necessarily their profile name and can be empty.
	Name string

	// Phone is the contact's phone number in international format when known.
	Phone string

	// About is the contact's "status" text, free-form and often absent.
	About string

	AvatarURL string

	// Verified and Business are profile attributes exposed by the protocol.
	// They are informational only.
	Verified bool
	Business bool

	// Blocked reports whether the local user has blocked this contact.
	Blocked bool

	// IsSelf is true for the logged-in account's own contact record.
	IsSelf bool

	Presence Presence
}

// DisplayName returns the best available name for the contact, never empty.
func (c Contact) DisplayName() string {
	if n := strings.TrimSpace(c.Name); n != "" {
		return n
	}
	if p := strings.TrimSpace(c.Phone); p != "" {
		return p
	}
	return string(c.ID)
}

// Initials returns up to two uppercase initials for the contact, for the
// avatar placeholder in terminals without graphics support.
//
// Initials are taken from the first and last word of the display name so that
// "Ada Lovelace" yields "AL" rather than "AL" from any two letters.
func (c Contact) Initials() string {
	words := strings.Fields(c.DisplayName())
	switch len(words) {
	case 0:
		return "?"
	case 1:
		return initialOf(words[0])
	default:
		return initialOf(words[0]) + initialOf(words[len(words)-1])
	}
}

func initialOf(word string) string {
	for _, r := range word {
		if isLetterOrDigit(r) {
			return strings.ToUpper(string(r))
		}
	}
	return "?"
}

func isLetterOrDigit(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	default:
		return false
	}
}

// ContactInfo is the aggregate view shown by the contact and group info
// overlay. It exists so the overlay depends on one type rather than assembling
// several.
type ContactInfo struct {
	Contact
	// Chats is the number of shared conversations.
	Chats int
	// Groups is the number of shared groups.
	Groups int
	// Blocked is the chat-level block state, which can differ from the
	// contact-level one.
	ChatBlocked bool
}

// Group is a WhatsApp group.
type Group struct {
	ID ChatID

	// Name is the group subject.
	Name string

	// Topic is the group description, distinct from the subject.
	Topic string

	// Description is the "group description" a member may set. Kept separate
	// from [Group.Topic] because WhatsApp exposes both and conflating them
	// loses information.
	Description string

	AvatarURL string

	// InviteLink is the group's join link when the protocol exposes it.
	InviteLink string

	OwnerID ContactID

	// Participants is the membership roster.
	Participants []GroupParticipant

	// MemberAddMode and JoinApproval are the group admission settings.
	MemberAddMode  MemberAddMode
	JoinApproval   bool
	AnnounceOnly   bool
	Locked         bool
	ParticipantAdd ParticipantAddPolicy

	CreatedAt time.Time
}

// ParticipantAddPolicy restricts who may add members.
type ParticipantAddPolicy int

// Recognised participant-add policies.
const (
	ParticipantAddAll ParticipantAddPolicy = iota
	ParticipantAddAdmins
	ParticipantAddNone
)

// String implements fmt.Stringer.
func (p ParticipantAddPolicy) String() string {
	switch p {
	case ParticipantAddAdmins:
		return "admins"
	case ParticipantAddNone:
		return "none"
	case ParticipantAddAll:
		return "everyone"
	default:
		return "everyone"
	}
}

// MemberAddMode is the group's member-add mode as declared by WhatsApp.
type MemberAddMode int

// Recognised member-add modes.
const (
	MemberAddModeUnknown MemberAddMode = iota
	MemberAddModeAll
	MemberAddModeAdmins
)

// String implements fmt.Stringer.
func (m MemberAddMode) String() string {
	switch m {
	case MemberAddModeAll:
		return "all"
	case MemberAddModeAdmins:
		return "admins"
	case MemberAddModeUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// GroupParticipant is one member of a group.
type GroupParticipant struct {
	ID        ContactID
	Name      string
	Phone     string
	AvatarURL string

	// IsAdmin and IsSuperAdmin reflect the group's own role flags, not the
	// local user's permissions.
	IsAdmin      bool
	IsSuperAdmin bool
}

// DisplayName returns the participant's best available name, never empty.
func (p GroupParticipant) DisplayName() string {
	if n := strings.TrimSpace(p.Name); n != "" {
		return n
	}
	if ph := strings.TrimSpace(p.Phone); ph != "" {
		return ph
	}
	return string(p.ID)
}

// Admins returns the participants holding the admin role, in roster order.
func (g Group) Admins() []GroupParticipant {
	out := make([]GroupParticipant, 0, 4)
	for _, p := range g.Participants {
		if p.IsAdmin || p.IsSuperAdmin {
			out = append(out, p)
		}
	}
	return out
}

// DisplayName returns the group's best available name, never empty.
func (g Group) DisplayName() string {
	if n := strings.TrimSpace(g.Name); n != "" {
		return n
	}
	return string(g.ID)
}
