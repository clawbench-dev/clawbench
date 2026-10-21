# docs/ 目录 Markdown 逐份审计报告

> 生成方式：机械抽取（链接/内联代码路径/符号 → 仓库实际文件与导出符号比对）＋ 子代理逐份通读。
> 覆盖范围：`docs/` 下全部 205 份 `.md`，无跳过。
>
> **状态（2026-10-21 归档）**：本报告为一次性调查快照，其中 P0 第一条（`database.md`）**已修复**——该文档已按全新安装实测的 36 张表重写。其余条目未处理，按原样留存。
>
> **已知勘误**：1.3 节 P0 第一条把 `ai_raw_responses` / `leaves` / `project_meta` 列为「真实存在的表」，**这三张实际都不存在**——`ai_raw_responses` 是已被 DROP 迁移移除的旧表（`database.go:1620`），`project_meta` 已被并入 `projects`（`projects_migrate.go:133`），`leaves` 是 `grep` 在注释里误命中的英文单词「leaves」（`database.go:232/1115`）。真实数字是 **36 张表**，文档当时覆盖 19 张 + 1 张幽灵，即漏记 **17** 张。

## 一、全局结论

### 1.1 文档构成

| 类别 | 目录 | 份数 |
|---|---|---|
| 其他 | `docs/Skills/clawbench-screenshot/` | 1 |
| 其他 | `docs/Skills/clawbench-user-guide/` | 1 |
| 开发探针记录 | `docs/dev/` | 21（本报告归档后为 22） |
| 实施计划（历史） | `docs/plans/` | 57 |
| 规格（权威） | `docs/spec/` | 3 |
| 规格（权威） | `docs/spec/api/` | 1 |
| 规格（权威） | `docs/spec/client/` | 7 |
| 规格（权威） | `docs/spec/core/` | 5 |
| 规格（权威） | `docs/spec/features/` | 22 |
| 规格（权威） | `docs/spec/infra/` | 13 |
| 实施计划（历史） | `docs/superpowers/plans/` | 17 |
| 实施计划（历史） | `docs/superpowers/specs/` | 19 |
| 其他 | `docs/timer/` | 9 |
| 用户手册（面向使用者） | `docs/user-guide/` | 29 |
| **合计** | | **205** |

### 1.2 机器层失效引用统计

- 失效引用总数：**260** 处，分布在 **56** 份文档中（占 27%）。
- 其余 **149** 份文档的链接/路径/符号引用全部可解析。

### 1.3 真正需要修的问题（按严重度）

**P0 — 规格与实现直接矛盾（会误导实现者）**

以下每条都经本机复核（给出验证命令）。

- ~~**`docs/spec/database.md`**~~ **[已修复 2026-10-21]** — 原问题：文档化了代码中**根本不存在的** `agent_api_keys` 表（列定义 / `agents.id` CASCADE 外键 / ER 图节点三处），同时漏记大量真实存在的表。修复方式：按 `InitDB()` 后 `sqlite_master` 实测重写全文（36 张表 + RAG 三表 + 四条真实 DB 外键 + 完整索引清单），并单列 `agent_api_keys` 的历史遗留说明。
  - 原建议「按 `CREATE TABLE IF NOT EXISTS` 的实际集合补齐 19 张表」**数字有误**（见页首勘误）：真实应为 36 张，其中 `ai_raw_responses` / `leaves` / `project_meta` 三张并不存在。
- **`docs/spec/infra/auth-and-middleware.md`** — 写 `POST /api/settings/password`，实际端点是 **`POST /api/config/password`**
  - 建议：改为 `/api/config/password`
  - 证据：`grep -n 'register("/api/config/password"' internal/handler/handler.go` → 476；`/api/settings/password` 在代码中不存在
- **`docs/spec/README.md`** — 「16 个后端规格」实际 **18**；「176 路径 / 224 操作」实际 **178 / 226**；索引表把 `agent_api_keys` 列为现存表，与同文件第 55/104 行「已移除」自相矛盾
  - 建议：同步计数为 18 / 178 / 226，并删除索引表中的 `agent_api_keys` 行
  - 证据：`internal/model/discovery_test.go:33` 的 `expectedIDs` 共 18 项；`grep -c "^  /" internal/api/openapi.yaml` → 178；`grep -cE "^    (get|post|put|patch|delete):"` → 226
- **`docs/spec/api/README.md`** — 第 7 行与第 75 行的「176 路径 / 224 操作」实际为 **178 / 226**。文档自己给出的复核命令（`grep -c "^  /"` 等）即可发现该漂移，说明计数未随规格更新
  - 建议：同步计数
  - 证据：同上两条 grep → 178 / 226
- **`docs/spec/features/setup-wizard.md`** — 「显示所有 **15** 个注册后端」（第 61 行）与「**16** 个后端规格」（第 11、24 行）实际均为 **18**
  - 建议：改为 18
  - 证据：`internal/model/discovery_test.go:33` `expectedIDs` 18 项 + `assert.Len(GetBackendRegistry(), len(expectedIDs))`；`GET /api/backends` 直接序列化该注册表
- **`docs/spec/.index.md`** — 「12 个 Java 模块」实际 **49** 个 `.java` 文件；「JS Bridge 25+ 方法」实际 `@JavascriptInterface` **79** 处（原述下限虽仍为真，但已明显偏小）
  - 建议：改为「49 个 Java 文件」，Bridge 方法数改为「79 个」
  - 证据：`find android/app/src/main/java -name "*.java" | wc -l` → 49；`grep -rho "@JavascriptInterface" android/app/src/main/java | wc -l` → 79
- **`docs/spec/features/push-notifications.md`** — 引用 `internal/push/dingtalk/session_command.go`，该文件不存在，实际在 `internal/push/common/session_command.go`
  - 建议：改路径
  - 证据：`ls internal/push/dingtalk/` 无该文件
- **`docs/user-guide/getting-started.md`** — 「Dock 的**九个**页签」并给出 9 行表格，实际 `DOCK_TABS` 有 **10** 个页签——表格**缺「通知中心」**（`notifications`，位于 `proxy` 与 `settings` 之间）
  - 建议：改为「十个页签」并在表格中补上「通知中心」行（位置 9，设置前移为 10）
  - 证据：`web/src/composables/dockTabs.ts` 的 `DOCK_TABS` 数组 10 项：`browse/view/history/forge/tasks/terminal/stats/proxy/notifications/settings`
- **`docs/user-guide/getting-started.md`** — 宽屏阈值写成 **1024px**（L61「视口宽度 ≥ 1024px」三栏布局、L91「窄屏 < 1024px」），实际阈值是 **960px**
  - 建议：两处 1024 改为 960
  - 证据：`web/src/composables/useWideScreenLayout.ts:27` `export const WIDE_SCREEN_MIN_WIDTH = 960`（`:58` 注释与 `:133` 的 `matchMedia('(min-width: 960px)')` 一致）；同仓库 `docs/spec/client/design-guide.md:231` 也写「≥960px」，可作为口径统一依据
- **`docs/user-guide/settings.md`** — 「设置首页按两列列出 **18** 个分类」实际 i18n `settings.categories` 有 **20** 个 key（新增 `mcp`、`usageMeter`；`内网穿透` 已移除）
  - 建议：改为 20，并在下文补 `mcp`（MCP 服务）与 `usageMeter`（用量）两类说明
  - 证据：`web/src/i18n/locales/zh.ts` 的 `settings.categories` 下 20 个一级 key

**P1 — 措辞/路径笔误（低风险，但会误导读者）**

- **`docs/spec/client/android-integration.md`** — 措辞暗示 `webViewRef` 的 WeakReference 修复**仍未做**，但代码早已实现
  - 建议：改为「已修复」并给出行号
  - 证据：`android/app/src/main/java/com/clawbench/app/BackgroundService.java:854` 已有 `webViewRef = new WeakReference<>(activity.webView)`
- **`docs/Skills/clawbench-user-guide/SKILL.md`** — 引用 `openfile.sh` / `annot.sh` / `termqc.sh` 三个脚本，已不存在
  - 建议：删除脚本引用，或补回脚本
  - 证据：三个脚本在仓库中均无匹配
- **`docs/Skills/clawbench-user-guide/SKILL.md`** — `data-path` 的内部语义与 `docs/Skills/clawbench-screenshot/SKILL.md:789` 矛盾；`file_share.go` 实际在 `internal/handler/file_share.go`
  - 建议：统一两份 Skill 的 `data-path` 约定；补全 handler 路径
- **`docs/user-guide/annotation.md`** — 引用 `web/src/app.ts`，实际在 `web/src/stores/app.ts`
  - 建议：改路径
  - 证据：`web/src/app.ts` 不存在
- **`docs/user-guide/FAQ.md`** — `config/config.example.yaml` 链接指向不存在的文件（仓库中**完全没有** `config/` 目录，也没有任何 `config.example.yaml`）
  - 建议：改为仓库实际存在的配置文件路径或删链接
  - 证据：`ls config/` → No such file or directory；`find . -name "config.example.yaml"` → 无输出。真实约定见 `internal/platform/config_path.go`（`<DataDir>/config/config.yaml` 优先，其次 CWD 下的 `config/config.yaml`）
  - 建议改法：改为「如需自定义，创建 `config/config.yaml`（字段说明见 `docs/spec/infra/config-and-discovery.md`）」，或把示例文件补进仓库再引用
- **`CLAUDE.md`（项目根，非 docs/ 但同源）** — 第 7 行引用 `docs/user-guid/user-guid.md`（`user-guid` 单数），实际目录是 `docs/user-guide/`（复数），该路径**从未存在过**
  - 建议：改为 `docs/user-guide/`
  - 证据：`ls docs/` 无 `user-guid` 目录

**P2 — 计划类文档引用尚未落地的文件（属预期，不建议逐个改）**

`docs/plans/` 与 `docs/superpowers/` 记录的是**实施当时的设想**，其中指向尚未创建或最终改了名的文件属正常。
本报告仍逐份列出，但建议的处理方式是「整份归档 + 顶部加状态横幅」，而不是逐条改路径。

### 1.4 文档体系健康度评估

- **规格层（`docs/spec/`）质量高**：大量显式的「已移除」「刻意不新增」声明，并有漂移守卫测试（`internal/handler/openapi_drift_test.go`、`internal/model` 后端注册表计数测试）兜底。
  残余问题集中在**数字**上——端点数、后端数、表数、模块数。这类数字一旦被单独抄进文档就会漂移。
  **建议**：把「后端数」「OpenAPI 路径/操作数」「DB 表数」这类数字改为**从代码实时读取**的表述，或直接删掉数字只留「全部」措辞。
- **用户手册层（`docs/user-guide/`）质量高**：面向使用者，代码符号引用极少，失效引用集中在少数几个链接。
- **探针记录层（`docs/dev/`）**：记录的是外部 CLI 的实测协议，路径多指向被测工具自身的 `dist/`，不算失效引用。
- **计划层（`docs/plans/`、`docs/superpowers/`）**：过时引用最多，但**基本无害**——它们是历史快照。

## 二、逐份检查

每份文档一节，含：① 讲什么 ② 失效引用（机器判定 + 子代理通读） ③ 建议怎么改。

### 规格层 `docs/spec/`（权威，现行有效）

---

#### `docs/spec/.index.md`

- **行数**：67 ｜ **类别**：规格（权威）
- **标题**：ClawBench 系统设计规格

**① 这份文档讲什么**

README 的精简版索引（core/features/infra/client 四组的一句话摘要）。

主要章节：目录结构、#core/ — 核心业务、#features/ — 功能特性、#infra/ — 基础设施、#client/ — 客户端

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - 「12 个 Java 模块」实际 **49** 个 `.java` 文件；「JS Bridge 25+ 方法」实际 `@JavascriptInterface` **79** 处。
  - 验证：`find android/app/src/main/java -name "*.java" | wc -l` → 49；`grep -rho "@JavascriptInterface" android/app/src/main/java | wc -l` → 79。

**③ 建议怎么改**

- 按上节「过时线索」逐条改：路径改成实际位置、符号改成实际名称；若该目标确实从未落地，改写为「曾计划但未实现」的显式声明。

---

#### `docs/spec/README.md`

- **行数**：106 ｜ **类别**：规格（权威）
- **标题**：ClawBench 系统设计规格

**① 这份文档讲什么**

系统规格总入口——项目定位、模块地图、全部子文档的一句话索引表与维护规则。

主要章节：模块地图、#数据模型、#core/ — 核心业务、#features/ — 功能特性、#infra/ — 基础设施、#api/ — API 规格、#client/ — 客户端、参考资料

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - 「16 个后端规格」→ 实际 18 个。验证：`grep -c . internal/model/discovery_test.go` 的 `expectedIDs` 列出 18 个 id（claude…commandcode）。
  - 「176 路径 / 224 操作」→ 实际 178 / 226。验证：`grep -c "^  /" internal/api/openapi.yaml` = 178；`grep -cE "^    (get|post|put|patch|delete):" internal/api/openapi.yaml` = 226。
  - 索引表把 `agent_api_keys` 列为现存的表，而同文件第 55/104 行又说该表「已移除」——自相矛盾，且代码中确无此表（`grep -rn "agent_api_keys" internal/ --include=*.go` 无输出）。

**③ 建议怎么改**

- 按上节「过时线索」逐条改：路径改成实际位置、符号改成实际名称；若该目标确实从未落地，改写为「曾计划但未实现」的显式声明。

---

#### `docs/spec/api/README.md`

- **行数**：78 ｜ **类别**：规格（权威）
- **标题**：API 文档

**① 这份文档讲什么**

OpenAPI 规格的位置迁移说明、鉴权/错误体约定、不可建模端点清单、内置斜杠命令的 operationId 注入表与漂移守卫。

主要章节：使用方式、约定、不建模的部分、Tag 分组、与实现的同步、内置斜杠命令的接口注入、维护

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - 「覆盖全部 176 个路径 / 224 个操作」（第 7 行）与第 75 行「2026-10-20 实测 176 / 224」→ 实际 178 / 226。验证：同上两条 grep（178 / 226）。文档自己给出的复核命令即已能发现该漂移，说明计数未随规格更新。

**③ 建议怎么改**

- 按上节「过时线索」逐条改：路径改成实际位置、符号改成实际名称；若该目标确实从未落地，改写为「曾计划但未实现」的显式声明。

---

#### `docs/spec/client/android-integration.md`

- **行数**：177 ｜ **类别**：规格（权威）
- **标题**：Android 集成

**① 这份文档讲什么**

描述 Android 端的集成形态：WebView 承载 Vue App + JS Bridge 双向通道、原生侧模块清单（MainActivity / ClawBenchWebView / 悬浮窗 / 后台服务 / 开机广播 / 日志回传）、APK 下发与 `go:embed` 链路、主题与 i18n 对齐、原生与前端的契约边界。

**② 有没有引用了已经不存在的文件或函数**

- **L12 / L90 / L107 / L113：桥名 `AndroidNative` 已不存在。** 真实名字是 `ClawBenchNative`（`android/app/src/main/java/com/clawbench/app/MainActivity.java:693` → `webView.addJavascriptInterface(new WebAppInterface(this), "ClawBenchNative")`）。L12 的 mermaid 节点还叫 `AndroidBridge`，L113 的小节标题也写作「JS Bridge 关键方法（`AndroidNative`）」。
  - **L110：「108 key 对齐」→ 实际 127。** `android/app/src/main/res/values/strings.xml` 与 `values-zh/strings.xml` 各 127 个 `<string name=`，两侧 diff 为空。
  - **L110：「`MainActivity` 21 处硬编码中文」「`BackgroundService` 16 处通知文字」→ 已无对应物。** 这两个文件现在残留的中文字符串字面量为 0（grep 命中的 4 + 5 处全部在注释里），`R.string.` 引用分别有 66 / 48 处。
  - 已核对为真：L113 之后列出的全部 bridge 方法（`dismissSplash`/`connectToServer`/`getServerList`/`setTheme`/`applyThemeColors`/`postEventNotificationFromWorker`/`dispatchSelectionAction` 等）、L113 表格里全部 8 个端点（`/api/apk` 公开、`/api/health` 公开、`/api/client-log` 鉴权、`/api/ssh/info` 公开、`/api/ai/events/pending`、`/api/ai/sessions/overview`、`/api/desktop/latest` 公开）、Java 端 21 个类（`FloatingStatusController` / `LiveUpdateManager` / `ThemePalette` / `NativeNotificationPolicy` 等）全部存在。

**③ 建议怎么改**

把 4 处 `AndroidNative` 全部改成 `ClawBenchNative`，mermaid 节点 `AndroidBridge` 一并改掉；L110 的 `108` 改为 `127`（可补「两侧 `diff` 为空」）；「21 处 / 16 处」改成不含计数的表述（"登录/连接错误、SSL 对话框、文件选择器、splash 与通知文案已全部抽到 `R.string`，现无残留硬编码中文"），或直接删掉计数。

---

#### `docs/spec/client/design-guide.md`

- **行数**：576 ｜ **类别**：规格（权威）
- **标题**：视觉设计指导手册

**① 这份文档讲什么**

视觉设计指导手册：三层样式归属（全局 CSS / 组件局部 / 内联）、设计 token 全表（字号/间距/圆角/层级/时长/透明度/阴影）、36 套主题机制与新增主题的标准步骤、主题内已知的重复色与对比度问题、按钮/弹窗/抽屉/加载环的组件语言、七条红线、守卫测试索引。

**② 有没有引用了已经不存在的文件或函数**

（文件均存在，主题数与新增步骤**仍然成立**；失效的是行号与计数）：
  - **L38：`wide-screen.css` 的「≥1024px」是错的 → 实际 ≥960px**（`web/css/wide-screen.css:2` 写 "≥960px CSS viewport"，`web/src/composables/useWideScreenLayout.ts:27` `WIDE_SCREEN_MIN_WIDTH = 960`）。本文 L231 自己也写 960px，**自相矛盾**。
  - **L154：「每套自包含约 40 个颜色变量」→ 实际 53 个**（`web/css/variables.css` 每个 `[data-theme="..."]` 块）。
  - **行号漂移**：`base.css:32`（L77，基准字号）→ 实际 `base.css:37`；`base.css:39`（L197，`.wallpaper-layer`）→ 实际 `base.css:80`（`z-index:0` 在 `:83`）；`base.css:99`（L198，那条 `z-index:1`）→ 实际 `base.css:128`；`designTokens.css.test.ts:156`（L130，两条 z 层级不变量）→ 实际 `:168` 与 `:179`；`:176`（L147，三档阴影）→ 实际 `:202`；`:252`（L177，拒绝静态 token 进主题块）→ 实际 `:278`；`:408`（L435，钉「配方」而非十六进制）→ 实际 `:448-454`（`toContain('color-mix(')`）。
  - **计数漂移**：L334 图标尺寸「14（311 处）、16（144）、12（114）、13（66）」→ 实际 **328 / 154 / 131 / 84**；L357 `<LoadingIndicator>`「覆盖 72 个文件」→ 实际 **77**；L370 `RefreshButton`「19 处已收敛」→ 实际 **22** 个文件。
  - **L369 关键帧名不精确**：`modal-fadeIn/scaleIn` → 实际是两个 `modal-` 前缀的名字：`modal-fadeIn` 与 `modal-scaleIn`（`web/src/assets/modal-card.css:205,215`）。其余 `refresh-spin`(0.8s) / `check-in`(0.4s) / `bs-slideUp`/`bs-slideDown` / `line-flash` / `refresh-pulse-glow` 均属实。
  - **L33-45 第一层 CSS 表漏列 `copy-button.css`**（`web/index.html:132` 确实引入）；`scrollbar.css` 由 `base.css:8` 的 `@import` 引入，表中 `base.css` 行已提到「滚动条」，不算漏。
  - **已核实为真、不必改**：「36 套主题色块」（`variables.css` 恰 36 个 `[data-theme=]` 块）、「36 套命名主题（16 浅 + 20 深）」（`web/index.html:35` `DARK_IDS` 20 条）、`themeMeta.ts:28` 的 `THEMES` 36 项、默认主题 `gruvbox-dark`（web 内联脚本 / `desktop/src/shared/theme.ts:13` / `ThemePalette.java:36`）、新增主题 5 步里的 `DARK_IDS` / `SB_COLORS`（20 / 36 项）、L376「3 套主题的 `--accent-color` 与 `--color-orange` 完全相同」（算出恰为 ayu-light / gruvbox-light / gruvbox-dark）、L427「15 套 secondary↔tertiary 对比度 < 1.12，ayu-dark 为 1.062」（复算一致）、L35 与 L45 的层级划分、L49 起 `web/src/assets/*.css` 的列举、L252 `.fbtn` 四个变体、L544-571 的 30 个守卫测试文件（逐个 `test -f` 通过）、`components.css:39/129/188`、`media-block.css:21`、`content.css:159`、`annotation-buttons.css:4` 等行号引用。

**③ 建议怎么改**

1. L38 的 `≥1024px` 改为 `≥960px`，与 L231 统一（否则读者会按 1024 断点写代码）。
  2. L154 的「约 40 个颜色变量」改为「53 个颜色变量」。
  3. 修掉 7 处行号引用（`base.css` 3 处、`designTokens.css.test.ts` 4 处）；**更稳妥的做法是改引 `it(...)` 的描述而非行号**（如「`designTokens.css.test.ts` 的『keeps each backdrop one below the surface it scrims』」），行号在测试文件增删后会再次漂移。
  4. L334 / L357 / L370 的三处计数更新为实测值（328/154/131/84、77、22）；这类数字最好标注统计口径（`grep -o ':size="N"' web/src --include=*.vue`）。
  5. L369 的 `modal-fadeIn/scaleIn` 改为 `modal-fadeIn` / `modal-scaleIn`。
  6. 第一层 CSS 表补一行 `copy-button.css`。
  7. 主题机制与新增主题步骤本身**无需改动**（36 主题、16+20、默认主题、`DARK_IDS`/`SB_COLORS` 双表同步都仍然成立）。

---

#### `docs/spec/client/desktop-client.md`

- **行数**：146 ｜ **类别**：规格（权威）
- **标题**：桌面端客户端

**① 这份文档讲什么**

Electron 桌面壳的职责边界：主窗口与无边框 chrome、原生上下文菜单、IPC bridge（`native:*`）、SSH 隧道、日志回传与 `client.log`、自升级的「侧装 + `current` 指针」模型、全量包（GitHub Release）与载荷包（npm）的分发边界。

**② 有没有引用了已经不存在的文件或函数**

**无硬失效引用**。逐项核对为真：`desktop/src/main/windowChrome.ts` / `certificatePolicy.ts` / `certificate.ts` / `clientLog.ts`（`getCookieHeaderForUrl`）、`web/src/utils/clawbenchNative.ts`、IPC 名 `native:get-tunnel-transport` / `native:get-active-tunnel-transport` / `native:set-tunnel-transport` / `native:share-file` / `native:dismiss-notification`、`APP_USER_MODEL_ID` == `electron-builder.yml` 的 `appId`、`formatInstallFailure`（`versionGate.ts:132`）、`desktopPayloadURLs` / `desktopPayloadNpmURLs`（`internal/service/desktop_upgrade.go:114,139`）、`~/.clawbench-desktop/{current,app-<version>}`、`/api/fs/raw/`、`/api/desktop/latest`、载荷包名 `@xulongzhe/clawbench-desktop-{linux-x64,linux-arm64,windows-x64}-payload` 均存在；L 里提到的 `npm/desktop-main/` 确已删除（现为 `npm/{main,platforms,desktop-payloads}`）。
  - **唯一措辞问题（L65）**：`setAutoCancel` 是 Android `Notification.Builder` 的 API，Electron 的 `Notification` **没有**这个属性。该句在桌面文档里描述的是 Android 同型问题（句末也的确链到了 `push-notifications.md`），但写在桌面篇会误导读者去 `desktop/src/main/notification.ts` 里找一个不存在的调用（该文件里确实 grep 不到 `setAutoCancel`）。

**③ 建议怎么改**

L65 改成「系统通知**不会自动消失**（Android 侧对应 `setAutoCancel`）时……」，把 `setAutoCancel` 明确标注为 Android 概念，或把整句移到 `push-notifications.md` 只留一句转引；其余内容无需改动。

---

#### `docs/spec/client/frontend-architecture.md`

- **行数**：184 ｜ **类别**：规格（权威）
- **标题**：前端架构

**① 这份文档讲什么**

前端总架构：目录分层与单一 `reactive()` store、聊天渲染流水线（裸域名 → worktree → 文件路径 → commit hash → localhost 的标注管道、Markdown 渲染、Mermaid/Excalidraw/媒体块）、主题与动态壁纸注册表、群聊、设置体系、移动端与宽屏适配、桥接层契约。

**② 有没有引用了已经不存在的文件或函数**

- **L87：`WaveBackground.vue` 已不存在**（被 `web/src/components/AnimatedWallpaper.vue` 取代；全仓库仅 `web/src/utils/animatedWallpapers/__tests__/xmb.test.ts:10` 的注释和 `docs/plans/` 里还提到旧名）。
  - **L156：`AndroidNative.log(level, tag, msg)` → 应为 `ClawBenchNative.log(...)`**（同 android-integration.md）。
  - **L52：两处「坏链接」的真实情况与预期不同。** 原文是 ``模型常写成 `` [label](`path`) `` 或 `[label]("path")` ``——两处都被包在**行内代码 span** 里，marked 实际渲染成 `<code>[label](`path`)</code>` 与 `<code>[label](&quot;path&quot;)</code>`，**不会生成 `<a>`**。也就是说：所谓「链接文字是 `"path"` / `` `path` ``」其实是**链接目标**，真正的链接文字是 `label`；其余 7 个 markdown 链接均能正常解析。问题不在渲染结果，而在于这段示例用反引号嵌套、任何链接检查器或读者都极易误判为两个坏链接。
  - 其余引用的路径、组件、`useXxx` composable、端点（`/api/config`、`/api/ai/events/*` 等）已逐项核对，全部存在且属实；`isPC` 被正确描述为「已删除」。

**③ 建议怎么改**

1. L52 改成不会歧义的写法，例如单独放一个围栏代码块展示这两个样例（` ```[label](`path`)``` ` 与 ` ```[label]("path")``` `），或用 HTML 实体转义反引号/引号，明确标注「这是**示例文本**，不是真链接」。
  2. L87 的 `WaveBackground.vue` 改为「（`AnimatedWallpaper.vue` 取代了早期的 `WaveBackground.vue`）」，或直接删掉旧名。
  3. L156 的 `AndroidNative` 改为 `ClawBenchNative`，并可顺手指向 `web/src/utils/clawbenchNative.ts` 作为前端侧统一入口。

---

#### `docs/spec/client/install-and-app-mode.md`

- **行数**：56 ｜ **类别**：规格（权威）
- **标题**：客户端安装与 App 模式

**① 这份文档讲什么**

安装与运行形态分工——网页 / PWA / 桌面 / Android 四种入口的判定（`isWebApp` / `isDesktopApp` / `isAndroidApp`）、平台识别（`isMobileOSUA`、iPadOS 陷阱）、桌面安装包下载入口、iOS 安装引导抽屉、Service Worker 作为 PWA 可安装性垫片且「结构上无法劫持」。

**② 有没有引用了已经不存在的文件或函数**

**无失效引用**。核对为真：`useDesktopDownload`（导出 `detectPlatformKey`）、`usePlatformDetect`（导出 `isMobileOSUA`）、`IosInstallDrawer`（`web/src/components/common/IosInstallDrawer.vue`，被 `WelcomeOverlay.vue:84` / `LoginView.vue:154` 引用）、`beforeinstallprompt`（`web/src/composables/usePwaInstall.ts:18`）、`web/sw.js`（`SW_VERSION = 'clawbench-sw-2'`，`NETWORK_ONLY_PATHS` 含 `/api/*`、`/share/*`、`/sw.js`、`/manifest.json`）、`web/src/utils/pwaServiceWorker.ts` 的 `updateViaCache` 与 `HEAD /sw.js` 探测（`:81`）、`/api/apk`、构建产物目录 `.clawbench-web/`（`vite.config.ts:14`）、以及「manifest 必须在 publicDir、Vite 才会原样复制 `<link rel="manifest">` href」的说法（`vite.config.ts:39-43` 注释与实现完全对应）。

**③ 建议怎么改**

无需修改。可选地把「四种形态」补一句 Android 与桌面壳共用同一 SPA 的边界说明，但当前文本已够准确。

---

#### `docs/spec/client/multi-server.md`

- **行数**：46 ｜ **类别**：规格（权威）
- **标题**：多服务器管理

**① 这份文档讲什么**

多服务器切换的存储与切换流程——Web 端用浏览器存储、Android 端走原生持久化，切换时复用已保存凭据、密码按服务端分别加密存储、认证失败区分三种情况。

**② 有没有引用了已经不存在的文件或函数**

**L25：`AndroidNative` Bridge 已不存在**，应为 `ClawBenchNative`（同 android-integration.md）。**L14 / L19：mermaid 里的 participant 叫 `AndroidBridge`**，全仓库不存在该名字。`useServerList`（`web/src/composables/useServerList.ts`）及其 `getServerList` / `saveServerNamed` / `saveServer` / `removeServer` 均存在且属实。

**③ 建议怎么改**

L14 的 participant 改名 `ClawBenchNative`，L19 的调用箭头文字与 L25 的正文一并改为 `ClawBenchNative`；其余内容保持。

---

#### `docs/spec/client/unified-back-navigation.md`

- **行数**：91 ｜ **类别**：规格（权威）
- **标题**：统一返回与跨界面导航

**① 这份文档讲什么**

统一返回导航——文件导航栈与路由上下文两级分层栈、`canHandleBack` 同步判定、`window.__clawbenchBackHandled` Android 契约、`BottomSheet` 自注册优先级、滚动位置三档锚、目录游历事务快照。

**② 有没有引用了已经不存在的文件或函数**

**无失效引用**。逐项核对为真：`useFileNavStack`（`MAX_STACK_DEPTH = 20`）、`useFileScrollRestore` + `web/src/utils/fileScrollCache.ts`、`data-source-line` / `sourceOffset`、`useNavigationContext`（normalize 在 `:52`）、`useDirectoryReturn`、`useNavigationStateMachine`（`canHandleBack`）、`useNavigationCoordinator`（`openFileInViewer` / `switchTab` / `shouldSettleOrigin`）、`hasTopmostOverlay` / `closeTopmostOverlay`（`App.vue`）、`BottomSheet`、`window.__clawbenchBackHandled`、`onBackPressed` / `evaluateJavascript`。

**③ 建议怎么改**

无需修改。

---

#### `docs/spec/core/ai-backend.md`

- **行数**：174 ｜ **类别**：规格（权威）
- **标题**：AI 后端抽象

**① 这份文档讲什么**

AI 后端抽象层——AIBackend 接口、双传输（CLI/ACP）、18 个后端注册、ACP 连接管理、工具调用、斜杠命令、压缩检测、子智能体归组、Skill 注入。

主要章节：流程图、#后端选择与传输分流、#ACP 连接与执行流程、#LoadSession 异步回放流程、功能与设计要点、#功能清单、#设计要点

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - **未发现明显过时**。双传输、ACP 连接管理、`ACPConnManager` 等核心机制均与代码一致。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/spec/core/chat-flow.md`

- **行数**：183 ｜ **类别**：规格（权威）
- **标题**：聊天流程

**① 这份文档讲什么**

聊天核心流程——消息发送、流式推送、排队机制、运行中插话、引用/附件、摘要/推荐、自动续接、自动巡航、ask-question 解析、分叉/回溯。

主要章节：流程图、#请求链路：从用户发消息到 AI 开始执行、#唯一 turn 实现：三入口共用、#WebSocket 推送链路：流式事件到前端渲染、#ACP 权限审批流程、#子智能体内容分组链路、功能与设计要点、#功能清单

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - 第 52 段描述 `usePortForward` 对 Android 调用 `openInSandbox`，对 Web 走 `openInBrowser`。代码中 `usePortForward.ts:1075-1076` 确实是 `openInSandbox` 优先、`openInBrowser` fallback——**正确，未过时**。
  - 第 114 段「排队消息不再进入对话数组」与代码一致（独立 `queued_messages` 表）；第 122 段摘要策略与 `ExtractLastAnswerFromBlocks` 一致——**均未过时**。

**③ 建议怎么改**

- 按上节「过时线索」逐条改：路径改成实际位置、符号改成实际名称；若该目标确实从未落地，改写为「曾计划但未实现」的显式声明。

---

#### `docs/spec/core/session-lifecycle.md`

- **行数**：144 ｜ **类别**：规格（权威）
- **标题**：会话生命周期

**① 这份文档讲什么**

会话生命周期——创建/运行/排队/取消/完成/归档/物理删除、ACP context_state 持久化、自动续接、分叉/续接、会话标题派生与锁定、Codex 项目级会话发现。

主要章节：Codex 项目级历史会话发现、流程图、#会话主生命周期、#运行态与可取消性由同一注册表持有、#会话归档与物理删除、#会话取消场景、功能与设计要点、#功能清单

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - 第 10 段称 Codex 恢复时「不转换会话 ID，直接将 `session_meta.payload.id` 作为 Codex thread ID 传给 ACP `LoadSession`」。代码 `list_sessions.go:207-209` 显示取值为 `sessionID := meta.ID`（若空则用 `meta.SessionID`）——逻辑描述准确，未过时。
  - `title_renamed` 旧字段（第 121 段）与代码 `database.go` 中标记为废弃的注释一致——**正确**。

**③ 建议怎么改**

- 按上节「过时线索」逐条改：路径改成实际位置、符号改成实际名称；若该目标确实从未落地，改写为「曾计划但未实现」的显式声明。

---

#### `docs/spec/core/streaming.md`

- **行数**：134 ｜ **类别**：规格（权威）
- **标题**：流式传输体系

**① 这份文档讲什么**

WebSocket 统一推送体系——StreamHub 会话扇出、断线缓冲重放（10s/50条）、投递分级（关键事件等待/高频增量丢弃）、前端缓冲回放、前端流式看门狗。

主要章节：流程图、#主通道：WebSocket 统一推送、#客户端侧消息类型、#投递分级：关键事件等待，高频增量丢弃、#前端缓冲回放：占位符未到时先攒着、功能与设计要点、#功能清单、#旁注：独立小通道（与聊天无关）

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - **未发现明显过时**。关键事件等待/高频增量丢弃机制、50ms 合流、500ms 入库 flush 均与代码一致。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/spec/core/summarization.md`

- **行数**：56 ｜ **类别**：规格（权威）
- **标题**：摘要管线

**① 这份文档讲什么**

摘要管线——固定提取结论（`ExtractLastAnswerFromBlocks`）、SummaryCards 结构化卡片、TTS 语音摘要（simple/api/auto）、多 pass 压缩、推荐回复。

主要章节：流程图、#摘要管线主流程、#TTS 摘要流程、功能与设计要点、#功能清单、#设计要点

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - 第 42 段描述 `tts_summaries` 表用于 TTS 摘要缓存，代码中表确实存在且列名是 `message_id`（新 schema）。`database.go:1602` 注释说明旧 schema 曾用 `cache_key`，已迁移——文档描述的是当前状态，未过时。
  - 验证：`grep -n "message_id\|cache_key" internal/service/database.go | grep tts` 确认 `message_id` 是当前列名。

**③ 建议怎么改**

- 按上节「过时线索」逐条改：路径改成实际位置、符号改成实际名称；若该目标确实从未落地，改写为「曾计划但未实现」的显式声明。

---

#### `docs/spec/database.md`

- **行数**：646 ｜ **类别**：规格（权威）
- **标题**：ClawBench 数据库

**① 这份文档讲什么**

主库 SQLite 的 20 张表列定义、外键级联汇总与 mermaid ER 图。

主要章节：表定义、#chat_sessions（聊天会话）、#chat_history（聊天消息）、#chat_metadata（用量台账）、#chat_tool_calls（工具调用）、#summaries（摘要）、#tts_summaries（TTS 摘要）、#scheduled_tasks（任务）

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - 文档化了一张**代码中不存在**的表 `agent_api_keys`（第 209/358/595/639 行：列定义、`agents.id` CASCADE 外键、ER 节点）。验证：`grep -rn "agent_api_keys" internal/ --include=*.go` → 无输出；`internal/service/crypto.go` 亦不存在（README/infra 文档均称已移除）。
  - **遗漏 19 张真实存在的表**：`ai_raw_responses, btw_questions, chat_recommendations, chat_thinking, feishu_subscribers, file_shares, forge_events, forge_items, forge_pipeline_runs, forge_sync_state, message_clusters_cache, message_clusters_meta, notifications, project_forges, queued_messages, schema_migrations, session_shares, session_tag_links, session_tags`。验证：遍历 `internal/**/*.go` 的 `CREATE TABLE IF NOT EXISTS` 集合与文档 `### xxx（` 标题集合求差。

**③ 建议怎么改**

- 按上节「过时线索」逐条改：路径改成实际位置、符号改成实际名称；若该目标确实从未落地，改写为「曾计划但未实现」的显式声明。

---

#### `docs/spec/features/attachments-and-share.md`

- **行数**：56 ｜ **类别**：规格（权威）
- **标题**：附件与系统分享

**① 这份文档讲什么**

描述附件系统——用户把本地文件、项目文件、系统 Share In 内容送进聊天的完整链路。覆盖多文件上传、Share In 最近列表（服务端 `.clawbench/share-in` 目录）、上传历史（`.clawbench/uploads`）、文件夹递归上传、目录树下载、粘贴上传、文件管理器拖拽（面包屑段/多选）到聊天，以及「结构化引用附件（`kind=quote`）」的五个入口与「按意图分流目标会话」（评论默认进当前会话 vs 显式选目标）的交互决策。

**② 有没有引用了已经不存在的文件或函数**

**无**。逐项核对通过：`DELETE /api/share-in/recent`（`internal/handler/upload.go:340` `ShareInRecent` 有 `MethodDelete` 分支）、`/api/upload/recent` + `DELETE /api/upload/recent`（`upload.go:360` `UploadRecent`）、`ServeListTree`（`internal/handler/file.go:185`，路由 `/api/file/list-tree`）、`model.FileEntry` 的 `Path`/`StartLine`/`EndLine`/`Kind=="quote"`（`internal/model/chat.go:33-46`）、`titleFromFileEntries`（`internal/service/chat.go:1063`）、前端 `requestTarget`（`web/src/composables/useConversationTarget.ts:184`，签名 `requestTarget(request: PendingRequest, opts)`，`deliverTo: 'active'` 分支存在）、`PendingRequest`（同文件）、`--z-quote-bar`（`web/css/variables.css:87`）、`switchTab('chat')`、`kind=quote`、`.clawbench/share-in`。链接 `[聊天流程](../core/chat-flow.md)` 指向存在的文件。

**③ 建议怎么改**

无需修改。

---

#### `docs/spec/features/chat-recommendation.md`

- **行数**：61 ｜ **类别**：规格（权威）
- **标题**：推荐回复

**① 这份文档讲什么**

描述「推荐回复」——AI 回复完成后由 LLM 生成一条下一步建议，以 `chat_recommendation` WS 事件推给前端显示横幅，可一键采纳/忽略。覆盖 stable/rolling payload 拆分与 prompt caching、快捷指令与项目上下文感知、离线恢复（`chat_recommendations` 表）、会话隔离、前端 generation 计数器防竞态，以及异步生成不阻塞 `Finalize()` 的性能取舍。

**② 有没有引用了已经不存在的文件或函数**

**无**。逐项核对通过：`chat_recommendation` 事件名（`internal/service/session_runtime.go:1171`）、`useChatRecommendation`（`web/src/composables/useChatRecommendation.ts`，`upsert`/`invalidate`/`show`/`accept`/`dismiss` + per-session `generation` 计数器全部存在）、`GET /api/chat/recommendation`（`internal/handler/handler.go:737` → `ServeChatRecommendation`）、`chat_recommendations` 表（`internal/service/database.go:876`）、`ai_summary` 配置键（`internal/model/config.go:150` `AISummary AISummaryConfig \`yaml:"ai_summary"\``）、`summarizeMessage`（`internal/service/summary.go:79`）、`RecommendNextStep`（`internal/summarize/`）、`DoRecommendPass`（`internal/summarize/`）、`Finalize()`（`internal/service/session_executor.go:1681`）、`done` 事件。

**③ 建议怎么改**

无需修改。

---

#### `docs/spec/features/completion-popup.md`

- **行数**：77 ｜ **类别**：规格（权威）
- **标题**：应用内完成通知

**① 这份文档讲什么**

描述「应用内完成通知」——会话/任务/仓库事件在聊天面板不在前台时滑入一张纯通知卡片，5 秒自动关闭（悬停暂停）。覆盖事件覆盖范围（与会话/任务/仓库三类完全对齐）、纯通知形态（徽章 vs 文字标题的两级层次）、跨项目整卡换色、已读端点分流、队列上限 3 + 同仓库合并、`inAppNotification` 本地开关，以及与系统通知的差异对照表。

**② 有没有引用了已经不存在的文件或函数**

**无**。逐项核对通过：`/api/ai/chat/read`（`handler.go:492` → `MarkChatRead`）、`PUT /api/tasks/{id}` + `action: "read"` + `executionId`（`internal/handler/scheduler.go:257-260` 的 PUT 结构体含 `action`/`executionId`，`"read"` 在注释的动作列表中）、`/api/forge/read` + `itemKey`（`handler.go:550` → `ServeForgeMarkRead`，`internal/handler/forge_endpoints.go:816` 有 `ItemKey` 字段）、`inAppNotification`（`web/src/components/settings/settingsFieldMap.ts:360`）、`useWideScreenLayout` 的 `isChatPanelVisible`（`web/src/composables/useWideScreenLayout.ts:209`，签名正是收 `isWideScreen`/`chatCollapsed`/`activeTab` 三个已解包值）、`SplitView` 的 `rightCollapsed`（`web/src/components/common/SplitView.vue:52`）、`MainActivity.isForeground`（`android/app/src/main/java/com/clawbench/app/MainActivity.java:120`）、`--font-size-lg: 14px`（`web/css/variables.css:217`）、`--font-size-md`、`--line-height-snug`（`variables.css:245`）、`fetchPendingEvents()`、`replayed: true`、`groupKey`、`response_preview_plain`/`session_title`/`project_path`/`agent_id`（`internal/ws/protocol.go:45-50`）、`session_update`/`task_update`/`forge_event`、`event.project_path`（`internal/service/forge_notify.go:95`）、`pipeline/run:<id>`。

**③ 建议怎么改**

无需修改。

---

#### `docs/spec/features/file-discovery.md`

- **行数**：80 ｜ **类别**：规格（权威）
- **标题**：文件发现

**① 这份文档讲什么**

描述「文件发现」——按文件名搜索（已融合进文件管理器 `browse` Tab 的视图切换，复用目录条目的全部交互）、按内容搜索 grep（独立的 `ContentSearchDialog` 响应式对话框，SSE 流式结果，点击跳行）、以及最近文件列表。覆盖搜索范围切换（当前目录/全局、递归蕴含）、剪枝表 `ignoredSearchDirs`（与内容搜索共享）、git 忽略条目灰显、结果高亮与定位。

**② 有没有引用了已经不存在的文件或函数**

**无**。逐项核对通过：`ContentSearchDialog`（`web/src/components/file/ContentSearchDialog.vue`）、`useFileContentSearch`（`web/src/composables/useFileContentSearch.ts`，`stopSearch()`/`cancelSearch()` 分离、`totals`/`done`/`stopped`/`truncated` 状态齐全）、SSE `/api/file/content-search`（`handler.go:665` → `internal/handler/content_search.go:434`，事件名 `result`/`done`/`error`）、`useToolbarOverflow`（`web/src/composables/useToolbarOverflow.ts`）、`BottomSheet` 的 `maximized` prop（`web/src/components/common/BottomSheet.vue:94`）、`ModalDialog` 的 `fullHeight`（`ModalDialog.vue:53`）、`useTabDrawer('browse')`（`ContentSearchDialog.vue:262`）、`openFilePath`（`web/src/composables/useFilePathAnnotation.ts`）、`open-file-overlay`、`fileNav.currentFilePath`（`web/src/composables/useFileNavStack.ts:146`）、`store.selectFile`（`web/src/stores/app.ts:506`）、`useFileNavStack`、`displayEntries`/`pathOf`（`web/src/components/file/FileManagerContent.vue:1565`）、`useFileSearch`（`web/src/composables/useFileSearch.ts`，`scope='global'` + `exact` + `immediate` 全在）、`effectiveRecursive = global || recursive`（`useFileSearch.ts:61`）、`shouldSkipSearchDir`/`ignoredSearchDirs`/`isStrictlyBelowBuild`（`internal/handler/dir_search.go:37/112/85`）、`internal/handler/dir_search.go` 文件存在、剪枝表 30 余个目录名与 `dir_search.go:37-80` 的 `ignoredSearchDirs` 逐条一致（含 `build` 刻意不在表内的说明与 `isStrictlyBelowBuild` 注释一致）、`SearchCode`（lucide 图标，`FileManagerContent.vue:566`）、`Ctrl+Shift+F`。

**③ 建议怎么改**

无需修改。

---

#### `docs/spec/features/file-management.md`

- **行数**：189 ｜ **类别**：规格（权威）
- **标题**：文件管理

**① 这份文档讲什么**

本组最大的规格文档，描述文件管理器全貌——目录浏览/面包屑/多选、路径标注（双候选解析、目录引用形态）、文件变更监听（fsnotify + WebSocket）与预览图片实时刷新、Markdown/代码双面的差异高亮与变更导航、分享链接（不可猜测 token、根快照边界）、`.gitignore` 灰亮条目、Excalidraw 画布、上传下载进度条、媒体预览的本地回退、预览器按能力分流与 HTML 沙箱 iframe 方案、Windows UNC 共享根、路径穿越防护等。

**② 有没有引用了已经不存在的文件或函数**

**1 处失效**。
  - **第 110 行**: `` `[指南](./docs/guide.md)` `` —— 这是一个**示例性的 markdown 相对链接字面量**（出现在描述"分享的 Markdown 里的相对链接可点击"的句子中），不是指向仓库内某篇文档的引用；`docs/guide.md` 不存在，且 `docs/` 下只有 `dev/ plans/ screenshots/ Skills/ spec/ superpowers/ timer/ user-guide/`。**判定：不是失效引用**，属于示例文本里的假路径。若要消歧，可把该示例改成 `../../README.md` 之类明显虚构的形式，或加一句"（示例路径）"。
  - 其余全部核对通过：`ServeListTree`（`internal/handler/file.go:185`）、`/api/dir`/`/api/projects`/`/api/dir/search`/`/api/file/watch/ws`/`/api/file/write`/`/api/file/batch-exists`/`/api/fs/raw/`/`/api/fs/file/`/`/api/share`（全部在 `internal/handler/handler.go` 的 `RegisterRoutes` 中）、`internal/model/file.go` 的 `excalidrawExts`/`IsTextFile`/`DetectSubtype`、`upload.max_size_mb`/`upload.max_files`（`internal/model/config.go:86-87`）、`useDownloadProgress`/`TransferProgressBar`/`downloadFileWithProgress`（`web/src/utils/clawbenchNative.ts`）、`useFileRefresh`/`useChangeNav`/`FileChangeNav.vue`/`resetKey`/`N/M`（`web/src/composables/useChangeNav.ts`）、`FileOverlay`/`TableRowModal`/`ToolDetailDrawer`/`FileDiffsDrawer`/`InertPathPicker`/`MediaLoadError.vue`/`MediaPreviewBody`/`Lightbox`/`AudioPreview`/`VideoPreview`/`HtmlPreviewBody`/`MarkdownPreviewBody`/`FileViewer`/`OfficePreview`/`OpenApiPreview`/`PdfPreview`/`CodeMirrorViewer`/`MarkdownPreview`/`CodePreviewToolbar`/`ExternalBadge`/`UnsupportedFileBody`/`DirBreadcrumb`（全部存在于 `web/src/components/`）、`web/src/utils/{dirList,htmlPreview,inertPathClick,localMediaFallback,mediaFullscreenGuard,lineRanges}.ts`（全部存在）、`web/src/share/shareLinks.ts`、`web/src/assets/__tests__/annotationButtons.css.test.ts`、`web/src/components/__tests__/fileViewerSandbox.test.ts`、`web/src/components/file/FileChangeNav.vue`、`internal/platform/path.go` 的 `ListRootPaths`/`UNCShareRoot`/`IsPathUnderAnyRoot`、`model.ValidatePath`、`isPathUnderAnyRoot`、`resolveShareTarget`、`normalizeForCompare`/`dirName`/`joinPath`（`web/src/utils/path.ts`）、`useFileSearch`/`deriveSearchQuery`/`resolveFilePathDual`/`reverifyAnnotations`/`verifyFilePaths`/`containsGlobChars`/`extractLineInfoFromText`/`LINE_*_RE`/`TRAILING_LINE_REF_RE`/`markInertLink`、`StaticBlockCache`/`renderTextBlock`/`renderMarkdownHtml`/`detectedPaths`、`useMarkdownRenderPipeline`/`createFixLocalImagePaths`/`rewriteImageUrls`/`useMediaWatch`/`useDirPreview`/`maxGetFileBytes`/`parseLineWindow`、`buildHtmlPreviewSrc`/`canRenderDocument`/`placeNearAnchor`/`mediaFullscreenGuard.ts`。
  - **两处路径前缀需注意（但不算失效）**：文档第 149 行写 `MediaLoadError.vue` 与 `media-block.css` 时未给全路径——实际 `MediaLoadError.vue` 在 `web/src/components/media/`，`media-block.css` 在 `web/css/`（不在 `web/src/assets/`）。因为文档只写了裸文件名，不构成失效引用，但读者按 `web/src/assets/` 找会扑空。

**③ 建议怎么改**

可选微调——第 149 行给 `MediaLoadError.vue`/`media-block.css` 补全仓库路径（`web/src/components/media/MediaLoadError.vue`、`web/css/media-block.css`），避免读者误以为在 `web/src/assets/`；第 110 行的示例路径可标注为示例以免被当成真实引用。

---

#### `docs/spec/features/forge-integration.md`

- **行数**：191 ｜ **类别**：规格（权威）
- **标题**：Forge 集成（GitHub / GitLab）

**① 这份文档讲什么**

描述 Forge（GitHub/GitLab）集成——仓库绑定、Issue/PR 浏览、未读与通知、后台轮询感知变化并推导事件、CI 完成事件的独立通道、事件触发 AI 任务、凭据按 host 隔离与协议/主机分离、水位线按 item 类型分离、只读 API 边界与已接受的限制。附完整 API 端点清单与数据模型表。

**② 有没有引用了已经不存在的文件或函数**

**无失效**——但**第 106 行的 `model.Language` 需要更正表述（该符号真实存在）**。
  - **第 106 行**原文："后端在渲染时按 `model.Language` 解析"。
  - **核实结果**：`model.Language` **确实存在**，是 `internal/model/config.go:449` 的包级变量（`var ( ... Language string )`，注释为 "the resolved UI language ("zh" / "en"), read by background goroutines that must localize without an HTTP request in scope"），并且 `internal/service/forge_event_context.go:21` 正是 `i18n.T(i18n.LocalizerForLocale(model.Language), messageID)`。`internal/service/forge_notify.go:128` 的注释也写明 "Localized from model.Language"。`go vet` / `go test` 编译验证通过。
  - **所以该处引用的符号是对的**；若指的是"配置里的语言字段"，那才是 `model.Config.Language`（`internal/model/config.go:70`，yaml 键 `language`）/ `model.ConfigInstance.Language` / `model.DefaultLanguage`（`config.go:47`）。
  - **建议改法**：为消除与"配置字段"的歧义，把该处改为「后端在渲染时按 `model.Language`（`internal/model/config.go` 的包级变量，值来自前端持久化到服务端的 `model.Config.Language`）解析」。既保留正确符号，也点明来源。
  - 其余核对通过：`internal/forge/` 的 `Provider`/`Item`/`Comment`/`Change`/`ItemState.CreatedAt`/`ItemState.MergedAt`/`Item.SourceBranch`、`DeriveChanges`（`internal/forge/events.go:121`）、`forge.ItemKey`（`events.go:293`）、`RecordPipelineRun`/`CountUnreadForgeEvents`/`ListForgeActivityItems`（`internal/service/forge_sync.go`）、`RenderEventContext`（`internal/service/forge_event_context.go:470`）、`ForgeEventDispatcher.HandleChange`/`ForgeRepoRef.Key()`/`ForgeRepoRef.ProjectPath`/`ProjectForge.ProjectPath`（`internal/service/forge_notify.go`）、`forgeTargetItemKey`（`web/src/composables/useForge.ts`）、`forgeTargetFromDetail`（`web/src/App.vue`）、`NotificationNav`、`ForgePanelContent`、`broadcast payload 键名`（`platform`/`host`/`owner`/`repo`/`item_type`/`number`/`run_id`/`event_type`/`project_path`）、`TestForgeDispatcher_PayloadEventKeysAreSnakeCase`、`TestEventContextLabelsMatchFrontendRegistry`（`internal/service/forge_event_context_parity_test.go:24`）、`web/src/utils/forgeEventContextVars.ts`、全部 14 个 `/api/forge/*` 端点（`handler.go:537-550`）、`docs/plans/2026-09-12-forge-webhook.md`（存在）、`forge.notify.*` 服务端开关（`internal/model/defaults.go`）、`desktopNotification`/`browserNotification`（`web/src/composables/useSettingsConfig.ts:38`）、`ItemKey` 格式 `issue/<n>`/`pr/<n>`/`pipeline/run:<id>`、水位线列 `issue_watermark`/`pr_watermark`（`internal/service/forge_sync.go:288`）、`comments_baselined`、`event_url`/`event_summary`、`task_executions`。

**③ 建议怎么改**

按上条改写第 106 行的 `model.Language` 表述（当前引用正确，但应补上"这是 `internal/model/config.go` 的包级变量、其值来自前端持久化的 `model.Config.Language`"这一层，以免读者误以为它是 `model.Config` 的结构体字段）。

---

#### `docs/spec/features/git-management.md`

- **行数**：95 ｜ **类别**：规格（权威）
- **标题**：Git 管理

**① 这份文档讲什么**

描述 Git 管理——项目级/文件级历史浏览（ASCII commit graph、commit SHA 前缀搜索）、工作区变更页（按需重拉、`inPlace` 原地刷新、drill-down 状态快照恢复）、文件 Diff 抽屉、提交详情面板的可跳转文件/路径行、Worktree 管理、分支与标签的内联操作按钮、commit hash 与 Worktree 路径标注，以及数据统计页签下的两个子页：代码存量（cloc/gocloc）与代码增量（git log --numstat，按 committer date 分桶）。

**② 有没有引用了已经不存在的文件或函数**

**无**。逐项核对通过：`workingTreeFileCount`/`wtFiles`/`filesRefreshing`/`inPlace`（`web/src/composables/useGitHistoryView.ts:76/112/607`）、`/api/git/working-tree`（`handler.go:587`）、`FileDiffsDrawer`（`web/src/components/chat/FileDiffsDrawer.vue`）、`useDiffNavigation`（`web/src/composables/useDiffNavigation.ts`）、`open-directory-from-context`（`web/src/App.vue:2318`）、`GET /api/git/cloc`（`handler.go:530` → `internal/handler/git_cloc.go:213`）、`GET /api/git/stats?start&end&trend`（`handler.go:529` → `internal/handler/git_stats.go:272-310`，`start`/`end` 走 `q.Get`，`trend` 需为 `"1"`）、`DELETE /api/git/branch` 或 tag（`internal/handler/git.go:311` `ServeGitBranch` 的 `MethodDelete` 分支、`git.go:1567` `ServeGitTags` 的 `MethodDelete` 分支）、cloc 排除层（`git_cloc.go:41` 的 `gitClocDefaultExclude` 正则含 `node_modules`/`vendor`/`.venv`/`dist`/`.git` 等）、`.gitignore` + `.git/info/exclude` 第二层过滤（`git_cloc.go:105` `gitignore.ForDir(projectPath)`，`internal/gitignore/` 包存在）、gocloc 走目录 + 事后过滤（`git_cloc.go:84-107` 注释与实现一致，`.github` 子串匹配问题在 `git_cloc.go:87-88` 有说明）。

**③ 建议怎么改**

无需修改。

---

#### `docs/spec/features/group-chat.md`

- **行数**：190 ｜ **类别**：规格（权威）
- **标题**：AI 群聊

**① 这份文档讲什么**

AI 群聊——主持人/自由两种协作模式、`@` 提及接力、并发发言快照、`cruise` 自动巡航开关、`group_orchestrator.go` 循环内核、成员压缩（单成员 ACP 直驱）。

**② 有没有引用了已经不存在的文件或函数**

**L144** 的 `resolveSpeakerTargets` **不存在**（全仓仅出现在 `docs/plans/2026-10-04-ai-group-chat-design.md` 等已归档设计稿）；真实函数是 `resolveSpeakerGroups`（`internal/service/group_orchestrator.go:1445`，另有 `resolveCruiseSpeakerGroups` 同文件 :1334）。**L112 / L135 的 `model.Language` 属实有效**（`internal/model/config.go:460` 的包级变量），非失效引用。

**③ 建议怎么改**

L144 把 `resolveSpeakerTargets` 改为 `resolveSpeakerGroups`（若要覆盖巡航分支则并列 `resolveCruiseSpeakerGroups`）。L112/L135 保持 `model.Language` 不动，但可精确化为「按 `model.Language`（运行时全局，源自 `model.Config.Language`，yaml `language`）本地化」，避免读者误以为它是结构体字段。

---

#### `docs/spec/features/notification-center.md`

- **行数**：76 ｜ **类别**：规格（权威）
- **标题**：通知中心（Notification Center）

**① 这份文档讲什么**

通知中心——`notifications` 单表只追加日志语义（无已读/无角标）、`created_at` 亚秒精度复合游标分页、三条写入路径（会话终态 / 任务事件 / Forge sink）均不复用 `pending_events` 门控、来源存在性探测与 30 天/500 条双上限。

**② 有没有引用了已经不存在的文件或函数**

**未发现失效引用**——`pending_events`、`store.DBReady`、`pushSessionTerminal`、`emitSessionEvent`、`emitTaskEvent`、`ForgeNotificationRecorder`、`StoreNotification`、`StoreNotifiableEvent`、`RecordSessionNotification`、`MarkMissingSources`、`ResolveGroupTimelineID`、`NavigationSurface`、`hotSwitchProject`、`clawbench-open-session/-task/-forge`、`idx_history_sess_unread`、`notifications` 表等逐项核对全部存在且位置相符。唯一轻微过时是 **L72**「三个 `Record*` 适配器」，现已是**四个**（`RecordSessionNotification` / `RecordTaskNotification` / `RecordForgeNotification` / `RecordUsageNotification`，`internal/service/notifications.go:544/606/661/689`）。

**③ 建议怎么改**

L72 把「三个 `Record*` 适配器」改为「四个 `Record*` 适配器（新增 `RecordUsageNotification` 承接用量计费路径）」，并补一句其事件 ID 幂等前缀，避免下次再加适配器时再次漂移。

---

#### `docs/spec/features/push-notifications.md`

- **行数**：218 ｜ **类别**：规格（权威）
- **标题**：推送通知

**① 这份文档讲什么**

推送通知——WS 实时通道与离线持久化（`pending_events`）、钉钉/飞书企业推送（Stream API / Lark SDK WebSocket）、会话命令解析（`ParseSessionCommand` / `ResolveShortSessionID`）、`suppress_notification` 已读门控、`read` 事件消除通知栏旧通知。

**② 有没有引用了已经不存在的文件或函数**

- **L130**：`handleSessionCommand()`（标注在 `internal/push/dingtalk/session_command.go`）——**文件不存在**（该目录只有 adapter/main_test/manager/media/push/sender/stream/token），**函数也不存在**；真实共享解析在 `internal/push/common/session_command.go`（`SessionCmdRe` :14 / `ParseSessionCommand` :18 / `ClassifyIncoming` :63 / `ResolveShortSessionID` :83），钉钉侧入口是 `internal/push/dingtalk/stream.go:145 handleIncomingText`、`:404 handleSessionList`、`:332 deliverToSession`。
  - **L151**：`handleSessionCommand()`（`internal/push/feishu/stream.go`）——文件存在但**该方法不存在**；飞书侧真实入口是 `internal/push/feishu/stream.go:187 handleIncomingText`、`:543 handleSessionList`、`:360 deliverToSession`。
  - **L139**：`DingtalkDB` 接口**不存在**（仅测试注释提及）；真实接口为 `common.PushDB`（`internal/push/common/interfaces.go:16`）。同段的 `SessionMessenger` 有效（`interfaces.go:43`）。
  - **L169**：`FeishuDB` **不存在**；同上应为 `common.PushDB`。
  - **L128 的 `model.Language`** 与 **L163 的 `model.UploadMaxFiles`** 经核实**均存在**（`internal/model/config.go:460` / `:464`），非失效引用。

**③ 建议怎么改**

L130 改为「`internal/push/common/session_command.go` 的 `ParseSessionCommand` 解析短 ID，钉钉侧 `internal/push/dingtalk/stream.go` 的 `handleIncomingText` 匹配运行中会话并入队」；L151 同样改为 `internal/push/feishu/stream.go` 的 `handleIncomingText`。L139 / L169 把 `DingtalkDB` / `FeishuDB` 统一改成 `common.PushDB`。L128 可补注「`model.Language` 运行时全局 ← `model.Config.Language`（yaml `language`）」；L163 可补注「`model.UploadMaxFiles` ← `model.Config.Upload.MaxFiles`（yaml `upload.max_files`），`cmd/server/main.go:672` 启动时赋值」，既保留正确符号又标明配置来源。

---

#### `docs/spec/features/quick-actions.md`

- **行数**：37 ｜ **类别**：规格（权威）
- **标题**：快捷操作

**① 这份文档讲什么**

聊天区快捷发送抽屉（`QuickSendDrawer`，多 Agent 一键发问 / 常用 prompt）与终端快捷命令抽屉（`QuickCommandDrawer`，预置命令片段插入）。

**② 有没有引用了已经不存在的文件或函数**

**未发现失效引用**——`web/src/components/chat/QuickSendDrawer.vue` 与 `web/src/components/terminal/QuickCommandDrawer.vue` 两个组件文件均实际存在，文档中无其他反引号代码符号或接口路径。

**③ 建议怎么改**

无需修改。若想提高可维护性，可补上两个组件的路径（本文件已写）并追加一句「两个抽屉的候选项均由前端静态配置 + 后端 agent 列表合并」，但非必须。

---

#### `docs/spec/features/rag.md`

- **行数**：72 ｜ **类别**：规格（权威）
- **标题**：RAG 检索

**① 这份文档讲什么**

RAG 检索——向量+BM25+混合检索（RRF 融合）、增量索引与三种粒度的重建（`fts` / `vector` / `full`）、会话聚合搜索（内容通道 + 名称通道）、消息聚类分析（Union-Find 三级相似度）。

**② 有没有引用了已经不存在的文件或函数**

- **L53**：`service.ResetAllIndexed` **不存在**于 `service` 包；真实函数是 `store.ResetAllIndexed`（`internal/store/session_queries.go:512`，签名 `func ResetAllIndexed() (int64, error)`）。
  - **L49**：`rag.search_mode` 配置键 **不存在**——`model.RAGConfig`（`internal/model/config.go:361-374`）只有 vector_enabled / base_url / model / api_key / ollama_* / chunk_size / chunk_overlap / poll_interval / batch_size / search_limit / search_pool_size / retention_days，**无 search_mode**；真实机制是**按请求**的 `SearchParams.PreferMode` + 常量 `rag.SearchModeHybrid/Vector/FTS/Recent/Title`（`internal/rag/search.go:19-27`，判分逻辑 :93-132）。
  - 其余符号（`RebuildCoordinator`、`insertOneChunk`、`SegmentText`/`SegmentTokens`/`InitSegmenter`、`resegmentPending`、`resegmentBatchSize`、`escapeLikePattern`、`ExtractLastAnswerFromBlocks`、`RAGSessionSearch`、`SessionSearchResult`、`SessionTypeFilterTypes`/`SessionTypeMatchesFilter`、`maybeAutoTitleSessionTx`、`IndexDiskUsage`、`GlobalStore`、`normalizeTimeBound`、`addMissingColumns`、`RetentionDays`）全部核对通过。

**③ 建议怎么改**

L53 把 `service.ResetAllIndexed` 改为 `store.ResetAllIndexed`（`internal/store/session_queries.go:512`）。L49 把「搜索模式通过 `rag.search_mode` 配置控制（hybrid/vector/fts）」改为「搜索模式由**单次请求**的 `SearchParams.PreferMode` 指定（`rag.SearchModeHybrid/Vector/FTS/Recent/Title`，`internal/rag/search.go`），不是全局配置」。

---

#### `docs/spec/features/scheduled-tasks.md`

- **行数**：115 ｜ **类别**：规格（权威）
- **标题**：任务

**① 这份文档讲什么**

任务调度——cron 与事件两种触发方式（`triggerMode`）、前置脚本（exit 0 且无输出则静默跳过 AI）、摘要推送、续接对话、执行历史事件驱动（`task_update` 广播 + 200ms debounce）、ACP 自动审批、异常自动重试。

**② 有没有引用了已经不存在的文件或函数**

**未发现失效引用**——`triggerMode`/`repeatMode`/`maxRuns`（`internal/model/scheduler.go:37-38`）、`eventVarVisible`（`web/src/utils/forgeEventContextVars.ts:217`）、`TaskEventTriggerUnsupported`（`internal/handler/scheduler.go:337`）、`GetRunningCounts`/`GetRunningExecutions`（:109/:81）、`OnStarted`（`run_turn.go:115`）、`ModeScheduled`（`session_executor.go:39`）、`BuildChatRequest`（`chat_request.go:42`）、`ScheduledExecution`（`scheduler.go:1086`）、`internal/handler/clawbench_command.go`、`internal/service/auto_continue.go`、`chat.auto_continue_max_retries`、`task_executions.read_at`、链接 `../core/session-lifecycle.md` 均存在。L100 的 `CLAWBENCH_SCHEDULED` 与 `clawbench task` 子命令是**「已移除」的正确陈述**，非失效引用。

**③ 建议怎么改**

无需修改。可选：把「`CLAWBENCH_SCHEDULED` 随子命令移除而失效」的表述补上具体移除版本/提交号，便于日后检索历史行为。

---

#### `docs/spec/features/session-navigation.md`

- **行数**：56 ｜ **类别**：规格（权威）
- **标题**：会话导航与分叉

**① 这份文档讲什么**

会话导航与分叉——用户消息索引抽屉（按会话列出用户消息 + 搜索/过滤）、会话重命名生成选项、会话分叉（`POST /api/ai/session/fork`，含 `beforeMessageId` 与 `fork_context_budget` 上下文预算）。

**② 有没有引用了已经不存在的文件或函数**

**未发现失效引用**——`GET /api/ai/chat/user-messages` 与 `POST /api/ai/session/fork` 均在 `internal/handler/handler.go` 注册（:509 / :518）；`UserMsgIndexDrawer`（`web/src/components/chat/UserMsgIndexDrawer.vue`，含 search/filter/`msgOrdinal`）、`MessageIndexRow.vue`（渲染 `<mark>` + `title`/`aria-label`）、`buildRenameGenerateOptions`（`web/src/utils/sessionRename.ts`）、`ai_summary.*`（`internal/model/config.go:150`）、`chat.fork_context_budget`（:96）、`beforeMessageId`（`internal/handler/session_rewind.go:84`）、`summaries` 表（`internal/service/database.go:696`）、链接 `../core/session-lifecycle.md` 全部存在且语义相符。

**③ 建议怎么改**

无需修改。

---

#### `docs/spec/features/session-share.md`

- **行数**：114 ｜ **类别**：规格（权威）
- **标题**：会话分享

**① 这份文档讲什么**

描述会话分享功能——把一段 AI 对话冻结成 JSON 快照存进 `session_shares`，签发 token 公开只读链接（`/share/{token}` 是独立 SPA）。覆盖创建/轮换/撤销、管理抽屉（按项目隔离）、归档保留策略、快照自包含原则（内联工具输入输出与思考文本、绝不走 `ContentBlock` 往返）、路径相对化与超限裁剪、以及分享页的只读渲染（抑制动作按钮、拦截路径 chip 跳转）。

**② 有没有引用了已经不存在的文件或函数**

- **L73** `ResolveSessionSpeakers(..., includeAvatar=true)` — 真实签名是 `service.ResolveSessionSpeakers(sessionID string) (*SpeakerIdentity, map[string]SpeakerIdentity)`（`internal/service/speaker_identity.go:44`），**没有** `includeAvatar` 参数；头像改为**无条件**随 `SpeakerIdentity.Avatar` 携带（见同文件 32-38 行注释）。
  - **L76** `payload.Messages[i].Content` — 该写法会与 handler 层的 `sessionShareStatusResponse.Messages []service.SessionMessagePreview`（`internal/handler/session_share.go:42`）混淆；后者是分享对话框的候选消息列表，**没有 `Content` 字段**。裁剪实际写回的是 service 层快照结构体。
  - 次要：**L87** `（/api/file/、/api/dir）` — `openFilePath` 实际打的鉴权端点是 `/api/dir` 与 `/api/file/batch-exists`；真正的文件读取路由是 `/api/fs/file/` 与 `/api/fs/raw/`（`internal/handler/handler.go:579,605`），没有 `/api/file/` 前缀路由。建议写成 `/api/file/batch-exists`、`/api/dir`。
  - 其余全部核对通过：`session_shares` 表 7 列与 `idx_session_shares_session` ✓；`/api/share/session`（GET/POST/DELETE）、`/api/share/session/list`（GET/DELETE）、`/api/share/{token}/meta`、`/api/share/{token}/session` 路由 ✓（`handler.go:568,573,574` + `file_share.go:32,36`）；`model.ContentBlock.MarshalJSON`、`SlimBlock.FilePath`、`think_id`、`formatShareTime`、`idx_session_shares_session`、`groupMembers`/`isHost`（`model.GroupMemberPreview.IsHost`）✓；前端 `shareLinks.ts`(web/src/share/)、`parseMessages`(utils/chatSessionUtils.ts)、`openFilePath`/`verifyFilePaths`/`fetchPathTypes`(useFilePathAnnotation.ts)、`buildMarkdownPreviewDom`(useMarkdownRenderPipeline.ts)、`handleAnchorClick`(useDoubleClickCopy.ts)、`isShareMode()`(share/shareMode.ts)、`useChatRender`、`renderMarkdown`、`ContentBlocks.reverifyAnnotations`、`ChatMessageItem`、`MarkdownPreview`、`FileHeader` ✓；`css/components.css`(= web/css/components.css)、`wrapCheck.css.test.ts`(= web/src/components/common/__tests__/) ✓；`share.html` ✓。

**③ 建议怎么改**

1. L73 改为 `service.ResolveSessionSpeakers(sessionID)`，并补一句「头像**恒**随 `SpeakerIdentity.Avatar` 冻结（无开关参数）」。
  2. L76 把 `payload.Messages[i].Content` 改为 `SessionSharePayload.Messages[i].Content`（`internal/service/session_share_payload.go:82,131`，元素类型 `SessionShareMessage`），并注明与 handler 的 `sessionShareStatusResponse.Messages`（`[]service.SessionMessagePreview`）无关。
  3. L87 端点列表改为 `/api/dir`、`/api/file/batch-exists`。

---

#### `docs/spec/features/session-tags.md`

- **行数**：86 ｜ **类别**：规格（权威）
- **标题**：会话标签

**① 这份文档讲什么**

会话标签功能——给会话打自定义标签、按项目隔离（全局标签用 `project_id=0` 哨兵）、会话列表顶部按标签过滤。覆盖数据模型（`session_tags` / `session_tag_links`）、全量替换式更新语义（`tags` 不传=不改、`[]`=清空）、哈希配色与对比度校准、失败必须可见等设计要点。

**② 有没有引用了已经不存在的文件或函数**

**入口描述整体过时**——
  - **L19**（mermaid）`用户->>SessionList: 长按会话 → 「设置标签」`
  - **L37**（mermaid）`A[长按会话菜单] --> D[SessionTagDialog]`
  - **L38**（mermaid）`B[会话行标签行] --> D` — 会话行的标签行（`web/src/components/session/SessionList.vue:153-159` 的 `.session-item-tags` / `.session-tag`）**没有 `@click`**，不可点开对话框。
  - **L48**（正文）`长按会话行（桌面端右键）在菜单里选「设置标签」`
  真实情况：`SessionList.vue` 明确「**There is deliberately no right-click and no long-press handler**」（L46-50），`showMenuFromButton` 注释写明「This is the only menu entry point」（L695-698）；「设置标签」是行尾 `⋮` 菜单里的一项（`SessionList.vue:270-273`，`openTagDialogFromMenu`）。这与 `session-share.md:85` 的「菜单入口统一走 `⋮`，不做右键/长按」是一致的——本文件没跟上。
  其余核对通过：`GET/DELETE /api/ai/session/tags`（`inUse=1`、`name`+`scope`）、`PATCH /api/ai/session/update` 的 `tags`、`GET /api/ai/sessions?tag=` 路由与参数全部匹配（`handler.go:500,501,497`；`chat_session.go:1221,1236,1245,131`）；表结构 `UNIQUE(name, project_id)`、`UNIQUE(session_id, tag_id)`、`ON DELETE CASCADE`、`scope`/`project_id` 列 ✓（`internal/service/database.go:1468-1480`）；`SessionTagDialog.vue` / `SessionTagFilterBar.vue` ✓。

**③ 建议怎么改**

把 L19 / L37 / L38 / L48 的「长按」「右键」统一改为「**行尾 `⋮` 按钮 → 菜单里的「设置标签」**」；L38 从流程图里删掉（标签行只做展示，不可点），把「标签的三个入口」改成「两个入口：`⋮` 菜单 / 顶部过滤栏」，或保留三个节点但把 B 改为「不可点击，仅展示」。

---

#### `docs/spec/features/setup-wizard.md`

- **行数**：87 ｜ **类别**：规格（权威）
- **标题**：首次访问欢迎面板（WelcomeOverlay）

**① 这份文档讲什么**

首次访问欢迎面板 `WelcomeOverlay`（不是分步设置向导）——展示已安装后端与安装入口，数据来自 `GET /api/backends` + `GET /api/agents`，`POST /api/agents/rescan` 触发 `model.RefreshAgents`。并显式声明**不存在**的一批概念（`/api/setup/*` 路由、`PiConfig`、`auth.json`/`models.json`）。

**② 有没有引用了已经不存在的文件或函数**

- **L11** `（16 个后端规格）` 与 **L24** `H-->>F: 16 个后端规格 (含 install_cmd)` — 实际 **18 个**（`internal/ai/backends` 18 个子包注册，`AllSpecsSorted()` 实测 18，且 18 个全部 `NoCLI=false`，即 `/api/backends` 全部返回）。id 列表：claude、codebuddy、opencode、codex、qoder、vecli、deepseek(CodeWhale)、pi、cursor、kimi、copilot、mimo、grok、antigravity、zcode、dsh、hermes、commandcode。
  - **L61** `显示所有 15 个注册后端（internal/model.BackendRegistry）` — 数量错（15→18），且 API 引用过时：`BackendRegistry` 是 `internal/model/discovery.go:123` 的**裸变量**，注释明确「Direct reads should use `GetBackendRegistry()` to ensure initialization」（L120-123）。
  - **L65** `ACP 能力（是否支持 Transport: "acp-stdio"）` — `Transport` 是 **`model.Agent`** 的字段（`internal/model/agent.go:51`，值写在 `agents` 表），**`model.BackendSpec` 上没有 `Transport` 字段**（`internal/model/discovery.go:13-42` 只有 `AcpCommand`）；`/api/backends` 的响应体 `backendInfo`（`internal/handler/agent.go:1968-1975`）只有 `id/name/specialty/default_cmd/thinking_effort_levels/install_cmd`，**不下发 transport**。`WelcomeOverlay.vue` 也确实没渲染任何 ACP 标记。
  - 其余核对通过：`internal/model/refresh.go` 存在且含 `func RefreshAgents(db dbutil.Writer, opts RefreshOptions)`（L96）✓；`BackendSpec.InstallCmd` ✓（`discovery.go:24`，claude 的 install cmd 正是 `"npm install -g @anthropic-ai/claude-code"`）✓；`POST /api/agents/rescan` ✓（`handler.go:613` + `agent.go:76`）；`GET /api/agents`、`GET /api/backends` ✓；`STORAGE_KEY='clawbench_welcome_dismissed'` 与 `clawbench-show-welcome` 事件 ✓（`WelcomeOverlay.vue:119,241`；`SettingsCategory.vue:570`）；`WelcomeOverlay.vue` / `AgentInstallDialog.vue` 存在 ✓；`PiConfig` / `auth.json` / `models.json` / `/api/setup/*` 确认**不存在** ✓（`auth.json` 仅 `~/.commandcode/auth.json` 是 CommandCode 自己的凭据文件，与本条无关）；loading spinner ✓（`WelcomeOverlay.vue:20,36`）。

**③ 建议怎么改**

1. L11 / L24 的「16 个后端规格」改为「18 个后端规格」。
  2. L61 改为「显示所有 **18** 个注册后端（`model.GetBackendRegistry()`，`internal/model/discovery.go:130`；`BackendRegistry` 裸变量须先经它初始化）」。
  3. L65 删除或改写：ACP 能力在 `BackendSpec` 上是 `AcpCommand`（空串=不支持 ACP），但 **`/api/backends` 不下发该字段**、欢迎面板也不渲染 ACP 标记 —— 若要保留该行需先给 `/api/backends` 加字段并同步 `internal/api/openapi.yaml`。

---

#### `docs/spec/features/stt.md`

- **行数**：64 ｜ **类别**：规格（权威）
- **标题**：语音输入（STT）

**① 这份文档讲什么**

语音输入（STT）——麦克风录音，后端 ASR 引擎转写。非流式（`POST /api/stt/transcribe` 一次性提交）与流式（`WS /api/stt/transcribe/ws`，按 `chunk_ms` 增量识别 + 结束帧全量重识别）双模式，`STTProvider` 接口抽象识别引擎。

**② 有没有引用了已经不存在的文件或函数**

**无**。逐项核对通过——`POST /api/stt/transcribe` 与 `/api/stt/transcribe/ws` 路由 ✓（`handler.go:618,619`）；`stt.STTProvider` 接口及 `Transcribe(ctx, audioReader, language)` ✓（`internal/stt/interface.go:11-18`）；配置键 `stt.streaming` / `stt.chunk_ms` / `stt.shortcut_key` ✓（`internal/model/config.go:298-306`，白名单 `internal/handler/settings.go:111-117`）；前端 `useVoiceInput` 读 `stt.streaming` / `stt.chunk_ms` / `stt.shortcut_key`，默认 F9 ✓（`web/src/composables/useVoiceInput.ts:62-64`）；10MB 非流式上限（`sttMaxBodyBytes = 10 << 20`）与 20MB 流式上限（`sttWSMaxAudioBytes = 20 << 20`）✓（`internal/handler/stt.go:62,101`）；`language` 形参 ✓。

**③ 建议怎么改**

无需修改。可选补一句「流式控制的 `chunk_ms` 间隔由前端从 `serverConfig['stt.chunk_ms']` 读取（默认 1000ms）」以对齐实现。

---

#### `docs/spec/features/system-resources.md`

- **行数**：53 ｜ **类别**：规格（权威）
- **标题**：系统资源监控

**① 这份文档讲什么**

系统资源监控——CPU/内存/磁盘/网络/负载的实时面板。服务端按需采样（`MetricsPusher` 只在存在已声明订阅者时采样）、遥测走非缓冲投递路径不挤占重连回放缓冲、前端前台 1000ms / 后台 5000ms 双速推送、页面隐藏即停推。

**② 有没有引用了已经不存在的文件或函数**

**L39** `SystemResourcesPanel 在 AppHeader 的 Gauge 图标弹出菜单中展示` —— AppHeader **没有 Gauge 图标**：`web/src/components/common/AppHeader.vue:336` 的 lucide 导入里是 `Server`（无 `Gauge`），L273-275 注释写明「Server button: merged gauge + status dot」，L275 渲染 `<Server v-if="!isUnderPressure || !showMetricIcon" :size="18" />`。本文档下一条（L40）自己写的却是「AppHeader 的 **Server** 图标」——同文档内自相矛盾。组件 `web/src/components/common/SystemResourcesPanel.vue` ✓ 存在。
  其余核对通过：`GET /api/system/resources` ✓（`handler.go:469`）；`system.GetResources()` + 500ms 缓存（`internal/system/resources.go:103,127`）✓；`service.MetricsPusher` ✓（`internal/service/metrics_pusher.go`）、`ws.Manager.MetricsDemand()` / `BroadcastToMetricsWatchers()` ✓（`internal/ws/manager.go:331,672`）；前端 `useSystemResources` 的 `startPolling`(1000ms) / `startBackgroundPolling`(5000ms) 与 `metrics_preference` 帧 ✓（`web/src/composables/useSystemResources.ts:48,50,95,149,161`）。

**③ 建议怎么改**

L39 把「Gauge 图标」改为「**Server** 图标（合并了仪表盘外观与状态点，lucide `Server`）」，与 L40 统一。

---

#### `docs/spec/features/terminal.md`

- **行数**：90 ｜ **类别**：规格（权威）
- **标题**：Web 终端

**① 这份文档讲什么**

Web 终端——后端 PTY + xterm.js 渲染，WebSocket 双向传输。覆盖多标签 PTY、断线重连回放与输出抑制、RingBuffer 有界缓冲、虚拟修饰键三态、三模式手势、选中即复制、键盘避让与布局重排、键位/快捷指令配置、157 个 xterm 主题、拖拽文件上传到实时 cwd（`/proc` 前台进程组探测）。

**② 有没有引用了已经不存在的文件或函数**

**无**。逐项核对通过——`/api/terminal/ws`、`/api/terminal/status`（`cwd_probe_supported` ✓，`internal/handler/terminal.go:126-151`）✓；`terminal.max_sessions`（默认 10）/ `buffer_lines` 2000 / `max_buffer_mb` 4 ✓（`internal/model/config.go:317-320`）；`terminal.NewSession` / `Session.Cwd()`（`internal/terminal/session.go:67,457`）/ `HandleResize`（:321）/ `RingBuffer`+`NewRingBuffer` ✓；`TIOCGPGRP` + `/proc/<pid>/cwd`（`internal/terminal/cwd_linux.go:39`）✓；4096 字节读缓冲（`session.go:130`）✓；`TERM=xterm-256color` / `COLORTERM=truecolor` / `Setsid:true`（`internal/terminal/shell.go:82-89`）✓；初始 80x24 默认（`handler.go:97-110` 注释与 cols/rows 参数）✓；前端 `useTerminalKeyboard` / `useTerminalViewport`（`fullScreenHeight - currentInnerHeight` ✓）/ `useTerminalFileDrop` / `useCrudList` / `useFileUpload` / `shouldAutoRefocusTerminal`(utils/terminalBlurUtils.ts) ✓；`TerminalPanelContent.vue` / `TerminalHelpDrawer.vue` / `DropOverlay.vue` / `UploadProgressBar.vue` ✓；`expandDataTransfer`(utils/dropFolder.ts) ✓；`disconnectAll`(useTerminalTabs.ts) ✓；`open-directory-from-context` 事件 ✓（`useFilePathAnnotation.ts:1463`）；`leftPanelActive`(App.vue) ✓；157 主题（`web/src/utils/terminalThemes.ts:8`）✓；150ms 沉降（`useTerminalCopyOnSelect.ts:17`）✓。

**③ 建议怎么改**

无需修改。

---

#### `docs/spec/features/tts.md`

- **行数**：55 ｜ **类别**：规格（权威）
- **标题**：语音合成（TTS）

**① 这份文档讲什么**

语音合成（TTS）——Edge / OpenAI 兼容 / Piper / Kokoro / MOSS-Nano 多引擎，统一 `SpeechProvider` 接口。覆盖摘要后端三选一（simple/api/auto，按 `StripMarkdownStats` 的杂质比例分流）、保守文本清理、群聊轮流朗读（发言人名并入缓存键）、文件缓存淘汰、OpenAI 兼容双形态流式（sse / audio，按 `Content-Type` 判定）、`Sec-MS-GEC` v2 令牌移植、以及三处会让失败退化成「永久转圈」的缺陷修复。

**② 有没有引用了已经不存在的文件或函数**

**无**。逐项核对通过——`POST /api/tts/generate`、`/api/tts/stream/` 路由 ✓（`handler.go:615,616`）；`speech.SpeechProvider` / `CLISpeechProvider` / `CacheIdentityProvider.CacheIdentity()` ✓（`internal/speech/interface.go:7,38,41`；`common_tts.go:67`）；`summarize.StripMarkdownStats` / `Summarizer` / `ShortTextThreshold = 300` ✓（`internal/summarize/strip_markdown.go:110`、`summarizer.go:112,155`）；`summarize.auto_junk_ratio` ✓（`internal/model/config.go:357`，白名单 `settings.go:120`）；`tts.openai.stream_mode` ✓（`settings.go:109`）；`ai_summary` 配置段 ✓（`config.go:150`）；`speakerName` 请求字段 ✓（`internal/handler/tts.go:93`）；`job.AudioCh` / `job.Done` ✓（`internal/service/tts_runtime.go:27,29`）；缓存上限 100（`model.TTSMaxCacheFiles` 默认 100，`config.go:487`；`EvictTTSCache` ✓）；`edge_tts_drm.go` / `edge_tts_drm_test.go` ✓（`internal/speech/`）且注释确认 v2 格式 `<16hex>-<64hex>` + `Sec-MS-GEC-Version: 2-<ver>` ✓、参考实现为 Python `edge-tts` 的 `generate_sec_ms_gec_v2` ✓；`/v1/audio/speech` + `stream_format` + `speech.audio.delta/done` ✓（`internal/speech/openai_tts.go`）；引擎实现文件 `edge_tts.go` / `openai_tts.go` / `piper.go` / `kokoro.go` / `moss_tts_nano.go` ✓。

**③ 建议怎么改**

无需修改。

---

#### `docs/spec/features/usage-stats.md`

- **行数**：83 ｜ **类别**：规格（权威）
- **标题**：智能体用量统计

**① 这份文档讲什么**

智能体用量统计——把 ACP `_meta` 归一化后按行写入 `chat_metadata` 台账，服务端按维度/时间窗聚合，前端用环形图/柱状/饼/趋势呈现。覆盖用量行独立台账（去级联外键、冗余项目与维度列）、一轮多次模型调用必须逐次累加（CodeBuddy 按 `codebuddy.ai/messageId` 去重求和）、`cost_usd` 存增量 / `cost_session_total` 存累计、`total ≥ input+output` 下界、群聊用量归发言者、以及 `scope=all` 的跨项目语义。

**② 有没有引用了已经不存在的文件或函数**

**无**。逐项核对通过——`chat_metadata` 表全部被引列存在（`input_tokens`/`output_tokens`/`total_tokens`/`cache_hit_tokens`/`cache_miss_tokens`/`cost_usd`/`cost_session_total`/`project_id`/`backend`/`agent_id`/`clawbench_session_id`/`response_model_id`/`created_at`，`internal/service/database.go:765-809`）；两个索引 `idx_chat_metadata_created` / `idx_chat_metadata_project_created` ✓（:812-813）；`service.SaveMetadata` ✓（`internal/service/chat.go:1914`）；`GET /api/usage/stats` ✓（`handler.go:527`）；`middleware.IsAuthenticated` / `IsLocalhost` ✓（`internal/middleware/auth.go:60,17`）；`/cb-usage` ✓（`internal/handler/clawbench_command.go:22`）；`maxUsageDims = 4`、`maxUsageLimit = 200`、`maxRangeDays = 370`、`defaultTrendTopN = 10` ✓（`internal/service/usage_stats.go:188-196`）；前端 `useUsageStats`（模块级单例 + 300ms 防抖 + AbortController + 项目切换 reset）✓、`statsChart.ts` / `UsageChart.vue` / `UsageStatsPanel.vue` 的 `filteredRows` ✓、`<460px` 纵向堆叠 ✓；`codebuddy.ai/messageId` ✓（`internal/ai/acp_events.go:71`）；`acp-go-sdk v0.13.5` 的 `Cost.Amount` 注释「Total cumulative cost for session」✓（`types_gen.go:1636`）；`total_cost_usd` ✓（`internal/ai/stream_parser.go:67`）。

**③ 建议怎么改**

无需修改。可选：把「维度上限随之从 3 提到 4」补上常量名 `maxUsageDims`（`internal/service/usage_stats.go:196`），便于读者定位。

---

#### `docs/spec/infra/auth-and-middleware.md`

- **行数**：100 ｜ **类别**：规格（权威）
- **标题**：认证与中间件

**① 这份文档讲什么**

密码认证 + 本机 AI 短时签名令牌的信任边界设计，以及全局中间件链与按路由鉴权。

主要章节：流程图、#请求中间件链、#认证决策流程、功能与设计要点、#功能清单、#设计要点、#刻意免鉴权的端点清单、#已知限制：隧道场景下的信任边界

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - 「`POST /api/settings/password`」→ 实际是 **`POST /api/config/password`**。验证：`grep -n 'register("/api/config/password"' internal/handler/handler.go` = 476；`grep -rn "config/password" web/src/components/settings/PasswordChangeDialog.vue` = 同路径。`/api/settings/password` 在代码中不存在。
  - 其余正确：`agent_api_keys`/`crypto.go` 已移除的表述与代码一致（`ls internal/service/crypto.go` 不存在）。

**③ 建议怎么改**

- 按上节「过时线索」逐条改：路径改成实际位置、符号改成实际名称；若该目标确实从未落地，改写为「曾计划但未实现」的显式声明。

---

#### `docs/spec/infra/bugfix-workflow.md`

- **行数**：67 ｜ **类别**：规格（权威）
- **标题**：Bugfix 工作流（GitHub Issues）

**① 这份文档讲什么**

自动 bugfix 生命周期（分类 → worktree 修复 → 测试 → PR）及其质量门禁。

主要章节：流程图、#Bugfix 生命周期、功能与设计要点、#功能清单、#标签体系、#设计要点

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/spec/infra/cli-reference.md`

- **行数**：47 ｜ **类别**：规格（权威）
- **标题**：CLI 子命令

**① 这份文档讲什么**

二进制双形态（无子命令起服务 / 带子命令作工具），当前仅剩 `upgrade-replace`。

主要章节：流程图、#upgrade-replace 执行链路、功能与设计要点、#功能清单、#设计要点、配置路径查找

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时（`internal/cli` 中确无 task/rag 子命令）。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/spec/infra/config-and-discovery.md`

- **行数**：182 ｜ **类别**：规格（权威）
- **标题**：配置与自动发现

**① 这份文档讲什么**

零配置启动、DB-backed Agent 与双传输后端、模型发现注册表、自定义 ACP 智能体与项目 id 化。

主要章节：流程图、#启动时自动发现流程、#模型来源与解析、功能与设计要点、#功能清单、#设计要点、迁移台账（schema_migrations）

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时（文中「`agent_api_keys` 表和 `crypto.go` 已移除」的表述与代码一致；`/api/agents/rescan` 存在，为 `POST` 子路径分发 `internal/handler/agent.go:76`）。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/spec/infra/docker-deployment.md`

- **行数**：46 ｜ **类别**：规格（权威）
- **标题**：Docker 部署

**① 这份文档讲什么**

单二进制 + 嵌入前端的 Docker 镜像、数据卷持久化与 GHCR 发布、容器内升级策略。

主要章节：流程图、#Docker 部署流程、#容器升级流程、功能与设计要点、#功能清单、#设计要点

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/spec/infra/event-system.md`

- **行数**：83 ｜ **类别**：规格（权威）
- **标题**：事件体系

**① 这份文档讲什么**

WS Manager/StreamHub 事件体系，会话级扇出 vs 广播、缓冲回放与投递丢弃计数。

主要章节：流程图、#事件从产生到推送、#投递失败不再是静默的、#客户端生命周期、功能与设计要点、#功能清单、#设计要点

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/spec/infra/i18n.md`

- **行数**：40 ｜ **类别**：规格（权威）
- **标题**：国际化（i18n）

**① 这份文档讲什么**

后端 go-i18n 错误/通知本地化，`X-Locale`/Cookie/`Accept-Language` 优先级链与推送通知独立 Localizer。

主要章节：流程图、#语言解析优先级、功能与设计要点、#功能清单、#设计要点

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时（`internal/i18n/locales/active.{en,zh}.yaml` 存在）。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/spec/infra/local-file-serving.md`

- **行数**：51 ｜ **类别**：规格（权威）
- **标题**：本地文件服务

**① 这份文档讲什么**

`/api/fs/raw/` 受控文件 URL 服务，含路径编码、项目外授权、目录拒绝与响应类型处理。

主要章节：流程图、#本地路径到浏览器响应、功能与设计要点、#功能清单、#设计要点

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/spec/infra/proxy.md`

- **行数**：62 ｜ **类别**：规格（权威）
- **标题**：代理注册表

**① 这份文档讲什么**

ProxyRegistry 端口注册/健康检查/白名单/保留端口与反向映射 Active 状态，SSH 与 h2 共用。

主要章节：流程图、#反向代理请求流程、#端口映射流程、功能与设计要点、#功能清单、#设计要点

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/spec/infra/self-upgrade.md`

- **行数**：89 ｜ **类别**：规格（权威）
- **标题**：应用自升级

**① 这份文档讲什么**

Web 内版本检查与二进制替换（备份、进度、断线轮询、升级后确认）及 Android 版本不匹配拦截。

主要章节：流程图、#从版本检查到服务恢复、功能与设计要点、#功能清单、#校验策略：能校验就校验，校验不了由用户决定、#设计要点

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时（`/api/desktop/latest`、`desktop_upgrade.go` 与 Android `VersionCompare` 均在）。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/spec/infra/ssh-tunnel.md`

- **行数**：369 ｜ **类别**：规格（权威）
- **标题**：SSH 隧道

**① 这份文档讲什么**

SSH 通道与 HTTP/2 流隧道两种端口转发传输，共用模型/白名单/守卫，含半关闭语义差异与保留端口规则。

主要章节：流程图、#SSH 隧道端口映射流程、#SSH 状态查询（非事件推送）、功能与设计要点、#功能清单、#方向语义、#端口被占用时的处理、#端点按受众拆分

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时（`internal/tunnel/` 各文件、`port_forward.transport` 恒为 `both`、`shouldCreateProxyRegistry` 恒 true 等论断均与代码一致）。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `RequestDuplex` （第 167 行） — 类型：符号

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

---

#### `docs/spec/infra/system-resources.md`

- **行数**：67 ｜ **类别**：规格（权威）
- **标题**：系统资源监控

**① 这份文档讲什么**

同 features 版的后端视角——`MetricsPusher` 按需采样、500ms 缓存、遥测非缓冲投递与客户端速率声明。

主要章节：流程图、#后端按需采样与推送、#前端订阅与声明、功能与设计要点、#功能清单、#设计要点

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时（与 features 版内容重叠，二者均与实现一致）。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/spec/infra/versioning.md`

- **行数**：268 ｜ **类别**：规格（权威）
- **标题**：版本号策略

**① 这份文档讲什么**

versionName/versionCode 双轨策略、versionCode 位宽公式、tag 推导路径、发布资产命名与桌面载荷包分发。

主要章节：versionCode 公式、#位宽取值、#两条取值路径、#tag 排序、#失败行为、为什么不能用 `git rev-list --count HEAD`、#仅换 `git describe` 还不够、流程图

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时（`scripts/lib/version-code.sh`、`scripts/__tests__/versionCode.test.ts`、`desktop/scripts/stage-payload.mjs`、`shell-fingerprint.mjs`、`.github/workflows/{ci,release}.yml` 均存在；`internal/service/desktop_upgrade.go` 的 tag 拼名与文档一致）。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `ProductVersion` （第 188 行） — 类型：符号

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

### 用户手册 `docs/user-guide/`（面向使用者）

---

#### `docs/user-guide/DINGTALK_PUSH.md`

- **行数**：154 ｜ **类别**：用户手册（面向使用者）
- **标题**：钉钉机器人推送配置

**① 这份文档讲什么**

钉钉机器人推送的创建步骤（Stream 模式）与 `/ls`/`@会话ID` 用法。

主要章节：在界面中配置与使用、配置步骤、#1. 创建钉钉应用、#2. 开启机器人能力、#3. 配置事件订阅、#4. 权限配置、#5. 发布应用、#6. 在 ClawBench 中配置

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/FAQ.md`

- **行数**：152 ｜ **类别**：用户手册（面向使用者）
- **标题**：常见问题（FAQ）

**① 这份文档讲什么**

高频问题解答：多后端列表、API Key 说明、TTS 本地模型、多实例、密码找回、数据存储路径、HTTPS 证书、STT、升级提示等。

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - 文档说「支持十八种后端」列出了 18 个名字，当前 `backends/` 目录下仍有 18 个后端子包（不含 custom-acp），列表准确。
  - 文档说密码在 `config/config.yaml` 的 `password` 字段，这是正确的第一优先路径（`~/.clawbench/config/config.yaml`）。
  - `auto-password` 文件路径描述正确。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `config/config.example.yaml` （第 42 行） — 类型：路径

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

---

#### `docs/user-guide/FEISHU_PUSH.md`

- **行数**：156 ｜ **类别**：用户手册（面向使用者）
- **标题**：飞书机器人推送配置

**① 这份文档讲什么**

飞书机器人推送的创建步骤（WebSocket 长连接）与 `/ls`/`@会话ID` 用法。

主要章节：在界面中配置与使用、配置步骤、#1. 创建飞书应用、#2. 开启机器人能力、#3. 配置事件订阅、#4. 权限配置、#5. 发布应用、#6. 在 ClawBench 中配置

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/LIVE_UPDATES.md`

- **行数**：79 ｜ **类别**：用户手册（面向使用者）
- **标题**：Android 16 灵动岛（Live Updates）

**① 这份文档讲什么**

Android 16 Live Updates（灵动岛）的技术规格、各厂商命名、系统要求与 ClawBench 实现细节。

主要章节：是什么、各厂商的叫法、技术要点、#成为 Live Update 的硬性条件、#关键 API、#权限（重点）、#ClawBench 实现、相关代码

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `ProgressStyle` （第 36 行） — 类型：符号
- `MetricStyle` （第 36 行） — 类型：符号
- `CallStyle` （第 36 行） — 类型：符号
- `FLAG_ONGOING_EVENT` （第 39 行） — 类型：符号

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

---

#### `docs/user-guide/PUBLIC_ACCESS.md`

- **行数**：254 ｜ **类别**：用户手册（面向使用者）
- **标题**：公网访问 ClawBench

**① 这份文档讲什么**

公网访问 ClawBench 的两种方式：IPv6 直连与 EasyTier 去中心化组网；FRP 已被移除。

主要章节：方式一：IPv6 公网直连、#1. 确认 IPv6 支持、#2. 配置路由器、#3. 启动 ClawBench、#4. 访问、方式三：EasyTier 去中心化组网、#特点、#架构说明

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时。FRP 移除已明确注明，PUBLIC_ACCESS.md 中间有一个「方式二」断档直接跳到「方式三」（EasyTier），流程图也未标注「方式二」，但内容本身准确。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `PPPoEv6` （第 49 行） — 类型：符号
- `DHCPv6` （第 50 行） — 类型：符号
- `Passthrough` （第 52 行） — 类型：符号

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

---

#### `docs/user-guide/RAG.md`

- **行数**：135 ｜ **类别**：用户手册（面向使用者）
- **标题**：RAG 历史记忆部署指南

**① 这份文档讲什么**

RAG 部署：搜索抽屉、索引参数、嵌入服务配置（Ollama/OpenAI 兼容）、HTTP API 用法、故障排查。

主要章节：在界面中使用、#设置：会话搜索、#搜索抽屉、系统架构、嵌入服务、配置、索引与搜索、删除与维护

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时。`config/config.yaml` 路径描述正确（代码优先读取 `<DataDir>/config/config.yaml` 即 `~/.clawbench/config/config.yaml`）。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/README.md`

- **行数**：181 ｜ **类别**：用户手册（面向使用者）
- **标题**：ClawBench 使用手册

**① 这份文档讲什么**

ClawBench 用户手册的总索引，列出所有章节及对应文件路径。

主要章节：1. 登录、2. 选择项目、3. 界面总览、4. 发起第一次对话、5. AI 对话、6. AI 群聊、7. 会话管理、8. 文件管理

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/TERMINAL.md`

- **行数**：464 ｜ **类别**：用户手册（面向使用者）
- **标题**：Web 终端使用指南

**① 这份文档讲什么**

无此文件（不存在 `terminal.md`）。

主要章节：目录、界面与入口、快捷指令、拖拽上传、移动端：标签与手势、交互式终端、多会话并发、虚拟按键栏

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - `docs/user-guide/terminal.md` **不存在**，实际文件为 `docs/user-guide/TERMINAL.md`（全大写）。README.md 正确引用 `TERMINAL.md`，但 `docs/spec/features/terminal.md` 引用小写 `terminal.md`（该路径本身是 spec 文件，非用户手册，不在本次审计范围）。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `ENV_VAR` （第 251 行） — 类型：符号

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

---

#### `docs/user-guide/TERMUX.md`

- **行数**：85 ｜ **类别**：用户手册（面向使用者）
- **标题**：在 Termux（Android）上完全运行 ClawBench

**① 这份文档讲什么**

在 Android Termux 中完整运行 ClawBench 的步骤：依赖安装、二进制安装/源码构建、访问方式。

主要章节：前置条件、一、安装依赖、二、安装 ClawBench、三、运行、#访问方式、#验证安装、四、可选：从源码构建、注意事项

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/TTS.md`

- **行数**：305 ｜ **类别**：用户手册（面向使用者）
- **标题**：TTS 语音合成部署指南

**① 这份文档讲什么**

TTS 语音合成部署：Edge / OpenAI / Piper / Kokoro / MOSS-TTS-Nano 五大引擎的配置与音色；`summarize.tts_backend` 的 simple / api / auto 三种后端。

主要章节：在界面中使用、TTS 引擎、总结后端、文本处理参数、Edge TTS（免费，需网络）、OpenAI 兼容 TTS（需网络）、Piper（本地离线，极速）、#安装步骤

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `WeTextProcessing` （第 225 行） — 类型：符号

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

---

#### `docs/user-guide/android-app.md`

- **行数**：187 ｜ **类别**：用户手册（面向使用者）
- **标题**：Android App 专有功能

**① 这份文档讲什么**

Android App 专有能力：悬浮状态窗、灵动岛（Live Updates）、原生推送、屏幕常亮与后台端口转发、国产 ROM 自启动白名单、终端音量键映射、重配服务器与 APK 自更新。

主要章节：安装与连接服务器、悬浮状态窗、灵动岛、#各厂商的叫法、#权限、#ClawBench 的表现、原生推送、屏幕常亮与后台端口转发

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/annotation.md`

- **行数**：146 ｜ **类别**：用户手册（面向使用者）
- **标题**：标注与跳转系统

**① 这份文档讲什么**

路径/commit/localhost/裸域名的自动标注与点击跳转、代码链接预览浮层、失效路径搜索、流式期间不标注等管道行为。

主要章节：标注类型总览、文件路径标注、代码链接预览、失效路径、Commit hash 标注、Worktree 路径标注、裸域名链接、外链的点击行为

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/chat.md`

- **行数**：500 ｜ **类别**：用户手册（面向使用者）
- **标题**：AI 对话

**① 这份文档讲什么**

AI 对话的全操作手册：输入栏、斜杠命令、附件引用、消息操作、语音输入、审批、提问卡、自动巡航、区域批注、快捷发送、消息聚类。

主要章节：输入栏、#顶部操作条、#外部会话同步与恢复、#输入区、#底部信息行、斜杠命令、井号技能引用、快捷发送

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时。「同步外部会话」按钮在 `ChatInputBar.vue:37` 存在，`/cb-task` 命令存在，均为有效内容。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/context.md`

- **行数**：49 ｜ **类别**：用户手册（面向使用者）
- **标题**：上下文、摘要与分叉

**① 这份文档讲什么**

上下文压缩、摘要生成、分叉与回溯（Rewind）的区别、续接与会话设置抽屉里的可调项。

主要章节：1. 上下文压缩、2. 摘要、3. 分叉与回溯、4. 续接、5. 相关可调设置

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时。「系统提示间隔」在 `settingsFieldMap.ts` 中仍存在（`chat.system_prompt_interval`）。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/desktop.md`

- **行数**：104 ｜ **类别**：用户手册（面向使用者）
- **标题**：桌面客户端（Windows / macOS / Linux）

**① 这份文档讲什么**

Electron 桌面客户端：下载渠道、窗口控制（无边框）、系统通知（最小化触发）、版本一致性门禁、侧装+指针自升级、多服务器登录与 safeStorage、自签名证书处理。

主要章节：下载与安装、#从哪里下载、#支持平台、窗口控制、原生通知、版本一致性门禁、自升级（侧装 + 指针）、多服务器与登录

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时。`~/.clawbench-desktop/` 路径描述与 `install.ts` 代码一致。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/file-manager.md`

- **行数**：230 ｜ **类别**：用户手册（面向使用者）
- **标题**：文件管理

**① 这份文档讲什么**

文件浏览、搜索（递归/全局/内容 grep）、多选、网格视图、右键/长按菜单、预览窗格、拖拽上传、项目外目录浏览。

主要章节：工具栏、浏览项目根之外的目录、排序、搜索、#按内容搜索（grep）、多选、网格视图、右键菜单与长按菜单

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/file-viewer.md`

- **行数**：394 ｜ **类别**：用户手册（面向使用者）
- **标题**：文件查看与编辑

**① 这份文档讲什么**

各类文件的查看与编辑：Markdown、代码（CodeMirror）、图片灯箱、音频/视频、PDF、Office、Excalidraw、OpenAPI；代码切片浮层、差异高亮、分享链接、导出 HTML。

主要章节：支持的文件类型总览、Markdown、代码、图片与灯箱、音频、视频、PDF、Word 文档

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/getting-started.md`

- **行数**：193 ｜ **类别**：用户手册（面向使用者）
- **标题**：快速上手

**① 这份文档讲什么**

从登录到发出第一条消息的完整入门指引，含界面布局、Dock 页签、项目切换和对话发起流程。

主要章节：1. 登录、2. 选择项目、3. 界面总览、#桌面端：三栏布局、#移动端：底部 Dock、#移动端：溢出菜单、#移动端：全屏面板与二级抽屉、4. 发起第一次对话

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - 文档说「Dock 的九个页签」并列出 9 行表格，实际 `DOCK_TABS` 有 **10** 个页签（`browse`、`view`、`history`、`forge`、`tasks`、`terminal`、`stats`、`proxy`、`notifications`、`settings`），表格**缺「通知中心」**（位于 `proxy` 与 `settings` 之间）。
  - 验证：`web/src/composables/dockTabs.ts` 的 `DOCK_TABS` 数组实测 10 项。

**③ 建议怎么改**

- 按上节「过时线索」逐条改：路径改成实际位置、符号改成实际名称；若该目标确实从未落地，改写为「曾计划但未实现」的显式声明。

---

#### `docs/user-guide/git.md`

- **行数**：108 ｜ **类别**：用户手册（面向使用者）
- **标题**：Git 管理

**① 这份文档讲什么**

Git 提交历史、分支管理、标签、工作树、Diff 抽屉，以及单文件历史。

主要章节：提交历史、#提交详情、#单文件历史、分支、标签、工作树、Diff

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/group-chat.md`

- **行数**：391 ｜ **类别**：用户手册（面向使用者）
- **标题**：AI 群聊

**① 这份文档讲什么**

AI 群聊的两种模式（主持人/自由）、自动巡航、@ 成员卡与密送、成员管理、上下文压缩与通知。

主要章节：1. 两种模式、2. 智能体从哪来、#2.1 自动扫描：装了什么 CLI，就有什么智能体、#2.2 复制智能体：造出「有角色设定」的成员、#2.3 在设置里管理智能体、#2.4 一句话总结、3. 建群、4. 群界面

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - 文档描述「设置 → 聊天 → 群聊 → 用户昵称」路径，实际 key 为 `chat.user_nickname`，sectionHeader 为 `groupChatSectionHeader`，"群聊"是 section 标题而非独立子页签，但功能位置一致，不构成严重过时。

**③ 建议怎么改**

- 按上节「过时线索」逐条改：路径改成实际位置、符号改成实际名称；若该目标确实从未落地，改写为「曾计划但未实现」的显式声明。

---

#### `docs/user-guide/notification-center.md`

- **行数**：75 ｜ **类别**：用户手册（面向使用者）
- **标题**：通知中心

**① 这份文档讲什么**

通知中心的列表结构、筛选、跳转（含跨项目确认框）、刷新清空与保留期限（30 天/500 条）。

主要章节：打开、列表、筛选、点击跳转、刷新与清空、保留期限、常见问题

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/parallel-workflow.md`

- **行数**：386 ｜ **类别**：用户手册（面向使用者）
- **标题**：多线并行工作方式

**① 这份文档讲什么**

多线并行工作方式的思维转变、多会话/多 Worktree/多项目机制、上下文投喂（引用与检索）、任务自动化与并行纪律。

主要章节：一、为什么要多线并行、二、思维方式的四个转变、#2.1 把工作全部纳入 AI 体系、#2.2 借 AI 的眼睛看代码、#2.3 把执行交给 AI，你审证明、#2.4 不懂就学：AI 输出不是黑箱、三、单人多线并行：四个机制、#3.1 多会话

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/projects.md`

- **行数**：64 ｜ **类别**：用户手册（面向使用者）
- **标题**：多项目与 Worktree

**① 这份文档讲什么**

多项目隔离、worktree 作为独立项目、跨项目会话、设置页项目注册表管理。

主要章节：1. 多项目概念、2. 切换项目、3. Worktree 作为独立项目、4. Worktree 路径标注、5. 跨项目会话、6. 管理项目（设置 → 项目）

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/pwa-apk.md`

- **行数**：30 ｜ **类别**：用户手册（面向使用者）
- **标题**：网页移动端：PWA 安装与 APK 下载

**① 这份文档讲什么**

PWA 安装（Chrome + iOS Safari 手动添加）与 APK 下载入口（设置 → 关于）。

主要章节：PWA 安装（推荐）、APK 下载

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时。APK 下载入口在设置中仍然存在。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/session.md`

- **行数**：248 ｜ **类别**：用户手册（面向使用者）
- **标题**：会话管理

**① 这份文档讲什么**

会话的增删改查、标签管理、搜索、分叉回溯与分享。

主要章节：会话列表与侧栏、#桌面端：会话侧栏、#移动端：会话列表、会话搜索、#桌面端、#移动端、会话标签、#设置标签

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/settings.md`

- **行数**：420 ｜ **类别**：用户手册（面向使用者）
- **标题**：设置

**① 这份文档讲什么**

全部设置分类的逐项说明，含项目、外观、文件、聊天、智能体、技能、终端、语音朗读、语音识别、AI 摘要、会话搜索、端口映射、GitHub/GitLab 集成、推送通知、安全、调试、关于。

主要章节：项目、外观、文件、聊天、智能体、#添加内置列表之外的智能体（自定义 ACP）、#集成测试（自检）、技能

**② 有没有引用了已经不存在的文件或函数**

- ⚠️ **发现过时线索**：
  - 文档说「设置首页按两列列出 **18** 个分类」，实际 i18n `settings.categories` 有 **20** 个 key（新增 `mcp`、`usageMeter`；`内网穿透` 已移除），文档未提及这两个新分类。
  - 验证：`web/src/i18n/locales/zh.ts` 的 `settings.categories` 下实测 20 个一级 key。

**③ 建议怎么改**

- 按上节「过时线索」逐条改：路径改成实际位置、符号改成实际名称；若该目标确实从未落地，改写为「曾计划但未实现」的显式声明。

---

#### `docs/user-guide/shortcuts.md`

- **行数**：120 ｜ **类别**：用户手册（面向使用者）
- **标题**：快捷键与手势速查

**① 这份文档讲什么**

桌面端键盘快捷键汇总（聊天、文件管理器、代码查看器、终端、应用级）与移动端手势速查。

主要章节：桌面端键盘快捷键、#聊天、#文件管理器、#代码查看器、#终端、#桌面端应用级快捷键（Electron 壳）、移动端手势、终端手势

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/stats.md`

- **行数**：68 ｜ **类别**：用户手册（面向使用者）
- **标题**：数据统计

**① 这份文档讲什么**

用量统计（按时间/模型/后端）、代码存量（按语言）、代码增量（按作者）、系统资源监控。

主要章节：用量统计、代码存量、代码增量、系统资源监控

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/user-guide/tasks.md`

- **行数**：205 ｜ **类别**：用户手册（面向使用者）
- **标题**：任务

**① 这份文档讲什么**

定时任务与事件任务的新建、表单字段、门控脚本、Forge 集成（GitHub/GitLab）、会话搜索（RAG）入口。

主要章节：任务列表、定时任务、事件任务、#事件变量、执行详情、Forge 集成（GitHub / GitLab）、会话搜索（RAG）、门控脚本

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时。`/cb-task` 命令在代码中仍然存在。

**③ 建议怎么改**

- 未发现需要修改之处。

### 探针记录 `docs/dev/`（外部 CLI 实测）

---

#### `docs/dev/acp_go_sdk_decision.md`

- **行数**：165 ｜ **类别**：开发探针记录
- **标题**：ACP Go SDK 选型与旁路策略（决策记录）

**① 这份文档讲什么**

记录 2026-10-02 评估 `github.com/coder/acp-go-sdk v0.13.5` 是否 fork/换库（结论：维持现状 + 自研 stdout tee 旁路），并列出代码规模、替代库对比、重新评估的触发条件与 fork 姿势。

**② 有没有引用了已经不存在的文件或函数**

① 内部实现引用全部有效（`internal/ai/acp_raw_notification.go`、`acp_stdout_filter.go`、`acp_raw_notification_probe_integration_test.go` 存在），`go.mod` 仍是 v0.13.5、`go 1.25.0` ✓。② 外部类：第 48 行 `` `cmd/generate/` ``、第 52 行 `` `schema.json` ``、第 60 行 `` `agentclientprotocol/agent-client-protocol` `` 指的是**上游 SDK 仓库**的路径，但表头只写「部分」，本仓库 `cmd/` 下真有一个 `cmd/`（acp-mock/backfill-credit/server），极易被读成本仓库路径。③ 第 84 行「69 个文件、157 个 `acp.*` 符号」已漂移：现为 85 个 Go 文件引用该 SDK、165 个不同的 `acp.*` 符号。

**③ 建议怎么改**

第 3 节的表头改成「上游 `coder/acp-go-sdk` 仓库的目录构成」，把 `cmd/generate/` 写成 `` `coder/acp-go-sdk` 的 `cmd/generate/` ``（或加一句「以下路径均相对上游仓库」）；把「69 文件 / 157 符号」更新为「85 文件 / 165 符号（含测试）」或改写成不易漂移的表述；§7 的 `third_party/acp-go-sdk/` 段落保留，它是明确的未来计划。

---

#### `docs/dev/acp_mcp_probe_results.md`

- **行数**：232 ｜ **类别**：开发探针记录
- **标题**：ACP `mcpServers` 注入实测结论（2026-10-19）

**① 这份文档讲什么**

2026-10-19 的真机探针结论——17 个 ACP 后端对 `session/new` 的 `mcpServers` 支持情况、HTTP/SSE 传输能力位、恢复路径（resume/load）是否保留配置、以及坏 MCP server 不会毒化会话的失败行为。

**② 有没有引用了已经不存在的文件或函数**

第 11-14 行的四处行号全部漂移：`acp_conn_lifecycle.go:308`（session/new）→ 实际 **332**；`:441`（session/load）→ 实际 **497**；`:501`（session/resume）→ 实际 **557**；`acp_pool.go:1614`（SyncLoadSession）→ 实际 **1647**。第 64 行 `` `acp_conn_lifecycle.go:697-712` ``（`--mcp-config` workaround）→ 实际在 **763-777**；第 75 行 `` `acp_conn_lifecycle.go:846-857` ``（只提取 listSessions/deleteSession）→ 实际在 **909-915**。表头「17 个声明了 `AcpCommand` 的后端」现为 **18**（`internal/ai/backends/dsh/register.go` 等新增）。探针文件与 guard（`acp_mcp_probe_integration_test.go`、`internal/ai/testdata/mcp_probe_server.mjs`、`mcp_probe_table_test.go`）均存在 ✓。

**③ 建议怎么改**

把 6 处行号改成实际值（或直接删掉行号只留函数名，避免再次漂移）；表头 17→18 并补 `dsh`；第 4 节「直接对设计的直接影响」是尚未落地的实现建议，建议加「截至本文写作时未实现」的状态标记。

---

#### `docs/dev/acp_provider_routing.md`

- **行数**：142 ｜ **类别**：开发探针记录
- **标题**：跨厂商模型切换（issue #340）— 能力探测与方案（未实现，issue 已关闭）

**① 这份文档讲什么**

issue #340「跨厂商换模型」的能力探测记录与落地方案，结论是**不做**（各后端 provider 配置与 ClawBench 解耦），文件作为调研归档保留。

**② 有没有引用了已经不存在的文件或函数**

① **外部 SDK 类，第 60 行 `` `conn.UnstableSetProvider(...)` `` 属实**——`acp-go-sdk@v0.13.5/client_gen.go:260` 确有 `(*ClientSideConnection).UnstableSetProvider`，且 `ACPConn.conn` 就是 `*acp.ClientSideConnection`（`internal/ai/acp_pool.go:944`）；文档「typed 方法因 `id` vs `providerId` 直接失败」的结论也被 `internal/ai/acp_provider_routing_probe_integration_test.go` 实证。**不是死引用**。② 但同段三处行号漂移：第 27 行 `internal/ai/acp_conn_prompt.go:92`（set_config_option 实际在 **508-525**）、第 28 行 `acp_pool.go:1972`（`lastSetModel` 实际字段在 **acp_pool.go:1108**、读写在 **2138/2157**）。③ 第 102 行 `` `internal/ai/provider_routing.go` `` **在本仓库不存在**——它出现在 §4「若将来要做」的提案里，措辞已是「新增」，但读者容易当成现有文件。④ 第 114 行 `registry.go` 的 `LookupProviderRouter` 在 `internal/ai/backends/registry.go` **不存在**（提案）；`acp_wire.go` 存在于 `internal/ai/backends/acp_wire.go` ✓。

**③ 建议怎么改**

保留 `UnstableSetProvider` 的写法（外部 SDK 类，准确），只把 §1 的两处行号改为 `acp_conn_prompt.go:508` 与 `acp_pool.go:1108`；把 §4.2 的 `internal/ai/provider_routing.go` 与 `LookupProviderRouter` 明确标成「**待实现（本仓库暂无此文件/符号）**」，或整体加一个「本节为未实现提案」的醒目块。

---

#### `docs/dev/acp_system_prompt_injection.md`

- **行数**：217 ｜ **类别**：开发探针记录
- **标题**：各 ACP 智能体的「系统提示词注入」支持面调研

**① 这份文档讲什么**

逐后端调研「能否在会话创建时真正注入系统提示词」——结论是只有 claude（`_meta.systemPrompt`）与 grok（`_meta.systemPromptOverride`/`rules`）支持协议级注入，codebuddy/qodercli 只能靠进程级 CLI flag，其余 8 个只能文本前缀。

**② 有没有引用了已经不存在的文件或函数**

① **外部 SDK 类，第 167 行 `` `acp.NewSessionRequest` `` 属实**——`acp-go-sdk@v0.13.5/types_gen.go:3236` 有该类型，`:3242` 正是 `Meta map[string]any \`json:"_meta,omitempty"\``，与文档自述的 `types_gen.go:3242` 完全一致。**符号有效**。② 但同句的行号 `acp_conn_lifecycle.go:223` 错：223/224 是 `ensureAliveWithSession` 的注释与函数头，`acp.NewSessionRequest` 实际构造于 **acp_conn_lifecycle.go:332**。③ 第 11 行 `internal/ai/acp_conn_state.go:518` → 文本前缀注入实际在 **514**。④ 第 175 行 `internal/ai/acp_conn_lifecycle.go:508-513`（`--mcp-config` 先例）→ 实际 **763-777**。⑤ 第 179 行 `acp_pool.go:463`（会话键控）/`acp_pool.go:432`（`__list_sessions__:<agentID>`）→ 实际 `connKey := "__list_sessions__:" + agent.ID` 在 **acp_pool.go:495**。

**③ 建议怎么改**

修正 4 处行号（332 / 514 / 763-777 / 495），保留 `acp.NewSessionRequest` 与 `types_gen.go:3242` 的写法（外部 SDK 类，正确）；§0 表格列了 12 个后端，与 `internal/ai/backends/` 现有 18 个 ACP 后端不一致，建议补一句「本文只覆盖当时接入的 12 个」。

---

#### `docs/dev/claude_acp_extensions.md`

- **行数**：311 ｜ **类别**：开发探针记录
- **标题**：Claude ACP 扩展面 — 与 CodeBuddy 对比

**① 这份文档讲什么**

对比 Claude（官方 ACP 适配器桥接 Claude Agent SDK）与 CodeBuddy（CLI 自实现 ACP）的扩展面差异——Claude 几乎完全守规范，扩展面极小；重点是 Claude 的 `promptQueueing`（标准 `session/prompt` 并发排队）与 CodeBuddy 私有 `session/steer` 的路线差异。

**② 有没有引用了已经不存在的文件或函数**

① 第 163 行 `acp_conn_prompt.go:71`（`client.RegisterSession`）→ 实际 **acp_conn_prompt.go:83**；第 163 行 `acp_client.go:74`（`c.sessionRoutes[acpSessionID] = ch`）→ 实际 **acp_client.go:102**；第 164 行 `acp_client.go:91`（`delete(c.sessionRoutes, ...)`）→ 实际 **acp_client.go:130**。② 第 257 行 `acp_conn_lifecycle.go:587-588`（读 `initResp.AgentCapabilities.SessionCapabilities`）→ 实际 **acp_conn_lifecycle.go:914-915**。③ 第 6 行 `internal/ai/backends/claude/cli.go:19` 仍准确（该行正是 `AcpCommand: "npx -y @agentclientprotocol/claude-agent-acp@latest"`）✓。④ `internal/ai/acp_meta_claude.go` 存在且确实处理 `_meta.quota.token_count`（第 78-97 行）✓；`codebuddy_acp_ext_probe_integration_test.go` 存在 ✓。

**③ 建议怎么改**

修正 4 处行号；§6.1 说「未处理 `claudeCode.toolName` / `parentToolUseId` / `_claude/origin` / `terminal_output`」——请复核现状后加时间戳（这类「未处理」结论极易过期）；§7 的「是否值得为 Claude 做探针」是未做事项，标注为待办。

---

#### `docs/dev/claude_tool_call_samples.md`

- **行数**：1036 ｜ **类别**：开发探针记录
- **标题**：Claude Code Tool Call JSON Samples

**① 这份文档讲什么**

Claude Code CLI（`--print --output-format stream-json`）实际输出的 JSON 工具调用样例集，含与 CodeBuddy 的结构差异对照表。

**② 有没有引用了已经不存在的文件或函数**

**样本记录，无失效引用**。全文唯一与本仓库相关的字符串是第 179/191 行 JSON 里的 `"/home/xulongzhe/projects/clawbench/internal/ai"` 与 `"internal/ai/codex.go"`——`internal/ai/codex.go` 确实存在，但同批样例里并列的 `gemini.go` / `claude.go` / `codebuddy.go` / `opencode.go` **均已不存在**（现为 `codex.go`、`codex_tool.go`、`opencode_tool.go` 等）。

**③ 建议怎么改**

属样本归档，不改内容；若要修，把第 182 行 JSON 里已消失的文件名换成现存文件名（或在文件头加一句「文件列表为采样当时的快照」），避免后来者以为这些文件缺失。

---

#### `docs/dev/claude_tools-definition.md`

- **行数**：1229 ｜ **类别**：开发探针记录
- **标题**：ClawBench 工具调用定义 — 完整 JSON Schema

**① 这份文档讲什么**

本会话可用工具的完整 JSON Schema（Draft-07）定义——核心 Agent/任务管理、文件与代码编辑、搜索检索三大类。

**② 有没有引用了已经不存在的文件或函数**

**无失效引用**。全文是纯 JSON Schema 文档，不含任何仓库路径、Go/TS 符号或端点引用；工具名（`Task`/`Read`/`Write`/`Edit`/`Glob`/`Grep`/`Bash`/`WebFetch`/`TodoWrite`/`ExitPlanMode` 等）与当前 Claude Code CLI 工具集一致。

**③ 建议怎么改**

无需修改。可在文件头补一行「采样自 X 版本，仅作 schema 参考」，便于判断新旧。

---

#### `docs/dev/codebuddy_acp_extensions.md`

- **行数**：773 ｜ **类别**：开发探针记录
- **标题**：CodeBuddy ACP 非标准扩展接口清单

**① 这份文档讲什么**

CodeBuddy `codebuddy --acp` 在 ACP v1 之外的**四层**非标准扩展清单（`extMethod` 17 个 `_codebuddy.ai/*`、19 个私有 `session/*`、10 个 agent→client 通知、163 个 `_meta` key），并附裸 JSON-RPC 通道实测（steer 边界切分已落地）。

**② 有没有引用了已经不存在的文件或函数**

① **外部 SDK 类，第 90 行 `` `acp.ExtensionMethodHandler` `` 与第 415 行 `` `acp.NewClientSideConnection` `` 均属实**——`acp-go-sdk@v0.13.5/extensions.go:19` 有该接口、`client.go:16` 有该函数，文档关于「未实现 ExtensionMethodHandler ⇒ `_` 前缀 request 回 Method not found」的结论也被 `internal/ai/codebuddy_acp_ext_probe_integration_test.go` 证实。**不是死引用**。② 但第 415 行的前提**已过期**：「`cmd.StdinPipe()` 得到的 `stdinPipe` 目前只传给了 `acp.NewClientSideConnection`、未被保留，需将其存入 `ACPConn`」——实际 `c.stdin = stdinWriter`（**internal/ai/acp_conn_lifecycle.go:976**）已保留写锁，`internal/ai/acp_raw_rpc.go` 的裸 JSON-RPC 通道正是靠它实现（§9.7 也已把 `session/steer` 标为「优先接入」）。③ 第 6 行/附录 B 的分析版本 v2.147.0 比同目录 `codebuddy_acp_team_integration.md` 的 v2.161.1 旧。④ §6.8 断言「其余 150+ 键未被消费（含 `teamUpdate`、`historyReplay`）」——**两者现已消费**（`internal/ai/codebuddy_team_bridge.go`、`internal/ai/acp_client.go` 的 `ReplayEndSeen`）。⑤ 章节编号跳号：§9.4 之后直接是 §9.6，缺 §9.5。

**③ 建议怎么改**

把第 415 行改写为「stdin 写锁已通过 `ACPConn.stdin` 保留，裸 JSON-RPC 通道已由 `acp_raw_rpc.go` 实现（见 §9.6）」；§6.8 的「未被消费」列表移除 `teamUpdate`/`historyReplay` 并注明已接入；把分析版本更新到 v2.161.1 或标注「v2.147.0 快照，team 相关以 v2.161.1 为准」；补上缺失的 §9.5（或重编号）。

---

#### `docs/dev/codebuddy_acp_team_integration.md`

- **行数**：427 ｜ **类别**：开发探针记录
- **标题**：CodeBuddy ACP Agent Teams：协议实测与 ClawBench 集成方案

**① 这份文档讲什么**

CodeBuddy Agent Teams 的真机协议实测（`teamUpdate` / `memberEvent` / `memberName` 关联键 / 审批帧 `toolCall._meta`）与 ClawBench 端到端集成方案（后端 `team_update` 事件、WS 转发、`TeamPanel.vue` 展示）。

**② 有没有引用了已经不存在的文件或函数**

① 第 2 节「ClawBench 现状（缺口）」整表**已过期**——缺口 1-5 都已闭合（§5 标 ✅）：`acp_events.go:401` 实际是 `SessionConfigOption` select 分支、**不是**「`session_info_update` 死分支」；`acp_events.go:139/158/173` 实际**已**读 `memberEvent`（`memberParentOrParent` / `extractTeamMemberName`）；`acp_client.go:358+` 实际是 `isModelProgressUpdate`、**不是**权限帧 meta。② 第 117 行 `acp_codebuddy_tool.go`、第 229 行 `session_sync.go`、第 211/296/313 行 `ContentBlocks.vue`、第 247 行 `TeamPanel.vue`、第 249 行 `ChatPanelContent.vue`、第 250 行 `PlanPanel.vue`、第 291 行 `usePlanProgress.ts`、第 377 行 `useTeamState.ts` / `TeamPanel.test.ts` / `useTeamState.test.ts`、第 378-382 行 `renderToolDetail.test.ts` / `chatStreamUtils.test.ts` / `teamMemberColor.test.ts` / `acp_raw_notification_test.go`、第 387-397 行 `interface.go` / `acp_pool.go` / `acp_events.go` / `acp_tool.go` / `acp_debounce.go` / `acp_backend.go` / `acp_client.go` / `acp_conn_lifecycle.go` / `useChatStream.ts` / `useChatSession.ts` / `App.vue` —— 这些是**裸文件名**（无目录前缀），逐一在仓库中均能找到。③ **内部类，第 234 行 `` `event.Team` `` 属实**——`internal/ws/stream_hub.go:374-375` 正是 `case "team_update": return event.Team`，`ai.StreamEvent.Team *TeamState`（`internal/ai/interface.go:503`）存在。**不是死引用**；但同句说「`acpStatePayload` 加分支」不准——实现放在 `buildChatStreamMessage` 的独立 switch，不在 `acpStatePayload`（`stream_hub.go:446`）里。

**③ 建议怎么改**

把 §2 整节标注为「**接入前缺口（已全部闭合，保留作对照）**」并把三处行号更新为当前实现位置（`acp_events.go` 的 `session_info_update` 分支与 memberEvent 归因、`acp_client.go` 的 `RequestPermission`）；§3.4 改成「`buildChatStreamMessage` 增加 `case "team_update"` 分支」；其余文件引用保持（均有效），可补全目录前缀以便跳转。

---

#### `docs/dev/codebuddy_sdk_probe_results.md`

- **行数**：114 ｜ **类别**：开发探针记录
- **标题**：CodeBuddy Agent SDK 能力探针结果

**① 这份文档讲什么**

`@tencent-ai/agent-sdk@0.3.266` 的 62 能力点探针结果（57 通过 / 4 缺口 / 1 未验证），结论是 SDK 覆盖面够但**不建议作为第三种 transport**（steer 无等价物）。

**② 有没有引用了已经不存在的文件或函数**

**无失效引用**。`test/codebuddy-sdk/sdk_probe.mjs` ✓、`internal/ai/codebuddy_sdk_integration_test.go` ✓、`internal/ai/midturn.go` ✓、`internal/ai/acp_raw_rpc.go` ✓、`internal/ai/factory.go` ✓，末尾「相关」里指向 `docs/dev/codebuddy_acp_extensions.md` 的相对链接有效。文中 `lib/transport/process-transport.js:292-296`、`lib/_internal/query-controller.js:56-68` 属**外部包内路径**，成立。

**③ 建议怎么改**

§四「改动量评估（若要接入）」整节是未执行的提案，标注「**未实施（结论：不接入）**」；补一句结论日期，避免后来者以为 SDK 通道已存在。

---

#### `docs/dev/codebuddy_tool_call_samples.md`

- **行数**：1097 ｜ **类别**：开发探针记录
- **标题**：CodeBuddy Tool Call JSON Samples

**① 这份文档讲什么**

CodeBuddy CLI（`--print --output-format json`）每个工具调用的实际 JSON 样例，含双条目结构（tool_call + tool_result）说明。

**② 有没有引用了已经不存在的文件或函数**

**样本记录，无失效引用**。第 182 行 JSON 里的 `"internal/ai/opencode.go"`、`"internal/ai/gemini.go"`、`"internal/ai/claude.go"`、`"internal/ai/codebuddy.go"` 四个文件**当前都不存在**（现存的是 `opencode_tool.go`/`codex.go` 等），属采样当时的目录快照。

**③ 建议怎么改**

不改样例内容；可在文件头加一句「文件列表为采样快照，路径可能已变化」。

---

#### `docs/dev/codebuddy_tool_definitions.md`

- **行数**：797 ｜ **类别**：开发探针记录
- **标题**：Tool Call Structure Definitions

**① 这份文档讲什么**

CodeBuddy 工具的 JSON Schema 定义集（Agent、文件编辑、Bash、搜索等）。

**② 有没有引用了已经不存在的文件或函数**

**无失效引用**——纯 schema 文档，不含仓库路径或内部符号；工具名（`Agent`/`read_file`/`edit_file`/`shell_command`/`web_search` 等）与 `internal/ai/codebuddy_acp_extensions.md` 记录的 CodeBuddy 原生工具集一致。

**③ 建议怎么改**

无需修改；如需，文件头补采样版本与来源。

---

#### `docs/dev/codex_tool_call_samples.md`

- **行数**：387 ｜ **类别**：开发探针记录
- **标题**：Codex CLI Tool Call JSON Samples

**① 这份文档讲什么**

Codex CLI `exec --json` 的 JSONL 输出样例（每行一个带 `type` 的对象）。

**② 有没有引用了已经不存在的文件或函数**

**无失效引用**（纯样本，无仓库路径、无符号）。文档记录的命令行 `codex exec --json --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check -c model="MiniMax-M2.7"` 与当前 `internal/ai/backends/codex/` 的调用方式一致。

**③ 建议怎么改**

无需修改。

---

#### `docs/dev/codex_tool_definitions.md`

- **行数**：1293 ｜ **类别**：开发探针记录
- **标题**：Codex 工具调用 JSON Schema 定义

**① 这份文档讲什么**

Codex CLI v0.137.0 全部工具的 JSON Schema 定义，标注了上游 Rust 源码路径（`core/src/tools/handlers/...`、`multi_agents_v2/...`）作为交叉验证依据。

**② 有没有引用了已经不存在的文件或函数**

**无失效引用**——引用的 `core/src/...`、`multi_agents_v2/...` 全部是 **Codex 上游仓库**的路径，不是 ClawBench 的，故在仓库里查不到属正常。全文不含任何 ClawBench 内部路径或符号。

**③ 建议怎么改**

在文件头加一句「文中 `core/src/...` 等路径均相对 **openai/codex 上游仓库**，非 ClawBench」，避免读者误判为失效引用。

---

#### `docs/dev/commandcode_acp_probe_results.md`

- **行数**：359 ｜ **类别**：开发探针记录
- **标题**：Command Code ACP 接入方案（含 BYOK 免额度配方）

**① 这份文档讲什么**

`command-code@1.84.0` 的 ACP 接入实测（8/8 PASS）+ BYOK 免额度配方，列出四条硬约束（BYOK 靠 `config.json` 预置、cwd lease、无轮中注入、未登录是 `-32000`）。

**② 有没有引用了已经不存在的文件或函数**

① 第 114 行起的 §3.1「新增子包 `internal/ai/backends/commandcode/`」**方案已全部落地**——`internal/ai/backends/commandcode/{register.go,acp.go}` 存在且 `register.go` 的 `DefaultCmd: "cmdc"`(35)、`AltCmd: "commandcode"`(36)、`AcpCommand: "cmdc acp"`(39)、`ACPLoadSession: true`(42)、`SortOrder: 18`(52)、`NativeSkillsDirs: []string{".commandcode/skills", ".agents/skills"}`(59)、`AutoLoadsNativeSkills: true`(60) 与文档代码块**逐字一致**。② 第 293-300 行的「接入改动清单」也已完成：`cmd/server/main.go:32` 有 `_ "clawbench/internal/ai/backends/commandcode"`、`internal/ai/backends/native_skills_test.go:51` 有 `"commandcode": true`、`internal/model/discovery_test.go:33` 的 `expectedIDs` 含 `"commandcode"`、`web/src/utils/backendNames.ts:24` 有 `commandcode: 'Command Code'`、`web/src/utils/agentIcons.ts:76` 有 `commandcode` 图标。③ 第 245 行称 `web/src/utils/icons.ts` 无 `BashOutput` —— **属实**（grep 无命中），但这是修复前状态。④ 第 356 行 `` `resolveSkillLoadPlan` `` 在本仓库不存在——它指 **Command Code 包内** `cli.mjs` 的函数（`internal/ai/backends/native_skills_test.go:51` 注释里也这么引用），成立。

**③ 建议怎么改**

把 §3.1 与 §293-300 标注为「**已实施（2026-10-21 核对：与 `internal/ai/backends/commandcode/register.go` 一致）**」；§2.4 的方案 A/B/C 标注为已选方案；第 245 行「icons.ts 无 BashOutput ⇒ 扳手图标」补一句当前已如何处理。

---

#### `docs/dev/group_chat_prompts.md`

- **行数**：188 ｜ **类别**：开发探针记录
- **标题**：群聊提示词样例（原始文本）

**① 这份文档讲什么**

群聊全部提示词的原始字符串快照（主持人/成员/收尾/自由模式四段系统提示词 + 注入上下文 + i18n 系统行），由 Go 构建器直接生成。

**② 有没有引用了已经不存在的文件或函数**

① 第 27 行 `group_orchestrator.go:465` 指「群聊把 A/B 段拼成 user prompt」——465 行实际是 `announceUserTurn` 的注释块，非该逻辑点。② 第 291 行 `group_inject.go:292`（`participantHeader`）→ 实际 **group_inject.go:322**。③ 第 315 行 `group_inject.go:216`（`appendHostDirectives`）→ 实际 **group_inject.go:222**。④ 第 106/129/152/177 行的 `group_prompt.go:83 / 152 / 182 / 207` → **全部准确**（`BuildHostSystemPrompt`=83、`BuildMemberSystemPrompt`=152、`BuildHostSummaryPrompt`=182、`BuildFreeMemberSystemPrompt`=207）✓；`group_inject.go:97`（`renderTimelineLine`）也准确 ✓。⑤ §C 的 6 个 i18n key 在 `internal/i18n/locales/active.zh.yaml:227,228,238,239,243,244` 全部存在 ✓。

**③ 建议怎么改**

修正 3 处行号（465 → 指向真正拼接处；292→322；216→222）；文件头已声明「改动这些构建器后须重新生成本文档」——建议在文末补一句「最近一次核对：2026-10-21，提示词正文与源码一致」，让读者知道快照新鲜度。

---

#### `docs/dev/opencode_tool_call_samples.md`

- **行数**：389 ｜ **类别**：开发探针记录
- **标题**：OpenCode Tool Call JSON Samples

**① 这份文档讲什么**

OpenCode `run --format json` 的 JSONL 工具调用样例集。

**② 有没有引用了已经不存在的文件或函数**

**样本记录，无失效引用**。第 168/326 行 JSON 里的 `/home/xulongzhe/projects/clawbench/internal/ai` 是真实目录 ✓。

**③ 建议怎么改**

无需修改。

---

#### `docs/dev/opencode_tool_definitions.md`

- **行数**：435 ｜ **类别**：开发探针记录
- **标题**：OpenCode 工具调用 JSON Schema 定义

**① 这份文档讲什么**

OpenCode 工具的 JSON Schema 定义（Read/Write/Edit/Glob/Grep/Bash/TodoWrite/EnterPlanMode/ExitPlanMode/Skill/Task/WebFetch）。

**② 有没有引用了已经不存在的文件或函数**

**无失效引用**——纯 schema，无仓库路径/符号。工具名与 `internal/ai/opencode_tool.go` 的 ACP 映射面一致；`Skill` 工具与 `internal/skill/` 的技能注入机制对得上。

**③ 建议怎么改**

无需修改；可补采样来源与版本。

---

#### `docs/dev/pi_sdk_probe_results.md`

- **行数**：103 ｜ **类别**：开发探针记录
- **标题**：Pi Coding Agent SDK 能力探针结果

**① 这份文档讲什么**

`@earendil-works/pi-coding-agent@0.87.1` 的 45 能力点探针（45/45 全过），结论是 SDK 能力完整但需 Node sidecar，Pi 现已走 ACP 桥接，SDK 是第三条路径。

**② 有没有引用了已经不存在的文件或函数**

**无失效引用**。`test/pi-sdk/pi_sdk_probe.mjs` ✓、`internal/ai/pi_sdk_integration_test.go` ✓、`internal/ai/backends/pi/cli.go` ✓、`internal/ai/pi_stream.go` ✓、`internal/ai/pi_tool.go` ✓、末尾指向 `docs/dev/pi_tool_definitions.md` 与 `docs/dev/codebuddy_sdk_probe_results.md` 的相对链接 ✓。第 23 行 `docs/json.md`、第 24 行 `text_/thinking_/toolcall_`、第 32-37 行的 `thinking_start/delta/end`、`off/minimal/low/medium/high` 等均属 **Pi 包内**（外部），成立；第 26 行 `backends/pi/cli.go` 指 `internal/ai/backends/pi/cli.go` ✓（该文件的 `--mode json`/`--no-context-files`/`--model` 实参与本仓库 `internal/ai/backends/pi/cli.go:102,125,134` 一致）。

**③ 建议怎么改**

§五「建议」标注为「未实施」；若 Pi 的 ACP 桥接已在生产启用，文首「调研时 ClawBench 的 Pi 通道只有 CLI 行解析」那句加个时间限定。

---

#### `docs/dev/pi_tool_call_samples.md`

- **行数**：378 ｜ **类别**：开发探针记录
- **标题**：Pi CLI Tool Call JSON Samples

**① 这份文档讲什么**

Pi CLI（`-p --mode json`）的 JSON 输出样例，含 `text_/thinking_/toolcall_` 增量与 `tool_execution_*` 事件形状。

**② 有没有引用了已经不存在的文件或函数**

**样本记录，无失效引用**。第 248/267 行 JSON 里的 `internal/ai/` 目录真实存在；文档记录的命令行与 `internal/ai/backends/pi/cli.go:102-134` 的实参一致 ✓。

**③ 建议怎么改**

无需修改。

---

#### `docs/dev/pi_tool_definitions.md`

- **行数**：438 ｜ **类别**：开发探针记录
- **标题**：Pi 工具调用 JSON Schema 定义

**① 这份文档讲什么**

Pi 内置工具（Read/Write/Edit/Bash/Grep/Find/Ls）JSON Schema 定义 + 可插拔接口 + 工具名总表。

**② 有没有引用了已经不存在的文件或函数**

**无失效引用**——纯 schema。工具名与 `internal/ai/pi_tool.go` 实际处理的 `read`/`write`/`edit`/`bash` 及 `path`/`file_path`/`old_string`/`new_string` 参数名一致。

**③ 建议怎么改**

无需修改。

---

#### `docs/plans/2026-07-14-dingtalk-session-reply.md`

- **行数**：1576 ｜ **类别**：实施计划（历史）
- **标题**：DingTalk Session Reply Implementation Plan

**① 这份文档讲什么**

钉钉推送的会话回信能力设计——去掉预览截断、改短会话 ID、在 `SessionMessenger` 接口上支持 `@{8hex}` 指令与 `/ls` 列表，并在 service 层抽出 `LaunchSessionExecution`/`executeStreamRunShared` 供推送通道复用会话执行链路。

**② 有没有引用了已经不存在的文件或函数**

文档按现状描述的 `dingtalk.SessionInfo` 实为 `common.SessionInfo`（`internal/push/common/interfaces.go`），`internal/push/dingtalk/session_command.go` 与 `dingtalk/integration_test.go` 从未存在（解析器落在 `internal/push/common/session_command.go`，测试在 `session_messenger_test.go`）；文档提到的 `ai.SendStreamEvent`/`ai.SendFinalStreamEvent`/`service.EnqueueMessage`/`service.SessionFullInfo` 均不存在（实际为 `GetSessionFullInfo`、`EnqueueAndMaybeStart`）；截断实现也与文档描述不同——现为 4000 rune 的 `truncateForDingTalk`（`dingtalk/push.go`），不是文档写的 18000 字节 `dingtalkMarkdownMaxBytes` + "…(内容过长已截断)"。

**③ 建议怎么改**

把文档改成「已实施结果」并补一行路径漂移说明（`dingtalk.SessionInfo`→`common.SessionInfo`、命令解析器移到 `internal/push/common`），把截断阈值/文案按现状（4000 rune、尾缀 `…`）订正，并补记「计划中的 `internal/push/dingtalk/integration_test.go` 未创建，相关覆盖由 `session_messenger_test.go` 承担」。

---

#### `docs/plans/2026-07-14-tts-tech-debt-cleanup.md`

- **行数**：791 ｜ **类别**：实施计划（历史）
- **标题**：TTS Technical Debt Cleanup Implementation Plan

**① 这份文档讲什么**

11 个任务清理 `internal/handler/tts.go` 的 TTS 流式/非流式重复代码——抽出 `ttsRunJob` 与 `audioExtForProvider`、补 3 个流式测试、清理 `nolint`。

**② 有没有引用了已经不存在的文件或函数**

全部任务已落地（`ttsRunJob` 在 tts.go:154、`audioExtForProvider` 在 :224、`mockStreamingSpeechProvider` 与 3 个流式用例在 tts_test.go:338/380/417），但文档给的行号全部漂移：`TTSGenerate` 的 nolint 在 L92→实际 L236、类型断言 L149-159→实际 L224、`ttsExtractConclusion` 的 nolint 在 L383→实际 L422（仍保留）；`ttsRunJob` 实际签名多了一个 `speakerPrefix string` 参数。

**③ 建议怎么改**

在文首加一行「已实施（2026-XX-XX）」并把各 Task 的行号引用改成符号名引用（`ttsRunJob` / `audioExtForProvider` / `ttsExtractConclusion`），或直接标注「行号已随重构失效，以符号为准」。

---

#### `docs/plans/2026-07-17-native-push-toggle.md`

- **行数**：398 ｜ **类别**：实施计划（历史）
- **标题**：Native Push Notification Toggle Implementation Plan

**① 这份文档讲什么**

设计一个独立的「原生推送」开关——前端加本地设置 `nativePushEnabled` + JS 桥透传，Android 侧用 `SharedPreferences` 存开关、开机广播恢复 WorkManager 任务、前后台切换时取消/重排 `PendingEventsWorker`，并补一份前端单测。

**② 有没有引用了已经不存在的文件或函数**

Android 半边已落地，但 Web 半边整体未落地——`web/src/composables/__tests__/useSettingsConfig.nativePush.test.ts` 不存在（真实测试在 `useSettingsConfig.test.ts`），`nativePushEnabled` 本地设置与 settingsFieldMap 条目也不存在（改由服务端 `push_mode` 驱动）；计划里的桥名 `AndroidNative` 实际是 `ClawBenchNative`；i18n key 已在 9e9f54d10 作为死文案删除；计划的 `cancelPendingEventsWork` Java 片段语法不成立。

**③ 建议怎么改**

把文档状态标为「Android 已实施 / 前端设计未采纳，实际改用服务端 `push_mode`」，并加一句指向 `web/src/composables/useSettingsConfig.ts` 的 push_mode 读取处，避免读者按文档去找 `nativePushEnabled`；同时更正桥类名与测试文件路径。

---

#### `docs/plans/2026-07-25-session-search-rag.md`

- **行数**：1163 ｜ **类别**：实施计划（历史）
- **标题**：Session Search via RAG Implementation Plan

**① 这份文档讲什么**

用 RAG（sqlite-vec + FTS5）做会话内/跨会话语义检索的方案：新增 `/api/rag/session-search` 与 `/api/rag/status` 两个端点，命中结果附 `textMatchPositions` 用于前端高亮，前端由 `useSessionSearch` + `SessionSearchDrawer` 承载。

**② 有没有引用了已经不存在的文件或函数**

文档以「已有文件需修改」口吻引用的 `internal/rag/search_test.go` 至今不存在（现存的是 `search_sqlite_test.go`/`match_test.go`）；前端 `SessionSearchDetailModal.vue` 与其测试 `__tests__/SessionSearchDetailModal.test.ts` 从未创建，下钻详情是内建在 `SessionSearchDrawer.vue` 里的 `detail-page`/`detail-chunk` 视图；文档提及的 `service.DBReady`/`service.ReadDB` 不存在（实为 `internal/store/db.go` 的 `DBReady`/`ReadDB`）。

**③ 建议怎么改**

正文标注「已落地」并把引用的测试文件/组件名换成现状路径（`internal/rag/search_sqlite_test.go`、`SessionSearchDrawer.vue`），同时补一句「独立的 `SessionSearchDetailModal.vue` 未拆出，详情视图内建于抽屉内」，避免后续读者按旧路径去找。

---

#### `docs/plans/2026-07-27-system-resources-monitor.md`

- **行数**：1164 ｜ **类别**：实施计划（历史）
- **标题**：System Resources Monitor Implementation Plan

**① 这份文档讲什么**

系统资源监控 Gauge 按钮功能（CPU/内存/磁盘/网络进度条 + 弹窗），Go 后端 `internal/system` + `GET /api/system/resources` + 前端 `useSystemResources` 轮询。

**② 有没有引用了已经不存在的文件或函数**

`web/src/components/common/__tests__/AppHeader.test.ts` **不存在**（`AppHeader.vue` 本体在，但无对应测试文件）。其余（`internal/system/`、`system_resources` WS 事件）已落地。

**③ 建议怎么改**

去掉对 `AppHeader.test.ts` 的引用，或确认测试实际位置（如 `AppHeader.vue` 本身的单测是否已存在；若不存在则删除该引用行）。

---

#### `docs/plans/2026-07-31-message-clusters-quick-send.md`

- **行数**：660 ｜ **类别**：实施计划（历史）
- **标题**：User Message Clustering & Quick Send Recommendation Implementation Plan

**① 这份文档讲什么**

消息聚类推荐——从聊天历史提取用户消息、按语义聚类、提供快捷发送建议。

**② 有没有引用了已经不存在的文件或函数**

`service.GetClusterCache` 应为 `store.GetClusterCache`（`internal/store/cluster.go`）；`ws.GlobalHub` 应为 `StreamHub`/`Manager`；`internal/rag/segment.go` 应为 `internal/rag/segmenter.go`。以上三处 plan 中的包/类型名与实际落地不符，但功能已实现，属于正常偏差。

**③ 建议怎么改**

更新三处符号名引用（`service` → `store`、`GlobalHub` → `StreamHub`、`segment.go` → `segmenter.go`），或视为已确认的实现细节无需修改。

---

#### `docs/plans/2026-08-02-remove-auto-resume.md`

- **行数**：498 ｜ **类别**：实施计划（历史）
- **标题**：Remove AutoResumeBackend Implementation Plan

**① 这份文档讲什么**

删除「自动续跑」机制——移除 `AutoResumeBackend`/`NeedsAutoResume`/`resume_split` 事件，后端注册表不再带续跑布尔位，WebSocket 与前端同步摘掉相关分支。

**② 有没有引用了已经不存在的文件或函数**

`ai.AutoResumeBackend` **不存在**；计划要新建的 `internal/ai/auto_resume.go`/`auto_resume_test.go` 也从未存在——但 `internal/ai` 下 grep `AutoResume` **零命中**，说明**该特性已彻底移除，计划已落地**；`internal/ai/backends/cline/cli.go` 不存在（Cline 后端已删，现为 antigravity/claude/codebuddy/codex/… 19 个）；`docs/timer/spec-update.md` 不存在；`docs/spec/.prompt:86-87` 仍把 `AutoResumeBackend` 当现有扩展点举例。

**③ 建议怎么改**

核心结论（自动续跑已彻底移除，`internal/ai` 下 grep `AutoResume` 零命中）建议在文档顶部补一条「已实施」结论；同时把 cline 与 spec-update 两处引用替换为现行后端列表，并清理 `docs/spec/.prompt` 的过期举例；另注明 `ChatRequest.Resume` 保留是给 CLI `--resume` 用，与续跑无关。

---

#### `docs/plans/2026-08-21-codemirror-autocomplete.md`

- **行数**：388 ｜ **类别**：实施计划（历史）
- **标题**：CodeMirror Autocomplete Implementation Plan

**① 这份文档讲什么**

给 CodeMirror 编辑器按语言挂 `@codemirror/autocomplete` 补全，只在可编辑模式启用并配主题化 tooltip 样式。

**② 有没有引用了已经不存在的文件或函数**

依赖（package.json:22）与 `buildCompletionExtension`/`completionCompartment`/`mountCompletion`（codeEditorLang.ts:255/319、CodeMirrorViewer.vue:230/595）、tooltip 样式（:1131）都已落地；但文档的**设计描述与现码矛盾**——它设想 `COMPLETION_LANGS` 是 `Record<string, factory|null>`（markdown 为 null）、且用 `autocompletion({override:[source]})`，实际是 `Set<string>`（含 markdown，无 null 项）且**刻意不用 override**（注释说明 override 会丢 JS 关键字补全）；文档里的断言 `expect(COMPLETION_LANGS['markdown']).toBeNull()` 今天必然失败。

**③ 建议怎么改**

补一句「实施期修正：改为 Set + 依赖 `language.data.of({autocomplete})` 的默认源，不使用 override」，并删掉/改写 markdown 为 null 的示例，避免后人照抄写错。

---

#### `docs/plans/2026-08-24-queued-message-persistence.md`

- **行数**：1126 ｜ **类别**：实施计划（历史）
- **标题**：排队消息持久化方案（彻底简化版）

**① 这份文档讲什么**

排队消息持久化方案——队列消息直接落 `chat_history`（新增 `queue_id`/`queued` 列）、把 `/api/ai/queue` 合并进 `/api/ai/chat`、用 `SignalDrain`/`WaitForEnqueue` 驱动 drain，并删除 `internal/service/queue.go` 与 `needs_start` 往返。

**② 有没有引用了已经不存在的文件或函数**

方案整体已落地但**实现路线全变**：队列表是独立的 `queued_messages` 表（`internal/service/database.go`），文档设想的 `chat_history.queue_id`/`queued` 列后来被 DROP（表重建于 `database.go:2817+`）；文件不是 `internal/service/queue.go` 而是 `internal/service/queue_store.go`（出队即在同一事务 DELETE 队列行 + INSERT 历史行）；文档点名的 `service.SignalDrain`/`service.WaitForEnqueue`/`sessionDrainChans` 与 `service.StartSessionRun` **均不存在**（现为 `internal/service/drain.go` 的 `RunDrainLoop` + `queue_reaper.go` 的 `launchConsumerExecution`）；决策 D3「删除 `POST /api/ai/chat`」未做——`handler.go:489` 仍注册 `/api/ai/chat`，`/api/ai/queue` 也仍在（`handler.go:493`）。

**③ 建议怎么改**

把文档状态改为「已实施（实现与设计有出入）」，逐条标注落地形态（独立表 + claim-on-dequeue、`RunDrainLoop` 取代 `SignalDrain`），并明确修正 D3（两个端点并存）与 `GetChatHistoryPaged` 的返回签名（现为 3 值，非文档的 4 元组）。

---

#### `docs/plans/2026-08-27-android-floating-status-window-plan.md`

- **行数**：282 ｜ **类别**：实施计划（历史）
- **标题**：Android 桌面悬浮状态窗 Implementation Plan

**① 这份文档讲什么**

上面那份设计稿的 6 任务实现计划（FloatingStatusView / FloatingStatusController / WS 事件派发 / 权限申请与测试）。

**② 有没有引用了已经不存在的文件或函数**

**`android/app/src/test/java/com/clawbench/app/BackgroundServiceFloatingDispatchTest.java` 不存在**，等价用例落在 `BackgroundServiceFloatingTest.java`；文档要求实现的 `FloatingStatusView.statusLabel` / `statusColor` 两个静态纯函数**未实现**，实际类是 `countRunning/countPending/countUnread`（`renderStats`/`refreshLocaleText`/`stopBreathing`）；`FloatingStatusController.shouldShow` 实际是 4 参（多 `unreadCount`）而非文档的 3 参；另有文档从未提及的 `FloatingStatusContentView.java`（logo + 统计点）。

**③ 建议怎么改**

把测试文件名改为 `BackgroundServiceFloatingTest.java` 并注明「重命名合并」；删除 `statusLabel`/`statusColor` 小节，替换为实际落地的 `countRunning/countPending/countUnread` 契约；补一句「实际实现另抽出 FloatingStatusContentView」。

---

#### `docs/plans/2026-08-27-android-floating-status-window.md`

- **行数**：108 ｜ **类别**：实施计划（历史）
- **标题**：Android 桌面悬浮状态窗设计

**① 这份文档讲什么**

Android 系统级悬浮胶囊窗的设计稿——复用 BackgroundService 原生 WS，事件驱动显隐、拖动贴边、点击深链回主界面。

**② 有没有引用了已经不存在的文件或函数**

文中「现状：Android 端无悬浮窗能力，Manifest 未声明 `SYSTEM_ALERT_WINDOW`」已被推翻（AndroidManifest.xml:23 已声明）；`android/.../SettingsFragment` 在整个仓库不存在（开关实际落在 Web 设置 `floatingStatusWindow` + JSBridge）；`BackgroundService.java` 的 L2099 已漂移到 L3164；**两项设计未实现**——「开启悬浮窗后抑制 BackgroundService 重复弹通知」无任何对应实现（悬浮窗与原生推送是两个独立开关，悬浮窗不抑制通知），「长按弹迷你菜单」也无 `setOnLongClickListener`。

**③ 建议怎么改**

改写「背景」段为「现状（2026-08-27）Android 端无悬浮窗能力」的历史陈述，明确标注未落地的两项（通知抑制、长按菜单），并把 SettingsFragment 一行改成实际落点（`settingsFieldMap.ts` 的 `floatingStatusWindow`）。

---

#### `docs/plans/2026-08-28-android-floating-panel-plan.md`

- **行数**：458 ｜ **类别**：实施计划（历史）
- **标题**：Android 悬浮窗多会话列表面板 Implementation Plan

**① 这份文档讲什么**

上一份面板设计的 7 任务实现计划（overview 端点 → Android 点击决策/面板 → 主题色透传 → 测试）。

**② 有没有引用了已经不存在的文件或函数**

全部验证命令都指向 `cd .../.worktrees/floating-panel`，**该 worktree 不存在**（`.worktrees/` 下只有 aa-clean、ab-dirty 等）；文档设计的 `decideCapsuleClick` / `CLICK_OPEN_SESSION` / `CLICK_EXPAND_PANEL` / `parseOverview` 在 android/ 下**一个都没有**，实际是 `FloatingStatusController.onCapsuleTap()` 无条件 `setExpanded(true)`（胶囊点击一律展开，不做单会话直进）；反过来 `internal/handler/session_overview_test.go`、`FloatingStatusPanelViewTest.java`、`MainActivityThemeTest.java`、`useSettingsConfig.test.ts`、`MainActivity.setTheme` 五参签名（MainActivity.java:4269 + desktop/src/preload/index.ts:155）都已落地。

**③ 建议怎么改**

删掉所有 `.worktrees/floating-panel` 路径前缀改用仓库根；把 `decideCapsuleClick` 一节改写为实际行为「胶囊点击恒展开面板，单会话直进改由面板行点击 + 深链（`launchFromFloatingWindow(sessionId, projectPath)`）承担」；整体标为「已实施（2026-08-28）」。

---

#### `docs/plans/2026-08-28-android-floating-panel.md`

- **行数**：127 ｜ **类别**：实施计划（历史）
- **标题**：Android 悬浮窗多会话列表面板设计

**① 这份文档讲什么**

把单胶囊升级为「迷你胶囊 ↔ 展开列表面板」双形态，面板按项目分组展示运行中/待审批/未读会话，并做主题色同步。

**② 有没有引用了已经不存在的文件或函数**

文档「现状」栏写「logo 当前未显示，需修复」与「数据：原生从已拉取的 overview JSON 本地计算，**无需后端改动**」均已失效——logo 已修（`FloatingStatusContentView.circularLogo`），后端端点 `/api/ai/sessions/overview` 已注册（handler.go:540、`ServeSessionsOverview`、`GetOverviewSessions`），与文档自己的「关键决策」表自相矛盾；「20+ 种主题」现为 36 个。

**③ 建议怎么改**

把「现状」一栏整体改为历史时点描述，删掉「无需后端改动」这句与决策表冲突的话，统一写成「后端新增 `/api/ai/sessions/overview`」；主题数量改为「36 种」。

---

#### `docs/plans/2026-08-28-push-authoritative-sync.md`

- **行数**：414 ｜ **类别**：实施计划（历史）
- **标题**：推送权威消息同步 + 断线补偿方案

**① 这份文档讲什么**

三段式改造——订阅与流式解耦、断线用 `pending_events` 增量补偿、气泡身份以后端 WS 推送为准。

**② 有没有引用了已经不存在的文件或函数**

文档「现状局限：IsNotifiableEvent 只放行 terminal 状态事件，不含 chat_stream」已被推翻（pending_events.go:73 `isUserMessageEvent` 分支）；write-ahead 已实现在 `ws.StreamHub.Emit/EmitToSession`（stream_hub.go:201、:295）而非文档指定的 handler 广播点（queue.go:110 / chat.go:475/503）；阶段 2 的「stream_start 在 session_command.go:725」现位于 `internal/service/run_turn.go:348`（handler 已改为委托 service.RunTurn，所谓「双路径」已不存在）。**仍未落地的是 F5**：`useGlobalEvents.ts:468-478` 的实时路径只在 terminal 事件推进游标，`user_message` 不推进（且变量名是 `lastSeenEventId`，全仓无 `LAST_SEEN_KEY` 常量）。

**③ 建议怎么改**

标注「阶段 1/2/3 已实施（2026-08-28）」并把 F5 单独列为未完成项写清「实时路径未对 user_message 推进游标，仍待补」；统一把 `LAST_SEEN_KEY` 改成实际的 `lastSeenEventId`，把行号引用换成符号名。

---

#### `docs/plans/2026-08-30-terminal-theme-picker-grid.md`

- **行数**：743 ｜ **类别**：实施计划（历史）
- **标题**：Terminal Theme Picker Grid Implementation Plan

**① 这份文档讲什么**

终端主题从 select 列表改为卡片网格，每卡渲染迷你终端预览；`optionPreviews` 扩成 `color | terminal` 判别联合。

**② 有没有引用了已经不存在的文件或函数**

功能已全部落地（`TerminalPreview`/`OptionPreview` 在 SettingsItem.vue:339-340/358、`isTerminalThemeSelect`:436、`TerminalPreviewCard.vue`、tooltip 样式 `:1131`、zh/en i18n `terminalTheme` 现于 zh.ts:2884 / en.ts:2888），但文档「现状」引用的行号全错（213-216→339/358、278→433、281-295→535、`handleEditToggle` 413→602、zh.ts 1556-1557→2884/2888）；文档里的 `settingsFieldIsTerminalTheme` 实际名为 `isTerminalThemeField`（SettingsGroupPanel.vue:341）；预览构造从文档设想的组件内 computed 挪到了 `utils/terminalThemes.ts` 的 `buildTerminalThemePreviews`；文档模板写根类名 `.tpc`，实际实现是 `.tpc-shell`（文档自身测试期望 `.tpc-shell`，模板与测试互相矛盾）。

**③ 建议怎么改**

把行号改为符号名、`settingsFieldIsTerminalTheme` 改名 `isTerminalThemeField`、预览构造改指 `buildTerminalThemePreviews`、根类名统一为 `.tpc-shell`；标注「已实施」。

---

#### `docs/plans/2026-08-31-toc-dock-inline-design.md`

- **行数**：113 ｜ **类别**：实施计划（历史）
- **标题**：PC 模式 TOC 停靠栏内联设计

**① 这份文档讲什么**

PC 宽屏下将 TOC 从浮动 BottomSheet 改为 FileOverlay 覆盖层内右侧停靠栏（可拖拽分隔条，200–400px），进入编辑自动收起。

**② 有没有引用了已经不存在的文件或函数**

核心前提「`TocDrawer.vue` 是纯内容组件、没有 BottomSheet 依赖」与「给 TocDrawer 加 `docked` prop」**都没发生**——TocDrawer.vue:2 仍包着 `<BottomSheet>`、无 `docked` prop，实际抽出的是文档从未提过的 `web/src/components/TocPanel.vue`（TocDock.vue:34/49 引入的正是它）；持久化 key `clawbench.tocDock.open/.width` 已被 `panelState.ts` 接管（新键 `clawbench-panel.toc-dock-open/-width`，旧键只作 LEGACY_GLOBAL_KEYS 迁移）；文档只说「右侧 260px」，实现多了 `tocDockSide` 左右切换（useTocDockPreference.ts:104）；`App.vue:790/1381`、`FileHeader.vue:11` 行号已漂移。

**③ 建议怎么改**

把「现状关键点」改为历史描述，把组件抽取一节改成「实际抽出 `TocPanel.vue`（TocDrawer 保留 BottomSheet 供窄屏底部抽屉）」，持久化一节改指 `readPanelState/writePanelState('tocDockOpen'/'tocDockWidth'/'tocDockSide')`，并补上左右切换这一新增能力。

---

#### `docs/plans/2026-09-06-custom-wallpaper.md`

- **行数**：160 ｜ **类别**：实施计划（历史）
- **标题**：自定义背景图（Custom Wallpaper）设计方案

**① 这份文档讲什么**

在外观面板加「自定义背景图」——服务端存 `<DataDir>/theme/background.<ext>` 并以 `appearance.wallpaperFile`/`appearance.panelOpacity` 两个 server key 承载，配套 POST/DELETE `/api/theme-background` 与 GET `/api/file/theme-background`，前端注入 `--bg-image-url`/`--panel-alpha` 实现主面板半透明。

**② 有没有引用了已经不存在的文件或函数**

`internal/handler/theme_background.go` 今天不存在（建过又于 dcb4f3074 删除），壁纸现由 `internal/wallpaper/` + `internal/handler/theme_gallery.go` 提供，端点也变成 `/api/theme/local/upload`、`/api/theme/bing/*`、`GET /api/file/theme-wallpaper`；`web/src/components/app/App.vue` 路径错（实际 `web/src/App.vue`）；文档的「服务端全局、跨设备生效」被今天的按设备 localStorage 选择取代（`AppearanceConfig{Local,Bing}`），三态 `unknown/set/unset` 变成 `WallpaperMode` 四态。

**③ 建议怎么改**

加「设计已被重塑」说明段，写清最终形态（`internal/wallpaper` + 画廊/必应两条来源 + 按设备存储），并把已失效的端点名与 server key 换成现行 `theme/local/upload`、`--wallpaper-scrim` 等，避免按旧文档调接口必然 404。

---

#### `docs/plans/2026-09-11-at-file-reference.md`

- **行数**：165 ｜ **类别**：实施计划（历史）
- **标题**：聊天输入框 @ 文件引用 + 补全交互组件抽取

**① 这份文档讲什么**

聊天输入框 `@` 文件引用 + fuzzy 补全，并把斜杠/`@` 的重叠交互抽成 `useCompletionMenu` + `CompletionMenu`（状态：已实施）。

**② 有没有引用了已经不存在的文件或函数**

主体已落地（completionMatch.ts / completionSources.ts / useCompletionMenu.ts / CompletionMenu.vue 及 4 个测试文件都在），但**来源数量已从 7 变 8**（新增 `skill`，zh.ts:546 的 `chat.completion.source` 下多 `skill: '技能'`）；文档写「接入两个 useCompletionMenu + 两个 CompletionMenu」，实际 ChatInputBar.vue 有**三个**（command:1095 / skill:1149 / file:1257，模板 215/223/236）；文档说高亮「复用 `highlightTextByPositions`」，实际 CompletionMenu.vue:112 自己实现了 `renderLabel()`（escapeHtml + `<mark>`），searchUtils 那个函数只被 SessionSearchDrawer 用；`parseSlashQuery` 的语义已从「`startsWith('/') && !includes(' ')`」改成 `parseFirstTokenQuery(text, caret, '/')`（首个 token 规则）；文档 §3 里的 ChatInputBar 行号（L802-844 等）全部漂移。

**③ 建议怎么改**

把 7 类来源改为 8 类并注明 skill 为后续新增；「文件改动」一节改成三个菜单实例；高亮一节改述为「组件内自实现 `renderLabel`（escapeHtml + mark）」；`parseSlashQuery` 语义改述为「仅消息首个 token 触发，避免 `cd /test` 误命中」；标注「已实施（2026-09-11），来源数量与菜单数后续有增补」。

---

#### `docs/plans/2026-09-11-cross-project-session-tab.md`

- **行数**：221 ｜ **类别**：实施计划（历史）
- **标题**：会话列表跨项目活跃会话 Tab

**① 这份文档讲什么**

在会话列表底部加「本项目 / 其他项目」分段页签，后端新增 `/api/ai/sessions/overview` 一次返回跨项目会话摘要，前端用模块级单例 composable 缓存，切页签只切数据不重拉。

**② 有没有引用了已经不存在的文件或函数**

已实施且与文档一致（`useCrossProjectSessions.ts`、`SessionListTabs.vue`、后端 `session_overview_test.go`、i18n `tabCross` 都在），但「web/src 零引用该端点」这句现状描述已过时；文档给 `App.vue`、`chat_session.go` 的行号已漂移。

**③ 建议怎么改**

把「现状」句改成历史陈述并加一句「后续本前端已自行消费该端点」，同时把页签位置按实际补注（页签条已抽到 `SessionListTabs.vue`、挂在抽屉 `#footer`），行号引用统一改为符号引用。

---

#### `docs/plans/2026-09-11-github-gitlab-integration.md`

- **行数**：446 ｜ **类别**：实施计划（历史）
- **标题**：GitHub / GitLab 集成（Issue + PR/MR）设计方案

**① 这份文档讲什么**

GitHub/GitLab 集成方案——平台无关 `Provider` 抽象、issue/PR/评论/流水线统一模型、远端 URL 解析、按 host 限流、事件推导与轮询，再叠加凭据加固、水印轮询、事件触发 AI 任务与 URL 附件。

**② 有没有引用了已经不存在的文件或函数**

整体已落地（`internal/forge` + `github`/`gitlab`、go-github v85、`forge_sync.go`、`project_forges.go`、`TriggerMode=cron|event`、`event_url`/`event_summary`、token 只写不读、config 0600 均在），但文档把 P1「拒绝私网/回环/链路本地/云元数据」的 SSRF 闸门列为硬 Blocker，与今天代码和 CLAUDE.md 相反——`internal/handler/forge_credentials_test.go:111-133` 明确断言私网 host 被接受（「That gate is gone by design」）；文档还把实现收敛成单个 `internal/handler/forge.go`，实际已拆成六个 forge handler 文件。

**③ 建议怎么改**

这是本批最需要修正的一处——在文档顶部加「安全策略已反转」告示，写明无 host 安全闸门、风险提示前移到前端绑定弹窗，并删掉或标注失效的 SSRF Blocker 描述；影响文件清单补齐拆分的 handler 文件。

---

#### `docs/plans/2026-09-12-forge-webhook.md`

- **行数**：876 ｜ **类别**：实施计划（历史）
- **标题**：Forge Webhook 接收（低延迟事件加速层）设计方案

**① 这份文档讲什么**

把 GitHub/GitLab webhook 做成 Forge 同步的**加速层**（底层仍是轮询）：新增 `POST /api/forge/webhook/{token}`、导出 `github.NormalizeState`/`gitlab.NormalizeState`、给 `service.GlobalForgeSyncer` 加 `DispatchChange`，并做 F1–F7 项 review 修正。

**② 有没有引用了已经不存在的文件或函数**

webhook **完全未实现**——`internal/forge/` 下无 `webhook.go`/`webhook_sign.go`，`internal/handler/` 无 `forge_webhook.go`，`handler.go` 路由表里没有任何 `/api/forge/webhook`；`github.NormalizeState`/`gitlab.NormalizeState` 至今仍是包内私有的 `normalizeState`（`internal/forge/github/github.go:453`、`gitlab/gitlab.go:390`）；`service.GlobalForgeSyncer` 不存在，`internal/service/forge_syncer.go` 仍只有未导出的 `sink` 字段与 `SyncRepo`/`SyncRepoWithOptions` 两个导出方法，plan 里的 `DispatchChange` 至今是私有的 `persistAndDispatch`（`forge_syncer.go:522`）；文中 `internal/service/forge_endpoints.go` 实为 `internal/handler/forge_endpoints.go`。

**③ 建议怎么改**

文档状态行「设计待评审（尚未实施）」与现实一致，只需补一段「2026-10 复核：仍未实施，Forge 同步仍为轮询（`internal/service/forge_poller.go`）」并订正 `forge_endpoints.go` 的包路径与行号漂移；`NormalizeState`/`DispatchChange` 两条建议可另开 issue 跟进。

---

#### `docs/plans/2026-09-12-upgrade-self-path-fix.md`

- **行数**：302 ｜ **类别**：实施计划（历史）
- **标题**：自升级在「运行中被 npm 替换」后失败 — 根因分析与修复方案（E + 版本短路）

**① 这份文档讲什么**

自升级卡死根因分析与「方案 E」——把自身二进制路径持久化到 `{data-dir}/self-path`，重启时经 `ResolveSelfBinary()` 重新定位自身，并加版本短路与 `ERR_SELF_PATH_UNRESOLVED`/`restart_failed` 错误码。

**② 有没有引用了已经不存在的文件或函数**

已实施（`internal/service/selfpath.go` 的 `ResolveSelfBinary()`/`ResolveRestartBinary()`、`upgrade_shortcircuit_test.go`、`settings_sentinel_selfpath_test.go`、`scripts/e2e-upgrade-npm-replace.sh` 都在），但文档引用的 `docs/clawbench-upgrade-stuck-analysis.md` 不存在；§7.3「哨兵改用 resolveSelfBinary」与今天相反——`internal/handler/settings_sentinel_unix.go:23-31` 用的是 `ResolveRestartBinary()`。

**③ 建议怎么改**

补一句根因分析文档已合并/删除；把哨兵一节的结论更新为「哨兵用 `ResolveRestartBinary()`，仅原地升级才用 `ResolveSelfBinary()`」，与实际注释一致。

---

#### `docs/plans/2026-09-15-ask-question-answer-state.md`

- **行数**：266 ｜ **类别**：实施计划（历史）
- **标题**：AskUserQuestion 补充信息/选项/已提交状态被清空 — 根因分析与修复

**① 这份文档讲什么**

AskUserQuestion 卡片「补充信息 / 选项勾选 / 已提交」三类状态只存在于 DOM，重渲染即丢——改为新增模块级 `askQuestionState.ts` 承载，按 session 前缀 + 四种卡片 kind 生成 key，写入/回填分离，提交后不删、发送失败回滚。

**② 有没有引用了已经不存在的文件或函数**

已实施且与文档描述吻合（`askQuestionState.ts` 的 API、`renderToolDetail` 的 `askKey`/`restoreAskStateFromStore`/`makeAskViewAnswerable`/`revertAskSubmission`、四类 kind、列出的测试文件全部到位），唯一漂移是 §4.4 指向的 `<ask-question>`→`tool_use` 转换位置已变（现在在 `internal/askquestion` 与 `internal/ai/block_helpers.go:121-143`），且标签名已改为 `<clawbench-ask-question>`。

**③ 建议怎么改**

只做小幅订正——把 §4.4 的转换位置与标签名更新为现状，并注明后续原生 Markdown 改造（见 09-19 计划）已改变该转换路径；其余结论保留。

---

#### `docs/plans/2026-09-15-tool-detail-wrap-toggle-state.md`

- **行数**：109 ｜ **类别**：实施计划（历史）
- **标题**：工具详情「换行切换」状态不持久 — 问题分析与修复建议

**① 这份文档讲什么**

工具详情「换行」按钮的状态只写 DOM class（`word-wrap`/`is-wrapped`），重新渲染即回默认「换行开」——建议渲染器改读全局设置 `wordWrap`（localStorage `clawbench-word-wrap`），切换时写回。

**② 有没有引用了已经不存在的文件或函数**

「待修复（未实施）」标记今天仍然成立（`renderToolDetail.ts` 依旧硬编码 `is-wrapped`，全文 grep `wordWrap` 仍零命中），但文档给的所有行号都已漂移（`useSettingsConfig.ts` 318/20/98→395/27/115、`settingsFieldMap.ts` 236→291、`FileViewer.vue` 577/585→643/651、`renderToolDetail.ts` 1419-1425→1467-1469、73→93）；§6 说 `layoutRefreshKey` 是「全仓库无 inject 的死代码」，而该标识符今天已从仓库彻底消失。

**③ 建议怎么改**

行号引用统一改为符号引用以免再漂；把 `layoutRefreshKey` 一节改写为「该死代码已在后续清理中移除」，并保留「未实施」结论但注明优先级仍低。

---

#### `docs/plans/2026-09-17-inapp-notification-toggle-design.md`

- **行数**：138 ｜ **类别**：实施计划（历史）
- **标题**：应用内通知开关 + 推送通知页小节重排

**① 这份文档讲什么**

新增本地开关 `inAppNotification` 专门门控应用内完成卡片，并把推送通知设置页重排为「应用内通知 / 桌面与系统 / 移动端通知」三个小节。

**② 有没有引用了已经不存在的文件或函数**

主体已实施（`localDefaults.inAppNotification: true`、fieldMap 的 switch 与 `sectionHeader`、`App.vue:1820` 的早返回、i18n 五个 key、`notificationSoundSection` 已删、两份 spec 已更新），但今天的页面是**四节**——新增了独立的「桌面端通知」/ `desktopNotification` 卡，与文档的三节布局不符；且 `floatingStatusWindow`/`liveUpdate` 今天标为 `androidOnly`，不是决策表写的 `appOnly`（照旧会让 Electron 里出现无效开关）。

**③ 建议怎么改**

把目标布局图与决策表更新为现行四节结构并改用 `androidOnly` 措辞，同时补一句 `appOnly` 在桌面端会渲染空开关的历史教训。

---

#### `docs/plans/2026-09-19-ask-question-native-markdown-design.md`

- **行数**：165 ｜ **类别**：实施计划（历史）
- **标题**：ask-question 改为标签内原生 Markdown

**① 这份文档讲什么**

把 `<ask-question>` 标签内的自定义 XML 载荷换成原生 Markdown（`- 选项`、`- [ ] 多选`、`**标题**`），失败时剥离标签把内部文字交给 Markdown 渲染，并对真实数据里的畸形标记（破折号无空格、全角符号、CJK 序号、复选框变体）逐条放宽。

**② 有没有引用了已经不存在的文件或函数**

状态标「已确认，待实施」今天已不成立——Markdown 格式已全面落地（`internal/askquestion/markdown.go`、`parse.go:29` 只走 `parseMarkdownItems`、`internal/model/agent.go` 的 `commonRulesTemplate`、`web/src/utils/askQuestion.ts`）；文档的「旧 XML 解析路径必须保留」与「不引入 JSON 载荷 / JSON 恢复」两条决策都被反转，今天无回退解析器，旧载荷只经 `legacy.go` 降级为散文；共享语料路径 `internal/askquestion/testdata/parity_corpus.json` 仍正确，前端镜像测试 `web/src/utils/__tests__/askQuestionParity.test.ts` 与 `internal/askquestion/parity_test.go` 读同一份，镜像对仍成立。

**③ 建议怎么改**

把状态改为「已实施」并写明最终契约（只解析 Markdown，失败走 `Match.Fallback` 文本降级，无 XML 回退、无 JSON 恢复），把两处被反转的决策标为「已废弃」并说明原因；保留放宽规则与实测指标作为设计沿革。

---

#### `docs/plans/2026-09-21-task-custom-script-design.md`

- **行数**：272 ｜ **类别**：实施计划（历史）
- **标题**：定时任务自定义脚本

**① 这份文档讲什么**

设计 cron 任务在调 AI 前执行自定义脚本的最初语义（退出码 0 且 stdout/stderr 皆空 → 跳过 AI 不发通知），并在文末追加 2026-09-29 的「门控脚本」重构（退出码 0 放行、非 0/超时跳过）。

**② 有没有引用了已经不存在的文件或函数**

实现偏差第 5 条称 i18n 键 `task.form.scriptHint` 已从 `zh.ts`/`en.ts` 双双删除——今天两处（各 :1483）都存在，且文案已改为门控脚本语义；第 2 条称渲染预算 7000→8000，`internal/api/render_test.go` 的 `CommandTask` 预算现为 **9700**；文档说 `internal/model/scheduler.go:9-39` 等行号与 `internal/service/scheduler.go:776/1303` 全部漂移（相关代码已搬到 `task_script.go` + scheduler 门控实现）。

**③ 建议怎么改**

把「实现偏差」一节改为指向当前实现（`internal/service/task_script.go` 的 `ScriptOutcome` 四态、`scheduler.go` 的 `scriptVarCode="{{code}}"` 与 `script_exit_code/script_stdout/script_stderr` 落库），修正 `scriptHint` 的删除结论与 8000→9700 预算值，行号改为符号引用。

---

#### `docs/plans/2026-09-25-android-url-autofill-design.md`

- **行数**：156 ｜ **类别**：实施计划（历史）
- **标题**：登录页粘贴完整 URL 自动解析协议与端口 — 设计文档

**① 这份文档讲什么**

设计在 Android 桌面端登录页「新增服务器」表单里，把粘贴的完整 URL 在 `#addHost` 失焦时自动拆成协议/主机/端口三段（`url-utils.js` 的 `parseServerInput`，android 与 desktop 共用一份）。

**② 有没有引用了已经不存在的文件或函数**

整个「三段式表单 + `#addHost` 失焦触发」前提已不存在——今天 `android/app/src/main/assets/login.html:1308` 是**单一输入框** `#addAddress`（placeholder `https://192.168.1.100:20000`），无 `#addHost`/`#addPort`/`addProtocol` 单选、无失焦监听；行为改由 `buildServerUrl`（缺协议或端口即返回 null）承担，测试文件 `web/src/__tests__/loginUrlUtils.test.ts` 也已重写；文档描述的「Enter 键拼出错误 URL」等已知限制随之失效。

**③ 建议怎么改**

在文首加「已被单地址字段重构取代」的横幅，说明现状是 `#addAddress` + `buildServerUrl` + 服务器名唯一性校验（`normalizeServerName`/`findServerNameConflict`），并把 `url-utils.js` 实际仍被双端加载的事实保留（该部分仍准确）。

---

#### `docs/plans/2026-09-25-android-url-autofill.md`

- **行数**：621 ｜ **类别**：实施计划（历史）
- **标题**：登录页粘贴 URL 自动解析协议与端口 Implementation Plan

**① 这份文档讲什么**

上一份设计的实施计划（按 Task 1-5 拆解），含「粘贴→失焦自动拆分」修订（commit `c1aa60ee`）与前端测试文件 `web/src/__tests__/loginUrlUtils.test.ts` 的创建。

**② 有没有引用了已经不存在的文件或函数**

计划里的工作目录 `/root/code/clawbench/.worktrees/android-url-autofill` 是已失效的绝对路径；「测试数由 51 增至 53、当前测试文件为 53 个用例」与现实差一个量级（`loginUrlUtils.test.ts` 已按 `parseServerInput`/`buildServerUrl`/名称唯一性表重写）；Task 3 引用的 `desktop/electron-builder.yml` L7-15 只列 login.html + logo.png，实际还多一条 `url-utils.js` 的 extraResources。

**③ 建议怎么改**

标注该计划已完结并被单地址字段方案取代，删掉绝对 worktree 路径，测试数量改为「以 `web/src/__tests__/loginUrlUtils.test.ts` 当前用例为准」，并补上 electron-builder 里的 `url-utils.js` 条目。

---

#### `docs/plans/2026-09-25-h2-tunnel-design.md`

- **行数**：833 ｜ **类别**：实施计划（历史）
- **标题**：HTTP/2 流隧道设计文档

**① 这份文档讲什么**

h2 流隧道的设计书（一条 h2 连接复用多路 TCP 流，`-L` 走 `/api/tunnel/stream`、`-R` 走 `/api/tunnel/control` + 单次 token），含 stdlib 行为事实、文件行号锚点表与风险清单。

**② 有没有引用了已经不存在的文件或函数**

状态行写「设计（未实现）」，实际已全部落地（`internal/tunnel/`、`internal/handler/tunnel_stream.go`/`tunnel_control.go`、`desktop/src/main/h2Transport.ts`、android `tunnel/` 包）；§3.2 称 `shouldCreateProxyRegistry` = `Enabled || Transport != model.TransportSSH`，今天 `cmd/server/proxy_registry_gate.go` 是无条件 `return true`（该比较被刻意删除）；称 claim token 在 `internal/tunnel/token.go`（今天是 `claim.go`）；`internal/wallpaper`… 无关，但文中「154 路径 / 193 操作」的 OpenAPI 计数今天为 177/225；`web/src/composables/usePortForward.ts` 的门控行号 :492 已漂到 :856（`!info?.enabled && !tunnelTransportAllowsH2()`）。

**③ 建议怎么改**

把状态改为「已实现（见 `internal/tunnel/` 与 h2 相关 spec）」，删/标注 §3.2 的 `shouldCreateProxyRegistry` 公式，token 文件名改 `claim.go`，OpenAPI 计数与行号改为不写死或指向 `docs/spec/api/README.md` 的实时计数。

---

#### `docs/plans/2026-09-25-h2-tunnel-implementation.md`

- **行数**：1354 ｜ **类别**：实施计划（历史）
- **标题**：HTTP/2 流隧道实施计划

**① 这份文档讲什么**

h2 流隧道的 T1–T14 实施任务清单（协议开关、两个 handler、内部包、Android/桌面/Web 客户端、测试与文档）。

**② 有没有引用了已经不存在的文件或函数**

`internal/tunnel/token.go` 与 `token_test.go` 从未以该名存在（实际是 `claim.go`/`claim_test.go`）；Android 侧 `H2TunnelStream.java`/`TunnelStream.java` 已移入 `android/app/src/main/java/com/clawbench/app/tunnel/`，`H2TunnelStreamTest.java` 也移入 `android/app/src/test/java/com/clawbench/app/tunnel/`；T8 引用的 `web/src/utils/clawbenchNative.test.ts` 与 `web/src/composables/usePortForward.test.ts` 均已迁到 `__tests__/` 子目录；T1 示例里的 `ws.Close`/`ws.Read` 是连接方法而非包级符号（无法按包级符号校验）；「`cmd/server` 无测试包」今天不成立（已有 `server_protocols_test.go` 等），`desktop/src/main/tunnel.test.ts` 现有 74 个 `it(`（计划称 34）。

**③ 建议怎么改**

把文件清单与行号改为「参见 `internal/tunnel/`、`android/.../tunnel/`、`web/src/**/__tests__/`」的符号级描述，删除「cmd/server 无测试包」这句现状断言，并注明 `internal/tunnel/` 现有 `bind/relay/target/ndjson/guard/claim` 六个文件。

---

#### `docs/plans/2026-09-26-android-e2e-decisions.md`

- **行数**：134 ｜ **类别**：实施计划（历史）
- **标题**：Android E2E harness 决策记录（h2 流隧道）

**① 这份文档讲什么**

记录 Android E2E 测试台（`android-e2e/`，Docker 模拟器 + Appium）的 8 条非业务决策（镜像、并行拉取、docker build 交付、禁 IME、CI 门控、按 tier 暂存资产等）与 5 个刻意未决的产品问题。

**② 有没有引用了已经不存在的文件或函数**

§4 称 Android 单测 1032 个、98 个分支基线——今天 `android/app/src/test` 的 `@Test` 合计 **1214**；§2.6 描述的 Tier 2 五个服务含 `server-no-h2`，而 `android-e2e/server-config/config.no-h2.yaml` 已**被刻意删除**（`android-e2e/README.md:189,197` 明述该 503 对照组已不存在），`run.sh:122-124` 现在只有 `emulator mock runner` / `emulator server target runner` / all 四服务；§3.1 称「全部 14 个 `getCookie()` 调用点」未认证，今天为 16 处；文中未提到 `e2e/README.md` 与 `e2e/scripts/prepare-assets.sh`（`e2e/` 下二者均不存在，资产脚本实际在 `android-e2e/scripts/prepare-assets.sh`）；`internal/handler/tunnel_control.go:318-321` 的 `tunnelGuard` 现为 :381。

**③ 建议怎么改**

更新测试计数与 `run.sh` 服务清单，删掉 `server-no-h2` 相关描述或加「已移除」标注，getCookie 点数改为 16，并把 `tunnelGuard` 行号改为符号引用。

---

#### `docs/plans/2026-09-26-auto-session-rename-design.md`

- **行数**：125 ｜ **类别**：实施计划（历史）
- **标题**：会话自动命名（AI 重命名）设计

**① 这份文档讲什么**

设计 AI 会话自动重命名（共享核心 `CollectSessionUserMessages`/`GenerateSessionTitleFromMessages`，异步 `autoRenameSession`，用 `title_source <> 'custom'` 做原子守卫）。

**② 有没有引用了已经不存在的文件或函数**

状态写「已确认，实施中」，实际已完整落地（`internal/service/session_auto_title.go` 全部函数、`internal/model/config.go:105-111` 的 `auto_rename_enabled`、`internal/model/defaults.go:219-227`）；文中「开关默认关闭 → 现有行为完全不变」的兼容性声明已失效——默认现在是 **ON**（`internal/model/defaults_test.go:962-996` 钉住 default true），2026-09-29 的横幅也说明了默认翻转。

**③ 建议怎么改**

把状态改为「已实现」，并把「默认关闭、行为不变」改写为「默认开启（2026-09-29 翻转），`title_source='custom'` 的会话不被覆盖」，避免读者误判兼容性。

---

#### `docs/plans/2026-09-26-desktop-splash-design.md`

- **行数**：211 ｜ **类别**：实施计划（历史）
- **标题**：桌面端登录加载屏（对齐 Android splash）设计

**① 这份文档讲什么**

设计 Electron 启动闪屏（原生 overlay 复用 `login.html?splash=1`），提出纯策略模块 `splashPolicy.ts` 与免费函数式 API（`createSplashView/showSplash/dismissSplash/armFailSafe/cancelTimers`）。

**② 有没有引用了已经不存在的文件或函数**

状态写「待实施」，实际已实现（`desktop/src/main/splash.ts`、`splashPolicy.ts` + 两个测试文件、`desktop/src/preload/index.ts:80` 的 `dismissSplash`、`desktop/src/main/bridge.ts:123,125`）；但**实际 API 形态与设计不同**：`splash.ts` 只导出 `createSplashController(win, opts)`，没有文档列的免费函数；`shouldShowSplash(url: string)` 是单参、`splashBackgroundColor(isDark: boolean)` 是布尔参，而文档写的是 `shouldShowSplash(url, loginUrl)` / `splashBackgroundColor(themeId)`；文档称 preload 的 `dismissSplash` 是空壳，今天已是真实现；`desktop/package.json` 「处于他人中间态」的注记已过时。

**③ 建议怎么改**

在文首标注「已实现」并把 API 段改为实际签名（`createSplashController` / `SplashStage` / `SPLASH_FAILSAFE_MS` / `CONNECTION_TIMEOUT_MS`），同时说明 `splash.ts` 未与 `navReady.rendererReady()` 耦合（闪屏靠 `login.html` 的 `window.__splashSetStage` 握手），避免与仓库总览描述混淆。

---

#### `docs/plans/2026-09-26-xmb-wave-wallpaper-design.md`

- **行数**：284 ｜ **类别**：实施计划（历史）
- **标题**：动态波浪壁纸（XMB Wave）设计方案

**① 这份文档讲什么**

设计第三种壁纸模式「XMB 波浪」——Canvas 手绘波纹作为 `wallpaper_mode` 的新枚举值，并规划后端 4 处枚举放宽。

**② 有没有引用了已经不存在的文件或函数**

`web/src/components/WaveBackground.vue` 从未存在，特性改以 `web/src/components/AnimatedWallpaper.vue` + `web/src/utils/animatedWallpapers/{xmb,silk}.ts` 落地（`DEFAULT_ANIMATED_STYLE='xmb'`）；文档的「后端改动 4 处均为枚举放宽」已被放弃——`internal/handler/settings.go` 现在**完全拒绝** `wallpaper_mode` PATCH（`settings_test.go:3041-3060` 明确拒绝 `"wave"` 与 `"bing"`），壁纸模式改为按设备存 localStorage；`internal/wallpaper/wallpaper.go:177` 的 `ResolveActive` 今天不存在（该文件只导出 MimeFor/ThemeDir/LocalDir/BingDir/FilePath/Process/Scale 等），前端 `web/src/utils/themeBackground.ts:214-270` 仍残留 `'wave'` 分支。

**③ 建议怎么改**

把「后端枚举放宽」一节整段标为「已废弃，改为设备级 localStorage」，`ResolveActive` 改为已移除说明，并顺手清理前端 `themeBackground.ts` / `useSettingsConfig.ts` 里残留的 `'wave'` 字面量。

---

#### `docs/plans/2026-09-28-android-h2-toggle-design.md`

- **行数**：755 ｜ **类别**：实施计划（历史）
- **标题**：Android h2 隧道本地开关 设计文档

**① 这份文档讲什么**

设计 Android 端本地 h2 开关（SharedPreferences `tunnel_transport_h2_enabled`）、Electron 桥的偏好钳制、前端去掉 push 同步通道，以及「三值 transport（ssh/h2/both）」的收敛讨论。

**② 有没有引用了已经不存在的文件或函数**

`android/app/src/main/java/com/clawbench/app/tunnel/PortForwardTransportKind.java` 不存在——枚举改名 `tunnel/TransportKind.java`，且取值是 TLS/H2C（无 SSH/BOTH 可删）；`android-e2e/server-config/config.no-h2.yaml` 缺失，503 对照组已被删除、`cmd/server/proxy_registry_gate.go` 现无条件 `return true`，故 §2.2「唯一 503 组合」不成立；`desktop/src/main/transport.ts:43` 的 `TransportPreference` 已**去掉** `'both'`（与决策 D3「三值保留」矛盾），`desktop/src/main/bridge.ts:143` 也不再钳制为 'ssh'；`web/src/composables/usePortForward.ts:70` 的 `TRANSPORTS` 同样去掉 'both'（与「仍要认 'both'」矛盾）；`web/src/utils/clawbenchNative.ts:115` 的 `setTunnelTransport?(pref:'ssh'|'h2')` 仍在（文档要求删除），`SettingsGroupPanel.vue` 仍在用。

**③ 建议怎么改**

把 §2.2 与 D3/D5 标注为「后续已变更：服务端固定 both、客户端去掉 both」，枚举文件名改 `TransportKind.java`，并补一条现状说明——`clawbenchNative.ts` 的 `setTunnelTransport` 声明与设置面板用法仍存在、未按本文删除。

---

#### `docs/plans/2026-09-28-android-h2-toggle-implementation.md`

- **行数**：1589 ｜ **类别**：实施计划（历史）
- **标题**：Android h2 隧道本地开关 实施计划

**① 这份文档讲什么**

Android h2 隧道本地开关（SharedPreferences 控制，默认关 = SSH），Electron clamp 到 ssh，前端移除服务端推送链路，同步文档。

**② 有没有引用了已经不存在的文件或函数**

同设计稿——`android/app/src/main/java/com/clawbench/app/tunnel/PortForwardTransportKind.java` 不存在（实为 `TransportKind.java`）；`e2e/server-config/config.no-h2.yaml` 不存在（实际目录 `android-e2e/server-config/`；503 对照组已被刻意删除）；文档点名的 `getActiveTunnelTransport`/`setTunnelTransportH2Enabled` 等当前实现名已变（现 `getTunnelTransport`/`getTunnelTransportH2Enabled`/`setTunnelTransport`）。

**③ 建议怎么改**

枚举文件名与 transport getter 名按现状订正；`config.no-h2.yaml` 引用改为「已移除」并指向 `android-e2e/README.md` 的说明。

---

#### `docs/plans/2026-09-28-animated-wallpaper-styles.md`

- **行数**：155 ｜ **类别**：实施计划（历史）
- **标题**：动态壁纸多风格化（XMB 波浪 + 丝雾星轨）

**① 这份文档讲什么**

动态壁纸多风格化（新增 `xmb` XMB 波浪 + `silk` 丝雾星轨），设置面板暴露全部滑块参数，加新风格只需三步（新建模块/注册/补 i18n）。

**② 有没有引用了已经不存在的文件或函数**

无——`internal/wallpaper/` 存在，`web/src/utils/animatedWallpapers/` 存在（含 `xmb.ts`、`silk.ts`、`index.ts`），`web/src/components/AnimatedWallpaper.vue` 存在，测试文件 `registry.test.ts`、`silk.test.ts`、`xmb.test.ts` 均已落地。

**③ 建议怎么改**

文档无失效引用，实现与设计一致（状态标注「已实施」准确）。

---

#### `docs/plans/2026-09-28-login-server-edit-design.md`

- **行数**：155 ｜ **类别**：实施计划（历史）
- **标题**：登录页服务器选择优化：编辑现有服务器 + 单一地址框 — 设计文档

**① 这份文档讲什么**

登录页服务器选择优化：编辑现有服务器 + 单一地址框（移除协议/端口单选），支持服务器名称字段（可选 name）。

**② 有没有引用了已经不存在的文件或函数**

无——`android/app/src/main/assets/login.html` 与 `desktop/assets/login.html` 均存在，`web/src/__tests__/loginUrlUtils.test.ts` 已实现（91 用例），服务器名称功能已落地。

**③ 建议怎么改**

文档无失效引用，实现与设计一致（状态标注「已实施」准确）。

---

#### `docs/plans/2026-09-29-btw-command-design.md`

- **行数**：243 ｜ **类别**：实施计划（历史）
- **标题**：`/btw` — 由 ClawBench 接管的旁路问答

**① 这份文档讲什么**

`/btw` 旁路问答——在当前会话抽一个「顺便问」的问题由摘要模型回答，结果落 `btw_questions` 表并存锚点。

**② 有没有引用了已经不存在的文件或函数**

状态写「设计已确认，待实现」，**实际已落地**（`internal/service/btw.go`、`btw_questions` 表与 `internal/service/btw_test.go` 都在）；文档设想的 `summarize.AskAboutContext` 抽象层**不存在**，实现直接调用 `recommendPassProvider.DoRecommendPass`。

**③ 建议怎么改**

确认 `AskAboutContext` 是否仍为必要的抽象层——若已无调用方，可删除该函数并更新 plan；若 plan 描述的 API 与实际实现有出入，应同步 plan。

---

#### `docs/plans/2026-09-29-startup-optimization.md`

- **行数**：1071 ｜ **类别**：实施计划（历史）
- **标题**：启动耗时优化 Implementation Plan

**① 这份文档讲什么**

启动耗时从 ~25s 优化到 ~7s，手段是迁移版本化、模型发现并行化与异步化。

**② 有没有引用了已经不存在的文件或函数**

无重大失效引用。`service.WriteDB` 在 plan 中多处出现（如 Task 5 Step 3 "写入走 `db.Exec`（`service.WriteDB()` 的 `WriteExec`）"），实际 `WriteDB` 是 `database.go` 内的函数变量而非 `service` 包的导出符号，属表述偏差但不影响理解。

**③ 建议怎么改**

将 `service.WriteDB()` 改为 `db.WriteExec()` 或 `service 包内 db 变量` 的精确引用。

---

#### `docs/plans/2026-10-01-file-change-marker-design.md`

- **行数**：145 ｜ **类别**：实施计划（历史）
- **标题**：文件预览的变更标记重做 — 细轨 + 变更导航 + 累计基线

**① 这份文档讲什么**

文件预览变更标记重做——把 20px 实心色块换成 3px 细轨，加变更导航胶囊，标记基线改为累计。

**② 有没有引用了已经不存在的文件或函数**

状态写「设计已确认，待实现」，**实际已落地**（`web/src/composables/useFileChangeBaseline.ts` + 其测试、`web/src/components/file/FileChangeNav.vue`）；`web/tsconfig.js` 应为 `web/tsconfig.json`（typo）。

**③ 建议怎么改**

修正 `tsconfig.js` → `tsconfig.json`。

---

#### `docs/plans/2026-10-01-file-change-marker-implementation.md`

- **行数**：1237 ｜ **类别**：实施计划（历史）
- **标题**：文件预览变更标记重做 — 实现计划

**① 这份文档讲什么**

文件变更标记重做的实现计划，含 Task 1-6 的具体步骤。

**② 有没有引用了已经不存在的文件或函数**

同上，`web/tsconfig.js` 应为 `web/tsconfig.json`；其余文件路径（`useFileChangeBaseline.ts`、`FileChangeNav.vue`、`syncMarkersFor`、`diff-marker.css` 细轨改造）均已落地。

**③ 建议怎么改**

修正 `tsconfig.js` → `tsconfig.json`。

---

#### `docs/plans/2026-10-04-ai-group-chat-design.md`

- **行数**：1452 ｜ **类别**：实施计划（历史）
- **标题**：AI 群聊设计（Group Chat）

**① 这份文档讲什么**

AI 群聊的总体设计稿——81 条决策，覆盖主持人/自由两模式、成员身份与发言路由、时间线与存储模型、以及 §0「实现清单（原未实现清单）」的 N1–N5/O1–O19 逐项落地状态。

**② 有没有引用了已经不存在的文件或函数**

§0「已实现（勿重复设计）」里写「`GroupMemberSheet.vue`、`groupRouting.ts` 等已存在」，但 `GroupMemberSheet.vue` **不存在**（真实组件是 `web/src/components/chat/GroupSettingsSheet.vue`，另有 `GroupAvatarStack.vue`）；决策 #78「群成员数上限 10」与今天的 `MaxGroupMembers = 12`（`internal/service/group_store.go:1114`）矛盾；§13.4「选中成员插入空体 `<clawbench-mention targets=…>`」已被 2026-10-08 的成员卡方案取代，但本文未标注；文中提到的 `internal/ai/stream_event.go` 在本文 I2 勘误里已自行说明「该文件不存在」（`StreamEvent` 实为 `internal/ai/interface.go`），但 plan 文件仍写着该路径。

**③ 建议怎么改**

在 §0 增加「2026-10-08 更新：§13.4 已被 2026-10-08-group-mention-card-design.md 取代」并把组件名订正为 `GroupSettingsSheet.vue`；把 #78 的上限改为 12（或注明「已按 `feat/group-duplicate-agents` 调整为 12」）；在 plan 文件中把 `internal/ai/stream_event.go` 一律改写为 `internal/ai/interface.go`。

---

#### `docs/plans/2026-10-04-ai-group-chat-plan.md`

- **行数**：2172 ｜ **类别**：实施计划（历史）
- **标题**：AI 群聊（Group Chat）实现计划

**① 这份文档讲什么**

AI 群聊的 A–M 基线 + N/O 阶段的实施计划（编排器、成员存储、注入、提示词、前端群聊面板与路由卡片），文件头声称「24/24 Task 全部完成并合入 main」。

**② 有没有引用了已经不存在的文件或函数**

多个「新建」产物至今不存在——前端 `GroupMemberBar.vue`、`GroupMemberSheet.vue`（实为 `GroupSettingsSheet.vue`/`GroupAvatarStack.vue`），测试 `useChatSession.group.test.ts`、`useChatStream.concurrent.test.ts`，后端 `internal/handler/chat_group_queue_test.go`、`internal/service/run_turn_timeline_test.go`（现存 `timeline_session_test.go`/`session_executor_timeline_test.go`）；入口 `service.RunGroupTurn`/`RunGroupTurnForSession` **已被删**（`group_orchestrator_test.go:64` 注明「review C1 删除，无生产调用方」），现入口是 `service.RunGroupTurnDrain`（group_orchestrator.go:226）；`internal/grouprouting/testdata/parity_corpus.js` 实为 `.json`；`internal/ai/stream_event.go` 不存在；另 `conn.GetAutoApprove()` 不存在（实为 `ACPConn.IsAutoApprove()`/`SetAutoApprove(bool)`）。

**③ 建议怎么改**

把文件头「24/24 全部完成」改为「主体已落地，以下条目已改名或重构」，逐条补「现路径/现符号」对照表（`RunGroupTurnDrain`、`GroupSettingsSheet.vue`、现有测试文件名）；否则「全部完成」的声明与实际产物名对不上，会误导后续读者按不存在的路径查证。

---

#### `docs/plans/2026-10-08-group-mention-card-design.md`

- **行数**：253 ｜ **类别**：实施计划（历史）
- **标题**：群聊 @ 成员卡（公开点名 + 密送批注）设计

**① 这份文档讲什么**

群聊 @ 成员卡设计——输入框不再显示裸协议标签，@ 成员改为「成员卡」（公开点名）+ 卡上「密送批注」（private），并补齐此前从未实现的用户密送链路。

**② 有没有引用了已经不存在的文件或函数**

本篇已带「§9 实施结果 / §10 第二轮 review 整改」，其声称的落地项**基本属实**（`MentionCard.vue`、`serializeMentionCards`、`stagedMentions`/`restoreStagedCards`、`storeRouteNotes` 按 id 解析、C2 三条泄漏路径 strip、I-2 `targetLabels` 均在代码中）；但 §2「关键事实（已核实）」仍以现状口吻断言「用户密送目前完全没实现」，而 `storeUserNotes` 已在 `group_orchestrator.go:1922` 落地且两种模式都生效，与本文 §5.3/§9 自相矛盾；大量行号已漂移（`group_orchestrator.go:590/609/303`、`ChatMessageItem.vue:332/420`、`groupRouting.ts:288`、`group_inject.go:107`、`chunker.go:35-37`、`chat.go:909`、`session_share_payload.go:173`、`group_store.go:45`）。

**③ 建议怎么改**

把 §2 改标题为「实施前的事实（2026-10-08 基线，已被 §9 推翻）」并加注「用户密送已于 2026-10-09 落地，见 §9」；行号引用统一改为「函数名」定位（如 `storeRouteNotes` / `lookupMember`），避免行号漂移再次产生失效引用。

---

#### `docs/plans/2026-10-09-group-parallel-toggle-design.md`

- **行数**：207 ｜ **类别**：实施计划（历史）
- **标题**：群聊并发开关 + 成员卡锁图标按需显示 设计

**① 这份文档讲什么**

群聊并发开关（用户 @ 的成员并行执行）+ MentionCard 锁图标按需显示。

**② 有没有引用了已经不存在的文件或函数**

无失效引用——后端 `parallelDefault`（`group_store.go:1279/1298`）、前端 `handleToggleGroupParallel`、`resolveSpeakerGroups` 与实施记录均一致；该文档自带「实施结果」段落，状态自洽。

**③ 建议怎么改**

无需修改——plan 本身已记录实施结果，符号名与实际一致。

---

#### `docs/plans/2026-10-10-agent-disable-and-delete-guard-design.md`

- **行数**：287 ｜ **类别**：实施计划（历史）
- **标题**：智能体：会话/任务计数、删除守卫、禁用

**① 这份文档讲什么**

智能体的会话/任务计数展示、删除守卫（有会话/任务/群成员关系时返回 409 `AgentInUse`）与 `agents.disabled` 禁用开关（被禁用者从所有新建入口隐藏，默认智能体不可禁用）。

**② 有没有引用了已经不存在的文件或函数**

整体已落地（`agents.disabled` 迁移 `database.go:1809`、`model.Agent.Disabled`、`AgentPatch.Disabled`、handler 侧 409 守卫与 i18n 键 `AgentInUse`/`CannotDisableDefaultAgent`/`AgentDisabled` 都在），但**计数 API 的形态与文档完全不符**：文档的 `service.AgentUsage(agentID) (sessions, tasks, memberships, err)` 与 `CountSessionsForAgent`/`CountTasksForAgent`/`CountGroupMembershipsForAgent` 三个函数**都不存在**，实际是 `type AgentUsageCounts` + `GetAgentUsage`/`GetAllAgentUsage`（`internal/service/agent_store.go:353/366/405`）；文档描述的 `handler/agent.go` 行号与 `refresh.go:475-485`/`638-676` 等引用均已漂移。

**③ 建议怎么改**

把计数一节改写为现状 API（`AgentUsageCounts`/`GetAgentUsage`/`GetAllAgentUsage`），删掉三个 `Count*ForAgent` 的测试清单或改注「已合并为 `GetAgentUsage`」；行号引用改为函数名定位；文档未标「已完成」，建议在开头补一行落地状态与落地日期。

---

#### `docs/plans/2026-10-10-group-duplicate-agents-design.md`

- **行数**：228 ｜ **类别**：实施计划（历史）
- **标题**：群聊支持同一智能体多实例（临时昵称）设计

**① 这份文档讲什么**

允许同一智能体在一个群里加入多份实例——新增 `chat_sessions.agent_name` 列并回填、用 `validateGroupSpecs` 替代去重、成员上限提到 12、实例命名走内置英文名池并给第 2 个及以后实例分配专属头像。

**② 有没有引用了已经不存在的文件或函数**

文档标「已实施（分支 `feat/group-duplicate-agents`）」，主体属实（`group_name_pool.go` 的 `groupNamePool`/`nextPoolName`、`assignMemberNames`、`insertGroupMemberRows`、`AddGroupMember` 每次新建实例均已落地），但 §2.2/§2.3 以现状口吻描述的 `dedupeGroupSpecs`、以及「`AddGroupMember` 复用已有成员行」的旧语义**已不成立**；文档点名的 `findGroupMemberByAgent` 已不存在，`speakerFromAgent`（`internal/service/speaker_identity.go:106`）也不再覆盖成员名（`speakerFromMember` 以存储的 `agent_name` 为准）；D7「头像：每实例各生成」与今天的规则冲突——只有第 2 个及以后实例才生成头像（`useGroupChat.ts:137-155`）。

**③ 建议怎么改**

把 §2 标注为「实施前现状（已过时）」并补一段「实施后现状」差异（去重函数已删、成员名以 `agent_name` 为准、头像仅 2nd+ 实例）；把 D7 的表述改为引用 2026-10-11 的头像规则变更；`speakerFromMember` 实际签名无 `includeAvatar` 参数，文中相关调用说明需订正。

---

#### `docs/plans/2026-10-10-group-member-identity-plan.md`

- **行数**：1097 ｜ **类别**：实施计划（历史）
- **标题**：群成员身份：内置英文名池 + 按名生成头像 + 真实智能体名披露

**① 这份文档讲什么**

群成员身份方案——内置英文名池（Alice/Bob…）作为同智能体多实例的显示名、每人一个 DiceBear 头像落 `agent_avatar`、真实智能体名在气泡里以小标签披露；含 Phase A–E 的后端、前端、i18n、e2e 任务分解。

**② 有没有引用了已经不存在的文件或函数**

主体已落地（`group_name_pool.go` + `group_instances_test.go`、`speaker_identity.go` 的 `SpeakerIdentity.AgentName`、`SetMemberAvatar`、`/api/group/member-avatars` 路由、`lazyAvatar.ts` 的 `generateAvatarSvg`、`useGroupChat.ts` 的 `generateMemberAvatars`、`group-realAgent` i18n 键、e2e `group-member-identity.spec.ts` 都在），但文档给出的若干代码签名已过期：Task A1 的 `nextPoolName(used map[string]bool)` 实为 `nextPoolName(used, reserved map[string]bool)`（后来加了「真名预留」）；Task B3 的 `speakerFromMember(m, includeAvatar bool)` 实为 `speakerFromMember(m)`；Task B4 的 `SetMemberAvatar` 实际返回 `(int64, error)` 而非 `error`；文档「决策表」中「所有新成员按成员名生成头像、忽略并覆盖智能体原头像」一行已被文档顶部 2026-10-11 变更说明标注过时（现只有第 2 个及以后实例生成），但决策表正文未同步；行号引用（`group_store.go:126-154/688-711/766-790`、`database.go:1201-1223`）已漂移。

**③ 建议怎么改**

把「决策表」中已被顶部变更说明否定的头像行直接删除或加「（已废弃，见顶部变更说明）」标记（避免读者同时看到两条矛盾规则）；三个函数签名按现状订正；行号引用改为函数名定位，并在文首补一句「已全部落地，2026-10-11 头像规则有二次调整」。

---

#### `docs/plans/2026-10-15-notification-center-design.md`

- **行数**：186 ｜ **类别**：实施计划（历史）
- **标题**：通知中心（Notification Center）设计

**① 这份文档讲什么**

通知中心——只追加的事件日志，30 天/500 条双上限，无已读/无角标，点击可跳转。

**② 有没有引用了已经不存在的文件或函数**

状态写「设计已定稿，待实现」，**实际已落地**（`internal/service/notifications.go` 的 `StoreNotification`/`ListNotifications`/`ClearNotifications`/`CleanupNotifications`、`notifications` 表无 `read_at` 列、dock 页签与 `NotificationsPanelContent.vue` 都在）；设计本身与实现一致。

**③ 建议怎么改**

状态改「已实现」，并把「只追加、无已读/无角标」这一关键约束作为落地现状保留。

---

#### `docs/plans/2026-10-15-single-chat-auto-cruise-design.md`

- **行数**：245 ｜ **类别**：实施计划（历史）
- **标题**：单聊自动巡航（Single-chat Auto-Cruise）设计

**① 这份文档讲什么**

单聊自动巡航——每轮回复后若未声明完成则注入确认轮，直到 AI 发出 `<clawbench-goal-satisfied/>` 或撞上限。

**② 有没有引用了已经不存在的文件或函数**

无。`internal/service/cruise.go`（`NewCruiseRunner`、`BuildCruiseSingleConfirmPrompt`）、`GetSessionCruise`/`SetSessionCruise`、`GoalSatisfied` 解析、`useSessionIdentity` 的 `cruise` ref、`ChatInputBar` 的巡航 chip 全部落地，`<clawbench-goal-satisfied/>` 标签由 `grouprouting.go` 解析。

**③ 建议怎么改**

状态改「已实现」；`internal/service/auto_continue.go` 与 `cruise.go` 的分工可补一句说明（对称的 drain 循环）。

---

#### `docs/plans/2026-10-18-group-member-context-design.md`

- **行数**：278 ｜ **类别**：实施计划（历史）
- **标题**：群成员上下文信息（成员列表展示 + 详情面板）设计

**① 这份文档讲什么**

群成员上下文信息——成员花名册每行展示用量（Activity 图标 + 28×6 细进度条 + 百分比），点击打开详情面板复用单聊的上下文面板。

**② 有没有引用了已经不存在的文件或函数**

标「已实现」基本属实——`service.GroupMemberContexts`（`group_store.go:899/911`）、`web/src/utils/usageDisplay.ts`、`web/src/components/chat/ContextUsageDetail.vue`、`POST /api/group/member-compact`（`internal/service/group_member_compact.go`）、`GroupMemberInfo.usage/model`（`useGroupChat.ts:28/30`）、1.5s 去抖重拉（`GroupSettingsSheet.vue:287/306`）全在；**唯一不符**：文档新建的 `web/src/components/chat/MemberContextSheet.vue` **不存在**——实现改为复用现成的 `web/src/components/chat/SessionDrawer.vue`（`GroupSettingsSheet.vue:101-107`），文末「实现记录」的四处差异**没有记录这一项**。

**③ 建议怎么改**

在「实现记录」补第五条「详情面板改为嵌套复用 `SessionDrawer.vue`，未新建 `MemberContextSheet.vue`」；文档里的路径按现状订正。

---

#### `docs/plans/2026-10-18-unified-group-mode-design.md`

- **行数**：574 ｜ **类别**：实施计划（历史）
- **标题**：群聊模式合一（主持人 = 可选协调者）设计

**① 这份文档讲什么**

群聊模式合一——主持人 = 可选协调者，共用一条循环；含成员提问降级、删除 `group_mode` 等 18 条决策。

**② 有没有引用了已经不存在的文件或函数**

决策 #1「不再有模式」**未落地**——`context_state.group_mode` 仍在（`group_store.go:410`、`GetGroupMode` group_store.go:1192），`runLoop` 仍按模式分派到 `runFreeLoop`/`runRounds` 两个独立循环；决策 #15「删除 `context_state.group_mode`」未做；决策 #17 依赖的 `ai.DowngradeAskQuestionBlocks` **不存在**（`internal/ai/block_helpers.go` 在但无该函数），`SuppressAskQuestion` 全仓零命中；文档「文档同步」要求在 `2026-10-04-ai-group-chat-design.md` 文末追加「2026-10-18 模式合一」一节——**未追加**（该设计稿末尾仍是 §15）。

**③ 建议怎么改**

确认该 plan 是否已暂停或放弃实施——若仍在进行，需实现 `ai.DowngradeAskQuestionBlocks`、在 `group_orchestrator.go` 中添加 `SuppressAskQuestion` 逻辑、并推进 `group_mode` 的删除与 `runLoop` 统一；若已搁置，应在文档头部加「状态：已暂停」标记。

---

#### `docs/plans/2026-10-19-group-bcc-authority-design.md`

- **行数**：226 ｜ **类别**：实施计划（历史）
- **标题**：主持人模式下的密送权限（成员间禁止互发）设计

**① 这份文档讲什么**

主持人模式下禁止成员之间互发密送（其余组合允许、自由模式全放行），判定落在写入侧 `storeRouteNotes`；顺带修五个密送链路旧问题。

**② 有没有引用了已经不存在的文件或函数**

状态标「已实施」**属实**——`noteAllowed`/`notePolicy`（group_orchestrator.go:182/1841）、`storeRouteNotes` 带 policy、`sender_role`/`sender_name` 列与迁移（database.go:399-417）、`maxPendingBccPerTarget` 上限、`pendingBccHeader`/`joinPendingBcc` 署名、`GetMessagesBySessionIDRawAfter`（chat.go:419）增量注入、`project_delete.go` 的 `group_pending_bcc` 清理、`deletePendingBccForGroup` 死代码已删、`BuildMemberSystemPrompt(members, selfName, hostName)`（group_prompt.go:152）全部落地；文档同步要求也已完成（`2026-10-04-ai-group-chat-design.md` 的 #86标注「已被 #97 取代」、#99 权限规则均在）。

**③ 建议怎么改**

无需修改（可选：补一句 `2026-10-18 模式合一` 尚未落地，故 #99 仍按「有主持人=host 模式」实现）。

---

#### `docs/plans/2026-10-19-mcp-config-design.md`

- **行数**：818 ｜ **类别**：实施计划（历史）
- **标题**：统一 MCP 配置（全局定义 + 按智能体启用）设计

**① 这份文档讲什么**

统一 MCP 配置——全局 `config.yaml` 的 `mcp.servers` + 按智能体的启用策略（all/none/selected），仅 ACP 传输生效，按能力位过滤。

**② 有没有引用了已经不存在的文件或函数**

状态写「设计待评审」，**实际已落地**——`internal/model/mcp.go`、`agents.mcp_servers` 策略（`agent_store.go:491/577`）、`PATCH /api/config` 的 `mcp.servers` 整体替换（`handler/settings.go:1488`）、`/cb-mcp` 斜杠命令注入（`handler/clawbench_command.go:153-175`）、前端 `AgentMCPSection.vue`、openapi.yaml 已写 `mcp.servers`/`mcp_servers`；文档 §1 的现状陈述「ClawBench 目前完全不管理 MCP / `mcpServers` 是四处硬编码的空切片」**已过时**；清单第 13 项 `internal/handler/mcp_test.go`（新，`POST /api/mcp/test`）标「未做」，但连通性测试**已实现**且路径不同——`POST /api/config/test` 带 `category=mcp`（`internal/handler/config_test_connectivity.go:83/104`，探针在 `internal/mcp`）。

**③ 建议怎么改**

状态改「已实现」；把 §1 现状段改写为历史陈述；第 13 项改为「已实现于 `/api/config/test`（category=mcp），而非 `/api/mcp/test`」。

---

#### `docs/plans/2026-10-19-usage-meter-design.md`

- **行数**：486 ｜ **类别**：实施计划（历史）
- **标题**：用量提醒（Usage Meter）设计

**① 这份文档讲什么**

用量提醒——设一条「周期 + 指标 + 额度」规则，按线性基准线与实际用量对比，越线/触顶各提醒一次（只通知不拦截），header 上可视化。

**② 有没有引用了已经不存在的文件或函数**

状态标「已实现（2026-10-19）」**属实且路径全对**——`internal/handler/usage_meter.go(_test)`、`internal/service/usage_meter.go(_test)`、`usage_meter_worker.go(_test)`、`web/src/composables/useUsageMeter.ts`、`web/src/utils/usageFormat.ts`、`GET /api/usage/meter`（handler.go:528）、通知 kind `usage`（`notifications.go:64`）、前端 `UsageMeterPopover.vue` 全部在。

**③ 建议怎么改**

无需修改。

---

#### `docs/superpowers/plans/2026-08-02-thinking-lazy-load.md`

- **行数**：1983 ｜ **类别**：实施计划（历史）
- **标题**：Thinking 独立存储与懒加载 Implementation Plan

**① 这份文档讲什么**

将 thinking 思考文本从 `chat_history.content` 拆到独立表 `chat_thinking`，前端按需懒加载的实现计划。

**② 有没有引用了已经不存在的文件或函数**

1. **「Known Tradeoffs」中声称 `rag message <id>` CLI（`internal/cli/rag.go`）会影响 thinking 显示**——`internal/cli/rag.go` 已不存在（CLI 的 task/rag 子命令已移除），该现状描述已过时。
  2. **Task 5 Step 1 测试代码写 `service.UnsafeDBForTest()`**——`UnsafeDBForTest` 实际在 `internal/store` 包（`store.UnsafeDBForTest()`），`internal/service` 包下不存在此符号。文档中该测试代码会编译失败（`thinking_test.go` 是 `package service`，应使用 `store.UnsafeDBForTest()`）。
  3. **Task 1 参考文件列「`internal/service/tool_calls.go`（CRUD）」、`internal/handler/chat_history.go:216`（`ServeToolCallDetail`）**——这些是文档参照的现有模式文件，路径属实（`tool_calls.go` 存在、`chat_history.go` 中 `ServeToolCallDetail` 在第 45 行）。

**③ 建议怎么改**

1. Known Tradeoffs 中删除 `rag message <id>` CLI 段落，或更新为「rag 能力现在由 `/api/rag/message` 端点提供（handler 存在），`rag message` CLI 已移除」。
  2. Task 5 Step 1 测试代码中将 `service.UnsafeDBForTest()` 改为 `store.UnsafeDBForTest()`（在 `internal/service/chat_test.go:1162` 中已确认 store 包已导入）。

---

#### `docs/superpowers/plans/2026-08-03-bigscreen-splitview.md`

- **行数**：1331 ｜ **类别**：实施计划（历史）
- **标题**：大屏双栏布局（Big-Screen SplitView）实现计划

**① 这份文档讲什么**

为 ≥1024px 宽屏实现双栏布局（纵向 Dock + 可拖动 SplitView）的实现计划。

**② 有没有引用了已经不存在的文件或函数**

1. **Task 2/3/4 多次引用 `useBigScreenLayout.ts`**（新建文件）——同名文件不存在。实际实现用的是 **`useWideScreenLayout.ts`**（`web/src/composables/`），功能完全对应：含 `isWideScreen`、`leftTab`、`splitRatio`、`registerWideScreenCallbacks`（对应文档的 `registerBigScreenCallbacks`）、`_setBigScreenForTest`（对应 `_setBigScreenForTest`）。只是命名从 "Big Screen" 改为 "Wide Screen"、最小宽度 960px（非 1024px）。文档说新建 `useBigScreenLayout.ts`，实际等价文件名为 `useWideScreenLayout.ts`。
  2. **Task 2 引用 `web/src/composables/__tests__/useBigScreenLayout.test.ts`**（不存在）——实际测试文件名应为 `useWideScreenLayout.test.ts`（已存在）。

**③ 建议怎么改**

1. 将全文中所有 `useBigScreenLayout` 引用替换为 `useWideScreenLayout`（包括常量名 `BIG_SCREEN_*` → `WIDE_SCREEN_*`，函数名 `switchLeftTab` 保留但所在模块名改了，`registerBigScreenCallbacks` → `registerWideScreenCallbacks`）。
  2. 将最小宽度描述从 1024 更新为 960（实际 `WIDE_SCREEN_MIN_WIDTH = 960`）。

---

#### `docs/superpowers/plans/2026-08-03-connection-overlay.md`

- **行数**：843 ｜ **类别**：实施计划（历史）
- **标题**：Unified Fullscreen Connection Overlay Implementation Plan

**① 这份文档讲什么**

用全屏遮罩替代 Header 黄色闪烁图标，显示断线重连 / 重启状态。

**② 有没有引用了已经不存在的文件或函数**

无。文档计划新建的文件和符号均已实现：
  - `web/src/composables/useConnectionOverlay.ts` **已存在**（含 `useConnectionOverlay()`、`ConnectionOverlayMode` 类型）
  - `web/src/components/common/ConnectionOverlay.vue` **已存在**
  - `useConnectionOverlay.ts` 中正确使用 `ws.MessageTypeEvent`（`internal/ws/protocol.go:6` 定义为 `"event"`，`summary.go:142` 广播 `summary_update` 事件时也正确使用）
  - `restartingOverlay` 已在 `useSettingsNavigation.ts` 中提升为模块级导出
  - `hasConnectedOnce` 已在 `useGlobalEvents.ts` 中实现

**③ 建议怎么改**

无需修改。

---

#### `docs/superpowers/plans/2026-08-05-file-edit-mode.md`

- **行数**：745 ｜ **类别**：实施计划（历史）
- **标题**：文件源码编辑模式 Implementation Plan

**① 这份文档讲什么**

为文件查看器增加 CodeMirror 6 编辑模式（切换按钮 + 保存逻辑）的实现计划。

**② 有没有引用了已经不存在的文件或函数**

1. **Task 4 新建 `web/src/components/file/CodeEditor.vue`**——**此文件不存在**。实际编辑功能由 `CodeMirrorViewer.vue`（已有，`editing` 模式内置）和 `useFileEditor.ts` composable 实现（`editing` ref 在模块级别共享），与文档计划的独立 `CodeEditor.vue` 组件完全不同。
  2. **Task 4 计划 import `CodeEditor from './CodeEditor.vue'`**——该路径无文件，`FileViewer.vue` 实际不使用此 import。
  3. **Task 5 i18n `file.editor.save` 等键**——这些键在 `zh.ts`/`en.ts` 中是否真实存在需人工确认（`file-edit-mode` 计划新增，未见明显冲突）。

**③ 建议怎么改**

1. 将 `CodeEditor.vue` 替换为对 `CodeMirrorViewer.vue` 编辑模式的描述，或明确说明两者关系。
  2. 更新文件路径引用：`useFileEditor.ts`（已存在）替代 `CodeEditor.vue`，`CodeMirrorViewer.vue` 替代计划中的新编辑器组件。

---

#### `docs/superpowers/plans/2026-08-07-terminal-select-mode.md`

- **行数**：933 ｜ **类别**：实施计划（历史）
- **标题**：终端三态交互（浏览 / 手势 / 选区）实现计划

**① 这份文档讲什么**

终端三态交互（浏览 / 手势 / 选区），用选区态拖选替代 `OutputDrawer` 抽屉。

**② 有没有引用了已经不存在的文件或函数**

1. **Task 6 计划「删除 `web/src/components/terminal/OutputDrawer.vue`」**——**此文件从未存在过**，`web/src/components/terminal/` 目录下现有文件为 `KeyConfigDrawer.vue`、`QuickCommandDrawer.vue`、`TerminalHelpDrawer.vue`、`TerminalInputDrawer.vue`、`TerminalPanelContent.vue` 等，无 `OutputDrawer.vue`。
  2. **Task 6 引用 `TerminalPanelContent.vue` 第 163-169 行 `<OutputDrawer .../>`**——`TerminalPanelContent.vue` 中无此引用，文档的删除目标本不存在。
  3. **文档 Architecture 提到「移除抽屉」**——`OutputDrawer` 抽屉本不存在，说明该功能可能从未实现过（或者计划移除的对象一开始就没有）。

**③ 建议怎么改**

1. 删除 Task 6 中关于删除 `OutputDrawer.vue` 的步骤（如该抽屉从未存在则无需删除）。
  2. 确认现有 Terminal 交互模式：实际上 `useTerminalGestures.ts` 已实现了三态（`'browse' | 'gesture' | 'selection'`，`TerminalMode` 类型在文件头部定义），`zh.ts` 已有 `modeBrowse`/`modeGesture`/`modeSelection` 键——说明该功能可能已经以另一种方式实现。

---

#### `docs/superpowers/plans/2026-08-08-summary-first-chat-loading.md`

- **行数**：1029 ｜ **类别**：实施计划（历史）
- **标题**：Chat Summary-First Loading Implementation Plan

**① 这份文档讲什么**

会话加载时默认只返回摘要（含卡片元数据），切换到原文时懒加载的实现计划。

**② 有没有引用了已经不存在的文件或函数**

1. **Architecture「`GetChatHistoryPaged` gains a `view=summary` mode」**——`GetChatHistoryPaged` 当前签名**没有** `summaryView bool` 参数（`func GetChatHistoryPaged(projectPath, backend, sessionID string, limit int, beforeID int)`，`internal/service/chat.go:73`），不存在摘要过滤逻辑。
  2. **Task 4「handler 解析 `view` 查询参数」**——`AIChat` handler（`internal/handler/chat.go:30`）目前只解析 `session_id`、`limit`、`before_id`、`before` 四个参数，**没有**解析 `view` 参数。
  3. **frontend 已被实现**：前端 `useChatSession.ts` 多处已使用 `view=summary`（第 658、659、717、869 行），前端期望后端按 `view=summary` 过滤内容，但**后端完全忽略该参数**，所有会话均返回完整 `content`。
  4. **已实现部分**：`SummaryCards` 模型（`internal/model/chat.go:239`）、`summary_cards` 列迁移（`database.go:679`）、`SummaryUpdateData` 含 `SummaryCards`（`protocol.go:111`）、`broadcastSummaryUpdate` 使用 `ws.MessageTypeEvent`（`summary.go:142`）均已实现。

**③ 建议怎么改**

1. Architecture 和 Task 4 的描述与代码现状不符——`view=summary` 端到端未实现（前端已发、后端未收），应补充 Task 4 实现（`GetChatHistoryPaged` 加 `summaryView bool` 参数、`AIChat` 解析 `view=summary`、在 `enrichMessagesWithSummaries` 中当 `summaryView=true` 时清空已摘要消息的 `content`）。
  2. `MessageTypeEvent` 的用法（`summary.go:142`）**正确**：`const MessageTypeEvent = "event"`（protocol.go:6），summary 广播正确使用了该常量。

---

#### `docs/superpowers/plans/2026-08-10-clawbench-android-rename.md`

- **行数**：133 ｜ **类别**：实施计划（历史）
- **标题**：ClawBench Android 桥注入名改名（AndroidNative → ClawBenchNative）实现计划

**① 这份文档讲什么**

把 Android WebView 注入的 JS 桥名从 `AndroidNative` 机械改名为 `ClawBenchNative`（含 Java 注入名、`JSErrorInjector`、静态 `login.html` 与相关单测），与前端平台无关桥层契约对齐。

**② 有没有引用了已经不存在的文件或函数**

- **【严重·现状陈述已过期】文档声称改名「待实施」（全部 `- [ ]` 未勾选），实际早已完成。** `android/app/src/main/java/com/clawbench/app/MainActivity.java:693` 现为 `webView.addJavascriptInterface(new WebAppInterface(this), "ClawBenchNative");`，`:2686` 现为 `view.evaluateJavascript(JSErrorInjector.buildScript("ClawBenchNative"), null);`；`android/app/src/main/java/com/clawbench/app/JSErrorInjector.java:7`/`:25`、`BrowserActivity.java:58` 注释也已是新名；`android/app/src/main/assets/login.html` 中 `ClawBenchNative` 出现 10 次、`AndroidNative` 0 次；`android/app/src/main/java` 下已无 `AndroidNative` 残留。
  - **【行号漂移】** 文档指的具体行号全部失效：注入名不在第 422 行（实际 693）、`buildScript` 不在第 1761 行（实际 2686）、三处注释不在 342/879/1802 行（实际约 148/484/1256/2731）；`JSErrorInjector.java` 的 7/25 与 `BrowserActivity.java:58` 仍对得上。
  - **【现状陈述与代码矛盾】** 文档「后续」段称「Java `WebAppInterface` 中 frontend **已不调用**的方法 `dismissSplash`/`setVolumeKeyMode`/`setTerminalSessionCount`/`stopBackgroundService` 保留不动」——实际前端仍在调用：`web/src/components/App.vue:3487`、`:3515` 调 `dismissSplash`，`web/src/components/terminal/TerminalPanelContent.vue:966`、`:982` 调 `setVolumeKeyMode`/`setTerminalSessionCount`，`web/src/composables/usePortForward.ts:683` 调 `stopBackgroundService`。
  - **【计数期望不符】** 文档 Task 2 Step 2 期望「ClawBenchNative 计数 = 8」，实际 `login.html` 现为 10 处（后续新增了桥调用）。
  - 文档提到的 `android/app/src/test/java/com/clawbench/app/JSErrorInjectorTest.java`、`MainActivityPreAuthTest.java` **均真实存在**且已断言 `ClawBenchNative`（`JSErrorInjectorTest.java:14-25`、`:84`；`MainActivityPreAuthTest.java:46`）；包名/应用名现状亦与文档不矛盾：`android/app/src/main/java/com/clawbench/app/` 目录在、`android/app/build.gradle:68` `namespace 'com.clawbench.app'`、`:72` `applicationId "com.clawbench.app"`、`:116` `applicationIdSuffix ".debug"`、`android/app/src/main/res/values/strings.xml:3` `app_name` = ClawBench。

**③ 建议怎么改**

1. 在标题下加一行「状态：已完成（实施于 2026-08 期间）」并把 Task 1–3 的 `- [ ]` 改为 `- [x]`，避免读者误以为待办。
  2. 把「后续」段的「frontend 已不调用」改成实际状态：这 4 个方法前端仍在调用，故 Java 侧保留是**必需**而非「降风险」；并指向 `2026-08-10-clawbench-frontend-bridge.md`（那份计划才打算移除这些调用，且至今未移除）。
  3. 删除/更新所有具体行号，改用符号名定位（`addJavascriptInterface(..., "ClawBenchNative")`、`buildScript("ClawBenchNative")`），行号在多次迭代后必然漂移；`login.html` 计数改为「≥1 且旧名为 0」而非固定 8。

---

#### `docs/superpowers/plans/2026-08-10-clawbench-frontend-bridge.md`

- **行数**：986 ｜ **类别**：实施计划（历史）
- **标题**：ClawBench 前端桥层抽象（ClawBenchNative）实现计划

**① 这份文档讲什么**

把前端直连 `window.AndroidNative` 的调用收敛到平台无关桥层 `web/src/utils/clawbenchNative.ts`（`window.ClawBenchNative`），读操作改异步，并移除若干「spec 明确跳过」的桥方法调用。

**② 有没有引用了已经不存在的文件或函数**

- **【严重·现状陈述错误】`web/src/components/VersionMismatchOverlay.vue` 不存在。** 该文件已在提交 `118b16ec3`（2026-09-13）中被删除；文档 Task 7 列它为 Modify 并给出具体编辑步骤。版本不一致提示现由原生层实现：Android `android/app/src/main/java/com/clawbench/app/MainActivity.java:1411-1417`（用 `R.string.version_mismatch_*`，配套 `android/app/src/test/java/com/clawbench/app/MainActivityVersionMismatchTest.java`），桌面 `desktop/src/main/splash.ts:57/281/283/297/441`（`showVersionMismatch` / `window.__versionMismatchShow`）；Web 侧只剩 i18n `upgrade.versionMismatch`（`web/src/i18n/locales/zh.ts:3563`、`en.ts:3567`）与 `web/src/components/UpgradePromptOverlay.vue`。
  - **【现状陈述与代码矛盾】** 文档称将移除 `setVolumeKeyMode`/`setTerminalSessionCount`/`dismissSplash`/`stopBackgroundService` 的调用，实际这些调用**依然存在**：`web/src/components/terminal/TerminalPanelContent.vue:966`、`:982`、`web/src/composables/usePortForward.ts:683`、`web/src/components/App.vue:3487`、`:3515`。
  - **【现状陈述过期】桥契约清单与实际不符。** 文档描述的 `ClawBenchNative` 接口只含少量方法；实际 `web/src/utils/clawbenchNative.ts:27` 的接口还包含 `isDesktopApp`、`setLanguage`、`setFloatingWindowEnabled`、`setLiveUpdateEnabled`、`dismissSplash`、`stopBackgroundService`、`setVolumeKeyMode`、`setTerminalSessionCount`、`showSelectionMenu`、`cancelSplash`、`canPostPromotedNotifications`、`openLiveUpdateSettings`、`getTunnelTransport`、`setTunnelTransport`、`saveServerNamed`、`reconnectTunnelAsync` 等。
  - **【现状陈述过期·已落地】** 文档多处写「当前 `usePortForward.ts` 直接用 `window.AndroidNative`」「`useServerList` 用同步 `localStorage`」等现状，实际已改造：`web/src/composables/usePortForward.ts:12-13` 改为导入 `getNative` 与 `reconnectTunnel`（别名 `nativeReconnectTunnel`），文档提到的 `RECONNECT_ASYNC_TIMEOUT_MS`/`RECONNECT_CALLBACK_NAME`/`getAndroidNative` 均已不存在；`web/src/composables/useServerList.ts:2` 已导入 `getNative`，`load/save/remove` 已异步化（`:65`/`:77`/`:103`），`saveServerNamed` 在 `:80`。
  - **【行号漂移】** 文档给出的 `App.vue:1096 / 1913 / 1995-2001 / 2079-2086` 等行号均已失效。
  - 其余引用路径均存在：`web/src/utils/clawbenchNative.ts`（导出 `getNative`:314、`isNativeApp`:319、`isDesktopApp`:333、`callNative`:346、`reconnectTunnel`:360）、`web/src/utils/__tests__/clawbenchNative.test.ts`（18 个 `it(`）、`web/src/composables/useAppMode.ts:2`（导入 `isNativeApp`+`isDesktopApp`）、`web/src/utils/appLog.ts:16`（导入 `getNative/isNativeApp/isDesktopApp`）。

**③ 建议怎么改**

1. 把 Task 7 中 `VersionMismatchOverlay.vue` 整节改写为「该组件已于 2026-09-13 删除，版本不一致提示现由 Android `MainActivity.java` 与桌面 `splash.ts` 的原生层负责」，并补上 `web/src/components/UpgradePromptOverlay.vue` + i18n `upgrade.versionMismatch` 作为 Web 侧现状。
  2. 删除「移除 4 个桥调用」这一未兑现的承诺，或改写为「这些调用仍在使用，保留桥方法为必需」——否则后续读者会误删仍在工作的代码。
  3. 用 `web/src/utils/clawbenchNative.ts:27` 的真实接口定义替换文档里的接口草稿，并删掉具体行号引用。

---

#### `docs/superpowers/plans/2026-08-11-voice-input-asr.md`

- **行数**：1931 ｜ **类别**：实施计划（历史）
- **标题**：语音输入（ASR）实现计划

**① 这份文档讲什么**

新增 ASR 语音输入能力：`internal/stt` provider 包、非流式 `POST /api/stt/transcribe` 与流式 `WS /api/stt/transcribe/ws`、`model.STTConfig` 配置、设置面板、`useVoiceInput` composable 与 ChatInputBar 长按录音。

**② 有没有引用了已经不存在的文件或函数**

- **【路径不存在】`internal/stt/transcribe.go` 与 `internal/stt/transcribe_test.go` 不存在。** `internal/stt/` 实际只有 `interface.go`、`vllm_stt.go`、`vllm_stt_test.go`。**【现状陈述错误】STT 转写入口现不在 `internal/stt`，而在 `internal/handler/stt.go`**：`SetSTTProvider`:47、`GetSTTProvider`:55、`STTTranscribe`:67、`STTTranscribeWS`:151（配套 `internal/handler/stt_test.go`）。
  - **【现状陈述与代码矛盾·默认值错误】** 文档多处（行 7、430、453、475、1419、1702、1770、1850）称默认快捷键为 **"Alt+Space"**，实际默认是 **"F9"**：`internal/model/defaults.go:390-405`（`:404`）、`internal/model/defaults_test.go:175-176`、`web/src/composables/useVoiceInput.ts:62`、`web/src/composables/useSettingsConfig.ts:574`、`web/src/i18n/locales/zh.ts:2983`（「默认 F9」）；`internal/model/config.go:305` 注释也是 F9。
  - **【路径不存在】`web/src/components/chat/ChatInputBarVoice.test.ts` 不存在。** 前端语音输入实际在 `web/src/composables/useVoiceInput.ts`（测试 `web/src/composables/useVoiceInput.test.ts`）与 `web/src/components/chat/__tests__/ChatInputBar.test.ts`（mock 在 `:437`，`describe('voice input')` 在 `:3646`）；`web/src/components/chat/` 下没有任何 Voice 组件文件。
  - **【现状陈述过期·已落地】** 文档把 `model.STTConfig`、设置面板、provider 装配等整体描述为待建，实际已全部落地：`internal/model/config.go:297-306`（`model.STTConfig`）、`:149`（`Config.STT`）、`internal/model/defaults.go:390-405`；`internal/handler/settings.go`（`configSTT`:451、白名单 `:119`/`:840`、校验 `:1455`、应用 `:2125`）；`web/src/composables/settingsFieldMap.ts:593-605`（stt / stt_engine 面板）、`web/src/components/settings/SettingsIndex.vue:84`（`{ id: 'stt', icon: Mic }`）；`internal/handler/config_test_connectivity.go` 的 `testSTT`；`cmd/server/main.go` 的 provider 构建/热重建。
  - **【现状陈述过期·路由已存在】** 文档描述「路由待注册」，实际 `internal/handler/handler.go:618-619` 已注册；且文档写的 `middleware.Auth(STTTranscribe)` 是冗余的——该 `register`（定义于 `:446-449`）本身已包 `middleware.Auth`。
  - **【行号漂移】** 文档给出的 `cmd/server/main.go` 「约第 614 行后」等定位已失效（provider 构建现位于文件后段 `newSTTProvider` 附近）。
  - 注：文档使用的 `model.ConfigInstance.STT.BaseURL` **写法正确**——`internal/model/config.go:427` 确有顶层 `var ConfigInstance Config`（`go doc ./internal/model ConfigInstance` 可证）。此处不是失效引用，但 `/tmp/audit_brief.md` 的「已知符号真相」把它判为非顶层符号，是 brief 自身有误，勿据以「修正」文档。

**③ 建议怎么改**

1. 把 STT 入口的现状描述改为「`internal/stt/` 只放 provider 实现（interface.go / vllm_stt.go），HTTP+WS 入口在 `internal/handler/stt.go`」，并把缺失的两个 `internal/stt/transcribe*.go` 从文件清单移除。
  2. 全局把默认快捷键 "Alt+Space" 改为 "F9"（含 code 示例、默认值表、设置页说明与 i18n 文案），否则照文档实现会与现有默认值/测试冲突。
  3. 把「语音测试文件 `web/src/components/chat/ChatInputBarVoice.test.ts`」改为实际位置 `web/src/composables/useVoiceInput.test.ts` 与 `web/src/components/chat/__tests__/ChatInputBar.test.ts`；路由注册一行改为引用 `internal/handler/handler.go:618-619` 并去掉多余的 `middleware.Auth` 包裹。

---

#### `docs/superpowers/plans/2026-08-12-terminal-theme-switch.md`

- **行数**：671 ｜ **类别**：实施计划（历史）
- **标题**：终端独立主题切换（157 主题懒加载）Implementation Plan

**① 这份文档讲什么**

为 Web 终端接入 `xterm-theme` 社区包的全部 157 种主题，支持手动选择、自动（跟随应用深浅色）、搜索与预览，并落地设置持久化与终端面板主题弹层。

**② 有没有引用了已经不存在的文件或函数**

- **【现状陈述错误·函数名不存在】** 文档的架构说明写「`loadAllThemes()` 一次性加载全部主题」，实际 `web/src/utils/terminalThemes.ts` 导出的是 **`loadThemesModule`（:72，懒加载）**，仓库中不存在 `loadAllThemes`。
  - **【现状陈述与代码矛盾·功能已被主动移除】** 文档 Task 6 要求主题弹层含 `themeSearchPlaceholder` 搜索框与 `filteredThemes` 过滤逻辑，实际二者已被**刻意删除**（提交 `68c91379`，2026-08-22）：`web/src/components/__tests__/terminalPanelSelection.test.ts:80-81` 明确断言 `expect(source).not.toContain('themeSearchPlaceholder')` 与 `not.toContain('filteredThemes')`；i18n 键 `terminal.themeSearchPlaceholder`（`zh.ts:3503`/`en.ts:3507`）成为残留未用键。
  - **【现状陈述过期·已落地】** 文档整体把该特性描述为待建，实际已完整实现：`web/src/utils/terminalThemes.ts` 导出 `TERMINAL_THEME_AUTO`(:4)、`TERMINAL_THEME_STORAGE_KEY`(:5)、`THEME_IDS`(:11，恰好 157 项)、`SORTED_THEME_IDS`(:448)、`resolveTheme`(:205)、`resolveThemeSync`、`resolveAutoTheme`/`resolveAutoThemeSync`、`formatThemeName`(:195)、`resetThemesCache`、`parseHexColor`、`colorDistance`、`ensureVisibleSelection`、`findClosestThemeByBackground`、`getThemeBackground`、`buildTerminalThemePreviews`、`isAppDarkTheme`、`getAppThemeBg`；`web/src/types/xterm-theme.d.ts`；`web/src/utils/__tests__/terminalThemes.test.ts:63` 断言 `THEME_IDS.length === 157`。
  - **【现状陈述过期·未在文档中的实现】** 终端面板主题 UI 现落在 `web/src/components/terminal/TerminalPanelContent.vue`：调色板按钮 `data-action="theme"`(:65)、主题弹层(:245-286)、`getXtermTheme`(:530)、`tabManager.updateTheme`(:562)、`openThemeMenu`(:566)、MutationObserver(:1348/1364)；另有**文档未提及的第二套实现** `web/src/components/settings/SettingsGroupPanel.vue` + `settingsFieldMap.ts:492`（terminalTheme 下拉 + 预览 + 重试），见 `web/src/components/settings/__tests__/TerminalThemePreviewIntegration.test.ts`。
  - **【依赖位置错误】** 文档称 `xterm-theme` 装在 `web/package.json`，实际只在仓库根 `package.json:84`（`"xterm-theme": "^1.1.0"`）；`web/package.json` 里没有该依赖。
  - **【现状陈述过期】** 设置持久化部分文档给的是「新建」描述，实际 `web/src/composables/useSettingsConfig.ts` 已有 `localDefaults.terminalTheme='auto'`(:391) 与 `legacyKeys.terminalTheme`(:139)。
  - 文档引用的其余路径均存在（`web/src/types/xterm-theme.d.ts`、`web/src/components/common/PopupMenu.vue` 等）。

**③ 建议怎么改**

1. 把架构段 `loadAllThemes()` 改为 `loadThemesModule()`（懒加载 `import('xterm-theme')`），并说明其返回值与缓存重置 `resetThemesCache`。
  2. 删除 Task 6 中关于搜索框 `themeSearchPlaceholder` / `filteredThemes` 的要求，改写为「搜索功能在 2026-08-22 已被移除（见 `terminalPanelSelection.test.ts:80-81`），并清理 i18n 残留键 `terminal.themeSearchPlaceholder`」——否则文档会指导读者把已废弃的功能再加回来。
  3. 补上文档未覆盖的两处现状：终端面板 `TerminalPanelContent.vue` 的主题弹层与设置面板 `SettingsGroupPanel.vue` 的第二套入口；并把依赖安装位置从 `web/package.json` 更正为仓库根 `package.json`。

---

#### `docs/superpowers/plans/2026-08-13-dir-jump-copy.md`

- **行数**：834 ｜ **类别**：实施计划（历史）
- **标题**：目录快速跳转 + 面包屑复制路径 Implementation Plan

**① 这份文档讲什么**

新增共享「跳转到路径」对话框 `JumpDirDialog.vue`，在文件管理器与项目选择器加跳转按钮，并在目录面包屑加一键复制绝对路径按钮，配套 `jump:` i18n 文案。

**② 有没有引用了已经不存在的文件或函数**

- **【现状陈述错误·i18n 文案与代码不符】** 文档给出的 `jump` 文案与实际不一致：实际 `web/src/i18n/locales/zh.ts:2027` 起 —— title 是「跳转到**路径**」（文档写「跳转到目录」）、`copyPath` 是「**拷贝**路径」（文档写「复制路径」）；实际还多一个文档未列的键 `placeholderBrowse`（用于 ProjectDialog 的占位符）。
  - **【现状陈述错误·按钮类名不存在】** 文档要求 `JumpDirDialog` 用 `.jump-confirm-btn` / `.jump-cancel-btn`，实际用共享的 `.fbtn` / `.fbtn-primary`；`web/src` 全量 grep 无 `jump-confirm-btn`/`jump-cancel-btn`，且 `web/src/components/file/__tests__/JumpDirDialog.test.ts` 断言的是 `.fbtn-primary`。
  - **【现状陈述错误·函数与行为不符】** 文档称「文件管理器确认跳转后调用 `store.navigateToDir`」，实际 `web/src/components/file/FileManagerContent.vue` 的 `handleJumpConfirm`(:819) 调用的是 **`navToFileInManager(path)`**（`web/src/composables/useFilePathAnnotation.ts`）；`store.navigateToDir`（`web/src/stores/app.ts:751`）确实存在，但由 `web/src/components/App.vue:2656` 使用。
  - **【现状陈述过期·实现方式已改】** 文档描述面包屑自写 `copyFullPath` 函数并加 `copied` class，实际改为复用共享组件 `web/src/components/common/CopyButton.vue`：`web/src/components/file/DirBreadcrumb.vue:44` `<CopyButton :text="fullPath" :size="13" title-key="jump.copyPath" class="crumb-copy-btn" @click.stop />`（样式 :288-305），无 `copyFullPath` 函数，状态 class 是 `is-copied` 而非 `copied`（见 `web/src/components/file/__tests__/DirBreadcrumb.test.ts:519`）。
  - **【现状陈述过期·已落地】** 文档整体把该特性描述为待建，实际已实现：`web/src/components/file/JumpDirDialog.vue`（+ `__tests__/JumpDirDialog.test.ts`）；`FileManagerContent.vue` 跳转按钮 :97、overflow 项 :169-172、`<JumpDirDialog>` :534；`web/src/components/project/ProjectDialog.vue` 跳转按钮 :18、`<JumpDirDialog :open="jumpOpen" :placeholder="t('jump.placeholderBrowse')" ...>` :54、import :75、`browseNavigate` :93/:184。
  - **【接口漂移】** `JumpDirDialog` 实际多了文档未描述的可选 `placeholder` prop（`ProjectDialog` 正是靠它区分文案）。

**③ 建议怎么改**

1. 把 i18n 现状更正为实际值：title「跳转到路径」、`copyPath`「拷贝路径」，并补记 `placeholderBrowse`。
  2. 把按钮样式从 `.jump-confirm-btn`/`.jump-cancel-btn` 改为实际复用的 `.fbtn`/`.fbtn-primary`，并同步删掉文档里按旧类名写的测试断言片段。
  3. 把「文件管理器确认后调用 `store.navigateToDir`」改为 `navToFileInManager(path)`（`useFilePathAnnotation.ts`），并把面包屑复制按钮的描述改为复用 `web/src/components/common/CopyButton.vue`（状态 class `is-copied`）；同时把 `JumpDirDialog` 的 `placeholder` 可选 prop 补进接口说明。

---

#### `docs/superpowers/plans/2026-08-14-acp-session-sync.md`

- **行数**：1087 ｜ **类别**：实施计划（历史）
- **标题**：ACP 会话增量同步 Implementation Plan

**① 这份文档讲什么**

为 ACP 会话提供增量同步能力——`chat_history` 增 `external_message_id` 列与回放持久化、`ACPConn.SyncLoadSession`、`POST /api/ai/session/acp-sync` 端点，以及前端 `acpSyncSession` 与 ChatInputBar 上的同步按钮。

**② 有没有引用了已经不存在的文件或函数**

- **【Go 符号指向错误包】文档写 `service.ReadDB()` / `service.WriteExec()`，这两个符号在 `internal/service` 中不存在。** 真实实现在 `internal/store`：`store.ReadDB()`（`internal/store/db.go:234`）、`store.WriteExec()`（`internal/store/db.go:160`）。文档给出的调用点在 `internal/service` 里必须改为 `store.` 前缀。
  - **【Go 符号名漂移】文档写 `ACPConn.GetAcpSessionID()`，实际实现为 `AcpSID()`（`internal/ai/acp_pool.go:1482`；另有别名 `AcpSessionID()` 在 :1489），没有 `GetAcpSessionID`。
  - **【Go 符号不存在】文档提到的 `MigrateDatabaseForTest` 在仓库中不存在**（迁移写死路径见 `internal/service/database.go:148-151`）。
  - **【表结构描述与代码不符·更严重】** 文档给出的 `chat_history` 建表语句用 `project_path TEXT NOT NULL`，实际 schema 是**整数 `project_id`**（`internal/service/database.go:448` 建表、`:466` 列定义；`internal/store/projects.go` 的项目注册表以整数 id 为身份）。
  - **【现状陈述与代码矛盾·行为已改】** 文档描述的是「增量合并 / 仅插入缺失消息 / 已存在消息不变」，实际实现是**覆盖式同步**：`internal/service/chat.go:3509` 的 `ReplaceSessionHistory` 重写历史（`internal/handler/session_sync.go:332-340` 有空回放保护），且前端已改为**需用户确认**才执行（i18n `chat.acpSession.syncConfirm`：「同步外部变更，将重写当前会话的历史…」）。
  - **【现状陈述过期·i18n 键】文档列的 `chat.acpSession.syncedNone` 不存在**；实际键集见 `web/src/i18n/locales/zh.ts:558+`（`synced` = 「已同步 {count} 条新消息」、`syncConfirm`、`syncConfirmTitle`），另有 `chat.acpSync*`（zh.ts:602-605）。
  - **【现状陈述过期·已落地】** 文档整体把该特性描述为待建，实际已实现：`internal/handler/session_sync.go`（`groupLoadSessionReplay`:35、`persistReplayMessages`:178、`ServeACPSyncSession`:214、`waitForReplaySettled`:373）+ `session_sync_test.go` / `session_sync_edge_test.go` / `session_sync_integration_test.go`；`internal/ai/acp_pool.go` 的 `SyncLoadSession`:1647、`GetClient`:1603、`SetLoadSessionActiveForTest`:1681；路由 `internal/handler/handler.go:508`；OpenAPI `internal/api/openapi.yaml:1962`；前端 `web/src/composables/useAcpSession.ts:163`（`acpSyncSession`，返回 :210）、`web/src/components/chat/ChatInputBar.vue:34`（`acp-sync-btn`，disabled 计算 :391）、`web/src/components/chat/ChatPanelContent.vue:155`（`@sync-acp-session`）与处理函数 :465-494。
  - **【行号漂移】** 文档引用的 `database.go:213-224`、`handler.go:246`、`session_resume.go:298-409` 均已失效（实际为 `database.go:448`/`:466`、`handler.go:508`、`session_resume.go:585-587`）。
  - 文档提到的 `service.UpdateExternalSessionID`（`internal/service/chat.go:3361`）与 `service.UpsertToolCall`（`internal/service/tool_calls.go:34`）**确实存在**，无需改动。

**③ 建议怎么改**

1. 把 `service.ReadDB()`/`service.WriteExec` 全部改为 `store.ReadDB()`/`store.WriteExec`（`internal/store/db.go`），并把 `GetAcpSessionID` 改为 `AcpSID`（或 `AcpSessionID`），删掉不存在的 `MigrateDatabaseForTest`。
  2. 把「增量合并」的核心设计改为**覆盖式同步**的现状描述（`service.ReplaceSessionHistory` + 空回放保护 + 前端 `syncConfirm` 二次确认），否则后续读者会以为去重合并逻辑仍然存在而写出错误的幂等假设。
  3. 把 `chat_history` 的 `project_path TEXT` 更正为整数 `project_id`，并更新所有行号；同时把 `chat.acpSession.syncedNone` 换成实际的 `synced`（带 `{count}` 插值）。

---

#### `docs/superpowers/plans/2026-08-14-session-sidebar-pin.md`

- **行数**：1833 ｜ **类别**：实施计划（历史）
- **标题**：会话列表图钉固定侧栏 Implementation Plan

**① 这份文档讲什么**

在 PC 宽屏下把会话列表从抽屉抽成右侧常驻可拖拽侧栏（"图钉"固定），新增 `useSessionSidebar` 单例负责开关/宽度/持久化，并新增 `SessionList.vue` / `SessionListHeader.vue` / `SessionSidebar.vue`。

**② 有没有引用了已经不存在的文件或函数**

- **【严重·现状陈述已变】** 文档 Architecture 写"侧栏打开时隐藏 `ChatInputBar` 的会话按钮并将 `openSessionTab` 桥接为收起侧栏" → 实际两者都未实现：`web/src/components/chat/ChatInputBar.vue` 的 `defineProps`（:562 起）没有 `sessionPanelOpen` prop，`data-action="session"` 按钮无条件渲染；`web/src/components/chat/ChatPanelContent.vue` 也没有 `session-sidebar-open` 透传。
  - **【严重·行为已改】** 文档规定 `unpinToDrawer()` = "closeSidebar + 调 openDrawer"、`openSessionTabBridge()` = "侧栏开着就收起，否则开抽屉" → 实际 `web/src/composables/useSessionSidebar.ts` 里 `unpinToDrawer()` 只 `closeSidebar()`（注释明写"does NOT re-open the drawer"），`openSessionTabBridge()` 只 `openDrawerFn?.()`（"keep the sidebar as-is"），两者语义与文档相反。
  - **【严重·持久化模型已迁移】** 文档 Task 1 定义 `SESSION_SIDEBAR_KEY = 'clawbench-session-sidebar'` 走裸 localStorage → 实际改用 `web/src/utils/panelState.ts` 注册表（`readPanelState/writePanelState`，条目 `id: 'sessionSidebar'`、key `session-sidebar`、前缀 `clawbench-panel.`），`useSessionSidebar.ts` 已不再持有该常量。
  - **【现状陈述】** 文档完全未提及会话的置顶/排序（后端 `pinned` / `sort_order`），而该能力真实存在于 `internal/service/chat.go:1347,1370,1391`（`ORDER BY pinned DESC, sort_order ASC, created_at DESC, id DESC`）与 `internal/service/database.go:1368,1384`（两列的迁移）；`internal/service/group_store.go` 里既无 `pinned` 也无 `sort_order`——按文档去 `group_store.go` 找置顶逻辑会落空。
  - 文档引用的文件路径全部存在：`useSessionSidebar.ts`、`SessionList.vue`、`SessionListHeader.vue`、`SessionSidebar.vue`、`SessionDrawer.vue`、`ChatInputBar.vue`、`ChatPanelContent.vue`、`__tests__/useSessionSidebar.test.ts` 均在。
  - 文档 48 个 checkbox 全为 `- [ ]`，但 Task 1/2/3/4/5/8 已实际落地（`SessionSidebar.vue` 已被 `web/src/App.vue:360-368` 挂载、`@unpin` 已接线 `:1617` 语义）→ 属"说没做其实做了"。

**③ 建议怎么改**

1. 在文件顶部加一行状态标注（如"已实施，部分行为后续调整"），并把 Task 6/7 的"隐藏会话按钮 + 桥接收起侧栏"明确标为**未实施**，避免读者按文档去 `ChatInputBar` 找 `sessionPanelOpen`。
  2. 把 `unpinToDrawer` / `openSessionTabBridge` 的语义改成实际行为（不重开抽屉 / 恒开抽屉），并把持久化章节从裸 localStorage 改写为 `panelState.ts` 注册表条目。
  3. 补一节"相关但不属于本计划的能力"，指向 `internal/service/chat.go` 的 `pinned`/`sort_order` 排序与 `UpdateSessionPinned`（:2280），并说明 `internal/service/group_store.go` 不承载该逻辑。

---

#### `docs/superpowers/plans/2026-08-14-shortcut-tips-context-aware.md`

- **行数**：802 ｜ **类别**：实施计划（历史）
- **标题**：上下文感知快捷键轮播 + 全部快捷键对话框 实现计划

**① 这份文档讲什么**

把 `web/src/config/shortcutTips.ts` 的快捷键提示数据加上 `context` 分组维度，新增两个选择器、给轮播组件加 `context` prop，并新建按分组渲染的 `ShortcutTipsDialog.vue`，由 `AppHeader` 解析当前上下文并接线。

**② 有没有引用了已经不存在的文件或函数**

- **【现状陈述已变】** 文档 Task 4 让 `AppHeader.vue` 内联 `computed<ShortcutContext>(() => isWideScreen ? (activePane==='right' ? 'chat' : leftTab) : activeTab)` → 实际该逻辑已被抽成 `web/src/config/shortcutTips.ts` 的 `resolveShortcutContext(opts)`（:112），`AppHeader.vue:390-396` 改为调用它；文档描述的实现形态已不是现状。
  - **【现状陈述已变】** 文档给出的 `ShortcutContext` 联合类型与 `SHORTCUT_CONTEXT_ORDER` 顺序是 `common, chat, browse, view, terminal, history, settings, proxy, tasks` → 实际 `shortcutTips.ts:35-38` 的顺序为 `common, chat, browse, view, terminal, history, settings, proxy, tasks`，`tasks` 被移到了**末尾**而非文档所写的位置（文档未标注 tasks 的位置，此项影响表格渲染顺序，属轻度不一致）。
  - 文档引用的路径全部存在：`web/src/config/shortcutTips.ts`、`web/src/config/__tests__/shortcutTips.test.ts`、`web/src/components/common/{ShortcutTipTicker,ShortcutTipsDialog}.vue` 及其两个测试、`web/src/composables/useWideScreenLayout.ts` 均在；i18n 的 `appHeader.shortcutTipGroup.*`（zh.ts:464 / en.ts:460）与 `appHeader.shortcutTipsDialog.*`（zh.ts:475 / en.ts:471）也已落地。
  - **【定位补充】** 文档把快捷键提示的数据放在 `web/src/config/`（正确，与实际一致）；实际"快捷键"相关代码分两处：`web/src/config/shortcutTips.ts` 是**提示文案数据**，`web/src/composables/use{KeyConfig,ListKeys,MenuKeyboard,TerminalKeys}.ts` 是**按键处理逻辑**——按 CLAUDE.md 的 composables 域分组约定，提示数据留在 `config/`、按键行为留在 `composables/`，两者不混。
  - 文档 23 个 checkbox 全为 `- [ ]`，但五个 Task（数据模型 + i18n + 选择器、轮播 `context` prop、对话框组件、AppHeader 接线）实际全部已实现 → 属"说没做其实做了"。

**③ 建议怎么改**

1. 在文档顶部标注"已实施"，并把 Task 4 的内联 `computed` 代码块替换为调用 `resolveShortcutContext`（`web/src/config/shortcutTips.ts`），同时说明该 helper 是实施时新增的、文档原稿中没有。
  2. 补一句"数据在 `web/src/config/shortcutTips.ts`，按键处理在 `web/src/composables/use*Keys.ts`"，避免后来者在 `composables/` 里找提示文案而找不到。

---

#### `docs/superpowers/plans/2026-08-17-message-display-mode.md`

- **行数**：572 ｜ **类别**：实施计划（历史）
- **标题**：消息展示模式配置（摘要/原文）Implementation Plan

**① 这份文档讲什么**

新增本地配置 `messageDisplayMode`（摘要 / 原文），让 `shouldShowSummary` 接受全局默认模式，并在原文模式下对被剥离内容的消息走 `ensure-content` 事件链懒加载全文。

**② 有没有引用了已经不存在的文件或函数**

- **【严重·默认值已变】** 文档 Architecture 与 Task 1 都写"默认 `'summary'`" → 实际 `web/src/composables/useSettingsConfig.ts:400` 的 `localDefaults.messageDisplayMode` 是 `'mixed'`；`web/src/utils/chatSessionUtils.ts:108` 的 `MessageDisplayMode` 也已扩为 `'summary' | 'original' | 'mixed'`，并新增 `normalizeDisplayMode()`（:115）做非法值兜底。按文档实现会把默认行为定在"摘要"，与现状相反。
  - **【严重·决策点已变】** 文档称 `shouldShowSummary`（`web/src/utils/chatSessionUtils.ts:105`）"是唯一渲染决策点" → 实际它已不是：`chatSessionUtils.ts:225` 新增 `isShowingSummary(msg, defaultMode, opts)`，其注释明确要求"Components must use this (not `shouldShowSummary` directly)"，并额外处理 `_loadingOriginal` / `_loadAttempted` 占位态；`shouldShowSummary` 现在在 :189，签名也多了第三参 `opts?: SummaryDisplayOptions`（`isLastAssistant`）。
  - **【现状陈述·行号漂移】** 文档给的一批行号已全部过时：`useSettingsConfig.ts:267`（`localConfig`）→ 实际 :478；`:245`（`localDefaults`）→ 实际 :389；`ChatPanelContent.vue:990`（`ensureMessageContent`）→ 实际 :2154；`:951`（`handleToggleSummary`）→ 实际 :2112；`ChatMessageItem.vue:38` → 实际 :99；`ChatMessageList.vue:73` → 实际 :133；`ChatPanelContent.vue:28` → 实际 :46。
  - **【现状陈述·选项集已变】** 文档 Task 3 断言设置项 options 为 `['summary', 'original']` → 实际 `web/src/components/settings/settingsFieldMap.ts:250-253` 是三项 `mixed / summary / original`；i18n 也多出 `messageDisplayModeMixed`（zh.ts:2778 / en.ts 对应键）。
  - 文档引用的路径全部存在：`web/src/utils/chatSessionUtils.ts`、`web/src/utils/__tests__/chatSessionUtils.test.ts`（测试确实在 `__tests__/` 子目录下）、`web/src/composables/useSettingsConfig.ts`、`web/src/composables/__tests__/useSettingsConfig.test.ts`、`web/src/components/chat/{ChatPanelContent,ChatMessageItem,ChatMessageList}.vue` 均在；`ensure-content` 事件链已在 `ChatMessageItem.vue:370` → `ChatMessageList.vue:134` → `ChatPanelContent.vue:47` 完整贯通。
  - 文档 30 个 checkbox 全为 `- [ ]`，但六个 Task 实际全部已实现（`ensure-content` 事件、`ensureMessageContent`、设置项、默认值、i18n 均在）→ 属"说没做其实做了"。

**③ 建议怎么改**

1. 顶部加"已实施，且实施后新增了 `mixed` 混合模式"的状态说明，把默认档从 `'summary'` 更正为 `'mixed'`，并补 `mixed` 的语义（最近一条 AI 回复显示原文、更早的显示摘要）与 `normalizeDisplayMode` 的兜底行为。
  2. 把"唯一渲染决策点是 `shouldShowSummary`"改为"渲染层应调用 `isShowingSummary`"，并补 `opts.isLastAssistant` 的用途，否则照文档实现会漏掉懒加载占位态。
  3. 全量刷新文中行号引用（`chatSessionUtils.ts` / `useSettingsConfig.ts` / `ChatPanelContent.vue` / `ChatMessageItem.vue` / `ChatMessageList.vue`），或干脆改为按函数名引用、不写行号。

---

#### `docs/superpowers/plans/2026-08-18-conflict-auto-number.md`

- **行数**：230 ｜ **类别**：实施计划（历史）
- **标题**：同名文件冲突自动加编号（不弹框）Implementation Plan

**① 这份文档讲什么**

文件管理器内复制/粘贴、拖拽移动撞同名时不再弹命名框，改为复用后端上传来电的 `name_1.ext` 顺序编号自动重试。

**② 有没有引用了已经不存在的文件或函数**

- **【现状陈述·行号漂移】** 文档 Task 2 指 `web/src/components/file/FileManagerContent.vue:997-1011` 的 `transferEntries` 409 分支 → 实际该逻辑在 :1982 与 :1999（`numberedName(srcEntry.name, attempt)`）；文档 Task 1 指 `formatSize` 在 `~L79` → 实际 `web/src/utils/fileManager.ts` 中 `formatSize` 在 :76、`numberedName` 在 :89。
  - **【现状陈述·行号漂移】** 文档 Task 3 给出 `zh.ts:856` / `en.ts:855` 的 `pasteNewName` 删除位置，以及 `FileManagerContent.test.ts:231` / `fileManagerContent.test.ts:131` 的 stub 删除位置 → 现有文件均已无 `pasteNewName`（全仓库 grep 零命中），但两处测试文件的行号已漂移。
  - 文档引用的路径全部存在且已落地：`web/src/utils/fileManager.ts`、`web/src/utils/__tests__/fileManager.test.ts`（已含 `numberedName` 6 个用例，含 `.gitignore` / 无扩展名边界）、`web/src/components/file/__tests__/FileManagerContent.test.ts`、`web/src/components/__tests__/fileManagerContent.test.ts` 均在。
  - 文档 Architecture 称"上传路径（`/api/upload/file`）后端已自动编号，不改" → **这条现状陈述仍然成立**：`internal/handler/upload.go:110-124` 确有 `for i := 1; i <= 9999; i++` 的 `_1`、`_2` 递增编号。
  - 文档 17 个 checkbox 全为 `- [ ]`，但四个 Task 实际全部已实现（`numberedName` 纯函数 + 409 重试循环 + `pasteNewName` 键已删 + 前后端行为一致）→ 属"说没做其实做了"。

**③ 建议怎么改**

1. 顶部标注"已实施"，并把 `FileManagerContent.vue` 的引用从行号改为函数名 + 大致区域（`transferEntries` 内的 409 重试循环），行号已漂移约 1000 行。
  2. 保留"上传路径后端已自动编号"这条说明（它仍准确），可补一句指向 `internal/handler/upload.go` 的 9999 次上限，说明前后端编号上限一致。

---

#### `docs/superpowers/plans/2026-08-22-ws-reconnect-state-sync.md`

- **行数**：282 ｜ **类别**：实施计划（历史）
- **标题**：WS 重连作为唯一状态同步触发点 — Implementation Plan

**① 这份文档讲什么**

把 WS 重连做成唯一的数据状态同步触发点——删除 `handleVisibilityChange` 的前台历史重载、删除 `App.vue` 的 `handleForeground` 全量拉取，把这些合并进 `handleReconnect`（WS 重连）与 `handleWsReconnect`（聊天历史重载）。

**② 有没有引用了已经不存在的文件或函数**

- **【严重·实现形态已变】** 文档 Task 1 Step 3 给出的 `handleWsReconnect` 完整函数体（内联 `loadSessionsOnceInner` + `forceCleanupStreamingState` + `loadHistory` 分支）→ 实际 `web/src/composables/useChatSession.ts:1602` 是 `const handleWsReconnect = () => runReload()`，真正的逻辑已被重构成独立的 `runReload()`（:1470 起），`forceCleanupStreamingState` 调用点有两处（:1371 与 :1577）。照文档重写会与现有 `runReload` 重复。
  - **【严重·API 已变】** 文档给出的 `forceCleanupStreamingState(messages, { onRenderNeeded, onExtractScheduledTasks })` 两参调用 → 实际 `web/src/utils/chatStreamUtils.ts:391` 的签名已是四参（多出 `targetMessageId?: number` 与 `options?: { holdsCompleteTurn?: boolean }`），调用点需传 `undefined, { holdsCompleteTurn: false }`。
  - **【现状陈述·行号漂移】** 文档给的 `useChatSession.ts:1008-1027`（handleWsReconnect）、`:980-1000`、`:1170`（handleVisibilityChange）、`ChatPanelContent.vue:1094`/`:1111`、`App.vue:837-865`/`:911`/`:2222` 均已漂移；`App.vue` 现在的 `handleReconnect` 在 :1946、`clawbench-reconnect` 监听在 :1987。
  - **【现状陈述·已落地】** 文档 Step 3/Task 3 要求删除的两处 `visibilitychange` 注册确已删除：`web/src/components/chat/ChatPanelContent.vue` 与 `web/src/composables/useChatSession.ts` 中 `visibilitychange` 零命中；`App.vue` 中 `handleForeground` 与 `clawbench-foreground` 监听也已零命中，`clawbench-foreground` 仅保留在 `web/src/composables/useGlobalEvents.ts:830` 与 `useConnectionOverlay.ts:78`——与文档 Task 5 Step 3 的预期完全一致。
  - 文档引用的路径全部存在：`web/src/composables/useChatSession.ts`、`web/src/composables/__tests__/useChatSession.test.ts`（内含 `when loading=false: reloads history (skipIfUnchanged)` 用例，实际在 :4947 而非文档所写 :3664-3690）、`web/src/components/chat/ChatPanelContent.vue`、`web/src/App.vue` 均在。
  - 文档 21 个 checkbox 全为 `- [ ]`，但五个 Task 实际全部已实现 → 属"说没做其实做了"。

**③ 建议怎么改**

1. 顶部标注"已实施"，把 Task 1 Step 3 的代码块换成当前的 `handleWsReconnect = () => runReload()` 形态，并说明 `runReload` 是实施时为复用而抽出的独立函数。
  2. 更新 `forceCleanupStreamingState` 的调用示例为四参形式（含 `{ holdsCompleteTurn: false }`），否则照抄会编译不过或丢失"整轮已完成"语义。
  3. 把所有行号引用改为函数名引用，避免再次漂移。

---

#### `docs/superpowers/specs/2026-08-02-grok-build-backend-design.md`

- **行数**：142 ｜ **类别**：实施计划（历史）
- **标题**：Grok Build 智能体适配设计

**① 这份文档讲什么**

Grok Build 后端接入设计——原方案是**仅 ACP 模式**（走 `grok agent stdio`），不注册 CLI factory；模型发现单独用 `grok models`；并借此重构 ACP/CLI 正交能力模型（`Agent.SupportsCLI`）。

**② 有没有引用了已经不存在的文件或函数**

- **【最严重·核心决策已被推翻】** 文档「实现方案（仅 ACP 模式）」明写"**不实现 CLI 模式**。Grok Build 不注册 `ai.RegisterBackend` CLI factory"，新文件表里 `register.go` 一栏也标注"**无** `ai.RegisterBackend`（无 CLI factory）"，「已知限制」更断言"仅 ACP 模式，无 CLI 回退……仍会报 unsupported backend type: grok" → 实际 `internal/ai/backends/grok/cli.go:12` 有 `ai.RegisterBackend("grok", newGrokBackend)`（streaming-json 回退），`internal/ai/backends/grok/register.go` 的注释也明写"a streaming-json CLI fallback is registered in cli.go via ai.RegisterBackend"，且 `internal/model/discovery_test.go:107` 断言 `model.BackendSupportsCLI("grok")` **为 true**（注释："grok has a CLI fallback factory"）。同时 `internal/ai/factory.go:71` 的报错文案已改为"no CLI implementation registered; is it an ACP-only backend?"。
  - **【严重·现状陈述】** 文档「ACP / CLI 正交能力模型」表格写 `SupportsCLI` 示例为"claude / pi；**grok=false**" → 实际 grok 为 true；真正 ACP-only 的例子是 `antigravity`（`internal/model/discovery_test.go:111` 断言其 false，且 `internal/ai/backends/antigravity/` 下只有 `discovery.go` / `register.go`，无 cli.go）。
  - **【严重·现状陈述】** 文档写"Grok 使用标准 ACP 工具（title/kind），共享 `acp_tool_names.go` 前缀匹配已覆盖……**无需 per-agent 解析函数或 ToolCallIDPrefixes**" → 实际 `internal/ai/backends/grok/acp.go:6` 定义了 `GrokACPTCIDPrefixes`（ACP 工具 ID → ClawBench 规范名映射），且 `register.go` 的 ACP 插件里显式挂了 `ToolCallIDPrefixes: GrokACPTCIDPrefixes`。
  - **【路径/符号不存在】** 文档新文件表写 `internal/ai/backends/grok/discovery.go` 提供 `DiscoverGrokModels()`，模型发现章节给出 `var grokDefaultModels = []model.AgentModel{...}` → 两者都不存在。实际 `discovery.go` 是 `model.RegisterModelSource(model.NewCLISource("grok", ...))` + 私有 `parseGrokModels()`，默认回退表在 `internal/model/catalogs.go:76` 的 `model.GrokCatalog`（同文件 :81 另有 `model.GrokModelNames`）。
  - **【文件清单不完整】** 文档「新文件」只列 `register.go` / `acp.go` / `discovery.go` / `acp_test.go` / `discovery_test.go` → 实际还存在 `internal/ai/backends/grok/cli.go` 与 `cli_test.go`。
  - **【现状陈述·图标来源】** 文档「修改文件」表写 `web/src/utils/agentIcons.ts` 添加 grok 图标"`grok.svg`" → 仓库内没有本地 `grok.svg` 文件，实际是 `web/src/utils/agentIcons.ts:27` 从 `@lobehub/icons-static-svg/icons/grok.svg?raw` 导入（`needsBg: true` 已按文档设置，`web/src/utils/__tests__/agentIcons.test.ts` 也已含 `grok`）。
  - **【核实通过·文档正确处】** 用户点名要核的 `model.BackendSupportsCLIFn` **确实定义在 `internal/model/discovery.go:61`**（注入变量 `var BackendSupportsCLIFn func(backendID string) bool`，:65 的 `BackendSupportsCLI` 兜底 false），文档 :109 的描述准确；注入链 `internal/ai/backends/acp_wire.go:32` 的 `model.BackendSupportsCLIFn = ai.BackendSupportsCLI`、`cmd/server/main.go:37` 的 `_ "clawbench/internal/ai/backends/grok"` blank import、`internal/model/agent.go:96-99` 的 `SupportsCLI bool \`json:"supportsCLI"\``（运行时字段、不持久化）、`internal/ai/acp_tool.go:156` 的 `parseGenericACPToolCall(tc, backendID)` 透传 backendID、`web/src/composables/useAgents.ts:619/629/653` 的 `supportsACP()/supportsCLI()/supportsDualTransport()`（后者确为 `supportsACP && supportsCLI`）——全部与文档相符。

**③ 建议怎么改**

1. 把「实现方案（仅 ACP 模式）」整节改写为"ACP 优先 + streaming-json CLI 回退"，并把「已知限制」首条删掉或改写——当前该限制描述的是已被推翻的实现；`grokDefaultModels` 代码块换成 `model.GrokCatalog` 的真实位置 `internal/model/catalogs.go:76`。
  2. 更新「ACP / CLI 正交能力模型」表格：`grok` 从 `false` 改为 true，ACP-only 的示例换成 `antigravity`；并把「无需 ToolCallIDPrefixes」改为"已新增 `GrokACPTCIDPrefixes`"。
  3. 在「新文件」表补 `internal/ai/backends/grok/{cli.go,cli_test.go}`，并把 `DiscoverGrokModels()` 改为 `parseGrokModels()` + `model.RegisterModelSource` 的实际机制；图标来源改为 `@lobehub/icons-static-svg`。

---

#### `docs/superpowers/specs/2026-08-02-thinking-lazy-load-design.md`

- **行数**：118 ｜ **类别**：实施计划（历史）
- **标题**：深度思考（thinking）独立存储与懒加载设计

**① 这份文档讲什么**

把深度思考文本从 `chat_history.content` 的 blocks JSON 里拆出来，单独存 `chat_thinking` 表，content 只留带 `think_id` 的 slim 块，前端折叠态不传全文、展开时懒加载。

**② 有没有引用了已经不存在的文件或函数**

- **【严重·现状陈述已变】** 文档「背景」写"助手消息的深度思考（thinking）文本目前**内嵌在** `chat_history.content` 的 blocks JSON 里"、"打开会话时 `/api/ai/chat` 把整条 content 原样返回" → 实际早已落地：`internal/service/thinking.go` 存在（含 `slimThinkingInContent`、`persistThinkingToDB`、`ReplaceThinkingForMessage`、`UpsertThinking`、`AppendThinkingSegment`、`GetThinking`/`GetThinkingBySession`/`GetThinkingBySessionAll`、`DeleteThinkingByMessage` 等 13 个函数，远超文档设计的 4 个）；`internal/service/database.go:679` 已建 `chat_thinking` 表；`internal/handler/handler.go:526` 已注册 `GET /api/ai/chat/thinking`（`internal/handler/chat_thinking.go:16` `ServeThinkingDetail`）；前端 `web/src/composables/useThinkingContent.ts` 已实现 `thinkingTextCache` / `clearThinkingCache` / `loadThinking` / `isProvisional`。所以"懒加载到底做没做"的答案是：**已完整实现**。
  - **【路径不存在·现状陈述】** 文档「已知取舍（非本次范围）」写"`rag message <id>` CLI 命令（`internal/cli/rag.go`）slim 后显示不到 thinking，可后续在 `GetMessageByID` 回填" → `internal/cli/rag.go` **不存在**，该目录当前只有 `upgrade.go` / `upgrade_test.go` / `upgrade_unix.go` / `upgrade_windows.go`；CLAUDE.md 亦记载"task/rag 业务子命令已移除，改由 `/cb-*` 内置斜杠命令直调 HTTP API"。这是把一条**已被删除的命令**当作现存命令来写取舍，属当前状态陈述错误。（`service.GetMessageByID` 确实存在于 `internal/service/chat.go:314`，那半句本身仍成立。）
  - **【现状陈述·Schema 已变】** 文档给出的建表 SQL 是 `UNIQUE(think_id, message_id)`，无 `seq` 列 → 实际 `internal/service/database.go:679-688` 为 `seq INTEGER NOT NULL DEFAULT 0` + `UNIQUE(think_id, message_id, seq)`（为增量流式落库：每 500ms 追加 delta 段 seq 0,1,2…，Finalize 时删除旧行重写单条 seq=0）。文档「写入路径」描述的"DELETE → INSERT 单条"也已变成流式追加 + Finalize 收敛。
  - **【现状陈述·行号漂移】** `model/chat.go:132`（`ContentBlock`）→ 实际 `internal/model/chat.go:366`；`handler/chat.go:254`（GET 返回 content）→ 实际 `internal/handler/chat.go:30` 是 `AIChat` 入口、:254 已是 POST 请求体解码；`session_executor.go:496`（`Finalize`）→ 实际 `internal/service/session_executor.go:1681`；`chat.go:1446`（`PurgeArchivedData`）→ 实际在 `internal/store/session_queries.go:594`；`chat.go:1508`（`HardDeleteSession`）→ 实际 `internal/service/chat.go:3448`；`ContentBlocks.vue` 的 `stableBlockKey`（`:370`）→ 实际 :1260、`getThinkingHtml`（`:550`）→ 实际 :1767；`MigrateToolCallsFromContent` 对应物 → 实际 `internal/service/database.go:2900`，thinking 迁移为 `internal/service/thinking_migrate.go` 的 `MigrateThinkingFromContent`（:1966 调用）。
  - **【现状陈述·行号漂移】** 文档「后端 3. 复制路径」提到 `copySessionDetailTables`（未给行号）→ 实际 `internal/service/continue_conversation.go:534`，与文档一致；但文档称其"复制 `chat_tool_calls` 和 `chat_thinking`"——已实现，无问题。
  - 文档引用的其余路径均存在：`internal/service/thinking.go`、`internal/service/thinking_migrate.go`、`internal/service/database.go`、`internal/service/session_executor.go`、`internal/service/continue_conversation.go`、`web/src/components/chat/ContentBlocks.vue`、`web/src/utils/chatBlocks.ts`、`internal/handler/chat.go` 均在。

**③ 建议怎么改**

1. 把「背景」改写成历史陈述（"设计时 thinking 内嵌在 content 里"），并在文首加"已实施"标注，指向 `internal/service/thinking.go`、`internal/service/database.go:679` 的 `chat_thinking` 表和 `GET /api/ai/chat/thinking` 端点。
  2. 删除或改写「已知取舍」里 `internal/cli/rag.go` 那条——该 CLI 命令已不存在；改为"`GetMessageByID`（`internal/service/chat.go:314`）供 `/api/rag/message` 复用"之类仍成立的表述。
  3. 更新建表 SQL 为含 `seq` 的真实版本，并修正 `model/chat.go:132`、`session_executor.go:496`、`chat.go:1446/1508`、`ContentBlocks.vue:370/550` 等行号（`PurgeArchivedData` 已挪到 `internal/store/session_queries.go`）。

---

#### `docs/superpowers/specs/2026-08-03-bigscreen-splitview-design.md`

- **行数**：209 ｜ **类别**：实施计划（历史）
- **标题**：大屏双栏布局（Big-Screen SplitView）设计

**① 这份文档讲什么**

大屏分栏设计——左栏 441px 固定 + 分割线 + panelState.ts 持久化。

主要章节：1. 目标与范围、2. 需求要点（已与用户确认）、#已确认的细化决策、3. 架构、#3.1 新增文件、#3.2 修改文件、#3.3 布局结构、4. 状态与路由设计

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时。引用的 `web/src/utils/panelState.ts` 存在。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-03-connection-overlay-design.md`

- **行数**：92 ｜ **类别**：实施计划（历史）
- **标题**：统一全屏状态遮罩（ConnectionOverlay）设计

**① 这份文档讲什么**

连接状态悬浮层设计——AI 后端连接状态、token 用量实时推送。

主要章节：背景与问题、现状代码、设计、#1. 新建组件 `web/src/components/common/ConnectionOverlay.vue`、#2. 状态共享、#3. 挂载与清理、#4. 文案（i18n）、#5. 测试

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-05-file-edit-mode-design.md`

- **行数**：102 ｜ **类别**：实施计划（历史）
- **标题**：文件源码编辑模式 — 设计文档

**① 这份文档讲什么**

文件编辑模式设计——只读/编辑切换、守卫逻辑。

主要章节：背景与目标、已确认决策、架构、组件设计、#1. 新增 `CodeEditor.vue`（web/src/components/file/CodeEditor.vue）、#2. 新增 `codeEditorLang.ts`（web/src/utils/codeEditorLang.ts）、#3. 修改 `FileHeader.vue`、#4. 修改 `FileViewer.vue`

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-07-codemirror-sticky-scroll-design.md`

- **行数**：79 ｜ **类别**：实施计划（历史）
- **标题**：CodeMirror 6 粘性滚动（Sticky Scroll）设计

**① 这份文档讲什么**

CodeMirror 粘性滚动（StickyScroll）设计。

主要章节：背景、目标、方案、#几何模型（关键点）、#高亮渲染、#触发与生命周期、#UI、#开关与配置

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-07-terminal-select-mode-design.md`

- **行数**：79 ｜ **类别**：实施计划（历史）
- **标题**：终端三态交互：浏览 / 手势 / 选区

**① 这份文档讲什么**

终端选择模式设计——vim alike 快捷键选中文本。

主要章节：目标、范围、状态模型、手势按钮（TerminalPanelContent.vue）、选区 + 复制（TerminalPanelContent.vue）、移除内容、测试（Vitest）、边界情况

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-09-file-view-tab-design.md`

- **行数**：64 ｜ **类别**：实施计划（历史）
- **标题**：文件浏览独立 Tab 设计

**① 这份文档讲什么**

文件查看 Tab 设计——多文件 Tab 切换。

主要章节：背景、目标标签结构、关闭文件后的去向、改动清单、#App.vue、#useWideScreenLayout.ts、#FileManagerContent.vue / FileViewer.vue、#入口组件

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-11-voice-input-asr-design.md`

- **行数**：151 ｜ **类别**：实施计划（历史）
- **标题**：语音输入（ASR）设计文档

**① 这份文档讲什么**

语音输入 ASR 设计。

主要章节：1. 背景与目标、#核心交互（用户需求）、#已确认的决策、2. 架构、#后端包 `internal/stt/`（镜像 `internal/speech/`）、#新增路由（`internal/handler/handler.go`）、#服务端配置（镜像 TTS）、3. 前端

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-12-terminal-theme-switch-design.md`

- **行数**：106 ｜ **类别**：实施计划（历史）
- **标题**：终端独立主题切换（支持 xterm.js 全部 157 主题，懒加载）

**① 这份文档讲什么**

终端主题切换设计。

主要章节：目标、背景、范围、架构、#1. 新增工具模块 `web/src/utils/terminalThemes.ts`（纯函数，可单测）、#2. 持久化、#3. 主题应用与自动跟随、#4. 切换入口 UI（TerminalPanelContent.vue）

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-13-acp-session-project-filter-design.md`

- **行数**：59 ｜ **类别**：实施计划（历史）
- **标题**：ACP 外部会话按当前项目过滤设计

**① 这份文档讲什么**

ACP 会话按项目过滤设计。

主要章节：背景与问题、目标、范围、设计、#1. `web/src/composables/useAcpSession.ts`、#2. `web/src/components/chat/AcpSessionDrawer.vue`、#3. 测试、效果验证

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-13-dir-jump-copy-design.md`

- **行数**：102 ｜ **类别**：实施计划（历史）
- **标题**：目录快速跳转 + 面包屑复制路径 设计文档

**① 这份文档讲什么**

目录跳转与路径复制设计。

主要章节：1. 目标、2. 需求要点、3. 架构与组件、#3.1 共享跳转对话框 — `web/src/components/file/JumpDirDialog.vue`、#3.2 文件管理器 — `web/src/components/file/FileManagerContent.vue`、#3.3 项目选择器 — `web/src/components/ProjectDialog.vue`、#3.4 面包屑复制按钮 — `web/src/components/file/DirBreadcrumb.vue`、4. 数据流

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `CornerDownLeft` （第 34 行） — 类型：符号

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

---

#### `docs/superpowers/specs/2026-08-13-shortcut-tips-header-design.md`

- **行数**：86 ｜ **类别**：实施计划（历史）
- **标题**：Design: Header 中段快捷键提示滚动条

**① 这份文档讲什么**

快捷键提示 Header 设计。

主要章节：目标、范围、架构、#1. i18n 文案（双语）、#2. 数据模块 `web/src/config/shortcutTips.ts`、#3. 组件 `web/src/components/common/ShortcutTipTicker.vue`、#4. 接入 `AppHeader.vue`、#5. 开关设置（默认开启）

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `SHOW_MS` （第 40 行） — 类型：符号

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

---

#### `docs/superpowers/specs/2026-08-14-acp-session-sync-design.md`

- **行数**：81 ｜ **类别**：实施计划（历史）
- **标题**：设计文档：ACP 会话增量同步按钮

**① 这份文档讲什么**

ACP 会话同步详细设计。

主要章节：目标、需求要点、关键事实（代码核对）、后端设计（Go）、#1. Schema 迁移、#2. 回放逻辑抽公用、#3. 新端点 `POST /api/ai/session/acp-sync`、前端设计（Vue 3）

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-14-session-sidebar-pin-design.md`

- **行数**：161 ｜ **类别**：实施计划（历史）
- **标题**：Design: 会话列表图钉固定侧栏（Session Sidebar Pin）

**① 这份文档讲什么**

会话侧栏固定设计。

主要章节：目标、背景与现状、范围、架构、#1. 抽取可复用组件 `SessionList.vue`、#2. 抽取可复用头部 `SessionListHeader.vue`、#3. `SessionDrawer.vue` 改造、#4. 新组件 `SessionSidebar.vue`

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-14-shortcut-tips-context-aware-design.md`

- **行数**：136 ｜ **类别**：实施计划（历史）
- **标题**：Design: 上下文感知的快捷键轮播 + 全部快捷键对话框

**① 这份文档讲什么**

上下文感知快捷键提示设计。

主要章节：目标、背景与现状、范围、架构、#1. 数据模型 `web/src/config/shortcutTips.ts`、#2. 轮播组件 `ShortcutTipTicker.vue`、#3. 新组件 `web/src/components/common/ShortcutTipsDialog.vue`、#4. 接入 `AppHeader.vue`

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-17-message-display-mode-design.md`

- **行数**：101 ｜ **类别**：实施计划（历史）
- **标题**：Design: 聊天消息展示模式配置（摘要模式 / 原文模式）

**① 这份文档讲什么**

消息显示模式设计。

主要章节：目标、背景与现状、范围、架构、#1. 配置项 `web/src/composables/useSettingsConfig.ts`、#2. 设置面板项 `web/src/components/settings/settingsFieldMap.ts`、#3. i18n `web/src/i18n/locales/zh.ts` / `en.ts`、#4. 渲染决策 `web/src/utils/chatSessionUtils.ts` `shouldShowSummary`

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-18-conflict-auto-number-design.md`

- **行数**：47 ｜ **类别**：实施计划（历史）
- **标题**：同名文件冲突：自动加编号（不弹框）

**① 这份文档讲什么**

冲突自动编号设计。

主要章节：背景、决策、行为规格、实现要点、测试、验证

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

---

#### `docs/superpowers/specs/2026-08-22-ws-reconnect-state-sync-design.md`

- **行数**：54 ｜ **类别**：实施计划（历史）
- **标题**：状态同步触发源：前台 → WS 重连

**① 这份文档讲什么**

WebSocket 重连状态同步设计。

主要章节：背景与问题、目标、设计、#1. `App.vue`、#2. `useChatSession.ts`、#3. `ChatPanelContent.vue`、数据流、边界（不扩大范围）

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 属历史实施计划，引用尚未落地的文件属正常。**建议在文件顶部加一行状态横幅**（如「本文为 2026-09 的实施计划，部分文件最终未创建/已改名，以现状为准」），避免读者把计划当作现状。

### 其他 `docs/Skills/`、`docs/timer/`

---

#### `docs/Skills/clawbench-screenshot/SKILL.md`

- **行数**：212 ｜ **类别**：其他
- **标题**：ClawBench 产品截图（Browser Automation）

**① 这份文档讲什么**

用 `playwright-cli` 给 ClawBench 截宣传图/功能图的完整流程——三个浮层的抑制键、桌面宽屏三栏构图、1080p/2x 参数、Hero 图（`product_hero.html`）的编辑与再导出。

**② 有没有引用了已经不存在的文件或函数**

**无失效引用**。`/api/upgrade/check` 端点存在（`internal/handler/handler.go:740` → `ServeUpgradeCheck`）✓；`docs/screenshots/product_hero.html` / `.en.html` / `.png` / `.en.png` 与 `product_hero_assets/{bg_tech.jpg,logo.png,phone_screenshot.jpg,tablet_screenshot.jpg}` 全部存在 ✓；README.md:6 / README.en.md:6 的 `product_hero.png` / `product_hero.en.png` 引用一致 ✓；三个 localStorage 键（`clawbench_welcome_dismissed` 在 `WelcomeOverlay.vue:119`、`clawbench-upgrade-skip` 在 `useUpgrade.ts:72`、`clawbench-settings-theme`）全部存在 ✓；`.completion-notify`/`.completion-notify-close`（`CompletionPopover.vue`）、`.up-overlay`（`UpgradePromptOverlay.vue`）、`.welcome-overlay`（`WelcomeOverlay.vue`）、`.toc-dock`（`FileViewer.vue`）、`.markdown-body`、`.session-sidebar`、`.file-item`、`.chat-tab-panel` 全部存在 ✓。

**③ 建议怎么改**

**关键矛盾**：本 skill 第 3 步用 `` `!!document.querySelector('.up-overlay')?.offsetParent` `` 判浮层可见，而 `docs/Skills/clawbench-user-guide/SKILL.md` §二 明确指出**该判据对 fixed 浮层恒为 false、完全失效**。应把这里换成 user-guide 给的 `checkVisibility` 版 `vis(e)` 判据，否则会照着这个 skill 拍出带升级弹窗的图；`clawbench-upgrade-skip` 硬编码的 `'0.89.0'` 也应改成「先 fetch `/api/upgrade/check` 取 `latest_version` 再写」。

---

#### `docs/Skills/clawbench-user-guide/SKILL.md`

- **行数**：1096 ｜ **类别**：其他
- **标题**：ClawBench 桌面端使用说明（截图 + 文档）

**① 这份文档讲什么**

桌面端 720p（2560×1440 + zoom=2 + LANCZOS 降采样）与移动端 390×844@3 两套截图参数、浮层抑制、按模块取景清单、Vue/Playwright 交互陷阱速查、群聊与设置页的 DOM 选择器手册。

**② 有没有引用了已经不存在的文件或函数**

① **手册路径是 `docs/user-guide/`（不是 `user-guid`）**——本文件 6 处引用（§七、§十一、§二十六、§901-902）全部写成 `docs/user-guide/`，**正确**；24 个详解文档（`getting-started.md`…`FAQ.md`）与 `screenshots/`、`screenshots-mobile/` 目录**全部存在** ✓。（注：仓库根 `CLAUDE.md` 里写的 `docs/user-guid/user-guid.md` 才是错的，正确入口是 `docs/user-guide/README.md`。）② 第 41 行 `web/src/components/LoginView.vue:326-334`（`.login-page` 的 `min-height:100vh`+`overflow:hidden`+flex 居中）→ 实际 CSS 在 **LoginView.vue:348**。③ 第 84 行 `web/src/composables/useCompletionPopover.ts:29-30`（`queue`/`active` 模块级内存态）→ 实际在 **:66-67**。④ 第 80 行 `UpgradePromptOverlay.vue:83`（`.up-overlay` fixed）→ 实际 **:82**；`WelcomeOverlay.vue:237` → 实际 **:254**。⑤ 第 646-652 行 `useWideScreenLayout.ts:8` 的 `WIDE_SCREEN_MIN_WIDTH = 1024` 与 `:50` 的 `computeIsWideScreen(cssWidth, screenWidth, screenHeight, dpr)` **已完全过时**：当前是 **:27 `WIDE_SCREEN_MIN_WIDTH = 960`**（注释明说「960 rather than 1024」），`computeIsWideScreen` 移到 **:65** 且签名收敛为单参 `computeIsWideScreen(cssWidth: number): boolean`（物理宽度兜底分支已删）——这直接影响 §二十一「zoom 后 innerWidth 仍 1170 ⇒ 判宽屏」的论证。⑥ 第 668 行 `usePlatformDetect.ts:55` 的 `isPC = !isAppMode && !AndroidUA && !iOSUA && !iPadOSUA` —— **`isPC` 已被彻底移除**（`usePlatformDetect.ts` 现只导出 `isAndroidUA`/`isIOSUA`/`isIPadOSUA`/`isMobileOSUA` 等，仓库内 `isPC` 只剩注释残迹），与 CLAUDE.md「平台判定拆为三条正交轴」一致。⑦ 组件路径漂移：`TabPanel.vue`→`web/src/components/common/TabPanel.vue`、`SplitView.vue`→`web/src/components/common/SplitView.vue`、`settingsFieldMap.ts`→`web/src/components/settings/settingsFieldMap.ts`、`modal-card.css`→`web/src/assets/modal-card.css`（文中均只写裸文件名）。⑧ 第 700 行 `ChatInputBar.vue:1209-1253`（输入框历史滑动）→ 1209 实际是 `props.isGroupSession` 相关代码；第 1080 行 `ChatInputBar.vue:703-723`（语音长按）→ 695 行实际是 `lastAssistantMessageId`。⑨ 第 651 行 `App.vue:1209`（跳过 replay 的历史 completed 事件）→ 实际在 **App.vue:1815**（`if (skipReplay && isReplayingEvents.value) return`）。⑩ 第 380 行 `FileManagerContent.vue:900`（`enabled: computed(() => filePreviewMode.value)`）→ 900 行实际是 `demotableToolbarIds` 数组。⑪ §二十四的 `useAppMode.ts:13-27` / `SettingsCategory.vue:204` / `settingsFieldMap.ts:296,297,262` 三处行号均已漂移。⑫ §十七引用的 `GET /api/ai/session/tags`、`DELETE /api/ai/session/archive`、`/api/share`、`/api/me`、`/api/terminal/status` 端点**全部存在** ✓；`shareRequestPath` 在 `internal/handler/file_share.go:76` ✓；`file_shares` 表列名 ✓；所有 DOM 选择器（`.session-tags-dialog`/`.st-chip`/`.context-menu-item`/`.file-header-dropdown-menu`/`.share-dialog-*`/`.code-preview-header`/`.app-menu`/`.drilldown-item`/`.scroll-fab-group`/`.msg-bcc`/`.agent-stepper`/`.settings-agents-index__row` 等 60+ 个）**逐一核对全部存在** ✓；`test/` 素材目录（office/pdf/media/images/markdown/openapi/excalidraw）存在 ✓。

**③ 建议怎么改**

优先修 ⑤⑥（`isPC` 与 960 阈值是**会误导实操**的两处，前者让读者以为该改一个不存在的变量，后者会让「zoom 后仍判宽屏」的结论算错）；再批量修 ②③④⑧⑨⑩⑪ 的行号，并把裸文件名补成完整路径（`web/src/components/common/TabPanel.vue` 等）。另外 `docs/Skills/clawbench-screenshot/SKILL.md` 的 `offsetParent` 判据与本文 §二 矛盾，应以本文为准回改前者。

---

#### `docs/timer/code-review.md`

- **行数**：266 ｜ **类别**：其他
- **标题**：代码 Review

**① 这份文档讲什么**

自动化代码 Review 的 agent 任务提示词——含 P0/P1/P2/P3 维度、复现要求、排除目录规则。

主要章节：Review 维度与优先级、复现要求、排除项、Step 1 — 确定模式、Step 2 — 流程追踪（增量模式）、Step 3 — 生成 Review 计划、Step 4 — 逐 Block 执行 Review、#Block 文件格式

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时。无过时文件/函数/端点引用。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/timer/deploy.md`

- **行数**：249 ｜ **类别**：其他
- **标题**：发布后部署

**① 这份文档讲什么**

ClawBench Docker + NPM 部署流程 agent 任务提示词。

主要章节：1. 获取最新版本标签、2. 检查是否已部署当前版本、3. 确认发布产物已就绪、4. 本地部署：绿色版（宿主机，端口 20300）、#4.1 清理端口占用、#4.2 下载绿色版、#4.3 启动绿色版、#4.4 等待启动并获取密码

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。
  - 未发现明显过时。无过时脚本引用；`clawbench-npm` 容器名、`/data/.clawbench/` 路径、`@xulongzhe/clawbench-linux-x64` 包名均与代码库一致。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/timer/doc-sync.md`

- **行数**：183 ｜ **类别**：其他
- **标题**：文档同步更新（每日补充 + 系统设计）

**① 这份文档讲什么**

文档同步 agent 任务提示词。

主要章节：你的角色、分支策略（重点）、执行步骤、#第一步：准备固定 Worktree 并 rebase 主线、#第二步：获取最近提交、#第三步：分析变更内容、#第四步：更新补充文档、#第五步：更新系统设计文档（docs/spec/）

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/timer/github-issue-fix.md`

- **行数**：237 ｜ **类别**：其他
- **标题**：GitHub Issue 自动修复

**① 这份文档讲什么**

GitHub Issue 修复 agent 任务提示词。

主要章节：前置知识、#标签体系、#与 Task #9 的关系、工作流程、#Step 1 — 扫描 & 分类、#Step 2 — 评估 & 选择目标、#Step 3 — 创建独立 Worktree 并修复、#Step 4 — 验证

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `YYYYMMDD` （第 101 行） — 类型：符号

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

---

#### `docs/timer/issue-fix.md`

- **行数**：183 ｜ **类别**：其他
- **标题**：Review Issue 清理

**① 这份文档讲什么**

通用 Issue 修复 agent 任务提示词。

主要章节：前置知识、工作流程、#Step 1 — 盘点、Issue 盘点、#Step 2 — 验证已有修复、#Step 3 — 选择修复目标、#Step 4 — 执行修复、#Step 5 — 在独立 Worktree 中提交改动

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `.clawbench/review-task-prompt.md` （第 173 行） — 类型：路径

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

---

#### `docs/timer/issue-locate.md`

- **行数**：175 ｜ **类别**：其他
- **标题**：Issue 定位与修复

**① 这份文档讲什么**

Issue 定位 agent 任务提示词。

主要章节：硬性边界：只读远端、输入：事件上下文、Step 1 — 定位问题、Step 2 — 评估可行性、Step 3 — 创建独立 WorkTree 并修复、#3a. 创建 WorkTree、#3b. 实施最小化修复、#3c. 补充测试用例（CI 有覆盖率门禁）

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `NUMBER` （第 38 行） — 类型：符号

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

---

#### `docs/timer/pr-auto-review.md`

- **行数**：219 ｜ **类别**：其他
- **标题**：PR / Issue 自动审查

**① 这份文档讲什么**

PR 自动 Review agent 任务提示词。

主要章节：硬性边界：只读，不写远端、输入：事件上下文、Step 0 — 定位条目、Step 1 — PR 分支走查（仅 `ITEM_TYPE=pr`）、Step 2 — Issue 走查（仅 `ITEM_TYPE=issue`）、Step 3 — 本地归档、结论、发现项

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

---

#### `docs/timer/pr-review-merge.md`

- **行数**：156 ｜ **类别**：其他
- **标题**：GitHub PR 审查与合并

**① 这份文档讲什么**

PR Review + Merge agent 任务提示词。

主要章节：总则、步骤、去重逻辑：不要重复评论（仅针对其他人的 PR）、注意事项

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

机器复核命中（链接/路径/符号在仓库中无法解析）：
- `YYYYMMDD` （第 49 行） — 类型：符号

**③ 建议怎么改**

- 按上表逐条核对：引用了不存在的文件/符号的，改成实际路径或实际符号名；确属「计划中尚未落地」的，加一句显式声明（与本仓库既有的「已移除 / 刻意不新增」写法保持一致）。

---

#### `docs/timer/release.md`

- **行数**：229 ｜ **类别**：其他
- **标题**：发布

**① 这份文档讲什么**

Release 发布 agent 任务提示词。

主要章节：1. 检查是否有新提交需要发布、2. 分析提交确定版本号、#版本升级规则、#版本号计算、3. 生成详细的 Release Notes、#3.1 获取完整提交信息和代码变更、#3.2 分析并分类提交、#3.3 生成 Release Notes 文本

**② 有没有引用了已经不存在的文件或函数**

- ✅ **未发现失效引用**。

**③ 建议怎么改**

- 未发现需要修改之处。

## 三、汇总

### 3.1 逐份结论统计

- 干净（无失效引用）：**149** 份 / 205（73%）
- 有机器命中：56 份

### 3.2 按目录的密度

「机器命中」= 链接/路径/符号在仓库中无法解析的处数。

| 目录 | 份数 | 机器命中（处） | 有命中的文档（份） |
|---|---|---|---|
| `docs/Skills/clawbench-screenshot/` | 1 | 0 | 0 |
| `docs/Skills/clawbench-user-guide/` | 1 | 3 | 1 |
| `docs/dev/` | 21 | 39 | 9 |
| `docs/plans/` | 57 | 184 | 24 |
| `docs/spec/` | 3 | 0 | 0 |
| `docs/spec/api/` | 1 | 0 | 0 |
| `docs/spec/client/` | 7 | 4 | 2 |
| `docs/spec/core/` | 5 | 0 | 0 |
| `docs/spec/features/` | 22 | 1 | 1 |
| `docs/spec/infra/` | 13 | 2 | 2 |
| `docs/superpowers/plans/` | 17 | 11 | 6 |
| `docs/superpowers/specs/` | 19 | 2 | 2 |
| `docs/timer/` | 9 | 4 | 4 |
| `docs/user-guide/` | 29 | 10 | 5 |

### 3.3 计划类文档的落地状态分布

- 已落地：**41** 份
- （未标注）：**36** 份
- 部分落地：**8** 份
- 已被取代：**5** 份
- 未落地：**3** 份

这批文档里「部分落地 / 未落地 / 已被取代」的合计 **16** 份，是 `docs/plans/` 里过时引用的真正来源——
设计在实施中被推翻或改名，才是路径失效的主因；这类文档加顶部状态横幅即可，不必逐条改路径。

### 3.4 行动清单（按性价比排序）

1. ~~**修 `docs/spec/database.md`** — 幽灵表 `agent_api_keys` + 漏记表。~~ **[已完成 2026-10-21]** 全文按实测 schema 重写（36 张表）。原条目里的「19 张漏记表」数字有误，见页首勘误。
2. **修九处 P0 规格/手册漂移** — `auth-and-middleware.md` 的 `/api/settings/password` → `/api/config/password`；`README.md` / `api/README.md` 的 176/224 → 178/226；`setup-wizard.md` 的后端数 15/16 → 18；`.index.md` 的 Java 模块数与 Bridge 方法数；`push-notifications.md` 的 `session_command.go` 路径；`getting-started.md` 的 Dock 页签数 9 → 10；`settings.md` 的设置分类数 18 → 20。
3. **消除「魔法数字」** — 把后端数、端点数、表数、页签数改为不写死数字，或加一条漂移守卫测试（参照已有的 `openapi_drift_test.go`）。这是本次问题的**共同根因**，不根治就会反复出现。
4. **统一计划文档的归档方式** — 不逐条改 `docs/plans/` 的路径，而是每份加状态横幅；同时把 `docs/plans/` 与 `docs/superpowers/` 移出「现行文档」的导航，避免读者误当规范。
5. **修六处 P1 笔误** — `push-notifications.md` 的路径、`annotation.md` 的 `web/src/app.ts`、`clawbench-user-guide/SKILL.md` 的三个脚本与其 `data-path` 语义、`FAQ.md` 的 `config.example.yaml` 链接、`android-integration.md` 的措辞、以及 `CLAUDE.md` 里的 `docs/user-guid/` 路径。

### 3.5 一句话结论

`docs/` 205 份文档整体健康度**很高**：73% 的文档零失效引用，规格层与用户手册层的关键论断经逐条 grep 核对基本与代码一致。
真正需要动的是 **9 处 P0 + 6 处 P1**，集中在「数字抄写」和「路径抄写」两类；
`docs/plans/` 的过时引用占机器命中数的大头，但属历史快照的**预期状态**，建议以「整份加状态横幅」而非逐条修路径的方式处理。
