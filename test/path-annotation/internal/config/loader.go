//go:build exclude
// +build exclude

// This file is a TEST FIXTURE for verifying file path annotation behavior.
// The go:build exclude tag prevents Go toolchain from compiling this file.
// See README.md in this directory for details.

package config

import (
	_ "fmt"
	_ "net/http"
	_ "os"
)

// ─── 项目内相对路径（以文件所在目录为基准）───

// 同目录文件: test/path-annotation/internal/config/settings.json
var cfg = LoadConfig("settings.json")

// 同目录其他文件: test/path-annotation/internal/config/template.yaml
var tpl = ParseTemplate("template.yaml")

// 上级目录文件: test/path-annotation/internal/handler.go
var handler = Register("handler.go")

// 上两级目录文件: test/path-annotation/README.md
var readme = ReadFile("README.md")

// 跨子目录: test/path-annotation/internal/handler/routes.go
var routes = Parse("handler/routes.go")

// 带行号: test/path-annotation/internal/config/settings.json:10
var lineRef = "settings.json:10"

// ─── 项目内绝对路径 ───

// 项目内绝对路径: web/src/App.vue
var appVue = ReadFile("/home/xulongzhe/projects/clawbench/web/src/App.vue")

// ─── 项目外路径 ───

// 系统文件（橙色）: /etc/hosts
var hostsFile = "/etc/hosts"

// home 目录文件（橙色）: /home/xulongzhe/.bashrc
var bashrc = "/home/xulongzhe/.bashrc"

// 波浪号路径（橙色）: ~/.bashrc
var dotfile = "~/.bashrc"

// var 目录（橙色）: /var/log/syslog
var syslog = "/var/log/syslog"

// ─── 逸出项目根的相对路径 ───

// 逸出到上级（橙色）: /home/xulongzhe/projects/other-project/main.go
var externalRef = "../../other-project/main.go"

// ─── 不应标注的路径 ───

// 标准库包名 — 无扩展名裸单词，不应标注
import _ "fmt"
import _ "net/http"
import _ "os"
import _ "strings"

// 裸标识符，不应标注
var name = "config"
var mode = "handler"

// URL，不应标注
var url = "https://example.com/config"

// 环境变量，不应标注
var env = "$HOME/.bashrc"

// 通配符，不应标注
var glob = "internal/**/*.go"
