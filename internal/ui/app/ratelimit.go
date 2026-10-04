package app

import (
	"fmt"
	"time"
)

// sendLimiter caps how fast messages leave the client.
//
// # Why this exists
//
// WhatsApp bans accounts for bulk or automated messaging. That is a real and
// separately-enforced rule from the "unofficial client" one: a client can be
// entirely innocent of automation and still get an account restricted for sending
// the same body to fifty people in a minute.
//
// This limiter exists so that the *user's own client* cannot do that to them. The
// realistic failure it prevents is not a user spamming deliberately — it is a bug.
// A stuck key repeat, a paste of a long list into the composer followed by a
// repeated `enter`, a retry loop that re-sends on failure, a selection action
// dispatched N times by a double keypress. Each of those produces a burst of
// identical messages, which is precisely the shape WhatsApp flags.
//
// A human cannot do this by accident. A client can.
//
// # What this is not
//
// It is not detection evasion. There is no timing jitter to look human, no typing
// simulation, no warm-up ramp and no proxy rotation, because those are techniques
// for avoiding enforcement rather than for avoiding a defect. What it does is keep
// the client under a rate a person could plausibly have produced.
//
// The limits are deliberately far below WhatsApp's own throughput limits. Those
// limits are tuned for businesses sending thousands of messages a day to opted-in
// customers; a person reading and replying does not come close, and there is no
// reason for a chat client to approach them.
//
// # The limits
//
// Bursts are allowed because a fast typist genuinely sends several messages in a
// row, and a hard "one every N seconds" would be unusable. What is bounded is the
// sustained rate, and the total inside a rolling window — which is the number that
// actually gets an account restricted.
type sendLimiter struct {
	// burst is how many messages may go out back to back.
	burst int
	// window is the rolling period the sustained rate is measured over.
	window time.Duration
	// sustained is the most messages permitted per window.
	sustained int

	// sent holds the time of each recent send, oldest first.
	sent []time.Time
}

// sendLimiterDefault is the limit for interactive use.
//
// Ten per minute, six in a row. A person replying in a fast conversation sends a
// handful per minute; a burst of six covers "paste, enter, paste, enter" without
// feeling throttled. A runaway loop hits the sustained limit inside about fifteen
// seconds and is stopped.
var sendLimiterDefault = sendLimiter{
	burst:     6,
	window:    time.Minute,
	sustained: 10,
}

// allow reports whether another message may be sent now, and why not when it may
// not.
//
// now is a parameter rather than read from the clock so the behaviour is testable
// without sleeping — a limiter whose only test is "wait a minute and check" is a
// limiter that gets tested once and then trusted.
func (l *sendLimiter) allow(now time.Time) (bool, string) {
	l.prune(now)

	// Sustained rate first: it is the one that gets an account restricted, so a
	// client that has already hit ten in this minute should not be allowed to spend
	// its burst allowance on top of that.
	//
	// The error case is guarded rather than handled: record() appends unconditionally
	// and lets prune() drop what has aged out, so a slice longer than the sustained
	// limit is representable. Counting only what is still inside the window is what
	// makes the limit mean "per minute" rather than "per session" — the earlier
	// version compared against the raw slice length and, because the slice was capped
	// by pruning rather than by the limit, allowed 100 sends in a minute.
	n := 0
	for _, t := range l.sent {
		if t.After(now.Add(-l.window)) {
			n++
		}
	}
	if n >= l.sustained {
		return false, fmt.Sprintf(
			"límite de envío: %d mensajes por %s. Esperá %s.",
			l.sustained, humanWindow(l.window), shortWait(now, l.oldest(), l.window))
	}

	// Burst: too many too fast, even though the window total is fine.
	if l.inBurst(now) >= l.burst {
		return false, fmt.Sprintf(
			"envías demasiado rápido. Esperá %s.",
			shortWait(now, l.sent[len(l.sent)-l.burst], l.burstWindow()))
	}

	l.sent = append(l.sent, now)
	return true, ""
}

// record counts a send without enforcing the limit.
//
// Used for sends that are not the user's keystrokes — a retry, or a queued
// message draining on connect. Those are already in flight; refusing them would
// strand a message the user believes was sent.
func (l *sendLimiter) record(now time.Time) {
	l.sent = append(l.sent, now)
	l.prune(now)
}

// inBurst counts the sends inside the burst window ending at now.
func (l *sendLimiter) inBurst(now time.Time) int {
	// The burst window is derived rather than stored: the burst allowance exists to
	// let a fast typist through, so it has to be a short, human-timescale window.
	cutoff := now.Add(-l.burstWindow())
	n := 0
	for _, t := range l.sent {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

// burstWindow is how wide the burst allowance is.
//
// A tenth of the sustained window: for a one-minute window that is six seconds,
// which covers "paste, enter, paste, enter" without covering a runaway loop.
func (l *sendLimiter) burstWindow() time.Duration {
	return l.window / 10
}

// prune drops entries that have fallen out of the window.
//
// The slice is bounded by `sustained`, so this is O(n) with a small n and there is
// no need for anything cleverer.
func (l *sendLimiter) prune(now time.Time) {
	cutoff := now.Add(-l.window)
	keep := l.sent[:0]
	for _, t := range l.sent {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	l.sent = keep
}

// oldest returns the earliest send still inside the window, or now if there is
// none. Used only to say how long to wait.
func (l *sendLimiter) oldest() time.Time {
	if len(l.sent) == 0 {
		return time.Time{}
	}
	return l.sent[0]
}

// shortWait returns how long until `from` falls outside a window of length window.
//
// The wait is rounded up to a whole second and reported as zero rather than a
// negative duration when the time has already passed. A limit that says "0s" and
// then refuses again is worse than one that is slightly generous: the user is told
// to wait, waits zero seconds, and concludes the message is broken.
func shortWait(now, from time.Time, window time.Duration) time.Duration {
	d := window - now.Sub(from)
	if d <= 0 {
		return 0
	}
	// Round up, so the reported wait is never short of the real one.
	return (d + time.Second - 1) / time.Second * time.Second
}

// humanWindow renders a window for a message: 1m, not 1m0s.
func humanWindow(d time.Duration) string {
	if d%time.Minute == 0 {
		return fmt.Sprintf("%d min", int(d/time.Minute))
	}
	return d.Round(time.Second).String()
}
