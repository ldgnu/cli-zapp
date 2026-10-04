# UX

This document records the interface decisions and the reasoning behind them. It is
written for the next person to change something, so it argues rather than lists: a
decision without its reason is one that gets reverted by someone who does not know why
it was made.

The interface is modelled on WhatsApp Web and follows the i3 window manager's keyboard
philosophy. Those two are in tension more often than it might seem, and every section
below that resolves a conflict says which one won.

---

## 1. The three zones

```
┌──────────────────────────┬─────────────────────────────────────────────────────────┐
│ WTERM                    │ Ada Lovelace                              ● en línea     │
│──────────────────────────│─────────────────────────────────────────────────────────│
│ ⌕ Buscar chats…          │                          HOY                               │
│──────────────────────────│ Did the analytical engine notes land?                    │
│──────────────────────────│                                                          │
│›Ada Lovelace     7 23:08📌│              I read the note on the looping notation.      │
│ Grace Hopper      3 23:08│                                                          │
│ Terminal Gophers 5 23:08🔇│                                    ✖ no se pudo enviar      │
│ Alan Turing       1 23:08│                                                          │
│                          │                                                          │
├──────────────────────────┼─────────────────────────────────────────────────────────┤
│                          │─────────────────────────────────────────────────────────│
│                          │› Escribir mensaje…                                       │
│                          │ enter enviar · alt+enter línea nueva                     │
│                          │─────────────────────────────────────────────────────────│
├──────────────────────────┴─────────────────────────────────────────────────────────┤
│● en línea · Ada Lovelace · 11 sin leer     [enter] abrir  [ctrl+u] leída          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

**Sidebar, conversation, status bar.** The same division every messenger uses, because
it is the division the task has. The status bar is the addition: an i3 user reads the
status line constantly, and a chat client with no status line makes them guess whether
a message failed to send.

### Why one row per conversation

WhatsApp Web uses two lines per conversation: the name above, the preview below. A
terminal cannot afford it. At 24 rows, a two-line list holds eleven conversations; a
one-line list holds twenty-three. The preview is the first thing to go, and it is gone
by 34 columns of sidebar width.

### Why the sidebar scrolls independently

The transcript's offset and the list's offset are separate state. A user reading back
through a long conversation must be able to find another chat without losing their
place, and vice versa. Sharing one offset means every scroll in one pane disturbs the
other, which is the single most annoying property a two-pane interface can have.

### Why the wheel routes by pointer, not by focus

Scrolling the pane under the cursor is what every scrollable interface does. Routing by
focus would mean the wheel scrolls a pane the user is not looking at, until they move
the pointer there — which they cannot do without also moving focus, which they were not
trying to do.

Scrolling *does* move focus, for the transcript. A user who scrolls is reading, and the
next keypress should act on what they are reading rather than on the pane they scrolled
away from.

---

## 2. Responsive layout

The geometry is a pure function of `(width, height)` in `internal/ui/layout`, tested
exhaustively rather than at a few reference sizes. Three tiers:

| Tier | Condition | What is drawn |
|---|---|---|
| Full | ≥ 72×18 | Sidebar, header, transcript, composer, status bar with hints |
| Minimal | ≥ 40×10, below the above | Conversation, composer, status bar |
| Too small | below 40×10 | One sentence saying what is needed |

### 80×24 is inside the full layout, deliberately

The classic default terminal is the most common one there is, and hiding the sidebar at
80 columns would penalise it. A 26-column sidebar beside a 53-column conversation is
readable. The breakpoint is 72 because that is the narrowest width at which both panes
still say something.

### Why the sidebar is dropped rather than squeezed

At 60 columns, a 26-column sidebar leaves 33 for the conversation. Thirty-three columns
shows about four words per line. A sidebar showing `Ada Lo… Tha…` is worse than no
sidebar: the user reads neither, and the conversation — the thing they came for — gets
less room.

The sidebar also loses its decorative rows before its content rows. Wordmark, then
search field, then rows.

### Why minimal mode drops the contact header

A row costs a row of transcript, and at this size the transcript is the scarce
resource. The contact's name is still in the status bar, one row from the bottom, where
it is visible for as long as the user is reading.

### Why minimal mode has no key hints

The interface stays operable without them. The keys are in the help sheet, in the
palette, and in the README. Spending rows on a reminder of documentation is a poor trade
against a line of the conversation.

---

## 3. Responsive type

Three widths are verified exhaustively and two of them are quoted in the test suite as
the reference sizes the design was built around: **80×24** and **120×30**, plus
**160×40** for the wide case.

At 160 columns the sidebar is capped at 34. Past that the list is mostly whitespace, and
the extra columns are worth more to the transcript, where long messages wrap less.

---

## 4. The composer

### No border

An earlier version drew the composer inside a box. Three things argued against it:

- The border cost two of the four rows the composer was given. At 24 rows total that is
  8% of the screen spent saying "this is the input".
- At two rows — which minimal mode allocates — there was no room for a border and
  content at once, so the region either overflowed its rectangle or dropped the box
  depending on how the arithmetic came out.
- Lip Gloss's `Width` counts the border and its `Height` does not. A trap worth not
  walking into when the frame's width has to be exact.

What replaces it: the `›` prompt, the dimmed placeholder, and the real terminal caret
the root places. Between the three, a user can tell an active input from an inactive one
without a single cell spent on a frame.

Two horizontal rules survive, above and below, and only when there is a row to spare.
They are what still separates the input from the conversation once the box is gone.

### Enter sends; alt+enter inserts a newline

Multiline input is necessary — pasting a stack trace into a chat is normal. It does not
follow from that: a chat box where enter inserts a newline is unusable at a prompt, and
every terminal messenger works the other way. So enter sends, alt+enter and shift+enter
insert a newline, and the hint line says so rather than leaving it to be discovered.

### Four rows, for the hint

The hint is the only way the user learns that alt+enter exists. Without it, a first
multiline message sends its first line on its own, which is the kind of mistake people
make once and remember. In minimal mode the hint is dropped and the row goes to the
transcript, because at that size there is no room for both.

### The prompt follows the caret

Putting `›` on the caret's line rather than always on the first one is what makes a
multi-line draft read as one input being typed into, rather than as a list of lines with
a glyph beside the first. It is why a shell moves its continuation prompt down as a
multi-line command grows.

---

## 5. Focus

Three focusable regions, cycled with tab. The focused one is marked three ways:

- the sidebar's selected row gets a `›` in the first column,
- the composer's prompt is accented rather than dimmed,
- the real terminal caret sits in the input.

None of them relies on colour alone. That is not fastidiousness: a monochrome terminal,
a colour-blind reader, and a screenshot pasted into a bug report all see the same thing,
and an interface that marks the selection only with a background colour is unusable to
each of them.

---

## 6. The command palette

`ctrl+shift+p`. A keyboard-driven client accumulates actions faster than any key table
can carry them. The bindings cover the frequent; the palette covers everything, and is
the only discoverable route to the actions that have no binding at all.

### Why not `ctrl+k`

`ctrl+k` is the convention every editor has trained users on, and it is also "up" in
the i3 and vim dialect this interface follows. That convention has priority here: a key
that means one thing in every other pane of a keyboard-driven application must not mean
another thing in one of them. `ctrl+shift+p` is the other established spelling, so
nothing is lost.

### Why unavailable commands are greyed rather than hidden

"Responder" with no message selected should look unavailable, not missing. A feature the
user cannot find and a feature that does not exist look identical from outside, and only
one of them can be fixed by adding the key they were about to press.

### Why the query is seeded rather than typed blind

The palette opens with an empty query and filters as you type. A seeded open — press
`ctrl+p` then a letter — is one keystroke saved on every use, and costs a second binding
that is only discoverable once the user knows the palette exists.

### Why the ordering is not alphabetical

The hint list in the status bar is ordered by usefulness, not by action name. A bar that
lists actions in the order of their identifiers shows none of them, because at a hundred
columns there is room for three labels and the three that matter are never adjacent.

---

## 7. Messages

### Incoming flush left, outgoing flush right

The alignment carries the direction, so a conversation reads as speech rather than as a
list. It is the one piece of the WhatsApp Web layout worth copying unconditionally.

### One timestamp per run

Three consecutive messages from one sender is one utterance. Stamping each of them would
triple the height of the exchange, which at 24 rows is the difference between reading a
conversation and scrolling one.

### Bubbles are capped at four fifths of the pane

A message that fills the pane edge to edge reads as a wall, not as a message. Capping
the width also keeps the gutter readable, which is what makes the alignment visible.

### Revoked messages leave a tombstone

The row does not reflow and the sender does not get to erase the fact of having sent
something. That is the whole point of revocation: the message is gone from everyone's
screen, and the record that it was there is not.

### Unknown presence renders nothing

WhatsApp hides presence for many accounts. Rendering "offline" for a peer whose presence
was merely withheld is worse than rendering nothing, because the user will act on it —
and "acting on it" here means assuming someone is ignoring them.

---

## 8. Search

The sidebar has a persistent field in the full layout. In minimal mode there is no field
to spare, so search runs through the palette, which filters conversations as well as
commands. The feature does not disappear with the layout; only its affordance does.

Archived conversations are hidden by default and included when they match. Hiding them
keeps the list short; making them findable stops archiving from being a destructive
operation rather than a filing one.

---

## 9. Input order

One path, and the order is load-bearing:

1. **Overlays** consume input. A dialog that let a key through would act on the
   conversation behind it — a confirmation asking "delete this message?" that then
   deleted a different one.
2. **Escape** is handled next, because the user must always be able to abandon a mode.
3. **Global bindings** resolve before any text field.
4. **The palette** takes printable input as its query, while it is open.
5. **The focused region** gets the remainder.

Step 3 before step 4 is the counter-intuitive one, and the reason is worth stating
because getting it backwards produces a bug that reads as "my shortcuts are broken": a
text field accepts arbitrary input and reports that it consumed the press, so if it saw
the key first it would swallow `ctrl+q`, `ctrl+r` and `ctrl+p` — and the user spends most
of their time with the composer focused.

`Escape` binds to `app.cancel` as well as being special-cased, so a user who rebinds it
gets the rebinding.

---

## 10. Deliberate non-features

- **No mouse-only affordances.** The mouse works; nothing is reachable only by mouse.
- **No configurable layout.** The breakpoints are the design. A user who wants a wider
  sidebar has a wider terminal or a smaller font.
- **No second page of commands.** The palette filters rather than paginates. Paging
  requires the user to know there is a second page; filtering does not.
- **No confirmation for sending.** A confirmation per message would be the single most
  annoying thing a chat client could do.