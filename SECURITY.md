# Security

## The most important thing in this document

**cli-zapp violates WhatsApp's Terms of Service, and linking your account can get
it banned.**

This is not a formality and it is not fixable. There is no official API for a
personal WhatsApp account, so a terminal client for one speaks a protocol WhatsApp
never documented and does not support. Meta says so directly:

> Using an unauthorized application and/or unsupported device violates our Terms
> of Service and can result in your account being banned.
> — [WhatsApp Help Center](https://faq.whatsapp.com/1064395290901991)

And, on linked devices specifically:

> Linking your account to an unofficial app or website, **now or in the past**, may
> result in a temporary or permanent account ban.
> — [About linked devices](https://faq.whatsapp.com/378279804439436)

Note "now or in the past". There is no clean slate.

**Use a secondary number.** It costs almost nothing to acquire and it is the only
measure that removes the risk rather than reducing it. Do not link an account you
depend on until you have read this.

### The failure mode is worse than a ban

A ban is recoverable. Losing the ability to create linked-device sessions is not,
and it is not limited to this project: in late 2025 Meta flagged a third-party
archiving vendor as an unofficial app and blocked the affected *accounts* from
linking any web client at all — including the official web.whatsapp.com. The
outage ran from roughly September 2025 to February 2026.

So the risk is not only "this program gets banned". It is "this account can no
longer be used as a client anywhere".

### Why there is no official alternative

The WhatsApp Business Platform Cloud API is real, supported and has no ban risk —
and cannot replace this program. It is addressed to businesses, it cannot read
your personal chat list, presence or contacts, and it requires pre-approved
message templates outside a 24-hour window. Meta's own getting-started guide
redirects personal-account users to the help centre.

`docs/PROTOCOL.md` has the full comparison, with the capability claims verified
against the library's exported symbols rather than its README.

---

## Reporting a vulnerability

Report security issues privately, not as a public issue.

- **GitHub Security Advisories** — the *Security* tab → *Report a vulnerability*.
  This is the preferred route.
- **Email** — if you prefer, open a private issue or contact the maintainers
  through the repository's owner page.

Please do not open a public issue for an unfixed vulnerability.

Include: what you found, how to reproduce it, and the impact. A version, terminal
emulator and Go version help a lot.

### What to expect

- Acknowledgement within a few days.
- An assessment and, where warranted, a fix or a mitigation.
- Credit, if you want it, in the release notes or CHANGELOG.

### What is *not* a vulnerability

- **Being banned by WhatsApp.** That is a foreseeable consequence of the protocol,
  documented above, not a defect.
- **An unofficial protocol breaking.** WhatsApp changes it without notice. This is
  the reason the protocol is confined to `internal/whatsapp/adapter/`.
- **Terminal escape sequences from message content.** Message bodies are sanitised
  before being copied to the clipboard, precisely so a message cannot inject escapes
  into whatever the user pastes it into.

---

## How the program handles data

### Credentials

The linked-device session is the only real credential, and it is not cli-zapp's to
leak: it lives in whatsmeow's device store, `0600`, and is expected to live in the
system keyring. Nothing in this repository contains a token, a session or a
password, and there is no code path that writes one to the log.

### Logs

Logs go to a file, never to the terminal — a log line written into the alternate
screen corrupts the frame. **No message content, contact name or credential is
written to the log.** The logger records lifecycle events, errors and state
transitions.

Check what is being recorded before sharing a log:

```sh
cli-zapp --debug --log /tmp/cli-zapp.log
```

If a log ever does contain something it should not, that is a security bug: report
it as above.

### Clipboard

Copied message bodies are sanitised before they reach the clipboard, so a message
containing escape sequences cannot inject them into the terminal the user pastes
into. Copying is a read of your own screen; nothing is transmitted.

### Network

The only outbound connection is to WhatsApp's servers, by the protocol library.
The program has no telemetry, no analytics, no update check and no phone-home.
Release verification is a local `sha256sum` against a file you download yourself.

---

## Verifying a release

```sh
sha256sum -c checksums.txt
```

Checksums are published with each release. For stronger assurance, the release
artifacts are reproducible: rebuilding the same tag with the same `SOURCE_DATE_EPOCH`
produces byte-identical archives, so `make verify-release` is a real check rather
than a formality.

---

## Reporting a ToS or legal concern

If you are WhatsApp or Meta and want to discuss this project, contact the
maintainers through the repository owner page.

---

## Supported versions

Only the latest release receives fixes. This is a young project; backporting to
older tags would consume maintenance capacity that is better spent on the protocol
tracking that the project exists to do.