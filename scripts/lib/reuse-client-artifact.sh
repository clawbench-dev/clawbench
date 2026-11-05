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
#   reuse-client-artifact.sh desktop <asset_base> <npm_pkg> <source_tag> \
#                                    <dest_zip> <payload_dir|-> 
#
# `<payload_dir>` is "-" for platforms without a payload (macOS). The payload is
# fetched from npm, where it is published under the SOURCE release's version.
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
    [ $# -eq 5 ] || die "usage: desktop <asset_base> <npm_pkg> <source_tag> <dest_zip> <payload_dir|->"
    asset_base="$1"; npm_pkg="$2"; source_tag="$3"; dest_zip="$4"; payload_dir="$5"
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

    if [ "$payload_dir" != "-" ]; then
      need npm
      need tar
      # The payload is published to npm only (never a release asset), under the
      # SOURCE release's version. npm versions are semver: no leading "v".
      version="${source_tag#v}"
      npm pack "${npm_pkg}@${version}" --pack-destination "$tmp" >/dev/null \
        || die "no npm payload ${npm_pkg}@${version}"

      tgz=$(find "$tmp" -name '*.tgz' -type f | head -1)
      [ -n "$tgz" ] || die "npm pack produced no tarball"

      mkdir -p "$tmp/unpack"
      tar -xzf "$tgz" -C "$tmp/unpack"
      # npm pack wraps every entry in a single `package/` directory.
      [ -d "$tmp/unpack/package" ] || die "unexpected npm tarball layout"
      [ -f "$tmp/unpack/package/payload.json" ] || die "payload tarball has no payload.json"

      mkdir -p "$payload_dir"
      cp -a "$tmp/unpack/package/." "$payload_dir/"
      echo "reused desktop payload from ${npm_pkg}@${version} -> $payload_dir"
    fi
    ;;

  *)
    die "unknown subcommand: $cmd"
    ;;
esac
