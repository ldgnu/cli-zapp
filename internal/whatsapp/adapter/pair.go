package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
)

// Pairing is one attempt to link this device to an account.
//
// It exists as a value rather than as a channel of raw whatsmeow items so that the
// caller — a terminal, or a test — never handles a protocol type. The QR item, the
// countdown and the error are all flattened into fields here.
type Pairing struct {
	// Code is the raw data a QR code encodes, or the pairing code to type on the
	// phone.
	//
	// Its length is not assumed anywhere. Upstream documents neither the length of a
	// pairing code nor its expiry, so anything rendering it measures it.
	Code string

	// Until is when this code stops working.
	//
	// This is not cosmetic. Upstream states that the login websocket is closed once
	// the QR codes run out — a 160-second window — so a pairing screen that does not
	// count down looks identical to one that has hung.
	Until time.Time

	// Passkey marks that the account requires a passkey confirmation, which the
	// terminal cannot answer.
	Passkey bool
}

// ErrPasskeyRequired means the account is passkey-locked.
//
// Returned rather than papered over: a passkey lives on the account owner's phone and
// has to be confirmed there, so there is nothing a terminal can do but tell them to go
// and do it. Upstream ships the request event for exactly this case.
var ErrPasskeyRequired = errors.New("this account is protected by a passkey")

// ErrPairingTimeout means the codes ran out before the phone accepted one.
var ErrPairingTimeout = errors.New("the pairing codes expired before the phone accepted one")

// QRMethod pairs by showing a QR code the phone scans.
//
// The returned channel yields one value per code, and the caller must keep reading it:
// a channel that is not drained stops the codes coming, and a pairing screen that
// shows one code and then nothing is the single most confusing failure in the whole
// program.
//
// A nil channel with a nil error means the device is already paired and there is
// nothing to do.
func (c *Client) QRMethod(ctx context.Context) (<-chan Pairing, error) {
	raw, err := c.Connect(ctx)
	if err != nil {
		return nil, err
	}
	if c.Paired() {
		// Already linked. Draining and discarding keeps the read loop from blocking,
		// and the nil return tells the caller there is no pairing to do.
		go func() {
			for range raw {
			}
		}()
		return nil, nil
	}

	out := make(chan Pairing)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.disconnected:
				return
			case item, ok := <-raw:
				if !ok {
					return
				}
				switch item.Event {
				case "code":
					p := Pairing{Code: item.Code}
					if item.Timeout > 0 {
						p.Until = time.Now().Add(item.Timeout)
					}
					select {
					case out <- p:
					case <-ctx.Done():
						return
					}
				case "error":
					// Reported through the channel so the UI can show it in place,
					// rather than by closing the channel, which would look like the
					// codes simply running out.
					select {
					case out <- Pairing{Code: "", Until: time.Time{}}:
					default:
					}
					return
				case "timeout":
					select {
					case out <- Pairing{}:
					case <-ctx.Done():
					}
					return
				}
			}
		}
	}()
	return out, nil
}

// PhoneMethod pairs by returning a code to type into the phone.
//
// # Why this route exists at all
//
// A terminal in an SSH session, or one narrower than a QR code is legible, cannot scan
// one. PairPhone exists for that and needs no camera.
//
// # The client type is not a choice
//
// Upstream is explicit: the display name must be formatted "Browser (OS)", the server
// validates it and returns 400 otherwise, and only common browsers and OSes are
// accepted. So this program cannot present itself as "cli-zapp on Linux" — it has to say
// it is a browser, which is worth stating plainly rather than discovering as a 400.
func (c *Client) PhoneMethod(ctx context.Context, phone string) (string, error) {
	if c.Paired() {
		return "", nil
	}

	// Connect first, then wait for the session to be established. Upstream documents
	// this ordering, and warns that requesting a code too early fails intermittently —
	// which reads as "sometimes the code does not work".
	if _, err := c.Connect(ctx); err != nil {
		return "", err
	}

	code, err := c.client.PairPhone(ctx, phone, true,
		whatsmeow.PairClientChrome, browserDisplayName())
	if err != nil {
		return "", fmt.Errorf("requesting a pairing code: %w", err)
	}

	c.mu.Lock()
	if id := c.client.Store.ID; id != nil {
		c.jid = *id
	}
	c.mu.Unlock()

	return code, nil
}

// browserDisplayName is the name WhatsApp shows on the phone's linked-devices list.
//
// It has to be a browser name followed by an OS name: the server rejects anything else
// with a 400, and only a known set of browsers and operating systems is accepted. So
// "cli-zapp on Linux" cannot be used, however much it would be the honest label — and
// saying so here is better than leaving someone to find out from a 400.
func browserDisplayName() string {
	return "Chrome (Linux)"
}
