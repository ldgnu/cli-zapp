package app

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cli-zapp/cli-zapp/internal/models"
	"github.com/cli-zapp/cli-zapp/internal/notifications"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/composer"
	"github.com/cli-zapp/cli-zapp/internal/ui/components/statusbar"
	"github.com/cli-zapp/cli-zapp/internal/whatsapp"
)

// historyLimit is how many messages are fetched for a conversation.
//
// A terminal shows about twenty rows. Two hundred gives ten screens of scrollback,
// which is enough to find a message someone referred to in conversation and cheap
// enough to refetch after every send.
const historyLimit = 200

// requestTimeout bounds every service call.
//
// These run on the goroutine that would otherwise be rendering, so a call that never
// returns freezes the interface with no way back. Thirty seconds is far longer than a
// metadata fetch needs on a working connection, which is the point: the timeout is
// there to catch a dead one, not to be tight.
const requestTimeout = 30 * time.Second

// longRequestTimeout bounds the calls that transfer data — attachments, mostly —
// which legitimately take far longer than a metadata fetch.
const longRequestTimeout = 3 * time.Minute

// Messages the model consumes. Each is produced by a command, so the model is only
// ever touched from Bubble Tea's goroutine.
type (
	// chatsReady carries the conversation list.
	chatsReady struct{ chats []models.Chat }

	// transcriptReady carries a transcript, tagged with the chat it belongs to.
	//
	// The tag is load-bearing: a slow response for a conversation the user has
	// already left must not overwrite the one they are reading.
	transcriptReady struct {
		forChat  models.ChatID
		messages []models.Message
	}

	// statusReady carries a connection state.
	statusReady struct{ conn statusbar.Connection }

	// eventsReady carries a drained batch of sync events.
	eventsReady struct{ events []whatsapp.Event }

	// noticesReady carries queued desktop notifications, to be raised off the
	// render path.
	noticesReady struct{ events []notifications.Event }

	// failure carries a non-fatal error worth surfacing.
	failure struct{ err error }

	// toastPushed is returned by a command that raised a toast from its own
	// goroutine. It exists only to give Bubble Tea something to redraw on.
	toastPushed struct{ msg string }

	// toastExpiredMsg asks the model to drop toasts that have outlived their
	// display window.
	toastExpiredMsg struct{ at time.Time }
)

// --- context ---

// ctx returns the context every service call is made with.
//
// It is a method rather than a field because a context stored on a model outlives the
// operation that created it, which is how a cancelled-but-not-cancelled context ends
// up in a later call.
func (m *Model) ctx() context.Context { return context.Background() }

// callCtx returns a bounded context for one service call.
func callCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// --- queueing ---

// queue records follow-up commands for the next tick.
//
// It is a method returning nothing, rather than one returning a nil command, so that
// "return m, m.reply(m.queue(x))" cannot be written: queue contributes to the reply, it
// does not produce one of its own.
func (m *Model) queue(cmds ...tea.Cmd) {
	for _, c := range cmds {
		if c != nil {
			m.pending = append(m.pending, c)
		}
	}
}

// reply joins the command produced by handling a message with queued follow-ups.
//
// Returning both matters: returning only the follow-ups drops the reply, and
// returning only the reply drops the follow-ups. Joining them in one place is what
// makes the omission impossible at each of the twenty call sites rather than
// something every call site has to remember.
func (m *Model) reply(cmd tea.Cmd) tea.Cmd {
	if len(m.pending) == 0 {
		return cmd
	}
	queued := m.pending
	m.pending = nil

	if cmd == nil {
		return tea.Batch(queued...)
	}
	// A batch, not a sequence: the queued commands are independent fetches, and
	// tea.Sequence would hide its members behind a message the caller has to
	// remember to expand.
	return tea.Batch(append(queued, cmd)...)
}

// --- chat commands ---

// listChats loads the conversation list.
func (m *Model) listChats() tea.Cmd {
	svc := m.services.Chat
	return func() tea.Msg {
		ctx, cancel := callCtx(requestTimeout)
		defer cancel()

		chats, err := svc.List(ctx)
		if err != nil {
			return failure{err: err}
		}
		return chatsReady{chats: chats}
	}
}

// reload re-fetches a transcript.
//
// Almost every mutation ends here: after a send, a delete or a reaction the
// authoritative list comes from the service, and re-fetching is cheaper than
// reasoning about which cached field each mutation invalidated.
func (m *Model) reload(id models.ChatID) tea.Cmd {
	svc := m.services.Message
	return func() tea.Msg {
		ctx, cancel := callCtx(requestTimeout)
		defer cancel()

		msgs, err := svc.History(ctx, id, historyLimit)
		if err != nil {
			return failure{err: err}
		}
		return transcriptReady{forChat: id, messages: msgs}
	}
}

// setRead marks a conversation read or unread.
func (m *Model) setRead(id models.ChatID, read bool) tea.Cmd {
	svc := m.services.Chat
	return func() tea.Msg {
		ctx, cancel := callCtx(requestTimeout)
		defer cancel()

		if err := svc.SetRead(ctx, id, read); err != nil {
			return failure{err: err}
		}
		return m.listChats()()
	}
}

// chatFlag implements the pin, mute and archive toggles.
//
// They differ only in which field they read and which call they make, so writing them
// out three times would be three places to get a subtlety wrong.
func (m *Model) chatFlag(id models.ChatID, current func(models.Chat) bool,
	set func(context.Context, whatsapp.ChatService, models.ChatID, bool) error,
) tea.Cmd {
	svc := m.services.Chat

	// Read the current value now, on the UI goroutine, so that two presses in quick
	// succession compute their targets from the same state rather than racing.
	ctx, cancel := callCtx(requestTimeout)
	chat, err := svc.Get(ctx, id)
	cancel()
	if err != nil {
		return nil
	}
	value := !current(chat)

	return func() tea.Msg {
		callCtx, cancel := callCtx(requestTimeout)
		defer cancel()

		if err := set(callCtx, svc, id, value); err != nil {
			return failure{err: err}
		}
		return m.listChats()()
	}
}

func setPinned(
	ctx context.Context, svc whatsapp.ChatService, id models.ChatID, v bool,
) error {
	return svc.SetPinned(ctx, id, v)
}

func setMuted(
	ctx context.Context, svc whatsapp.ChatService, id models.ChatID, v bool,
) error {
	return svc.SetMuted(ctx, id, v)
}

func setArchived(
	ctx context.Context, svc whatsapp.ChatService, id models.ChatID, v bool,
) error {
	return svc.SetArchived(ctx, id, v)
}

// deleteChat removes a conversation.
func (m *Model) deleteChat(id models.ChatID) tea.Cmd {
	svc := m.services.Chat
	return func() tea.Msg {
		ctx, cancel := callCtx(requestTimeout)
		defer cancel()

		if err := svc.Delete(ctx, id); err != nil {
			return failure{err: err}
		}
		return m.listChats()()
	}
}

// --- message commands ---

// revoke removes a message for everyone.
func (m *Model) revoke(chat models.ChatID, id models.MessageID) tea.Cmd {
	svc := m.services.Message
	return func() tea.Msg {
		ctx, cancel := callCtx(requestTimeout)
		defer cancel()

		if err := svc.Delete(ctx, chat, id); err != nil {
			return failure{err: err}
		}
		return m.reload(chat)()
	}
}

// react applies a reaction. An empty glyph removes one.
func (m *Model) react(chat models.ChatID, id models.MessageID, glyph string) tea.Cmd {
	svc := m.services.Message
	return func() tea.Msg {
		ctx, cancel := callCtx(requestTimeout)
		defer cancel()

		if err := svc.React(ctx, chat, id, glyph); err != nil {
			return failure{err: err}
		}
		return m.reload(chat)()
	}
}

// send delivers the composer's draft.
func (m *Model) send(chat models.ChatID, body string, mode composer.Mode) tea.Cmd {
	svc := m.services.Message

	if mode == composer.ModeEdit {
		// Editing replaces the body of an existing message rather than sending a new
		// one, so the transcript keeps its shape: an edit that appeared as an extra
		// message would misrepresent what was said and when.
		id := m.editing
		return func() tea.Msg {
			ctx, cancel := callCtx(requestTimeout)
			defer cancel()

			if _, err := svc.Edit(ctx, chat, id, body); err != nil {
				return failure{err: err}
			}
			return m.reload(chat)()
		}
	}

	return func() tea.Msg {
		ctx, cancel := callCtx(requestTimeout)
		defer cancel()

		if _, err := svc.Send(ctx, chat, body); err != nil {
			return failure{err: err}
		}
		return m.reload(chat)()
	}
}

// download fetches a message's attachment.
func (m *Model) download(chat models.ChatID, id models.MessageID) tea.Cmd {
	svc := m.services.Media
	return func() tea.Msg {
		ctx, cancel := callCtx(longRequestTimeout)
		defer cancel()

		path, err := svc.Download(ctx, chat, id)
		if err != nil {
			return failure{err: err}
		}
		return toastPushed{msg: "guardado en " + path}
	}
}

// openInDesktop hands an attachment to the desktop's file handler.
//
// It downloads first when the file is not already local, so the user never waits
// twice for the same bytes.
func (m *Model) openInDesktop(chat models.ChatID, msg models.Message) tea.Cmd {
	svc := m.services.Media

	path := ""
	if msg.Media != nil {
		path = msg.Media.LocalPath
	}

	return func() tea.Msg {
		ctx, cancel := callCtx(longRequestTimeout)
		defer cancel()

		if path == "" {
			p, err := svc.Download(ctx, chat, msg.ID)
			if err != nil {
				return failure{err: err}
			}
			path = p
		}
		if err := svc.Open(ctx, path); err != nil {
			return failure{err: err}
		}
		return nil
	}
}

// --- sync commands ---

// syncNow restarts the event stream and reloads everything on screen.
//
// Stopping and starting rather than merely refetching is deliberate: "resync" has to
// recover a wedged connection, and a refetch would leave the stream wedged while
// making the status bar look healthy — which is worse than doing nothing, because the
// user would stop retrying.
func (m *Model) syncNow() tea.Cmd {
	svc := m.services.Sync
	if svc == nil {
		return m.listChats()
	}

	return func() tea.Msg {
		ctx, cancel := callCtx(longRequestTimeout)
		defer cancel()

		if err := svc.Stop(ctx); err != nil {
			return failure{err: err}
		}
		if err := svc.Start(ctx); err != nil {
			return failure{err: err}
		}
		// Each member of the batch is wrapped in its own command, because tea.Batch
		// takes commands rather than messages. A message passed directly here would
		// be a compile error now and a silently dropped update if the types ever
		// converged.
		conn := statusReady{conn: connectionFor(connectionStateFor(svc.Connected()))}
		return tea.Batch(
			func() tea.Msg { return conn },
			m.listChats(),
			m.waitForEvent(),
		)()
	}
}

// waitForEvent blocks until the next batch of account events arrives.
//
// Draining in batches rather than one message per event is what keeps a resync from
// flooding Bubble Tea's loop with a redraw per message: two thousand history messages
// become a handful of frames instead of two thousand.
func (m *Model) waitForEvent() tea.Cmd {
	svc := m.services.Sync
	if svc == nil {
		return nil
	}

	return func() tea.Msg {
		events := svc.Events()

		first, ok := <-events
		if !ok {
			// The stream closed: the service was stopped. Returning a connection
			// state rather than nothing means the status bar stops claiming to be
			// online.
			return statusReady{conn: statusbar.Offline}
		}

		batch := append(make([]whatsapp.Event, 0, 16), first)

		// Take whatever else is already waiting, without blocking. The cap bounds
		// how long a single Update can hold the UI; the rest arrive on the next
		// pass, one tick later.
		const maxBatch = 64
	drain:
		for len(batch) < maxBatch {
			select {
			case e, ok := <-events:
				if !ok {
					break drain
				}
				batch = append(batch, e)
			default:
				break drain
			}
		}

		return eventsReady{events: batch}
	}
}

// --- notifications ---

// notifyAll raises queued desktop notifications.
//
// They are raised here, on the UI goroutine, rather than from the stream reader,
// because the notifier shells out and a slow `notify-send` would stall the event
// pump that everything else depends on.
func (m *Model) notifyAll(events []notifications.Event) tea.Cmd {
	if len(events) == 0 || m.notifier == nil {
		return nil
	}
	notifier := m.notifier
	return func() tea.Msg {
		for _, e := range events {
			notifier.Notify(e)
		}
		return nil
	}
}

// connectionFor maps the service's connection state onto the bar's.
//
// The four protocol states collapse onto three display states on purpose: the bar has
// one line and the difference between "connecting" and "syncing" is not something a
// user can act on. Offline stays distinct because it is the one state that changes what
// they should expect to happen.
func connectionFor(state whatsapp.ConnectionState) statusbar.Connection {
	switch state {
	case whatsapp.ConnectionOnline:
		return statusbar.Online
	case whatsapp.ConnectionConnecting, whatsapp.ConnectionSyncing:
		return statusbar.Syncing
	default:
		return statusbar.Offline
	}
}

// connectionStateFor maps the service's boolean onto a protocol state, so that
// [connectionFor] has a single input type.
func connectionStateFor(connected bool) whatsapp.ConnectionState {
	if connected {
		return whatsapp.ConnectionOnline
	}
	return whatsapp.ConnectionOffline
}
