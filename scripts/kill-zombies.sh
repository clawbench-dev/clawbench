#!/usr/bin/env bash
# kill-zombies.sh — Reap zombie (defunct) processes and their orphaned process trees.
#
# Zombie processes cannot be killed directly (they are already dead); they only
# go away when their parent reaps them (wait()) or the parent dies and they are
# reparented to PID 1 (init), which reaps them. This script:
#   1. Lists all zombies grouped by parent
#   2. For each zombie whose parent is NOT protected, walks the parent's process
#      tree and kills it (TERM, then KILL), which reparents zombies to init
#   3. Reports the result
#
# PROTECTION: the script NEVER touches the main ClawBench server (default port
# 20000) or anything in its process tree (e.g. `clawbench --acp` sessions that
# spawn live vitest runs). Use --port to protect additional ports, and
# --kill-protected to override the protection (use with extreme care).
#
# Usage:
#   ./scripts/kill-zombies.sh                  # dry-run: list zombies and what would be killed
#   ./scripts/kill-zombies.sh --kill           # actually kill zombie parent trees
#   ./scripts/kill-zombies.sh --kill --force   # skip the confirmation prompt
#   ./scripts/kill-zombies.sh --port 8080      # protect the server on port 8080 too
#   ./scripts/kill-zombies.sh --kill --kill-protected  # DANGEROUS: also kill protected trees

set -euo pipefail

MAIN_PORT="${MAIN_PORT:-20000}"
DO_KILL=false
FORCE=false
KILL_PROTECTED=false
declare -a EXTRA_PORTS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --kill) DO_KILL=true ;;
    --force) FORCE=true ;;
    --kill-protected) KILL_PROTECTED=true ;;
    --port)
      shift
      EXTRA_PORTS+=("$1")
      ;;
    -h|--help)
      grep '^#' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "Unknown option: $1 (see --help)" >&2
      exit 2
      ;;
  esac
  shift
done

# collect_descendants: recursively collect all descendant PIDs of a process.
# Prints one PID per line to stdout (root itself is NOT included).
collect_descendants() {
  local root_pid=$1
  local queue=("$root_pid")
  local visited=""
  while [ ${#queue[@]} -gt 0 ]; do
    local parent=${queue[0]}
    queue=("${queue[@]:1}")
    [[ "$parent" =~ ^[0-9]+$ ]] || continue
    case " $visited " in
      *" $parent "*) continue ;;
    esac
    visited="$visited $parent"
    local children
    if [ -d "/proc" ]; then
      children=$(pgrep -P "$parent" 2>/dev/null || true)
    else
      children=$(ps -o pid= -o ppid= | awk -v p="$parent" '$2 == p { print $1 }')
    fi
    if [ -n "$children" ]; then
      for child in $children; do
        echo "$child"
        queue+=("$child")
      done
    fi
  done
}

# pids_from_port: PIDs of processes listening on a TCP port.
pids_from_port() {
  local port=$1
  if command -v ss >/dev/null 2>&1; then
    ss -tlnp 2>/dev/null | grep ":$port" | grep -oP 'pid=\K[0-9]+' | sort -u || true
  elif command -v lsof >/dev/null 2>&1; then
    lsof -t -i ":$port" 2>/dev/null || true
  else
    # Fallback: /proc walk
    for pid in /proc/[0-9]*; do
      local p=${pid#/proc/}
      grep -q ":$port " "$pid/net/tcp" 2>/dev/null || true
      local inode
      inode=$(awk -v port="$(printf '%04X' "$port")" '$2 ~ /:0000$/ { split($2,a,":"); if (a[2]==port) print $10 }' "$pid/net/tcp" 2>/dev/null | head -1) || true
      if [ -n "$inode" ] && grep -q "$inode" "$pid/fdinfo" 2>/dev/null; then
        echo "$p"
      fi
    done
  fi
}

# is_process_in_tree: is $2 (a PID) a descendant of $1 (root PID)?
is_process_in_tree() {
  local root=$1
  local target=$2
  local t
  t=$(collect_descendants "$root")
  for p in $t; do
    [ "$p" = "$target" ] && return 0
  done
  return 1
}

# find_protected_roots: server PIDs that must never be killed.
find_protected_roots() {
  local roots=""
  roots="$roots $(pids_from_port "$MAIN_PORT")"
  local port
  for port in "${EXTRA_PORTS[@]}"; do
    roots="$roots $(pids_from_port "$port")"
  done
  # Fallback: exact command match for the clawbench binary
  if [ -z "$roots" ]; then
    roots=$(pgrep -x clawbench 2>/dev/null || true)
  fi
  # Expand each server PID to its full descendant set (the whole tree is protected)
  local expanded=""
  local root
  for root in $roots; do
    expanded="$expanded $root $(collect_descendants "$root")"
  done
  echo "$expanded"
}

PROTECTED_TREE=$(find_protected_roots)
PROTECTED_TREE=$(echo "$PROTECTED_TREE" | tr ' ' '\n' | grep -E '^[0-9]+$' | sort -un)

# ---- Collect zombies grouped by parent ----
declare -a ZOMBIE_PIDS=()
declare -a PARENT_PIDS=()
while IFS= read -r zpid; do
  [ -z "$zpid" ] && continue
  ZOMBIE_PIDS+=("$zpid")
  ppid=$(ps -o ppid= -p "$zpid" 2>/dev/null | tr -d ' ') || ppid="?"
  PARENT_PIDS+=("$ppid")
done < <(ps -eo pid,stat | awk '$2 ~ /^Z/ { print $1 }')

if [ ${#ZOMBIE_PIDS[@]} -eq 0 ]; then
  echo "No zombie processes found. All clean."
  exit 0
fi

# ---- Determine which parent trees to kill ----
declare -a KILL_ROOTS=()      # root PIDs of trees we will kill
declare -a SKIP_ROOTS=()      # protected / ineligible parents
declare -A ROOT_OF_ZOMBIE=()  # zombie pid -> chosen root pid

for i in "${!ZOMBIE_PIDS[@]}"; do
  z_zpid="${ZOMBIE_PIDS[$i]}"
  z_ppid="${PARENT_PIDS[$i]}"

  if ! [[ "$z_ppid" =~ ^[0-9]+$ ]]; then
    SKIP_ROOTS+=("?")
    echo "  [SKIP] zombie $z_zpid: cannot determine parent"
    continue
  fi
  # Zombie reparented to init (PID 1) — init will reap it automatically.
  if [ "$z_ppid" = "1" ]; then
    SKIP_ROOTS+=("$z_ppid")
    echo "  [SKIP] zombie $z_zpid: parent is init (reaped automatically)"
    continue
  fi

  # Find the highest ancestor that is still alive (the tree root).
  z_root="$z_ppid"
  while :; do
    z_gppid=$(ps -o ppid= -p "$z_root" 2>/dev/null | tr -d ' ') || break
    [[ "$z_gppid" =~ ^[0-9]+$ ]] || break
    [ "$z_gppid" = "1" ] || [ "$z_gppid" = "0" ] && break
    z_root="$z_gppid"
  done

  # Protection check: root is the server itself, or the zombie's parent chain
  # contains the protected server tree (e.g. tests spawned via the ACP bridge).
  z_protected=false
  for z_prot in $PROTECTED_TREE; do
    if [ "$z_root" = "$z_prot" ] || is_process_in_tree "$z_root" "$z_prot"; then
      z_protected=true
      break
    fi
  done

  if [ "$z_protected" = true ] && [ "$KILL_PROTECTED" != true ]; then
    SKIP_ROOTS+=("$z_root")
    echo "  [PROTECTED] zombie $z_zpid: parent tree root $z_root is the ClawBench server tree (port $MAIN_PORT). Not touched."
    continue
  fi

  # Avoid killing the same tree twice.
  z_dup=false
  for r in "${KILL_ROOTS[@]}"; do
    [ "$r" = "$z_root" ] && { z_dup=true; break; }
  done
  if [ "$z_dup" = true ]; then
    ROOT_OF_ZOMBIE["$z_zpid"]="$z_root"
    continue
  fi

  KILL_ROOTS+=("$z_root")
  ROOT_OF_ZOMBIE["$z_zpid"]="$z_root"
done

echo
echo "=== Zombie summary ==="
for i in "${!ZOMBIE_PIDS[@]}"; do
  z_zpid="${ZOMBIE_PIDS[$i]}"
  z_ppid="${PARENT_PIDS[$i]}"
  z_pcmd=$(ps -o args= -p "$z_ppid" 2>/dev/null | cut -c1-90 || echo "?")
  printf '  zombie %-8s parent %-8s %s\n' "$z_zpid" "$z_ppid" "$z_pcmd"
done

echo
echo "=== Trees that would be killed ==="
if [ ${#KILL_ROOTS[@]} -eq 0 ]; then
  echo "  (none)"
else
  for root in "${KILL_ROOTS[@]}"; do
    z_rcmd=$(ps -o args= -p "$root" 2>/dev/null | cut -c1-90 || echo "?")
    echo "  root $root: $z_rcmd"
  done
fi

echo
echo "=== Protected trees (not touched) ==="
if [ -z "$PROTECTED_TREE" ]; then
  echo "  (no ClawBench server found on port $MAIN_PORT)"
else
  for root in $PROTECTED_TREE; do
    z_rcmd=$(ps -o args= -p "$root" 2>/dev/null | cut -c1-90 || echo "?")
    echo "  pid $root: $z_rcmd"
  done
fi

if [ "$DO_KILL" != true ]; then
  echo
  echo "Dry-run: nothing was killed. Re-run with --kill to actually kill the trees above."
  exit 0
fi

if [ ${#KILL_ROOTS[@]} -eq 0 ]; then
  echo "Nothing to kill."
  exit 0
fi

if [ "$FORCE" != true ]; then
  echo
  read -r -p "Kill ${#KILL_ROOTS[@]} process tree(s) (killing their zombies)? [y/N] " answer
  case "$answer" in
    y|Y|yes) ;;
    *) echo "Aborted."; exit 1 ;;
  esac
fi

echo
echo "Killing ${#KILL_ROOTS[@]} process tree(s)..."
for root in "${KILL_ROOTS[@]}"; do
  # Children first, then root — prevents new zombies being left behind.
  z_all=$(collect_descendants "$root")
  if [ -n "$z_all" ]; then
    echo "$z_all" | sort -n -r | xargs -r kill -TERM 2>/dev/null || true
  fi
  kill -TERM "$root" 2>/dev/null || true
done

sleep 3

# Force-kill stragglers
for root in "${KILL_ROOTS[@]}"; do
  if kill -0 "$root" 2>/dev/null; then
    z_all=$(collect_descendants "$root")
    if [ -n "$z_all" ]; then
      echo "$z_all" | sort -n -r | xargs -r kill -KILL 2>/dev/null || true
    fi
    kill -KILL "$root" 2>/dev/null || true
  fi
done

sleep 1

echo
echo "=== Remaining zombies ==="
remaining=$(ps -eo pid,ppid,stat,args | awk '$3 ~ /^Z/ { printf "  %s parent %s %s\n", $1, $2, substr($0, index($0,$4), 70) }')
if [ -z "$remaining" ]; then
  echo "  (none — all cleaned)"
else
  echo "$remaining"
  echo "  (these are reparented to init or still reaping; they clear automatically)"
fi
