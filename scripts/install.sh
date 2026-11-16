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
# 安装位置（可用 CLAWBENCH_HOME 覆盖）：
#   ${XDG_DATA_HOME:-~/.local/share}/clawbench/node   Node.js
#   ${XDG_DATA_HOME:-~/.local/share}/clawbench/npm    全局 npm 前缀（clawbench 在这里）

set -euo pipefail

CLAWBENCH_HOME="${CLAWBENCH_HOME:-${XDG_DATA_HOME:-$HOME/.local/share}/clawbench}"
NODE_DIR="$CLAWBENCH_HOME/node"
NPM_PREFIX="$CLAWBENCH_HOME/npm"
NODE_MIN_MAJOR=20
NPM_PACKAGE="@xulongzhe/clawbench"

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

# ---------- HTTP ----------

if command -v curl >/dev/null 2>&1; then
  http_ok() { curl -fsS -m 8 -o /dev/null "$1" 2>/dev/null; }
  http_get() { curl -fsSL --retry 2 --connect-timeout 10 "$1"; }
  http_download() { curl -fL --retry 3 --connect-timeout 10 --progress-bar -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  http_ok() { wget -q -T 8 -O /dev/null "$1" 2>/dev/null; }
  http_get() { wget -q -T 30 -O - "$1"; }
  http_download() { wget -q --show-progress -O "$2" "$1"; }
else
  die "需要 curl 或 wget 才能下载，请先安装其中一个"
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
  *) die "不支持的 CPU 架构：$(uname -m)" ;;
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

NODE_READY=0

if [ -x "$NODE_DIR/bin/node" ]; then
  if bundled_version="$(node -v 2>&1)"; then
    info "已有本脚本装的 Node.js $bundled_version，直接使用"
    NODE_READY=1
  else
    node_runtime_error "$bundled_version"
  fi
elif command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
  current="$(node -v 2>/dev/null || echo '')"
  if [ "$(major_of "$current")" -ge "$NODE_MIN_MAJOR" ]; then
    ok "已安装 Node.js $current，直接使用"
    NODE_READY=1
  else
    info "已安装 Node.js $current，低于 $NODE_MIN_MAJOR，另装一份"
  fi
fi

if [ "$NODE_READY" = 0 ]; then
  info "查询最新 LTS 版本..."
  index="$(http_get "$MIRROR_BASE/index.json" 2>/dev/null || true)"
  if [ -z "$index" ] && [ "$MIRROR_BASE" != "$MIRROR_ALT" ]; then
    info "镜像取不到版本列表，换官方源"
    MIRROR_BASE="$MIRROR_ALT"
    index="$(http_get "$MIRROR_BASE/index.json")" || die "拿不到 Node.js 版本列表，请检查网络"
  fi
  [ -n "$index" ] || die "拿不到 Node.js 版本列表，请检查网络"

  # index.json 每个版本一行，取第一条带 lts 的（列表按新到旧排）。
  # `|| true`：grep -m1 命中就退出，上游 printf 会吃 SIGPIPE，
  # 开了 pipefail 的话整条管道算失败，这里不能让它把脚本带走。
  version="$(printf '%s\n' "$index" |
    grep -m1 '"lts":"' |
    sed -n 's/.*"version":"\(v[0-9][^"]*\)".*/\1/p' || true)"
  [ -n "$version" ] || die "解析 Node.js 版本号失败"

  file="node-$version-$OS-$ARCH.tar.gz"
  info "下载 Node.js $version（$OS-$ARCH）"

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT

  http_download "$MIRROR_BASE/$version/$file" "$tmp/$file" || {
    if [ "$MIRROR_BASE" != "$MIRROR_ALT" ]; then
      info "镜像下载失败，换官方源重试"
      MIRROR_BASE="$MIRROR_ALT"
      http_download "$MIRROR_BASE/$version/$file" "$tmp/$file" || die "下载 Node.js 失败"
    else
      die "下载 Node.js 失败"
    fi
  }

  # 校验和能拿到就校验；拿不到不阻断（官方源偶尔抽风）
  sums="$(http_get "$MIRROR_BASE/$version/SHASUMS256.txt" 2>/dev/null || true)"
  expected="$(printf '%s\n' "$sums" | awk -v f="$file" '$2 == f { print $1 }')"
  if [ -n "$expected" ]; then
    if command -v sha256sum >/dev/null 2>&1; then
      actual="$(sha256sum "$tmp/$file" | awk '{ print $1 }')"
    elif command -v shasum >/dev/null 2>&1; then
      actual="$(shasum -a 256 "$tmp/$file" | awk '{ print $1 }')"
    else
      actual=""
    fi
    if [ -n "$actual" ] && [ "$actual" != "$expected" ]; then
      die "Node.js 安装包校验失败（下载不完整），请重试"
    fi
  fi

  tar -xzf "$tmp/$file" -C "$tmp"
  mkdir -p "$CLAWBENCH_HOME"
  rm -rf "$NODE_DIR"
  mv "$tmp/node-$version-$OS-$ARCH" "$NODE_DIR"

  if ! installed_version="$(node -v 2>&1)"; then
    node_runtime_error "$installed_version"
  fi
  ok "Node.js $installed_version 已就位：$NODE_DIR"
fi

command -v npm >/dev/null 2>&1 || die "找不到 npm，Node.js 安装似乎不完整"

# ---------- ClawBench ----------

step "安装 ClawBench（npm 全局包）"
info "下载量约 150MB，国内镜像下通常一两分钟"
mkdir -p "$NPM_PREFIX"

npm install -g "$NPM_PACKAGE@latest" \
  --prefix "$NPM_PREFIX" \
  --registry "$NPM_REGISTRY" \
  --no-fund \
  --no-audit

CLAWBENCH_BIN="$NPM_PREFIX/bin/clawbench"
[ -x "$CLAWBENCH_BIN" ] || die "npm 跑完了但没找到 $CLAWBENCH_BIN，请把上面的输出发出来"

export PATH="$NPM_PREFIX/bin:$PATH"
version="$(clawbench --version 2>/dev/null | head -1 || true)"
if [ -z "$version" ]; then
  die "clawbench 装上了但跑不起来（多半是平台包没装上）。请把上面的 npm 输出发出来"
fi
ok "clawbench $version"

# ---------- PATH ----------

step "把命令加进 PATH"
if [ -d "$NODE_DIR/bin" ]; then
  PATH_ENTRY="$NODE_DIR/bin:$NPM_PREFIX/bin"
else
  PATH_ENTRY="$NPM_PREFIX/bin"
fi

case "$(basename "${SHELL:-/bin/bash}")" in
  zsh) RC="$HOME/.zshrc" ;;
  bash)
    # Linux 的终端一般是「非登录交互 shell」，读 .bashrc；
    # macOS 的 Terminal 开的是登录 shell，读 .bash_profile。
    if [ "$OS" = darwin ]; then
      RC="$HOME/.bash_profile"
    else
      RC="$HOME/.bashrc"
    fi
    ;;
  *) RC="$HOME/.profile" ;;
esac

if [ -f "$RC" ] && grep -q '>>> clawbench installer >>>' "$RC" 2>/dev/null; then
  info "PATH 已经配置过（$RC）"
else
  {
    printf '\n# >>> clawbench installer >>>\n'
    printf '# ClawBench / Node.js 的安装目录（由 install.sh 添加）\n'
    printf 'export PATH="%s:$PATH"\n' "$PATH_ENTRY"
    printf '# <<< clawbench installer <<<\n'
  } >>"$RC"
  ok "已写入 $RC"
fi

# ---------- 收尾 ----------

cat <<EOF

$(printf '%sClawBench 装好了。%s' "$GREEN" "$RESET")

启动：
    ${BOLD}clawbench${RESET}

然后在浏览器打开：
    http://localhost:20000

首次启动会把随机密码打印在终端里，请保存好。

${DIM}当前终端还没加载新的 PATH，先执行下面这行（或者重开一个终端）：${RESET}
    export PATH="$PATH_ENTRY:\$PATH"

${DIM}AI 智能体不在这里装。启动后打开网页，欢迎界面里每个智能体都有「安装」按钮，
点一下就在这台机器上装好（同样走国内镜像）。${RESET}
EOF
