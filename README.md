# cli-zapp

A WhatsApp client for the terminal, built with Bubble Tea and Go.

cli-zapp is keyboard-driven and visually close to WhatsApp Web: a chat list on the
left, the conversation on the right, bubbles, timestamps, delivery marks and
presence. It runs on Linux, builds to a static binary with no C toolchain, and
keeps its state in a local SQLite database.

> **Status: 0.1.0.** The interface, the keybinding system and the domain model are
> complete and tested, and the protocol adapter is written — `cli-zapp pair` links a
> device and `cli-zapp --live` connects.
>
> **`--live` does not yet populate the conversation list.** The set of chats *is* app
> state in WhatsApp's model, and history sync is not written; messages that arrive
> appear, the list of conversations stays empty. Clipboard **copy** works, bracketed
> paste does not. Both are the next pieces of work — see
> [Limitations](docs/LIMITATIONS.md) and [Roadmap](#roadmap).
>
> Until then `cli-zapp` with no flags runs against in-memory demo data, and the whole
> interface is usable.

The interface decisions and their reasoning are in
[docs/UX.md](docs/UX.md); the layering is in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

> ⚠️ **Read [SECURITY.md](SECURITY.md) before linking an account.** This client
> speaks an unofficial protocol and violates WhatsApp's Terms of Service.
> Linking an account carries a real risk of a temporary or permanent ban. Use a
> secondary number.

---

## Screenshots

<!-- Reproduce any frame exactly as the renderer sends it, with the colour codes
     stripped, rather than trusting a screenshot to still be true:

       make frame COLS=92 ROWS=26 KEYS="ctrl+p" | sed 's/\x1b\[[0-9;]*m//g'
-->

```
$ cli-zapp
```

---

## Requirements

| | |
| --- | --- |
| OS | Linux, amd64 or arm64. Built and tested on Arch and CachyOS. |
| Terminal | 80x24 minimum; 120x30 or larger recommended. |
| Go | 1.27 or later — only to build from source. |
| Runtime | None. The binary is statically linked: no libc, no C toolchain. |
| Optional | `wl-clipboard`, `xclip` or `xsel` for copy. `libnotify` for notifications. |

---

## Installation

### Arch Linux

```sh
sudo pacman -U cli-zapp-0.1.0-1-x86_64.pkg.tar.zst
```

Or build the package from a checkout:

```sh
make package-arch
sudo pacman -U dist/cli-zapp-*.pkg.tar.zst
```

The recipe is [`packaging/arch/PKGBUILD`](packaging/arch/PKGBUILD). It is
version-independent: `pkgver` comes from the release tag, so a new release does not
need a new recipe.

> Nothing publishes to the AUR automatically. An AUR push is a publication and a
> human decision. The recipe is validated by `makepkg` on an Arch runner in CI, and
> is ready to copy into an AUR repository when you decide to.

### Debian / Ubuntu

```sh
sudo apt install ./cli-zapp_0.1.0_amd64.deb
```

To build it from a checkout:

```sh
make package-deb
sudo apt install ./dist/cli-zapp_*.deb
```

Removal is clean and reversible:

```sh
sudo apt remove cli-zapp
```

There are no maintainer scripts. This is a terminal application with no daemon, no
systemd unit and nothing written outside your home directory, so there is nothing
for `postinst` to do that you could not do yourself.

### Binary

```sh
tar xzf cli-zapp-linux-amd64.tar.gz
sudo install -m755 cli-zapp-linux-amd64/cli-zapp /usr/local/bin/
```

Or without root:

```sh
mkdir -p ~/.local/bin
install -m755 cli-zapp-linux-amd64/cli-zapp ~/.local/bin/
```

Verify what you downloaded:

```sh
sha256sum -c checksums.txt
```

### Build from source

```sh
git clone https://github.com/ldgnu/cli-zapp.git
cd cli-zapp
make build
./bin/cli-zapp
```

Install system-wide:

```sh
sudo make install              # → /usr/local/bin/cli-zapp
sudo make install PREFIX=/usr  # → /usr/bin/cli-zapp
```

Stage an install without touching the system:

```sh
make install DESTDIR=/tmp/stage
```

---

## Usage

```sh
cli-zapp pair --phone +<código de país y número>   # link this device
cli-zapp unpair                                    # unlink it
cli-zapp keys                                      # the default keybindings

cli-zapp                                           # demo data (default)
cli-zapp --live                                    # a linked account
```

```sh
cli-zapp [flags]

  --version        print version information and exit
  --live           run against a linked WhatsApp account
  --demo           run against in-memory demo data (default true)
  --color=dark     colour scheme: dark or light
  --glyphs=unicode glyph set: unicode or ascii
  --ascii          shorthand for --glyphs=ascii
  --debug          write debug-level logs
  --verbose        write verbose logs
  --log=PATH       log file (default: a temporary file)
  --help-keys      print the default keybindings as TOML and exit
```

```sh
$ cli-zapp --version
cli-zapp 0.1.0
  commit:     94f64b2
  built:      2026-10-04T22:58:47Z
  go:         go1.27.1
  platform:   linux/amd64
```

Other useful commands while developing:

```sh
make run                                     # build and start with demo data
make keys                                    # the default keybindings as TOML
make frame COLS=92 ROWS=26 KEYS="ctrl+p"    # print one frame, exactly as rendered
```

`make frame` prints precisely what the renderer would send, at any size, with any keys
pressed first. It is how layout is reviewed.

---

## Linking an account

```sh
cli-zapp pair --phone +<código de país y número>
```

The command shows the ban risk before it touches the network and asks for
confirmation. Read [SECURITY.md](SECURITY.md) first — the worst case is not a ban but
losing the ability to link any client at all, including the official web one, which is
what happened to another vendor's users for five months.

Pairing is by **code**, not QR: the code takes eight keystrokes on the phone and works
in every terminal, including one over SSH with no graphics. A QR encoder is a few
hundred lines that cannot be verified in this repository, and a subtly wrong one
produces a picture that looks right and does not scan.

Where things are stored:

| | |
| --- | --- |
| `~/.config/cli-zapp/` | settings (nothing yet) |
| `~/.local/share/cli-zapp/` | message cache, chat metadata — `0700` |
| `~/.local/share/cli-zapp/devices.db` | the linked session — `0600` |

The device store is credential material. `.gitignore` excludes `*.db`, and
`scripts/backup.sh` derives its contents from git, so a session database cannot end up
in a backup.

## Configuration

There is no configuration file yet, and none is required — the binary runs as-is.

When one lands it will follow the XDG Base Directory Specification:

| Path | Contents |
| --- | --- |
| `~/.config/cli-zapp/config.toml` | settings and keybinding overrides |
| `~/.local/share/cli-zapp/` | message cache, chat metadata |
| `$XDG_CONFIG_HOME`, `$XDG_DATA_HOME` | honoured when set |

Secrets never belong in the repository, and the linked-device session belongs in the
system keyring rather than in either path.

Keybindings can be inspected today:

```sh
cli-zapp --help-keys > bindings.toml   # the full default table, as TOML
```

---

## Keyboard shortcuts

The navigation vocabulary follows i3wm: **mod** means `ctrl`, `hjkl` move, and the
modifier-free keys stay free for text entry.

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
| `alt+enter` / `shift+enter` | New line |
| `tab` / `shift+tab` | Next / previous panel |
| `pgup` / `pgdown` | Scroll |
| `space` | Select or deselect a message |
| `r` / `R` | Reply / react |
| `mod+?` | Help sheet |

`mod+p` is the command palette, and **pinning a chat has no shortcut**. That is the
trade: a conversation flag does not deserve a more memorable key than the thing that
reaches every other command. Pinning stays on the binding table — it appears in
`mod+?` under a heading with no key beside it, and it is configurable — and it is one
of the palette's entries.

Every action stays reachable. The palette exists partly so that giving up a shortcut
is never the same as losing a feature.

---

## Packaging

```sh
make package        # everything: .deb for both architectures, plus Arch
make package-deb    # cli-zapp_VERSION_amd64.deb and _arm64.deb
make package-arch   # cli-zapp-VERSION-1-x86_64.pkg.tar.zst
make package-arch-arm64
make release        # binary tarballs for every platform, plus checksums
make checksums      # rewrite dist/checksums.txt
make verify-release # rebuild and confirm the archives are byte-identical
```

Everything lands in `dist/`:

```
cli-zapp-linux-amd64.tar.gz
cli-zapp-linux-arm64.tar.gz
cli-zapp_0.1.0_amd64.deb
cli-zapp_0.1.0_arm64.deb
cli-zapp-0.1.0-1-x86_64.pkg.tar.zst
checksums.txt
```

### Reproducibility

Reproducibility here means: building the same commit with the same `VERSION`,
`COMMIT` and `BUILD_DATE` produces the same bytes. That is what makes a published
checksum meaningful, so it is verified per artifact rather than assumed:

| Artifact | Byte-identical across builds? |
| --- | --- |
| The binary (`linux/amd64`, `linux/arm64`) | **yes** — verified |
| `.deb` packages | **yes** — verified |
| `.tar.gz` binary archives | **no** — see below |
| Arch package via `makepkg` | **no** — see below |

What is pinned, and does work:

- `-trimpath` removes local filesystem paths from the binary.
- `-buildvcs=false` stops the toolchain stamping a dirty-tree flag into it.
- `SOURCE_DATE_EPOCH` fixes every mtime written into an archive.
- File order inside archives is sorted rather than left to directory order.

The two that are not byte-identical, and why:

- **The `.tar.gz` wrappers.** The extracted *contents* are identical, and so are the
  tar listing — names, modes, owners, sizes, mtimes — and the gzip header; the
  compressed stream still differs. The cause has not been isolated, so it is recorded
  here rather than papered over. The binary inside *is* reproducible, so the practical
  consequence is only that the archive's own checksum differs between two builds of
  one commit.
- **The Arch package.** `makepkg` writes its own `builddate` into `.PKGINFO` and
  `.BUILDINFO`, so the package records when it was built. That is makepkg's behaviour
  and the recipe does not override it.

Pin a release explicitly:

```sh
SOURCE_DATE_EPOCH=$(git log -1 --format=%ct) \
VERSION=1.0.0 COMMIT=$(git rev-parse HEAD) BUILD_DATE=2026-10-04T00:00:00Z \
  make release
```

`make verify-release` rebuilds and re-compares the checksums. It currently reports a
difference for the two artifact kinds above; it is a diagnostic, not a gate.
### The Debian package is built by hand

`scripts/mkdeb.sh` writes the `ar` archive directly instead of shelling out to
`dpkg-deb`, so a release can be produced on a machine that has Go and tar but no
Debian packaging tools. It also means timestamps are pinned rather than taken from
the filesystem, which is what `dpkg-deb` would not give you.

### The Arch package is built by makepkg where possible

`scripts/mkarch.sh` prefers `makepkg`, which validates the `PKGBUILD` itself, and
falls back to constructing the archive directly when Arch tooling is absent so a
Debian CI runner can still produce it. It builds in a copy of the recipe rather than
in the checkout — see the troubleshooting note below for why that matters.

---

## Releases

Releases are cut by tagging. Nothing is published automatically except as a
consequence of pushing a tag.

```sh
git tag -a v1.0.0 -m "v1.0.0"
git push origin v1.0.0
```

That triggers [`.github/workflows/release.yml`](.github/workflows/release.yml):

1. runs the same checks as CI — format, vet, race tests, lint, pty smoke test
2. builds Linux amd64 and arm64 binaries
3. generates SHA256 checksums
4. builds `.deb` for amd64 and arm64
5. builds the Arch package, and validates the `PKGBUILD` with `makepkg` on a real
   Arch runner
6. verifies the artifacts — including that the arm64 archive really contains an
   aarch64 binary, and that `--version` works from the packaged binary
7. creates the GitHub Release and uploads everything

Versions follow [Semantic Versioning](https://semver.org/). The tag is the single
source of truth for the embedded version, so a package and a binary from the same tag
always agree.

---

## Backup

```sh
./scripts/backup.sh [output-directory]
```

Produces `cli-zapp-backup-YYYYMMDD-HHMMSS.tar.zst` plus a `.sha256` beside it.

The contents come from `git archive` and `git bundle`, not from the filesystem. An
exclude list only covers what somebody remembered to enumerate, so the first time a
`.env` or a session database lands in the tree it goes into a backup that then gets
uploaded and shared. Deriving the archive from git makes an untracked secret
*structurally* unable to be included rather than merely excluded by policy.

A credential-pattern scan runs before the archive is written, and refuses to produce
one if it matches.

| Inside | |
| --- | --- |
| source tree | every tracked file at the recorded commit |
| `repo.bundle` | full history, branches and tags, deduplicated |
| `MANIFEST.txt` | commit, describe, and how to restore |
| `UNTRACKED.txt` | working-tree files deliberately **not** included |
| `SHA256SUMS.txt` | a hash of every file in the archive |

Restoring:

```sh
zstd -dc cli-zapp-backup-*.tar.zst | tar -xf -   # source and metadata
git clone repo.bundle cli-zapp                    # history
```

`UNTRACKED.txt` is the honest part: **a backup is not a substitute for committing.**
Anything listed there is in neither the archive nor the history.

---

## Uninstall

```sh
sudo apt remove cli-zapp          # from a .deb
sudo pacman -Rns cli-zapp         # from the Arch package
sudo make uninstall               # from make install
```

By hand, if you installed the binary yourself:

```sh
sudo rm -f /usr/local/bin/cli-zapp
sudo rm -rf /usr/local/share/doc/cli-zapp
```

cli-zapp writes nothing outside your home directory. There is no daemon to stop and
no system service to disable, so removal leaves nothing behind.

---

## Troubleshooting

**The screen looks garbled, or the colours are wrong.**
`TERM` is probably unset or unknown; check with `infocmp`. On a terminal without
truecolor the theme degrades rather than emitting colour it cannot render.

**Nothing happens when I press a key.**
Terminals disagree about which modifier they send. cli-zapp reads the modifier field
of the key event rather than parsing a rendered string, which handles the common
cases, but some terminals send `shift+enter` as a plain `enter`. Compare
`cli-zapp --help-keys` with your terminal's documentation.

**Copying does nothing.**
cli-zapp shells out to a clipboard helper and says which one it wanted when none
works:

```sh
sudo pacman -S wl-clipboard      # Wayland
sudo apt install xclip           # X11
```

It does not assume `DISPLAY` is set and does not require X11 — on Wayland it uses
`wl-copy`.

**The interface says "terminal demasiado pequeña".**
It needs at least 80x24. Make the window larger, or reduce the font size.

**It hung when I ran it in a pipe or a script.**
A Bubble Tea application needs a terminal. There is no headless mode.

**`make build` fails with a permission error under `packaging/`.**
An earlier `makepkg` run left an unreadable scratch directory in the checkout. `go
build ./...` walks the whole tree, so that breaks the Go toolchain too.
`scripts/mkarch.sh` now builds in a copy precisely so it cannot recur; remove the
leftovers by hand with:

```sh
sudo chmod -R u+rwX packaging/arch/pkg packaging/arch/src
rm -rf packaging/arch/pkg packaging/arch/src
```

**Is my account safe?**
No, and nothing in this project can make it so. Read [SECURITY.md](SECURITY.md)
before linking an account.

---


The binary is built with `CGO_ENABLED=0` and is statically linked — `file bin/cli-zapp`
says so, and CI asserts it. When the local database arrives it will be provided by
[modernc.org/sqlite](https://modernc.org/sqlite), a pure-Go implementation, precisely
so that this stays true: a chat client that needs a matching libc is a chat client
somebody cannot run.

---

## Nothing is hardcoded

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
| 2 | Packaging: Makefile, Arch, Debian, release workflow, backup | done |
| 3 | `whatsapp/adapter`: store, client lifecycle, all six services | done |
| 4 | Pairing, `--live`, the send rate limiter | done |
| 5 | **History sync**, so the conversation list populates | next |
| 6 | Persistence in the UI: offline history, search index | |
| 7 | Clipboard paste: bracketed paste, `ctrl+shift+v`, native selection | |
| 8 | Media download, keyring, voice notes | |

The whole of Phase 1's scope is built and tested, including the features the original
plan deferred to Phases 5 and 6 — reactions, edit, delete, reply, groups, search,
pin/mute/archive and the context menus. They were built against the in-memory fake,
which is what they were designed to be testable against, so landing the adapter is a
matter of filling in one package rather than writing the interface twice.

---

## Licence

**MIT.** See [LICENSE](LICENSE).

The protocol library, [whatsmeow](https://pkg.go.dev/go.mau.fi/whatsmeow), is MPL-2.0.
MPL-2.0 is file-level copyleft, so using it does not oblige this project's own source
to change licence — which is why cli-zapp can be MIT. cli-zapp does not vendor or
modify whatsmeow; it imports it as a module dependency, so the file-level obligation
is satisfied by the module boundary itself.

cli-zapp is not affiliated with, endorsed by, or connected to WhatsApp or Meta.
"WhatsApp" is a trademark of its respective owner.

This project uses an unofficial protocol and violates WhatsApp's Terms of Service.
**Read [SECURITY.md](SECURITY.md) before linking an account**, and see
[docs/LIMITATIONS.md](docs/LIMITATIONS.md) for what the protocol cannot do.

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) and
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
