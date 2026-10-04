#!/usr/bin/env python3
"""Drive bin/cli-zapp through a real pty and check that it lives up.

# What this checks, and what it does not

A Bubble Tea application cannot be exercised by calling its model. The renderer
negotiates capabilities with the terminal and blocks on answers it never receives when
there is no tty on stdin, and it reports a 0x0 window as "too small". All three make a
headless run differ from a real one, so some checking has to happen against a real pty.

This checks that the application starts under a tty, negotiates without hanging, paints
something, responds to keys without crashing, and exits when told to. It does not check
the layout: a pty capture is a stream of cursor moves and partial repaints, and
reconstructing a frame from it faithfully is a terminal emulator's job. For the layout
use framedump, which prints the exact string the renderer would send.

Usage:
    ptycheck.py <cols> <rows> [key ...]

Keys are terminal names: up, down, left, right, enter, esc, tab, pgup, pgdn, home,
end, or "ctrl+x". Anything else is typed literally.

Exits non-zero, with a message on stderr, when the check fails.
"""

import fcntl
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import time

KEYS = {
    "up": "\x1b[A",
    "down": "\x1b[B",
    "right": "\x1b[C",
    "left": "\x1b[D",
    "enter": "\r",
    "esc": "\x1b",
    "tab": "\t",
    "shift+tab": "\x1b[Z",
    "pgup": "\x1b[5~",
    "pgdn": "\x1b[6~",
    "home": "\x1b[H",
    "end": "\x1b[F",
    "ctrl+enter": "\x1b\r",
    "ctrl+left": "\x1b[1;5D",
}

# Sequences that end in a letter, a digit or a terminator; anything else between an
# escape and one of those is a parameter.
CSI = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Za-z0-9]|\x1b[@-Z\\-_]")


def translate(token):
    if token in KEYS:
        return KEYS[token]
    if token.startswith("ctrl+") and len(token) == 6:
        return chr(ord(token[5].lower()) & 0x1F)
    return token


def set_size(fd, cols, rows):
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))


def main():
    if len(sys.argv) < 3:
        sys.exit(__doc__)

    cols, rows = int(sys.argv[1]), int(sys.argv[2])
    tokens = sys.argv[3:]

    pid, master = pty.fork()
    if pid == 0:
        os.execv("./bin/cli-zapp", ["cli-zapp", "--demo"])
        os._exit(1)

    # The size must be set before the process starts rendering: a 0x0 window makes the
    # application print its "terminal too small" notice and stop.
    set_size(master, cols, rows)

    captured = bytearray()

    def pump(seconds):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            ready, _, _ = select.select([master], [], [], 0.05)
            if not ready:
                continue
            try:
                chunk = os.read(master, 65536)
            except OSError:
                return
            if not chunk:
                return
            captured.extend(chunk)

    # Long enough for the capability negotiation, the conversation fetch and the first
    # frames. Shorter and the assertion runs against an empty screen.
    pump(1.0)

    for token in tokens:
        os.write(master, translate(token).encode())
        pump(0.35)

    pump(0.5)

    # ctrl+q, the documented way out. If the process is still alive afterwards, the
    # exit path is broken and the check fails rather than passing silently.
    os.write(master, b"\x11")
    pump(0.5)

    exited = False
    for _ in range(30):
        done, _ = os.waitpid(pid, os.WNOHANG)
        if done == pid:
            exited = True
            break
        pump(0.1)

    if not exited:
        os.kill(pid, signal.SIGKILL)
        os.waitpid(pid, 0)
        sys.stderr.write("FAIL: the application did not exit on ctrl+q\n")
        return 1

    return report(captured, cols, rows)


# The minimum at which the full layout draws. Below it the application shows its
# "too small" notice instead, and asserting on the zones would be asserting on a layout
# it has deliberately chosen not to draw.
FULL_LAYOUT_MIN = (72, 18)


def report(captured, cols, rows):
    """Assert the pty run produced the parts a user would notice missing."""
    raw = captured.decode("utf-8", "replace")
    plain = stripANSI(raw)
    problems = []

    if (cols, rows) >= FULL_LAYOUT_MIN and (cols, rows) < (80, 10):
        problems.append("unexpected size")

    if cols < 40 or rows < 10:
        # Too small for any layout: the one thing that must appear is the explanation,
        # and the one thing that must not is a scrambled grid.
        if "demasiado" not in plain:
            problems.append("a too-small terminal should explain itself")
    else:
        # The three zones, each identified by content only it draws.
        if (cols, rows) >= FULL_LAYOUT_MIN:
            if "CLI-ZAPP" not in plain:
                problems.append("the sidebar's wordmark never appeared")
            if "\u2502" not in plain and "|" not in plain:
                problems.append("the divider between the columns never appeared")
        if "Escribir mensaje" not in plain:
            problems.append("the composer never appeared")

    # The program left the alternate screen rather than dying inside it.
    if "\x1b[?1049l" not in raw:
        problems.append("the alternate screen was never left; the terminal is in a bad state")

    if problems:
        for p in problems:
            sys.stderr.write("FAIL: " + p + "\n")
        sys.stderr.write("\n--- last frame as far as it could be reconstructed ---\n")
        sys.stderr.write(render(captured, cols, rows))
        return 1

    sys.stderr.write("pty check passed at %dx%d\n" % (cols, rows))
    return 0


def stripANSI(s):
    """Remove the escape sequences a pty capture is full of."""
    return CSI.sub("", s)


def render(captured, cols, rows):
    """Replay a pty capture into a screen and print it.

    Interpreting only the sequences the interface actually emits — cursor positioning,
    erases, line feeds — keeps the check honest without pulling in a terminal emulator.
    """
    text = stripANSI(captured.decode("utf-8", "replace"))
    screen = [[" "] * cols for _ in range(rows)]
    row = col = 0

    pos = 0
    while pos < len(text):
        m = CSI.match(text, pos)
        if m:
            # The sequence itself is what matters, not its contents: this replay is a
            # diagnostic for a failed check, not a second renderer.
            seq = m.group(0)
            pos = m.end()
            nums = [int(n) for n in re.findall(r"\d+", seq)]
            tail = seq[-1]

            if seq.startswith("\x1b[2J") or seq.startswith("\x1b[3J"):
                screen = [[" "] * cols for _ in range(rows)]
                row = col = 0
            elif tail in "Hf":
                if len(nums) >= 2:
                    row, col = nums[0] - 1, nums[1] - 1
                elif nums:
                    row, col = nums[0] - 1, 0
            elif tail == "d" and nums:
                row = nums[0] - 1
            elif tail == "G" and nums:
                col = nums[0] - 1
            elif tail in "ABCD":
                n = nums[0] if nums else 1
                if tail == "A":
                    row = max(0, row - n)
                elif tail == "B":
                    row = min(rows - 1, row + n)
                elif tail == "C":
                    col = min(cols - 1, col + n)
                else:
                    col = max(0, col - n)
            elif tail == "K":
                # Erase in line: 0 to the end, 1 to the start, 2 the whole line.
                mode = nums[0] if nums else 0
                lo, hi = (col, cols) if mode == 0 else ((0, col + 1) if mode == 1 else (0, cols))
                for c in range(lo, hi):
                    screen[row][c] = " "
            continue

        ch = text[pos]
        pos += 1
        if ch == "\n":
            row = min(rows - 1, row + 1)
            col = 0
            continue
        if ch == "\r":
            col = 0
            continue
        if ch in "\x07\x08":
            continue
        if ord(ch) < 32:
            continue
        if 0 <= row < rows and 0 <= col < cols:
            screen[row][col] = ch
        col += 1
        if col >= cols:
            col = 0
            row = min(rows - 1, row + 1)

    return "\n".join("".join(r).rstrip() for r in screen) + "\n"


if __name__ == "__main__":
    sys.exit(main())