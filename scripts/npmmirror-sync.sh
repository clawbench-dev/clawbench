#!/usr/bin/env bash
#
# npmmirror-sync.sh — 让 npmmirror 立刻同步刚发布的版本
#
# 用法：
#   ./scripts/npmmirror-sync.sh <版本号> <包名> [包名...]
#
# 为什么要它：npmmirror 平时靠上游事件自动同步，但大包（linux 服务端包 60MB）
# 经常漏或拖很久。v0.132.0 实测：主包已经是 0.132.0，而 linux-x64 / linux-arm64 /
# darwin-* / win32-x64 五个平台包全停在 0.131.0。主包的 optionalDependencies
# 指向 0.132.0，从镜像装的人拉不到对应平台包，直接装不上。
#
# 做法：对每个包提交一次同步任务，然后轮询镜像的版本元数据，直到能查到该版本；
# 最后再探一次 tarball 能否下载 —— 元数据先到、文件后到，只查元数据会误判成功。
#
# 退出码：0 = 全部可安装；1 = 有包在等待窗口内没同步好
#
# 环境变量（测试用，正常不用改）：
#   NPMMIRROR_REGISTRY        默认 https://registry.npmmirror.com
#   NPMMIRROR_WAIT_SECONDS    每个包最多等多久，默认 600
#   NPMMIRROR_INTERVAL_SECONDS 轮询间隔，默认 15

set -euo pipefail

REGISTRY="${NPMMIRROR_REGISTRY:-https://registry.npmmirror.com}"
WAIT_SECONDS="${NPMMIRROR_WAIT_SECONDS:-600}"
INTERVAL="${NPMMIRROR_INTERVAL_SECONDS:-15}"

if [ $# -lt 2 ]; then
  echo "usage: $0 <version> <package> [package...]" >&2
  exit 2
fi

VERSION="$1"
shift

# npm 的 scope 包名里的 "/" 在 URL 里必须编码，否则请求会打到别的路径上。
encode_name() {
  printf '%s' "$1" | sed 's|/|%2F|g'
}

MAX_ATTEMPTS=$(awk -v w="$WAIT_SECONDS" -v i="$INTERVAL" 'BEGIN { n = (i > 0) ? w / i : 0; printf "%d", (n < 1 ? 1 : n) }')

failed=()
total=0

# 元数据里出现该版本号才算同步完成。用 curl 而不是 `npm view`：后者会读本地
# 缓存与 .npmrc，在 CI 里容易拿到别的源的结果。
version_visible() {
  local name="$1"
  curl -fsS --max-time 30 "$REGISTRY/$(encode_name "$name")/$VERSION" 2>/dev/null \
    | grep -q "\"version\":\"$VERSION\""
}

# tarball 能下才算真的可用。只取前 1KB，不把 60MB 拉下来。
tarball_reachable() {
  local name="$1" doc url code
  doc=$(curl -fsS --max-time 30 "$REGISTRY/$(encode_name "$name")/$VERSION" 2>/dev/null) || return 1
  url=$(printf '%s' "$doc" | grep -o '"tarball":"[^"]*"' | head -1 | cut -d'"' -f4)
  [ -n "$url" ] || return 1
  code=$(curl -sSL --max-time 120 -r 0-1023 -o /dev/null -w '%{http_code}' "$url" 2>/dev/null || echo 000)
  case "$code" in
    200|206) return 0 ;;
    *) echo "    tarball HTTP $code: $url" >&2; return 1 ;;
  esac
}

# 先全部提交同步任务，再统一等：镜像的任务是并行的，逐个等会把最坏情况变成
# 「包数 × 等待窗口」。这里总共只等 WAIT_SECONDS。
for name in "$@"; do
  total=$((total + 1))
  echo "→ $name@$VERSION"
  # 提交失败不直接判死：可能已经有一个任务在跑，后面的轮询才是判据。
  if curl -fsS --max-time 30 -X PUT "$REGISTRY/-/package/$(encode_name "$name")/syncs" \
    -H 'Content-Type: application/json' -d '{}' >/dev/null 2>&1; then
    echo "  sync queued"
  else
    echo "::warning::could not queue a sync for $name; waiting anyway" >&2
  fi
done

pending=("$@")
attempt=0
while [ ${#pending[@]} -gt 0 ] && [ "$attempt" -lt "$MAX_ATTEMPTS" ]; do
  still=()
  for name in "${pending[@]}"; do
    if version_visible "$name"; then
      echo "  ✓ $name@$VERSION 元数据已同步"
    else
      still+=("$name")
    fi
  done
  pending=(${still[@]+"${still[@]}"})
  if [ ${#pending[@]} -gt 0 ]; then
    attempt=$((attempt + 1))
    sleep "$INTERVAL"
  fi
done

for name in ${pending[@]+"${pending[@]}"}; do
  echo "  ✗ $name：${WAIT_SECONDS}s 内版本没有出现" >&2
  failed+=("$name")
done

# 元数据到了不等于能装：tarball 是随后才传到 CDN 的，这里实际取 1KB 验一次。
for name in "$@"; do
  # 已经在上面判超时的包不再重复报。
  skip=0
  for f in ${failed[@]+"${failed[@]}"}; do
    [ "$f" = "$name" ] && skip=1
  done
  [ "$skip" = "1" ] && continue

  if tarball_reachable "$name"; then
    echo "  ✓ $name 可下载"
  else
    echo "  ✗ $name：元数据已同步，但 tarball 下载不了" >&2
    failed+=("$name")
  fi
done

if [ ${#failed[@]} -gt 0 ]; then
  echo "::error::npmmirror 未同步完成：${failed[*]}（重跑这个 job 即可，不影响已发布的包）" >&2
  exit 1
fi

echo "全部 $total 个包在 npmmirror 上可安装"
