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
BASELINE_DIR="$ROOT_DIR/.clawbench-ci/baseline"

SKIP_TEST=false
# `--shard=i/n` runs only this slice and writes its coverage under a per-shard
# directory; `--merge-shards=a,b,c` then folds those into the normal report. CI
# uses both (one job per shard, plus a final gate job); locally you pass neither
# and get a single full run.
SHARD=""
MERGE_SHARDS=""

for arg in "$@"; do
  case "$arg" in
    --skip-test) SKIP_TEST=true ;;
    --shard=*) SHARD="${arg#--shard=}" ;;
    --merge-shards=*) MERGE_SHARDS="${arg#--merge-shards=}" ;;
    --help|-h)
      echo "Usage: $0 [--skip-test] [--shard=i/n] [--merge-shards=dir,dir,...] [--help]"
      echo ""
      echo "  (no shard args)   run the whole suite, write coverage/ and gate on it"
      echo "  --shard=i/n       run slice i of n, write coverage-shards/<i>"
      echo "  --merge-shards=…  merge the listed shard dirs into coverage/, then gate"
      exit 0 ;;
  esac
done

cd "$ROOT_DIR"

# Where this invocation writes coverage. A shard run writes to its own dir so
# parallel jobs cannot clobber each other; everything else uses coverage/.
if [ -n "$SHARD" ]; then
  COVERAGE_DIR="$ROOT_DIR/coverage-shards/${SHARD%%/*}"
else
  COVERAGE_DIR="$ROOT_DIR/coverage"
fi
VITEST_RESULT_JSON="$COVERAGE_DIR/vitest-results.json"

# Step 1: Run tests with coverage (if not skipped)
if [ "$SKIP_TEST" = false ]; then
  echo "==> Running frontend tests with coverage..."
fi

result=0
if [ "$SKIP_TEST" = false ]; then
  rm -f "$VITEST_RESULT_JSON"
  mkdir -p "$COVERAGE_DIR"
  shard_args=()
  if [ -n "$SHARD" ]; then shard_args+=("--shard=$SHARD"); fi
  "$SCRIPT_DIR/vitest-run.sh" --coverage \
    --coverage.reportsDirectory="$COVERAGE_DIR" \
    --reporter=default --reporter=json --outputFile="$VITEST_RESULT_JSON" \
    ${shard_args[@]+"${shard_args[@]}"} || result=$?
  # Exit code 124 = vitest-run.sh watchdog timeout. If coverage data was
  # written before the hang (V8 provider writes during test execution),
  # we can still proceed. This handles the vitest 4 pool cleanup hang bug
  # (vitest-dev/vitest#8766) where all tests pass but the process can't exit.
  #
  # Exit code 1 is ambiguous, and this is where a real failure used to hide:
  # it can mean "some tests failed" OR "workers were killed during pool
  # cleanup". The old check only asked "does coverage data exist?", which is
  # TRUE in both cases (coverage is written per test, before the pool closes).
  # CI run 42519003966 waved through 17 failed files / 20 failed tests that way
  # — 14 desktop specs could not resolve `electron` (no desktop deps installed)
  # and 3 others failed for real — and still reported a green gate.
  #
  # So the question is now answered from the test RESULTS, not from the
  # presence of a file. The JSON reporter is enabled only for this purpose;
  # it records the counts even when workers are killed.
  if [ $result -ne 0 ] && [ $result -ne 124 ] && [ $result -ne 1 ]; then
    echo "ERROR: Frontend tests failed (exit code $result). Fix test failures before checking coverage."
    exit 1
  fi
  if [ $result -eq 124 ]; then
    echo "WARNING: vitest timed out (pool cleanup hang), checking if coverage data was written..."
  fi
  if [ $result -eq 1 ] || [ $result -eq 124 ]; then
    # $COVERAGE_DIR, not a hardcoded coverage/: a shard run writes to
    # coverage-shards/<i>/, and checking the wrong directory reported "no
    # coverage data was written" even though the shard had just produced a
    # 4.3 MB report that uploaded fine (CI job 128970870673).
    if [ ! -f "$COVERAGE_DIR/coverage-summary.json" ]; then
      echo "ERROR: vitest exited with code $result and no coverage data was written to $COVERAGE_DIR."
      exit 1
    fi
    # Distinguish "clean run whose pool failed to close" from "tests failed".
    #
    # Both fields are needed. `numFailedTests` alone is not enough: a file that
    # fails to IMPORT (e.g. desktop specs with no `electron` installed) never
    # produces a test result at all, so it reports numFailedTests=0 while
    # `success` is false and numTotalTests is 0. Checking only the failure count
    # would call that a clean run — which is exactly the shape of the CI run
    # this guard was written for (14 files: "0 test", import error).
    verdict=$(python3 - "$VITEST_RESULT_JSON" <<'PY'
import json, sys, os
p = sys.argv[1]
if not os.path.exists(p):
    print("missing 0 0")   # no report ⇒ cannot vouch for the run
    raise SystemExit
try:
    d = json.load(open(p))
except Exception:
    print("unreadable 0 0")
    raise SystemExit
failed = int(d.get("numFailedTests") or 0)
total = int(d.get("numTotalTests") or 0)
ok = bool(d.get("success"))
# success=false with zero failures means collection/import errors.
print(("ok" if ok and failed == 0 else "bad"), failed, total)
PY
)
    read -r verdict_state verdict_failed verdict_total <<<"$verdict"
    if [ "$verdict_state" = "ok" ]; then
      echo "WARNING: vitest exited with code $result but every test passed (pool cleanup worker kill)"
    else
      case "$verdict_state" in
        missing) echo "ERROR: vitest exited with code $result and wrote no JSON report," ;;
        unreadable) echo "ERROR: vitest exited with code $result and its JSON report is unreadable," ;;
        *) echo "WARNING: $verdict_failed of $verdict_total test(s) failed (exit code $result)." ;;
      esac
      if [ "$verdict_state" = "bad" ] && [ "${verdict_failed:-0}" != "0" ]; then
        # Failures can be REAL or an artefact of a loaded machine: every vitest
        # test has a 10 s budget, and under CPU contention ordinary component
        # mounts blow through it (measured: 24 "failures" at 10-41 s each, all of
        # which passed when re-run alone). Re-running just the failed tests is
        # the cheap way to tell the two apart — it costs seconds, and a genuine
        # failure fails again.
        #
        # Only the tests that failed are re-run, NOT the suite: re-running
        # everything would double the job's wall clock on a machine that is
        # already slow.
        echo "==> Re-running the failed tests to rule out machine contention..."
        mapfile -t failed_files < <(python3 - "$VITEST_RESULT_JSON" <<'PY'
import json, sys, os
p = sys.argv[1]
try:
    d = json.load(open(p))
except Exception:
    raise SystemExit
seen = set()
for tr in d.get("testResults", []):
    name = tr.get("name") or ""
    if any(a.get("status") == "failed" for a in tr.get("assertionResults", [])):
        if name and name not in seen:
            seen.add(name)
            print(name)
PY
)
        if [ ${#failed_files[@]} -gt 0 ]; then
          retry_rc=0
          "$SCRIPT_DIR/vitest-run.sh" "${failed_files[@]}" || retry_rc=$?
          if [ "$retry_rc" -eq 0 ]; then
            echo "WARNING: every failed test passed on re-run — the failures were machine contention, not defects"
            # Keep the first run's coverage: it is complete and the re-run did
            # not collect any.
            echo "==> Continuing with the coverage gate from the first run."
          else
            echo "ERROR: the failures REPRODUCE on a quiet re-run. Fix them before the coverage gate."
            exit 1
          fi
        else
          echo "ERROR: failures were reported but no failing test file could be identified."
          exit 1
        fi
      else
        echo "       so test failures cannot be ruled out. Treating as a failure."
        echo "       (Coverage data may still exist: it is written per test, before"
        echo "        the pool closes, which is why 'coverage exists' proves nothing.)"
        exit 1
      fi
    fi
  fi
else
  # --skip-test: verify the report exists — but only when nothing is going to
  # produce it later in this same run. With --merge-shards the report is
  # created by the merge step BELOW, so checking here failed every merge job
  # with "coverage data not found" (CI job 129071809219) even though all three
  # shards had uploaded fine.
  if [ -z "$MERGE_SHARDS" ]; then
    if [ ! -f "$COVERAGE_DIR/coverage-summary.json" ] || [ ! -f "$COVERAGE_DIR/coverage-final.json" ]; then
      echo "ERROR: coverage data not found in $COVERAGE_DIR. Run without --skip-test first."
      exit 1
    fi
  fi
fi

# ── Sharded runs ────────────────────────────────────────────────────
# A `--shard=i/n` invocation stops here: its job is to produce that slice's
# coverage and report its own test results, NOT to gate (one shard sees only
# part of the suite, so both coverage tiers would be measured on a partial
# picture — Tier 2 in particular would call untouched changed files "0%").
if [ -n "$SHARD" ]; then
  echo ""
  echo "==> Shard $SHARD finished; coverage written to $COVERAGE_DIR"
  echo "    The gate runs in the merge job, over all shards combined."
  exit 0
fi

# Fold per-shard coverage into coverage/ so the gate below reads one report.
if [ -n "$MERGE_SHARDS" ]; then
  echo "==> Merging shard coverage..."
  # shellcheck disable=SC2207  # splitting the comma list is the point
  IFS=',' read -r -a shard_dirs <<<"$MERGE_SHARDS"
  node "$SCRIPT_DIR/merge-coverage.mjs" "$ROOT_DIR/coverage" "${shard_dirs[@]}"
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
    echo "ℹ Using baseline from .clawbench-ci/baseline/"
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

# ── Exempt files ─────────────────────────────────────────────────
# Files exempt from coverage gates because they contain code that is
# fundamentally untestable without complex integration setup.
FE_EXEMPT_FILES = {
    "web/src/composables/useUpgrade.ts",          # upgrade: real API calls, WebSocket, complex async orchestration
    "web/src/composables/useSessionIdentity.ts",   # session: reactive state sync, complex composable with many deps
    "web/src/composables/useAgents.ts",            # agents: ACP state cache, model list sync, complex composable
}
EXCLUDED = {"src/i18n"}  # i18n locale dict aggregation directory

# Individual files to exclude from Tier 1 coverage calculation.
# These have intrinsically low coverage (DOM/IPC deps, CLI subprocess, etc.)
# and drag down the per-directory weighted average below the baseline floor.
TIER1_EXCLUDED_FILES = {
    "src/utils/globals.ts",  # Pure re-exports of third-party libraries (marked, hljs, katex, DOMPurify, mermaid)
    "src/utils/diff.ts",     # Diff rendering: depends on hljs + DOM rendering, needs browser environment
    "src/utils/api.ts",      # HTTP API client: depends on fetch + server, needs integration test
    "src/utils/app.ts",      # App store bootstrap: global state init, needs full app context
    "src/stores/app.ts",     # Reactive store: 1000+ lines with heavy deps, integration-level testing only
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
    """Extract the src/... portion of a FRONTEND path, else None.

    Must be anchored to the frontend root (`web/src/`). Matching the first
    `/src/` anywhere in the path looks equivalent but is not: it also matches
    `desktop/src/…`, `android/app/src/…` and `web/vendor-build/excalidraw/src/…`.
    A desktop path like /repo/desktop/src/main/install.ts then reduces to
    `src/main/install.ts`, whose first two segments give the Tier 1 bucket
    `src/main` — a directory that does not exist in the frontend at all. Those
    files got averaged into that phantom bucket and dragged it below the floor,
    failing Tier 1 locally while CI passed (CI has no desktop/node_modules, so
    it collects only the dependency-free desktop tests).

    Non-frontend paths are not frontend coverage and must be skipped outright.
    """
    idx = path.find("/web/src/")
    if idx >= 0:
        return path[idx + 5:]  # e.g., src/components/Foo.vue
    if path.startswith("web/src/"):
        return path[4:]
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
        # Only frontend sources participate. extract_web_src_path and
        # extract_src_path are both anchored to web/src/, so a path that fails
        # the first also fails the second — there is deliberately no
        # "src/…"-style fallback here, because that is what used to admit
        # desktop/android paths and fabricate a web/src/… path that git diff
        # could never match anyway.
        norm_path = extract_web_src_path(file_path)
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
    exempt_stats = {}
    # Changed files with no coverage entry at all — reported, never gated.
    no_coverage_files = []
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
        # Exclude files with DOM/rendering dependencies that require full browser env
        TIER2_EXCLUDED_SUFFIXES = (
            "src/utils/exportHtml.ts",  # DOM cloning + Mermaid + CSS serialization + batch-base64 API
            "src/utils/download.ts",  # Blob/FileReader/Android bridge, jsdom can't fully exercise
            "src/composables/useCodeBlockHeader.ts",  # DOM manipulation (wrap/annotate code blocks), hard to test in jsdom
            "src/components/file/FileViewer.vue",  # PDF/image/video/audio previews, heavy component mocking
            "src/components/file/FileHeader.vue",  # Menu with many action handlers, needs full app context
            "src/components/media/PdfPreview.vue",  # PDF.js worker integration, needs real browser
            "src/stores/app.ts",  # Reactive store: 1000+ lines with heavy deps, integration-level testing only
        )
        if any(file_path.endswith(s) for s in TIER2_EXCLUDED_SUFFIXES):
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
            # No coverage entry at all. This is NOT the same as "0% covered": the
            # file never entered coverage-final.json because no test imports it
            # (e.g. App.vue, whose only tests read it as source text). Istanbul
            # cannot report a line it never instrumented, so these changed lines
            # are silently unenforced — record them so the report can say so
            # instead of implying the change was measured and passed.
            no_coverage_files.append(file_path)
            continue

        # Check exemption
        is_exempt = False
        for ef in FE_EXEMPT_FILES:
            if file_path.endswith(ef) or file_path == ef:
                is_exempt = True
                break

        total_changed = 0
        covered_changed = 0
        for ln in lines:
            if ln in cov_data:
                total_changed += 1
                if cov_data[ln]:
                    covered_changed += 1

        if is_exempt:
            if total_changed > 0:
                exempt_stats[file_path] = {"total": total_changed, "covered": covered_changed}
            continue

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

    # Files with no coverage entry are reported in both branches — the whole
    # point is that the gate cannot see them.
    def print_no_coverage_files():
        if not no_coverage_files:
            return
        print(f"\n{YELLOW}{BOLD}Changed files with NO coverage data (not gated — no test imports them):{RESET}")
        for file_path in sorted(no_coverage_files):
            print(f"  {YELLOW}{file_path}{RESET}")
        print(f"  {YELLOW}Their changed lines are NOT measured by Tier 2. Review them by hand.{RESET}")

    if not diff_stats:
        tier2_skipped = True
        print(f"\n{CYAN}ℹ Tier 2 (Diff Coverage) SKIPPED — no changed frontend lines with coverage data{RESET}")
        print_no_coverage_files()
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

        # Show exempt files
        if exempt_stats:
            print(f"\n{YELLOW}{BOLD}Exempt files (not counted toward gate):{RESET}")
            for file_path, stats in sorted(exempt_stats.items()):
                pct = (stats["covered"] / stats["total"] * 100) if stats["total"] > 0 else 0
                print(f"  {YELLOW}{file_path:<50} {stats['covered']}/{stats['total']} ({pct:.1f}%){RESET}")

        print_no_coverage_files()

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
    # Exit code 2: Tier 1 only failed (baseline drift, non-blocking for CI).
    # Exit code 1: Tier 2 failed (diff coverage below threshold, blocking).
    tier2_failed = not tier2_skipped and not tier2_pass
    sys.exit(1 if tier2_failed else 2)
PYTHON_SCRIPT
