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

### 项目外文件夹（橙色，保留，可导航进入）

项目外**目录与文件一样保留标注**（`verifyFilePaths` 收到 `dir` 只标记
`data-path-type="dir"`，不再摘除）。点击后由文件管理器打开该目录——
绝对路径走 `/api/projects`（`/api/dir` 对项目外路径返回 400，前端按路径形态
选端点，见 `web/src/utils/dirList.ts`）。

注意文本分支同样要求扩展名，`/var/log` 这类无扩展名的外部目录只有
**行内代码**形式才会被标注：

- `/home/xulongzhe/.codebuddy` — 行内代码与文本形式都会标注（末段 `.codebuddy` 像扩展名）
- `/home/xulongzhe/.codebuddy/plugins`
- `/var/log` — 仅行内代码形式进入标注（无扩展名，文本正则不匹配）
- `~/.codebuddy` → 展开为 `/home/xulongzhe/.codebuddy`
- `/var/log/syslog` 的上级 `/var/log`（对照：`/var/log/syslog` 是文件）

> 历史行为（2026-09-20 已改）：项目外目录的标注曾被 `verifyFilePaths` 摘除，
> 点击时弹「仅支持项目内的路径跳转」。该文案（`file.toast.externalPathNotSupported`）
> 随之删除，en/zh 两份都已移除。项目外**文件**点击后仍会提示
> 「此文件位于项目目录之外」（`file.toast.externalFile`，信息性提示，不阻断打开）。

### 项目外路径的「上一级」与两个根

**Home 图标永远表示项目根**（`navigate('')`），与是否在项目外无关——走多深都有
一个确定出口。因此外部浏览时面包屑最左侧会多一个**文件系统根**图标（`HardDrive`），
点击回 `/`（Windows 为 `C:/`）。两个相邻的「根」用颜色区分：外部浏览时 Home 转橙色，
文件系统根保持中性色。

「上一级」按钮与 Home 的分工：**逐级向上 vs 一步回项目**。

手工验证：进入 `/var/log` → 连点「上一级」→ 应停在 `/`（不会跳回项目内）；
点 Home → 直接回项目根；点最左的文件系统根图标 → 回 `/`。

> 历史行为（2026-09-21 已改）：Home 曾按路径形态切换含义——项目内指项目根、
> 项目外指文件系统根，导致外部浏览时**没有任何回到项目的入口**（Back 一路向上
> 停在 `/`）。现改为 Home 恒为项目根，文件系统根另设图标。
>
> 另注：`navigateToParentDir` 曾有个错误守卫 `parent === ''`，而 `dirName('web') === ''`
> 正是项目内**单段**目录 → 「上一级」静默无动作，且状态机仍报「能返回」，用户卡死。
> 已修为只挡自指（文件系统根 `dirName('/') === '/'`）。

`/` 与项目根不同形：`toProjectRelative('/', root)` 特判返回 `'/'`（不做相对化），
否则会被归一成 `''` 而与项目根混淆。

### 项目外的视觉区分（四个面）

橙色是本项目既有的「项目外」约定色。以下四面都应有橙色标识（`ExternalBadge` 组件）：

| 面 | 位置 | 触发条件 |
|---|---|---|
| 文件浏览 | 面包屑栏右侧 chip（滚动区之外） | `currentDir` 为绝对路径 |
| 文件预览 | 文件名旁 chip | `file.path` 为绝对路径 |
| 目录浏览 | 同文件浏览 | 同上 |
| 目录预览 | 标题行 chip | `dirPath` 为绝对路径 |

验证：从聊天点开项目外路径 → 对应面出现橙色 chip；项目内文件/目录**不应**出现。

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

### 项目外目录跳转（点击进入该目录，不弹「不支持」）

以下路径点击后应打开**文件管理器并列出该目录内容**（面包屑为绝对路径，
「上一级」继续在文件系统内向上）。此前会弹「仅支持项目内的路径跳转」：

- `/var/log` — 行内代码形式（无扩展名，文本形式不标注）
- `/home/xulongzhe/.codebuddy`
- `/home/xulongzhe/.codebuddy/plugins`
- `/usr/share/pixmaps`
- `~/.codebuddy` — tilde 展开后同上

预期网络请求（可在浏览器 Network 面板核对）：列目录打到
`/api/projects?path=<绝对路径>`，**不是** `/api/dir`（后者对项目外路径返回 400）。

### 项目外媒体渲染（`/api/local-file/?path=` 绝对路径形式）

Markdown 里的图片/音频/视频若指向项目外真实文件，必须能渲染出来。
历史缺陷：所有以 `/` 开头的 `src` 都被当成「站点根 URL」原样放行，
浏览器于是向站点根请求而 404——图片**根本不显示**。

- `/usr/share/pixmaps/debian-logo.png` — 项目外图片，应正常显示
- `/tmp/diagram.svg` — 项目外 SVG（存在时），应正常显示
- `/etc/hosts` — 非媒体文件，不应被当图片渲染（对照）

预期：项目外媒体被改写为 `src="/api/local-file/?path=%2Fusr%2F…"`
（项目内则仍是稳定的 `/api/local-file/<项目相对路径>`）。
项目外图片**不带** `data-attach-src`——附加流程只认项目相对路径。
