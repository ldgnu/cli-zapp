# Contributing

Thanks for considering it. This document is about what makes a change easy to
review rather than hard.

## Before you start

**This project speaks an unofficial protocol.** Please read
[SECURITY.md](SECURITY.md) first — particularly the section on account bans. If you
are not willing to accept that, contributing is not a good use of your time.

Then read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Most rejected pull requests
in a project like this are rejected because they were correct in isolation and
wrong in context: a shortcut added to a component instead of the binding table, a
piece of behaviour in a region that should emit an event, or WhatsApp protocol
knowledge leaking into the UI.

## Getting set up

```sh
git clone https://github.com/ldgnu/cli-zapp.git
cd cli-zapp
make build
./bin/cli-zapp
```

The binary starts against in-memory demo data, so there is nothing to configure and
no account to link.

You need Go 1.27 or later, and a Linux terminal at least 80x24. `make tools`
installs `golangci-lint`.

## The loop

```sh
make build      # build into bin/
make run        # build and run with demo data
make test       # unit tests with the race detector
make lint       # golangci-lint
make fmt        # gofmt
make check      # everything CI runs, including a real-pty smoke test
```

`make check` is the one to run before opening a pull request. It includes the pty
smoke test, which drives the real binary through a real pseudo-terminal — a Bubble
Tea program cannot be exercised any other way, and several classes of defect only
appear when there is a tty.

### Looking at a frame

Rendering bugs are much easier to diagnose as text than as a screenshot:

```sh
make frame COLS=100 ROWS=28 [KEYS="ctrl+p esc"]
```

That prints the exact frame, with ANSI stripped. Regions never compute their own
geometry — `internal/ui/layout` is a pure function of `(width, height)` and returns
rectangles — so a layout question usually has an answer in one function.

## Rules that are not negotiable

These are enforced by `make check`, by `.golangci.yml`, or by tests. A change that
breaks one will not merge.

**No hardcoded shortcuts in components.** A key press is resolved to an action by
the table in `internal/keybindings`, and a component is handed the action. If you
find yourself comparing a key string inside a component, the change is in the wrong
place.

**Regions emit events; the root decides.** A region never mutates a sibling and
never interprets anything about WhatsApp. If a region needs something to happen, it
returns an event and the root model handles it.

**The UI must not import the protocol.** `internal/ui` talks to six interfaces
expressed in terms of `models` types. It must not import `go.mau.fi/whatsmeow`. The
whole interface builds and tests against `whatsapp.Fake`.

**Geometry is computed in `internal/ui/layout`.** Regions are handed rectangles.
None of them should be doing arithmetic on terminal dimensions.

**No credentials, tokens or message content in the repository or the logs.** Logs
record lifecycle events, errors and state transitions. Not content.

**gofmt and `golangci-lint` clean.** Zero issues, not "no new issues".

## Tests

Tests are part of the change, not a follow-up.

- **Test behaviour through the root model, not the component**, when the claim is
  about the application. A component test cannot see whether the application routes
  keys to it — and that routing was exactly where the palette's broken `enter` hid,
  behind a green component test suite.
- **Drive the commands `Update` returns.** In the app package, `send` and the
  harness `settle` run them. A test that skips that loop never reaches a send, and a
  service path nobody reached is a service path nobody has run.
- **Assert on what the user sees**, and prefer asserting on text that only one thing
  produces. A test that searches for a command's title will also find it in the
  palette's own list — which is how "this opens the help sheet" passed while the
  sheet never opened.
- **Cover the layout at several sizes**, not one. Every layout invariant is checked
  from 40x10 to 200x60.

## Commit messages

One logical change per commit. Describe why, not what — the diff already says what.

```
Fix the conflict detector comparing only the first claim

Conflicts compared each binding against the first one in the panel rather than
against each other, so a key claimed three times looked unambiguous. ctrl+e was
bound to both "edit message" and "scroll down" while startup reported nothing.
```

## Pull requests

- One topic per pull request.
- Explain the problem before the solution.
- Note anything you deliberately did not do, and why. "I left the wrapping alone
  because the width is already clamped upstream" is useful; silence is not.
- CI must be green. It runs format, vet, race tests, lint and the pty smoke test,
  and the same checks gate the release workflow.

## Adding a capability that WhatsApp Web has

Check [docs/LIMITATIONS.md](docs/LIMITATIONS.md) and [docs/PROTOCOL.md](docs/PROTOCOL.md)
first — several are marked **NOT POSSIBLE** or **LIMITED** upstream.

If a capability is genuinely unavailable, the contribution is not to hide it. It is
to represent it in the model as an explicit state. The pattern throughout is that an
unavailable capability is never an error and never a missing affordance the user has
to discover: the pairing view owns its 160-second deadline, undecryptable media
renders its text and a marker, and unknown presence renders nothing rather than
guessing "offline".

## Licence

Contributions are accepted under the [MIT licence](LICENSE). Note that the protocol
library is MPL-2.0, which is file-level copyleft and does not oblige this
project's own source to change licence.

By contributing you agree that your contribution is licensed under MIT.