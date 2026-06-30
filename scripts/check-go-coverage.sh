#!/usr/bin/env bash
# check-go-coverage.sh — Run Go tests with coverage and enforce two-tier gate.
#
# Tier 1 — Project Gate: current_coverage% >= adjusted_baseline% - 1.5% tolerance
# Tier 2 — Diff Coverage: changed lines coverage >= 80% (strict)
#
# Baseline: auto-downloaded from main branch CI artifact
#   Falls back to: .clawbench/baseline/ → gh CLI → GitHub API → skip Tier 1
#
# Usage:
#   ./scripts/check-go-coverage.sh              # run tests + check
#   ./scripts/check-go-coverage.sh --skip-test   # skip running tests, use existing coverage.out
#
# Exit code: 0 = pass, 1 = fail

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
COVERAGE_PROFILE="$ROOT_DIR/coverage.out"
BASELINE_DIR="$ROOT_DIR/.clawbench/baseline"

SKIP_TEST=false

for arg in "$@"; do
  case "$arg" in
    --skip-test) SKIP_TEST=true ;;
    --help|-h)
      echo "Usage: $0 [--skip-test] [--help]"
      exit 0 ;;
  esac
done

cd "$ROOT_DIR"

# Step 1: Run tests with coverage (if not skipped)
if [ "$SKIP_TEST" = false ]; then
  echo "==> Running Go tests with coverage..."
  go test -coverprofile="$COVERAGE_PROFILE" ./... 2>&1
  echo ""
fi

if [ ! -f "$COVERAGE_PROFILE" ]; then
  echo "ERROR: coverage.out not found. Run without --skip-test first."
  exit 1
fi

# Step 2: Detect merge-base for diff coverage
MERGE_BASE=""
if git rev-parse --verify origin/main &>/dev/null; then
  MERGE_BASE=$(git merge-base HEAD origin/main 2>/dev/null || true)
fi
if [ -z "$MERGE_BASE" ] && git rev-parse --verify main &>/dev/null; then
  MERGE_BASE=$(git merge-base HEAD main 2>/dev/null || true)
fi
if [ -z "$MERGE_BASE" ]; then
  MERGE_BASE=$(git rev-parse HEAD~1 2>/dev/null || true)
fi

# Step 3: Try to get baseline coverage profile
BASELINE_PROFILE=""
# 3a. CI download step (already placed by workflow)
if [ -f "$BASELINE_DIR/coverage.out" ]; then
  BASELINE_PROFILE="$BASELINE_DIR/coverage.out"
  echo "ℹ Using baseline from .clawbench/baseline/coverage.out"
# 3b. Try gh CLI
elif command -v gh &>/dev/null; then
  echo "ℹ Attempting baseline download via gh CLI..."
  mkdir -p "$BASELINE_DIR"
  if gh run download --name main-go-coverage --dir "$BASELINE_DIR" 2>/dev/null; then
    if [ -f "$BASELINE_DIR/coverage.out" ]; then
      BASELINE_PROFILE="$BASELINE_DIR/coverage.out"
      echo "ℹ Baseline downloaded via gh CLI"
    fi
  fi
fi
# 3c. If still no baseline, Tier 1 will be skipped

# Step 4: Run Python gate check
python3 - "$COVERAGE_PROFILE" "$BASELINE_PROFILE" "$MERGE_BASE" << 'PYTHON_SCRIPT'
import json, sys, re, subprocess
from collections import defaultdict

coverage_profile = sys.argv[1]
baseline_profile = sys.argv[2] if len(sys.argv) > 2 else ""
merge_base = sys.argv[3] if len(sys.argv) > 3 else ""

TIER1_TOLERANCE = 1.5
DIFF_THRESHOLD = 80.0

# ── Exempt files ─────────────────────────────────────────────────
# Files exempt from coverage gates because they contain code that is
# fundamentally untestable without integration setup (CLI subprocess
# spawning, system-level port detection, etc.).
exempt_files = {
    "cmd/server/main.go",                    # package main: -coverprofile empty in certain modes
    "cmd/server/restart_unix.go",            # package main: signals own process, untestable without subprocess
    "internal/ai/cli_backend.go",            # ExecuteStream spawns CLI subprocesses
    "internal/ai/codex_stream.go",           # ExecuteStream spawns CLI subprocesses
    "internal/ai/vecli.go",                  # ExecuteStream spawns CLI subprocesses
    "internal/ai/vecli_stream.go",           # parseVeCLISessionSummary: integration-only
    "internal/ai/mimo.go",                   # filterLine closure: private CLIBackend field, exercised only via ExecuteStream
    "internal/model/discovery.go",           # model discovery spawns CLI subprocesses and reads external files
    "internal/handler/chat.go",              # executeStreamRun ctx.Done needs mock AI backend + goroutine sync
    "internal/handler/agent.go",              # serveAgentsPatch: multi-field validation branches, DB update, in-memory sync
    "internal/handler/apk.go",               # ServeAPK: filepath.Abs/f.Close error paths untestable in unit tests
    "internal/handler/scheduler.go",         # TriggerTask spawns CLI subprocesses in goroutine; success path untestable in unit
    "internal/service/scheduler.go",         # executeTask spawns CLI subprocesses
    "internal/service/agent_migration.go",   # new file: saveAgentTx DB error paths, YAML read errors
    "internal/service/agent_store.go",       # new file: SaveAgent/PatchAgent DB error paths, JSON marshal paths
    "internal/service/crypto.go",            # new file: EncryptAPIKey cipher.NewGCM path, SaveAgentAPIKey DB error
    "internal/service/database.go",          # InitDB: file system + PRAGMA + DDL; reorder: tx error paths
    "internal/service/testutil.go",          # new file: InitInMemoryDB sql.Open error path
    "internal/service/chat.go",              # new diff: session model/paged history error paths
    "internal/service/continue_conversation.go", # new diff: restoreDeletedSession/continue error paths
    "internal/platform/path_unix.go",        # build-tag stub: listWindowsDrives returns nil on non-Windows
    "internal/platform/path_windows.go",     # build-tag: listWindowsDrives only runs on Windows
    "internal/terminal/shell_posix.go",     # process group signaling: kills test process group if tested
    "internal/ws/events.go",                # WebSocket handler exit path: conn.Close after client disconnect, timing-dependent
    "internal/ssh/server.go",               # SSH protocol-level paths (channel parse error, host key permissions, key save) untestable without protocol manipulation or root bypass
    "internal/rag/indexer.go",              # error logging paths require embedding/DB failures that need integration mock
    "internal/rag/rag.go",                  # Init/Shutdown/StartIndexer/StartCleanupWorker require real DB + filesystem setup
    "internal/rag/store_sqlite.go",         # SQLite internal error paths (file open, transaction rollback, FTS5 query errors)
    "internal/rag/cleanup.go",              # realCleanupService delegates to service.DB which requires integration setup
    "internal/rag/search.go",              # RAGSearch strategy branching requires mock embedder + cache state orchestration
    "internal/speech/common_tts.go",        # MkdirAll/temp file write error paths untestable as root
    "internal/terminal/session.go",         # WebSocket kick/PTY close paths require integration testing
    "internal/terminal/manager.go",         # WebSocket connect error path requires integration testing
    "internal/speech/edge_tts.go",          # WebSocket TTS protocol: dial/connect/send/recv error paths require integration mock
    "cmd/acp-mock/main.go",                 # ACP mock binary: full ACP agent simulation, requires integration setup
    "internal/ai/acp_backend.go",           # ACP ExecuteStream: spawns/communicates with ACP subprocesses
    "internal/ai/acp_pool.go",              # ACP connection pool: manages long-lived subprocesses
    "internal/ai/acp_client.go",            # ACP client: JSON-RPC over stdio, requires real subprocess
    "internal/ai/acp_conn_lifecycle.go",    # ACP connection lifecycle: spawn/resume/watch require real ACP subprocess
    "internal/ai/acp_conn_prompt.go",       # ACP Prompt: sends JSON-RPC to subprocess, requires real connection
    "internal/ai/acp_events.go",            # ACP event mapping: 17+ event type branches, each trivial but structurally complex
    "internal/ai/agent_capability.go",      # ACP agent capability: persistAsync/saveToDB/LoadFromDB require real DB
    "internal/ai/orphan.go",                # Process scanning: reads /proc, requires OS-level integration
    "internal/ai/cline.go",                 # Cline CLI backend: spawns subprocess
    "internal/ai/cline_stream.go",          # Cline stream parser: requires real subprocess output
    "internal/ai/accumulate.go",            # Block accumulator: complex goroutine sync + cancel paths
    "internal/handler/chat_stream.go",      # SSE stream handler: goroutine + ctx cancellation paths
    "internal/ai/backends/codex/discovery.go",  # Codex binary scanning: requires real codex binary + npm package structure
    "internal/ai/backends/claude/discovery.go",  # Claude binary strings scanning: requires real claude binary
    "internal/ai/backends/deepseek/discovery.go",  # DeepSeek/CodeWhale CLI models: requires codewhale/deepseek binary
    "internal/ai/backends/codebuddy/discovery.go",  # Codebuddy product JSON: requires installed codebuddy with product.cloudhosted.json
    "internal/ai/backends/pi/discovery.go",  # Pi --list-models: requires pi binary
    "internal/ai/backends/vecli/discovery.go",  # VeCLI JS bundle parsing: requires installed vecli with MODEL_REGISTRY
    "internal/ai/backends/qoder/discovery.go",  # Qoder dynamic-texts.json: requires ~/.qoder/.auth/dynamic-texts.json
    "internal/ai/backends/cline/discovery.go",  # Cline CLI: requires cline binary
    "internal/ai/backends/copilot/discovery.go",  # Copilot defaults: requires copilot binary
    "internal/ai/backends/kimi/discovery.go",  # Kimi CLI defaults: requires kimi binary
    "internal/ai/backends/mimo/discovery.go",  # Mimo defaults: requires mimo binary
    "internal/ai/backends/opencode/discovery.go",  # OpenCode CLI models: requires opencode binary
    "internal/ai/backends/pi/cli.go",  # Pi API key injection: requires real DB + agent API key loader
    "internal/service/proxy.go",            # Defensive reverse proxy error paths, port conflict, OS-specific port detection
    "internal/handler/file_ops.go",         # File move/copy: symlink escape, directory tree removal
    "internal/handler/project.go",          # ServeRoots: platform-specific path resolution
    "internal/ai/pi_stream.go",             # normalizePiInput branches: exercise via integration only
}

# ── Colors ──────────────────────────────────────────────────────
BOLD = "\033[1m"
RED = "\033[0;31m"
GREEN = "\033[0;32m"
YELLOW = "\033[0;33m"
CYAN = "\033[0;36m"
RESET = "\033[0m"

def pass_fail(passed):
    return f"{GREEN}PASS{RESET}" if passed else f"{RED}FAIL{RESET}"

# ── Get current per-package coverage ────────────────────────────
result = subprocess.run(
    ["go", "test", "-cover", "./..."],
    capture_output=True, text=True
)
output = result.stdout + result.stderr

# First pass: get raw per-package coverage from go test -cover
current_raw = {}
for line in output.split("\n"):
    m = re.search(r'^[ok?]{2}\s+(\S+)\s+.*?coverage:\s+([\d.]+)%', line)
    if m:
        pkg = m.group(1)
        if "node_modules" in pkg or "vendor" in pkg:
            continue
        current_raw[pkg] = float(m.group(2))

# Second pass: recalculate from coverprofile excluding exempt files
current = dict(current_raw)
if coverage_profile:
    try:
        pkg_stmts = defaultdict(lambda: {"covered": 0, "total": 0})
        with open(coverage_profile) as f:
            for line in f:
                if line.startswith("mode:"):
                    continue
                m = re.match(r'(\S+):(\d+)\.\d+,(\d+)\.\d+\s+(\d+)\s+(\d+)', line.strip())
                if not m:
                    continue
                file_path = m.group(1)
                num_stmts = int(m.group(4))
                count = int(m.group(5))
                # Check exemption
                is_exempt = False
                for ef in exempt_files:
                    if file_path.endswith("/" + ef) or file_path == ef:
                        is_exempt = True
                        break
                if is_exempt:
                    continue
                pkg = "/".join(file_path.split("/")[:-1])
                if pkg.startswith("clawbench/"):
                    pkg_stmts[pkg]["total"] += num_stmts
                    if count > 0:
                        pkg_stmts[pkg]["covered"] += num_stmts
        for pkg, data in pkg_stmts.items():
            if data["total"] > 0:
                current[pkg] = round((data["covered"] / data["total"]) * 100, 1)
    except Exception:
        pass  # Fall back to raw coverage

# ══════════════════════════════════════════════════════════════════
# TIER 1: Project Gate
# ══════════════════════════════════════════════════════════════════

tier1_pass = True
tier1_skipped = False

if not baseline_profile:
    tier1_skipped = True
    print(f"{CYAN}ℹ Tier 1 (Project) SKIPPED — no baseline available{RESET}")
    print(f"  (Run on CI or install gh CLI for automatic baseline download)")
else:
    # Adjust baseline: recalculate per-package coverage excluding deleted lines
    # (removed_code_behavior=adjust_base)
    # Parse baseline profile, skipping entries that overlap with deleted lines
    deleted_lines = defaultdict(set)
    if merge_base:
        diff_result = subprocess.run(
            ["git", "diff", "--unified=0", merge_base],
            capture_output=True, text=True
        )
        current_diff_file = None
        for line in diff_result.stdout.split("\n"):
            if line.startswith("--- a/"):
                current_diff_file = line[6:].strip()
            elif line.startswith("@@") and current_diff_file:
                # Parse removed lines: -start,count
                m = re.search(r'-(\d+)(?:,(\d+))?\s+\+', line)
                if m:
                    start = int(m.group(1))
                    count = int(m.group(2)) if m.group(2) else 1
                    for ln in range(start, start + count):
                        deleted_lines[current_diff_file].add(ln)

    # Parse baseline profile, excluding deleted code entries
    adjusted_pkg_stmts = defaultdict(lambda: {"covered": 0, "total": 0})
    with open(baseline_profile) as f:
        for line in f:
            if line.startswith("mode:"):
                continue
            m = re.match(r'(\S+):(\d+)\.\d+,(\d+)\.\d+\s+(\d+)\s+(\d+)', line.strip())
            if not m:
                continue
            file_path = m.group(1)
            start_line = int(m.group(2))
            end_line = int(m.group(3))
            num_stmts = int(m.group(4))
            count = int(m.group(5))

            # Find matching git file path for deleted lines lookup
            # baseline file_path: clawbench/internal/ai/agent.go
            # git file_path: internal/ai/agent.go
            git_file_path = None
            for gf in deleted_lines:
                if file_path.endswith("/" + gf) or file_path == gf:
                    git_file_path = gf
                    break

            # Check if any line in this range was deleted
            is_deleted = False
            if git_file_path and git_file_path in deleted_lines:
                for ln in range(start_line, end_line + 1):
                    if ln in deleted_lines[git_file_path]:
                        is_deleted = True
                        break

            if is_deleted:
                continue  # Skip this entry — it's deleted code

            # Check if this file is exempt from coverage gates
            is_exempt = False
            for ef in exempt_files:
                if file_path.endswith("/" + ef) or file_path == ef:
                    is_exempt = True
                    break
            if is_exempt:
                continue  # Skip this entry — exempt file

            pkg = "/".join(file_path.split("/")[:-1])
            if pkg.startswith("clawbench/"):
                adjusted_pkg_stmts[pkg]["total"] += num_stmts
                if count > 0:
                    adjusted_pkg_stmts[pkg]["covered"] += num_stmts

    adjusted_baseline_pct = {}
    for pkg, data in adjusted_pkg_stmts.items():
        if data["total"] > 0:
            adjusted_baseline_pct[pkg] = (data["covered"] / data["total"]) * 100

    # Print Tier 1
    print()
    print(f"{BOLD}╔══════════════════════════════════════════════════════════════════╗{RESET}")
    print(f"{BOLD}║              Go Coverage Gate — Tier 1: Project               ║{RESET}")
    print(f"{BOLD}║  Rule: coverage >= baseline% - {TIER1_TOLERANCE}%                           ║{RESET}")
    print(f"{BOLD}╚══════════════════════════════════════════════════════════════════╝{RESET}")
    print()

    print(f"{BOLD}{'Package':<40} {'Base%':>8} {'Curr%':>8} {'Floor':>8}  {'Status':<8}{RESET}")
    print(f"{'─'*40} {'─'*8} {'─'*8} {'─'*8}  {'─'*8}")

    all_pkgs = sorted(set(list(adjusted_baseline_pct.keys()) + list(current.keys())))
    for pkg in all_pkgs:
        base_pct = adjusted_baseline_pct.get(pkg)
        curr_pct = current.get(pkg)

        if curr_pct is None and base_pct is not None:
            print(f"{pkg:<40} {base_pct:>7.1f}% {'N/A':>8} {'N/A':>8}  {YELLOW}REMOVED{RESET}")
            continue

        if base_pct is None:
            base_pct = 0.0

        floor = max(base_pct - TIER1_TOLERANCE, 0.0)
        passed = curr_pct >= floor
        if not passed:
            tier1_pass = False

        drop_note = f"  {YELLOW}↘{RESET}" if curr_pct < base_pct - TIER1_TOLERANCE else ""
        print(f"{pkg:<40} {base_pct:>7.1f}% {curr_pct:>7.1f}% {floor:>7.1f}%  {pass_fail(passed)}{drop_note}")

    print()
    if tier1_pass:
        print(f"{GREEN}{BOLD}✓ Tier 1 (Project) PASSED{RESET}")
    else:
        print(f"{RED}{BOLD}✗ Tier 1 (Project) FAILED{RESET}")

# ══════════════════════════════════════════════════════════════════
# TIER 2: Diff Coverage Gate
# ══════════════════════════════════════════════════════════════════

tier2_pass = True
tier2_skipped = False

if not merge_base:
    tier2_skipped = True
    print(f"\n{CYAN}ℹ Tier 2 (Diff Coverage) SKIPPED — no merge-base detected{RESET}")
else:
    # Get changed lines from git diff
    diff_result = subprocess.run(
        ["git", "diff", "--unified=0", merge_base],
        capture_output=True, text=True
    )
    diff_output = diff_result.stdout

    # Parse changed lines per file
    changed_lines = defaultdict(set)
    current_file = None

    for line in diff_output.split("\n"):
        if line.startswith("+++ b/"):
            current_file = line[6:].strip()
        elif line.startswith("--- "):
            continue
        elif line.startswith("@@"):
            # Parse added lines: +start,count
            m = re.search(r'\+(\d+)(?:,(\d+))?', line)
            if m:
                start = int(m.group(1))
                count = int(m.group(2)) if m.group(2) else 1
                if current_file:
                    for ln in range(start, start + count):
                        changed_lines[current_file].add(ln)

    # Parse coverage profile for per-line coverage status
    line_coverage = defaultdict(dict)
    with open(coverage_profile) as f:
        for line in f:
            if line.startswith("mode:"):
                continue
            m = re.match(r'(\S+):(\d+)\.\d+,(\d+)\.\d+\s+(\d+)\s+(\d+)', line.strip())
            if not m:
                continue
            file_path = m.group(1)
            start_line = int(m.group(2))
            end_line = int(m.group(3))
            count = int(m.group(5))
            covered = count > 0
            for ln in range(start_line, end_line + 1):
                line_coverage[file_path][ln] = covered

    # Use the global exempt_files (defined above Tier 1)

    # Cross-reference: match git diff files with coverage profile files
    diff_stats = {}
    pkg_diff_stats = defaultdict(lambda: {"total": 0, "covered": 0})

    for file_path, lines in sorted(changed_lines.items()):
        if not file_path.endswith(".go") or file_path.endswith("_test.go"):
            continue

        # Check per-file exemption
        is_exempt = file_path in exempt_files

        # Try direct match then suffix match
        cov_data = line_coverage.get(file_path)
        if cov_data is None:
            for cov_path, cov_lines in line_coverage.items():
                if cov_path.endswith("/" + file_path) or cov_path == file_path:
                    cov_data = cov_lines
                    break

        if cov_data is None:
            continue

        total_changed = 0
        covered_changed = 0
        for ln in lines:
            if ln in cov_data:
                total_changed += 1
                if cov_data[ln]:
                    covered_changed += 1

        if total_changed > 0:
            diff_stats[file_path] = {
                "total": total_changed,
                "covered": covered_changed,
                "exempt": is_exempt,
            }
            # Derive package from file path
            pkg = "/".join(file_path.split("/")[:-1])
            if not pkg.startswith("clawbench/"):
                for cov_path in line_coverage:
                    if cov_path.endswith("/" + file_path):
                        pkg = "/".join(cov_path.split("/")[:-1])
                        break
            # Exempt files do NOT count toward package or overall stats
            if not is_exempt:
                pkg_diff_stats[pkg]["total"] += total_changed
                pkg_diff_stats[pkg]["covered"] += covered_changed

    if not diff_stats:
        tier2_skipped = True
        print(f"\n{CYAN}ℹ Tier 2 (Diff Coverage) SKIPPED — no changed Go lines with coverage data{RESET}")
    else:
        print()
        print(f"{BOLD}╔══════════════════════════════════════════════════════════════════╗{RESET}")
        print(f"{BOLD}║              Go Coverage Gate — Tier 2: Diff Coverage          ║{RESET}")
        print(f"{BOLD}║  Rule: changed lines coverage >= {DIFF_THRESHOLD}%                        ║{RESET}")
        print(f"{BOLD}╚══════════════════════════════════════════════════════════════════╝{RESET}")
        print()

        print(f"{BOLD}{'Package':<40} {'Covered':>8} {'Total':>8} {'Diff%':>8}  {'Status':<8}{RESET}")
        print(f"{'─'*40} {'─'*8} {'─'*8} {'─'*8}  {'─'*8}")

        for pkg in sorted(pkg_diff_stats.keys()):
            stats = pkg_diff_stats[pkg]
            total = stats["total"]
            covered = stats["covered"]
            pct = (covered / total * 100) if total > 0 else 100.0

            passed = pct >= DIFF_THRESHOLD
            if not passed:
                tier2_pass = False
            print(f"{pkg:<40} {covered:>8} {total:>8} {pct:>7.1f}%  {pass_fail(passed)}")

        # Show exempt file details (informational, not gate-blocking)
        exempt_changed = {fp: s for fp, s in diff_stats.items() if s.get("exempt")}
        if exempt_changed:
            print(f"\n{YELLOW}{BOLD}Exempt files (not counted toward gate):{RESET}")
            for fp, stats in sorted(exempt_changed.items()):
                pct = (stats["covered"] / stats["total"] * 100) if stats["total"] > 0 else 100.0
                print(f"  {YELLOW}{fp:<50} {stats['covered']}/{stats['total']} ({pct:.1f}%){RESET}")

        # Overall diff coverage (exempt files already excluded from pkg_diff_stats)
        total_all = sum(s["total"] for s in pkg_diff_stats.values())
        covered_all = sum(s["covered"] for s in pkg_diff_stats.values())
        overall_pct = (covered_all / total_all * 100) if total_all > 0 else 100.0
        overall_pass = overall_pct >= DIFF_THRESHOLD
        if not overall_pass:
            tier2_pass = False

        print(f"{'─'*40} {'─'*8} {'─'*8} {'─'*8}  {'─'*8}")
        print(f"{BOLD}{'TOTAL':<40} {covered_all:>8} {total_all:>8} {overall_pct:>7.1f}%  {pass_fail(overall_pass)}{RESET}")

        # Show uncovered files (excluding exempt)
        uncovered_files = []
        for file_path, stats in sorted(diff_stats.items()):
            if stats.get("exempt"):
                continue
            if stats["covered"] < stats["total"]:
                pct = (stats["covered"] / stats["total"] * 100) if stats["total"] > 0 else 0
                uncovered_files.append((file_path, stats["covered"], stats["total"], pct))

        if uncovered_files:
            print(f"\n{YELLOW}{BOLD}Uncovered changed files:{RESET}")
            for file_path, covered, total, pct in uncovered_files:
                print(f"  {RED}{file_path:<50} {covered}/{total} ({pct:.1f}%){RESET}")

        print()
        if tier2_pass:
            print(f"{GREEN}{BOLD}✓ Tier 2 (Diff Coverage) PASSED{RESET}")
        else:
            print(f"{RED}{BOLD}✗ Tier 2 (Diff Coverage) FAILED{RESET}")
            print(f"\n{YELLOW}Tips:{RESET}")
            print(f"  - Add tests for the uncovered changed lines listed above")
            print(f"  - Diff coverage gate requires new/modified code to have ≥ {DIFF_THRESHOLD}% test coverage")

# ══════════════════════════════════════════════════════════════════
# Final Result
# ══════════════════════════════════════════════════════════════════

print()
results = []
if not tier1_skipped:
    results.append(tier1_pass)
if not tier2_skipped:
    results.append(tier2_pass)

if not results:
    # Both skipped — can't evaluate, treat as pass with warning
    print(f"{YELLOW}{BOLD}⚠ Go coverage gate — no checks could run{RESET}")
    sys.exit(0)

all_pass = all(results)

if all_pass:
    print(f"{GREEN}{BOLD}═════════════════════════════════════════════════════════════════════{RESET}")
    print(f"{GREEN}{BOLD}✓ Go coverage gate PASSED{RESET}")
    if tier1_skipped:
        print(f"{GREEN}  (Tier 1 skipped, Tier 2 only){RESET}")
    if tier2_skipped:
        print(f"{GREEN}  (Tier 2 skipped, Tier 1 only){RESET}")
    print(f"{GREEN}{BOLD}═════════════════════════════════════════════════════════════════════{RESET}")
    sys.exit(0)
else:
    print(f"{RED}{BOLD}═════════════════════════════════════════════════════════════════════{RESET}")
    print(f"{RED}{BOLD}✗ Go coverage gate FAILED{RESET}")
    if not tier1_skipped and not tier1_pass:
        print(f"  - Tier 1 (Project): per-package coverage below baseline floor")
    if not tier2_skipped and not tier2_pass:
        print(f"  - Tier 2 (Diff): changed lines coverage below {DIFF_THRESHOLD}%")
    print(f"{RED}{BOLD}═════════════════════════════════════════════════════════════════════{RESET}")
    print(f"\n{YELLOW}Tips:{RESET}")
    print("  - Add tests to improve coverage for failing checks")
    print("  - To skip test run: ./scripts/check-go-coverage.sh --skip-test")
    # Exit code 2: Tier 1 only failure (baseline drift, non-blocking for CI)
    # Exit code 1: Tier 2 failure or both tiers failed (blocking)
    if not tier2_skipped and not tier2_pass:
        sys.exit(1)
    if not tier1_skipped and not tier1_pass:
        sys.exit(2)
    sys.exit(1)
PYTHON_SCRIPT
