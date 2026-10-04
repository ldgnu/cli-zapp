// Command framedump renders cli-zapp's interface at a given size and prints it.
//
// # Why this exists
//
// A Bubble Tea application cannot be verified by rendering it in a test. The renderer
// negotiates capabilities with the terminal and blocks on answers it never receives when
// there is no tty on stdin, and it reports a 0x0 window as "too small". Driving a real
// pty fixes that but turns the output into a stream of partial repaints rather than a
// frame.
//
// This tool sits between the two: it builds the same model the program builds, feeds it
// the same messages, and prints View() — the exact string the renderer would send. What
// it shows is what a user sees.
//
// # It drives the model through its public surface
//
// The tool never reaches inside the app package. It runs the commands Update returns
// and feeds the results back, which is precisely what Bubble Tea's loop does, so a
// change that breaks the command/reply contract breaks this tool too.
//
// Usage:
//
//	framedump [cols] [rows] [key ...]
//
// Keys are terminal names (up, down, left, right, enter, esc, tab, pgup, pgdn,
// home, end, ctrl+x) or literal text.
package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/keybindings"
	"github.com/cli-zapp/cli-zapp/internal/ui/app"
	"github.com/cli-zapp/cli-zapp/internal/ui/theme"
)

// debug turns on a trace of every message the driver settles, for working out why a
// dump shows less than expected.
var debug = os.Getenv("FRAMEDUMP_DEBUG") != ""

func main() {
	cols, rows := 100, 30
	if len(os.Args) > 1 {
		n, err := strconv.Atoi(os.Args[1])
		if err != nil {
			usage()
		}
		cols = n
	}
	if len(os.Args) > 2 {
		n, err := strconv.Atoi(os.Args[2])
		if err != nil {
			usage()
		}
		rows = n
	}
	if cols < 1 || rows < 1 {
		usage()
	}

	m := app.New(app.DemoData().Services(), theme.Dark(), keybindings.DefaultMap())
	m.DisableTimers = true

	// A small event loop: run the commands Update returns, feed their results back, and
	// stop when nothing new arrives. This is Bubble Tea's own contract, and driving the
	// model this way means a broken contract shows up here rather than only in the
	// running program.
	drain(m, m.Init())
	// Init runs first, exactly as the program's own startup order does, so
	// the session is established and the conversation list fetched before the
	// first frame. A dump taken before that would show an empty application and
	// say nothing about whether the interface works.
	drive(m, tea.WindowSizeMsg{Width: cols, Height: rows})

	for _, token := range os.Args[3:] {
		send(m, token)
	}

	fmt.Print(m.View().Content)
}

// usage reports a bad invocation.
//
// The tool is run by hand from a shell, where a mistyped argument is likely and a
// zero-sized layout produces a confusing empty frame rather than an obvious failure.
func usage() {
	fmt.Fprintln(os.Stderr, "usage: framedump [cols] [rows] [key ...]")
	os.Exit(2)
}

// drain runs a command and applies everything it leads to.
func drain(m *app.Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if got := run(cmd); got != nil {
		for _, inner := range expand(got) {
			drive(m, inner)
		}
	}
}

// run executes one command with a deadline.
//
// A command that never returns is one waiting on a socket, which the in-memory fake has
// nothing to deliver on. The deadline keeps the tool from hanging on it, and it is why
// this is a development tool rather than part of the test suite: the suite already
// covers the same paths through the model's own message types.
func run(cmd tea.Cmd) tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case got := <-done:
		return got
	case <-time.After(200 * time.Millisecond):
		return nil
	}
}

// expand flattens the envelopes Bubble Tea wraps batches in.
//
// tea.Batch returns a command whose *result* is a [tea.BatchMsg] holding more commands,
// and the program's loop runs those. A driver that applies the envelope as if it were a
// result stops one level early — which looks like "the data never arrived" rather than
// like a driver bug, and is exactly the kind of thing that sends someone looking in the
// wrong package.
func expand(msg tea.Msg) []tea.Msg {
	switch v := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range v {
			if got := run(c); got != nil {
				out = append(out, expand(got)...)
			}
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

// drive applies a message and settles: every command it produced is run, and their
// results are applied in turn, until the model stops asking for work.
//
// The bound is a safety net rather than a design constraint: a model that never settles
// is a model that would spin in the running program, and this tool should say so rather
// than hang.
func drive(m *app.Model, msg tea.Msg) {
	pending := []tea.Msg{msg}

	for round := 0; round < 40 && len(pending) > 0; round++ {
		var next []tea.Msg
		var mu sync.Mutex

		var wg sync.WaitGroup
		for _, m2 := range pending {
			_, cmd := m.Update(m2)
			if cmd == nil {
				continue
			}
			wg.Add(1)
			go func(c tea.Cmd) {
				defer wg.Done()
				if got := run(c); got != nil {
					mu.Lock()
					next = append(next, expand(got)...)
					mu.Unlock()
				}
			}(cmd)
		}
		wg.Wait()

		if debug {
			for _, n := range next {
				fmt.Fprintf(os.Stderr, "settle: %T\n", n)
			}
		}
		sort.Slice(next, func(i, j int) bool {
			return fmt.Sprint(next[i]) < fmt.Sprint(next[j])
		})
		pending = next
	}
}

// keyFor builds the message a terminal would deliver for a token.
//
// A token longer than one character is not a key press but a word to type, so it is
// returned as a [word] and applied one character at a time by [send] — which is how a
// terminal delivers it and the only way to see the composed result.
func keyFor(token string) tea.Msg {
	switch token {
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdn":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: '\t', Text: "\t"}
	case "shift+tab":
		return tea.KeyPressMsg{Code: '\t', Text: "\t", Mod: tea.ModShift}
	case "enter":
		// The terminal sends a carriage return as text, which is the case
		// KeyPressMsg.String() and Keystroke() disagree about.
		return tea.KeyPressMsg{Code: '\r', Text: "\r"}
	case "ctrl+enter":
		return tea.KeyPressMsg{Code: '\r', Text: "\r", Mod: tea.ModCtrl}
	}

	if rest, ok := strings.CutPrefix(token, "ctrl+"); ok && len(rest) == 1 {
		return tea.KeyPressMsg{Code: rune(rest[0]), Text: rest, Mod: tea.ModCtrl}
	}

	return word(token)
}

// word is a token to be delivered as a sequence of printable key presses.
type word string

// send applies one token to the model.
func send(m *app.Model, token string) {
	if w, ok := keyFor(token).(word); ok {
		for _, r := range string(w) {
			drive(m, tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		return
	}
	drive(m, keyFor(token))
}
