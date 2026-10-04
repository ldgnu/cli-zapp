package models

import "time"

// FormatRelativeTime renders a timestamp relative to now, in the style people
// expect from a chat client.
//
// The thresholds follow the conventions of mainstream messengers:
//   - under a minute: "just now"
//   - under an hour: relative minutes
//   - same calendar day: local clock time
//   - this year: day and month
//   - otherwise: day, month and year
//
// now is a parameter rather than read from the clock so the function is
// deterministic and testable.
func FormatRelativeTime(ts time.Time) string {
	return formatRelativeTime(ts, time.Now())
}

func formatRelativeTime(ts, now time.Time) string {
	if ts.IsZero() {
		return ""
	}
	d := now.Sub(ts)

	// A timestamp in the future means clock skew between peers. Reporting a
	// negative age would be nonsense, so treat it as current.
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return plural(m, "minute") + " ago"
	}

	// Compare in the location of `now` so that "today" means the viewer's
	// today, not UTC's.
	tsLocal, nowLocal := ts.In(now.Location()), now.In(now.Location())
	y1, m1, d1 := tsLocal.Date()
	y2, m2, d2 := nowLocal.Date()
	_, _, _ = m1, m2, d2

	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return tsLocal.Format("15:04")
	case y1 == y2:
		return tsLocal.Format("Jan 2")
	default:
		return tsLocal.Format("Jan 2, 2006")
	}
}

// FormatChatTimestamp renders the timestamp shown in the chat list, which
// differs from [FormatRelativeTime] in that today collapses to the clock time
// and other days show a short date.
func FormatChatTimestamp(ts time.Time) string {
	return formatChatTimestampAt(ts, time.Now())
}

func formatChatTimestampAt(ts, now time.Time) string {
	if ts.IsZero() {
		return ""
	}
	if now.Sub(ts) < 0 {
		return ts.Format("15:04")
	}
	tsLocal, nowLocal := ts.In(now.Location()), now.In(now.Location())
	if tsLocal.Year() == nowLocal.Year() && tsLocal.YearDay() == nowLocal.YearDay() {
		return tsLocal.Format("15:04")
	}
	if tsLocal.Year() == nowLocal.Year() {
		return tsLocal.Format("Jan 2")
	}
	return tsLocal.Format("Jan 2, 2006")
}

// FormatMessageTimestamp renders the time shown beside a message bubble.
func FormatMessageTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Format("15:04")
}

func plural(n int, noun string) string {
	s := noun + "s"
	if n == 1 {
		return "1 " + noun
	}
	return itoa(n) + " " + s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// DayKey returns a sortable key that groups timestamps by calendar day in the
// given location. The message list uses it to insert date separators.
func DayKey(ts time.Time, loc *time.Location) string {
	return ts.In(loc).Format("2006-01-02")
}
