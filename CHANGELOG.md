# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `cli-zapp pair --phone <número>` — links this device to an account.
- `cli-zapp unpair` — unlinks it.
- `cli-zapp keys` and `cli-zapp help` — subcommands rather than flags, because
  `pair` and `unpair` change something irreversible on someone else's account and
  should be something you type deliberately.
- `--live` — runs against a linked account. Still opt-in: pairing is irreversible and
  a program that contacts WhatsApp on startup without being asked would be a bad
  neighbour on a shared machine.

The pairing screen shows the ban risk **before anything touches the network**, names the
worse case (the account losing the ability to link any client at all, which is what
happened to another vendor's users for five months), and asks. `--yes` skips it for
scripts that have already made the decision.

A closed stdin counts as "no". The prompt guards an irreversible action, so nobody
answering must resolve to doing nothing.

### Fixed

- **A first run failed with a SQL error.** `sqlstore.NewWithDB` only wraps a
  connection; the schema is created by `Upgrade`, which is called separately. Without
  it, `GetFirstDevice` fails with "no such table" and a first-time user sees a database
  error instead of a pairing prompt.
- **The device store was world-readable.** SQLite creates a database 0644 minus the
  umask, and the chmod was placed after `sql.Open` — which is lazy and does not create
  the file, so it was a silent no-op on exactly the installs where it mattered. Now
  0600, applied after the migration.
- **`--phone` only worked with an equals sign**, so the form printed in the warning
  itself did not work.
- **The Arch package was not traceable to a commit.** The `.deb` path passes
  `main.commit` and `main.buildDate`; the PKGBUILD hardcoded only `main.version`, so an
  installed Arch copy reported `commit: unknown` and `built: unknown` — precisely the two
  fields SECURITY.md tells a user to quote in a bug report, and precisely what you need
  to tell whether an installed copy is affected by a fix. Caught by unpacking the
  package and asking the binary, not by anything the build reported.
- **A pairing code was chunked raggedly.** Fixed groups of four stranded a lone letter
  when the length was not a multiple of four; the real code is eight characters. Split
  into two equal halves instead, which works whatever the length.

### Known issues

- **No QR pairing.** The pairing-code route works everywhere including over SSH. A QR
  encoder is a few hundred lines that cannot be verified here — nothing in the
  repository can scan a code, so a subtly wrong encoder produces a picture that looks
  right and does not scan, and the user cannot tell the code is broken. Adding it later
  means a vetted dependency or a golden-vector test against another implementation.
- **No history sync yet.** `--live` connects and receives events, but the conversation
  list is empty because the set of chats *is* app state and nothing fetches it. That is
  the next piece of work.

## [0.1.0] - 2026-10-04

First tagged release. The interface and the release tooling; no account is linked
yet.

### Added

**Interface**

- Three-zone responsive layout — sidebar, transcript, composer — with full,
  minimal and too-small tiers, from 40x10 upward.
- Conversation list with search, unread counts, pinning, muting and archiving.
- Message transcript with per-sender run collapsing, one timestamp per run,
  incoming left / outgoing right alignment, revoked-message tombstones and reaction
  summaries.
- Composer with multiline input, reply quoting, edit-in-place, draft persistence,
  and message states (pending, sent, delivered, read, failed).
- Status bar with connection state, presence and contextual hints; sidebar footer
  answering "what can I do with this conversation".
- **Command palette** on `ctrl+p`: fuzzy subsequence search with exact, prefix,
  word-boundary and substring ranking ahead of it, `j`/`k` navigation, categories,
  per-command shortcuts, availability shown as greyed rather than hidden, and the
  highlighted command's description below the list.
- Overlays: help sheet, confirmation dialogs, context menus, contact picker,
  reaction picker.
- Message actions on bare keys: `r` reply, `R` react, `e` edit, `d` delete,
  `f` forward, `y` copy, `x` select.
- Mouse support throughout, with the whole interface usable without it.
- ASCII glyph set for terminals without Unicode coverage.

**Architecture**

- Centralised, configurable keybinding table with startup conflict detection and
  no hardcoded shortcuts in any component.
- Region contract: each region owns its state, emits events, and never mutates a
  sibling. The root model owns every decision and every pixel.
- Geometry as a pure function of `(width, height)`, tested exhaustively from
  40x10 to 200x60.
- Terminal-text layer that measures in cells rather than bytes, wraps on word
  boundaries and is escape-sequence aware.
- `whatsapp.Fake` — a full in-memory implementation of every service interface, so
  the entire interface builds and tests with no protocol dependency present.

**Protocol** (researched and documented; adapter not yet written)

- `docs/PROTOCOL.md` rewritten against primary sources: Meta's own documentation
  and whatsmeow's exported symbols, verified 2026-10-04.
- whatsmeow chosen over the Cloud API, Baileys, browser automation and relay
  providers, with the reasoning recorded.
- Capabilities that cannot be implemented are marked **NOT POSSIBLE** (calls
  beyond declining one, broadcast lists) or **LIMITED** (status posting, pairing
  window, history completeness), each with the contract that absorbs it.

**Build and release**

- Static `CGO_ENABLED=0` binary; no libc to match.
- Makefile: `build`, `install`, `uninstall`, `test`, `lint`, `fmt`, `clean`,
  `release`, `package`, `package-arch`, `package-deb`, `checksums`,
  `verify-release`, `run`, `check`, `keys`, `frame`, `pty`.
- Reproducible builds: `-trimpath`, `-buildvcs=false`, and `SOURCE_DATE_EPOCH`
  honoured by every archive.
- `cli-zapp --version` reporting version, commit, build date, Go version and
  platform.
- Arch `PKGBUILD` at `packaging/arch/`, validated by `makepkg` on an Arch runner.
- Debian `.deb` for amd64 and arm64, built as `ar` archives so no Debian tooling
  is required.
- Release workflow triggered by a `v*` tag: verify, build both architectures,
  SHA256 checksums, `.deb` and Arch packages, artifact verification that the arm64
  archive really contains an aarch64 binary, and GitHub Release creation.
- `scripts/backup.sh`, producing a zstd archive plus SHA256. Its contents are
  derived from `git archive` and `git bundle`, so an untracked secret cannot enter
  it; a credential-pattern scan refuses to produce an archive that would.

### Fixed

Bugs found by the test suite during development, each now covered by a
regression test:

- **`KeyPressMsg.String()` dropped modifiers.** It returns `"j"` for `ctrl+j`, so
  parsing it produced unbound keys and every `ctrl` shortcut was dead. Key
  conversion reads the modifier field instead.
- **The conflict detector could not report a conflict.** `Conflicts` compared
  every claim against only the first, so a key claimed twice in one panel and once
  in another looked unambiguous. `ctrl+e` was bound to both "edit message" and
  "scroll down" while startup reported nothing.
- **A command palette that could not run a command.** The application routed only
  printable key presses into the palette and dropped the rest, so `enter`, `down`
  and `ctrl+j` never arrived: the list could not be navigated and nothing could be
  chosen. The palette's own tests passed throughout, because they drove the
  component directly and never went through the routing.
- **A frame-test harness that hid the above.** `send` discarded the `tea.Cmd` that
  `Update` returns, so any path reaching the model through an event was invisible.
  A test asserting "choosing the help command opens the help sheet" had been
  passing while the sheet never opened, because both strings it looked for were
  rendered by the palette it had failed to close.
- **Editing that never worked.** Compose-mode fields were cleared before the
  command reading them was built, so an edit was sent with an empty message
  identifier and refused by the service.
- **A conversation that changed under the cursor.** Pinning re-sorts the list and
  the selection was an index, so pinning silently opened a different conversation.
  Selection is now remembered by identifier.
- **A status bar pushed off screen.** The header divider was one row too tall.
- **Every shortcut wrapping to its own line.** Two Lip Gloss traps at once: `Width`
  includes the border, and `maxInt`'s second argument is the fallback rather than
  the minimum.
- **Sidebar click coordinates applied twice**, adding `list.Y` to a position that
  already included it.

### Known issues

Deliberately not fixed in this release:

- **Shutdown is skipped when the program exits with an error.** `Shutdown` runs
  after a successful `Run` and is not reached on the error path, so the sync
  service is not stopped there.
- **In-flight service calls are not cancelled on exit.** `Model.ctx` returns
  `context.Background()`, so nothing cancels outstanding calls when the program
  ends. Harmless today because the process exit does it, but it is not what the
  audit in `docs/LIMITATIONS.md` asks for.
- **Clipboard paste is not implemented.** Copy works (`wl-copy`, `xclip`, `xsel`,
  `pbcopy`, `clip.exe`). Bracketed paste and `ctrl+shift+v` do not.
- **No protocol adapter.** The program runs against demo data; it cannot yet link
  an account. This is the next phase, not a defect.
- **Two release artifacts are not byte-reproducible.** The binaries and the `.deb`
  packages are; the `.tar.gz` wrappers and the `makepkg` Arch package are not. For
  the latter the cause is known — makepkg writes its own `builddate`. For the former
  the tar listing, contents and gzip header are all identical and the compressed
  stream still differs, so the cause is unisolated and `make verify-release` reports
  it rather than gating on it. See the reproducibility table in README.md.
- **Packaging is Linux-only.** `scripts/mkdeb.sh` builds an `ar` archive by hand and
  `scripts/mkarch.sh` falls back to direct construction when `makepkg` is absent, so
  both run on a Debian CI runner. Only Linux amd64 and arm64 are built.

[Unreleased]: https://github.com/ldgnu/cli-zapp/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/ldgnu/cli-zapp/releases/tag/v0.1.0