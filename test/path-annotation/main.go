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

var hostsFile = "/etc/hosts"
var bashrc = "/home/xulongzhe/.bashrc"
var dotfile = "~/.bashrc"
var syslog = "/var/log/syslog"
var tmpFile = "/tmp/test.log"

// ─── 逸出项目根 ───

var externalRef = "../../other-project/main.go"

// ─── 不应标注 ───

var pkg1 = "fmt"
var pkg2 = "net/http"
var pkg3 = "os"
var pkg4 = "strings"
var url = "https://example.com"
var env = "$HOME/.bashrc"
var glob = "src/**/*.go"

// ─── 标准库路径（有斜杠但项目外，校验后应移除）───

var stdlib1 = "net/http"
var stdlib2 = "github.com/gin-gonic/gin"
