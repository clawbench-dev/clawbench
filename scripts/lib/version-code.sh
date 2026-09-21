#!/usr/bin/env bash
#
# scripts/lib/version-code.sh — single source of truth for the Android versionCode.
#
# Why this exists
# ---------------
# versionCode is the ONLY thing Android's package installer compares when
# deciding whether a build may replace an already-installed one (versionName is
# display-only). It used to be derived as `git rev-list --count HEAD`, which is
# silently wrong under a shallow clone: actions/checkout defaults to depth=1, so
# every official release APK shipped versionCode=1. Verified against the real
# assets — the v0.95.0, v0.98.0 and v0.99.0 GitHub Release APKs all report
# versionCode=1, while a dev APK built locally from a full clone reported 1167.
#
# The consequence was backwards: a *newer* release could not be installed over
# an *older* dev build. Android saw 1 < 1167 and refused with
# "已安装更高版本", even though v0.99.0 > v0.97.0-51-g463667161 by versionName.
#
#   versionCode = major*100000000 + minor*100000 + patch*1000 + distance
#
# `distance` is the number of commits since the nearest vX.Y.Z tag (0 for a
# release build). Keeping it means builds *between* two releases stay ordered
# against each other and against both releases:
#
#   v0.99.0        -> 0*1e8 +  99*1e5 + 0*1e3 +   0 =   9900000
#   v0.99.0+188    -> 0*1e8 +  99*1e5 + 0*1e3 + 188 =   9900188
#   v0.100.0       -> 0*1e8 + 100*1e5 + 0*1e3 +   0 =  10000000
#   v1.0.0         -> 1*1e8 +   0*1e5 + 0*1e3 +   0 = 100000000
#
# `distance` needs the commit graph, which a CI checkout does not have. So the
# two callers use different entry points (see "Two modes" below): CI reads the
# tag alone and never fetches history; local dev builds use `git describe`.
#
# Field widths are chosen so the fields never collide, and are *checked* rather
# than silently truncated. The tighter `minor*10000` layout that looks natural
# is broken: v0.100.0 and v1.0.0 both collapse to 1000000, so v1.0.0 could
# never replace v0.100.0. Widths (Android caps versionCode at 2100000000):
#
#   major <= 20   (21*1e8 is already 2100000000, over the cap)
#   minor <= 999  (historical max: 99)
#   patch <= 99   (historical max: 8)
#   distance <= 999, clamped — a between-releases gap over 999 is not worth
#                 failing a release over; only dev-build ordering is affected
#
# Fail-open: anything unparseable (no tags, a bare hash, "dev") yields 1, which
# is the historical value and never *blocks* an install it previously allowed.
#
# Usage
# -----
#   # as a script
#   scripts/lib/version-code.sh                 # print the versionCode
#   scripts/lib/version-code.sh --assert        # exit 1 if it degenerates to <= 1
#   scripts/lib/version-code.sh --repo <dir>    # inspect another checkout
#   scripts/lib/version-code.sh --parse '<describe output>'   # pure parse, no git
#   scripts/lib/version-code.sh --tag-only      # CI mode: use the newest tag, no history
#
#   # sourced (build.sh)
#   source scripts/lib/version-code.sh
#   VERSION_CODE=$(clawbench_version_code "$SCRIPT_DIR")
#
# Two modes
# ---------
# `git describe` needs the commit graph. A CI checkout does not have one, and
# asking CI to fetch all history just for a version number is wasteful. So:
#
#   local dev build  -> `git describe`: nearest tag + commit distance (distance>0)
#   CI               -> `--tag-only`: newest vX.Y.Z tag, distance forced to 0
#
# CI builds are release builds (a tag push), so distance is 0 there anyway —
# `--tag-only` produces the same number, from the tags alone.

# Repository this library lives in: scripts/lib/ -> repo root.
CLAWBENCH_VERSION_CODE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Hard limits, derived from Android's versionCode ceiling (2100000000).
readonly CLAWBENCH_VC_MAX_MAJOR=20
readonly CLAWBENCH_VC_MAX_MINOR=999
readonly CLAWBENCH_VC_MAX_PATCH=99
readonly CLAWBENCH_VC_MAX_DISTANCE=999

# clawbench_version_code_from_describe <describe-output>
#
# Converts the output of `git describe --tags --long --match 'v[0-9]*'`
# (e.g. "v0.99.0-188-gabcdef1") into a versionCode.
#
# Returns 0 and prints the integer on success.
# Returns 1 when the input is not a parseable versioned build (fail-open).
# Returns 2 when the version is outside the supported range (hard error — the
#            caller must not paper over this with a fallback).
clawbench_version_code_from_describe() {
    local desc="$1"
    local rest tag distance

    # Format is always "<tag>-<distance>-g<hash>". Strip the "-g<hash>" tail
    # first and read `distance` off the end, so a tag that itself contains "-"
    # (e.g. the historical "v0.1.0-alpha") is not mis-split.
    case "$desc" in
        *-g*) rest="${desc%-g*}" ;;
        *) return 1 ;;
    esac

    distance="${rest##*-}"
    tag="${rest%-*}"

    # Strict vX.Y.Z only. A tag like "v0.1.0-alpha" is deliberately rejected
    # rather than guessed at: release tags are always clean.
    if [[ ! "$tag" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
        return 1
    fi
    local major="${BASH_REMATCH[1]}" minor="${BASH_REMATCH[2]}" patch="${BASH_REMATCH[3]}"

    case "$distance" in
        '' | *[!0-9]*) return 1 ;;
    esac

    # 10# forces base-10 so a zero-padded segment is not read as octal.
    if (( 10#$major > CLAWBENCH_VC_MAX_MAJOR )); then
        echo "version-code: major=$major exceeds the supported maximum ($CLAWBENCH_VC_MAX_MAJOR)" >&2
        return 2
    fi
    if (( 10#$minor > CLAWBENCH_VC_MAX_MINOR )); then
        echo "version-code: minor=$minor exceeds the supported maximum ($CLAWBENCH_VC_MAX_MINOR)" >&2
        return 2
    fi
    if (( 10#$patch > CLAWBENCH_VC_MAX_PATCH )); then
        echo "version-code: patch=$patch exceeds the supported maximum ($CLAWBENCH_VC_MAX_PATCH)" >&2
        return 2
    fi
    if (( 10#$distance > CLAWBENCH_VC_MAX_DISTANCE )); then
        echo "version-code: distance=$distance since $tag clamped to $CLAWBENCH_VC_MAX_DISTANCE" >&2
        distance="$CLAWBENCH_VC_MAX_DISTANCE"
    fi

    echo $(( 10#$major * 100000000 + 10#$minor * 100000 + 10#$patch * 1000 + 10#$distance ))
}

# clawbench_version_code_from_tag <tag>
#
# Same as from_describe but for a bare tag ("v0.99.0"), i.e. a release build
# where the commit distance is 0 by definition. Implemented by delegating to
# from_describe so the range checks and the formula exist in exactly one place.
clawbench_version_code_from_tag() {
    local tag="$1"
    if [[ ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        return 1
    fi
    clawbench_version_code_from_describe "${tag}-0-g0"
}

# clawbench_version_code [<repo-dir>]
#
# Prints the versionCode for the given checkout (default: this library's own
# repository) using `git describe`, i.e. nearest tag PLUS the commit distance.
# This is the developer path: it orders builds made between two releases.
#
# Fail-open: when git cannot describe a version tag the historical value 1 is
# printed and the function still succeeds. That matters for `set -e` callers
# such as build.sh — a non-zero status here would abort the whole build on a
# checkout that simply has no tags yet.
#
# The only non-zero status is 2 (out of the supported version range), which is a
# real error the caller should surface rather than paper over.
clawbench_version_code() {
    local repo="${1:-$CLAWBENCH_VERSION_CODE_ROOT}"
    local desc code rc=0

    if ! desc=$(git -C "$repo" describe --tags --long --match 'v[0-9]*' 2>/dev/null); then
        echo 1
        return 0
    fi

    code=$(clawbench_version_code_from_describe "$desc") || rc=$?
    case "$rc" in
        0) echo "$code" ;;
        1) echo 1 ;; # unparseable tag — fail open to the historical value
        *) return "$rc" ;;
    esac
}

# clawbench_version_code_tag_only [<repo-dir>]
#
# CI path: derives the versionCode from the newest version tag alone, with no
# dependence on the commit graph (`git tag` lists refs; it does not walk
# history). Intended for a checkout that fetched only tag refs, so CI never has
# to pull the full history just to compute a version number.
#
# Equivalent to `clawbench_version_code` on a release checkout, where the
# distance is 0 anyway. On a checkout that is ahead of its newest tag this
# reports the release it is based on rather than a distance-suffixed value —
# acceptable for CI, which builds release tags.
#
# Fail-open to 1 when there is no version tag at all, matching
# `clawbench_version_code`.
clawbench_version_code_tag_only() {
    local repo="${1:-$CLAWBENCH_VERSION_CODE_ROOT}"
    local tag code rc=0

    # -v:refname sorts by version, not lexically, so v0.100.0 outranks v0.99.1
    # (a plain sort would put v0.99.1 last). Verified against those two tags.
    tag=$(git -C "$repo" tag --sort=-v:refname --list 'v[0-9]*' 2>/dev/null | head -1)
    if [[ -z "$tag" ]]; then
        echo 1
        return 0
    fi

    code=$(clawbench_version_code_from_tag "$tag") || rc=$?
    case "$rc" in
        0) echo "$code" ;;
        1) echo 1 ;;
        *) return "$rc" ;;
    esac
}

_clawbench_version_code_usage() {
    cat <<'EOF'
Usage: version-code.sh [options]

  --assert        exit 1 if the versionCode degenerates to <= 1
  --repo <dir>    inspect this checkout instead of the library's own
  --parse <str>   parse a `git describe --tags --long` string, no git needed
  --tag-only      newest tag only, no commit history (CI mode)
  -h, --help      show this help
EOF
}

_clawbench_version_code_main() {
    local repo="$CLAWBENCH_VERSION_CODE_ROOT"
    local parse="" assert=0 tag_only=0

    while (( $# )); do
        case "$1" in
            --repo)
                repo="${2:-}"
                [[ -n "$repo" ]] || { echo "version-code: --repo needs a directory" >&2; return 64; }
                shift 2
                ;;
            --parse)
                parse="${2:-}"
                [[ -n "$parse" ]] || { echo "version-code: --parse needs a value" >&2; return 64; }
                shift 2
                ;;
            --assert)
                assert=1
                shift
                ;;
            --tag-only)
                tag_only=1
                shift
                ;;
            -h | --help)
                _clawbench_version_code_usage
                return 0
                ;;
            *)
                echo "version-code: unknown argument: $1" >&2
                return 64
                ;;
        esac
    done

    local code rc=0
    if [[ -n "$parse" ]]; then
        code=$(clawbench_version_code_from_describe "$parse") || rc=$?
    elif (( tag_only )); then
        code=$(clawbench_version_code_tag_only "$repo") || rc=$?
    else
        code=$(clawbench_version_code "$repo") || rc=$?
    fi

    case "$rc" in
        0) ;;
        1) code=1 ;; # unparseable — fail open to the historical value
        *) return "$rc" ;;
    esac

    # A release build must never ship the degenerate value again. Asserting here
    # (rather than only checking the tag) catches the whole class of regressions:
    # a shallow clone, a detached HEAD, a missing tag, a future rewrite of this
    # formula that collapses to 1.
    #
    # Only --parse (a deliberate probe) can reach the degenerate value now that
    # the git path fails open internally, so the check is applied before echoing
    # either way — it costs nothing and keeps the contract honest.
    if (( assert )) && (( code <= 1 )); then
        echo "version-code: refusing degenerate versionCode=$code (a release APK must be > 1)" >&2
        return 1
    fi

    echo "$code"
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    set -euo pipefail
    _clawbench_version_code_main "$@"
fi
