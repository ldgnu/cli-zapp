package app

import (
	"context"
	"time"

	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/notifications"
	"github.com/wterm/wterm/internal/whatsapp"
)

// Shutdown releases resources the model holds.
//
// Bubble Tea's Run has returned by the time this is called, so the terminal has
// already been restored. That makes it the right place to stop background work:
// a goroutine still writing to a now-dead terminal would corrupt the shell's
// prompt.
func (m *Model) Shutdown() error {
	// Stop the pump first, so it cannot post to a program that has already
	// returned, and so that Stop's channel close is not raced by a send.
	m.eventMu.Lock()
	if m.eventCancel != nil {
		m.eventCancel()
		m.eventCancel = nil
	}
	m.eventMu.Unlock()

	if m.deps.Sync == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return m.deps.Sync.Stop(ctx)
}

// SetNotifier attaches the notifier used for background messages.
//
// It is a setter rather than a constructor parameter because the notifier depends
// on the environment — whether notify-send exists — which is resolved in main
// after the model would otherwise already exist.
func (m *Model) SetNotifier(n *notifications.Notifier) { m.notifier = n }

// DemoData builds a fake populated with realistic conversations.
//
// The fixtures are deliberately varied: unread counts, a pinned chat, a group with
// members, media, a failed send, a reaction, a long multi-line message and one
// containing an escape sequence. They exist so that every rendering path is
// exercised during development, not because a screenshot would look better.
//
// They also double as the input for the UI's component tests, which is why they
// live in the app package rather than in main.
func DemoData() *whatsapp.Fake {
	const selfID = models.ContactID("5491100000001")

	self := models.Contact{
		ID:     selfID,
		Name:   "You",
		Phone:  "+54 9 11 0000-0001",
		About:  "using wterm",
		IsSelf: true,
	}
	f := whatsapp.NewFake(self)

	ada := models.Contact{
		ID:        "5491100000002",
		Name:      "Ada Lovelace",
		Phone:     "+54 9 11 0000-0002",
		About:     "analytical engine, 1843",
		AvatarURL: "",
		Presence:  models.Presence{ContactID: "5491100000002", State: models.PresenceAvailable},
		Verified:  true,
	}
	grace := models.Contact{
		ID:    "5491100000003",
		Name:  "Grace Hopper",
		Phone: "+54 9 11 0000-0003",
		Presence: models.Presence{
			ContactID: "5491100000003",
			State:     models.PresenceUnavailable,
			LastSeen:  time.Now().Add(-2 * time.Hour),
		},
	}
	alan := models.Contact{
		ID:    "5491100000004",
		Name:  "Alan Turing",
		Phone: "+44 20 7946 0000",
		Presence: models.Presence{
			ContactID: "5491100000004",
			State:     models.PresenceLastSeen,
			LastSeen:  time.Now().Add(-26 * time.Hour),
		},
	}
	f.PutContact(ada)
	f.PutContact(grace)
	f.PutContact(alan)

	now := time.Now()

	// Direct chats.
	f.PutChat(models.Chat{
		ID: "chat-ada", Type: models.ChatTypeDirect, Name: "Ada Lovelace",
		AvatarURL: ada.AvatarURL, Pinned: true, Timestamp: now,
		UnreadCount: 2, HasUnread: true,
		Contact: &ada,
	})
	f.PutChat(models.Chat{
		ID: "chat-grace", Type: models.ChatTypeDirect, Name: "Grace Hopper",
		AvatarURL: grace.AvatarURL, Timestamp: now.Add(-20 * time.Minute),
		UnreadCount: 1, HasUnread: true,
		Contact: &grace,
	})
	f.PutChat(models.Chat{
		ID: "chat-alan", Type: models.ChatTypeDirect, Name: "Alan Turing",
		AvatarURL: alan.AvatarURL, Timestamp: now.Add(-3 * time.Hour),
		Contact: &alan,
	})

	// A group, so group-specific rendering is visible from the first frame.
	f.PutChat(models.Chat{
		ID: "chat-group", Type: models.ChatTypeGroup, Name: "Terminal Gophers",
		Timestamp:   now.Add(-2 * time.Hour),
		UnreadCount: 5, HasUnread: true, Muted: true,
	})

	// A quiet chat, so the sidebar has a low-contrast row too.
	f.PutChat(models.Chat{
		ID: "chat-quiet", Type: models.ChatTypeDirect, Name: "Newsletter",
		AvatarURL: "", Timestamp: now.Add(-4 * 24 * time.Hour), Archived: true,
	})

	seed := func(chatID models.ChatID, sender models.ContactID, bodies ...string) {
		for i, body := range bodies {
			f.Receive(chatID, sender, body)
			// Space the messages out so the day separators and relative times
			// have something realistic to render.
			_ = i
		}
	}

	seed("chat-ada", "5491100000002",
		"Did the analytical engine notes land?",
		"They did. I started on the Bernoulli table this morning.",
		"One question: the looping notation. Is it the same as your punch cards?",
		"It's the same idea. Cards in, results out, no room for interpretation.",
		"That is reassuring. I will review it tonight.",
	)

	seed("chat-grace", "5491100000003",
		"Found the bug. It was a moth.",
	)
	seed("chat-alan", "5491100000004",
		"Can machines think? I keep going back and forth.",
	)

	seed("chat-group", "5491100000002",
		"Anyone up for reviewing the release notes before Friday?",
	)
	seed("chat-group", "5491100000003",
		"I can take the migration section.",
	)
	seed("chat-group", selfID,
		"I will do the keybindings table.",
	)

	// Long and multi-line, to exercise wrapping.
	f.Receive("chat-grace", "5491100000003",
		"Long one so you can check the wrapping behaviour in the bubble:\n\n"+
			"The transcript wraps on word boundaries, and breaks overlong words "+
			"mid-word rather than letting them overflow the pane, because a URL "+
			"is longer than any terminal and the alternative is a corrupted layout.")

	return f
}
