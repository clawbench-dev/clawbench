#!/usr/bin/env bash
# check-frontend-coverage.sh — Run frontend tests with coverage and enforce two-tier gate.
#
# Tier 1 — Project Gate: current statements% >= baseline% - 1.5% tolerance
# Tier 2 — Diff Coverage: changed lines coverage >= 80% (strict)
#
# Excluded: src/i18n/locales (translation dictionaries)
#
# Usage:
#   ./scripts/check-frontend-coverage.sh              # run tests + check
#   ./scripts/check-frontend-coverage.sh --skip-test  # skip tests, use existing coverage
#
# Exit code: 0 = pass, 1 = fail

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
WEB_DIR="$ROOT_DIR/web"
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
  echo "==> Running frontend tests with coverage..."
fi

result=0
if [ "$SKIP_TEST" = false ]; then
  npx --prefix "$WEB_DIR" vitest run --coverage || result=$?
  if [ $result -ne 0 ]; then
    echo "ERROR: Frontend tests failed. Fix test failures before checking coverage."
    exit 1
  fi
else
  # --skip-test: just verify coverage data exists
  if [ ! -f "$ROOT_DIR/coverage/coverage-summary.json" ] || [ ! -f "$ROOT_DIR/coverage/coverage-final.json" ]; then
    echo "ERROR: coverage data not found. Run without --skip-test first."
    exit 1
  fi
fi

# Coverage data: vitest writes to <root>/coverage/ (not web/coverage/)
# because vitest.config.ts lives at project root
COVERAGE_JSON="$ROOT_DIR/coverage/coverage-summary.json"
COVERAGE_FINAL="$ROOT_DIR/coverage/coverage-final.json"

if [ ! -f "$COVERAGE_JSON" ]; then
  echo "ERROR: coverage-summary.json not found. Run without --skip-test first."
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

# Step 3: Try to get baseline
BASELINE_JSON=""
# CI artifact may have flat files or coverage/ subdirectory
for _bf in "$BASELINE_DIR/coverage-summary.json" "$BASELINE_DIR/coverage/coverage-summary.json"; do
  if [ -f "$_bf" ]; then
    BASELINE_JSON="$_bf"
    echo "ℹ Using baseline from .clawbench/baseline/"
    break
  fi
done
if [ -z "$BASELINE_JSON" ] && command -v gh &>/dev/null; then
  echo "ℹ Attempting baseline download via gh CLI..."
  mkdir -p "$BASELINE_DIR"
  if gh run download --name main-frontend-coverage --dir "$BASELINE_DIR" 2>/dev/null; then
    for _bf in "$BASELINE_DIR/coverage-summary.json" "$BASELINE_DIR/coverage/coverage-summary.json"; do
      if [ -f "$_bf" ]; then
        BASELINE_JSON="$_bf"
        echo "ℹ Baseline downloaded via gh CLI"
        break
      fi
    done
  fi
fi

# Step 4: Run Python gate check
python3 - "$COVERAGE_JSON" "$COVERAGE_FINAL" "$BASELINE_JSON" "$MERGE_BASE" << 'PYTHON_SCRIPT'
import json, sys, re, subprocess
from collections import defaultdict

coverage_json = sys.argv[1]
coverage_final = sys.argv[2] if len(sys.argv) > 2 else ""
baseline_json = sys.argv[3] if len(sys.argv) > 3 else ""
merge_base = sys.argv[4] if len(sys.argv) > 4 else ""

TIER1_TOLERANCE = 1.5
DIFF_THRESHOLD = 80.0
EXCLUDED = {"src/i18n"}  # i18n locale dict aggregation directory

# Individual files to exclude from Tier 1 coverage calculation.
# These have intrinsically low coverage (DOM/IPC deps, CLI subprocess, etc.)
# and drag down the per-directory weighted average below the baseline floor.
TIER1_EXCLUDED_FILES = {
    "src/utils/globals.ts",  # Pure re-exports of third-party libraries (marked, hljs, katex, DOMPurify, mermaid)
    "src/utils/diff.ts",     # Diff rendering: depends on hljs + DOM rendering, needs browser environment
    "src/utils/api.ts",      # HTTP API client: depends on fetch + server, needs integration test
    "src/utils/app.ts",      # App store bootstrap: global state init, needs full app context
}

BOLD = "\033[1m"
RED = "\033[0;31m"
GREEN = "\033[0;32m"
YELLOW = "\033[0;33m"
CYAN = "\033[0;36m"
RESET = "\033[0m"

def pass_fail(passed):
    return f"{GREEN}PASS{RESET}" if passed else f"{RED}FAIL{RESET}"

# ── Helper: extract src/... path from absolute or relative path ──
def extract_src_path(path):
    """Extract the src/... portion from a path like /abs/path/web/src/components/Foo.vue"""
    idx = path.find("/src/")
    if idx >= 0:
        return path[idx + 1:]  # e.g., src/components/Foo.vue
    if path.startswith("src/"):
        return path
    return None

def extract_web_src_path(path):
    """Extract the web/src/... portion from a path like /abs/path/web/src/components/Foo.vue"""
    idx = path.find("/web/src/")
    if idx >= 0:
        return path[idx + 1:]  # e.g., web/src/components/Foo.vue
    if path.startswith("web/src/"):
        return path
    return None

# ── Parse current coverage-summary.json ────────────────────────
with open(coverage_json) as f:
    summary = json.load(f)

# Aggregate per top-level directory under src/ using weighted average
dir_stmts = defaultdict(lambda: {"covered": 0, "total": 0})
for dir_path, data in summary.items():
    src_path = extract_src_path(dir_path)
    if not src_path:
        continue
    # Skip individually excluded files
    if src_path in TIER1_EXCLUDED_FILES:
        continue
    # Aggregate per top-level directory under src/
    parts = src_path.split("/")
    if len(parts) >= 2:
        top_dir = "/".join(parts[:2])  # e.g., src/components
    else:
        continue
    stmt_data = data.get("statements", {})
    covered = stmt_data.get("covered", 0)
    total = stmt_data.get("total", 0)
    dir_stmts[top_dir]["covered"] += covered
    dir_stmts[top_dir]["total"] += total

current = {}
for dir_name, data in dir_stmts.items():
    if data["total"] > 0:
        current[dir_name] = (data["covered"] / data["total"]) * 100
    else:
        current[dir_name] = 0.0

# ══════════════════════════════════════════════════════════════════
# TIER 1: Project Gate
# ══════════════════════════════════════════════════════════════════

tier1_pass = True
tier1_skipped = False

if not baseline_json:
    tier1_skipped = True
    print(f"{CYAN}ℹ Tier 1 (Project) SKIPPED — no baseline available{RESET}")
    print(f"  (Run on CI or install gh CLI for automatic baseline download)")
else:
    with open(baseline_json) as f:
        baseline_summary = json.load(f)

    baseline_dir_stmts = defaultdict(lambda: {"covered": 0, "total": 0})
    for dir_path, data in baseline_summary.items():
        src_path = extract_src_path(dir_path)
        if not src_path:
            continue
        parts = src_path.split("/")
        if len(parts) >= 2:
            top_dir = "/".join(parts[:2])
        else:
            continue
        stmt_data = data.get("statements", {})
        covered = stmt_data.get("covered", 0)
        total = stmt_data.get("total", 0)
        baseline_dir_stmts[top_dir]["covered"] += covered
        baseline_dir_stmts[top_dir]["total"] += total

    baseline = {}
    for dir_name, data in baseline_dir_stmts.items():
        if data["total"] > 0:
            baseline[dir_name] = (data["covered"] / data["total"]) * 100
        else:
            baseline[dir_name] = 0.0

    print()
    print(f"{BOLD}╔══════════════════════════════════════════════════════════════════╗{RESET}")
    print(f"{BOLD}║           Frontend Coverage Gate — Tier 1: Project             ║{RESET}")
    print(f"{BOLD}║  Rule: statements% >= baseline% - {TIER1_TOLERANCE}%                           ║{RESET}")
    print(f"{BOLD}╚══════════════════════════════════════════════════════════════════╝{RESET}")
    print()

    print(f"{BOLD}{'Directory':<25} {'Base%':>8} {'Curr%':>8} {'Floor':>8}  {'Status':<8}{RESET}")
    print(f"{'─'*25} {'─'*8} {'─'*8} {'─'*8}  {'─'*8}")

    all_dirs = sorted(set(list(baseline.keys()) + list(current.keys())))
    for dir_name in all_dirs:
        if dir_name in EXCLUDED:
            base_pct = baseline.get(dir_name, 0)
            curr_pct = current.get(dir_name, 0)
            print(f"{dir_name:<25} {base_pct:>7.1f}% {curr_pct:>7.1f}% {'N/A':>8}  {YELLOW}SKIP{RESET}")
            continue

        base_pct = baseline.get(dir_name)
        curr_pct = current.get(dir_name)

        if curr_pct is None and base_pct is not None:
            print(f"{dir_name:<25} {base_pct:>7.1f}% {'N/A':>8} {'N/A':>8}  {YELLOW}REMOVED{RESET}")
            continue

        if base_pct is None:
            base_pct = 0.0

        floor = max(base_pct - TIER1_TOLERANCE, 0.0)
        passed = curr_pct >= floor
        if not passed:
            tier1_pass = False

        drop_note = f"  {YELLOW}↘{RESET}" if curr_pct < base_pct - TIER1_TOLERANCE else ""
        print(f"{dir_name:<25} {base_pct:>7.1f}% {curr_pct:>7.1f}% {floor:>7.1f}%  {pass_fail(passed)}{drop_note}")

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
elif not coverage_final or not __import__('os').path.exists(coverage_final):
    tier2_skipped = True
    print(f"\n{CYAN}ℹ Tier 2 (Diff Coverage) SKIPPED — no coverage-final.json{RESET}")
else:
    # Get changed lines from git diff
    diff_result = subprocess.run(
        ["git", "diff", "--unified=0", merge_base, "--", "web/src/"],
        capture_output=True, text=True
    )
    diff_output = diff_result.stdout

    # Parse changed lines
    changed_lines = defaultdict(set)
    current_file = None

    for line in diff_output.split("\n"):
        if line.startswith("+++ b/"):
            current_file = line[6:].strip()
        elif line.startswith("--- "):
            continue
        elif line.startswith("@@"):
            m = re.search(r'\+(\d+)(?:,(\d+))?', line)
            if m:
                start = int(m.group(1))
                count = int(m.group(2)) if m.group(2) else 1
                if current_file:
                    for ln in range(start, start + count):
                        changed_lines[current_file].add(ln)

    # Parse coverage-final.json (Istanbul format)
    with open(coverage_final) as f:
        istanbul = json.load(f)

    # Build line coverage map: file -> {line: covered}
    # Normalize Istanbul absolute paths to web/src/... for git diff matching
    line_coverage = defaultdict(dict)
    for file_path, file_data in istanbul.items():
        # Normalize: extract web/src/... or src/... from absolute paths
        norm_path = extract_web_src_path(file_path)
        if not norm_path:
            norm_path = extract_src_path(file_path)
            if norm_path:
                norm_path = "web/" + norm_path  # add web/ prefix for git diff matching
        if not norm_path:
            continue
        stmt_map = file_data.get("statementMap", {})
        stmts = file_data.get("s", {})
        # Map statement index to coverage
        for idx, stmt_info in stmt_map.items():
            if idx in stmts:
                start_line = stmt_info.get("start", {}).get("line", 0)
                end_line = stmt_info.get("end", {}).get("line", 0)
                if start_line == 0:
                    continue  # skip invalid entries
                covered = stmts[idx] > 0
                for ln in range(start_line, end_line + 1):
                    # OR logic: if ANY statement on this line is covered, the line is covered
                    line_coverage[norm_path][ln] = line_coverage[norm_path].get(ln, False) or covered

    # Cross-reference: Istanbul paths are now normalized to web/src/...
    diff_stats = {}
    dir_diff_stats = defaultdict(lambda: {"total": 0, "covered": 0})

    for file_path, lines in sorted(changed_lines.items()):
        if not (file_path.endswith(".ts") or file_path.endswith(".vue")):
            continue
        # Exclude test files from diff coverage check
        if file_path.endswith(".test.ts") or file_path.endswith(".spec.ts"):
            continue
        # Exclude i18n locale dictionaries
        if "i18n/locales" in file_path:
            continue

        # Direct match first (git diff paths are web/src/...)
        cov_data = line_coverage.get(file_path)
        if cov_data is None:
            # Fallback: try suffix match
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
            diff_stats[file_path] = {"total": total_changed, "covered": covered_changed}
            # Derive directory under src/
            src_idx = file_path.find("src/")
            if src_idx >= 0:
                rel = file_path[src_idx:]
                parts = rel.split("/")
                if len(parts) >= 2:
                    top_dir = "/".join(parts[:2])
                else:
                    top_dir = rel
            else:
                top_dir = file_path
            dir_diff_stats[top_dir]["total"] += total_changed
            dir_diff_stats[top_dir]["covered"] += covered_changed

    if not diff_stats:
        tier2_skipped = True
        print(f"\n{CYAN}ℹ Tier 2 (Diff Coverage) SKIPPED — no changed frontend lines with coverage data{RESET}")
    else:
        print()
        print(f"{BOLD}╔══════════════════════════════════════════════════════════════════╗{RESET}")
        print(f"{BOLD}║           Frontend Coverage Gate — Tier 2: Diff Coverage       ║{RESET}")
        print(f"{BOLD}║  Rule: changed lines coverage >= {DIFF_THRESHOLD}%                        ║{RESET}")
        print(f"{BOLD}╚══════════════════════════════════════════════════════════════════╝{RESET}")
        print()

        print(f"{BOLD}{'Directory':<25} {'Covered':>8} {'Total':>8} {'Diff%':>8}  {'Status':<8}{RESET}")
        print(f"{'─'*25} {'─'*8} {'─'*8} {'─'*8}  {'─'*8}")

        for dir_name in sorted(dir_diff_stats.keys()):
            stats = dir_diff_stats[dir_name]
            total = stats["total"]
            covered = stats["covered"]
            pct = (covered / total * 100) if total > 0 else 100.0
            passed = pct >= DIFF_THRESHOLD
            if not passed:
                tier2_pass = False
            print(f"{dir_name:<25} {covered:>8} {total:>8} {pct:>7.1f}%  {pass_fail(passed)}")

        total_all = sum(s["total"] for s in dir_diff_stats.values())
        covered_all = sum(s["covered"] for s in dir_diff_stats.values())
        overall_pct = (covered_all / total_all * 100) if total_all > 0 else 100.0
        overall_pass = overall_pct >= DIFF_THRESHOLD
        if not overall_pass:
            tier2_pass = False

        print(f"{'─'*25} {'─'*8} {'─'*8} {'─'*8}  {'─'*8}")
        print(f"{BOLD}{'TOTAL':<25} {covered_all:>8} {total_all:>8} {overall_pct:>7.1f}%  {pass_fail(overall_pass)}{RESET}")

        # Show uncovered files
        uncovered_files = []
        for file_path, stats in sorted(diff_stats.items()):
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
    print(f"{YELLOW}{BOLD}⚠ Frontend coverage gate — no checks could run{RESET}")
    sys.exit(0)

all_pass = all(results)

if all_pass:
    print(f"{GREEN}{BOLD}═════════════════════════════════════════════════════════════════════{RESET}")
    print(f"{GREEN}{BOLD}✓ Frontend coverage gate PASSED{RESET}")
    if tier1_skipped:
        print(f"{GREEN}  (Tier 1 skipped, Tier 2 only){RESET}")
    if tier2_skipped:
        print(f"{GREEN}  (Tier 2 skipped, Tier 1 only){RESET}")
    print(f"{GREEN}{BOLD}═════════════════════════════════════════════════════════════════════{RESET}")
    sys.exit(0)
else:
    print(f"{RED}{BOLD}═════════════════════════════════════════════════════════════════════{RESET}")
    print(f"{RED}{BOLD}✗ Frontend coverage gate FAILED{RESET}")
    if not tier1_skipped and not tier1_pass:
        print(f"  - Tier 1 (Project): directory coverage below baseline floor")
    if not tier2_skipped and not tier2_pass:
        print(f"  - Tier 2 (Diff): changed lines coverage below {DIFF_THRESHOLD}%")
    print(f"{RED}{BOLD}═════════════════════════════════════════════════════════════════════{RESET}")
    print(f"\n{YELLOW}Tips:{RESET}")
    print("  - Add tests to improve coverage for failing checks")
    print("  - To skip test run: ./scripts/check-frontend-coverage.sh --skip-test")
    sys.exit(1)
PYTHON_SCRIPT
