package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/log/v2"
)

func TestNoopDiscardsAndNeverPanics(t *testing.T) {
	// The no-op logger is what production uses when logging is not requested, so
	// every call site runs through it by default.
	l := Noop()

	l.Debug("debug", "k", "v")
	l.Info("info")
	l.Warn("warn")
	l.Error("error")

	if got := l.Path(); got != "" {
		t.Errorf("Path on a no-op logger = %q, want empty", got)
	}
	l.close()
}

func TestNilLoggerIsSafe(t *testing.T) {
	// A nil *Logger must not panic: call sites would otherwise need a guard
	// everywhere, and a logger that can crash the UI is worse than no logger.
	var l *Logger

	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")

	if got := l.Path(); got != "" {
		t.Errorf("Path on a nil logger = %q", got)
	}
	l.close()
}

func TestNewWritesToTheGivenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wterm.log")

	l, closeLog, err := New(Options{Verbose: true, File: path})
	if err != nil {
		t.Fatal(err)
	}
	defer closeLog()

	l.Info("a message", "key", "value")

	if got := l.Path(); got != path {
		t.Errorf("Path = %q, want %q", got, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "a message") {
		t.Errorf("the message was not written: %q", data)
	}
	if !strings.Contains(string(data), "key=value") {
		t.Errorf("structured context was not written: %q", data)
	}
}

func TestLogFileIsNotWorldReadable(t *testing.T) {
	// The log holds protocol diagnostics and message metadata, which on a shared
	// machine must not be readable by other users.
	path := filepath.Join(t.TempDir(), "wterm.log")

	l, closeLog, err := New(Options{Debug: true, File: path})
	if err != nil {
		t.Fatal(err)
	}
	l.Info("secret-adjacent detail")
	closeLog()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
}

func TestTemporaryFileIsUsedByDefault(t *testing.T) {
	// Silently discarding logs the user asked for would be worse than leaving them
	// somewhere they have to find.
	l, closeLog, err := New(Options{Verbose: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeLog()

	path := l.Path()
	if path == "" {
		t.Fatal("expected a temporary log file path")
	}
	if !strings.Contains(path, "wterm-") {
		t.Errorf("unexpected temporary path %q", path)
	}

	l.Info("written to a temp file")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the temporary log was not created: %v", err)
	}
}

func TestUnopenableFileIsReported(t *testing.T) {
	// A path that cannot be opened is an error the user deserves to hear about,
	// rather than a silent no-op.
	if _, _, err := New(Options{File: "/nonexistent-directory/wterm.log"}); err == nil {
		t.Error("expected an error for an unopenable path")
	}
}

func TestLevelSelection(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want log.Level
	}{
		{"debug", Options{Debug: true}, log.DebugLevel},
		{"verbose", Options{Verbose: true}, log.InfoLevel},
		// Warnings and errors are recorded even without a flag: they are rare and
		// they are what a user pastes into a bug report.
		{"default", Options{}, log.WarnLevel},
		{"debug wins over verbose", Options{Debug: true, Verbose: true}, log.DebugLevel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := level(tc.opts); got != tc.want {
				t.Errorf("level = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDefaultLevelSuppressesInfo(t *testing.T) {
	// Without --debug or --verbose, informational output must not reach the file,
	// or the "opt-in" promise is a lie.
	path := filepath.Join(t.TempDir(), "wterm.log")

	l, closeLog, err := New(Options{File: path})
	if err != nil {
		t.Fatal(err)
	}
	l.Info("should be suppressed")
	l.Warn("should be recorded")
	closeLog()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "should be suppressed") {
		t.Error("info was recorded without --verbose")
	}
	if !strings.Contains(string(data), "should be recorded") {
		t.Error("a warning should always be recorded")
	}
}

func TestDefaultPath(t *testing.T) {
	path, err := DefaultPath("wterm.db")
	if err != nil {
		t.Skipf("no config directory available: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join("wterm", "wterm.db")) {
		t.Errorf("DefaultPath = %q", path)
	}
	if filepath.Base(filepath.Dir(path)) != "wterm" {
		t.Errorf("expected wterm's own subdirectory, got %q", path)
	}
}

func TestEnsureDirIsRestrictive(t *testing.T) {
	// This directory holds the linked-session state and must not be traversable by
	// other users.
	base := t.TempDir()
	dir := filepath.Join(base, "state")

	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("permissions = %o, want 700", perm)
	}
}

func TestEnsureDirIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	for range 3 {
		if err := EnsureDir(dir); err != nil {
			t.Fatalf("call failed: %v", err)
		}
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	_, closeLog, err := New(Options{File: filepath.Join(t.TempDir(), "x.log")})
	if err != nil {
		t.Fatal(err)
	}
	closeLog()
	closeLog() // must not panic
}
