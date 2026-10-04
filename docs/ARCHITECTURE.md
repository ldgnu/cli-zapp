# Architecture

Why the code is arranged the way it is. The short version: the protocol is
unofficial and unstable, so it is confined to one directory, and everything above
that directory is testable without it.

## The dependency rule

```
cmd/wterm                composition root: flags, theme, services, wiring

internal/ui              ← imports models, keybindings, text, theme, whatsapp
  components/               (interfaces only — never whatsmeow)
  app/
internal/whatsapp       interfaces + the in-memory fake
  adapter/                ← the ONLY package importing go.mau.fi/whatsmeow
internal/models         domain types; imports nothing from wterm
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

## Pure-function rendering

`View()` takes a state struct and returns a string. Components hold no cursor,
no scroll offset and no selection; all of that lives in the caller's state.

Consequences:

- **The whole UI is testable without a terminal.** Every layout invariant —
  no line exceeds the width, the frame is exactly as tall as the terminal, it
  holds from 40×10 to 200×60 — is asserted in unit tests.
- **The previous frame cannot be corrupted.** `Update` returns a copy.
- **Rendering is deterministic.** No map iteration order leaks into the output:
  `SortChats` and `ReactionSummary` are stable sorts.

The composer is the deliberate exception: it owns its text buffer, because a text
field needs a caret and a scroll offset that persist across frames.

## The input architecture

Key handling has one path, and the order is load-bearing:

```
key press
  ├─▶ 1. overlay?        ──▶ the overlay consumes it
  ├─▶ 2. escape?         ──▶ contextual cancel
  ├─▶ 3. global binding? ──▶ quit, search, sync, panel focus
  ├─▶ 4. search field?   ──▶ text
  ├─▶ 5. composer?       ──▶ text and editing keys
  └─▶ 6. panel binding?  ──▶ the rest
```

**Step 3 comes before step 5 on purpose.** The obvious order — offer the key to
the focused text field first — makes every `ctrl` shortcut silently dead, because
a text field accepts arbitrary input and reports that it consumed the press. The
user would find that `ctrl+q` quit the application from the sidebar but not from
the composer, which is where they spend most of their time.

**Step 5 excludes reserved keys.** Enter, tab, the arrows and anything with a
modifier are the binding map's, not the text field's. A textarea would treat
enter as a newline, which is how a compose box ends up where nothing is ever
sent.

**Step 6 is scoped to the focused panel.** A key that scrolls the transcript must
not scroll the sidebar, so `ctrl+b` means different things in different panels.
Panel focus itself (`mod+h`, `mod+l`) is global, because the point of an
i3-style hierarchy is that it reaches a panel from anywhere.

### Key conversion

`internal/ui/app/keys.go` is the only place the two representations meet.
`keybindings.Key` is a plain struct so the whole binding system tests without
Bubble Tea.

The conversion reads `KeyPressMsg.Mod` and `.Code` rather than parsing
`.String()`, because `String()` is lossy for printable keys: it returns `"j"` for
ctrl+j, because ctrl+j and j arrive as the same byte. Parsing that string yields
a key with no modifier and the binding silently fails to match.

Named keys are recognised through an explicit table, not a numeric range, because
Bubble Tea is inconsistent: arrows and page keys live above `unicode.MaxRune`,
while tab, enter, backspace, delete and space are plain ASCII. A range test
catches half of them.

## Terminal text handling

Three problems, one scanner.

**Measurement.** A Go string counts bytes; a terminal counts cells. For `ñ` those
are 2 and 1, for an emoji 4 and 2, for a CJK ideograph 3 and 2. Byte length
overflows every column.

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

`text.OverlayCells` composes a styled box over existing content cell by cell,
which is what lets a dialog sit over the conversation without erasing the parts
that stick out around it.

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
since the terminal's background is out of wterm's control. There is deliberately
no `Background` field: overriding the user's terminal colours is a nuisance, not a
feature.

## Commands and the pending queue

`Update` returns a command. `Model.reply` joins the command produced by handling
a message with any follow-ups the model queued for itself.

The distinction matters: a command that is the *reply* must be returned from
`Update`, because Bubble Tea only schedules what `Update` hands back — queueing it
strands the key press and the key appears to do nothing. A follow-up has no
caller, so queueing it until the next tick is correct and testable.

The event stream is drained on its own goroutine and posted through
`Program.Send`, so the model is only ever touched from the UI goroutine. All the
model's service caches are replaced wholesale by command results rather than
mutated in place, which is what lets `Shutdown` guarantee nothing is left running
against a dead terminal.

## Testing strategy

- **Pure functions, tested directly.** Layout, measurement, sanitisation and the
  binding table are ordinary functions with no terminal in sight.
- **The model, driven through its own handlers.** A test constructs the model,
  applies a `WindowSizeMsg` and a `KeyPressMsg`, and calls `View()`. Commands are
  run and their messages folded back in, because a Bubble Tea command is just a
  function returning a message.
- **The fake, through its interfaces.** Tests use the service views, so a change
  to an interface breaks them rather than silently passing.
- **`-race` in CI.** The sync goroutine writes while the UI reads;
  `TestConcurrentAccessIsSafe` exercises that deliberately.
- **A pty smoke test** for the built binary, because a TUI refuses to start
  without a terminal and the alternate screen does not exist anywhere else.

## Where Phase 2 goes

`storage/sqlite` and `sync/` slot in without touching the UI:

- The `ChatStore`/`MessageStore` interfaces mirror the service interfaces, so the
  adapter composes them into one implementation.
- The sync engine reconciles events into the stores. The UI already consumes a
  single ordered event stream, so it does not change.
- Two databases stay separate: whatsmeow's device store holds cryptographic
  material under the library's schema, and wterm's holds application state under
  wterm's. Merging them would couple wterm's migrations to an upstream schema that
  changes with every protocol update.
