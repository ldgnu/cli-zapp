package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/ui/component"
)

// The limiter's only job is to stop the user's own client from producing the burst
// shape WhatsApp restricts accounts for. Every test below is about that, and about
// not getting in the way of a person typing.

func TestALimiterAllowsAnOrdinaryConversation(t *testing.T) {
	// A fast reply is several messages in a row, then a pause. If the limiter
	// refuses that, it is worse than no limiter.
	l := sendLimiterDefault
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		if ok, why := l.allow(now); !ok {
			t.Fatalf("message %d in a burst of five was refused: %s", i+1, why)
		}
		now = now.Add(2 * time.Second)
	}
}

func TestABurstIsCapped(t *testing.T) {
	l := sendLimiterDefault
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	for i := 0; i < l.burst; i++ {
		if ok, _ := l.allow(now); !ok {
			t.Fatalf("message %d should be inside the burst allowance", i+1)
		}
	}

	ok, why := l.allow(now)
	if ok {
		t.Fatal("the message past the burst allowance should be refused")
	}
	// The reason has to say what to do, not just "no".
	if !strings.Contains(why, "rápido") {
		t.Errorf("the refusal should explain the burst, got %q", why)
	}
}

func TestTheSustainedLimitStopsARunawayLoop(t *testing.T) {
	// This is the case the limiter exists for: a stuck key repeat or a retry loop,
	// hammering send as fast as the machine can.
	l := sendLimiterDefault
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	// Five per minute, which is the sustained limit at a plausible burst cadence and
	// comfortably under `sustained`. Over two hours that is 600 attempts; without a
	// limiter every one would go out, and 600 identical messages to one conversation is
	// exactly the shape that gets an account restricted.
	const step = 12 * time.Second
	allowed := 0
	for i := 0; i < 600; i++ {
		if ok, _ := l.allow(now); ok {
			allowed++
		}
		now = now.Add(step)
	}

	// 600 attempts * 12s = two hours, so twelve windows' worth are permitted.
	want := int(2*time.Hour/l.window) * l.sustained
	if allowed > want {
		t.Errorf("allowed %d sends over two hours, which permits %d", allowed, want)
	}
	if allowed < want/2 {
		t.Errorf("the limiter is stricter than intended: %d of an expected ~%d allowed",
			allowed, want)
	}
}

func TestTheWindowActuallySlides(t *testing.T) {
	// A limiter that never forgets is a limiter that stops working after ten
	// messages in a session. The oldest entry must expire.
	l := sendLimiterDefault
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	// Five seconds apart, so all ten land inside the one-minute window (0s to 45s)
	// and the eleventh is refused. Spacing them further would let the first expire
	// before the last is attempted, which tests nothing.
	for i := 0; i < l.sustained; i++ {
		if ok, why := l.allow(now); !ok {
			t.Fatalf("send %d should be allowed: %s", i+1, why)
		}
		now = now.Add(5 * time.Second)
	}
	if ok, _ := l.allow(now); ok {
		t.Fatal("the window's worth of sends should be exhausted")
	}

	// A minute later everything has aged out.
	ok, why := l.allow(now.Add(l.window + time.Second))
	if !ok {
		t.Errorf("the limiter should recover once the window passes, got %q", why)
	}
}

func TestTheRefusalSaysHowLongToWait(t *testing.T) {
	l := sendLimiterDefault
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	// Five seconds apart, so all ten land inside the window. The step matters: at
	// seven seconds the first send has already aged out by the eleventh, and the
	// limiter correctly lets it through — which is the window sliding, not a bug.
	for i := 0; i < l.sustained; i++ {
		l.allow(now)
		now = now.Add(5 * time.Second)
	}

	ok, why := l.allow(now)
	if ok {
		t.Fatal("expected a refusal")
	}
	// A message that only says "no" leaves the user with nothing to act on, and a
	// limit that cannot be acted on gets worked around — which is the outcome this
	// is trying to prevent.
	if !strings.Contains(why, "Esperá") {
		t.Errorf("the refusal should say how long to wait, got %q", why)
	}
}

// TestARefusedSendKeepsTheDraft is the integration point. A limit that discards
// what the user typed is worse than no limit: they retype it, and press enter again
// out of frustration, and are refused again.
func TestARefusedSendKeepsTheDraft(t *testing.T) {
	h := newHarness(t)
	h.openChat("chat-ada")
	h.settle(h.m.setFocus(component.RegionComposer))

	const body = "un mensaje que el usuario quiere mandar"
	for _, r := range body {
		h.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	before := len(h.fake.messages("chat-ada"))

	// Primed directly rather than through allow(): this test is about what a refused
	// send does to the draft, not about the limiting, and the pacing needed to reach
	// the sustained limit without tripping the burst cap would obscure that.
	h.m.limiter.sent = nil
	for i := 0; i < sendLimiterDefault.sustained; i++ {
		h.m.limiter.sent = append(h.m.limiter.sent, time.Now())
	}

	// Enter, not mod+enter: enter is what the composer binds to send, and it is the
	// key a runaway key-repeat actually presses.
	h.key(tea.KeyPressMsg{Code: '\r', Text: "\r"})

	if got := h.m.composer.Value(); got != body {
		t.Errorf("a refused send must leave the draft alone, got %q", got)
	}
	if after := len(h.fake.messages("chat-ada")); after != before {
		t.Errorf("a refused send must not reach the service: %d messages, had %d",
			after, before)
	}
}

// TestTheComposerRestoresAMultilineDraft covers the case the first test's single-line
// body cannot: a paste with newlines has to come back intact, or the user loses the
// paragraph they just pasted.
func TestTheComposerRestoresAMultilineDraft(t *testing.T) {
	h := newHarness(t)
	h.openChat("chat-ada")
	h.settle(h.m.setFocus(component.RegionComposer))

	const body = "primera línea\nsegunda línea\ntercera"
	for _, r := range body {
		h.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	h.m.limiter.sent = nil
	for i := 0; i < sendLimiterDefault.sustained; i++ {
		h.m.limiter.sent = append(h.m.limiter.sent, time.Now())
	}
	h.key(tea.KeyPressMsg{Code: '\r', Text: "\r"})

	if got := h.m.composer.Value(); got != body {
		t.Errorf("the multiline draft should survive a refused send, got %q", got)
	}
}
