# 视觉设计指导手册

面向**改 UI 的人**：动样式、加组件、加主题之前先查这里，能避开绝大多数"本地看着对、换主题/换设备就坏"的问题。

这份手册只讲**必须遵守的约定**和**已经踩过的坑**。系统长什么样（每个组件的视觉规格）不在这里——那由 `web/css/` 的源码注释和守卫测试负责。

> 与 [前端架构](frontend-architecture.md) 的分工：那份讲**逻辑**（store、composable、渲染管线、导航），这份讲**视觉**（token、主题、布局、CSS 归属）。两份的「设计要点」不重复。

## 快速定位

| 我要做的事 | 看哪节 |
|---|---|
| 改颜色 / 字号 / 间距 / 圆角 | [设计 token](#设计-token) |
| 加一套新主题 | [主题系统](#主题系统) |
| 摆布局、调头部 / 宽屏 / 安全区 | [布局骨架](#布局骨架) |
| 做按钮、角标、弹层、菜单 | [组件约定](#组件约定) |
| 加动画 | [动效](#动效) |
| **写 CSS 前** | [六条红线](#六条红线先读这节再写-css) |
| 提 PR 前自检 | [改动检查清单](#改动检查清单) |

---

## 样式架构

**没有 Tailwind，没有 SCSS，没有 CSS-in-JS。** 全部是原生 CSS + Vue SFC 的 `<style>` / `<style scoped>`。

样式分三层，**新样式放哪层取决于它被谁消费**：

### 第一层：`web/css/` — 全局，`<link>` 加载

`web/index.html` 按顺序引入，**变量必须最先**（后续文件全靠它）。

| 文件 | 职责 |
|---|---|
| `variables.css` | **全部设计 token** + 36 套主题色块。唯一的 token 来源 |
| `base.css` | Reset、`html/body` 基准、壁纸层、滚动条、`color-scheme` |
| `layout.css` | `.app-container` / `.header` / `.main-content` / `.content-area` |
| `wide-screen.css` | ≥1024px 两栏 Dock 布局（类驱动，不用 `@media`） |
| `markdown-common.css` | Markdown 基础元素（`.markdown-body` / `.chat-message.*` 共用） |
| `code-block.css` / `code-block-header.css` | 代码块行结构与头部栏 |
| `content.css` | `.markdown-body` 阅读列、标题、表格、mermaid、KaTeX |
| `media-block.css` | 统一媒体图（`.image-block-wrapper`）+ 浮动工具栏 |
| `components.css` | **跨组件共享的全局类**（`.count-badge`、`.drilldown-*`、`.context-menu`、引用附件卡） |
| `diff-rows.css` | 四种 diff 界面共用的行语言 |
| `share-chrome.css` | 分享 SPA 专用（由 `web/src/share/main.ts` 引入，不在 index.html） |

### 第二层：`web/src/assets/*.css` — 全局但按需引入

组件里 `import '@/assets/x.css'` 或 `@import`，**不加 scoped**，所以同样是全局作用域。放的是**成组的基础设施样式**：`modal-card.css`、`modal-footer-btn.css`（`.fbtn`）、`refresh-spin.css`、`search-bar.css`、`theme-picker.css`、`resize-divider.css`、`code-link-preview.css` 等。

判断标准：这套样式是**一个原语**（按钮语言、弹窗外壳、刷新动效），且被多个组件用 → 放这里。

### 第三层：SFC `<style scoped>` — 组件私有

绝大多数组件样式在这里。**唯一例外**是 `ChatMessageItem.vue` 里那段**非 scoped** 的 `<style>`（`.chat-message` 相关）——因为聊天正文是 `v-html` 注入的，scoped 规则**永远匹配不到注入的元素**。见[红线 1](#红线-1v-html-注入的内容匹配不到-scoped-规则)。

---

## 设计 token

全部在 `web/css/variables.css` 的 `:root` 里（静态值）+ `[data-theme="…"]` 块里（颜色）。**不要写魔法数字**，找最近的 token。

### 字号（7 档，固定 px）

| Token | 值 | 用途 |
|---|---|---|
| `--font-size-2xs` | 10px | 角标、角标、diff 行号槽 |
| `--font-size-xs` | 11px | 密集元信息：工具卡片、文件路径、diff |
| `--font-size-sm` | 12px | 紧凑正文：工具详情、列表行 |
| `--font-size-md` | 13px | **默认 UI 文字**：聊天消息、设置 |
| `--font-size-lg` | 14px | 工具图标、弹窗标题、主按钮 |
| `--font-size-xl` | 15px | 阅读文字——**同时是 html/body 基准** |
| `--font-size-2xl` | 16px | 标题；输入框固定 16px 防移动端 Safari 聚焦缩放 |

**为什么是 px 不是 rem**：外观「缩放」设置通过 `html { zoom }` 缩放整个 UI（`useSettingsConfig.applyUIScale`），px 已经跟着缩放了。引入 rem 反而会双重缩放。

⚠️ **`--font-size-xl` 是 html/body 的基准字号**（`base.css:32`）。改动它 = 全站基准漂移。`designTokens.css.test.ts:57` 钉死这条不变量。

### 间距（4px 基数，保留 2/6/10px）

`--space-1: 2px` → `--space-2: 4px` → `--space-3: 6px` → `--space-4: 8px` → `--space-5: 10px` → `--space-6: 12px` → `--space-7: 16px` → `--space-8: 20px` → `--space-9: 24px` → `--space-10: 28px`

数字是**步号不是尺寸**，同一个步号在密集行里是"内边距"、在卡片间是"间距"，所以刻意不起语义名。

### 圆角

| Token | 值 | 用途 |
|---|---|---|
| `--radius-xs` | 3px | 标签、小 chip |
| `--radius-sm` | 6px | 按钮、输入框、菜单行 |
| `--radius-md` | 10px | 卡片、面板、浮层 |
| `--radius-lg` | 14px | 弹窗、大表面 |
| `--radius-full` | 999px | 药丸、头像、图标按钮 |

`0`（直角）**刻意不 token 化**——写死 `0` 是在表达"这里就是要方"这个决定。

### 动效时长

`--duration-fast: 0.1s`（hover/active 反馈）、`--duration-base: 0.15s`（颜色/背景默认）、`--duration-slow: 0.2s`（大表面淡入）。

**更长的时长刻意不 token 化**：0.25s+ 那批驱动的是 transform / 宽度 / max-height，那点时间正是效果本身（面板滑入用 0.15s 会像坏了）。所以**不要**为了"统一"把它们收进 token。

### 层级（z-index）

**不要写魔法数字**，用命名 token：

```
--z-overlay: 1000              欢迎页/弹窗/底部抽屉的遮罩
--z-overlay-raised: 1001       升级提示，压在遮罩之上
--z-header: 1100               应用头部
--z-sheet: 1200                全屏抽屉、代码预览浮层
--z-preview-tooltip: 1300      预览浮层上的 tooltip
--z-header-overlay: 2000       头部自己的下拉遮罩
--z-quote-bar: 2400            引用回复栏
--z-context-menu-backdrop: 2499
--z-context-menu: 2500
--z-modal: 3000                安装/通用对话框
--z-popover-backdrop: 9998
--z-popover: 9999              菜单、下拉、tooltip、toast
--z-lightbox: 10000            图片灯箱——最高
```

两条不变量（`designTokens.css.test.ts:156`）：
1. 每个 `-backdrop` **恰好比它的表面小 1**——相等会让遮罩盖住自己的菜单。
2. popover / lightbox **必须高于 modal**——从对话框里打开的菜单不能被对话框裁掉。

### 透明度

`--opacity-disabled: 0.4` < `--opacity-muted: 0.5` < `--opacity-soft: 0.7` < `--opacity-hover: 0.9`。

按**为什么淡化**命名，不是按数字。`0`（隐藏）和 `1`（完全显示）保持字面量——它们是区间的两端，不是"淡化的档位"。

### 其他常用 token

- 背景四级：`--bg-primary` / `--bg-secondary` / `--bg-tertiary` / `--bg-elevated`
- 文字：`--text-primary` / `--text-secondary` / `--text-muted` / `--text-hint` / `--text-bold`
- 强调：`--accent-color` / `--accent-hover` / `--brand-color`
- 状态：`--color-green` / `--color-yellow` / `--color-red` / `--color-purple` / `--color-info` / `--color-orange` / `--color-success`
- 用户气泡：`--user-msg-color`
- 阴影：`--shadow-sm` / `--shadow-md` / `--shadow-lg`（**每套主题都必须定义全三档**，`designTokens.css.test.ts:176` 钉死）
- 字号以外还有 `--font-ui` / `--font-mono`（用户可在设置里改，运行时覆盖到 `<html>` 内联样式）

---

## 主题系统

**36 套命名主题**（16 浅 + 20 深），每套**自包含约 40 个颜色变量**，互不继承。

### 机制

- `THEMES` 数组（`web/src/utils/themeMeta.ts:28`）是**唯一真相**——主题 ID、明暗分类、状态栏色、预览色、i18n 键全从它派生。
- 颜色值在 `variables.css` 的 `[data-theme="<id>"]` 块里。
- `<html>` 上挂三个属性：
  - `data-theme="<id>"` — 选色板
  - `data-theme-base="light|dark"` — 明暗快捷选择器（`color-scheme`、hljs、diff 浓度、`.fbtn` 深色覆盖都靠它）
  - `data-hljs-theme="light|dark"` — 代码高亮主题
- `'auto'` 解析为 `matchMedia('(prefers-color-scheme: dark)')` → `github-dark` / `github-light`。
- **首屏不闪白**：`web/index.html:20` 有一段**在 CSS 加载前**跑的内联脚本读 localStorage 并设属性。全新安装猜 `gruvbox-dark`（用 `localStorage.length === 0` 判断，因为服务端的 `config.first_run` 那时还拿不到），记 `clawbench-fresh-theme-guess` 供之后回退。
- 主题是**纯前端本地设置**（只进 localStorage）——同一服务器多设备各选各的，互不干扰。

### 加一套新主题要改的地方

1. `themeMeta.ts` 的 `THEMES` 数组加一行（`id` / `dark` / `labelKey` / `statusBar` / `preview`）
2. `variables.css` 加对应的 `[data-theme="<id>"]` 块，**约 40 个变量一个都不能少**（尤其 `--shadow-sm/md/lg`，守卫测试会查）
3. `web/index.html` 的 `DARK_IDS` 和 `SB_COLORS` 两张表各加一项（内联脚本跑在模块加载前，读不到 `THEMES`，只能手工同步）
4. i18n 加 `settings.items.theme<Name>` 标签键（缺失会渲染裸键）
5. 原生侧若有映射（`MainActivity.java` / `login.html`）同步

**不要**把静态 token（字号、间距、圆角、时长）写进主题块——它们是布局值不是颜色值，`designTokens.css.test.ts:252` 会拒绝。

### 主题选择器的视觉语言

`web/src/assets/theme-picker.css`：行本身**刻意中性**（用当前主题的底色/文字），只用两个小载体表达主题——左边 13px 圆形 `.theme-swatch`（预览色）、右边太阳/月亮图标（用该主题自己的强调色染色）。

选中态靠**三件事**表达：2px 强调色竖条（`.theme-item.active::before`）+ 12% 强调色底纹 + 字重提升。**竖条是承重的**——实测对比度 2.04，而底纹只有 1.09，光靠底纹在浅色主题上看不出来。

### 壁纸

- 后端 `internal/wallpaper/`（校验/缩放/编码），前端 `web/src/utils/themeBackground.ts`（运行时）。
- 四种模式：`none` / `local` / `bing` / `wave`。
- 壁纸渲染成 **`<img>`**（不是 CSS `background-image`）——Android WebView 里 `<img>` 换 `src` 能可靠重解码，而 CSS 自定义属性驱动的 `background-image` 换图可能静默留在旧帧直到重启。
- 层次（`base.css:39`）：`.wallpaper-layer` 在 `z-index:0`，`.main-content` / `.bottom-dock-wrapper` 被提到 `z-index:1`。
- **新增 `.app-container` 的直接子元素必须补进 `base.css:99` 那条 `z-index:1` 规则**，否则会被壁纸盖住。
- 开启壁纸后，工作面板通过 `--panel-alpha` 半透明；**整页根节点转为全透明**，只留 `.tab-panel` 一层可见表面——避免多层 alpha 叠乘。
- 遮罩强度：深色 `rgba(0,0,0,0.35)`，浅色 `rgba(0,0,0,0.12)`；面板不透明度默认 0.85，钳制 0.5–1.0。

---

## 布局骨架

### 根布局

```
.app-container      position:fixed; inset:0; flex column; padding-top: header 高度 + 安全区
  .wallpaper-layer  z-index:0
  .header           position:fixed; z-index: var(--z-header); -webkit-app-region: drag
  .main-content     flex:1; z-index:1（壁纸时）
    .content-area   各 tab panel 容器
  .bottom-dock-wrapper  z-index:1（壁纸时）
```

### 宽屏 vs 窄屏

- 阈值（`useWideScreenLayout.ts:8`）：CSS 宽度 ≥1024px，**或**物理宽度 ≥1280px **且**横屏。
- 物理宽度那条是为了高分辨率平板：2400 物理 px / DPR 2.5 = 960 CSS px，只看 CSS 宽度会漏判。
- 横屏判定用 `screen.width/height` 而非 `window` 内尺寸，防止 Android 软键盘 `adjustResize` 把竖屏平板抖成宽屏。
- 生效方式是给 `.main-content` 加 `.wide-screen` 类，`wide-screen.css` 切成 `flex-direction: row`。

### 安全区

全仓只有两处消费 `env(safe-area-inset-*)`，都在 `variables.css`：`--header-safe-area-top`（顶）与 `--dock-height`（底，`47px + 安全区`）。不要在别处另起一套。

### 头部拖拽（无边框桌面端）

`.header` 是 `-webkit-app-region: drag`。里面的可交互元素必须 `no-drag`，见[红线 3](#红线-3app-region-豁免是控件白名单绝不是填空容器)。

---

## 组件约定

### 按钮

**`.fbtn`**（`web/src/assets/modal-footer-btn.css`）是弹窗/底部抽屉的**药丸按钮语言**：30px 高、圆角 15px、内边距 `0 var(--space-7)`。变体：`.fbtn-primary`（实心强调色）、`.fbtn-danger`、`.fbtn-warn`、`.fbtn-success`。

- 次要按钮的边框从**文字色**混出来（`color-mix(--text-secondary 40%)`），不用 `--border-color`——后者在浅色主题上太接近 `--bg-tertiary`，按钮会融进面板。
- 深色主题下 hover 用 `--text-primary` 提亮而非换灰，否则浅色文字压在同色底上。
- **复用自 `<a>` 的类必须显式 `border: none`**，图标按钮同理（`components.css:129`）——`<a>` 的 UA 边框不会自己消失。

### 角标

**`.count-badge`**（`components.css:39`）两档：默认 16px 高 / `--font-size-2xs`，`.count-badge--md` 18px / `--font-size-xs`。高度来自 `line-height` 而非固定 `height`，所以塞进 spinner 等更富内容时会撑开而不是裁掉。

⚠️ **调用点不得在自己的 scoped 规则里重复声明 `border-radius` 和几何**——scoped 编译成 `.foo[data-v-xxx]`，特异性高于这条全局单类，会把药丸悄悄变回方块。scoped 里只留颜色和字重。`countBadge.css.test.ts` 守住。

### 聊天消息

`.chat-message` 在 `ChatMessageItem.vue` 的**非 scoped** 块里（必须穿透 `v-html`）。角色色：`.chat-message.user` / `.chat-message.assistant`。

- 助手气泡：`--bg-tertiary`，直角；用户气泡：`--user-msg-color`，`border-radius: 20px 20px 0 20px`。
- 元信息栏（复制/详情）在**气泡外**。
- **禁止**给 `.chat-message` 加 `content-visibility: auto`——见[红线 5](#红线-5content-visibility-会造成滚动跳变)。

### 弹层 / 菜单

- **`.context-menu`**（`components.css:188`）：`position:fixed`、`--z-context-menu`、`--radius-md`、`--shadow-md`、`min-width:140px`。
- **`PopupMenu.vue`**：`Teleport to="body"` + `<Transition name="menu-fade">`，定位走 `@/utils/popupMenuPosition`。
- 弹窗外壳用 `ModalDialog.vue`（宽屏）/ `BottomSheet.vue`（窄屏），样式在 `modal-card.css` / `BottomSheet.vue` 的 keyframes 里。
- **跨面板弹层不要用 `useTabDrawer` 的单 tab 绑定**（`effectiveOpen = currentTab===tabId && open`）——窄屏下会把可从多面板触发的弹层在其它 tab 静默吞掉。正解是直绑共享 `open` + 切 tab 时显式关闭。

### 图标

- 库：**`lucide-vue-next`**，用 `:size` 属性。
- 实测最常用的尺寸：**14（311 处）**、16（144）、12（114）、13（66）。**新图标默认用 14**，除非所在位置的邻居都是别的尺寸。
- 共享类里的图标尺寸写在 CSS 里：`.chat-action-btn svg { width:14px; height:14px }`、`.fbtn svg { flex-shrink: 0 }`。
- 自定义品牌图标走 `AgentIcon.vue` / `ProviderIcon.vue`；单色图标配色在 `mono-icon-colors.css`，深浅主题各一套。

---

## 动效

- **时长用 token**：`--duration-fast` / `--duration-base` / `--duration-slow`（含义见[设计 token](#动效时长)）。
- **keyframes 复用现成的**：`refresh-spin`（刷新，0.8s）、`check-in`（成功弹跳，0.4s）、`modal-fadeIn/scaleIn`、`bs-slideUp/Down`、`line-flash`（跳转闪烁）、`refresh-pulse-glow`（陈旧数据脉动）。
- **新按钮不要自建旋转 keyframes**——统一用 `.refresh-spin` + `RefreshButton` 组件（19 处已收敛）。`RefreshButton` 用 WAAPI 驱动旋转并内联 `animation:none` 覆盖 CSS 动画。
- **菜单淡入**：`opacity` + `transform: translateY(-4px)`，`--duration-base`。
- **`prefers-reduced-motion` 必须逐处处理**（没有全局规则）。已处理的参考 `CompletionPopover.vue`、`ChatInputBar.vue`、`SessionList.vue`；`flashReducedMotion.css.test.ts` 守住闪烁类。
- 非 CSS 动效：running 彗星走 WAAPI 指令 `directives/runningSweep.ts`（1500ms，`cubic-bezier(.45,.05,.55,.95)`，与文档时间轴相位锁定）。
- **会话行底边只有一层效果**：3px 平轨道 + 38% 彗星（`--running-track` / `--running-comet` / `--running-head`）。**不要再叠第二层**——曾经是「14px 带 mask 的光晕 + 80% 扫过光带」两层，看起来像两个效果打架、且光晕把光带糊成环境光。待审批时彗星停止并变成整条琥珀呼吸（`--pending-track` / `--pending-comet`）。**被阻塞的行必须换一个不带指令的元素**：指令用 WAAPI 写 `transform`，优先级高于普通 CSS `transform`，同一元素无法靠样式停下。守卫：`runningSweepTheme.css.test.ts`。
- **会话行状态槽（`.session-status`）用「动效」而非「颜色」区分状态**：运行中＝旋转环、待审批＝原地脉动环、未读＝静止圆点。理由是可测量的——36 套主题里有 3 套（ayu-light / gruvbox-light / gruvbox-dark）的 `--accent-color` 与 `--color-orange` **完全相同**，色相本就无法承载区分；且色觉障碍读者拿不到色相信息。优先级 pending > running > unread（`rowStatus()`），因为待审批的会话 runner 仍活着（`running` 为真），若 running 优先则审批请求会被完全隐藏。被阻塞时底部彗星停止并变为整条琥珀呼吸，且**必须换一个不带指令的元素**（`v-if`/`v-else` 两个 `<i>`）——指令用 WAAPI 写 `transform`，`transform:none` 压不过它。守卫：`sessionStatusSlot.css.test.ts`。

---

## 六条红线（先读这节再写 CSS）

### 红线 1：`v-html` 注入的内容匹配不到 scoped 规则

Vue 的 scoped 属性只加在**组件模板渲染出的**元素上。`v-html` 注入的 DOM 没有这个属性，所以 scoped 规则**永远不生效**——而且**静默失效**，不报错。

任何可能被 `v-html` 注入的样式（Markdown 正文、媒体图、KaTeX、标注按钮、DOMPurify 保留下来的标签）必须放**全局** CSS。

```css
/* ✗ 在 <style scoped> 里 —— 对注入内容无效 */
.markdown-body .katex { … }

/* ✓ 在全局 CSS 里 */
```

`media-block.css:21`、`content.css:159`、`annotation-buttons.css:4` 的注释都在反复讲这件事。

**注意**：元素移出 scoped 后必须**自加锚点类**（如 `.chat-message`），否则会污染其它界面。

### 红线 2：共享类必须全局，而且「基规则」也必须全局

组件 `<style scoped>` 只作用于声明它的组件。任何被多个组件复用的 class（行布局、空状态、列表 chrome）都必须进 `css/components.css` 并从 scoped 块**删掉副本**。

**最隐蔽的变体**：修饰规则（`.forge-row.unread`、`.count-badge--md`）早就全局了，只有**基规则**留在 scoped 里。于是拆出的独立组件渲染出的元素「能显示但没有任何几何」——没有 `display:block`、零内边距、零边框、标题不省略——**而单测只断言文本内容，全部通过**。

这就是为什么这里有一批**清单式守卫测试**：`countBadge.css.test.ts`、`forgeDetailChrome.css.test.ts`、`gitHistoryChrome.css.test.ts`。

> 守卫的局限要知道：**只保护清单里列出的类名，漏列即永远不查**。新增共享类必须同步登记。

### 红线 3：`app-region` 豁免是「控件白名单」，绝不是填空容器

`.header` 是拖拽区，里面的可交互元素要 `no-drag`。但**加进豁免清单的必须是控件**（`button` / `a` / `input` / `select` / `.badge-capsule`），**绝不能是占满剩余空间的容器**。

真实事故：`.header-tips` 被列进豁免清单，而它是 `flex: 1`（实测占 1131.5 / 1400px），于是整个头部中间带变成拖不动的死区。

判断方法：加之前问「**它会不会吃掉剩余空间**」。`flex:1` / `inset:0` 都危险。需要豁免时**下沉到内部的收缩元素**（`.stt-viewport` 是 `width: fit-content`）。

### 红线 4：对比度不能靠「固定跳一档背景」

**不要**假设 `--bg-tertiary` 或 `--bg-elevated` 在每套主题里都"明显不同于 `--bg-secondary`"。实测 36 套主题里 **15 套**的 secondary↔tertiary 对比度低于 1.12，ayu-dark 只有 **1.062**——方块直接消失。

正解是**把 `--text-primary` 混进 `--bg-secondary`**，方向由构造保证正确：

```css
background: color-mix(in srgb, var(--text-primary) 8%, var(--bg-secondary));
```

`designTokens.css.test.ts:408` 钉的是**配方**而不是某套主题的十六进制值，所以调色不会悄悄复活这个问题。

同类问题：diff 行**光靠红绿底色不够**。红绿底色之间只有约 1.1:1，色觉障碍读者完全拿不到信息。所以每行还带一条 **3px 实心轨**（`--diff-*-accent`），这是**形状通道**。编辑 diff 界面时**不要删掉那条轨**。

> 轨必须用 `background-image` 渐变实现，**不能**用 `border-left` 或 inset shadow——在 `border-collapse` 表格和 CodeMirror 里会引起抖动。

### 红线 5：`content-visibility` 会造成滚动跳变

**不要**给 `.chat-message` 加 `content-visibility: auto`。它的高度记忆是**每元素**的，而 `listKey` 含消息条数，所以每次发送都整体重挂载并清空记忆 → `scrollHeight` 塌陷 → 钳制 + ResizeObserver 兜底 = **两跳**（实测 5018 → 4050 → 5710）。`ChatMessageItem.vue:619` 有详细记录，**已回退过，勿重加**。

### 红线 6：Android WebView 的像素级怪癖

- **`:root[data-app-mode] .chat-message { will-change: transform }` 不能删**（`ChatMessageItem.vue:663`）——它是跨图层像素污染（GPU ghost）的唯一解。
- **表单控件行高必须是整数 px**：`--input-line-height: 18px`。textarea 文字顶对齐，内容盒高度恰是一行时没有余量；Android WebView 会**独立地**四舍五入行盒和内容盒高度，CJK 字体升降部更大，分数行高下两次舍入不再抵消，光标会明显偏高。桌面 Chrome 恰好舍入一致，所以**只在 WebView 复现**。
- **畸形表单标签要重置 `white-space`**：DOMPurify 会保留真实 `<option>` / `<optgroup>` / `<select>`，而 UA 样式给 `<option>` 加了 `nowrap`，会静默撑破气泡（`overflow:hidden` 裁掉）。`ChatMessageItem.vue:641` 逐标签重置。

---

## 改动检查清单

### 加一个共享类 / 原语样式

- [ ] 放进 `web/css/components.css` 或 `web/src/assets/*.css`（**全局**）
- [ ] 从所有 scoped 块删掉副本，**包括基规则**
- [ ] 如果调用点需要覆盖，只覆盖颜色/字重，**不碰几何和圆角**
- [ ] 加一条清单式守卫测试（源码断言类必须全局声明、不得出现在 scoped 块）
- [ ] 复用自 `<a>` 的类显式 `border: none`

### 加一套主题

- [ ] `THEMES` 数组 + `variables.css` 色块（~40 变量，含三档 shadow）
- [ ] `index.html` 的 `DARK_IDS` / `SB_COLORS`
- [ ] i18n 标签键
- [ ] 原生侧映射（如涉及）
- [ ] 跑 `designTokens.css.test.ts`

### 加一个 dock tab

- [ ] `dockTabs.ts`（union + `DOCK_TABS`，**保持零 import**）
- [ ] `dockTabMeta.ts` 加图标
- [ ] 深链接：模块级 pending ref + `watch([pending, active])`

### 加一个动效

- [ ] 时长用 `--duration-*` token
- [ ] 优先复用现成 keyframes；刷新类一律用 `.refresh-spin` + `RefreshButton`
- [ ] 处理 `prefers-reduced-motion`
- [ ] 闪烁/跳转类加源码守卫测试

### 任何 UI 改动

- [ ] 只用了 token，没有魔法数字（尤其颜色和 z-index）
- [ ] **在浅色和深色主题下各看一眼**（至少 `github-light` + `github-dark`）
- [ ] 没碰 `--font-size-xl` / `--input-line-height` 的语义
- [ ] 新增 `.app-container` 直接子元素已补进 `base.css` 的 `z-index:1` 规则
- [ ] 纯前端改动跑 `npm run build`

---

## 守卫测试索引

这些是「设计系统契约」的可执行形式。改到相关区域时它们会先失败——**先读测试里的注释**，那里写着当初为什么这么定。

| 测试 | 守什么 |
|---|---|
| `components/common/__tests__/designTokens.css.test.ts` | token 值、主题块完整性、未定义 token 引用、窗口控件对比度配方 |
| `components/common/__tests__/countBadge.css.test.ts` | 角标几何全局唯一、scoped 未重加圆角 |
| `components/common/__tests__/modalFooterBtn.theme.css.test.ts` | `.fbtn` 深色主题前景提亮 |
| `components/common/__tests__/wallpaperSurfaceTransparency.css.test.ts` | 壁纸开启后各表面透明度不叠乘 |
| `components/common/__tests__/wallpaperBlurCost.css.test.ts` | 壁纸模糊不留 `will-change` |
| `components/common/__tests__/resizeDivider.css.test.ts` | 拖拽分隔条外观 |
| `components/file/__tests__/flashReducedMotion.css.test.ts` | 闪烁动效遵守 `prefers-reduced-motion` |
| `components/file/__tests__/dockedPaneStacking.css.test.ts` | 停靠预览窗格层级 |
| `components/forge/__tests__/forgeDetailChrome.css.test.ts` | forge 面板 chrome 全局 |
| `components/git/__tests__/gitHistoryChrome.css.test.ts` | git 历史 chrome 全局 |
| `components/settings/__tests__/settingsRowTypography.css.test.ts` | 设置行字号层级 |
| `components/settings/__tests__/settingsHeaderAlignment.css.test.ts` | 设置页头部对齐 |
| `components/__tests__/wideDockIconSize.css.test.ts` | 宽屏 dock 图标尺寸 |
| `assets/__tests__/themePicker.css.test.ts` | 主题选择器中性底 + 色点载体 |
| `assets/__tests__/annotationButtons.css.test.ts` | 标注按钮全局作用域 |

**写新守卫时注意两个坑**（都实际栽过）：
1. **jsdom 不解析 `var()` 和 `color-mix()`**——`getComputedStyle` 会把 `var(--x)` 原样返回。所以 token 类断言必须**读源码**，不能读计算样式。
2. **一个 `.vue` 文件可以有多个 `<style>` 块**，且**选择器可能是逗号组**——源码嗅探要 `matchAll` + `split(',')`，否则漏匹配。
