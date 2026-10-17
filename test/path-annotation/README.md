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
| Step 3（普通文本） | `FILE_PATH_RE`：需形如 `…/x.ext`，**或**「≥2 段 + 尾部 `/`」 | 带尾部 `/` **能**，否则不能 |

所以文件夹**写成行内代码一定有效**：

- `web/src/composables`
- `web/src/components/chat`
- `web/src/stores`
- `internal/rag`
- `docs/spec`
- `test/path-annotation`
- `web/src` — 只有两段也照样进入标注；主候选是相对本目录的
  `test/path-annotation/web/src`（不存在），靠回退候选 `web/src` 落到真实目录

**文本形式现在也能标注**（2026-10-17 新增目录分支）——但**必须带尾部斜杠**且
**至少两段**。真实回复里列目录几乎都写成这样（树形图尤其），此前完全点不动
（下列路径均真实存在，校验后应保留标注）：

- web/src/composables/
- internal/ai/backends/
- desktop/src/main/
- web/src/components/chat/

中文目录名同样支持（`FILE_PATH_RE` 的目录分支段字符集含 CJK；示例用本仓库
不存在的名字，仅为展示形态——校验后会因不存在而摘除，属预期）：

- 05_债务工程/ppt/gen_v7/

以下写法**不会**被标注（Step 3 的反例对照，无扩展名且无尾部斜杠）：

- web/src/composables
- internal/rag
- docs/spec

**单段目录带斜杠也不标注**（「≥2 段」是硬性要求，用来挡散文）：

- internal/
- logs/
- coverage/
- scripts/

下面这些是「单分隔符散文」的反例，**任何形式都不应标注**（不满足 ≥2 段）：

- and/or
- N/A
- A/B
- 100MB/s

数字日期也不应被截成目录（右边界检查，`2024/05/12` 不得产出 `2024/05/`）：

- 日期 2024/05/12 生效

以本文件所在目录（`test/path-annotation/`）为基准的相对文件夹：

- `../path-annotation` → 主候选 `test/path-annotation`，回退 `path-annotation`
- `../path-annotation/` → 同上（尾部斜杠形式）
- `./..` → `test`（纯 `..` 段，主/回退相同）
- `../..` → 逸出项目根，解析为 `null`，不标注

项目内绝对路径（归一化为项目相对路径，仍为蓝色）：

- `/home/xulongzhe/projects/clawbench/web/src/composables` → `web/src/composables`
- `/home/xulongzhe/projects/clawbench/internal/rag` → `internal/rag`

末尾带斜杠：

- `web/src/composables/` → 标注 `web/src/composables`（`data-file-path` 不含斜杠）
- `/home/xulongzhe/projects/clawbench/web/src/composables/` → 同上

名称含点号、易被误判为文件的文件夹（靠「后接 `/segment` 抑制」兜底）：

- 反例对照：`/home/user/project/.worktrees/gitgraph-fix` — `.worktrees` 会被当成
  「扩展名」前缀匹配，但后面还跟着 `/gitgraph-fix`，整段被抑制，**不标注**
- 反例对照：`/home/user/project/.worktrees` — 无后续 `/segment`，是合法文件夹，
  应标注（可导航进入）

**超长段不应被切出假目录**（段长有上界 128，超长串的尾巴不得被当路径）：

- 见 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/bb/cc/ 这段（单个 200 字符段 > 上界 128，**不标注**）

**字面 `\n`（未反转义的 JSON 字符串）不应吞掉首字母**——**仅普通文本分支**：

- 见 \n/home/xulongzhe/projects/clawbench/docs/\n 目录 — 文本形式应标注
  `/home/xulongzhe/projects/clawbench/docs/`，**不是** `n/home/…`

`<code>`／围栏代码块里的字面 `\n` 同样已修（2026-10-17）：Step 2 对含反斜杠的
内容**主动让位**给 Step 3 的扫描循环（它能在同一段文本里重新找到真路径），
所以下面这条现在也标注正确：

- `\n/home/xulongzhe/projects/clawbench/docs/\n` （行内代码形式）→ `/home/xulongzhe/projects/clawbench/docs/`

多条目代码块（每行一个路径）应拆成**多个**标注，而不是串成一条假路径：

- 见 `\n/a/x.go\n/b/y.go\n` — 应得到两条（`a/x.go`、`/b/y.go`）

Windows 盘符路径的尾随 `\n` 应被裁掉（真路径是前缀）：

- 见 `\nE:/git/x/y.go\n` — 应标注 `E:/git/x/y.go`

真实的反斜杠 Windows 路径（无转义）必须原样保留：

- 见 `E:\git\x\y.go` — 应标注 `E:/git/x/y.go`

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

**到达 `/` 时**（本轮修复）：面包屑**仍然可见**，最左的文件系统根图标带 `current`
样式且点击无动作，Home 仍是「返回项目根目录」的橙色出口；「上一级」按钮变为
**禁用**（`dirName("/") === "/"`，向上走是空操作），不再是一个点了没反应的假按钮。
Windows 的 `C:\` 一直正常（`splitPath` 会保留 `C:` 段），只有 POSIX `/` 会因
`splitPath("/")` 无段而命中这个缺陷。

> 历史行为（2026-09-21 已改）：到达 `/` 时面包屑整体**消失**（`v-if="parts.length > 0"`），
> 而「上一级」在 `/` 是空操作 → 用户**被困在文件系统根**，无法回到项目。
> 根因是 `splitPath("/")` 不产生任何段，于是 `parts` 为空把 Home 一起藏了。
>
> 同轮另修：Back 在 `/` 被**消费但无动作**（`canGoBackDir` 只看 `currentDir !== ''`，
> 而 `/` 满足该条件）→ 现在 `canGoBackDir` 与 `goBackDir` 用同一个「有父级」判据。

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

橙色是本项目既有的「项目外」约定色。以下四面都应有橙色标识（`ExternalBadge` 组件），
**标签固定为单个短词「外部」** + 图标（文件用 `FileText`、目录用 `FolderOpen`），
完整含义在 `title` 提示里：

| 面 | 位置 | 触发条件 |
|---|---|---|
| 文件浏览 | 面包屑栏右侧 chip（滚动区之外） | `currentDir` 为绝对路径 |
| 文件预览 | 文件名旁 chip | `file.path` 为绝对路径 |
| 目录浏览 | 同文件浏览 | 同上 |
| 目录预览 | 标题行 chip | `dirPath` 为绝对路径 |

验证：从聊天点开项目外路径 → 对应面出现橙色 chip，文字是**「外部」两个字**，
悬停显示「位于项目目录之外」；项目内文件/目录**不应**出现。

> 早期文案是「项目外目录 / 项目外文件」，在密集行里会把行内正文挤走；已改为
> 单短词 + 图标承担文件/目录区分（2026-09-21）。

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

### URL（链接化，非文件标注）

外链分成两类，**带协议的由 marked 自动处理，无协议的本项目补一步**。判据实测
（下表**故意不加反引号**——加反引号会渲染成行内代码而不是链接，Demo 要看的就是链接）：

| 写法 | 谁处理 | 结果 |
|---|---|---|
| https://example.com/a/b/ | marked（GFM autolink） | 链接 |
| www.example.com/a/b/ | marked | 链接 |
| ftp://example.com/a/b | marked | 链接（非 http(s) 协议不再被误标，见下） |
| foo@example.com | marked | 链接（mailto） |
| github.com/foo/bar | **本项目 `annotateBareUrls`** | 链接 |
| build.sh | 都不处理 | 纯文本 |

无协议域名必须**同时满足两条**才会被链接化，这两条正是用来区分 URL 与文件名：

1. **后缀在 TLD 白名单内**（`com`/`org`/`net`/`io`/`dev`/`cn`/`ai`/`app`/`sh`…）；
2. **后面至少跟一段路径**（`/xxx`）。

所以以下**不应**变成链接（它们是本仓库真实的文件名/标识符，`sh`、`app`、`com`
既是 TLD 又是常见后缀）：

- build.sh — 纯文本（无 `/`，Step 3 不标注）
- tts/compose.sh — **文件 chip**（有 `/` + 扩展名），不是链接
- scripts/pre-push-checks.sh — 文件 chip
- cfg.FR — 纯文本
- useSessionIdentity.app — 纯文本
- internal/ai/factory_test.go — 文件 chip
- web/src/App.vue — 文件 chip

以下**应**变成链接（可点击，浏览器模式下新标签打开）：

- github.com/foo/bar
- raw.githubusercontent.com/o/r/master/README.md
- vuejs.org/guide/intro.html
- github.com/fatedier/frp

各种容器里都应链接化。下面每一条都是**真实的**容器（不是把语法写进正文）：

列表里：

- 见 github.com/foo/bar 仓库

引用里：

> 见 github.com/foo/bar 仓库

加粗：**github.com/foo/bar**

括号：见 (github.com/foo/bar) 文档

句末：见 github.com/foo/bar。

表格里（见上方 URL 判据表最后一列）

行内代码里——**这是唯一同时得到链接与死 chip 的形态**，见下方遗留说明：

- `github.com/foo/bar`

反例：**不应从 token 中间开始匹配**（前面是路径分隔符时不链接）：

- src/github.com/foo/bar — 整体是路径 token，不链接

> 邮箱形式 `user@github.com/foo/bar` 由 **marked** 把 `user@github.com` 链成
> mailto，剩余 `/foo/bar` 是普通文本——这是 marked 的行为，不是本项目的裸域名规则：
>
> - user@github.com/foo/bar

**非 http(s) 协议不再被误标**（2026-10-17 已修）：`<a href>` 的文件判定原先只
跳过一个固定白名单（`https?:|//|mailto:|tel:|#`），其它协议会掉进去被当相对路径
解析——`ftp://` 里的 `/` 被当成路径分隔，于是渲染出死 chip `ftp:/example.com/a/b`。
现在改为「任何非 `file:` 的协议一律跳过」。以下都应**只是链接，没有死 chip**：

- ftp://example.com/a/b
- ssh://host/path

`file://` 仍正常（它是真的文件引用），Windows 盘符也不会被误当协议（单字母协议）：

- `file:///tmp/x/y.go` → 标注 `/tmp/x/y.go`
- `E:/git/x/y.go` → 标注 `E:/git/x/y.go`

> 行内代码／围栏代码块里的无协议域名仍会**同时**得到链接与一个死文件 chip
> （`<code class="chat-file-path" data-file-path="github.com/foo/bar">`）。死 chip 是
> Step 2 的 `looksLikeFilePath` 只看「含 `/`」所致，与本次两处修复无关：
>
> - `github.com/foo/bar` （行内代码形式）→ 链接 + 死 chip

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

### 项目外媒体渲染（`/api/fs/raw/?target=` 绝对路径形式）

Markdown 里的图片/音频/视频若指向项目外真实文件，必须能渲染出来。
历史缺陷：所有以 `/` 开头的 `src` 都被当成「站点根 URL」原样放行，
浏览器于是向站点根请求而 404——图片**根本不显示**。

- `/usr/share/pixmaps/debian-logo.png` — 项目外图片，应正常显示
- `/tmp/diagram.svg` — 项目外 SVG（存在时），应正常显示
- `/etc/hosts` — 非媒体文件，不应被当图片渲染（对照）

预期：项目外媒体被改写为 `src="/api/fs/raw/?target=%2Fusr%2F…"`
（项目内则仍是稳定的 `/api/fs/raw/<项目相对路径>`）。
项目外图片**不带** `data-attach-src`——附加流程只认项目相对路径。
