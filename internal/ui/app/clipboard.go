package app

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/wterm/wterm/internal/models"
	"github.com/wterm/wterm/internal/text"
)

// copySelected puts the selection on the system clipboard.
//
// # Why not the OSC 52 escape
//
// OSC 52 is the portable way to set a terminal's clipboard, and it works
// everywhere including over SSH. It is not used here because the sequence has to
// be written straight to the terminal's output stream, mid-render, which fights
// Bubble Tea's renderer for the tty. Bubbles' clipboard support does exactly
// that dance internally.
//
// Instead an external helper is tried, in the conventional order, and the user is
// told when none is present. The alternative — silently doing nothing — is how a
// user ends up pasting stale content and believing the copy was broken.
func (m *Model) copySelected() tea.Cmd {
	ids := m.selectionOrCursor()

	msgs := m.currentMessages()
	var parts []string
	for _, msg := range msgs {
		if !containsID(ids, msg.ID) {
			continue
		}
		// Sanitize: copying a message body that contains escape sequences into a
		// clipboard would inject them into whatever the user pastes into.
		parts = append(parts, text.Sanitize(msg.Text()))
	}

	if len(parts) == 0 {
		return m.toast("Nothing to copy", false)
	}

	body := strings.Join(parts, "\n\n")
	if err := copyToClipboard(context.Background(), body); err != nil {
		return m.toast("Copy failed: "+err.Error(), true)
	}

	return m.toast("Copied "+plural(len(parts), "message"), false)
}

// selectionOrCursor returns the selection, falling back to the cursor message.
func (m *Model) selectionOrCursor() []models.MessageID {
	if ids := sortedSelectedIDs(m.selected); len(ids) > 0 {
		return ids
	}
	if id := m.cursorID(); id != "" {
		return []models.MessageID{id}
	}
	return nil
}

func containsID(ids []models.MessageID, id models.MessageID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// copyToClipboard writes s to the system clipboard using the first available
// helper.
//
// The candidate order follows what each tool is actually good at: wl-copy and
// xclip for X11, pbcopy on macOS, clip.exe and powershell.exe on Windows.
func copyToClipboard(ctx context.Context, s string) error {
	if _, ok := os.LookupEnv("WAYLAND_DISPLAY"); ok {
		if err := run(ctx, "wl-copy", nil, s); err == nil {
			return nil
		}
	}
	if os.Getenv("DISPLAY") != "" {
		if err := run(ctx, "xclip", []string{"-selection", "clipboard"}, s); err == nil {
			return nil
		}
		if err := run(ctx, "xsel", []string{"--clipboard", "--input"}, s); err == nil {
			return nil
		}
	}

	switch runtime.GOOS {
	case "darwin":
		if err := run(ctx, "pbcopy", nil, s); err == nil {
			return nil
		}
	case "windows":
		if err := run(ctx, "clip.exe", nil, s); err == nil {
			return nil
		}
	}

	return errNoClipboard
}

// run executes a clipboard helper, passing body on stdin.
//
// The body goes on stdin rather than as an argument: a message can be far larger
// than the shell's argument limit, and every helper reads stdin by convention.
func run(ctx context.Context, name string, args []string, body string) error {
	// Bound to the caller's context: a wedged clipboard helper must not hang the
	// caller for ever, because the caller is the UI.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(body)

	var b strings.Builder
	cmd.Stdout = &b
	cmd.Stderr = &b

	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(b.String()); msg != "" {
			return &clipboardError{name: name, msg: msg}
		}
		return &clipboardError{name: name, msg: err.Error()}
	}
	return nil
}

// errNoClipboard reports that no clipboard helper is available.
var errNoClipboard = &clipboardError{
	name: "clipboard",
	msg:  "no clipboard helper found (install wl-clipboard, xclip or xsel)",
}

// clipboardError names the helper that failed, because the fix differs per tool.
type clipboardError struct {
	name string
	msg  string
}

func (e *clipboardError) Error() string { return e.name + ": " + e.msg }

// plural renders a count with a correctly pluralised noun.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return text.Itoa(n) + " " + noun + "s"
}
