// Package store opens the on-disk locations cli-zapp keeps.
//
// # Two stores, deliberately separate
//
// whatsmeow's device store holds cryptographic material: identity keys, prekeys,
// session state, and its schema belongs to the library. cli-zapp's own store holds
// application state: the message cache, chat metadata, UI preferences.
//
// Merging them would couple our migrations to an upstream schema that changes with
// every protocol update, so a whatsmeow release could require a migration here. The
// split costs one extra file on disk and removes that coupling entirely.
//
// # Why SQLite, and why this driver
//
// modernc.org/sqlite is a pure-Go translation of SQLite. That is not a
// free choice: it is what keeps CGO_ENABLED=0 working, and therefore what keeps
// the shipped binary static and installable on any Linux of the same architecture
// with no libc version to match. A chat client that needs a matching libc is a chat
// client somebody cannot run.
//
// # XDG, and what is deliberately not here
//
// Configuration and data follow the XDG Base Directory Specification, honouring
// $XDG_CONFIG_HOME and $XDG_DATA_HOME when set. The device store is the exception:
// it lives under the data directory but at 0600, because it is credential material,
// and it is the one file a backup must never pick up.
//
// No secret, token or session is ever written to the repository, to the config
// directory, or to a log.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Paths holds every location cli-zapp writes to.
type Paths struct {
	// Config is the directory holding config.toml.
	Config string
	// Data holds the message cache and application state.
	Data string
	// Devices is whatsmeow's device store: identity keys, prekeys, session.
	//
	// 0600, and excluded from backups. It is credential material in the same sense
	// a private key is.
	Devices string
}

// ErrNoDataDir is returned when the XDG data directory cannot be determined.
var ErrNoDataDir = errors.New("cannot determine a data directory")

// Default returns the standard paths, honouring the XDG environment.
//
// $HOME is checked explicitly rather than relying on os.UserConfigDir, because
// UserConfigDir silently returns a relative path when HOME is unset — and a relative
// path is how a database ends up written into whatever directory the program happened
// to be started from.
func Default() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return Paths{}, fmt.Errorf("%w: $HOME is not set", ErrNoDataDir)
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}

	base := filepath.Join(dataHome, "cli-zapp")
	return Paths{
		Config:  filepath.Join(configHome, "cli-zapp"),
		Data:    base,
		Devices: filepath.Join(base, "devices.db"),
	}, nil
}

// Ensure creates the directories, with permissions that match what they hold.
//
// Data is 0700 rather than 0755 because the cache holds message content: on a shared
// machine, a world-readable directory of private correspondence is a leak that nobody
// would think to look for. The devices file is 0600 for the same reason, one level
// down.
func (p Paths) Ensure() error {
	for _, dir := range []string{p.Config, p.Data} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	return nil
}

// LogPath returns where the log file should go.
//
// The default is a file in the data directory rather than a temporary file: a log the
// user cannot find is a log nobody reads, and "it crashed, what was in the log?" is
// the first question worth being able to answer. Logs contain lifecycle events and
// errors, never message content or credentials.
func (p Paths) LogPath() string { return filepath.Join(p.Data, "cli-zapp.log") }
