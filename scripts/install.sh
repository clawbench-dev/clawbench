#!/usr/bin/env bash
#
# ClawBench 一键安装脚本 — Linux / macOS
#
# 做两件事：
#   1. 准备 Node.js（本机已有 20 及以上就直接用，否则下载一份到自己的目录）
#   2. 用 npm 把 clawbench 装到这个目录里
#
# 不碰系统目录，不需要 sudo。国内默认走 npmmirror，连不上自动换官方源。
#
# 用法：
#   curl -fsSL https://clawbench.pages.dev/install.sh | bash
#   # 或者下载后执行： bash install.sh
#
#   # 固定版本（默认装最新版；需要可复现的安装时指定版本号）
#   curl -fsSL https://clawbench.pages.dev/install.sh | CLAWBENCH_VERSION=0.130.0 bash
#
#   # 校验脚本本身（sha256 在 GitHub Release 的说明里给出）
#   curl -fsSL https://clawbench.pages.dev/install.sh -o install.sh
#   echo "<sha256>  install.sh" | sha256sum -c - && bash install.sh
#
#   # 卸载（删安装目录 + rc 文件里那段 PATH 标记）
#   curl -fsSL https://clawbench.pages.dev/install.sh | bash -s -- --uninstall
#
# 安装位置（可用 CLAWBENCH_HOME 覆盖）：
#   ${XDG_DATA_HOME:-~/.local/share}/clawbench/node   Node.js
#   ${XDG_DATA_HOME:-~/.local/share}/clawbench/npm    全局 npm 前缀（clawbench 在这里）

set -euo pipefail

CLAWBENCH_HOME="${CLAWBENCH_HOME:-${XDG_DATA_HOME:-$HOME/.local/share}/clawbench}"
NODE_DIR="$CLAWBENCH_HOME/node"
NPM_PREFIX="$CLAWBENCH_HOME/npm"
NODE_MIN_MAJOR=20
NPM_PACKAGE="@xulongzhe/clawbench"
# 固定版本（不带 v 前缀，与 npm 语义化版本一致）。留空 = 装最新版。
CLAWBENCH_VERSION="${CLAWBENCH_VERSION:-}"
RC_MARKER='>>> clawbench installer >>>'

if [ -t 1 ]; then
  BOLD=$'\033[1m'
  DIM=$'\033[2m'
  RED=$'\033[31m'
  GREEN=$'\033[32m'
  YELLOW=$'\033[33m'
  RESET=$'\033[0m'
else
  BOLD=''
  DIM=''
  RED=''
  GREEN=''
  YELLOW=''
  RESET=''
fi

step() { printf '\n%s==>%s %s\n' "$BOLD" "$RESET" "$1"; }
info() { printf '    %s\n' "$1"; }
ok() { printf '    %s✓%s %s\n' "$GREEN" "$RESET" "$1"; }
warn() { printf '%s警告:%s %s\n' "$YELLOW" "$RESET" "$1" >&2; }
die() { printf '%s错误:%s %s\n' "$RED" "$RESET" "$1" >&2; exit 1; }

usage() {
  cat <<'EOF'
ClawBench 一键安装脚本（Linux / macOS）

用法：
  bash install.sh               安装（Node.js + ClawBench）
  bash install.sh --uninstall   卸载（删安装目录 + rc 文件里的 PATH 标记）
  bash install.sh --help        显示这段帮助

环境变量：
  CLAWBENCH_HOME     安装目录，默认 ${XDG_DATA_HOME:-~/.local/share}/clawbench
  CLAWBENCH_VERSION  固定版本，例如 0.130.0；留空装最新版

示例：
  curl -fsSL https://clawbench.pages.dev/install.sh | bash
  curl -fsSL https://clawbench.pages.dev/install.sh | CLAWBENCH_VERSION=0.130.0 bash
  curl -fsSL https://clawbench.pages.dev/install.sh | bash -s -- --uninstall
EOF
}

# 当前 shell 会读的 rc 文件。卸载时要用同一套判定，所以抽成函数。
rc_file() {
  case "$(basename "${SHELL:-/bin/bash}")" in
    zsh) printf '%s\n' "$HOME/.zshrc" ;;
    bash)
      # Linux 的终端一般是「非登录交互 shell」，读 .bashrc；
      # macOS 的 Terminal 开的是登录 shell，读 .bash_profile。
      if [ "$(uname -s)" = Darwin ]; then
        printf '%s\n' "$HOME/.bash_profile"
      else
        printf '%s\n' "$HOME/.bashrc"
      fi
      ;;
    *) printf '%s\n' "$HOME/.profile" ;;
  esac
}

# ---------- 卸载 ----------
#
# 装的时候写了三处状态：安装目录（Node.js + npm 全局前缀）、rc 文件里的标记块、
# 以及（只在「跑过 clawbench 但没在 PATH 里」时）~/.npm 下的 npm 缓存条目。
# 这里逐项清理，任何一步失败都只警告、不中断——「装了一半」的机器也要能卸干净。

uninstall() {
  step "卸载 ClawBench"

  if [ -d "$CLAWBENCH_HOME" ]; then
    # 目录可能是只读树（或里面有别人放的文件），删不掉就告诉用户自己删，
    # 但继续往下清 rc 与 npm 缓存，不要留下半截状态。
    if rm -rf "$CLAWBENCH_HOME" 2>/dev/null; then
      ok "已删除安装目录 $CLAWBENCH_HOME"
    else
      warn "删不掉 $CLAWBENCH_HOME，请手动执行：rm -rf \"$CLAWBENCH_HOME\""
    fi
  else
    info "安装目录不存在（$CLAWBENCH_HOME），跳过"
  fi

  # 只删我们自己写的那个标记块（开始行、两行说明/export、结束行），rc 里的
  # 其他内容原样不动。
  #
  # 不用「找到开始行就删到结束行」：结束标记被手工删掉或写坏时，那会把后面
  # **所有**行都吞掉，等于毁掉用户的 shell 配置。这里逐行判定，最多只跳过
  # 那块已知内容，遇到不认识的行走「结束标记丢了」分支原样吐回。
  #
  # 注意 awk 的排版坑：`}` 与下一条规则之间**不能留空行**。留了之后
  # `line == b` 会被当成一条独立语句，紧跟的 `{ ... }` 变成孤立动作块被丢掉，
  # gawk 与 mawk 都是这个行为且不报错（写这里的调试里实际踩到过）。
  local rc
  rc="$(rc_file)"
  if [ -f "$rc" ] && grep -qF "$RC_MARKER" "$rc" 2>/dev/null; then
    local filtered
    if filtered="$(awk -v b="$RC_MARKER" -v e="<<< clawbench installer <<<" '
      {
        line = $0
        sub(/^# */, "", line)
        sub(/^ \**/, "", line)
      }
      line == b {
        block = 1
        next
      }
      block == 1 {
        if (line == e) { block = 2; next }
        if (line == "ClawBench / Node.js 的安装目录（由 install.sh 添加）") next
        if (line ~ /^export PATH=/) next
        # 结束标记丢了（被手工删掉或写坏）：把这一行原样吐回去，
        # 从下一行起恢复正常处理——宁可留下一行残留标记，也不能吞掉用户的配置。
        block = 0
        print
        next
      }
      { print }
    ' "$rc" 2>/dev/null)"; then
      if printf '%s\n' "$filtered" >"$rc" 2>/dev/null; then
        ok "已从 $rc 移除 PATH 标记块"
      else
        warn "改不了 $rc，请手动删除其中 >>> clawbench installer >>> 到 <<< clawbench installer <<< 之间的内容"
      fi
    else
      warn "读取 $rc 失败，请手动删除其中 >>> clawbench installer >>> 到 <<< clawbench installer <<< 之间的内容"
    fi
  else
    info "$rc 里没有 PATH 标记，跳过"
  fi

  # npm 会为用户装到自建前缀里的包建一个 ~/.npm/_lib 条目，其中存着一份
  # 绝对路径；npm 自己在 install 时会尝试删它，但那只在包还在的时候就生效了。
  local npm_link="$HOME/.npm/_lib/node_modules/$NPM_PACKAGE"
  if [ -L "$npm_link" ]; then
    rm -f "$npm_link" 2>/dev/null || warn "删不掉 $npm_link，可以手动删除"
  fi

  cat <<EOF

$(printf '%s卸载完成。%s' "$GREEN" "$RESET")

${DIM}用户数据（会话、配置、.clawbench 数据目录）没有动，还在原来的位置。
当前终端里的 PATH 还指着旧目录，重开一个终端即可。${RESET}
EOF
}

# ---------- 参数 ----------

MODE=install
while [ "$#" -gt 0 ]; do
  case "$1" in
    --uninstall) MODE=uninstall ;;
    --help | -h) usage; exit 0 ;;
    *)
      printf '%s错误:%s 未知参数：%s（--help 看用法）\n' "$RED" "$RESET" "$1" >&2
      exit 1
      ;;
  esac
  shift
done

if [ "$MODE" = uninstall ]; then
  uninstall
  exit 0
fi

# ---------- 平台 ----------

case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *)
    die "不支持的系统：$(uname -s)。Linux / macOS 用这个脚本，Windows 请用 install.ps1"
    ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) ARCH=x64 ;;
  arm64 | aarch64) ARCH=arm64 ;;
  *) die "不支持的 CPU 架构：$(uname -m)。ClawBench 只发布 x64 与 arm64 平台包" ;;
esac

if [ "$OS" = linux ] && [ -f /etc/alpine-release ]; then
  die "这是 Alpine（musl）系统，Node.js 官方包在这里跑不起来。
    请改用系统包：apk add nodejs npm && npm install -g $NPM_PACKAGE"
fi

# Termux 上报的是 Linux，但它是 Android 的 bionic，官方 Node 二进制跑不起来，
# clawbench 也要用专门的 android-arm64 包。这里直接给 Termux 自己的装法。
if [ -n "${TERMUX_VERSION:-}" ] || [ -d /data/data/com.termux ]; then
  die "这是 Termux（安卓）。请用 Termux 的方式装：
    pkg install -y nodejs-lts git
    npm install -g $NPM_PACKAGE"
fi

if [ "$(id -u)" = 0 ] && [ -n "${SUDO_USER:-}" ]; then
  warn "正在用 sudo 运行。会装到 /root 下，普通用户之后用不了。建议去掉 sudo 重跑。"
fi

# ---------- HTTP ----------
#
# 脚本里所有 2>/dev/null 都在探测或「可选内容」上，且每个都有人接着判断结果：
#   · http_ok / http_get(index.json) / SHASUMS —— 失败就是换源或跳过校验，
#     调用方自己会打提示；
#   · node -v（系统 Node）—— 失败意味着改用自装那份，没有可行动信息；
#   · uninstall 里的 rm/grep/awk/printf —— 失败只让我们换一句「手动删除」提示；
#   · PATH 写入 —— 失败要走「警告 + 打印手动 PATH 行」而不是裸退出。
# 真正要打断安装的失败一律用 die 并打印我们自己的一句话，所以只有它们不该被吞。

if command -v curl >/dev/null 2>&1; then
  # 探活：只看退出码，curl 的 stderr（连不上的细节）对用户没用
  http_ok() { curl -fsS -m 8 -o /dev/null "$1" 2>/dev/null; }
  http_get() { curl -fsSL --retry 2 --connect-timeout 10 "$1"; }
  http_download() { curl -fL --retry 3 --connect-timeout 10 --progress-bar -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  # 同上：探活的失败由 if 分支处理
  http_ok() { wget -q -T 8 -O /dev/null "$1" 2>/dev/null; }
  http_get() { wget -q -T 30 -O - "$1"; }
  http_download() { wget -q --show-progress -O "$2" "$1"; }
else
  die "需要 curl 或 wget 才能下载，请先安装其中一个"
fi

# ---------- 镜像 ----------

step "检查网络"
MIRROR_BASE="https://registry.npmmirror.com/-/binary/node"
MIRROR_ALT="https://nodejs.org/dist"
NPM_REGISTRY="https://registry.npmmirror.com"
NPM_REGISTRY_ALT="https://registry.npmjs.org"

if http_ok "$NPM_REGISTRY/-/ping"; then
  ok "使用国内镜像 npmmirror"
else
  MIRROR_BASE="$MIRROR_ALT"
  NPM_REGISTRY="$NPM_REGISTRY_ALT"
  info "镜像不可达，使用官方源"
fi

# ---------- Node.js ----------

step "准备 Node.js"
export PATH="$NODE_DIR/bin:$PATH"

major_of() {
  # "v20.11.1" -> 20；解析不出来时返回 0
  local v="${1#v}"
  v="${v%%.*}"
  case "$v" in
    '' | *[!0-9]*) echo 0 ;;
    *) echo "$v" ;;
  esac
}

# Node 官方二进制是动态链接的，最小化系统（容器、server 版、部分精简发行版）
# 常常没有 libatomic1，解压完一跑就 "error while loading shared libraries"。
# 那种报错对用户没有任何指向性，这里换成一句能照着做的提示。
node_runtime_error() {
  die "Node.js 装好了但启动不了：
    $1
    这通常是系统缺运行库（最常见是 libatomic1）。
    Debian / Ubuntu 可以执行：sudo apt-get install -y libatomic1
    或者直接用系统包管理器装 Node.js 20 及以上，再执行：npm install -g $NPM_PACKAGE"
}

# 下载前先确认系统的动态链接库够不够，缺了当场停下。
#
# 官方 Node 二进制是动态链接的，最小化系统（容器、server 版、部分精简发行版）
# 常常没有 libatomic1，解压完一跑就 "error while loading shared libraries"。
# 早先靠「系统里的 node 能不能跑」间接判断，但发行版自带的 node 常常打了补丁、
# 不依赖 libatomic.so.1，于是判断通过、下载完 60MB 才发现装的那份跑不起来。
# 这里改成直接找文件：ldconfig 认得的库、或是 ld.so 搜索路径里存在的库都算数。
have_shared_lib() {
  local name="$1" dir
  # 测试要在没有这个库的机器上验证「缺库」，而宿主几乎一定有它；
  # 用这个变量把路径收窄到沙箱目录，不然这分支永远测不到。
  if [ -n "${CLAWBENCH_LIBDIR_OVERRIDE:-}" ]; then
    for dir in $CLAWBENCH_LIBDIR_OVERRIDE; do
      [ -e "$dir/$name" ] && return 0
    done
    return 1
  fi
  if command -v ldconfig >/dev/null 2>&1; then
    ldconfig -p 2>/dev/null | grep -qF "$name" && return 0
  fi
  # ldconfig 不可用（或缓存过期）时退回到按路径找。
  for dir in /lib /lib64 /usr/lib /usr/lib64 /usr/local/lib \
    /lib/x86_64-linux-gnu /usr/lib/x86_64-linux-gnu \
    /lib/aarch64-linux-gnu /usr/lib/aarch64-linux-gnu; do
    [ -e "$dir/$name" ] && return 0
  done
  return 1
}

preflight_node_libs() {
  # 只有 Linux 的官方包链接 libatomic；macOS 不需要，也不会有 ldconfig。
  [ "$OS" = linux ] || return 0
  have_shared_lib libatomic.so.1 && return 0
  die "系统缺少 libatomic.so.1，Node.js 装上也跑不起来，这次先不下载了。
    Debian / Ubuntu：sudo apt-get install -y libatomic1
    RHEL / CentOS / Rocky：sudo dnf install -y libatomic
    Alpine / Termux 的官方包不依赖它，但那两个环境请走系统包管理器装 Node.js。
    装好运行库后重跑本脚本即可。"
}

# 把一个 JSON 数组里的 version 字段全部抽出来。`" ]` 只出现在每个对象末尾，
# 所以这条路径不会踩到 engines / files 之类嵌套数组里的 "version" 字段。
json_lts_versions() {
  tr -d '\r\n\t ' <"$1" | sed 's/"version"/\n"version"/g' |
    # 只留 `"lts":"<名字>"` 的对象：未进入 LTS 的版本是 `"lts":false`，
    # 靠「偶数码」猜是错的（v24 一度也标 false）。
    sed -n 's/^"version":"\([^"]*\)".*"lts":"[^"]*".*/\1/p'
}

# 取最新 LTS：先按数值排序取最大的 lts 主版本，再取该大版本里最大的版本号。
# 早先的写法是「取数组里第一条带 lts 的」——依赖服务端返回顺序，那不是契约。
pick_latest_lts() {
  local versions major max_lts
  versions="$(json_lts_versions "$1")"
  [ -n "$versions" ] || return 0
  # 先在所有 LTS 里取最大的主版本，再在那个主版本里取最大版本号。
  # 排序一律按数值：字符串比较会把 22.9 排到 22.20 后面。
  max_lts="$(printf '%s\n' "$versions" | while IFS= read -r lv; do
    major="$(major_of "$lv")"
    [ "$major" -ge "$NODE_MIN_MAJOR" ] || continue
    printf '%s\n' "$major"
  done | sort -n | tail -1)"
  [ -n "$max_lts" ] || return 0
  # 在那个大版本里取最大版本号。先去掉 v 前缀再排序：`sort -k1,1n` 碰到
  # v24 这种非数字会被当成字符串比较，结果整个排序失效（仍然是原顺序）。
  printf '%s\n' "$versions" | while IFS= read -r lv; do
    if [ "$(major_of "$lv")" = "$max_lts" ]; then printf '%s\n' "${lv#v}"; fi
  done | sort -t. -k1,1n -k2,2n -k3,3n | tail -1
}

NODE_READY=0

if [ -x "$NODE_DIR/bin/node" ]; then
  if bundled_version="$(node -v 2>&1)"; then
    info "已有本脚本装的 Node.js $bundled_version，直接使用"
    NODE_READY=1
  else
    node_runtime_error "$bundled_version"
  fi
elif command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
  # `node -v` 在能跑时退出码为 0；这里失败只说明系统 Node 坏了/是假的，
  # 下面的分支会当成「没有可用的 Node」另装一份，所以不需要它的 stderr。
  current="$(node -v 2>/dev/null || echo '')"
  if [ "$(major_of "$current")" -ge "$NODE_MIN_MAJOR" ]; then
    ok "已安装 Node.js $current，直接使用"
    NODE_READY=1
  else
    info "已安装 Node.js $current，低于 $NODE_MIN_MAJOR，另装一份"
  fi
fi

if [ "$NODE_READY" = 0 ]; then
  # 解压要用 tar，校验要用 sha256sum/shasum。缺了的话下面会以一条看起来像
  # 「下载失败」的报错退出（tar: not found 混在一堆 curl 输出里），先讲清楚。
  command -v tar >/dev/null 2>&1 || die "需要 tar 才能解压 Node.js，请先安装（Debian / Ubuntu：apt-get install -y tar）"
  preflight_node_libs

  info "查询最新 LTS 版本..."
  # index.json 是可选内容：镜像取不到就换官方源，所以镜像那次不看 stderr
  # （换源时我们自己会打一行说明，curl 的报错只会让人以为安装失败了）。
  index_file="$(mktemp)"
  trap 'rm -f "$index_file"' EXIT
  http_get "$MIRROR_BASE/index.json" 2>/dev/null >"$index_file" || true
  if [ -z "$(pick_latest_lts "$index_file")" ] && [ "$MIRROR_BASE" != "$MIRROR_ALT" ]; then
    info "镜像取不到版本列表，换官方源"
    MIRROR_BASE="$MIRROR_ALT"
    http_get "$MIRROR_BASE/index.json" >"$index_file" || die "拿不到 Node.js 版本列表，请检查网络"
  fi
  version="$(pick_latest_lts "$index_file")"
  [ -n "$version" ] || die "拿不到 Node.js 版本列表，请检查网络"

  # 目录名和文件名都带 v（index.json 里的 "version" 也带，但 pick_latest_lts
  # 去掉过前缀，所以这里必须补回来）。少了这个 v 两个源都是 404。
  file="node-v$version-$OS-$ARCH.tar.gz"
  info "下载 Node.js $version（$OS-$ARCH）"

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp" "$index_file"' EXIT

  http_download "$MIRROR_BASE/v$version/$file" "$tmp/$file" || {
    if [ "$MIRROR_BASE" != "$MIRROR_ALT" ]; then
      info "镜像下载失败，换官方源重试"
      MIRROR_BASE="$MIRROR_ALT"
      http_download "$MIRROR_BASE/v$version/$file" "$tmp/$file" || die "下载 Node.js 失败"
    else
      die "下载 Node.js 失败"
    fi
  }

  # 校验和能拿到就校验；拿不到不阻断（官方源偶尔抽风）。
  # 先在国内镜像取：安装包本身就是从这个源下的，跟它比才有意义，也免得
  # 国内直连官方源（实测几 KB/s）把「已经下完 60MB」的安装卡在一次多余下载上。
  # 镜像取不到（比如刚才已经换成官方源了）再换官方源。
  # 这里吞掉 stderr 是刻意的：拿不到校验和不是用户能处理的问题，
  # 而真的校验不过会由下面的 die 明确说出来。
  sums="$(http_get "https://registry.npmmirror.com/-/binary/node/v$version/SHASUMS256.txt" 2>/dev/null || true)"
  if [ -z "$sums" ] && [ "$MIRROR_BASE" != "$MIRROR_ALT" ]; then
    sums="$(http_get "$MIRROR_BASE/v$version/SHASUMS256.txt" 2>/dev/null || true)"
  fi
  expected="$(printf '%s\n' "$sums" | awk -v f="$file" '$2 == f { print $1 }')"
  if [ -n "$expected" ]; then
    if command -v sha256sum >/dev/null 2>&1; then
      actual="$(sha256sum "$tmp/$file" | awk '{ print $1 }')"
    elif command -v shasum >/dev/null 2>&1; then
      actual="$(shasum -a 256 "$tmp/$file" | awk '{ print $1 }')"
    else
      # 两个校验工具都没有：跳过校验，但要说出来，别让用户以为验过了。
      info "系统里没有 sha256sum / shasum，跳过安装包校验"
      actual=""
    fi
    if [ -n "$actual" ] && [ "$actual" != "$expected" ]; then
      die "Node.js 安装包校验失败（下载不完整），请重试"
    fi
  fi

  tar -xzf "$tmp/$file" -C "$tmp"
  mkdir -p "$CLAWBENCH_HOME"
  rm -rf "$NODE_DIR"
  mv "$tmp/node-v$version-$OS-$ARCH" "$NODE_DIR"

  if ! installed_version="$(node -v 2>&1)"; then
    node_runtime_error "$installed_version"
  fi
  ok "Node.js $installed_version 已就位：$NODE_DIR"
fi

command -v npm >/dev/null 2>&1 || die "找不到 npm，Node.js 安装似乎不完整"

# ---------- ClawBench ----------

step "安装 ClawBench（npm 全局包）"
mkdir -p "$NPM_PREFIX"

# 公式：ClawBench==<版本>。默认 latest（每次安装可能不再相同——脚本与服务端一样
# 不保证可复现）；要复现就指定 CLAWBENCH_VERSION。npm 会自校验 tarball 的
# dist.integrity，脚本这里不再重复做。
if [ -n "$CLAWBENCH_VERSION" ]; then
  NPM_SPEC="$NPM_PACKAGE@$CLAWBENCH_VERSION"
  info "固定版本：$CLAWBENCH_VERSION"
else
  NPM_SPEC="$NPM_PACKAGE@latest"
  info "下载量约 150MB，国内镜像下通常一两分钟（装最新版；要可复现请设 CLAWBENCH_VERSION）"
fi

npm install -g "$NPM_SPEC" \
  --prefix "$NPM_PREFIX" \
  --registry "$NPM_REGISTRY" \
  --no-fund \
  --no-audit

CLAWBENCH_BIN="$NPM_PREFIX/bin/clawbench"
[ -x "$CLAWBENCH_BIN" ] || die "npm 跑完了但没找到 $CLAWBENCH_BIN，请把上面的输出发出来"

export PATH="$NPM_PREFIX/bin:$PATH"
# stderr 必须一起收：平台包缺失、动态库缺失时，真正的原因（"Cannot find module
# .../clawbench-linux-x64"、"error while loading shared libraries"）全在 stderr 里，
# 只留一句「多半是平台包没装上」等于把用户唯一能自助的线索扔掉。
if ! version="$(clawbench --version 2>&1)"; then
  die "clawbench 装上了但跑不起来：

$version

    上面这段就是它自己的报错。常见原因：平台包没装上（npm 输出里找 EBADPLATFORM /
    EBADENGINE），或者系统缺运行库（见前面的 libatomic1 提示）。"
fi
version="$(printf '%s\n' "$version" | head -1)"
[ -n "$version" ] || die "clawbench --version 没有输出，装出来的东西可能不完整，请把上面的 npm 输出发出来"
# 实际装上的版本号：npm install 可能解析到比预期更新的版本（或镜像滞后），
# 所以这里打印的是二进制自己报的版本，而不是我们请求的那个。
actual_version="${version#clawbench }"
ok "clawbench $actual_version"

# ---------- PATH ----------

step "把命令加进 PATH"
if [ -d "$NODE_DIR/bin" ]; then
  PATH_ENTRY="$NODE_DIR/bin:$NPM_PREFIX/bin"
else
  PATH_ENTRY="$NPM_PREFIX/bin"
fi

MANUAL_PATH_LINE="export PATH=\"$PATH_ENTRY:\$PATH\""
RC="$(rc_file)"

# 先判可写再写：rc 不存在（新建）、只读、家目录挂载成只读都会让 `>>` 失败，
# 而 set -e 会在「装好了」之前裸退出——用户看到的是一个装了但用不了的机器，
# 也没有任何提示告诉他该加什么。
dir_writable() {
  [ -d "$1" ] && [ -w "$1" ]
}

if [ -f "$RC" ] && grep -qF "$RC_MARKER" "$RC" 2>/dev/null; then
  info "PATH 已经配置过（$RC）"
elif { [ -e "$RC" ] && [ -w "$RC" ]; } || { [ ! -e "$RC" ] && dir_writable "$(dirname "$RC")"; }; then
  if {
    printf '\n# %s\n' "$RC_MARKER"
    printf '# ClawBench / Node.js 的安装目录（由 install.sh 添加）\n'
    printf '%s\n' "$MANUAL_PATH_LINE"
    printf '# <<< clawbench installer <<<\n'
  } >>"$RC" 2>/dev/null; then
    ok "已写入 $RC"
  else
    warn "写不进 $RC（权限或只读文件系统），请手动把下面这行加到 $RC："
    printf '\n    %s\n\n' "$MANUAL_PATH_LINE" >&2
  fi
else
  warn "$RC 不可写（权限或只读文件系统），没有改它。请手动把下面这行加到 $RC："
  printf '\n    %s\n\n' "$MANUAL_PATH_LINE" >&2
fi

# ---------- 收尾 ----------

cat <<EOF

$(printf '%sClawBench %s 装好了。%s' "$GREEN" "$actual_version" "$RESET")

启动：
    ${BOLD}clawbench${RESET}

然后在浏览器打开：
    http://localhost:20000

首次启动会把随机密码打印在终端里，请保存好。

${DIM}当前终端还没加载新的 PATH，先执行下面这行（或者重开一个终端）：${RESET}
    $MANUAL_PATH_LINE

${DIM}AI 智能体不在这里装。启动后打开网页，欢迎界面里每个智能体都有「安装」按钮，
点一下就在这台机器上装好（同样走国内镜像）。${RESET}

${DIM}卸载：bash install.sh --uninstall（curl | bash 时写成 bash -s -- --uninstall）${RESET}
EOF
