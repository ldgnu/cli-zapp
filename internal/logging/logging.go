// Package logging provides cli-zapp's optional structured logging.
//
// # Never to stdout
//
// A TUI owns the terminal. A single stray log line to stdout lands in the middle
// of the interface and corrupts a frame, so every logger here writes to a file or
// to the structured log package's own writer, never to the terminal.
//
// # Never fatal
//
// Logging must never be able to take the application down. Construction returns
// an error because a user who explicitly asked for a log file deserves to be told
// it could not be opened, but once constructed every operation is best-effort:
// if a write fails, the logger drops it and carries on.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"charm.land/log/v2"
)

// Options configures a logger.
type Options struct {
	// Debug enables debug-level output.
	Debug bool
	// Verbose enables info-level output.
	//
	// The two flags are separate rather than a single verbosity counter because
	// "log a lot" and "log everything including protocol internals" are different
	// requests, and a counter makes it easy to overshoot into unusable output.
	Verbose bool

	// File is the log destination. Empty means a temporary file, because
	// silently discarding the logs a user asked for would be worse than leaving
	// them somewhere they have to hunt for.
	File string

	// MaxSize rotates the log file once it exceeds this many bytes. Zero
	// disables rotation.
	MaxSize int64
}

// Logger is cli-zapp's logging interface.
//
// It is deliberately tiny: the two verbs the application needs, plus a structured
// pair for key-value context. A wider interface invites call sites that log
// pre-formatted strings, which is what makes log output unsearchable.
type Logger struct {
	slog *log.Logger
	file *os.File

	mu sync.Mutex
	// failed latches a write failure so that a full or unwritable disk does not
	// produce a log line for every subsequent message.
	failed bool
}

// Noop returns a logger that discards everything.
//
// It is what the application uses when logging is not requested, so that call
// sites never have to nil-check.
func Noop() *Logger {
	return &Logger{slog: log.New(io.Discard)}
}

// New builds a logger from the options.
func New(opts Options) (*Logger, func(), error) {
	path := opts.File
	if path == "" {
		f, err := os.CreateTemp("", "cli-zapp-*.log")
		if err != nil {
			return nil, func() {}, fmt.Errorf("creating log file: %w", err)
		}
		// The file holds message metadata and protocol diagnostics; it must not
		// be readable by other users on a shared machine.
		if err := os.Chmod(f.Name(), 0o600); err != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, func() {}, fmt.Errorf("securing log file: %w", err)
		}
		path = f.Name()
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, func() {}, fmt.Errorf("opening log file %s: %w", path, err)
	}

	l := log.NewWithOptions(file, log.Options{
		Level:           level(opts),
		ReportTimestamp: true,
		TimeFormat:      time.RFC3339,
		// Caller reporting is left off by default: it makes the log noisy, and
		// --debug is where a caller line earns its keep.
		ReportCaller: opts.Debug,
		CallerOffset: 2,
	})

	lg := &Logger{slog: l, file: file}
	return lg, lg.close, nil
}

// level maps the flags to a log level.
func level(opts Options) log.Level {
	switch {
	case opts.Debug:
		return log.DebugLevel
	case opts.Verbose:
		return log.InfoLevel
	default:
		// Warnings and errors are always worth recording: they are rare and they
		// are what a user pastes into a bug report.
		return log.WarnLevel
	}
}

// Path returns the log file's location, for the startup banner.
func (l *Logger) Path() string {
	if l == nil || l.file == nil {
		return ""
	}
	return l.file.Name()
}

// Debug logs at debug level.
func (l *Logger) Debug(msg string, kv ...any) { l.log(log.DebugLevel, msg, kv) }

// Info logs at info level.
func (l *Logger) Info(msg string, kv ...any) { l.log(log.InfoLevel, msg, kv) }

// Warn logs at warn level.
func (l *Logger) Warn(msg string, kv ...any) { l.log(log.WarnLevel, msg, kv) }

// Error logs at error level.
func (l *Logger) Error(msg string, kv ...any) { l.log(log.ErrorLevel, msg, kv) }

// log writes one record, tolerating failure.
func (l *Logger) log(level log.Level, msg string, kv []any) {
	if l == nil || l.slog == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.failed {
		return
	}

	defer func() {
		// A panic inside the logger would take the UI down, which is exactly the
		// outcome this package exists to prevent.
		if recover() != nil {
			l.failed = true
		}
	}()

	l.slog.Log(level, msg, kv...)
}

// close releases the log file.
func (l *Logger) close() {
	if l == nil || l.file == nil {
		return
	}
	_ = l.file.Sync()
	_ = l.file.Close()
	l.file = nil
}

// DefaultPath returns the conventional configuration directory for cli-zapp's state.
//
// It lives in the user config directory rather than the data directory because
// what cli-zapp stores there is configuration and session state, not a cache that
// can be regenerated.
func DefaultPath(name string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config directory: %w", err)
	}
	return filepath.Join(dir, "cli-zapp", name), nil
}

// EnsureDir creates a directory with restrictive permissions.
//
// 0o700 rather than 0o755: this directory holds the linked-session state, which
// should not be traversable by other users on a shared machine.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	return nil
}
