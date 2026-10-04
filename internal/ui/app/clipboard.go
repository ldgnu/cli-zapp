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

// copySelected puts messages on the system clipboard.
//
// # Why not OSC 52
//
// OSC 52 is the portable way to set a terminal's clipboard: it works over SSH, in
// every emulator, and needs no helper binary. It is not used here because the sequence
// has to be written straight to the terminal's output stream mid-render, which fights
// Bubble Tea's renderer for the tty. Bubbles' clipboard support does exactly that dance
// internally, and adopting it would mean giving up the declarative View this design
// rests on.
//
// So an external helper is tried instead, in the conventional order, and the user is
// told when none is present. Silently doing nothing is worse: a user who pastes stale
// content concludes the copy is broken and stops using it.
func (m *Model) copySelected(ids []models.MessageID) tea.Cmd {
	var parts []string
	for _, msg := range m.messages {
		if !containsID(ids, msg.ID) {
			continue
		}
		// Sanitised, because a message body carrying escape sequences would inject
		// them into whatever the user pastes into — including a shell.
		parts = append(parts, text.Sanitize(msg.Text()))
	}

	if len(parts) == 0 {
		return m.toast("nada que copiar", false)
	}

	body := strings.Join(parts, "\n\n")

	ctx, cancel := callCtx(requestTimeout)
	defer cancel()

	if err := copyToClipboard(ctx, body); err != nil {
		return m.toast("no se pudo copiar: "+err.Error(), true)
	}
	return m.toast("copiado "+plural(len(parts), "mensaje"), false)
}

// copyToClipboard writes s to the system clipboard using the first helper that works.
//
// The candidate order follows what each tool is actually good at: wl-copy and xclip for
// X11, pbcopy on macOS, clip.exe on Windows.
func copyToClipboard(ctx context.Context, s string) error {
	if _, ok := os.LookupEnv("WAYLAND_DISPLAY"); ok {
		if err := runHelper(ctx, "wl-copy", nil, s); err == nil {
			return nil
		}
	}
	if os.Getenv("DISPLAY") != "" {
		if err := runHelper(ctx, "xclip", []string{"-selection", "clipboard"}, s); err == nil {
			return nil
		}
		if err := runHelper(ctx, "xsel", []string{"--clipboard", "--input"}, s); err == nil {
			return nil
		}
	}

	switch runtime.GOOS {
	case "darwin":
		if err := runHelper(ctx, "pbcopy", nil, s); err == nil {
			return nil
		}
	case "windows":
		if err := runHelper(ctx, "clip.exe", nil, s); err == nil {
			return nil
		}
	}

	return errNoClipboard
}

// runHelper executes a clipboard helper, passing body on stdin.
//
// The body goes on stdin rather than as an argument because a message can be far
// larger than the shell's argument limit, and every helper reads stdin by convention.
func runHelper(ctx context.Context, name string, args []string, body string) error {
	// Bound to the caller's context: a wedged clipboard helper must not hang the
	// caller for ever, because the caller is the UI.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(body)

	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = err.Error()
		}
		return &clipboardError{name: name, msg: msg}
	}
	return nil
}

// errNoClipboard reports that no clipboard helper is available.
var errNoClipboard = &clipboardError{
	name: "portapapeles",
	msg:  "no se encontró ningún ayudante (instala wl-clipboard, xclip o xsel)",
}

// clipboardError names the helper that failed.
//
// The name matters because the fix differs per tool: the user has to know whether to
// install wl-clipboard or xclip, and a generic "copy failed" does not tell them.
type clipboardError struct {
	name string
	msg  string
}

func (e *clipboardError) Error() string { return e.name + ": " + e.msg }

// containsID reports whether a slice holds an identifier.
func containsID(ids []models.MessageID, id models.MessageID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
