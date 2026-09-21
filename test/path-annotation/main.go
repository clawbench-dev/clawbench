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

// ─── 项目外媒体（应渲染，改写为 /api/local-file/?path=<绝对路径>）───

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
