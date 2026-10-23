#!/usr/bin/env bash
#
# npm-publish-idempotent.sh — 幂等发布单个 npm 包
#
# 用法：
#   ./scripts/npm-publish-idempotent.sh <包目录> [npm publish 的额外参数...]
#
# 为什么需要它：发布流水线可能对同一个 tag 并行跑多次（tag 重推、API 重建
# ref、手动重跑），两个 run 会同时进入发布步骤。npm 的版本不可变，后到的
# 那个必然失败，但失败原因是「别人已经发了」而不是「这个包坏了」：
#
#   npm error code E409
#   npm error 409 Conflict - Cannot publish over previously staged version
#
#   npm error code E403
#   npm error 403 Forbidden - You cannot publish over the previously published
#                              versions: 1.2.3
#
# 这两种都应当视为成功。判定不能只看「失败后立刻 npm view」——发布是分阶段的
# （先暂存、再落库），实测从 E409 到 `npm view` 能查到相差 71 秒，所以必须轮询。
#
# 退出码：0 = 已发布（本次或他人），1 = 真的失败

set -euo pipefail

if [ $# -lt 1 ]; then
  echo "usage: $0 <package-dir> [extra npm publish args...]" >&2
  exit 2
fi

PKG_DIR="${1%/}"
shift

if [ ! -f "$PKG_DIR/package.json" ]; then
  echo "::error::$PKG_DIR/package.json not found" >&2
  exit 1
fi

# 解析成绝对路径再读清单：`./` 前缀只在相对路径下成立，绝对路径拼上去会
# 变成 `.//abs/path`（require 无法解析）。
PKG_DIR="$(cd "$PKG_DIR" && pwd)"
MANIFEST="$PKG_DIR/package.json"
NAME=$(node -p "require('$MANIFEST').name")
VERSION=$(node -p "require('$MANIFEST').version")

# 已经发布过（重跑整条流水线的常见情形）→ 直接跳过，不发第二次。
if npm view "$NAME@$VERSION" version >/dev/null 2>&1; then
  echo "$NAME@$VERSION already published, skipping"
  exit 0
fi

set +e
OUTPUT=$(npm publish "$PKG_DIR" "$@" 2>&1)
STATUS=$?
set -e

if [ $STATUS -eq 0 ]; then
  echo "$OUTPUT"
  exit 0
fi

echo "$OUTPUT" >&2

# 并发竞争：另一个 run 抢先发了同一版本。轮询等待它落库（暂存→发布有延迟）。
# 只有「版本最终出现」才算成功；超时仍查不到说明是真失败（包本身有问题）。
#
# 三个参数都可用环境变量覆盖，测试据此把 180 秒压到毫秒级。间隔允许小数
# （sleep 接受），故用独立的整数计数器而不是把秒数直接累加到浮点上。
WAIT_SECONDS="${NPM_PUBLISH_RACE_WAIT_SECONDS:-180}"
INTERVAL="${NPM_PUBLISH_RACE_INTERVAL_SECONDS:-10}"
ATTEMPTS=0
MAX_ATTEMPTS=$(awk -v w="$WAIT_SECONDS" -v i="$INTERVAL" 'BEGIN { n = (i > 0) ? w / i : 0; printf "%d", (n < 1 ? 1 : n) }')
while [ "$ATTEMPTS" -lt "$MAX_ATTEMPTS" ]; do
  sleep "$INTERVAL"
  ATTEMPTS=$((ATTEMPTS + 1))
  if npm view "$NAME@$VERSION" version >/dev/null 2>&1; then
    echo "$NAME@$VERSION was published by a concurrent run; treating as success"
    exit 0
  fi
done

echo "::error::failed to publish $NAME@$VERSION (see npm output above)"
exit 1
