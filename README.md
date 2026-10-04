# cli-zapp

A WhatsApp client for the terminal, built with Bubble Tea and Go.

cli-zapp is keyboard-driven and visually close to WhatsApp Web: a chat list on the
left, the conversation on the right, bubbles, timestamps, delivery marks and
presence. It runs on Linux, builds to a static binary with no C toolchain, and
keeps its state in a local SQLite database.

> **Status: Phase 1.** The interface, the keybinding system and the domain model
> are complete and tested. The protocol adapter is not yet implemented, so the
> application currently runs against an in-memory fake. See
> [Roadmap](#roadmap) and [Limitations](docs/LIMITATIONS.md).

The interface decisions and their reasoning are in
[docs/UX.md](docs/UX.md); the layering is in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

---

## Running it

```sh
make build      # → bin/cli-zapp
make run        # build and start with demo data
make keys       # print the default keybindings as TOML
make frame      # print one frame of the interface, at any size
```

```
cli-zapp [flags]

  --demo           run against in-memory demo data (default true)
  --color=dark     colour scheme: dark or light
  --glyphs=unicode glyph set: unicode or ascii
  --debug          write debug-level logs
  --verbose        write verbose logs
  --log=PATH       log file (default: a temporary file)
  --help-keys      print the default keybindings and exit
```

The binary is built with `CGO_ENABLED=0` and is statically linked — `file bin/cli-zapp`
says so, and CI asserts it. When the local database arrives it will be provided by
[modernc.org/sqlite](https://modernc.org/sqlite), a pure-Go implementation, precisely
so that this stays true: a chat client that needs a matching libc is a chat client
somebody cannot run.

---

## Keybindings

The navigation vocabulary follows i3wm: **mod** means `ctrl`, `hjkl` move,
and the modifier-free keys stay free for scrolling and text entry.

| Key | Action |
| --- | --- |
| `mod+p` | **Command palette** |
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
| `mod+m` | Mute the chat |
| `esc` | Cancel, contextual |
| `enter` | Send (in the composer) |
| `tab` / `shift+tab` | Next / previous panel |
| `pgup` / `pgdown` | Scroll |
| `ctrl+home` / `ctrl+end` | Top / bottom of the transcript |
| `space` | Select or deselect a message |
| `r` / `R` | Reply / react |
| `mod+?` | Help |

`mod+p` is the command palette, and **pinning a chat has no shortcut**. That is
the trade: a conversation flag does not deserve a more memorable key than the
thing that reaches all twenty other commands. Pinning is still on the binding
table — it appears in `mod+?` under a heading with no key beside it, and it is
configurable — and it is one of the palette's entries. Losing a shortcut is only
acceptable while every action stays reachable somewhere.

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
scripts/
  framedump/     prints one frame, exactly as the renderer sends it
  ptycheck.py    drives the binary through a real pty

internal/
  models/        domain types; imports nothing from cli-zapp
  keybindings/   actions, key specs, binding table — no Bubble Tea
  text/          terminal-aware measurement, wrapping, ANSI safety
  whatsapp/      service interfaces + the in-memory fake
  storage/       SQLite (Phase 2)
  sync/          reconciliation engine (Phase 3)
  ui/
    layout/      geometry as a pure function of (width, height)
    component/   the Region contract and the shared event vocabulary
    theme/       palettes, metrics, glyph sets
    components/  sidebar, transcript, composer, statusbar, palette, overlay
    app/         root model: composition, routing, dispatch
  notifications/ bell, notify-send, unread counter
  config/        TOML configuration (Phase 2)
  logging/       structured logging to a file, never to the terminal
```

Every part of the interface implements `component.Region` — its own model, its own
`Update`, its own `View`. A region emits events and the root decides what they mean,
so the transcript cannot mark a chat read and the sidebar cannot scroll the
transcript. See [docs/UX.md](docs/UX.md) for the interface decisions and
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the layering.

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

That is not theoretical. The test suite has caught, among others:

- **every `ctrl` shortcut dead** — Bubble Tea's `KeyPressMsg.String()` returns `"j"`
  for ctrl+j, dropping the modifier, so parsing that string produced an unbound key.
  `internal/ui/component/keys.go` reads the modifier field instead.
- **a conflict detector that could not report anything** — `Conflicts` compared every
  claim against only the first, so a key claimed twice in one panel and once in
  another looked unambiguous. `ctrl+e` was bound to both "edit message" and
  "scroll down" while the startup check reported nothing.
- **editing that never worked** — the compose-mode fields were cleared before the
  command that read them was built, so an edit was sent against an empty message
  identifier and refused by the service.
- **a conversation that changed under the cursor** — pinning re-sorts the list, and
  the selection was an index, so pinning a conversation silently opened a different
  one. The sidebar now remembers the selection by identifier.
- **a command palette that could not run a command** — the application routed only
  printable presses into the palette and dropped everything else, so `enter`, `down`
  and `ctrl+j` never arrived. The list could not be navigated and nothing could be
  chosen. The palette's own tests passed the whole time, because they drove the
  component directly and never went through the routing.

That last one is the general lesson, and it is why `send` in the frame tests now
runs the commands `Update` returns instead of discarding them: anything reaching the
model through an event command rather than by mutating it is invisible to a harness
that drops the `tea.Cmd`. A test asserting "choosing the help command opens the help
sheet" had been passing while the sheet never opened, because the two strings it
looked for were both rendered by the palette it had failed to close.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full reasoning.

---

## Protocol

whatsmeow is a Go implementation of the WhatsApp Web multidevice protocol, and
it is the only viable option. Verified capabilities and rejected alternatives
are in [docs/PROTOCOL.md](docs/PROTOCOL.md); the honest limitations are in
[docs/LIMITATIONS.md](docs/LIMITATIONS.md).

**There is no official API for a personal WhatsApp account.** cli-zapp speaks an
unofficial protocol, which carries real consequences — including the possibility
of an account ban. Those are documented rather than glossed over.

---

## Development

```sh
make check      # fmt-check, vet, test -race, lint, pty smoke test
make test       # unit tests with the race detector
make test-cover # coverage per package
make lint       # golangci-lint
make frame COLS=100 ROWS=28 KEYS="tab enter hola"   # print one frame
make pty        # drive the binary through a real pty at six sizes
make tools      # install the linter
```

`make frame` prints exactly what the renderer would send, at any size, with any keys
pressed first. It is how the layout is reviewed:

```
$ make frame COLS=100 ROWS=28 KEYS="ctrl+enter enter hola"
```

`make pty` starts the real binary on a real tty, sends keys and requires a clean
exit. That is not redundant with the frame: a Bubble Tea application negotiates
capabilities with the terminal and blocks on answers it never receives when there is
no tty on stdin, so some checking has to happen against a real one.

Current coverage: **72% of statements, 325 test functions**, run under `-race`. The
highest-covered packages are the ones where a mistake is visible:

| Package | Coverage |
| --- | --- |
| `ui/components/statusbar` | 99% |
| `ui/theme` | 92% |
| `ui/layout` | 92% |
| `logging` | 92% |
| `ui/components/overlay` | 89% |
| `ui/component` | 88% |
| `ui/app` (the root model) | 82% |
| `ui/components/sidebar` | 84% |
| `ui/components/composer` | 83% |
| `ui/components/transcript` | 72% |
| `text` | 79% |
| `models` | 51% |

### Testing the interface without a terminal

`View()` touches nothing and returns a string, so the whole UI is testable without a
terminal:

```go
m := New(fake.Services(), theme.Dark(), keybindings.DefaultMap())
m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
got := m.View().Content
```

That is how the layout invariants are enforced — no line exceeds the terminal width,
the output is exactly as many lines as the terminal has rows, and it holds at every
size from 40×10 to 200×60:

```go
func TestFrameFillsTheTerminalAcrossTheWholeSizeRange(t *testing.T) {
	for w := 40; w <= 200; w += 7 {
		for h := 10; h <= 50; h += 3 {
			m := newModel(t, w, h)
			for i, line := range frame(m) {
				if got := text.VisibleWidth(line); got != w {
					t.Fatalf("at %dx%d row %d is %d cells", w, h, i, got)
				}
			}
		}
	}
}
```

The service paths need more than that: a command is a function Bubble Tea schedules
and a test does not, so a test that skips the loop never reaches a send. The harness
in `actions_test.go` runs the commands `Update` returns and folds the results back
in, which is what caught editing being broken.

The built binary is additionally smoke-tested on a real pty, since a TUI needs a
terminal to start at all. `make pty` drives the real navigation paths at six sizes
and asserts it exits cleanly on `ctrl+q`.

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
| 1 | `theme`, `layout`, regions, root model, command palette, demo data | done |
| 2 | `storage/sqlite`: schema, migrations, repositories | next |
| 3 | `whatsapp/adapter`: pairing, sync engine, send/receive | |
| 4 | Full loop against a real account | |
| 5 | Persistence wired into the UI: offline history, search index | |
| 6 | Media, keyring, voice notes | |

The whole of Phase 1's scope is built and tested, including the features the original
plan deferred to Phases 5 and 6 — reactions, edit, delete, reply, groups, search,
pin/mute/archive and the context menus. They were built against the in-memory fake,
which is what they were designed to be testable against, so landing the adapter is a
matter of filling in one package rather than writing the interface twice.

---

## Licence

MPL-2.0. See [LICENSE](LICENSE).

cli-zapp is not affiliated with, endorsed by, or connected to WhatsApp or Meta. It
uses an unofficial protocol; read [docs/LIMITATIONS.md](docs/LIMITATIONS.md)
before linking an account you care about.
