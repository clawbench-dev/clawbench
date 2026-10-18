//go:build exclude
// +build exclude

// This file is a TEST FIXTURE for verifying file path annotation behavior.
// The go:build exclude tag prevents Go toolchain from compiling this file.
// See README.md in this directory for details.

package main

import (
	_ "fmt"
	_ "net/http"
)

// ─── 项目根目录下的文件 ───

// 项目内相对路径: web/src/App.vue
var appVue = "web/src/App.vue"

// 项目内带 ./ 前缀: ./go.mod
var goMod = "./go.mod"

// 纯扩展名: README.md
var readme = "README.md"

// 绝对路径-项目内: /home/xulongzhe/projects/clawbench/web/src/App.vue
var absInternal = "/home/xulongzhe/projects/clawbench/web/src/App.vue"

// ─── 项目外路径（应标注为橙色）───

var (
	hostsFile = "/etc/hosts"
	bashrc    = "/home/xulongzhe/.bashrc"
	dotfile   = "~/.bashrc"
	syslog    = "/var/log/syslog"
	tmpFile   = "/tmp/test.log"
)

// ─── 逸出项目根 ───

var externalRef = "../../other-project/main.go"

// ─── 文件夹路径（无扩展名）───
// 注意：文件夹必须写成行内代码才进入标注（文本分支要求扩展名）。
// 以下为「行内代码形式」样例。

// 项目内文件夹（应标注，可导航进入）
var (
	dirComposables = "web/src/composables"
	dirChat        = "web/src/components/chat"
	dirStores      = "web/src/stores"
	dirRag         = "internal/rag"
	dirSpec        = "docs/spec"
	dirFixture     = "test/path-annotation"
	dirTwoSeg      = "web/src"
	dirTrailing    = "web/src/composables/"
)

// ─── 目录引用（尾部斜杠，文本形式也标注；2026-10-17 新增）───
//
// 真实回复里列目录几乎都写成「≥2 段 + 尾部 /」，此前文本形式完全点不动。
// 要求：至少两段 + 必须带尾部斜杠 + 右边界不是段字符。
// 以下路径均真实存在，校验后应保留标注（data-file-path 不含尾部斜杠）。

// 应标注（≥2 段 + 尾部斜杠）
var (
	dirRefComposables = "web/src/composables/"
	dirRefBackends    = "internal/ai/backends/"
	dirRefDesktop     = "desktop/src/main/"
	dirRefChat        = "web/src/components/chat/"
)

// 不应标注：单段目录带斜杠（「≥2 段」是硬性要求，用来挡散文）
var (
	dirRefOneSegInternal = "internal/"
	dirRefOneSegLogs     = "logs/"
	dirRefOneSegCoverage = "coverage/"
	dirRefOneSegScripts  = "scripts/"
)

// 不应标注：无尾部斜杠且无扩展名（文本分支不匹配）
var (
	dirNoSlashComposables = "web/src/composables"
	dirNoSlashRag         = "internal/rag"
	dirNoSlashSpec        = "docs/spec"
)

// 不应标注：单分隔符散文（不满足 ≥2 段，任何形式都不匹配）
var (
	proseAndOr   = "and/or"
	proseNA      = "N/A"
	proseAB      = "A/B"
	proseMBps    = "100MB/s"
	proseDateCtx = "日期 2024/05/12 生效"
)

// 不应标注：超长单段（段长上界 128，长串的尾巴不得被切出假目录）
var longSegNoDir = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bb/cc/"

// 字面 \n（未反转义的 JSON 字符串）：不应吞掉首字母，也不应跨转义串成一条路径。
// 文本分支与 <code>/围栏代码块分支（Step 2 让位给 Step 3）都已覆盖。
var literalBackslashN = "见 \\n/home/xulongzhe/projects/clawbench/docs/\\n 目录"

// 多条目代码块：每行一个路径，应拆成多个标注而非一条假路径。
var literalBackslashNMulti = "见 \\n/a/x.go\\n/b/y.go\\n"

// Windows 盘符路径的尾随转义应被裁掉（真路径是前缀）。
var literalBackslashNWin = "见 \\nE:/git/x/y.go\\n"

// 真实的反斜杠 Windows 路径（无转义）必须原样保留。
var literalBackslashWinReal = "见 E:\\git\\x\\y.go"

// ─── URL（链接化，非文件标注）───
//
// 带协议的由 marked 自动链接化；无协议域名由本项目 annotateBareUrls 补。
// 无协议域名须同时满足：① 后缀在 TLD 白名单；② 后面至少跟一段路径。

// 应链接化（无协议域名 + 至少一段路径）
var (
	urlBareGithub = "github.com/foo/bar"
	urlBareDeep   = "raw.githubusercontent.com/o/r/master/README.md"
	urlBareVue    = "vuejs.org/guide/intro.html"
	urlBareFrp    = "github.com/fatedier/frp"
)

// 应链接化（marked 处理，带协议 / www / 邮箱）
var (
	urlWithScheme = "https://example.com/a/b/"
	urlWww        = "www.example.com/a/b/"
	urlMailto     = "foo@example.com"
)

// 不应链接化：真实文件名/标识符（sh / app / com 既是 TLD 又是常见后缀）
// 「后面至少跟一段路径」这一条把它们全部挡掉。
var (
	notUrlBuildSh   = "build.sh"
	notUrlCfgFR     = "cfg.FR"
	notUrlIdentApp  = "useSessionIdentity.app"
	notUrlScriptSh  = "tts/compose.sh"
	notUrlScriptSh2 = "scripts/pre-push-checks.sh"
	notUrlGoFile    = "internal/ai/factory_test.go"
	notUrlVueFile   = "web/src/App.vue"
)

// 不应链接化：从 token 中间开始（前面是路径分隔符）
var notUrlMidToken = "src/github.com/foo/bar"

// 非 http(s) 协议不再被误标为文件（2026-10-17 已修）：<a href> 的文件判定原先
// 只跳过一个固定白名单，其它协议掉进去被当相对路径解析，渲染出 ftp:/... 死 chip。
// 以下应只是链接，没有死 chip。
var ftpNotAFile = "ftp://example.com/a/b"
var sshNotAFile = "ssh://host/path"

// 项目内绝对文件夹路径（应归一化为项目相对路径）
var (
	absDirComposables = "/home/xulongzhe/projects/clawbench/web/src/composables"
	absDirRag         = "/home/xulongzhe/projects/clawbench/internal/rag"
)

// 以本文件所在目录为基准的相对文件夹
var (
	relDirSibling = "../path-annotation"
	relDirParent  = "./.."
	relDirEscape  = "../.."
)

// 项目外文件夹（校验后标注保留，点击进入该目录；见 README）
var (
	extDirCodebuddy = "/home/xulongzhe/.codebuddy"
	extDirPlugins   = "/home/xulongzhe/.codebuddy/plugins"
	extDirLog       = "/var/log"
	extDirTilde     = "~/.codebuddy"
)

// 项目外目录跳转：点击应打开文件管理器并列出该目录（不弹「不支持」）。
// 列目录请求应打到 /api/projects（绝对路径），而非 /api/dir。
var (
	extDirPixmaps = "/usr/share/pixmaps"
	extDirEtc     = "/etc"
)

// ─── 项目外媒体（应渲染，改写为 /api/fs/raw/?target=<绝对路径>）───

// 历史缺陷：所有 "/" 开头的 src 都被当站点根 URL 放行 → 404 不显示。
var (
	extImage = "/usr/share/pixmaps/debian-logo.png"
	extSvg   = "/tmp/diagram.svg"
)

// ─── 文件系统根边界（「上一级」不应跳回项目内）───

// dirName("/tmp") === "/"，dirName("/") === "/"（根自身，无上级）
var (
	rootDir  = "/"
	rootTmp  = "/tmp"
	rootHome = "/home"
)

// 末尾含点号、易被误判为文件的文件夹
var (
	dotDirWorktree   = "/home/user/project/.worktrees"
	dotDirSuppressed = "/home/user/project/.worktrees/gitgraph-fix"
)

// ─── 文件夹 + 行号（无意义，不应这么写）───

var (
	dirWithLine  = "web/src/composables:10"
	dirWithRange = "internal/rag:1-5"
)

// ─── 不应标注 ───

var (
	pkg1 = "fmt"
	pkg2 = "net/http"
	pkg3 = "os"
	pkg4 = "strings"
	url  = "https://example.com"
	env  = "$HOME/.bashrc"
	glob = "src/**/*.go"
)

// ─── 标准库路径（有斜杠但项目外，校验后应移除）───

var (
	stdlib1 = "net/http"
	stdlib2 = "github.com/gin-gonic/gin"
)
