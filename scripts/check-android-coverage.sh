#!/usr/bin/env bash
# check-android-coverage.sh — Run Android tests with coverage and enforce two-tier gate.
#
# Tier 1 — Project Gate: current_coverage% >= adjusted_baseline% - 1.5% tolerance
# Tier 2 — Diff Coverage: changed lines coverage >= 80% (strict)
#
# Baseline: auto-downloaded from main branch CI artifact
#   Falls back to: .clawbench-ci/baseline/ → gh CLI → skip Tier 1
#
# Usage:
#   ./scripts/check-android-coverage.sh              # run tests + check
#   ./scripts/check-android-coverage.sh --skip-test   # skip running tests, use existing report
#
# Exit code: 0 = pass, 1 = fail

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ANDROID_DIR="$ROOT_DIR/android"
JACOCO_XML="$ANDROID_DIR/app/build/reports/jacoco/jacocoTestReport/jacocoTestReport.xml"
BASELINE_DIR="$ROOT_DIR/.clawbench-ci/baseline"

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
  echo "==> Running Android tests with coverage..."
  cd "$ANDROID_DIR"
  JAVA_HOME=${JAVA_HOME:-/usr/lib/jvm/java-17-openjdk-amd64} ./gradlew jacocoTestReport 2>&1
  cd "$ROOT_DIR"
  echo ""
else
  # --skip-test: just verify coverage data exists
  if [ ! -f "$JACOCO_XML" ]; then
    echo "ERROR: jacocoTestReport.xml not found. Run without --skip-test first."
    exit 1
  fi
fi

if [ ! -f "$JACOCO_XML" ]; then
  echo "ERROR: jacocoTestReport.xml not found. Run without --skip-test first."
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

# Step 3: Try to get baseline coverage report
BASELINE_XML=""
# 3a. CI download step (already placed by workflow)
for _bf in "$BASELINE_DIR/jacocoTestReport.xml" \
           "$BASELINE_DIR/coverage/jacocoTestReport.xml" \
           "$BASELINE_DIR/android/app/build/reports/jacoco/jacocoTestReport/jacocoTestReport.xml"; do
  if [ -f "$_bf" ]; then
    BASELINE_XML="$_bf"
    echo "ℹ Using baseline from .clawbench-ci/baseline/"
    break
  fi
done
if [ -z "$BASELINE_XML" ] && command -v gh &>/dev/null; then
  echo "ℹ Attempting baseline download via gh CLI..."
  mkdir -p "$BASELINE_DIR"
  if gh run download --name main-android-coverage --dir "$BASELINE_DIR" 2>/dev/null; then
    for _bf in "$BASELINE_DIR/jacocoTestReport.xml" \
           "$BASELINE_DIR/coverage/jacocoTestReport.xml" \
           "$BASELINE_DIR/android/app/build/reports/jacoco/jacocoTestReport/jacocoTestReport.xml"; do
      if [ -f "$_bf" ]; then
        BASELINE_XML="$_bf"
        echo "ℹ Baseline downloaded via gh CLI"
        break
      fi
    done
  fi
fi
# 3c. If still no baseline, Tier 1 will be skipped

# Step 4: Run Python gate check
python3 - "$JACOCO_XML" "$BASELINE_XML" "$MERGE_BASE" << 'PYTHON_SCRIPT'
import sys, re, subprocess
import xml.etree.ElementTree as ET
from collections import defaultdict

coverage_xml = sys.argv[1]
baseline_xml = sys.argv[2] if len(sys.argv) > 2 else ""
merge_base = sys.argv[3] if len(sys.argv) > 3 else ""

TIER1_TOLERANCE = 1.5
DIFF_THRESHOLD = 80.0

# ── Colors ──────────────────────────────────────────────────────
BOLD = "\033[1m"
RED = "\033[0;31m"
GREEN = "\033[0;32m"
YELLOW = "\033[0;33m"
CYAN = "\033[0;36m"
RESET = "\033[0m"

def pass_fail(passed):
    return f"{GREEN}PASS{RESET}" if passed else f"{RED}FAIL{RESET}"

# ── Helper: convert JaCoCo package+sourcefile to git diff path ──
def jacoco_to_git_path(pkg_name, sourcefile_name):
    """Convert JaCoCo 'com/clawbench/app/PortForwardService.java' to
    'android/app/src/main/java/com/clawbench/app/PortForwardService.java'"""
    if pkg_name:
        return f"android/app/src/main/java/{pkg_name}/{sourcefile_name}"
    return f"android/app/src/main/java/{sourcefile_name}"

def git_to_jacoco_path(git_path):
    """Convert git diff path to JaCoCo package+sourcefile key."""
    # android/app/src/main/java/com/clawbench/app/PortForwardService.java
    # → com/clawbench/app/PortForwardService.java
    idx = git_path.find("src/main/java/")
    if idx >= 0:
        return git_path[idx + len("src/main/java/"):]
    return git_path

# ── Parse current JaCoCo XML ──────────────────────────────────
tree = ET.parse(coverage_xml)
root = tree.getroot()

# Tier 1: per-class LINE coverage (using sourcefile/line for consistency with baseline)
# We use sourcefile/line ci/mi aggregation (same as baseline) so metrics are comparable.
# Inner classes (e.g., Foo$1) are aggregated with their outer class (Foo).
current_class_stmts = defaultdict(lambda: {"covered": 0, "total": 0})
for pkg in root.findall(".//package"):
    pkg_name = pkg.get("name", "")
    for sf in pkg.findall("sourcefile"):
        sf_name = sf.get("name", "")
        # Derive class key: package + sourcefile name (strip .java), aggregate inner classes
        base_name = sf_name[:-5] if sf_name.endswith(".java") else sf_name  # strip .java
        if pkg_name:
            class_key = f"{pkg_name.replace('/', '.')}.{base_name}"
        else:
            class_key = base_name
        for line_elem in sf.findall("line"):
            ci = int(line_elem.get("ci", "0"))
            mi = int(line_elem.get("mi", "0"))
            total = ci + mi
            if total > 0:
                current_class_stmts[class_key]["total"] += total
                if ci > 0:
                    current_class_stmts[class_key]["covered"] += ci

current = {}
for cls_key, data in current_class_stmts.items():
    if data["total"] > 0:
        current[cls_key] = (data["covered"] / data["total"]) * 100

# ══════════════════════════════════════════════════════════════════
# TIER 1: Project Gate
# ══════════════════════════════════════════════════════════════════

tier1_pass = True
tier1_skipped = False

if not baseline_xml:
    tier1_skipped = True
    print(f"{CYAN}ℹ Tier 1 (Project) SKIPPED — no baseline available{RESET}")
    print(f"  (Run on CI or install gh CLI for automatic baseline download)")
else:
    # Parse baseline
    baseline_tree = ET.parse(baseline_xml)
    baseline_root = baseline_tree.getroot()

    # Adjust baseline: exclude deleted lines (removed_code_behavior=adjust_base)
    deleted_lines = defaultdict(set)
    if merge_base:
        diff_result = subprocess.run(
            ["git", "diff", "--unified=0", merge_base, "--", "android/app/src/main/java/"],
            capture_output=True, text=True
        )
        current_diff_file = None
        for line in diff_result.stdout.split("\n"):
            if line.startswith("--- a/"):
                current_diff_file = line[6:].strip()
            elif line.startswith("@@") and current_diff_file:
                m = re.search(r'-(\d+)(?:,(\d+))?\s+\+', line)
                if m:
                    start = int(m.group(1))
                    count = int(m.group(2)) if m.group(2) else 1
                    for ln in range(start, start + count):
                        deleted_lines[current_diff_file].add(ln)

    # Parse baseline with deleted-line exclusion
    adjusted_class_stmts = defaultdict(lambda: {"covered": 0, "total": 0})
    for pkg in baseline_root.findall(".//package"):
        pkg_name = pkg.get("name", "")
        for sf in pkg.findall("sourcefile"):
            sf_name = sf.get("name", "")
            git_path = jacoco_to_git_path(pkg_name, sf_name)
            for line_elem in sf.findall("line"):
                ln = int(line_elem.get("nr", "0"))
                ci = int(line_elem.get("ci", "0"))
                mi = int(line_elem.get("mi", "0"))
                # Check if this line was deleted
                if git_path in deleted_lines and ln in deleted_lines[git_path]:
                    continue
                total = ci + mi
                if total > 0:
                    # Derive class key from package + sourcefile (strip .java), same as current
                    base_name = sf_name[:-5] if sf_name.endswith(".java") else sf_name
                    if pkg_name:
                        class_key = f"{pkg_name.replace('/', '.')}.{base_name}"
                    else:
                        class_key = base_name
                    adjusted_class_stmts[class_key]["total"] += total
                    if ci > 0:
                        adjusted_class_stmts[class_key]["covered"] += ci

    adjusted_baseline_pct = {}
    for cls_key, data in adjusted_class_stmts.items():
        if data["total"] > 0:
            adjusted_baseline_pct[cls_key] = (data["covered"] / data["total"]) * 100

    # Print Tier 1
    print()
    print(f"{BOLD}╔══════════════════════════════════════════════════════════════════╗{RESET}")
    print(f"{BOLD}║           Android Coverage Gate — Tier 1: Project              ║{RESET}")
    print(f"{BOLD}║  Rule: coverage >= baseline% - {TIER1_TOLERANCE}%                           ║{RESET}")
    print(f"{BOLD}╚══════════════════════════════════════════════════════════════════╝{RESET}")
    print()

    print(f"{BOLD}{'Class':<50} {'Base%':>8} {'Curr%':>8} {'Floor':>8}  {'Status':<8}{RESET}")
    print(f"{'─'*50} {'─'*8} {'─'*8} {'─'*8}  {'─'*8}")

    all_classes = sorted(set(list(adjusted_baseline_pct.keys()) + list(current.keys())))

    # Tier 1 exempt classes: classes whose code fundamentally requires Android framework
    # for testing (same rationale as Tier 2 exempt_files, but keyed by class name).
    tier1_exempt_classes = {
        "com.clawbench.app.BackgroundService",  # startForegroundCompat, ensureConnection need Android framework + JSch
        "com.clawbench.app.MainActivity",  # Needs Android framework for most methods; Share Out logic extracted to SharedCacheUtils
    }

    for cls in all_classes:
        base_pct = adjusted_baseline_pct.get(cls)
        curr_pct = current.get(cls)

        if curr_pct is None and base_pct is not None:
            print(f"{cls:<50} {base_pct:>7.1f}% {'N/A':>8} {'N/A':>8}  {YELLOW}REMOVED{RESET}")
            continue

        if base_pct is None:
            base_pct = 0.0

        floor = max(base_pct - TIER1_TOLERANCE, 0.0)
        passed = curr_pct >= floor
        is_tier1_exempt = cls in tier1_exempt_classes
        if not passed and not is_tier1_exempt:
            tier1_pass = False

        drop_note = f"  {YELLOW}↘{RESET}" if curr_pct < base_pct - TIER1_TOLERANCE else ""
        exempt_note = f"  {YELLOW}EXEMPT{RESET}" if is_tier1_exempt and not passed else ""
        print(f"{cls:<50} {base_pct:>7.1f}% {curr_pct:>7.1f}% {floor:>7.1f}%  {pass_fail(passed)}{drop_note}{exempt_note}")

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
        ["git", "diff", "--unified=0", merge_base, "--", "android/app/src/main/java/"],
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
            m = re.search(r'\+(\d+)(?:,(\d+))?', line)
            if m:
                start = int(m.group(1))
                count = int(m.group(2)) if m.group(2) else 1
                if current_file:
                    for ln in range(start, start + count):
                        changed_lines[current_file].add(ln)

    # Parse JaCoCo XML for per-line coverage
    line_coverage = defaultdict(dict)
    for pkg in root.findall(".//package"):
        pkg_name = pkg.get("name", "")
        for sf in pkg.findall("sourcefile"):
            sf_name = sf.get("name", "")
            git_path = jacoco_to_git_path(pkg_name, sf_name)
            for line_elem in sf.findall("line"):
                ln = int(line_elem.get("nr", "0"))
                ci = int(line_elem.get("ci", "0"))
                covered = ci > 0
                # OR logic: if ANY statement on this line is covered, the line is covered
                line_coverage[git_path][ln] = line_coverage[git_path].get(ln, False) or covered

    # Cross-reference
    diff_stats = {}
    class_diff_stats = defaultdict(lambda: {"total": 0, "covered": 0})

    # Files exempt from Tier 2 because they contain code that is fundamentally
    # untestable in JVM unit tests (Android Service lifecycle methods, JSch SSH
    # session management, etc.). Exempt status is per-file; other changed files
    # in the same class are still checked normally.
    exempt_files = {
        "android/app/src/main/java/com/clawbench/app/BackgroundService.java",  # onStartCommand, ensureConnection need Android framework + JSch
        "android/app/src/main/java/com/clawbench/app/BrowserActivity.java",    # shouldInterceptRequest needs WebView + HttpURLConnection; onReceivedError needs WebViewClient lifecycle
        "android/app/src/main/java/com/clawbench/app/MainActivity.java",       # launchFileChooserWithCamera needs PackageManager + FileProvider; onShowFileChooser needs WebView ChromeClient lifecycle
        "android/app/src/main/java/com/clawbench/app/PushService.java",        # Service lifecycle methods (onCreate/onStartCommand/startForeground) need Android framework; static methods tested in PushServiceTest
    }

    for file_path, lines in sorted(changed_lines.items()):
        if not file_path.endswith(".java"):
            continue
        # Exclude test files
        if file_path.endswith("Test.java") or "test/" in file_path:
            continue

        # Check per-file exemption
        is_exempt = file_path in exempt_files

        # Convert git path to JaCoCo key for matching
        jacoco_key = git_to_jacoco_path(file_path)
        cov_data = line_coverage.get(jacoco_key)
        if cov_data is None:
            # Try suffix match
            for cov_path, cov_lines in line_coverage.items():
                if cov_path.endswith("/" + jacoco_key) or cov_path == jacoco_key:
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
            diff_stats[file_path] = {"total": total_changed, "covered": covered_changed, "exempt": is_exempt}
            # Derive class from path
            idx = file_path.find("src/main/java/")
            if idx >= 0:
                class_path = file_path[idx + len("src/main/java/"):]
                class_key = class_path.rsplit("/", 1)[0].replace("/", ".")
            else:
                class_key = file_path
            if not is_exempt:
                class_diff_stats[class_key]["total"] += total_changed
            class_diff_stats[class_key]["covered"] += covered_changed

    if not diff_stats:
        tier2_skipped = True
        print(f"\n{CYAN}ℹ Tier 2 (Diff Coverage) SKIPPED — no changed Android lines with coverage data{RESET}")
    else:
        print()
        print(f"{BOLD}╔══════════════════════════════════════════════════════════════════╗{RESET}")
        print(f"{BOLD}║           Android Coverage Gate — Tier 2: Diff Coverage        ║{RESET}")
        print(f"{BOLD}║  Rule: changed lines coverage >= {DIFF_THRESHOLD}%                        ║{RESET}")
        print(f"{BOLD}╚══════════════════════════════════════════════════════════════════╝{RESET}")
        print()

        print(f"{BOLD}{'Class':<50} {'Covered':>8} {'Total':>8} {'Diff%':>8}  {'Status':<8}{RESET}")
        print(f"{'─'*50} {'─'*8} {'─'*8} {'─'*8}  {'─'*8}")

        for cls in sorted(class_diff_stats.keys()):
            stats = class_diff_stats[cls]
            total = stats["total"]
            covered = stats["covered"]
            pct = (covered / total * 100) if total > 0 else 100.0
            passed = pct >= DIFF_THRESHOLD
            if not passed:
                tier2_pass = False
            print(f"{cls:<50} {covered:>8} {total:>8} {pct:>7.1f}%  {pass_fail(passed)}")

        total_all = sum(s["total"] for s in class_diff_stats.values())
        covered_all = sum(s["covered"] for s in class_diff_stats.values())
        overall_pct = (covered_all / total_all * 100) if total_all > 0 else 100.0
        overall_pass = overall_pct >= DIFF_THRESHOLD
        if not overall_pass:
            tier2_pass = False

        print(f"{'─'*50} {'─'*8} {'─'*8} {'─'*8}  {'─'*8}")
        print(f"{BOLD}{'TOTAL':<50} {covered_all:>8} {total_all:>8} {overall_pct:>7.1f}%  {pass_fail(overall_pass)}{RESET}")

        # Show uncovered files
        uncovered_files = []
        exempt_changed = {}
        for file_path, stats in sorted(diff_stats.items()):
            is_exempt = stats.get("exempt", False)
            if is_exempt:
                exempt_changed[file_path] = stats
                continue
            if stats["covered"] < stats["total"]:
                pct = (stats["covered"] / stats["total"] * 100) if stats["total"] > 0 else 0
                uncovered_files.append((file_path, stats["covered"], stats["total"], pct))

        # Show exempt files (informational, not gate-blocking)
        if exempt_changed:
            print(f"\n{CYAN}{BOLD}Exempt files (not counted toward gate):{RESET}")
            for file_path, stats in sorted(exempt_changed.items()):
                pct = (stats["covered"] / stats["total"] * 100) if stats["total"] > 0 else 0
                print(f"  {CYAN}{file_path:<60} {stats['covered']}/{stats['total']} ({pct:.1f}%){RESET}")

        if uncovered_files:
            print(f"\n{YELLOW}{BOLD}Uncovered changed files:{RESET}")
            for file_path, covered, total, pct in uncovered_files:
                print(f"  {RED}{file_path:<60} {covered}/{total} ({pct:.1f}%){RESET}")

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
    print(f"{YELLOW}{BOLD}⚠ Android coverage gate — no checks could run{RESET}")
    sys.exit(0)

all_pass = all(results)

if all_pass:
    print(f"{GREEN}{BOLD}═════════════════════════════════════════════════════════════════════{RESET}")
    print(f"{GREEN}{BOLD}✓ Android coverage gate PASSED{RESET}")
    if tier1_skipped:
        print(f"{GREEN}  (Tier 1 skipped, Tier 2 only){RESET}")
    if tier2_skipped:
        print(f"{GREEN}  (Tier 2 skipped, Tier 1 only){RESET}")
    print(f"{GREEN}{BOLD}═════════════════════════════════════════════════════════════════════{RESET}")
    sys.exit(0)
else:
    print(f"{RED}{BOLD}═════════════════════════════════════════════════════════════════════{RESET}")
    print(f"{RED}{BOLD}✗ Android coverage gate FAILED{RESET}")
    if not tier1_skipped and not tier1_pass:
        print(f"  - Tier 1 (Project): per-class coverage below baseline floor")
    if not tier2_skipped and not tier2_pass:
        print(f"  - Tier 2 (Diff): changed lines coverage below {DIFF_THRESHOLD}%")
    print(f"{RED}{BOLD}═════════════════════════════════════════════════════════════════════{RESET}")
    print(f"\n{YELLOW}Tips:{RESET}")
    print("  - Add tests to improve coverage for failing checks")
    print("  - To skip test run: ./scripts/check-android-coverage.sh --skip-test")
    sys.exit(1)
PYTHON_SCRIPT
