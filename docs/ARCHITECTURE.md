# Architecture

Why the code is arranged the way it is. The short version: the protocol is
unofficial and unstable, so it is confined to one directory; the interface is
built out of regions that own their own state; and everything above that
directory is testable without either.

See [UX.md](UX.md) for the interface decisions and their reasoning, and
[LIMITATIONS.md](LIMITATIONS.md) for what the protocol does not support.

## The dependency rule

```
cmd/cli-zapp                composition root: flags, theme, services, wiring

scripts/                 development tools; not part of the binary
  framedump/               prints one frame, exactly as the renderer sends it
  ptycheck.py              drives the binary through a real pty

internal/ui              ← imports models, keybindings, text, theme, whatsapp
  layout/                    geometry, as a pure function of (width, height)
  component/                the Region contract and the shared event vocabulary
  theme/                    palette, metrics, glyphs, styles — a value
  components/               sidebar, transcript, composer, statusbar, palette, overlay
  app/                      the root model: composition, routing, dispatch
internal/whatsapp       interfaces + the in-memory fake
  adapter/                ← the ONLY package importing go.mau.fi/whatsmeow
internal/models         domain types; imports nothing from cli-zapp
internal/keybindings    actions and keys; no Bubble Tea
internal/text           terminal measurement, wrapping, ANSI safety
internal/storage        SQLite
internal/sync           reconciliation
internal/notifications  bell, notify-send
internal/config         TOML
internal/logging        structured logging to a file
```

Arrows point at what a package may import. `models` sits at the bottom and
imports nothing, which is what makes it cheap to test and impossible to
contaminate with protocol details.

**This is enforced, not documented.** `internal/ui` does not import `whatsmeow`,
so a leak fails to compile in the direction that matters: a Phase 1 build
exercises the entire interface with the protocol absent from the dependency
graph.

## Regions own their state

Every part of the interface implements `component.Region`:

```go
type Region interface {
    Name() string
    Resize(layout.Rect)
    Update(tea.Msg) (Region, tea.Cmd)
    View() string
    Focus() tea.Cmd
    Blur()
    Focused() bool
}
```

That is Bubble Tea's pattern applied once per region rather than once per
application, and it is what makes the components reusable: a sidebar that owns
its own `Update` can be dropped into another application without that
application reimplementing sidebar key handling.

### Regions emit events, they do not mutate siblings

A region never reaches into another region. It returns a `component.Event` as a
command result, and the root decides what it means. This is the whole of the
architecture:

- The transcript cannot mark a chat read.
- The sidebar cannot scroll the transcript.
- Neither mentions a JID, a service or a protocol value.

An event describes *intent* — "the user chose this chat" — never "set this
field", so the root remains free to reject an event it disagrees with and to own
every consequence.

### Regions receive rectangles, they do not compute them

The root owns the layout and is the only thing that knows it. Two regions
independently guessing at widths is how a sidebar and a transcript end up
disagreeing about where the divider is, and the disagreement shows up only as a
visibly crooked frame.

## The root owns decisions, regions own pixels

`app.Model` composes regions into a frame and turns their events into
consequences. It never draws a region itself.

```go
// in the transcript
case keybindings.ActionReply:
    return m, m.reportCursorAnd(component.KindMessageActivated, string(a))

// in the root
case component.KindMessageActivated:
    return m.beginReply()
```

Neither half knows about the other, so either can be replaced — the transcript
by a different renderer, the root by a different application — without touching
the other.

## Why interfaces, given a single implementation

Six interfaces with one real implementation and one fake. The justification is
not mockability for its own sake:

- **The protocol can break.** WhatsApp can change it without notice. Confining
  that to `whatsapp/adapter` is the difference between a one-package fix and a
  rewrite.
- **The UI was built first.** Phase 1 exists to make the boundary real before
  there is anything behind it. Every layout, navigation and rendering decision
  has been made and tested with the protocol absent.
- **The fake is not a mock.** It keeps real state and enforces real invariants: a
  send appends to the transcript, updates the chat preview, increments the unread
  count for incoming messages, and confirms delivery asynchronously. A mock
  returning canned values would let the UI be built against behaviour the real
  adapter never produces — the failure mode mocks are supposed to prevent and
  often cause.

## Six service views over one fake

Several services need differently-shaped methods of the same name — `Get` for a
chat, a contact and a group; `Delete` for a chat and a message. Go cannot
overload, so `whatsapp.Fake` exposes one view per interface, each an embedding
shim that shadows only the conflicting methods. The compile-time assertions at
the top of `fake.go` are what prove the split stays complete.

## The input architecture

Key handling has one path, and the order is load-bearing:

```
key press
  ├─▶ 1. overlay open?  ──▶ the overlay consumes it
  ├─▶ 2. escape?         ──▶ contextual cancel
  ├─▶ 3. global binding? ──▶ quit, search, sync, palette, panel focus
  ├─▶ 4. palette open?   ──▶ the palette handles it: text, backspace, enter, up, down
  └─▶ 5. focused region  ──▶ everything else
```

**Step 3 comes before steps 4 and 5 on purpose.** The obvious order — offer the
key to the focused text field first — makes every `ctrl` shortcut silently
dead, because a text field accepts arbitrary input and reports that it consumed
the press. The user would find that `ctrl+q` quits from the sidebar but not from
the composer, which is where they spend most of their time.

**Step 4 delegates rather than filtering.** An earlier version checked whether
the press was printable, appended it to the query, and dropped everything else —
which meant `enter`, `down` and `ctrl+j` never reached the palette, so its list
could not be moved and no command could be run. The component's tests passed
throughout, because they drove the component directly. The rule this establishes:
a region's own `Update` is the single authority on its keys, and the application
routes rather than reinterprets.

**Step 2 comes before step 3** because the user must always be able to abandon a
mode they are stuck in. An escape that is itself bound to something is a trap.

**Step 5 is scoped by panel.** A key that scrolls the transcript must not scroll
the sidebar, so `ctrl+b` means different things in different panels. Panel focus
itself (`mod+h`, `mod+l`) is global, because the point of an i3-style hierarchy
is that it reaches a panel from anywhere.

### Key conversion

`internal/ui/component/keys.go` is the only place the two representations meet.
`keybindings.Key` is a plain struct so the whole binding system tests without
Bubble Tea.

Three facts about Bubble Tea were verified against its source rather than its
documentation, because each one breaks a shortcut and presents as "the
application is ignoring my keys":

- **`String()` drops modifiers for printable keys.** It reports `"j"` for ctrl+j,
  because ctrl+j and j arrive as the same byte. The conversion therefore reads
  `KeyPressMsg.Mod` and `.Code` and never parses the string.
- **Named keys are not uniformly encoded.** Arrows and page keys live above
  `unicode.MaxRune`; tab, enter, backspace, delete and space are plain ASCII
  codes. A range test catches half of them, so recognition goes through an
  explicit table.
- **`String()` is inconsistent about whether a key has text.**
  `{Code: 13, Text: "\r"}` reports `"\r"` and `{Code: 13}` reports `"enter"`.
  Anything matching a key *name* must use `Keystroke()`, which is stable across
  both.

## Layout as a pure function

`internal/ui/layout` computes the geometry of one frame from `(width, height)`
and returns rectangles. It is a package of its own because a layout computed
inside the render function cannot be tested without rendering, and a layout that
is wrong is visible only as a broken screen.

`Compute` is total: every size yields a valid layout, and sizes too small to
render produce `ModeTooSmall` rather than negative dimensions, so callers never
defend against it. The test suite sweeps every size from 40×10 to 200×60 and
asserts that no region is negative and none escapes the terminal.

## Pure-function rendering

Every `View()` returns a string and touches nothing. The root enforces the
frame's exact dimensions in one place — `paintBlock` — rather than trusting each
region, because Lip Gloss pads a short block out to the tallest one's width and
no region knows the terminal's dimensions. Without it the frame is exactly as
wide as the widest line any region produced, which is how a TUI overflows its
terminal and starts wrapping.

Consequences:

- **The whole UI is testable without a terminal.** Every layout invariant — no
  line exceeds the width, the frame is exactly as tall as the terminal — is
  asserted in unit tests, at every size.
- **Rendering is deterministic.** No map iteration order leaks into the output:
  `SortChats` and `ReactionSummary` are stable sorts.
- **Overlays are layers.** Both the palette and the dialog stack return
  full-screen frames; merging is one rule: a non-blank cell in the layer wins.
  A centred dialog therefore covers the conversation without repainting the
  status bar it overlaps.

## Commands, and who runs them

`component.Command` is a value with `ID`, `Title`, `Description`, `Shortcut`,
`Category`, `Available`, `Danger` and an `Execute func() tea.Cmd`.

The split that matters is **who calls `Execute`**. The palette filters a list of
commands and emits the chosen one as an event; the application runs it. The
command carries the behaviour, and the palette does not know what any of it
means.

The tempting alternative — the palette calling `Execute` itself — would need the
application's state passed in, which makes the palette a second copy of the
application rather than a component. Keeping the call on the application's side
is also what makes adding a command a one-place change: `Model.command` wires the
action, so there is no parallel dispatch table to update and no way for the two to
disagree about what a command does.

`Available` is a `bool` rather than a `func() bool` for a related reason. A
closure in a value the palette holds would capture the state at the moment the
table was built and then go stale — "Responder" would stay greyed after the user
selected a message. The table is rebuilt whenever the state it depends on changes,
which is one line at one place and has nothing to capture. `Execute` is safe for
the same reason: it is a fresh closure each time, not a stored one.

A command with a nil `Execute` is declared but not implemented, and the
application reports it in a toast rather than ignoring it. A palette entry that
looks right and does nothing is the failure mode this design exists to make
visible, and the table's tests assert that no entry is in that state.

## Terminal text handling

Three problems, one scanner.

**Measurement.** A Go string counts bytes; a terminal counts cells. For `ñ` those
are 2 and 1, for an emoji 4 and 2, for a CJK ideograph 3 and 2. Byte length
overflows every column. `text.Width` ignores escape sequences, because the
terminal does — and an earlier version did not, which padded every styled
fragment several cells too wide and made every column calculation wrong.

**Layout.** Long bodies wrap on word boundaries; an overlong word — a URL —
breaks mid-word, because leaving it overlong would overflow the bubble and corrupt
the layout. Wrapping works on grapheme clusters, so a combining accent or an emoji
modifier is never split off its base character.

**Safety.** Message bodies arrive from the network and are drawn directly, so a
body containing escape sequences would let a sender repaint the screen, move the
cursor, or set the terminal title and clipboard. `text.Sanitize` is a state
machine over the full escape grammar — CSI, OSC, DCS, 8-bit C1 forms — and consumes
unterminated sequences, because emitting a truncated payload is the
vulnerability.

Two bugs here are worth recording, since both were invisible until a concrete
layout went wrong:

- A first scanner mishandled Lip Gloss's 24-bit truecolour
  (`\x1b[38;2;17;27;33m`), leaving sequence fragments in the visible text and
  corrupting every column measurement.
- It then tested every *byte* for 8-bit C1 introducers, and `0x9f` — a UTF-8
  continuation byte inside an emoji — matched APC, so emoji were silently eaten.
  Runes are now decoded before the byte-level scan.

## Theme as a value

`theme.Theme` bundles a palette, layout metrics, glyphs and the styles derived
from them. Not a package-level singleton with a mutex: a value costs one struct
copy and removes the locking question from the render path, and it makes styles
testable in isolation.

Widths are deliberately **not** baked into styles. A style with a fixed width
silently overflows the moment the terminal is resized — a bug this project had,
caught by a test that renders at 24 columns while the theme says 30.

Both palettes are derived from WhatsApp's own colours, tuned for legibility on
light and dark terminal themes rather than matching the web app pixel for pixel,
since the terminal's background is out of cli-zapp's control. There is deliberately
no `Background` field: overriding the user's terminal colours is a nuisance, not a
feature.

## Commands and the pending queue

`Update` returns a command. `Model.reply` joins the command produced by handling
a message with any follow-ups the model queued for itself.

The distinction matters: a command that is the *reply* must be returned from
`Update`, because Bubble Tea only schedules what `Update` hands back — queueing it
strands the key press and the key appears to do nothing. A follow-up has no
caller, so queueing it until the next tick is correct and testable.

The account event stream is drained by a long-lived command that reads the
service's channel and returns a *batch*, capped at sixty-four events. Draining in
batches rather than one message per event is what keeps a resync usable: two
thousand history messages become a handful of redraws rather than two thousand.
There is no separate pump goroutine and no `Program.Send`, so the model is only
ever touched from the UI goroutine.

All the model's service caches are replaced wholesale by command results rather
than mutated in place, which is what lets `Shutdown` guarantee nothing is left
running against a dead terminal.

## Detecting conflicts in the binding table

`Map.Conflicts` compares every pair of claims on a key, not every claim against
the first. Comparing against the first misses the case that matters: a key
claimed once in the sidebar and twice in the message list is a conflict between
the two message-list claims, while the first-claim comparison calls all three
unambiguous. That is how `ctrl+e` was bound to both "edit message" and "scroll
down" with a startup check that reported nothing.

A regression test covers the three-claim shape directly, because a comparison
that is subtly wrong in the direction of "no conflict" is invisible: a table with
no real conflicts looks identical to a checker that cannot find any.

## Testing strategy

- **Pure functions, tested directly.** Layout, measurement, sanitisation and the
  binding table are ordinary functions with no terminal in sight.
- **Regions, driven through their own `Update`.** A region is constructed,
  given a rectangle and a set of models, and asked for its `View`. No program, no
  terminal, no Bubble Tea loop.
- **The model, driven through its own handlers.** A test constructs the model,
  applies a `WindowSizeMsg` and a `KeyPressMsg`, and calls `View()`.
- **The fake, through its interfaces.** Tests use the service views, so a change
  that breaks one is caught where it is used.
- **The frame, asserted exactly.** The root's frame is checked to be precisely
  the terminal's dimensions at every size in a sweep, because a frame one cell
  too wide makes the terminal wrap every line and one row short leaves stale
  characters.
- **The real program, through a pty.** `scripts/ptycheck.py` starts the binary
  on a real tty at six sizes, sends keys, and requires a clean exit. A Bubble Tea
  application cannot be exercised any other way: the renderer negotiates
  capabilities with the terminal and blocks on answers it never receives when
  there is no tty on stdin.
- **The frame, exactly as sent.** `scripts/framedump` builds the same model the
  program builds, drives it through its public surface by running the commands
  `Update` returns, and prints `View()`. What it shows is what a user sees; the
  pty capture, being a stream of partial repaints, is not.