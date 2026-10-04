// Package notifications raises alerts for events the user is not looking at.
//
// # Three channels, chosen by what the terminal can do
//
//   - the terminal bell, which works everywhere and can be silenced by the user
//   - a desktop notification through notify-send, for when cli-zapp is not focused
//   - the in-app visual badge, which is the only one that survives inside cli-zapp
//
// The desktop channel is optional by nature: notify-send may be absent, and the
// server it talks to may be unavailable. That is detected once at startup rather
// than probed on every notification, because a failed exec per message would be
// slow enough to notice.
//
// # Never blocking
//
// Every method is best-effort and returns nothing. A notification must never
// delay the UI or propagate an error into the render path.
package notifications

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Options configures a Notifier.
type Options struct {
	// Bell rings the terminal bell on a new message.
	Bell bool

	// Desktop sends a desktop notification via notify-send.
	Desktop bool

	// Available reports whether the desktop channel works. Callers obtain it from
	// [DesktopAvailable] rather than having the notifier shell out.
	Available bool

	// SuppressWhileFocused skips desktop notifications when cli-zapp has focus,
	// which is what any sane messenger does.
	SuppressWhileFocused bool

	// NotifyCommand overrides the desktop helper, for testing.
	NotifyCommand string
}

// Event is something worth telling the user about.
type Event struct {
	// Title is the headline, normally the sender's name.
	Title string
	// Body is the message preview.
	Body string
	// ChatID identifies the conversation, for de-duplication.
	ChatID string
}

// Notifier delivers alerts.
type Notifier struct {
	opts Options

	mu sync.Mutex
	// recent de-duplicates alerts per chat within the suppression window, so a
	// burst of messages from one chat produces one notification rather than ten.
	recent map[string]time.Time
	queue  []Event
}

// suppressWindow is how long a chat stays suppressed after notifying.
const suppressWindow = 30 * time.Second

// New builds a Notifier.
func New(opts Options) *Notifier {
	return &Notifier{opts: opts, recent: make(map[string]time.Time)}
}

// DesktopAvailable reports whether notify-send is present.
//
// It is a PATH lookup rather than a test execution, because running notify-send
// at startup would pop a notification the moment cli-zapp launches.
func DesktopAvailable() bool {
	_, err := exec.LookPath("notify-send")
	return err == nil
}

// Notify raises an alert for an event, unless it is a duplicate.
func (n *Notifier) Notify(e Event) {
	if n == nil {
		return
	}

	if e.ChatID != "" && n.suppressed(e.ChatID) {
		return
	}

	if n.opts.Bell {
		n.bell()
	}
	if n.opts.Desktop && n.opts.Available {
		n.desktop(e)
	}
}

// suppressed reports whether the chat was notified recently.
func (n *Notifier) suppressed(chatID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	if at, ok := n.recent[chatID]; ok && time.Since(at) < suppressWindow {
		return true
	}
	n.recent[chatID] = time.Now()

	// Keep the map from growing without bound over a long session.
	if len(n.recent) > 512 {
		for k, at := range n.recent {
			if time.Since(at) > suppressWindow {
				delete(n.recent, k)
			}
		}
	}
	return false
}

// bell writes the terminal bell.
//
// The bell is written to stderr rather than stdout: stdout belongs to the TUI,
// and writing to it directly would fight the renderer for the same fd.
func (n *Notifier) bell() {
	_, _ = os.Stderr.WriteString("\a")
}

// desktop sends a desktop notification.
func (n *Notifier) desktop(e Event) {
	cmdName := n.opts.NotifyCommand
	if cmdName == "" {
		cmdName = "notify-send"
	}

	// The context bounds how long the helper may run, so that a wedged
	// notification daemon cannot accumulate processes for the whole session.
	//
	// Its cancel must NOT be deferred here. exec.CommandContext kills the child the
	// moment its context becomes Done, and a deferred cancel fires as soon as this
	// function returns — which is immediately after Start. Every notification would
	// be killed before notify-send had written anything, and the failure looks like
	// "no notifications ever appear" rather than like a bug.
	//
	// Instead the cancellation moves to the reaping goroutine, which is the only
	// place that knows when the process is genuinely finished.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

	cmd := exec.CommandContext(ctx, cmdName,
		"--app-name=cli-zapp",
		"--urgency=normal",
		e.Title,
		e.Body,
	)
	// Inheriting the terminal's descriptors would corrupt a frame, and inheriting
	// os.Stdout after it has been reassigned would write into a test's capture
	// file. A notification daemon has no use for either stream.
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		cancel()
		return
	}

	// Reap asynchronously: waiting here would block the caller for as long as
	// notify-send takes to reach the notification daemon, which is exactly the
	// render path this package must never stall.
	go func() {
		defer cancel()
		_ = cmd.Wait()
	}()
}

// Flush delivers anything queued while the terminal was unavailable.
//
// The in-app badge is the channel that survives regardless, so this exists for
// the case where the desktop channel became available late.
func (n *Notifier) Flush() {
	if n == nil {
		return
	}
	n.mu.Lock()
	pending := n.queue
	n.queue = nil
	n.mu.Unlock()

	for _, e := range pending {
		n.Notify(e)
	}
}

// Queue stores an event for later delivery.
func (n *Notifier) Queue(e Event) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()

	// Bound the queue so a sync of thousands of messages cannot exhaust memory.
	const maxQueue = 100
	if len(n.queue) >= maxQueue {
		n.queue = n.queue[1:]
	}
	n.queue = append(n.queue, e)
}
