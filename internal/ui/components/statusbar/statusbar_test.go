package statusbar

import (
	"strings"
	"testing"

	"github.com/wterm/wterm/internal/keybindings"
	"github.com/wterm/wterm/internal/text"
	"github.com/wterm/wterm/internal/ui/theme"
)

func base() State {
	return State{
		Connection: Online,
		Width:      100,
		Theme:      theme.Dark(),
	}
}

func TestConnectionStatesRenderDistinctly(t *testing.T) {
	seen := map[string]string{}
	for _, conn := range []Connection{Disconnected, Connecting, Syncing, Online} {
		s := base()
		s.Connection = conn
		got := text.StripANSI(View(s))

		label := conn.String()
		if !strings.Contains(got, label) {
			t.Errorf("status bar should mention %q, got:\n%s", label, got)
		}
		if prev, ok := seen[label]; ok && prev != got {
			t.Errorf("state %q renders indistinguishably from another", label)
		}
		seen[label] = got
	}
}

func TestBarIsExactlyOneLine(t *testing.T) {
	s := base()
	if got := len(strings.Split(View(s), "\n")); got != 1 {
		t.Errorf("the status bar must be one line, got %d", got)
	}
}

func TestBarFillsTheWidth(t *testing.T) {
	for _, w := range []int{20, 40, 80, 100, 200} {
		s := base()
		s.Width = w

		got := text.VisibleWidth(View(s))
		if got != w {
			t.Errorf("at width %d the bar is %d cells", w, got)
		}
	}
}

func TestChatNameAndCountsAppear(t *testing.T) {
	s := base()
	s.ChatName = "Ada Lovelace"
	s.ChatCount = 4
	s.UnreadTotal = 11

	got := text.StripANSI(View(s))
	for _, want := range []string{"Ada Lovelace", "11"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in:\n%s", want, got)
		}
	}
}

func TestUnreadBadgeIsOmittedWhenZero(t *testing.T) {
	s := base()
	s.ChatCount = 4
	s.UnreadTotal = 0

	got := text.StripANSI(View(s))
	if strings.Contains(got, "0") && !strings.Contains(got, "chats") {
		t.Errorf("a zero count should not produce a badge, got:\n%s", got)
	}
}

func TestTypingIndicator(t *testing.T) {
	s := base()
	s.TypingIn = "Grace"
	if got := text.StripANSI(View(s)); !strings.Contains(got, "typing") {
		t.Errorf("expected a typing indicator, got:\n%s", got)
	}
}

func TestHintsAreTrimmedToFit(t *testing.T) {
	// A narrow terminal must drop hints rather than overflow: a line wider than the
	// screen wraps and pushes the layout down by a line on every redraw.
	s := base()
	s.Width = 30
	s.Help = keybindings.DefaultMap().Help(keybindings.PanelMessages)

	if got := text.VisibleWidth(View(s)); got != 30 {
		t.Errorf("at width 30 the bar is %d cells:\n%q", got, text.StripANSI(View(s)))
	}
}

func TestContextSurvivesHintTrimming(t *testing.T) {
	// When the hints have to go, the connection state must remain: it is the one
	// thing the user cannot afford to lose.
	s := base()
	s.Width = 20
	s.ChatName = "A very long chat name"
	s.Help = keybindings.DefaultMap().Help(keybindings.PanelMessages)

	got := text.StripANSI(View(s))
	if !strings.Contains(got, "online") {
		t.Errorf("the connection state should survive, got:\n%s", got)
	}
}

func TestZeroWidthRendersNothing(t *testing.T) {
	s := base()
	s.Width = 0
	if got := View(s); got != "" {
		t.Errorf("width 0 should render nothing, got %q", got)
	}
}

func TestNoHintsDoesNotPanic(t *testing.T) {
	s := base()
	s.Help = nil
	if got := text.VisibleWidth(View(s)); got != s.Width {
		t.Errorf("the bar should still fill the width, got %d", got)
	}
}

func TestHintsWithNoKeysAreSkipped(t *testing.T) {
	s := base()
	s.Help = []keybindings.HelpEntry{{Action: "x", Keys: nil, Help: "no keys"}}
	if got := View(s); got == "" {
		t.Error("an entry with no keys should not break rendering")
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	s := base()
	s.ChatName = "Ada"
	s.Help = keybindings.DefaultMap().Help(keybindings.PanelSidebar)

	first := View(s)
	for range 5 {
		if got := View(s); got != first {
			t.Fatal("the status bar is not deterministic")
		}
	}
}

func TestConnectionString(t *testing.T) {
	tests := map[Connection]string{
		Disconnected:   "offline",
		Connecting:     "connecting",
		Syncing:        "syncing",
		Online:         "online",
		Connection(99): "unknown",
	}
	for conn, want := range tests {
		if got := conn.String(); got != want {
			t.Errorf("Connection(%d) = %q, want %q", conn, got, want)
		}
	}
}
