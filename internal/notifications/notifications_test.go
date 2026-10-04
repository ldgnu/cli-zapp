package notifications

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNilNotifierIsSafe(t *testing.T) {
	// Call sites should not need a guard, and a notifier that can panic is worse
	// than no notifier at all.
	var n *Notifier

	n.Notify(Event{Title: "t", Body: "b"})
	n.Queue(Event{Title: "t"})
	n.Flush()
}

func TestBellWritesToStderrOnly(t *testing.T) {
	// The TUI owns stdout. A notification written there would land in the middle
	// of a frame.
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	errOut := filepath.Join(dir, "err")

	restore := captureStdio(t, out, errOut)
	defer restore()

	New(Options{Bell: true}).Notify(Event{Title: "ping"})

	if data, _ := os.ReadFile(errOut); !strings.Contains(string(data), "\a") {
		t.Errorf("expected a bell on stderr, got %q", data)
	}
	if data, _ := os.ReadFile(out); len(data) != 0 {
		t.Errorf("stdout must stay clean, got %q", data)
	}
}

func TestDesktopNotificationUsesTheHelper(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "notify.out")

	// A stub that records its arguments, standing in for notify-send.
	stub := writeStub(t, dir, "notify-send", capture)

	n := New(Options{Desktop: true, Available: true, NotifyCommand: stub})
	n.Notify(Event{Title: "Ada", Body: "hello there", ChatID: "c1"})

	got := waitForFile(t, capture, 2*time.Second)
	for _, want := range []string{"Ada", "hello there"} {
		if !strings.Contains(got, want) {
			t.Errorf("notification missing %q: %q", want, got)
		}
	}
}

func TestDuplicateChatsAreSuppressed(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "notify.out")
	stub := writeStub(t, dir, "notify-send", capture)

	n := New(Options{Desktop: true, Available: true, NotifyCommand: stub})

	// A burst from one chat must produce one notification, not ten.
	for i := range 5 {
		n.Notify(Event{Title: "Ada", Body: string(rune('a' + i)), ChatID: "same"})
	}

	// A different chat is not suppressed.
	n.Notify(Event{Title: "Grace", Body: "hi", ChatID: "other"})

	time.Sleep(300 * time.Millisecond)

	// The helper is reaped on its own goroutine, so wait for the file to settle.
	data := waitForFile(t, capture, 2*time.Second)

	// The stub records one argument per line, so a notification's title and body
	// are consecutive entries. Counting titles is what the assertion is about.
	var titles int
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		switch line {
		case "Ada", "Grace":
			titles++
		}
	}
	if titles != 2 {
		t.Errorf("expected one notification per chat, got %d: %q", titles, data)
	}
}

func TestEventsWithoutAChatAreNotDeduplicated(t *testing.T) {
	// With no chat there is nothing to key the suppression on, so every event
	// must reach the user.
	n := New(Options{})
	for range 5 {
		n.Notify(Event{Title: "sync failed"})
	}
	n.mu.Lock()
	suppressed := len(n.recent)
	n.mu.Unlock()

	if suppressed != 0 {
		t.Errorf("chatless events should not be recorded for suppression, got %d", suppressed)
	}
}

func TestDesktopAvailableIsFalseWithoutTheHelper(t *testing.T) {
	// A PATH without notify-send must report unavailable rather than fail at the
	// first notification.
	t.Setenv("PATH", t.TempDir())
	if DesktopAvailable() {
		t.Error("expected false with no notify-send on PATH")
	}
}

func TestDesktopAvailableIsTrueWithTheHelper(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "notify-send", filepath.Join(dir, "out"))
	t.Setenv("PATH", dir)

	if !DesktopAvailable() {
		t.Error("expected true with notify-send on PATH")
	}
}

func TestQueueIsBounded(t *testing.T) {
	// A resync can deliver thousands of messages; the queue must not grow with it.
	n := New(Options{})
	for range 500 {
		n.Queue(Event{Title: "x"})
	}

	n.mu.Lock()
	size := len(n.queue)
	n.mu.Unlock()

	if size > 100 {
		t.Errorf("queue grew to %d entries, want it bounded", size)
	}
}

func TestFlushDrainsTheQueue(t *testing.T) {
	n := New(Options{})
	n.Queue(Event{Title: "a"})
	n.Queue(Event{Title: "b"})

	n.Flush()

	n.mu.Lock()
	size := len(n.queue)
	n.mu.Unlock()

	if size != 0 {
		t.Errorf("Flush left %d queued events", size)
	}
}

func TestConcurrentNotifyIsSafe(t *testing.T) {
	// Run under -race: notifications arrive from the event pump while the UI
	// renders.
	n := New(Options{Bell: false})

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			n.Notify(Event{Title: "t", ChatID: string(rune('a' + i%5))})
		}(i)
	}
	wg.Wait()
}

// waitForFile polls until path is non-empty, and returns its contents.
//
// The notification helper is spawned and reaped asynchronously, so a test cannot
// simply read the file once.
func waitForFile(t *testing.T, path string, within time.Duration) string {
	t.Helper()

	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			// A short settle, so a second notification is not missed mid-write.
			time.Sleep(100 * time.Millisecond)
			out, _ := os.ReadFile(path)
			return string(out)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("nothing was written to %s within %v", path, within)
	return ""
}

// captureStdio redirects os.Stdout and os.Stderr for the duration of a test.
func captureStdio(t *testing.T, out, errOut string) func() {
	t.Helper()

	o, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	e, err := os.Create(errOut)
	if err != nil {
		t.Fatal(err)
	}

	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = o, e

	return func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		_ = o.Close()
		_ = e.Close()
	}
}

// writeStub installs an executable that records its arguments to a file.
func writeStub(t *testing.T, dir, name string, capture string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\" >> " + capture + "; done\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
