#!/bin/sh
# backup.sh — produce a restorable archive of the project.
#
# # Why this is built from git rather than from the filesystem
#
# The obvious implementation is `tar -czf backup.tar.gz --exclude=... .`, and it is
# wrong in a way that matters. An exclude list only covers what somebody remembered to
# enumerate, so the first time a `.env`, a session database or a log lands in the
# working tree, it goes into a backup that is then uploaded somewhere and shared.
#
# So the contents come from `git archive`, which emits only files git is tracking.
# An untracked secret is not excluded by policy — it is structurally incapable of
# being included. History comes from `git bundle`, which is a single deduplicated
# packfile rather than a copy of .git/objects.
#
# What that costs, and how it is handled: work that is not committed is not backed
# up. Rather than silently produce an archive missing recent edits, the script lists
# exactly what it left out (UNTRACKED.txt) so the omission is visible.
#
# Usage: scripts/backup.sh [output-directory]

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
name=cli-zapp
outdir=${1:-$root}
stamp=$(date -u +%Y%m%d-%H%M%S)
base="$name-backup-$stamp"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

payload="$work/$base"
mkdir -p "$payload"

cd "$root"

# --- guard: this must be a repository ---------------------------------------
if ! git rev-parse --git-dir >/dev/null 2>&1; then
	echo "backup: not a git repository; refusing to guess what to include" >&2
	exit 1
fi

commit=$(git rev-parse HEAD)
describe=$(git describe --tags --always --dirty 2>/dev/null || echo unknown)
branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)

# --- tracked source ----------------------------------------------------------
# `git archive` is used with an explicit ref so the backup is a snapshot of a
# commit, not of whatever happens to be in the index.
echo "==> source tree ($commit)"
git archive --format=tar "$commit" | tar -x -C "$payload"

# --- git history -------------------------------------------------------------
# A bundle is the whole repository's objects in one file, and unlike a copy of
# .git/objects it does not duplicate anything already in the source tree.
echo "==> git history"
git bundle create "$payload/repo.bundle" --all >/dev/null 2>&1 \
	|| echo "    (no refs to bundle yet — continuing with source only)" >&2

# --- git metadata as text ---------------------------------------------------
# The bundle is opaque without this: a bundle can be cloned but a reader needs to
# know which commit it corresponds to.
git log --oneline -50 > "$payload/COMMITS.txt" 2>/dev/null || true
git tag -l > "$payload/TAGS.txt" 2>/dev/null || true

# --- what was NOT included --------------------------------------------------
# The point of this file is that a backup is not a substitute for committing. If it
# lists work, that work is in neither the archive nor the history.
{
	echo "# Files present in the working tree but NOT in this backup."
	echo "#"
	# Single-quoted, and that matters: the obvious phrasing of this line wants to
	# quote the command with backticks, and backticks inside a double-quoted echo are
	# command substitution. Writing it that way runs git archive and splices the whole
	# raw tar archive — pax headers and all — into this file, yielding a 700 KB "list
	# of untracked files" and a "null byte ignored" warning from the shell.
	echo '# Reason: this backup is built from git archive, which emits'
	echo "# only tracked files. Anything below is either untracked or ignored, and"
	echo "# is therefore absent by construction."
	echo "#"
	echo "# Generated: $stamp"
	echo
	git status --porcelain --untracked-files=all 2>/dev/null | sed 's/^/  /' || true
	echo
	echo "# Ignored paths that exist on disk (never included):"
	git status --porcelain --ignored --untracked-files=all 2>/dev/null \
		| grep '^!!' | sed 's/^!! /  /' || echo "  (none)"
} > "$payload/UNTRACKED.txt"

# --- metadata ----------------------------------------------------------------
cat > "$payload/MANIFEST.txt" <<EOF
# cli-zapp project backup
generated:   $stamp
commit:      $commit
describe:    $describe
branch:      $branch
source:      git archive $commit
history:     repo.bundle (git bundle --all)
restore:     git clone repo.bundle cli-zapp

# Contents
source tree:   every tracked file at $commit
repo.bundle:   full git history, tags and branches
MANIFEST.txt:  this file
UNTRACKED.txt: working-tree files deliberately NOT included
COMMITS.txt:   last 50 commits, human readable
TAGS.txt:      tags

# What this backup deliberately does not contain
#   - secrets, tokens, sessions or credentials (untracked by construction)
#   - runtime databases, logs or caches
#   - built binaries, dist/, coverage output
EOF

# --- manifest of contents with per-file hashes -------------------------------
# Written after the payload is complete so the hashes describe what is actually
# inside the archive, not what was intended to be.
(
	cd "$payload" && find . -type f ! -name SHA256SUMS.txt -print \
		| LC_ALL=C sort | xargs -r sha256sum
) > "$payload/SHA256SUMS.txt"

# --- verify: nothing that looks like a secret got in --------------------------
# Belt and braces. `git archive` should already guarantee this, and the check below
# is here so that a future change to this script which stops using git archive
# fails loudly instead of quietly producing an unsafe archive.
echo "==> scanning for credentials"
if grep -rIlE '(BEGIN [A-Z ]*PRIVATE KEY|AKIA[0-9A-Z]{16}|ghp_[A-Za-z0-9]{20,}|xox[baprs]-)' "$payload" \
	>/dev/null 2>&1; then
	echo "backup: a file matching a credential pattern is present; refusing" >&2
	echo "        offending files listed above" >&2
	exit 1
fi
echo "    clean"

# --- archive ------------------------------------------------------------------
# zstd where available, gzip otherwise. The suffix follows the compressor so the
# name never lies about the contents.
if command -v zstd >/dev/null 2>&1; then
	ext=zst
	tar --sort=name --owner=0 --group=0 --numeric-owner \
		--mtime="@$(date -u +%s)" --zstd -cf "$outdir/$base.tar.$ext" \
		-C "$work" "$base"
else
	ext=gz
	tar --sort=name --owner=0 --group=0 --numeric-owner \
		--mtime="@$(date -u +%s)" -czf "$outdir/$base.tar.$ext" \
		-C "$work" "$base"
	echo "    (zstd not found; wrote gzip instead)"
fi

cd "$outdir"
sha256sum "$base.tar.$ext" > "$base.tar.$ext.sha256"

echo
echo "    $outdir/$base.tar.$ext"
echo "    $outdir/$base.tar.$ext.sha256"
cat "$base.tar.$ext.sha256"

# --- inspect what was produced ------------------------------------------------
echo
echo "==> contents"
if command -v zstd >/dev/null 2>&1 && [ "$ext" = zst ]; then
	zstd -dc "$base.tar.$ext" | tar -tf - | head -40
else
	tar -tf "$base.tar.$ext" | head -40
fi
echo
echo "Restoring:"
echo "  zstd -dc $base.tar.$ext | tar -xf -"
echo "  git clone repo.bundle cli-zapp"