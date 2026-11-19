#!/usr/bin/env bash
#
# Reuse a previously published client artifact instead of rebuilding it.
#
# The APK and the desktop shells are thin wrappers around the web UI, so a
# release that changed neither can ship the previous release's binaries. The
# release workflow decides that in scripts/lib/client-guard.mjs; this script
# only fetches what it decided to reuse, and verifies the download is really a
# binary (a proxy error page saved as .apk/.zip would otherwise be embedded in
# every server binary and only fail when a user tries to install it).
#
# Usage:
#   reuse-client-artifact.sh apk <source_tag> <dest_apk>
#   reuse-client-artifact.sh desktop <asset_base> <source_tag> <dest_zip>
#
# Exits non-zero on any failure so the caller can fall back to building.
set -euo pipefail

die() { echo "reuse-client-artifact: $*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"; }

cmd="${1:-}"
[ -n "$cmd" ] || die "missing subcommand (apk|desktop)"
shift

case "$cmd" in
  apk)
    [ $# -eq 2 ] || die "usage: apk <source_tag> <dest_apk>"
    source_tag="$1"; dest="$2"
    need gh
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT

    gh release download "$source_tag" --pattern 'clawbench-android-*.apk' --dir "$tmp" \
      || die "no APK asset on release $source_tag"

    src=$(find "$tmp" -name '*.apk' -type f | head -1)
    [ -n "$src" ] || die "downloaded archive contained no .apk"

    # An APK is a zip; "PK" is the magic. A captive portal / proxy error page
    # would be saved as HTML and only fail at install time.
    [ "$(head -c 2 "$src")" = "PK" ] || die "$src is not a zip archive (corrupt download)"

    mkdir -p "$(dirname "$dest")"
    cp "$src" "$dest"
    echo "reused APK from $source_tag -> $dest"
    ;;

  desktop)
    [ $# -eq 3 ] || die "usage: desktop <asset_base> <source_tag> <dest_zip>"
    asset_base="$1"; source_tag="$2"; dest_zip="$3"
    need gh
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT

    gh release download "$source_tag" --pattern "${asset_base}-*.zip" --dir "$tmp" \
      || die "no desktop package on release $source_tag"

    src=$(find "$tmp" -name '*.zip' -type f | head -1)
    [ -n "$src" ] || die "downloaded archive contained no .zip"
    [ "$(head -c 2 "$src")" = "PK" ] || die "$src is not a zip archive (corrupt download)"

    mkdir -p "$(dirname "$dest_zip")"
    cp "$src" "$dest_zip"
    echo "reused desktop package from $source_tag -> $dest_zip"

    ;;

  *)
    die "unknown subcommand: $cmd"
    ;;
esac
