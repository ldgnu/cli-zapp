# Protocol

How cli-zapp talks to WhatsApp, and why. Every API name below was checked against
the library's exported symbols or against Meta's own documentation on
2026-10-04. Where a claim could not be verified from a primary source it is
marked **[unverified]** rather than asserted.

## The constraint

**There is no official API for a personal WhatsApp account.**

Meta's Cloud API documentation is addressed to businesses and contains this
disclaimer on its own getting-started page:

> This guide is for developers building on the WhatsApp Business Platform. If
> you are a WhatsApp user experiencing issues with your **personal account**,
> visit the WhatsApp Help Center.

Third-party integrations say the same thing in their own words. Odoo, for
instance: *"Personal WhatsApp accounts and WhatsApp Business App accounts are
not compatible with the Odoo WhatsApp integration."*

There is no roadmap document, beta or otherwise, that changes this. Any client
for a personal account speaks a protocol WhatsApp never documented and does not
support. That shapes every option below.

---

## Comparison

| Solution | Type | Go support | QR login | Personal chats | Messages | Media | Groups | WebSocket | Maintenance | Risk | License |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| **whatsmeow** (`go.mau.fi/whatsmeow`) | Protocol library | native | yes + pairing code | **yes** | full | up/download | full, incl. communities | yes (the transport) | very active — `v0.0.0-20260929112325`, 301 importers | ban / ToS | MPL-2.0 |
| **WhatsApp Business Cloud API** | Official HTTP API | via SDK | no | **no** | business-initiated only, templates outside 24 h | upload/download | limited, entitlement-gated **[unverified]** | no — webhooks | Meta-supported | low | proprietary |
| Business Management API | Official HTTP API | via SDK | no | no | no — asset management | no | no | no | Meta-supported | low | proprietary |
| **Baileys** | Protocol library (Node/TS) | no | yes | yes | full | yes | yes | yes | active, but heavy forking churn | ban / ToS | MIT |
| **whatsapp-web.js / WPPConnect / OpenWA** | Browser automation | no | via browser | yes | full | yes | yes | no (Puppeteer) | active | ban / ToS, + fragile | MIT |
| **gomuks** | Full desktop client, on whatsmeow | native | yes | yes | full | yes | yes | via whatsmeow | active — `v0.2607.0` | ban / ToS | AGPL-3.0 |
| **wacli** | Terminal CLI, on whatsmeow | native | yes | yes | full | yes | yes | via whatsmeow | active | ban / ToS | MIT |
| **hypermeow** | Fork of whatsmeow | native | yes | yes | full | yes | yes | yes | newer — `2026-08-11`, one vendor | ban / ToS | MPL-2.0 / MIT |
| REST gateways on whatsmeow (GOWA, zapmeow, apime, evolution-go, whatsapp-ws) | HTTP/WS *server* | server in Go | yes | yes | full | varies | varies | yes (server side) | varies | ban / ToS, **+ vendor sees your session** | mostly MIT |
| Relay providers (Whapi.Cloud, Wati, Green API, Evolution API) | Hosted gateway | client SDKs | yes | yes | full | yes | yes | yes (to *them*) | commercial | ban / ToS, **+ third party holds the session** | commercial |
| Cloud API Go SDKs (`piusalfred/whatsapp`, `gowhatsapp`) | Official-API clients | native | no | no | business only | yes | limited | no — webhooks | small | low | MIT |
| Browser/DevTools automation | Scraping | no | via browser | yes | fragile | fragile | fragile | no | n/a | ban / ToS | n/a |

Notes on the table:

- **"ban / ToS" is not a formality.** See the risk section below.
- The REST gateways and relay providers are *not* alternatives to whatsmeow —
  most are built **on top of it**. Adopting one adds an operator between your
  messages and WhatsApp without removing the ToS problem.
- **hypermeow** is a fork of whatsmeow with its own benchmark claims. For a
  single-account terminal client the measured differences (heap per client,
  group-send p95) do not apply, and it trades an upstream with 301 importers for
  a newer fork with one.

---

## What the Cloud API can and cannot do

**Can:** send and receive messages, media and interactive messages; receive
status over webhooks; manage WABA assets, templates and billing; make and
receive **calls** (WhatsApp Calling API — Meta's first public voice API, with
SIP support under the 2026 account model); per-message pricing.

**Cannot:** read your personal chat list; see presence or last seen; see your
phone's contacts; receive messages from people who have never contacted the
business; send outside a 24-hour customer-service window without a pre-approved
template; link as a *device* and receive the account's own history.

**Groups** are the one place the official API has genuinely moved. Meta's
platform page now lists *"Groups: Create, manage, and message WhatsApp group
conversations."* But the groups surface is **entitlement-gated per phone
number**: a documented third-party toolkit reports that `createGroup` on a test
number was *"rejected with an entitlement error"* and that mutations remain
unavailable until a Groups-entitled number exists. Third-party providers
consistently report an **8-participant cap** versus 1024 in normal WhatsApp;
that figure is **[unverified]** against Meta's own documentation, which I could
not retrieve. Several sources also note WhatsApp Web itself cannot send to
broadcast lists, so the 1024-member group and a broadcast list are not the same
thing.

For cli-zapp this settles it regardless: **the Cloud API cannot replace a
personal WhatsApp client**, and that is the entire product.

---

## Verified capabilities of whatsmeow

Checked against the exported symbols of `v0.0.0-20260929112325-8b41cfe6d9c4`,
and cross-checked against the library's own README, which lists exactly two
gaps.

### Available

| Area | Verified symbols |
| --- | --- |
| Messaging | `SendMessage`, `BuildEdit`, `RevokeMessage`, `BuildReaction`, `BuildRevoke`, `SendChatPresence`, `MarkRead`, `SendPresence`, `SubscribePresence` |
| Media | `Upload`, `UploadReader`, `Download`, `DownloadToFile`, `DownloadThumbnail`, `DownloadAny`, `DownloadMediaWithPathToFile` |
| Groups | `CreateGroup`, `GetGroupInfo`, `GetJoinedGroups`, `SetGroupName`, `SetGroupTopic`, `SetGroupDescription`, `SetGroupPhoto`, `UpdateGroupParticipants`, `LinkGroup`, `LeaveGroup`, `JoinGroupWithLink`, `GetGroupInviteLink`, `SetGroupAnnounce` |
| Communities | `GetSubGroups`, `LinkGroup`, `UnlinkGroup` |
| Channels / newsletters | `CreateNewsletter`, `GetNewsletterMessages`, `FollowNewsletter`, `NewsletterToggleMute` |
| Polls, comments | `BuildPollCreation`, `BuildPollVote`, `EncryptComment` |
| App state | `FetchAppState`, `SendAppState` — contact list, pinned, muted, archived |
| History sync | `BuildHistorySyncRequest`, `DownloadHistorySync`, `ParseWebMessage` |
| Status | `SetStatusMessage` (upstream marks this *experimental*) |
| Pairing | `GetQRChannel`, `PairPhone` |

### NOT POSSIBLE

| Missing | Evidence |
| --- | --- |
| **Calls** — no make, no accept, no media | Upstream README, verbatim: *"Things that are not yet implemented: Sending broadcast list messages (this is not supported on WhatsApp web either), Calls."* The only call method on `Client` is `RejectCall`. |
| **Broadcast lists** | Same README line. `Client` has zero methods matching `broadcast`; `JID.IsBroadcastList()` exists only to *classify* an inbound JID. |
| **Status posting at scale** | Upstream: *"experimental, may not work for large contact lists."* Treat as LIMITED, not available. |

### LIMITED

| Area | What works | What does not |
| --- | --- | --- |
| **Calls** | Incoming call *notification* — `CallOffer`, `CallOfferNotice`, `CallAccept`, `CallReject`, `CallTerminate` are all received — and `RejectCall` to decline. | Cannot accept, place, or carry audio. A TUI can show "Ada is calling" and offer *decline*. Nothing else. |
| **Pairing** | `GetQRChannel` streams codes; the docs state the login websocket **closes after the QR codes run out, a 160-second limit**. `PairPhone` returns a code, but its length and expiry are **not documented**. | The TUI must not assume the pairing code's width, must show a countdown, and must let the user retry within the 160 s window. |
| **History** | On-demand sync via `BuildHistorySyncRequest`; WhatsApp Web history itself is best-effort. | No guaranteed full-history export. A fresh device may simply not have old messages. |
| **Contacts** | Read and write via app state; `GetUserInfo`, `GetProfilePictureInfo`, `IsOnWhatsApp`. | No per-contact blocking of the *local* name; names come from the account's own contact list, which may be empty. |
| **Device type** | `PairPhone`'s `clientDisplayName` must be formatted `Browser (OS)` and **the server validates it**, returning 400 otherwise. Only common browsers/OSes are allowed. | cli-zapp cannot present itself as "cli-zapp on Linux" during pairing. |

---

## Ban and ToS risk

This is not theoretical, and it is not only about bans.

WhatsApp's own Help Center, under *About account bans for unofficial apps*:

> Using an unauthorized application and/or unsupported device violates our Terms
> of Service and can result in your account being banned.

And under *About linked devices*:

> Linking your account to an unofficial app or website, **now or in the past**,
> may result in a temporary or permanent account ban.

Two things follow that belong in the design, not just the docs:

1. **whatsmeow surfaces bans as an ordinary event.** `events.TemporaryBan`
   carries `Code TempBanReason` and `Expire time.Duration`. The UI has to render
   an expiry countdown, because the alternative — a spinner — is what makes users
   think the app has hung and restart it, which is how a temporary ban becomes a
   permanent one.

2. **The worse outcome is losing linked devices entirely.** In late 2025 Meta
   flagged Telemessage as an unofficial app and blocked the affected *accounts*
   from creating linked-device sessions at all — including on the official
   web.whatsapp.com. The outage ran roughly September 2025 to February 2026, and
   during it no archiving product that needed a linked device worked. So the risk
   is not only "my number is banned" but "my number can no longer be used as a
   client anywhere."

Related: WhatsApp was reported in September 2026 to be testing a warning shown in
**Linked Devices** when the account is linked from an unofficial web client. It is
a warning, not a block — but it means the linked-device path is under active
scrutiny.

Widely repeated ban-rate figures (for example "8 million accounts per month")
come from blog posts, not from Meta. They are not cited here as fact.

---

## Choice

**whatsmeow.** It is the only Go library that speaks the WhatsApp Web multidevice
protocol directly, and it is actively maintained — the version resolved during
this investigation is dated one day before it, it has 301 importers, and it is
what mautrix-whatsapp, gomuks and wacli are built on, which is the strongest
available evidence that it keeps up with protocol changes.

Why not the others:

- **Cloud API** — cannot read a personal chat list. Not a WhatsApp client.
- **Baileys** — Node, and the wrong language for a Go project regardless.
- **Browser automation** — 200–500 MB of RAM, breaks when WhatsApp ships, and
  is categorically worse than speaking the protocol.
- **REST gateway or relay** — you are already accepting the ToS and ban risk;
  adding an operator who now holds your decrypted session is a strictly worse
  trade than owning the risk yourself.

MPL-2.0 matters for one decision: whatsmeow is file-level copyleft, so using it
does not force cli-zapp's own source into MPL. A GPL/AGPL alternative such as
gomuks would have made cli-zapp AGPL-obligated for anyone who linked it.

---

## Consequences for the architecture

The protocol is unversioned and undocumented, so it is confined to one package
behind narrow interfaces:

```
internal/whatsapp/services.go   interfaces, in terms of models.*
internal/whatsapp/adapter/      the only package importing go.mau.fi/whatsmeow
```

`internal/ui` must not import `whatsmeow`; that is enforced by the dependency
graph and the whole interface is built and tested against `whatsapp.Fake` — a
fake **this repository owns**, not one from whatsmeow, which has no test double
at all.

### Designing around the gaps

A missing capability must not become a broken one. Each limitation above maps to
a specific contract:

| Gap | How the system absorbs it |
| --- | --- |
| **Calls NOT POSSIBLE** | No call UI is rendered at all. `CallOffer` is translated into a transient "Ada is calling" row with a *decline* action, and no affordance that implies the app can talk. Absence of a feature is not a broken feature. |
| **Broadcast lists NOT POSSIBLE** | Not offered in the new-chat menu. A broadcast JID arriving inbound renders as a normal chat with its name, not as an error. |
| **Status LIMITED** | `SetStatusMessage` is not wired. Nothing in the UI advertises posting status. |
| **Pairing 160 s / unknown code length** | The pairing view owns a deadline and re-renders a countdown. The code is rendered with `text.VisibleWidth`, never with a hardcoded width, so a change in length cannot corrupt the frame. |
| **History best-effort** | A conversation that has not synced shows an explicit empty state distinguishing "no messages" from "not yet downloaded". Conflating them would make a working client look broken. |
| **Presence unknown** | Renders nothing, never "offline". Unknown and offline are different states and the model keeps them different. |
| **Media may be undecryptable** | `UndecryptableMessage` and `MediaRetryError` exist as events. A message whose media cannot be decrypted renders its text and an explicit marker, rather than an empty bubble. |

The common shape: **every unavailable capability is represented in the model as
an explicit state, never as an error, and never as a missing affordance the user
has to discover.**

### Two stores, deliberately separate

- **whatsmeow's** holds cryptographic material — identity keys, prekeys,
  session state. `sqlstore.NewWithDB(db *sql.DB, dialect string, log)` takes a
  plain `*sql.DB`, so `modernc.org/sqlite` works and `CGO_ENABLED=0` survives.
  Documented dialects are `sqlite3` and `postgres`.
- **cli-zapp's** holds application state: message cache, chat metadata, UI
  state, configuration.

Merging them would couple our migrations to an upstream schema that changes with
every protocol update.

### Ordering constraints, verified from the API

- **`PairPhone` requires a prior `Connect`, and upstream recommends waiting for
  the first `*events.QR`** (or ~1 s) so the connection is fully established.
  Its own docs call this out; getting it wrong yields intermittent failures.
- **Sending requires a live connection.** Messages are stored pending and
  confirmed through events, never optimistically marked sent.
- **Events arrive as one ordered stream.** Draining a single channel rather than
  several avoids interleaving a presence update with a message.

---

## What is deliberately not attempted

- **No browser automation, no scraping.** Wrong tool regardless.
- **No calls beyond declining one.** NOT POSSIBLE upstream.
- **No broadcast lists.** NOT POSSIBLE upstream, and on WhatsApp Web too.
- **No status posting.** LIMITED and experimental upstream.
- **No multi-device coordination.** One account, one terminal.

---

## Before linking an account you care about

Read [LIMITATIONS.md](LIMITATIONS.md). In short: this is an unofficial protocol,
it violates WhatsApp's Terms of Service, and the failure mode includes losing
the ability to link *any* client. Use a secondary number.