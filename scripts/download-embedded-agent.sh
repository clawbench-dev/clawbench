#!/usr/bin/env bash
# Shared helper for downloading embedded agent binaries.
# Sourced by build.sh, Dockerfile RUN, and CI composite action.
#
# Requires: curl, embedded-agents.yaml in CWD or SCRIPT_DIR
#
# Functions:
#   parse_embedded_agent_config <agent_id> <field>  — read a field from embedded-agents.yaml
#   download_embedded_agent <agent_id>               — full download (version resolve + platform + download)
#   download_embedded_agent_for_docker <agent_id> <version> [arch]  — Docker RUN variant

# NOTE: This script is sourced — do NOT set shell options here (set -euo pipefail)
# as they would propagate to the caller. Each function handles its own errors.

# Locate embedded-agents.yaml relative to this script or CWD
_EMBEDDED_AGENTS_YAML="${EMBEDDED_AGENTS_YAML:-}"
if [ -z "$_EMBEDDED_AGENTS_YAML" ]; then
    _SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "${_SCRIPT_DIR}/../embedded-agents.yaml" ]; then
        _EMBEDDED_AGENTS_YAML="${_SCRIPT_DIR}/../embedded-agents.yaml"
    elif [ -f "./embedded-agents.yaml" ]; then
        _EMBEDDED_AGENTS_YAML="./embedded-agents.yaml"
    else
        echo "ERROR: embedded-agents.yaml not found. Set EMBEDDED_AGENTS_YAML env var." >&2
        return 1 2>/dev/null || exit 1
    fi
fi

# parse_embedded_agent_config <agent_id> <field>
# Reads a field value from embedded-agents.yaml for the given agent.
# Supported fields: id, github_repo, subdir, version_env, cmd, version_file
# For archive_naming, use: parse_archive_naming <agent_id> <os>
# For arch_mapping, use: parse_arch_mapping <agent_id> <arch>
parse_embedded_agent_config() {
    local agent_id="$1" field="$2"

    # Use awk to parse the simple YAML structure
    # Find the agent block starting with "- id: <agent_id>", then extract the field
    awk -v agent="$agent_id" -v fld="$field" '
    BEGIN { in_agent=0; found=0 }
    /^  - id:/ { in_agent=0 }
    /^  - id: '"$agent_id"'/ { in_agent=1; next }
    in_agent && $1 == (fld ":") {
        # Print value after "field: "
        sub(/^[^:]*: */, "")
        print
        found=1
        exit
    }
    ' "$_EMBEDDED_AGENTS_YAML"
}

# parse_archive_naming <agent_id> <os>
# Returns the archive naming pattern for the given OS (linux/darwin/windows)
parse_archive_naming() {
    local agent_id="$1" os="$2"

    awk -v agent="$agent_id" -v target_os="$os" '
    BEGIN { in_agent=0; in_archive=0 }
    /^  - id:/ { in_agent=0; in_archive=0 }
    /^  - id: '"$agent_id"'/ { in_agent=1; next }
    in_agent && /^    archive_naming:/ { in_archive=1; next }
    in_archive && /^      '"$os"':/ {
        sub(/^[^:]*: */, "")
        gsub(/"/, "")
        print
        exit
    }
    in_archive && /^[^ ]/ { in_archive=0 }
    ' "$_EMBEDDED_AGENTS_YAML"
}

# parse_arch_mapping <agent_id> <arch>
# Returns the mapped arch name (e.g., amd64 -> x64)
parse_arch_mapping() {
    local agent_id="$1" arch="$2"

    awk -v agent="$agent_id" -v target_arch="$arch" '
    BEGIN { in_agent=0; in_mapping=0 }
    /^  - id:/ { in_agent=0; in_mapping=0 }
    /^  - id: '"$agent_id"'/ { in_agent=1; next }
    in_agent && /^    arch_mapping:/ { in_mapping=1; next }
    in_mapping && /^      [a-z]/ {
        key = $1
        sub(/:$/, "", key)
        if (key == target_arch) {
            sub(/^[^:]*: */, "")
            print
            exit
        }
    }
    in_mapping && /^[^ ]/ { in_mapping=0 }
    ' "$_EMBEDDED_AGENTS_YAML"
}

# validate_agent_id <agent_id>
# Validates that the agent exists in embedded-agents.yaml and has required fields.
validate_agent_id() {
    local agent_id="$1"
    local subdir
    subdir=$(parse_embedded_agent_config "$agent_id" subdir)
    if [ -z "$subdir" ]; then
        echo "ERROR: Agent '${agent_id}' not found in embedded-agents.yaml. Available agents:" >&2
        awk '/^  - id:/ { sub(/^  - id: */, ""); print "  - " $0 }' "$_EMBEDDED_AGENTS_YAML" >&2
        return 1
    fi
}

# validate_version <version>
# Validates that a version string looks like a semver version (e.g., "1.17.10" or "0.30.0-rc1")
validate_version() {
    local version="$1"
    if ! [[ "$version" =~ ^[0-9]+(\.[0-9]+)+(-[a-zA-Z0-9._-]+)?$ ]]; then
        echo "ERROR: Invalid version format: '${version}'" >&2
        return 1
    fi
}

# validate_github_repo <repo>
# Validates that a github_repo matches the "owner/repo" format
validate_github_repo() {
    local repo="$1"
    if ! [[ "$repo" =~ ^[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+$ ]]; then
        echo "ERROR: Invalid github_repo format: '${repo}'" >&2
        return 1
    fi
}

# resolve_agent_version <agent_id>
# Resolves the agent version: env var override > auto-detect latest from GitHub
resolve_agent_version() {
    local agent_id="$1"
    local version_env
    version_env=$(parse_embedded_agent_config "$agent_id" version_env)
    local github_repo
    github_repo=$(parse_embedded_agent_config "$agent_id" github_repo)

    local version=""

    # Check env var override first
    if [ -n "$version_env" ]; then
        version="${!version_env:-}"
    fi

    # Auto-detect from GitHub if no override
    if [ -z "$version" ] && [ -n "$github_repo" ]; then
        validate_github_repo "$github_repo" || return 1
        version=$(curl -sI "https://github.com/${github_repo}/releases/latest" 2>/dev/null \
            | grep -i "^location:" \
            | sed 's|.*/tag/v||' \
            | tr -d '[:space:]')
        if [ -z "$version" ]; then
            echo "ERROR: Could not detect latest version for ${agent_id}. Set ${version_env} manually." >&2
            return 1
        fi
        echo "  Auto-detected latest ${agent_id} version: v${version}" >&2
    fi

    validate_version "$version" || return 1
    echo "$version"
}

# resolve_agent_version_gh <agent_id> <github_token>
# Resolves version using GitHub API (gh cli) — for CI environments
resolve_agent_version_gh() {
    local agent_id="$1" gh_token="$2"
    local version_env
    version_env=$(parse_embedded_agent_config "$agent_id" version_env)
    local github_repo
    github_repo=$(parse_embedded_agent_config "$agent_id" github_repo)

    local version=""

    # Check env var override first
    if [ -n "$version_env" ]; then
        version="${!version_env:-}"
    fi

    # Use gh API
    if [ -z "$version" ] && [ -n "$github_repo" ]; then
        validate_github_repo "$github_repo" || return 1
        version=$(GH_TOKEN="$gh_token" gh api "repos/${github_repo}/releases/latest" --jq '.tag_name' | sed 's/^v//')
        if [ -z "$version" ]; then
            echo "ERROR: Could not detect latest version for ${agent_id} via GitHub API." >&2
            return 1
        fi
    fi

    validate_version "$version" || return 1
    echo "$version"
}

# download_embedded_agent <agent_id>
# Full download: resolve version, determine platform, download, chmod, write VERSION
download_embedded_agent() {
    local agent_id="$1"

    validate_agent_id "$agent_id" || return 1

    local subdir
    subdir=$(parse_embedded_agent_config "$agent_id" subdir)
    local cmd
    cmd=$(parse_embedded_agent_config "$agent_id" cmd)
    local version_file
    version_file=$(parse_embedded_agent_config "$agent_id" version_file)
    local github_repo
    github_repo=$(parse_embedded_agent_config "$agent_id" github_repo)

    validate_github_repo "$github_repo" || return 1

    local version
    version=$(resolve_agent_version "$agent_id") || return 1

    echo "[*] Downloading ${agent_id} v${version}..."

    local agent_dir=".clawbench/${subdir}"

    # Determine platform
    local os arch
    if [ -n "${TARGET_OS:-}" ] && [ -n "${TARGET_ARCH:-}" ]; then
        os="$TARGET_OS"
        arch="$TARGET_ARCH"
    else
        os="$(uname -s | tr '[:upper:]' '[:lower:]')"
        arch="$(uname -m)"
        # Normalize arch
        arch="${arch/x86_64/amd64}"
        arch="${arch/aarch64/arm64}"
    fi

    # Apply arch mapping
    local mapped_arch
    mapped_arch=$(parse_arch_mapping "$agent_id" "$arch")
    [ -z "$mapped_arch" ] && mapped_arch="$arch"

    # Get archive pattern and substitute {arch}
    local archive_pattern
    archive_pattern=$(parse_archive_naming "$agent_id" "$os")
    if [ -z "$archive_pattern" ]; then
        echo "  ERROR: No archive naming pattern for OS '${os}' in agent '${agent_id}'." >&2
        return 1
    fi
    local archive_name="${archive_pattern//\{arch\}/$mapped_arch}"

    local url="https://github.com/${github_repo}/releases/download/v${version}/${archive_name}"

    mkdir -p "$agent_dir"

    # Check cache
    if [ -f "$agent_dir/${version_file}" ] && [ "$(cat "$agent_dir/${version_file}")" = "$version" ] && [ -f "$agent_dir/$cmd" -o -f "$agent_dir/${cmd}.exe" ]; then
        echo "  ${agent_id} v${version} already cached in $agent_dir/"
        return 0
    fi

    # Download (use temp file + curl --fail to catch HTTP errors)
    local ext="${archive_name##*.}"
    echo "  Downloading $url ..."
    if [ "$ext" = "zip" ]; then
        local tmp="/tmp/${agent_id}-download.zip"
        curl -sL --fail "$url" -o "$tmp" && unzip -qo "$tmp" -d "$agent_dir" && rm -f "$tmp"
    else
        local tmp="/tmp/${agent_id}-download.tar.gz"
        curl -sL --fail "$url" -o "$tmp" && tar xzf "$tmp" -C "$agent_dir" && rm -f "$tmp"
    fi
    chmod +x "$agent_dir/$cmd" 2>/dev/null || true
    echo -n "$version" > "$agent_dir/${version_file}"
    echo "  ${agent_id} v${version} downloaded to $agent_dir/"
}

# download_embedded_agent_for_docker <agent_id> <version> [arch]
# Docker RUN variant: version and arch are explicit, no auto-detect
download_embedded_agent_for_docker() {
    local agent_id="$1" version="$2"
    local arch="${3:-}"

    validate_agent_id "$agent_id" || return 1
    validate_version "$version" || return 1

    local subdir
    subdir=$(parse_embedded_agent_config "$agent_id" subdir)
    local cmd
    cmd=$(parse_embedded_agent_config "$agent_id" cmd)
    local version_file
    version_file=$(parse_embedded_agent_config "$agent_id" version_file)
    local github_repo
    github_repo=$(parse_embedded_agent_config "$agent_id" github_repo)

    validate_github_repo "$github_repo" || return 1

    local agent_dir=".clawbench/${subdir}"

    # Docker is always linux
    local os="linux"

    # Determine arch from TARGETARCH or explicit parameter
    if [ -z "$arch" ] && [ -n "${TARGETARCH:-}" ]; then
        arch="$TARGETARCH"
    fi
    [ -z "$arch" ] && arch="amd64"

    # Apply arch mapping
    local mapped_arch
    mapped_arch=$(parse_arch_mapping "$agent_id" "$arch")
    [ -z "$mapped_arch" ] && mapped_arch="$arch"

    # Get archive pattern
    local archive_pattern
    archive_pattern=$(parse_archive_naming "$agent_id" "$os")
    if [ -z "$archive_pattern" ]; then
        echo "ERROR: No archive naming pattern for OS '${os}'." >&2
        return 1
    fi
    local archive_name="${archive_pattern//\{arch\}/$mapped_arch}"

    local url="https://github.com/${github_repo}/releases/download/v${version}/${archive_name}"

    mkdir -p "$agent_dir"

    # Download (use temp file + curl --fail to catch HTTP errors)
    local ext="${archive_name##*.}"
    echo "Downloading ${agent_id} v${version} (${mapped_arch})..."
    if [ "$ext" = "zip" ]; then
        local tmp="/tmp/${agent_id}-download.zip"
        curl -sL --fail "$url" -o "$tmp" && unzip -qo "$tmp" -d "$agent_dir" && rm -f "$tmp"
    else
        local tmp="/tmp/${agent_id}-download.tar.gz"
        curl -sL --fail "$url" -o "$tmp" && tar xzf "$tmp" -C "$agent_dir" && rm -f "$tmp"
    fi
    chmod +x "$agent_dir/$cmd" 2>/dev/null || true
    echo -n "$version" > "$agent_dir/${version_file}"
    echo "${agent_id} v${version} (${mapped_arch}) downloaded"
}
