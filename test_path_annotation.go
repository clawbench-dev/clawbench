//go:build manualtest

package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

// ── 应该被标注的路径（含 / 或有扩展名） ──

// 绝对路径
// /etc/hosts
// /home/user/.bashrc

// 项目内相对路径
// src/main.go
// web/src/App.vue
// internal/config/settings.json
// ./cmd/server/main.go
// ../config/database.yml

// 含行号的路径
// src/main.go:42
// web/src/App.vue:10-25
// internal/handler.go:100

// 有扩展名无斜杠（纯单词+扩展名）
// README.md
// Makefile.bak
// go.mod
// go.sum
// package.json
// Dockerfile.dev

// ── 不应该被标注的路径（裸单词无扩展名） ──

// 标准库包名
// fmt
// os
// strings
// net/http（这个有斜杠，但标准库路径不存在于项目中）

// 变量名/函数名
// main
// handler
// httpClient

// ── 字符串字面量中的路径 ──

var (
	configPath  = "config/app.yaml"
	templateDir = "./templates/"
	outputPath  = "../build/output.bin"
	logFile     = "/var/log/app.log"
	readmeFile  = "README.md"
	packageName = "fmt"                 // 不应标注
	anotherPkg  = "net/http"            // 有斜杠，会被乐观标注但校验后移除
	justAWord   = "strings"             // 不应标注
	anExtension = "go.mod"              // 应标注
	urlStr      = "https://example.com" // URL，不应标注
	envPath     = "$HOME/.bashrc"       // 环境变量，不应标注
	globPattern = "src/**/*.go"         // 通配符，不应标注
	templateVar = "<placeholder>"       // 尖括号，不应标注
)

func main() {
	fmt.Println("config path:", configPath)
	fmt.Println("readme:", readmeFile)
	fmt.Println("package:", packageName)

	// 路径在字符串中
	fmt.Println("see web/src/App.vue for details")
	fmt.Println("check internal/config/settings.json:50")
	fmt.Println("refer to README.md")
	fmt.Println("import fmt")      // 不应标注 fmt
	fmt.Println("import net/http") // 乐观标注后移除
	fmt.Println("see go.mod")      // 应标注
	fmt.Println("open main.go:30") // 应标注

	// 多路径在一行
	fmt.Println("compare web/src/App.vue and web/src/main.ts")

	// URL 不标注
	fmt.Println("visit https://github.com/golang/go")

	// 环境变量路径不标注
	fmt.Println("config at $HOME/.config/app")

	// 通配符不标注
	fmt.Println("all files in src/**/*.go")
}
