#!/bin/sh
# mkdeb.sh — build a Debian package for cli-zapp.
#
# A .deb is an `ar` archive holding three members in a fixed order: debian-binary,
# control.tar.gz, data.tar.gz. That is the whole format; nothing here needs
# dpkg-deb, so a release can be produced on a machine that has Go and tar but no
# Debian packaging tools.
#
# Building by hand rather than shelling out to dpkg-deb is deliberate: dpkg-deb
# silently sets mtimes from the filesystem and produces archives that differ
# between two builds of the same commit. Every timestamp here is pinned to
# SOURCE_DATE_EPOCH so that `make release` twice yields identical bytes.
#
# Usage: mkdeb.sh <arch> <version> <distdir> <commit> <build-date> <ldflags>

set -eu

arch=${1:?arch}
version=${2:?version}
dist=${3:?distdir}
commit=${4:?commit}
build_date=${5:?build-date}
ldflags=${6:?ldflags}

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
bin=cli-zapp
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

# Reproducible timestamps: SOURCE_DATE_EPOCH if the caller set one, else the commit
# date, else now. dpkg's policy is to honour SOURCE_DATE_EPOCH.
epoch=${SOURCE_DATE_EPOCH:-$(git -C "$root" log -1 --format=%ct 2>/dev/null || date +%s)}

case "$arch" in
amd64) goarch=amd64; debarch=amd64 ;;
arm64) goarch=arm64; debarch=arm64 ;;
*) echo "mkdeb.sh: unsupported architecture '$arch'" >&2; exit 1 ;;
esac

# A .deb version may not contain a hyphen or a tilde, and SemVer prereleases use
# both. Hyphen becomes `~` is wrong (it sorts as "nothing"); the conventional and
# policy-correct mapping is `-` to `~`, which dpkg sorts *before* the release.
debversion=$(printf '%s' "$version" | sed 's/-/~/g')

echo "==> building $bin $debversion ($debarch)"

mkdir -p "$work/root/DEBIAN"

# --- the binary -------------------------------------------------------------
# Cross-compiled here rather than copied from dist/, so the package can be built
# without a prior `make release`. CGO is off: the binary is static.
echo "    compiling linux/$goarch"
GOOS=linux GOARCH=$goarch CGO_ENABLED=0 \
	go build -buildvcs=false -trimpath -ldflags "$ldflags" \
	-o "$work/root/usr/bin/$bin" "$root/cmd/$bin"

# --- documentation ----------------------------------------------------------
# Documentation belongs in a docdir, not scattered in /usr/share/doc. The Debian
# policy path would be /usr/share/doc/<package>, which is what makes `apt remove`
# find and delete it.
install -D -m 0644 "$root/LICENSE" "$work/root/usr/share/doc/$bin/copyright"
install -D -m 0644 "$root/README.md" "$work/root/usr/share/doc/$bin/README.md"
if [ -d "$root/docs" ]; then
	cp -r "$root/docs" "$work/root/usr/share/doc/$bin/docs"
	find "$work/root/usr/share/doc/$bin/docs" -type f -exec chmod 0644 {} +
fi

# --- no maintainer scripts --------------------------------------------------
# The program is a terminal application with no daemon, no config file it writes
# outside the user's home, and nothing to migrate. Empty md5sums, so `dpkg` does
# not warn about the files it expects, but postinst/prerm/postrm are deliberately
# absent: `dpkg -V` runs their templates on install and there is nothing here to
# run.

# --- control ----------------------------------------------------------------
# Installed-Size is in KiB and dpkg uses it to show the download size. It is
# computed from the actual tree rather than written by hand so it cannot drift.
size_kb=$(du -sk "$work/root" | cut -f1)

cat > "$work/root/DEBIAN/control" <<EOF
Package: $bin
Version: $debversion
Section: net
Priority: optional
Architecture: $debarch
Maintainer: cli-zapp maintainers <maintainers@cli-zapp.invalid>
Installed-Size: $size_kb
Homepage: https://github.com/ldgnu/cli-zapp
Description: WhatsApp client for the terminal
 A keyboard-driven WhatsApp client for the terminal, in the tradition of
 WhatsApp Web and the i3 window manager: every action has a binding, the
 command palette reaches the rest, and the mouse is optional throughout.
 .
 This package contains the interface only. It talks to WhatsApp over an
 unofficial protocol, which violates WhatsApp's Terms of Service and carries
 a real risk of a temporary or permanent account ban. See the SECURITY and
 LIMITATIONS documents shipped with the source.
EOF

# --- archive ----------------------------------------------------------------
# Member order is fixed by the format and dpkg enforces it: debian-binary first,
# then control, then data.
printf '0.0\n' > "$work/debian-binary"

tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$epoch" \
	-czf "$work/control.tar.gz" -C "$work/root" DEBIAN

# DEBIAN must not appear in the data archive: it belongs to the control archive, and
# a data.tar.gz that carries it confuses some installers.
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$epoch" \
	--exclude=./DEBIAN -czf "$work/data.tar.gz" -C "$work/root" .

out="$dist/${bin}_${debversion}_${debarch}.deb"
mkdir -p "$dist"
rm -f "$out"
( cd "$work" && ar rc "$out" debian-binary control.tar.gz data.tar.gz )
# `ar` stores each member's mtime in its header, so the members are touched to the
# fixed epoch and the archive rewritten. Without this two builds of one commit differ.
touch -d "@$epoch" "$work/debian-binary" "$work/control.tar.gz" "$work/data.tar.gz" 2>/dev/null || true
rm -f "$out"
( cd "$work" && ar rc "$out" debian-binary control.tar.gz data.tar.gz )
touch -d "@$epoch" "$out" 2>/dev/null || true

echo "    $out"