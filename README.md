# wterm

A WhatsApp client for the terminal, built with Bubble Tea and Go.

wterm is keyboard-driven and visually close to WhatsApp Web: a chat list on the
left, the conversation on the right, bubbles, timestamps, delivery marks and
presence. It runs on Linux, builds to a static binary with no C toolchain, and
keeps its state in a local SQLite database.

> **Status: Phase 1.** The interface, the keybinding system and the domain model
> are complete and tested. The protocol adapter is not yet implemented, so the
> application currently runs against an in-memory fake. See
> [Roadmap](#roadmap) and [Limitations](docs/LIMITATIONS.md).

---

## Running it

```sh
make build      # → bin/wterm
make run        # build and start with demo data
make keys       # print the default keybindings as TOML
```

```
wterm [flags]

  --demo           run against in-memory demo data (default true)
  --color=dark     colour scheme: dark or light
  --glyphs=unicode glyph set: unicode or ascii
  --debug          write debug-level logs
  --verbose        write verbose logs
  --log=PATH       log file (default: a temporary file)
  --help-keys      print the default keybindings and exit
```

The binary is built with `CGO_ENABLED=0`. SQLite is provided by
[modernc.org/sqlite](https://modernc.org/sqlite), a pure-Go implementation, so
the result is statically linked and needs no libc to match.

---

## Keybindings

The navigation vocabulary follows i3wm: **mod** means `ctrl`, `hjkl` move,
and the modifier-free keys stay free for scrolling and text entry.

| Key | Action |
| --- | --- |
| `mod+q` | Quit, or close the top overlay |
| `mod+f` | Search |
| `mod+r` | Reconnect and resynchronise |
| `mod+n` | New chat |
| `mod+g` | Contact or group information |
| `mod+h` / `mod+l` | Focus the sidebar / the composer |
| `mod+j` / `mod+k` | Next / previous item |
| `mod+enter` | Primary action for the focused panel |
| `mod+e` | Edit the selected message |
| `mod+d` | Delete the selected message |
| `mod+u` | Mark the chat read or unread |
| `mod+p` | Pin the chat |
| `mod+m` | Mute the chat |
| `esc` | Cancel, contextual |
| `enter` | Send (in the composer) |
| `tab` / `shift+tab` | Next / previous panel |
| `pgup` / `pgdown` | Scroll |
| `ctrl+home` / `ctrl+end` | Top / bottom of the transcript |
| `space` | Select or deselect a message |
| `r` / `R` | Reply / react |
| `mod+?` | Help |

`make keys` prints the full table in TOML form, ready to paste into a
configuration file.

### Nothing is hardcoded

No component compares key strings. A key press is resolved to an
[action](internal/keybindings/keybindings.go) by a table, and components are
handed the action:

```
key press ──▶ keybindings.Map.Resolve ──▶ Action ──▶ component
```

This is enforced rather than merely intended:

- `internal/keybindings` has no dependency on Bubble Tea, so the whole input
  layer is tested with plain structs and no terminal.
- The binding table is data, and `Map.Conflicts` reports duplicates **at
  startup**. During development this caught three real collisions in the
  defaults: `ctrl+d` was both "delete message" and "scroll down", `ctrl+f` was
  both "search" and "page down", and `ctrl+n` was both "new chat" and "next
  chat" in the composer.
- A test walks the navigation paths from the design brief with real key presses
  to prove the table behaves as intended.

Bindings can be scoped globally or per panel. Global bindings are resolved
*before* the composer is offered the key — otherwise a text field would swallow
`ctrl+q`, `ctrl+f` and every other application-wide shortcut, and they would
work everywhere except where typing happens.

---

## Architecture

```
internal/
  models/        domain types; imports nothing from wterm
  keybindings/   actions, key specs, binding table — no Bubble Tea
  text/          terminal-aware measurement, wrapping, ANSI safety
  whatsapp/      service interfaces + the in-memory fake
  storage/       SQLite (Phase 2)
  sync/          reconciliation engine (Phase 3)
  ui/
    theme/       palettes, metrics, glyph sets
    components/  chatlist, messagelist, composer, statusbar, overlay
    app/         root Bubble Tea model
  notifications/ bell, notify-send, unread counter
  config/        TOML configuration (Phase 2)
  logging/       structured logging to a file, never to the terminal
```

The dependency rule is one-directional:

```
ui ──▶ models, keybindings, text, theme
whatsapp/adapter ──▶ models          (Phase 3)
models ──▶ (nothing)
```

**The UI cannot know about the protocol.** It talks to six interfaces —
`ChatService`, `MessageService`, `ContactService`, `GroupService`,
`MediaService`, `SyncService` — expressed in terms of `models` types. This is
enforced by construction: `internal/ui` does not import `whatsmeow`, and the
whole interface builds and tests against the fake.

That is not theoretical. During Phase 1 the test suite caught a bug where **every
`ctrl` shortcut was dead**: Bubble Tea's `KeyPressMsg.String()` returns `"j"` for
ctrl+j, dropping the modifier, so parsing that string produced an unbound key.
`internal/ui/app/keys.go` now reads the modifier field directly, and the comment
there records why.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full reasoning.

---

## Protocol

whatsmeow is a Go implementation of the WhatsApp Web multidevice protocol, and
it is the only viable option. Verified capabilities and rejected alternatives
are in [docs/PROTOCOL.md](docs/PROTOCOL.md); the honest limitations are in
[docs/LIMITATIONS.md](docs/LIMITATIONS.md).

**There is no official API for a personal WhatsApp account.** wterm speaks an
unofficial protocol, which carries real consequences — including the possibility
of an account ban. Those are documented rather than glossed over.

---

## Development

```sh
make check      # fmt-check, vet, test -race, lint — what CI runs
make test       # unit tests with the race detector
make test-cover # coverage per package
make lint       # golangci-lint
make tools      # install the linter
```

Current coverage:

| Package | Coverage |
| --- | --- |
| `ui/components/statusbar` | 100% |
| `ui/components/chatlist` | 94% |
| `ui/theme` | 94% |
| `notifications` | 93% |
| `keybindings` | 88% |
| `ui/components/overlay` | 86% |
| `whatsapp` (the fake) | 84% |
| `ui/components/messagelist` | 84% |
| `logging` | 86% |
| `text` | 69% |
| `ui/app` | 61% |
| `models` | 60% |

255 test functions, run under `-race`.

### Testing the interface without a terminal

`View()` is a pure function of state, so the whole UI is testable without a
terminal:

```go
m := app.New(fake.Services(), theme.Dark(), keybindings.DefaultMap())
m = resize(t, m, 100, 30)          // drive the model's own resize handler
got := text.StripANSI(m.View().Content)
```

That is how the layout invariants are enforced — no line exceeds the terminal
width, the output is exactly as many lines as the terminal has rows, and it holds
at every size from 40×10 to 200×60:

```go
func TestViewRespectsTerminalWidth(t *testing.T) {
	m, _ := newTestModel(t, 100, 30)
	for i, line := range strings.Split(render(m), "\n") {
		if width := text.VisibleWidth(line); width > 100 {
			t.Errorf("line %d is %d cells", i, width)
		}
	}
}
```

The built binary is additionally smoke-tested on a real pty, since a TUI needs a
terminal to start at all. That test drives the real navigation paths — focus
switching, typing, sending — and asserts it exits cleanly on `ctrl+q`.

### Linting

`golangci-lint` runs with a deliberately chosen set rather than the maximum. Each
enabled linter maps to a risk this codebase is actually exposed to: measuring text
by cells, compositing escape sequences, and sharing state between a sync goroutine
and the render loop. A whitespace linter was tried and removed — it produced about
950 findings on already-gofmt'd code, and a linter whose output cannot be acted on
gets switched off within a week, after which it catches nothing.

### Security

- Message bodies are sanitised before rendering (`text.Sanitize`). A peer who
  sends raw escape sequences cannot repaint the screen, move the cursor, or set
  the terminal title. This is a state machine over the full escape grammar, not a
  filter on the ESC byte: it handles CSI, OSC, DCS, 8-bit C1 forms, and consumes
  unterminated sequences — emitting a truncated payload is the vulnerability.
- Logs go to a file, never to stdout, and are created `0600`.
- Downloads are written `0600`: attachments are private correspondence.

---

## Roadmap

| Phase | Contents | Status |
| --- | --- | --- |
| 0 | Foundations: tooling, lint, CI, `models`, `keybindings`, `text` | done |
| 1 | `theme`, components, root model, demo data | done |
| 2 | `storage/sqlite`: schema, migrations, repositories | next |
| 3 | `whatsapp/adapter`: pairing, sync engine, send/receive | |
| 4 | Full loop against a real account | |
| 5 | Reactions, edit, delete, reply, forward, menus | |
| 6 | Groups, search, pin/mute/archive, contact info | |
| 7 | Media, keyring, notifications, voice notes | |

---

## Licence

MPL-2.0. See [LICENSE](LICENSE).

wterm is not affiliated with, endorsed by, or connected to WhatsApp or Meta. It
uses an unofficial protocol; read [docs/LIMITATIONS.md](docs/LIMITATIONS.md)
before linking an account you care about.
