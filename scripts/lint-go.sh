#!/usr/bin/env bash
# lint-go.sh — 本地运行 golangci-lint 检查
#
# 自动检测/安装 golangci-lint，读取 .golangci-lint-version 锁定版本。
#
# 用法：
#   ./scripts/lint-go.sh              # 运行全量 lint
#   ./scripts/lint-go.sh --fix        # 自动修复可修复的问题
#   ./scripts/lint-go.sh --diff       # 仅检查暂存区变更
#
# 退出码：0 = 通过，1 = 有问题

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
VERSION_FILE="$ROOT_DIR/.golangci-lint-version"

cd "$ROOT_DIR"

# 读取锁定版本
LINT_VERSION=""
if [ -f "$VERSION_FILE" ]; then
    LINT_VERSION="$(cat "$VERSION_FILE" | tr -d '[:space:]')"
fi

# 解析参数
FIX_MODE=false
DIFF_MODE=false
EXTRA_ARGS=""

for arg in "$@"; do
    case "$arg" in
        --fix)
            FIX_MODE=true
            EXTRA_ARGS="$EXTRA_ARGS --fix"
            ;;
        --diff)
            DIFF_MODE=true
            ;;
        --help|-h)
            echo "用法: $0 [--fix] [--diff] [--help]"
            echo ""
            echo "  --fix    自动修复可修复的问题"
            echo "  --diff   仅检查暂存区变更（与 master/main 的差异）"
            echo "  --help   显示帮助"
            exit 0
            ;;
        *)
            echo "未知参数: $arg"
            exit 1
            ;;
    esac
done

# 检测/安装 golangci-lint
ensure_golangci_lint() {
    if command -v golangci-lint >/dev/null 2>&1; then
        LOCAL_VERSION="$(golangci-lint version --short 2>/dev/null || echo "unknown")"
        if [ -n "$LINT_VERSION" ] && [ "v$LOCAL_VERSION" != "$LINT_VERSION" ] && [ "$LOCAL_VERSION" != "$LINT_VERSION" ]; then
            echo "⚠️  golangci-lint 版本不匹配: 本地=$LOCAL_VERSION, 要求=$LINT_VERSION"
            echo "   正在安装正确版本..."
            install_golangci_lint
        fi
    else
        echo "⚠️  未找到 golangci-lint，正在安装..."
        install_golangci_lint
    fi
}

install_golangci_lint() {
    if [ -n "$LINT_VERSION" ]; then
        curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
            sh -s -- -b "$(go env GOPATH)/bin" "$LINT_VERSION"
    else
        curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
            sh -s -- -b "$(go env GOPATH)/bin"
    fi
}

ensure_golangci_lint

# 构建 lint 参数
LINT_ARGS="--timeout=5m"

if [ "$FIX_MODE" = true ]; then
    LINT_ARGS="$LINT_ARGS --fix"
fi

if [ "$DIFF_MODE" = true ]; then
    # 检测 merge-base
    MERGE_BASE=""
    if git rev-parse --verify origin/main &>/dev/null; then
        MERGE_BASE="$(git merge-base origin/main HEAD 2>/dev/null || true)"
    elif git rev-parse --verify origin/master &>/dev/null; then
        MERGE_BASE="$(git merge-base origin/master HEAD 2>/dev/null || true)"
    fi

    if [ -n "$MERGE_BASE" ]; then
        LINT_ARGS="$LINT_ARGS --new-from-rev=$MERGE_BASE"
        echo "🔍 仅检查自 $MERGE_BASE 以来的变更"
    else
        echo "⚠️  无法确定 merge-base，将运行全量检查"
    fi
fi

# 运行 lint
echo "🔍 运行 golangci-lint ($(golangci-lint version --short 2>/dev/null || echo "unknown"))..."
echo ""

if golangci-lint run $LINT_ARGS ./...; then
    echo ""
    echo "✅ Lint 检查通过"
else
    echo ""
    echo "❌ Lint 检查未通过"
    exit 1
fi
