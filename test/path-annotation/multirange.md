# 多区间行号标注测试（Markdown 夹具）

本文件用于人工验证**逗号分隔多区间行号**的路径标注。点击任意路径应：

1. 跳转到**最早**区间（min start）；
2. 闪烁**全部**指定行（源码模式 CodeMirror）；
3. 悬停预览卡高亮**窗口内**的全部区间；
4. 预览卡标题 / 复制路径输出完整区间串。

本文件在 `test/path-annotation/` 下，相对路径以本目录为基准解析。

---

## 1. 基础（应全部正确标注）

行内代码形式（推荐，最稳）：

- `internal/rag/store_sqlite.go:90-91,309,324,343,938-943` — 原始需求示例
- `web/src/App.vue:879-885,1000,1200-1205` — 三段区间
- `web/src/composables/useFilePathAnnotation.ts:400,410-415,430` — 多单行 + 一段

普通文本形式（Step 3 正则，需含 `/` 或扩展名）：

- 见 internal/rag/store_sqlite.go:90-91,309 的上下文
- 见 web/src/App.vue:879-885,1000 的上下文

绝对路径 / 外部路径（橙色）：

- `/etc/hosts:1-2,4`
- `/var/log/syslog:1,3-4`

项目内绝对路径：

- `/home/xulongzhe/projects/clawbench/web/src/App.vue:879-885,1000`

波浪号路径（需 homeDir；`~` 形式建议用行内代码）：

- `~/.bashrc:1,5-6`

## 2. 解析宽容度（等价输入）

以下四条**语义等价**，都应归一化为 `data-line-ranges="879-885,1000"`：

- `web/src/App.vue:879-885,1000`
- `web/src/App.vue:879-885, 1000` — 逗号后空格
- `web/src/App.vue:1000,879-885` — 乱序（应升序归一）
- `web/src/App.vue:L879-L885,L1000` — `L` 前缀（大小写均可）

大小写混合前缀：

- `web/src/App.vue:l879-L885,L1000`

## 3. 边界：应折叠为单区间（不产生 data-line-ranges）

这些输入在解析阶段被合并/降级，最终只有一个区间，因此不带 `data-line-ranges` 属性：

- `web/src/App.vue:879,880` — 相邻单行合并为 `879-880`
- `web/src/App.vue:879-880,881-882` — 相邻区间合并为 `879-882`
- `web/src/App.vue:879,879,879` — 重复去重为 `879`
- `web/src/App.vue:1200-879` — 倒序降级为单行 `1200`
- `web/src/composables/useFilePathAnnotation.ts:1-1000` — 单区间但过宽，
  预览卡只高亮 200 行窗口内的部分，跳转仍到第 1 行

## 4. 反例（不应被误标注 / 不应吞掉正文）

- `web/src/App.vue:879, and then the rest` — 逗号后是散文，只标注 `879`，
  `, and then the rest` 原样保留
- `1,2,3` — 裸数字列表，不是路径，不标注
- `web/src/App.vue:879-885,` — 尾逗号**不**匹配后缀（正则要求以数字结尾），
  整串被当作路径名（既有行为，非多区间特性）
- `web/src/App.vue:0` — 非正行号，路径保留但无行号（既有行为）

Windows 盘符（盘符不应被当成行号后缀）：

- 行内代码：`C:/repo/src/main.go:10-11,20`
- 行内代码：`E:\git\app\src\a.ts:10-11,20`
- 普通文本：C:/repo/src/main.go:10-11,20 — 注意：普通文本形式的
  Windows 后缀剥离是**既有行为**，此处仅作对照记录

## 5. 混合/组合

- 同一行多处：`web/src/App.vue:879-885,1000` 与 `go.mod:1,3,5` 同段出现
- 链接形式：见 [多区间链接](web/src/App.vue:879-885,1000)
- 长列表（8 段）：`web/src/App.vue:10,20,30,40-42,50,60,70-72,80`

## 6. 与单区间/单行号的回归对照

- `web/src/App.vue:879` — 单行，无 data-line-ranges
- `web/src/App.vue:879-885` — 单区间，无 data-line-ranges
- `go.mod:1-5` — 单区间
- `README.md:1-3,5` — 多区间

## 7. 项目外目录（点击进入该目录，保留橙色标注）

无扩展名的外部目录需**行内代码**形式（文本正则要求扩展名）：

- `/var/log`
- `/home/xulongzhe/.codebuddy`
- `/usr/share/pixmaps`
- `~/.codebuddy`

点击后应打开文件管理器列出该目录（走 `/api/projects`），而不是弹
「仅支持项目内的路径跳转」。项目外目录的标注**不再**被校验撤销。

## 8. 项目外媒体渲染（改写为 `/api/fs/raw/?target=<绝对路径>`）

- `/usr/share/pixmaps/debian-logo.png` — 项目外图片，应正常显示（历史缺陷：404 不显示）
- `/tmp/diagram.svg` — 项目外 SVG（存在时）

## 9. 文件系统根边界（「上一级」不应跳回项目内）

进入 `/var/log` 后连点「上一级」：`/var/log` → `/var` → `/`，
到 `/` 后按钮应无动作（不跳回项目根）。
