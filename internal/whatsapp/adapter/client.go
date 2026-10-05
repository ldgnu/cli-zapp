// Package adapter is the only package in cli-zapp that imports go.mau.fi/whatsmeow.
//
// # The one boundary that matters
//
// Everything the protocol does lives here. The UI depends on the interfaces in
// [github.com/cli-zapp/cli-zapp/internal/whatsapp], expressed in terms of
// [github.com/cli-zapp/cli-zapp/internal/models] types, and has no idea whatsmeow
// exists.
//
// That is not architectural decoration. WhatsApp ships protocol changes without
// notice and without a changelog, and upstream absorbs them within days. When one
// lands, the fix belongs in one directory. The blast radius of "WhatsApp changed the
// protocol again" is this package and this package only.
//
// # What is not here, and why
//
// No browser automation, no scraping, no desktop-app driving. All three are the
// wrong tool regardless, and the last two are the same category as the first.
//
// Calls are absent entirely. Upstream states it plainly — "Things that are not yet
// implemented: ... Calls" — and the only call method on a client is RejectCall. There
// is no half-implementation to ship: a TUI offering "accept call" that cannot carry
// audio would be worse than offering nothing.
//
// Broadcast lists are absent for the same reason, and upstream notes they are not
// supported on WhatsApp Web either, so there is no user-visible capability being
// withheld.
//
// # Pairing
//
// Two routes, because terminals differ in what they can show. PairPhone returns a
// code to type into the phone, which works anywhere including over SSH and in a
// terminal too narrow for a QR code. The QR channel is the fallback for terminals that
// can render one.
//
// The QR route has a hard constraint that shapes the UI: upstream documents that the
// login websocket closes once the QR codes run out, a 160-second window. The pairing
// view therefore owns a deadline and re-renders a countdown, rather than sitting on a
// spinner. A pairing screen that appears hung is a pairing screen users restart.
package adapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	wastore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/cli-zapp/cli-zapp/internal/store"
)

// Client wraps a whatsmeow client with the lifecycle cli-zapp needs.
//
// The zero value is not usable; call [New]. The mutex guards the fields Bubble Tea
// commands read from goroutines the library starts: whatsmeow runs its own read loop,
// and the event handler is called from it.
type Client struct {
	mu sync.RWMutex

	container *sqlstore.Container
	client    *whatsmeow.Client
	device    *wastore.Device

	// jid is this account's own identifier, empty until paired.
	jid types.JID

	log waLog.Logger

	// disconnected is closed when the connection drops, so the reader can stop.
	disconnected chan struct{}
	closeOnce    sync.Once
}

// ErrNotPaired is returned by anything requiring a linked account when there is none.
var ErrNotPaired = errors.New("not paired with a WhatsApp account")

// New opens the device store and prepares a client. It does not connect.
//
// Connection is a separate step because connecting on a cold start can take minutes
// on a real account, and the UI has to show that rather than appear to hang. It also
// needs a device store on disk, so it cannot be done before the user has a data
// directory.
func New(ctx context.Context, p store.Paths, log waLog.Logger) (*Client, error) {
	// "sqlite3" is the Drizzle dialect name for SQLite, not a driver reference: the
	// driver is whatever *sql.DB was opened with, and that is the pure-Go one.
	db, err := sql.Open("sqlite", p.Devices+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("opening the device store: %w", err)
	}

	// sql.DB is a pool. whatsmeow's store issues concurrent queries from its own read
	// loop, and SQLite writes serialise on the file, so an unbounded pool turns "one
	// writer" into "N writers contending for the same lock" — which surfaces as
	// SQLITE_BUSY under load rather than as anything recognisable.
	db.SetMaxOpenConns(1)

	container := sqlstore.NewWithDB(db, "sqlite3", log)

	// Upgrade creates and migrates the schema, and it has to be called explicitly.
	//
	// NewWithDB does not do it: it only wraps a connection. sqlstore.New calls Upgrade
	// internally, but taking that path means giving up the *sql.DB and with it the
	// one-connection cap above — which is the whole reason for opening the database by
	// hand. Skipping Upgrade instead is not an option either: GetFirstDevice queries
	// whatsmeow_device, so on a fresh install it fails with "no such table", and a
	// first-run user sees a SQL error instead of a pairing prompt.
	if err := container.Upgrade(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("preparing the device store at %s: %w", p.Devices, err)
	}

	// Tighten the file's own permissions, now that it exists.
	//
	// This has to happen after Upgrade rather than after sql.Open, because sql.Open is
	// lazy: it does not create the file, so a chmod placed there is a silent no-op on a
	// fresh install — which is exactly the install where it matters most.
	//
	// SQLite creates a database 0644 minus the umask. The 0700 directory around it is
	// the real protection, since a file cannot be reached through a directory that
	// cannot be listed or traversed, but "the parent happens to be locked" is not the
	// same guarantee as "this file is not readable" and store.Paths promises the latter.
	//
	// Best-effort on purpose: a filesystem without Unix permissions fails here, and
	// refusing to run would be worse than running with a warning-worthy default.
	if err := os.Chmod(p.Devices, 0o600); err != nil {
		log.Warnf("could not restrict permissions on the device store: %v", err)
	}

	// GetFirstDevice is what makes this an account rather than a multi-device relay:
	// the first linked device is the identity, and subsequent sessions reuse it.
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("reading the device: %w", err)
	}

	return &Client{
		container: container,
		device:    device,
		client:    whatsmeow.NewClient(device, log),
		log:       log,
	}, nil
}

// Close stops the client's background work.
//
// It does not log the device out: logging out unlinks this client from the account and
// the phone stops trusting it, which is a decision for the user and not something a
// program exiting should decide on their behalf.
//
// It also deliberately does not close the *sql.DB. The container's schema is created
// lazily, and closing the pool while whatsmeow's read loop may still be finishing a
// query turns a clean exit into "database is closed" on stderr — after the terminal has
// been restored, so it lands in the user's shell.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		if c.disconnected != nil {
			close(c.disconnected)
		}
		if c.client != nil {
			c.client.Disconnect()
		}
	})
	return nil
}

// Paired reports whether a linked device is stored on disk.
//
// This is the check the UI makes to decide between the pairing view and the main
// interface, so it must not touch the network: it is answered on the first frame.
func (c *Client) Paired() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.device != nil && c.device.ID != nil
}

// JID returns this account's own identifier, or the zero value when not paired.
func (c *Client) JID() types.JID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.jid
}

// DeviceName returns the name shown on the phone's linked-devices list.
//
// It is empty when not paired.
func (c *Client) DeviceName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.device == nil || c.device.ID == nil {
		return ""
	}
	return c.device.PushName
}

// Connect opens the websocket and waits for the session to be usable.
//
// The QR channel is opened first even when the caller intends to use a pairing code:
// upstream is explicit that Connect must happen before PairPhone, and that waiting for
// the first QR item is the reliable way to know the connection is established. A
// pairing code requested too early fails intermittently, which is the kind of bug that
// gets reported as "sometimes the code doesn't work".
func (c *Client) Connect(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	c.mu.Lock()
	if c.disconnected == nil {
		c.disconnected = make(chan struct{})
	}
	ch := c.disconnected
	c.mu.Unlock()

	qrChan, err := c.client.GetQRChannel(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening the QR channel: %w", err)
	}

	if err := c.client.Connect(); err != nil {
		return nil, fmt.Errorf("connecting: %w", err)
	}

	if !c.client.IsLoggedIn() {
		// Wait for the first QR item, which means the connection is up. Bounded,
		// because a terminal that never gets an answer must not hang the program.
		select {
		case _, ok := <-qrChan:
			if !ok {
				return nil, errors.New("the login channel closed before connecting")
			}
		case <-ctx.Done():
			c.client.Disconnect()
			return nil, ctx.Err()
		case <-time.After(30 * time.Second):
			c.client.Disconnect()
			return nil, errors.New("timed out waiting for the connection")
		case <-ch:
			c.client.Disconnect()
			return nil, errors.New("disconnected while connecting")
		}
	}

	// Store.ID is a pointer: nil until paired, which is the same condition Paired()
	// reports. Dereferenced under the same lock so the two cannot disagree.
	c.mu.Lock()
	if id := c.client.Store.ID; id != nil {
		c.jid = *id
	}
	c.mu.Unlock()

	return qrChan, nil
}

// Logout unlinks this device from the account.
//
// This is destructive and irreversible from the program's side: the phone forgets the
// link and a new pairing is required. It is exposed as an explicit command rather than
// folded into Close, because "close the program" and "remove this device from my
// phone" are not the same request.
func (c *Client) Logout(ctx context.Context) error {
	if !c.Paired() {
		return ErrNotPaired
	}
	c.client.Disconnect()
	if err := c.client.Logout(ctx); err != nil {
		return fmt.Errorf("logging out: %w", err)
	}
	c.mu.Lock()
	c.jid = types.JID{}
	c.mu.Unlock()
	return nil
}

// Raw exposes the underlying client for the translation layer.
//
// Returning it rather than re-exporting a hundred methods keeps whatsmeow's surface in
// this package and this package only. Everything above this line is cli-zapp's; the
// line below is a protocol library's.
func (c *Client) Raw() *whatsmeow.Client { return c.client }
