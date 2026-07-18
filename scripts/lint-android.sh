#!/usr/bin/env bash
# lint-android.sh — Check that Android production code uses AppLog, not raw android.util.Log
#
# Enforces the project rule: all Android Java/Kotlin code MUST use AppLog
# instead of android.util.Log, ensuring logs are relayed to the server.
#
# Exceptions (allowed to use raw Log):
#   - AppLog.java itself (it delegates to android.util.Log internally)
#   - Test files (*Test.java, *_test.kt)
#
# Usage:
#   ./scripts/lint-android.sh              # Run check
#   ./scripts/lint-android.sh --help       # Show help
#
# Exit code: 0 = pass, 1 = violations found

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ANDROID_SRC="$ROOT_DIR/android/app/src/main/java"

for arg in "$@"; do
    case "$arg" in
        --help|-h)
            echo "Usage: $0 [--help]"
            echo ""
            echo "  Checks that Android production code uses AppLog, not raw android.util.Log"
            echo "  Exceptions: AppLog.java itself and test files"
            exit 0
            ;;
        *)
            echo "Unknown argument: $arg"
            exit 1
            ;;
    esac
done

if [ ! -d "$ANDROID_SRC" ]; then
    echo "⚠️  Android source directory not found: $ANDROID_SRC"
    echo "   Skipping Android lint check"
    exit 0
fi

VIOLATIONS=()

# Find all Java/Kotlin production files (excluding AppLog.java and test files)
while IFS= read -r -d '' file; do
    RELATIVE="${file#$ANDROID_SRC/}"

    # Skip AppLog.java itself
    if [[ "$RELATIVE" == *"AppLog.java" ]]; then
        continue
    fi

    # Check for raw android.util.Log calls: Log.d(, Log.e(, Log.i(, Log.v(, Log.w(
    # Match patterns like: Log.d( Log.e( Log.i( Log.v( Log.w( Log.wtf(
    # But NOT: AppLog.d( AppLog.e( etc.
    MATCHES=$(grep -nE '(?<!App)(?<!\w)Log\.[deivw]' "$file" 2>/dev/null || true)
    if [ -n "$MATCHES" ]; then
        while IFS= read -r line; do
            # Filter out import statements — those are handled separately
            if [[ "$line" == *"import android.util.Log"* ]]; then
                # Unused import violation
                VIOLATIONS+=("$RELATIVE:$(echo "$line" | cut -d: -f1): unused 'import android.util.Log' — remove it")
            else
                # Raw Log.d/e/i/v/w call violation
                VIOLATIONS+=("$RELATIVE:$(echo "$line" | cut -d: -f1): use AppLog instead of android.util.Log")
            fi
        done <<< "$MATCHES"
    fi

    # Check for unused import android.util.Log (files that import but don't use raw Log.*)
    HAS_IMPORT=$(grep -c "import android.util.Log;" "$file" 2>/dev/null || echo "0")
    if [ "$HAS_IMPORT" -gt 0 ]; then
        # Check if there are any raw Log.* calls (excluding AppLog.*)
        RAW_CALLS=$(grep -cE '(?<!App)(?<!\w)Log\.[deivw]' "$file" 2>/dev/null || echo "0")
        if [ "$RAW_CALLS" -eq 0 ]; then
            # Import exists but no raw calls — unused import
            LINE_NUM=$(grep -n "import android.util.Log;" "$file" | head -1 | cut -d: -f1)
            # Only add if not already reported
            ALREADY_REPORTED=false
            for v in "${VIOLATIONS[@]}"; do
                if [[ "$v" == *"$RELATIVE:$LINE_NUM:"* ]]; then
                    ALREADY_REPORTED=true
                    break
                fi
            done
            if [ "$ALREADY_REPORTED" = false ]; then
                VIOLATIONS+=("$RELATIVE:$LINE_NUM: unused 'import android.util.Log' — remove it")
            fi
        fi
    fi
done < <(find "$ANDROID_SRC" -name "*.java" -o -name "*.kt" -print0)

if [ ${#VIOLATIONS[@]} -eq 0 ]; then
    echo "✅ Android lint check passed — all production code uses AppLog"
    exit 0
else
    echo "❌ Android lint check failed — found ${#VIOLATIONS[@]} violation(s):"
    echo ""
    echo "  Rule: All Android production code MUST use AppLog instead of"
    echo "        android.util.Log, so logs are relayed to the server."
    echo "        Only AppLog.java itself may use raw android.util.Log."
    echo ""
    for v in "${VIOLATIONS[@]}"; do
        echo "  $v"
    done
    echo ""
    echo "  Fix: Replace Log.d(TAG, msg) with AppLog.d(TAG, msg) etc."
    echo "       Remove unused 'import android.util.Log;' statements."
    exit 1
fi
