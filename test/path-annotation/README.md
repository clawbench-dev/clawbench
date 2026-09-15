# Path Annotation Test

This directory contains test fixtures for verifying file path annotation behavior
in the web frontend. These files are **not** meant to be compiled or executed.

- Go files have `//go:build exclude` tags to prevent compilation
- The vitest config excludes `test/path-annotation/**` from test discovery

**多区间行号专项夹具**：`multirange.md`（Markdown 预览）与 `multirange.go`
（代码视图/纯文本）覆盖逗号分隔多区间的全部场景（基础、宽容度、折叠边界、
反例、Windows 盘符、回归对照）。

## Markdown 标注测试

### 项目内路径（蓝色）

- `web/src/App.vue`
- `README.md`
- `go.mod`
- `./go.mod`
- `../README.md`

### 项目外路径（橙色）

- `/etc/hosts`
- `/home/xulongzhe/.bashrc`
- `~/.bashrc`
- `/var/log/syslog`

### 相对路径（以文件所在目录为基准）

本文件在 `test/path-annotation/` 目录下，所以：

- `README.md` → `test/path-annotation/README.md`
- `internal/config/settings.json` → 相对当前目录
- `../README.md` → 项目根 README.md

### 文件夹路径（无扩展名）

文件夹没有扩展名，两个分支的判据不同（下表结论均为实测）：

| 分支 | 判据 | 无扩展名的文件夹 |
|---|---|---|
| Step 2（行内代码） | `looksLikeFilePath`：含 `/` 即可 | **能**标注 |
| Step 3（普通文本） | `FILE_PATH_RE`：需形如 `…/x.ext` | **不能**标注 |

所以文件夹**必须写成行内代码**才有效：

- `web/src/composables`
- `web/src/components/chat`
- `web/src/stores`
- `internal/rag`
- `docs/spec`
- `test/path-annotation`
- `web/src` — 只有两段也照样进入标注；主候选是相对本目录的
  `test/path-annotation/web/src`（不存在），靠回退候选 `web/src` 落到真实目录

以下写法**不会**被标注（Step 3 的反例对照，无扩展名不匹配）：

- web/src/composables
- internal/rag
- docs/spec

以本文件所在目录（`test/path-annotation/`）为基准的相对文件夹：

- `../path-annotation` → 主候选 `test/path-annotation`，回退 `path-annotation`
- `./..` → `test`（纯 `..` 段，主/回退相同）
- `../..` → 逸出项目根，解析为 `null`，不标注

项目内绝对路径（归一化为项目相对路径，仍为蓝色）：

- `/home/xulongzhe/projects/clawbench/web/src/composables` → `web/src/composables`
- `/home/xulongzhe/projects/clawbench/internal/rag` → `internal/rag`

末尾带斜杠（既有行为：斜杠不被消费，`data-file-path` 落回去掉斜杠的部分）：

- `web/src/composables/` → 标注 `web/src/composables`

名称含点号、易被误判为文件的文件夹（靠「后接 `/segment` 抑制」兜底）：

- 反例对照：`/home/user/project/.worktrees/gitgraph-fix` — `.worktrees` 会被当成
  「扩展名」前缀匹配，但后面还跟着 `/gitgraph-fix`，整段被抑制，**不标注**
- 反例对照：`/home/user/project/.worktrees` — 无后续 `/segment`，是合法文件夹，
  应标注（可导航进入）

### 项目外文件夹（橙色，但校验后会被撤销）

只有**文件**才保留项目外标注；项目外**目录**在 `verifyFilePaths` 收到
`dir` 后会被摘掉标注（`openFilePath` 点击时也会弹「不支持外部路径」）。
另注意文本分支同样要求扩展名，`/var/log` 这类无扩展名的外部目录只有
**行内代码**形式才会先被标注、再被校验撤销：

- `/home/xulongzhe/.codebuddy` — 行内代码标注后撤销（末段 `.codebuddy` 像扩展名，文本形式也会先标注）
- `/home/xulongzhe/.codebuddy/plugins`
- `/var/log` — 仅行内代码形式进入标注，随后撤销
- `~/.codebuddy` → 展开为 `/home/xulongzhe/.codebuddy`
- `/var/log/syslog` 的上级 `/var/log`（对照：`/var/log/syslog` 是文件，保留橙色）

### 文件夹 + 行号（无意义，不应出现）

行号后缀对目录不生效，以下写法即便解析出行号也不会跳转高亮，
仅作「不该这么写」的记录（实测仍会标注为文件夹）：

- `web/src/composables:10` — 目录带行号，跳转时仍按目录处理
- `internal/rag:1-5`

### 不应标注

- fmt
- net/http
- https://example.com
- $HOME/.bashrc
- src/**/*.go

### 带行号的路径（点击跳转并闪烁高亮对应行）

- `web/src/App.vue:879` — 单行号，跳转并闪烁第 879 行
- `go.mod:1` — 项目根 go.mod 第 1 行
- `README.md:5` — 相对路径 + 行号
- `/etc/hosts:3` — 绝对路径 + 行号
- `~/.bashrc:10` — tilde 路径 + 行号

### 带行号范围的路径（点击跳转并闪烁高亮对应行范围）

- `web/src/App.vue:879-885` — 行号范围，跳转并闪烁第 879–885 行
- `go.mod:1-5` — 项目根 go.mod 第 1–5 行
- `README.md:1-3` — 相对路径 + 行号范围
- `/etc/hosts:1-5` — 绝对路径 + 行号范围
- `~/.bashrc:5-10` — tilde 路径 + 行号范围

### 多区间行号（逗号分隔，点击跳到最早区间并闪烁全部指定行）

- `web/src/App.vue:879-885,1000,1200-1205` — 跳转并闪烁 879–885、1000、1200–1205
- `web/src/composables/useFilePathAnnotation.ts:400,410-415,430` — 乱序也支持：`430,400,410-415`
- `go.mod:1,3,5` — 项目根 go.mod 多行
- `/etc/hosts:1-2,4` — 绝对路径 + 多区间
- `~/.bashrc:1,5-6` — tilde 路径 + 多区间
- `web/src/App.vue:L879-L885,L1000` — 带 L 前缀的多区间
- `web/src/App.vue:879-885, 1000, 1200-1205` — 逗号后允许空格
- `web/src/App.vue:1-1000` — 区间过宽，预览卡只高亮窗口内（约 200 行）的部分
