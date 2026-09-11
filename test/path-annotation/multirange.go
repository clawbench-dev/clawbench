//go:build exclude
// +build exclude

// This file is a TEST FIXTURE for verifying multi-range file path annotation.
// The go:build exclude tag prevents the Go toolchain from compiling it.
// See README.md and multirange.md in this directory for details.
//
// 覆盖：逗号分隔多区间、逗号后空格、L 前缀、乱序、去重合并、相邻合并、
// 倒序降级、超宽区间、反例（散文/裸数字/尾逗号/非正行号）、Windows 盘符。

package main

import (
	_ "fmt"
	_ "net/http"
)

// ─── 1. 基础：多区间（字符串字面量形式）───

// 原始需求示例
var storeSQLite = "internal/rag/store_sqlite.go:90-91,309,324,343,938-943"

// 三段区间
var appVue = "web/src/App.vue:879-885,1000,1200-1205"

// 多单行 + 一段
var annPaths = "web/src/composables/useFilePathAnnotation.ts:400,410-415,430"

// 绝对路径 + 多区间（外部，橙色）
var hostsFile = "/etc/hosts:1-2,4"

// 项目内绝对路径 + 多区间
var absInternal = "/home/xulongzhe/projects/clawbench/web/src/App.vue:879-885,1000"

// ─── 2. 解析宽容度（语义等价）───

// 逗号后空格
var spaced = "web/src/App.vue:879-885, 1000, 1200-1205"

// 乱序（应升序归一）
var unsorted = "web/src/App.vue:1000,879-885"

// L 前缀（大小写均可）
var lPrefixUpper = "web/src/App.vue:L879-L885,L1000"
var lPrefixMixed = "web/src/App.vue:l879-L885,L1000"

// ─── 3. 边界：应折叠为单区间（不产生 data-line-ranges）───

// 相邻单行 → 879-880
var adjacentLines = "web/src/App.vue:879,880"

// 相邻区间 → 879-882
var adjacentRanges = "web/src/App.vue:879-880,881-882"

// 重复去重 → 879
var duplicated = "web/src/App.vue:879,879,879"

// 倒序降级 → 单行 1200
var inverted = "web/src/App.vue:1200-879"

// 单区间但过宽：预览卡只高亮 200 行窗口内的部分
var tooWide = "web/src/composables/useFilePathAnnotation.ts:1-1000"

// ─── 4. 反例：不应误标注 / 不应吞正文 ───

// 逗号后是散文：只标注 879，正文原样保留
var proseAfterComma = "web/src/App.vue:879, and then the rest"

// 裸数字列表：不是路径，不标注
var bareNumbers = "1,2,3"

// 尾逗号：不匹配后缀（正则要求以数字结尾），整串当作路径名（既有行为）
var trailingComma = "web/src/App.vue:879-885,"

// 非正行号：路径保留但无行号（既有行为）
var zeroLine = "web/src/App.vue:0"

// ─── 5. Windows 盘符（盘符不应被当作行号后缀）───

// 行内代码形式可正确剥离后缀（见 multirange.md 的推荐写法）
var winForward = "C:/repo/src/main.go:10-11,20"
var winBackward = "E:\\git\\app\\src\\a.ts:10-11,20"

// ─── 6. 不应标注 ───

var (
	pkg1 = "fmt"
	pkg2 = "net/http"
	url  = "https://example.com/config"
	env  = "$HOME/.bashrc"
	glob = "internal/**/*.go"
)

// ─── 7. 与单区间/单行号的回归对照 ───

// 单行号：无 data-line-ranges
var singleLine = "web/src/App.vue:879"

// 单区间：无 data-line-ranges
var singleRange = "web/src/App.vue:879-885"

// 普通文本形式的多区间（Step 3 正则，需含 / 或扩展名）
// 见 internal/rag/store_sqlite.go:90-91,309 的上下文
