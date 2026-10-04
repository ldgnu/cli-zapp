# Protocol

How cli-zapp talks to WhatsApp, and why this way. Written before implementation, as
the brief required; verified against the library's source rather than its README.

## The constraint

**There is no official API for a personal WhatsApp account.** Meta exposes the
Cloud API for *businesses* to message *their own customers*, and nothing else.
Any client for a personal account is speaking a protocol WhatsApp never
documented.

That shapes every option below.

## Options evaluated

| Option | Viable? | Why |
| --- | --- | --- |
| **WhatsApp Business Cloud API** | No | Verified business only. No chat list, no presence, no personal messages. Message templates require prior approval. Cannot replace a normal WhatsApp client. |
| **An official personal API** | No | Does not exist. |
| **whatsmeow** (`go.mau.fi/whatsmeow`) | **Yes** | Go implementation of the WhatsApp Web multidevice protocol. Active: last commit days before this was written. Used in production by Matrix and Signal bridges serving thousands of users. |
| **whatsapp-web.js / Baileys** | No | Node, not Go. Automates a browser session — fragile, and prohibited by WhatsApp's terms. |
| **Scraping a browser** | No | Explicitly excluded by the brief, and rightly so. |
| **Desktop-app automation** | No | Same category as scraping. |

whatsmeow is the only serious option in Go.

## Verified capabilities

Checked against the library's exported API, not its feature list:

```
✅ send / receive text and media        ✅ edit    Client.BuildEdit
✅ groups: members, admins, invite      ✅ revoke  Client.RevokeMessage
   links, topic, description, leave     ✅ react   Client.BuildReaction
✅ typing and recording indicators      ✅ receipts (sent/delivered/read)
✅ presence and last-seen               ✅ app state: pinned, muted, archived
✅ media download                       ✅ pairing by phone number
❌ calls                                ❌ broadcast lists
❌ voice notes as a first-class API
```

Two details matter for the design:

- **Pairing by phone number.** `Client.PairPhone(ctx, phone, ...)` returns an
  8-digit code. No QR scanner is needed, which is ideal for a terminal. The QR
  path stays as a fallback.
- **The device store is driver-agnostic.** `store/sqlstore` uses plain
  `database/sql` via `go.mau.fi/util/dbutil`, so `modernc.org/sqlite` works. That
  is what makes a `CGO_ENABLED=0` static build possible, and it means the same
  code would work against Postgres if a second device ever needed to share state.

## Consequences for the architecture

Because the protocol is unstable and unversioned, it is confined to one package
behind narrow interfaces:

```
internal/whatsapp/services.go   interfaces, in terms of models.*
internal/whatsapp/adapter/      the only package importing go.mau.fi/whatsmeow
```

The UI imports `models` and the interfaces, never `whatsmeow`. When the protocol
changes — and it will — the blast radius is one directory. Phase 1 exists to make
that boundary real before there is anything behind it: the entire interface is
built and tested against `whatsapp.Fake` with the protocol absent from the
dependency graph.

### The device store and cli-zapp's own database

Two stores, deliberately separate:

- **whatsmeow's** holds cryptographic material: identity keys, prekeys, session
  state. Its schema is the library's to own, and it must be readable by it.
- **cli-zapp's** holds application state: message cache, chat metadata, UI state,
  configuration.

Merging them would couple cli-zapp's migrations to an upstream schema that changes
with protocol updates. Credentials go to the **system keyring** where one is
available; the device store holds only what the library needs, `0600`.

## Ordering constraints

Verified from the API rather than assumed:

- **`PairPhone` must be called after `Connect` and before `IsLoggedIn`.** The
  login can take minutes on a cold start; the UI must show progress rather than
  appear hung.
- **Sending requires a live connection.** Messages are stored as pending first and
  confirmed through events, never optimistically marked sent. `whatsmeow.Fake`
  models this with a delayed confirmation so the pending state is testable.
- **Events arrive as a single ordered stream.** Draining one channel rather than
  several avoids the interleaving bugs that appear when a presence update races a
  message on different channels.

## What is deliberately not attempted

- **No browser automation, and no scraping.** Explicitly excluded, and the wrong
  tool regardless.
- **No calls.** whatsmeow does not implement them. This is a hard boundary, not
  a backlog item.
- **No broadcast lists.** Also unimplemented upstream; WhatsApp Web itself cannot
  send to them.
- **No multi-device-coordination.** Linking a second device is out of scope; one
  account, one terminal.

## Before linking an account you care about

Read [LIMITATIONS.md](LIMITATIONS.md). In short: this is an unofficial protocol
and using it carries a non-zero risk of an account ban. A secondary number costs
almost nothing to acquire and removes that risk.
