# Limitations

What wterm cannot do, and what might go wrong. Written before implementation, as
the brief required, and kept current as things land.

## The one that matters

**wterm speaks an unofficial protocol, and using it carries a non-zero risk of
an account ban.**

WhatsApp's Terms of Service prohibit unauthorised automated access. There is no
API for personal accounts, so any third-party client is in that category. Being
straight about the risk:

- **Practical exposure is low.** wterm sends what a human sends, at human volume.
  The Matrix and Signal bridges built on whatsmeow have served thousands of
  accounts for years.
- **The risk is not zero, and cannot be made zero.** Nobody can promise an
  unofficial client will never be detected. Accounts used for automation have
  been restricted.
- **The consequences are asymmetric.** A ban means re-verifying a number,
  possibly losing access to a chat history.

**Use a secondary number.** It costs nothing and removes the risk to the account
that matters.

## Protocol limitations

These come from whatsmeow and are not fixable in wterm:

| Limitation | Detail |
| --- | --- |
| **Calls** | Not implemented upstream. Hard boundary, not a backlog item. |
| **Broadcast lists** | Not implemented upstream; WhatsApp Web cannot send to them either. |
| **Voice notes** | Receiving works. Sending requires hand-building the protobuf message; deferred to a later phase rather than shipped half-working. |
| **"Last seen" and online** | Depends on the *other* user's privacy settings. If they hide it, there is no data to display. This is correct behaviour, not a bug. |
| **Protocol breakage** | WhatsApp can change the protocol at any time. whatsmeow tracks it actively, and a major change needs an immediate library update. |
| **No stability guarantee** | The library has no semantic version tags — only pseudo-versions like `v0.0.0-20260929…`. See below. |

### Why dependencies are vendored

whatsmeow publishes **no git tags**. `go get` yields a pseudo-version pinned to a
commit date, and there is no `v1.2.3` to ask for. That makes a plain `go build`
non-reproducible in practice: any upstream commit can change what a fresh build
resolves to.

wterm therefore vendors dependencies and commits `vendor/`, so a given commit of
this repository always builds the same bytes. `make vendor` refreshes it.

## Phase 1 scope

Implemented and tested: the interface, the keybinding system, the domain model,
the theme, and all the components. Not yet implemented:

- **The protocol adapter.** The application runs against `whatsapp.Fake`. `--demo`
  is the default, and will become opt-in when the adapter lands.
- **Persistence.** `internal/storage` is a Phase 2 deliverable; no database is
  written yet.
- **Media handling.** The service interface and the download path exist; nothing
  calls them.
- **Configuration files.** Flags only. The TOML loader is Phase 2.
- **Full-text search across history.** Search covers chat names and the latest
  message. Searching all history needs an index, which is Phase 2 with the
  database. `TestSearchDoesNotMatchOlderMessagesYet` records the boundary.
- **Group member names.** The roster is modelled but not yet attached to the
  transcript, so a group sender shows their identifier rather than their push
  name. Phase 6.
- **Inline images.** Terminal graphics need Sixel, Kitty or iTerm support, or an
  external renderer such as `chafa`. Media is described in text; the fallback
  path works everywhere. The renderer is Phase 7.

## Terminal limitations

| Terminal | What works | What does not |
| --- | --- | --- |
| Any with truecolour | Full colour | — |
| 256-colour | Downsampled automatically | — |
| 16-colour | Usable, muted | Subtle distinctions blur |
| No Unicode | `--glyphs=ascii` | Emoji fall back to ASCII |
| Narrow (< 60 columns) | Sidebar collapses; conversation only | No two-column layout |
| Short (< 10 rows) | Explanation rather than a scrambled render | — |
| Mouse | Click to select, wheel to scroll, right-click for the menu | — |

Colours degrade through lipgloss's downsampling rather than being hardcoded to
ANSI indices, which is what keeps the interface legible on a limited palette.

## Known rough edges

Honest about what is not finished:

- **Composer height is fixed.** A long draft scrolls inside a three-line box
  rather than growing the pane.
- **Search is filtered client-side.** Correct for a few thousand messages; an
  index will be needed for a large history.
- **No unread markers inside a transcript.** The chat list shows counts, but
  scrolling back does not mark where you stopped reading. WhatsApp Web does.
- **Reaction entry is a fixed palette** rather than free emoji, because a picker
  has to be operable in a terminal.
- **Clipboard needs a helper binary** (`wl-clipboard`, `xclip` or `xsel`). The
  OSC 52 escape would avoid the dependency but fights the renderer for the tty;
  see the comment in `internal/ui/app/clipboard.go`.

## Bugs this project found in itself

Kept here because they are the kind of defect that is invisible until something
concrete breaks, and because each one has a regression test:

1. **Every `ctrl` shortcut was dead.** Bubble Tea's `KeyPressMsg.String()` returns
   `"j"` for ctrl+j, dropping the modifier. Parsing that string produced an
   unbound key. Fixed in `internal/ui/app/keys.go` by reading the modifier field.
2. **Typing did nothing.** The composer declared a local interface for the key
   message to avoid importing Bubble Tea, but `textarea.Update` type-switches on
   the concrete type, so a wrapper struct was silently ignored.
3. **Truecolour escapes leaked into width calculations.** The first ANSI scanner
   mishandled Lip Gloss's `\x1b[38;2;17;27;33m`, corrupting every column
   measurement. Fixed by a state machine over the full escape grammar.
4. **Emoji were eaten.** The scanner tested every *byte* for 8-bit C1
   introducers, and `0x9f` — a UTF-8 continuation byte — matched APC.
5. **Global shortcuts stopped working while typing.** The composer consumed keys
   before the binding map was consulted. Fixed by resolving global bindings first.
6. **Three keybinding collisions in the defaults**, caught by `Map.Conflicts`:
   `ctrl+d`, `ctrl+f` and `ctrl+n` each meant two things.
7. **Every desktop notification was killed instantly.** `exec.CommandContext`
   kills the child as soon as its context is done, and `defer cancel()` fired
   immediately after `Start`. Found by a test that stubbed `notify-send` and
   waited for its output.
