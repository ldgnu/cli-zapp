package statusbar_test

import (
	"strings"
	"testing"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/text"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/statusbar"
	"github.com/cli-zapp/cli-zapp/internal/ui/layout"
	"github.com/cli-zapp/cli-zapp/internal/ui/theme"
)

// build returns a status bar of the given width in the given mode.
func build(w int, mode layout.Mode) *statusbar.Model {
	b := statusbar.New(theme.Dark())
	b.SetMode(mode)
	b.Resize(layout.Rect{Width: w, Height: 1})
	return b
}

// render returns the bar's single row with styling stripped.
func render(b *statusbar.Model) string {
	rows := strings.Split(b.View(), "\n")
	if len(rows) != 1 {
		return strings.Join(rows, "\n")
	}
	return text.StripANSI(rows[0])
}

// --- shape ---

func TestBarIsExactlyOneRow(t *testing.T) {
	// The bar shares its row with the frame's bottom edge. A second row would push the
	// whole interface up by one, which is visible as a jump on every resize.
	b := build(80, layout.ModeFull)
	if n := len(strings.Split(b.View(), "\n")); n != 1 {
		t.Errorf("the bar produced %d rows", n)
	}
}

func TestBarIsExactlyAsWideAsItsRectangle(t *testing.T) {
	for _, w := range []int{20, 40, 80, 200} {
		if got := text.VisibleWidth(render(build(w, layout.ModeFull))); got != w {
			t.Errorf("at width %d the bar is %d cells", w, got)
		}
	}
}

func TestEmptyBarRendersNothing(t *testing.T) {
	if got := build(0, layout.ModeFull).View(); got != "" {
		t.Errorf("a zero-width bar should render nothing, got %q", got)
	}
}

// --- content ---

func TestConnectionStateIsReported(t *testing.T) {
	tests := map[statusbar.Connection]string{
		statusbar.Online:     "en línea",
		statusbar.Connecting: "conectando",
		statusbar.Syncing:    "sincronizando",
		statusbar.Offline:    "sin conexión",
	}

	for conn, want := range tests {
		b := build(80, layout.ModeFull)
		b.SetState(statusbar.State{Connection: conn})
		if got := render(b); !strings.Contains(got, want) {
			t.Errorf("connection %v: want %q in %q", conn, want, got)
		}
	}
}

func TestUnreadCountIsReported(t *testing.T) {
	b := build(80, layout.ModeFull)
	b.SetState(statusbar.State{Connection: statusbar.Online, Unread: 7})

	if got := render(b); !strings.Contains(got, "7") || !strings.Contains(got, "sin leer") {
		t.Errorf("the unread count should be visible, got %q", got)
	}
}

func TestNoUnreadCountIsNotMentioned(t *testing.T) {
	// "0 sin leer" is noise. The absence of a number is the information.
	b := build(80, layout.ModeFull)
	b.SetState(statusbar.State{Connection: statusbar.Online})

	if got := render(b); strings.Contains(got, "sin leer") {
		t.Errorf("nothing unread should say nothing, got %q", got)
	}
}

func TestTypingIsReported(t *testing.T) {
	b := build(80, layout.ModeFull)
	b.SetState(statusbar.State{Connection: statusbar.Online, Typing: "Ada"})

	if got := render(b); !strings.Contains(got, "escribiendo") {
		t.Errorf("a composing peer should be reported, got %q", got)
	}
}

func TestChatNameIsReported(t *testing.T) {
	b := build(80, layout.ModeFull)
	b.SetState(statusbar.State{Connection: statusbar.Online, ChatName: "Ada Lovelace"})

	if got := render(b); !strings.Contains(got, "Ada") {
		t.Errorf("the open conversation should be named, got %q", got)
	}
}

// --- hints ---

func TestHintsAreRenderedRightAligned(t *testing.T) {
	b := build(100, layout.ModeFull)
	b.SetState(statusbar.State{
		Connection: statusbar.Online,
		Hints: []keybindings.HelpEntry{
			{Action: keybindings.ActionQuit, Keys: []string{"ctrl+q"}, Help: "Salir"},
			{Action: keybindings.ActionHelp, Keys: []string{"ctrl+?"}, Help: "Ayuda"},
		},
	})

	got := render(b)
	if !strings.Contains(got, "Salir") || !strings.Contains(got, "Ayuda") {
		t.Fatalf("the hints should be rendered, got %q", got)
	}
	// Right-aligned means the context is on the left and the hints are not.
	if strings.Index(got, "Salir") < strings.Index(got, "en línea") {
		t.Errorf("the hints should follow the context, got %q", got)
	}
}

func TestHintsAreDroppedFromTheRightWhenNarrow(t *testing.T) {
	// Hints are the most expendable thing on the bar: they are in the help sheet and in
	// the README. The connection state is not, because a user who cannot see whether
	// they are connected cannot tell a silent client from a working one.
	many := []keybindings.HelpEntry{
		{Action: keybindings.ActionQuit, Keys: []string{"ctrl+q"}, Help: "Salir"},
		{Action: keybindings.ActionHelp, Keys: []string{"ctrl+?"}, Help: "Ayuda"},
		{Action: keybindings.ActionSearch, Keys: []string{"ctrl+f"}, Help: "Buscar"},
		{Action: keybindings.ActionPalette, Keys: []string{"ctrl+shift+p"}, Help: "Comandos"},
	}

	for _, w := range []int{20, 30, 45, 60} {
		b := build(w, layout.ModeFull)
		b.SetState(statusbar.State{Connection: statusbar.Offline, Hints: many})

		got := render(b)
		if !strings.Contains(got, "sin conexión") {
			t.Errorf("at width %d the connection state must survive, got %q", w, got)
		}
		if text.VisibleWidth(got) != w {
			t.Errorf("at width %d the bar is %d cells", w, text.VisibleWidth(got))
		}
	}
}

func TestMinimalModeDropsTheHints(t *testing.T) {
	b := build(100, layout.ModeMinimal)
	b.SetState(statusbar.State{
		Connection: statusbar.Online,
		Hints: []keybindings.HelpEntry{
			{Action: keybindings.ActionQuit, Keys: []string{"ctrl+q"}, Help: "Salir"},
		},
	})

	if got := render(b); strings.Contains(got, "Salir") {
		t.Errorf("minimal mode should not spend rows on hints, got %q", got)
	}
}

func TestHintWithoutKeysIsSkipped(t *testing.T) {
	b := build(80, layout.ModeFull)
	b.SetState(statusbar.State{
		Connection: statusbar.Online,
		Hints: []keybindings.HelpEntry{
			{Action: keybindings.ActionQuit, Help: "Salir"},
		},
	})

	if got := render(b); strings.Contains(got, "Salir") {
		t.Errorf("a hint with no key cannot be shown, got %q", got)
	}
}

// --- strings ---

func TestConnectionString(t *testing.T) {
	for conn, want := range map[statusbar.Connection]string{
		statusbar.Online:         "en línea",
		statusbar.Connecting:     "conectando",
		statusbar.Syncing:        "sincronizando",
		statusbar.Offline:        "sin conexión",
		statusbar.Connection(99): "desconocido",
	} {
		if got := conn.String(); got != want {
			t.Errorf("Connection(%d) = %q, want %q", int(conn), got, want)
		}
	}
}

// TestNothingIsEverOverflowed runs every combination of content at every width, which is
// where the trimming loops in View are exercised.
func TestNothingIsEverOverflowed(t *testing.T) {
	contents := []statusbar.State{
		{},
		{Connection: statusbar.Offline},
		{Connection: statusbar.Online, ChatName: strings.Repeat("nombre muy largo ", 5)},
		{Connection: statusbar.Online, Typing: "Ada", Unread: 1234},
		{
			Connection: statusbar.Syncing, ChatName: "Familia", Typing: "Grace",
			Unread: 99, Hints: []keybindings.HelpEntry{
				{Action: keybindings.ActionQuit, Keys: []string{"ctrl+q"}, Help: "Salir"},
			},
		},
	}

	for w := 1; w <= 200; w++ {
		for i, state := range contents {
			b := build(w, layout.ModeFull)
			b.SetState(state)

			got := text.StripANSI(b.View())
			if width := text.VisibleWidth(got); width != w {
				t.Fatalf("content %d at width %d rendered %d cells: %q", i, w, width, got)
			}
		}
	}
}
