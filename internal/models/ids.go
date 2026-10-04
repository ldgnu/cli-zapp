package models

import (
	"errors"
	"fmt"
	"strings"
)

// ErrEmptyID is returned when an identifier is required but was left blank.
var ErrEmptyID = errors.New("models: identifier must not be empty")

// ChatID identifies a conversation: a direct chat or a group.
//
// It is an opaque string. Producers (currently the whatsmeow adapter) are
// responsible for encoding the underlying protocol identifier into it.
type ChatID string

// Valid reports whether the ChatID is usable.
func (c ChatID) Valid() bool { return strings.TrimSpace(string(c)) != "" }

// String implements fmt.Stringer.
func (c ChatID) String() string { return string(c) }

// MessageID identifies a single message within a chat.
type MessageID string

// Valid reports whether the MessageID is usable.
func (m MessageID) Valid() bool { return strings.TrimSpace(string(m)) != "" }

// String implements fmt.Stringer.
func (m MessageID) String() string { return string(m) }

// ContactID identifies a WhatsApp account.
type ContactID string

// Valid reports whether the ContactID is usable.
func (c ContactID) Valid() bool { return strings.TrimSpace(string(c)) != "" }

// String implements fmt.Stringer.
func (c ContactID) String() string { return string(c) }

// NewChatID builds a ChatID, rejecting blank input.
//
// Adapters call this instead of casting, so that a malformed protocol payload
// fails at the boundary rather than producing an unusable ChatID that silently
// matches nothing downstream.
func NewChatID(s string) (ChatID, error) {
	if strings.TrimSpace(s) == "" {
		return "", ErrEmptyID
	}
	return ChatID(s), nil
}

// NewMessageID builds a MessageID, rejecting blank input.
func NewMessageID(s string) (MessageID, error) {
	if strings.TrimSpace(s) == "" {
		return "", ErrEmptyID
	}
	return MessageID(s), nil
}

// NewContactID builds a ContactID, rejecting blank input.
func NewContactID(s string) (ContactID, error) {
	if strings.TrimSpace(s) == "" {
		return "", ErrEmptyID
	}
	return ContactID(s), nil
}

// MustMessageID is NewMessageID for use in tests and static fixtures, where a
// failure indicates a programming error rather than bad input.
func MustMessageID(s string) MessageID {
	id, err := NewMessageID(s)
	if err != nil {
		panic(fmt.Sprintf("MustMessageID(%q): %v", s, err))
	}
	return id
}
