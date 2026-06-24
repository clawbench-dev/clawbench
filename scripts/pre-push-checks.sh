#!/usr/bin/env bash
#
# pre-push-checks.sh — 提交 PR 前的本地预检，复刻 CI 流水线的全部检查
#
# 避免 push 后才发现远端 CI 报错，本地先跑一遍。
#
# 用法：
#   ./scripts/pre-push-checks.sh              # 全量检查
#   ./scripts/pre-push-checks.sh --skip-coverage  # 跳过覆盖率门槛（仅 lint + test + build + typecheck）
#   ./scripts/pre-push-checks.sh --skip-android    # 跳过 Android 覆盖率（无 JDK/Android SDK 时）
#   ./scripts/pre-push-checks.sh --fix             # 对 golangci-lint 传 --fix 自动修复
#
# 退出码：0 = 全部通过，1 = 有检查失败

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

# 默认参数
SKIP_COVERAGE=false
SKIP_ANDROID=false
FIX_MODE=""
FAILED=()
PASSED=()

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

for arg in "$@"; do
    case "$arg" in
        --skip-coverage)  SKIP_COVERAGE=true ;;
        --skip-android)   SKIP_ANDROID=true ;;
        --fix)            FIX_MODE="--fix" ;;
        --help|-h)
            echo "用法: $0 [--skip-coverage] [--skip-android] [--fix] [--help]"
            echo ""
            echo "  --skip-coverage   跳过覆盖率门槛检查"
            echo "  --skip-android    跳过 Android 覆盖率检查"
            echo "  --fix             golangci-lint --fix 自动修复"
            echo "  --help            显示帮助"
            exit 0
            ;;
        *)
            echo "未知参数: $arg"
            exit 1
            ;;
    esac
done

run_check() {
    local name="$1"
    shift
    echo -e "${BOLD}━━━ $name ━━━${NC}"
    echo "  命令: $*"
    echo ""
    if "$@"; then
        echo -e "${GREEN}✅ $name 通过${NC}"
        PASSED+=("$name")
    else
        echo -e "${RED}❌ $name 失败${NC}"
        FAILED+=("$name")
    fi
    echo ""
}

# ─── 1. golangci-lint ───
if [ -n "$FIX_MODE" ]; then
    run_check "Lint (Go)" "$SCRIPT_DIR/lint-go.sh" "$FIX_MODE"
else
    run_check "Lint (Go)" "$SCRIPT_DIR/lint-go.sh"
fi

# ─── 2. Go test ───
run_check "Test (Go)" go test ./...

# ─── 3. Go build ───
run_check "Build (Go)" go build ./cmd/server

# ─── 4. Frontend typecheck ───
if command -v npx >/dev/null 2>&1 && [ -f "$ROOT_DIR/package.json" ]; then
    # 确保 node_modules 存在
    if [ ! -d "$ROOT_DIR/node_modules" ]; then
        echo -e "${YELLOW}⚠️  node_modules 不存在，先运行 npm ci...${NC}"
        npm ci
        echo ""
    fi
    run_check "Typecheck (Frontend)" npm run typecheck
else
    echo -e "${YELLOW}⚠️  跳过 Frontend typecheck（未找到 npm/node_modules）${NC}"
    echo ""
fi

# ─── 5. Go coverage gate ───
if [ "$SKIP_COVERAGE" = false ]; then
    run_check "Coverage Gate (Go)" "$SCRIPT_DIR/check-go-coverage.sh"
else
    echo -e "${YELLOW}⚠️  跳过 Go 覆盖率门槛（--skip-coverage）${NC}"
    echo ""
fi

# ─── 6. Frontend coverage gate ───
if [ "$SKIP_COVERAGE" = false ]; then
    if command -v npx >/dev/null 2>&1 && [ -f "$ROOT_DIR/package.json" ]; then
        run_check "Coverage Gate (Frontend)" "$SCRIPT_DIR/check-frontend-coverage.sh"
    else
        echo -e "${YELLOW}⚠️  跳过 Frontend 覆盖率门槛（未找到 npm）${NC}"
        echo ""
    fi
else
    echo -e "${YELLOW}⚠️  跳过 Frontend 覆盖率门槛（--skip-coverage）${NC}"
    echo ""
fi

# ─── 7. Android coverage gate ───
if [ "$SKIP_COVERAGE" = false ] && [ "$SKIP_ANDROID" = false ]; then
    if command -v java >/dev/null 2>&1 && [ -d "$ROOT_DIR/android" ]; then
        run_check "Coverage Gate (Android)" "$SCRIPT_DIR/check-android-coverage.sh"
    else
        echo -e "${YELLOW}⚠️  跳过 Android 覆盖率门槛（未找到 java 或 android/）${NC}"
        echo ""
    fi
else
    if [ "$SKIP_ANDROID" = true ]; then
        echo -e "${YELLOW}⚠️  跳过 Android 覆盖率门槛（--skip-android）${NC}"
        echo ""
    fi
fi

# ─── 8. Frontend build ───
if command -v npx >/dev/null 2>&1 && [ -f "$ROOT_DIR/package.json" ]; then
    run_check "Build (Frontend)" npm run build
else
    echo -e "${YELLOW}⚠️  跳过 Frontend build（未找到 npm）${NC}"
    echo ""
fi

# ─── 汇总 ───
echo -e "${BOLD}════════════════════════════════════════${NC}"
echo -e "${BOLD}预检结果汇总${NC}"
echo -e "${BOLD}════════════════════════════════════════${NC}"
echo ""

if [ ${#PASSED[@]} -gt 0 ]; then
    echo -e "${GREEN}通过 (${#PASSED[@]}):${NC}"
    for name in "${PASSED[@]}"; do
        echo -e "  ${GREEN}✅ $name${NC}"
    done
fi

if [ ${#FAILED[@]} -gt 0 ]; then
    echo ""
    echo -e "${RED}失败 (${#FAILED[@]}):${NC}"
    for name in "${FAILED[@]}"; do
        echo -e "  ${RED}❌ $name${NC}"
    done
    echo ""
    echo -e "${RED}${BOLD}预检未通过，请修复后再 push/创建 PR${NC}"
    exit 1
else
    echo ""
    echo -e "${GREEN}${BOLD}全部预检通过，可以安全 push/创建 PR${NC}"
    exit 0
fi
