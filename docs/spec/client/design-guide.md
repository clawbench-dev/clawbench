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
| **写 CSS 前** | [七条红线](#七条红线先读这节再写-css) |
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
| `components.css` | **跨组件共享的全局类**（`.count-badge`、`.drilldown-*`、`.context-menu`、引用附件卡、**app-menu 弹层容器** `.app-menu-title`/`.app-menu-scroll`/`.app-menu-column`/`.menu-divider`/`.theme-picker`、**快捷发送/快捷指令弹层的固定标题** `.quick-send-title`） |
| `diff-rows.css` | 四种 diff 界面共用的行语言 |
| `share-chrome.css` | 分享 SPA 专用（由 `web/src/share/main.ts` 引入，不在 index.html） |

### 第二层：`web/src/assets/*.css` — 全局但按需引入

组件里 `import '@/assets/x.css'` 或 `@import`，**不加 scoped**，所以同样是全局作用域。放的是**成组的基础设施样式**：`modal-card.css`、`modal-footer-btn.css`（`.fbtn`）、`refresh-spin.css`、`search-bar.css`、`theme-picker.css`、`resize-divider.css`、`code-link-preview.css` 等。

判断标准：这套样式是**一个原语**（按钮语言、弹窗外壳、刷新动效），且被多个组件用 → 放这里。

### 第三层：SFC `<style scoped>` — 组件私有

绝大多数组件样式在这里。**例外只有两种**：① `ChatMessageItem.vue` 里那段**非 scoped** 的 `<style>`（`.chat-message` 相关）——因为聊天正文是 `v-html` 注入的，scoped 规则**永远匹配不到注入的元素**（见[红线 1](#红线-1v-html-注入的内容匹配不到-scoped-规则)）；② 组件里**非 scoped** 的 `<style>` 用来给 `Teleport to="body"` 的弹层写样式。

**② 是个陷阱**：非 scoped 的组件样式能生效，只因为该组件挂载了。`AppHeader.vue` 的全局块曾经装着 `.app-menu-title` / `.app-menu-scroll` / `.app-menu-column` / `.menu-divider` / `.theme-picker`——这些是**所有** appSurface 弹层（含终端主题选择器、通知筛选、分享页主题选择器）共用的容器样式，但因为 AppHeader 在每个应用页面都挂着，问题一直没暴露；分享 SPA 不挂 AppHeader，于是「除了唯一还需要它的宿主以外都正常」。**判据**：这段规则除了本组件，还有没有别的宿主（尤其是 `web/src/share/` 这个独立入口）需要？需要就下沉到 `css/components.css`（两个入口都会加载）。

---

## 设计 token

全部在 `web/css/variables.css` 的 `:root` 里（静态值）+ `[data-theme="…"]` 块里（颜色）。**不要写魔法数字**，找最近的 token。

### 字号（7 档，设计 px）

| Token | 设计值 | 用途 |
|---|---|---|
| `--font-size-2xs` | 10 | 角标、角标、diff 行号槽 |
| `--font-size-xs` | 11 | 密集元信息：工具卡片、文件路径、diff |
| `--font-size-sm` | 12 | 紧凑正文：工具详情、列表行 |
| `--font-size-md` | 13 | **默认 UI 文字**：聊天消息、设置 |
| `--font-size-lg` | 14 | 工具图标、弹窗标题、主按钮 |
| `--font-size-xl` | 15 | 阅读文字——**同时是 html/body 基准** |
| `--font-size-2xl` | 16 | 标题；输入框固定 16 防移动端 Safari 聚焦缩放 |

⚠️ 表里是**设计值**，不是最终 px。token 实际定义成 `calc(N * var(--dp))`，见下节。

### 字号驱动的缩放（`--ui-scale` / `--dp`）

**整套 UI 只由一个数字缩放：根字号。** 外观「缩放」写 `--ui-scale`（0.5–2，步长 1%），`base.css` 把它乘进 `html { font-size }`，而所有尺寸都用 `--dp` 表达：

```css
:root { --ui-scale: 1; --dp: calc(1rem / 15); }
html  { font-size: calc(15px * var(--ui-scale, 1)); }
```

- **`--dp` = 一个「设计像素」**（`1rem / 15`）。布局按 15px 基准画的，所以 `calc(4 * var(--dp))` 就是原型里的「4px」，与当前缩放无关。
- **写几何尺寸一律用 `calc(N * var(--dp))`**：padding / gap / width / height / radius / offset / 阴影模糊半径 / 图标尺寸。
- **文字用 `--font-size-*` token**（它们内部就是 `--dp` 倍数）。没有对应档位时（11.5 / 12.5 / 26 / 48 等）写 `calc(N * var(--dp))`，**绝不写裸 px**——裸 px 的字号在缩放后不跟随，视觉上就是「比周围小一号」，而 scale=1 时完全看不出来。守卫：`fontDrivenScaling.test.ts`。
- **字距（`letter-spacing`）同样要缩放**：它是相对字号的比例属性，`0.5px` 在字号放大 1.5× 后视觉上「变紧」。写 `calc(N * var(--dp))`（负值同理，`calc(-0.3 * var(--dp))`）。不用 `em` 的原因同上（会嵌套复合），且不少比例是非有限小数（0.5/11），`em` 在 scale=1 时做不到逐像素一致。
- **发丝线保持字面 `1px`**：边框、分隔线、焦点环、`outline-offset`、1px 阴影。它们该是**一个设备像素**，放大到 1.5px 只会发虚。
- **为什么不用 `em`**：`em` 会随嵌套复合——`1.6em` 的标题放进 `0.875em` 的徽标里会乘起来，而 markdown 里的字号嵌套非常自由。`rem` / `--dp` 永远相对根，不会复合。
- **为什么放弃 `html { zoom }`**：CSS zoom **不缩放布局视口**。实测 zoom 1.5 / 1000px 窗口下 `innerWidth` 仍是 1000 而 `clientWidth` 变成 667，于是 `100vw` 画出 1500 视觉 px（浮动卡片跑出屏幕）、`60dvh` 从占屏 60% 变 90%、媒体查询仍以为有 1000px、canvas 拉伸 backing store；每个消费方都要用 `toFixedCSS()` 在两套坐标间换算。rem 缩放只有一个坐标空间，这些坑整体消失。
- **`--input-line-height` 例外**：表单控件的行盒必须是**整数 px**（Android WebView 会独立舍入行盒与内容盒，分数值下光标偏高），所以由 JS 按 `round(13 × scale × 1.4)` 写入，不走 `calc`。
- **`max()` / `min()` / `clamp()` 里不能出现裸 `0`**：这些函数的参数必须同类型，裸 `0` 是 `<number>`，与 `<length>` 混用会让**整条属性在 computed-value 阶段被判无效并丢弃**——不是夹到 0，而是连同同一简写里的其它方向一起消失。踩过两次：`padding: calc(10 * var(--dp)) calc(10 * var(--dp) + max(0, (100% - 900px) / 2))` 丢掉整条 padding，900px 阅读栏的居中直接没了；`right: max(0, …)` 让 diff 标记回落到 `auto`、被推到视口外。**必须写 `0px`**。这类失效在 jsdom 里看不出来（无布局），`fontDrivenScaling.test.ts` 因此全仓扫描 `max/min/clamp` 的参数列表禁止混用。
- **`--radius-full` 与 `--header-safe-area-top` 不缩放**：前者只要大于元素高度一半就是药丸，后者是物理刘海尺寸。
- **图标**：lucide 的 `:size` 写成 SVG 的 `width` **属性**，属性不跟随根字号。`css/components.css` 里有一组 `:where(svg[width="N"])` 规则按属性值缩放它们，所以 885 个调用点一个都不用改。`:where()` 把特异性压到 0,1,0，自己定尺寸的组件（`.agent-icon-svg`、`.li-spinner`）仍然赢。
- **JS 里参与布局的常量也必须缩放**。这是最容易漏的一类：CSS 改成 `calc(34 * var(--dp))` 之后，JS 里同源的 `34` 就变成了**设计 px**，而 `getBoundingClientRect()` / `clientWidth` 返回的是**已缩放**的 CSS px。两者直接比较会让「放得下几个」算错，直接写进 `style.transform` 会让指示器错位（错量随索引线性增长）。换算**只有一个实现**：`utils/uiScale.ts` 的 `designToCss(designPx)`（非响应式，读一次 DOM）。这一处曾被复制成四份，第五、六处就是这么漏掉的。已处理：
  - `useDockOverflow` / `useToolbarOverflow`：按钮尺寸与间距在 `computed` 里乘 `uiScale`（**必须是 computed**，否则缩放变化不会让计数重算）。这两处刻意保留本地乘法而不用 `designToCss`——`computed` 需要的是响应式依赖，`designToCss` 读一次 DOM 无法作为依赖。缩放 ref 用 `composables/useUiScale.ts`（**每实例订阅，随 `stopObserving` 卸载**；早先的模块级 ref + 模块级监听会把上一个用例的缩放值泄漏给下一个）。
  - `SplitView`：`minLeft` / `minRight` / `gutterSize` 是设计 px，缩放后再用于 `clampRatio` 与 `--split-min-*`。同样保留本地乘法：`onScaleChange` 必须**同步**重新量容器，`watch` 会延到下一 tick，中间有一帧用新缩放对旧尺寸算夹取。
  - `GitCommitList` 传给 `GitGraph` 的 `row-height`：行高 CSS 是 `calc(64 * var(--dp))`，SVG 的泳道网格必须跟着走，否则越往下偏差越大（用 `useUiScale()`）。
  - `UsageStatsPanel` 的堆叠断点 460 与 `CodeLinkPreview` 的卡片最小尺寸 320×160：都是设计 px 对 CSS px 的比较，用 `designToCss()`。前者还要监听 `UI_SCALE_EVENT`——面板宽度是视口百分比，rem 缩放不改变它的 CSS px，`resize` 与 `ResizeObserver` 都不会触发。
  - 判据：**只要一个数字同时出现在 CSS 和 JS 里，它就必须在 JS 侧乘 `uiScale`**。缩放变化用 `UI_SCALE_EVENT` 通知（rem 缩放不触发 `resize`）。

**启动时只「建议」，绝不自动套用。** 存储值就是生效值（`resolveUIScale` 不看屏幕），所以启动时不能重新推导——那正是早先「UI 一跳」的成因。高分辨率屏幕上的补偿改为**询问**：`composables/useStartupUIScalePrompt.ts` 在初始化完成后测一次屏幕，`shouldSuggestUIScale` 判定「建议值比当前值高出 `UI_SCALE_SUGGEST_THRESHOLD`（0.15）」才弹确认框，同意后走 `setLocalConfig('uiScale', …)`（与滑块、自动适配同一个写入路径）。

⚠️ **量的是短边（`currentScreenShortEdge`），不是高度。** 面板的尺寸档位（1080p / 1440p）说的就是短边，而短边是唯一**不随旋转变化**的量。用高度量出过两个 bug：同一台 12.9 寸 iPad（1366×1024）竖屏建议 126%、横屏不弹；1080p 显示器转竖屏（1080×1920）报 178%——竖屏读代码是很常见的用法。设置里的「自动适配」按钮走同一个函数，所以也一并修好了。Wayland 的设备像素归一化（`screenHeightToCssPixels`）仍在，短边必须同样过它，否则 4K@200% 会得到 2.0 再叠 OS 的 2×。

五条边界：①**只往大调**——用户在 1080p 上手动调到 200% 是明确选择，每次启动反问他很粗暴；②1080p 及以下推导恒为 1，永不弹；③**手机与平板一律不弹**（`isMobileOSUA`：Android / iOS / iPadOS 桌面 UA 全覆盖）——系统已按屏幕密度缩放，再叠一层就重复了，设置里 Android 隐藏自动适配行是同一个理由。用 `isMobileOSUA` 而不是 `isAndroidApp`，因为后者漏掉 Android 浏览器和所有 iPad；原生壳判定仍保留（WebView 可以被要求发桌面 UA，`BrowserActivity` 就这么做）；④拒绝时把**建议值**记进 `clawbench_ui_scale_prompt_declined`，同一块屏幕不再问、换分辨率会重新问（记布尔会让换屏后失效）；⑤弹框走 `useDialog` 单例，因此**已有对话框打开时直接跳过**（开第二个会把第一个 resolve 成取消）。两条启动路径（`onMounted` 冷启动、`handleLoginSuccess` 登录后）都要调，composable 内部有每次会话一次的守卫；首次运行还要等欢迎面板关掉（两个全屏模态不叠加）。

⚠️ **`--font-size-xl` 是 body 的基准字号**，且**必须与 `html` 的 15px 设计基准一致**。但 `html` 自己**不能**用这个 token：`--dp` 是 `1rem / 15`，`html { font-size: var(--font-size-xl) }` 会闭合成 `1rem ← 根字号 ← token ← 1rem` 的环，浏览器回退到 16px 初始值（迁移时踩过，表现是整个 UI 无视设置）。`designTokens.css.test.ts` 与 `fontDrivenScaling.test.ts` 都钉住这条。

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
--z-overlay-raised: 1050       升级提示，压在遮罩之上
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

**`--z-overlay` 之上留了一条 1000..1049 的带**给 BottomSheet 按**打开顺序**叠放
（`calc(var(--z-overlay) + var(--bs-open-order))`）：所有 BottomSheet 都 teleport 到
`<body>` 且同一层级，两个同时打开时由 **DOM 顺序**决定谁在上，而 Teleport 在**挂载**时
就固定了顺序——于是在 App 根部挂载、却**后**打开的抽屉（如 /btw 抽屉里打开的失效路径
选择器）会被先打开的抽屉盖住。开序叠放修的就是这个。`--z-overlay-raised` 因此停在整条
带之上（1050），保证升级提示仍压过任何数量的抽屉。

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
- **出厂默认主题 = `gruvbox-dark`**，全平台一致（web 内联脚本 / 桌面壳 `DEFAULT_THEME_ID` / Android `ThemePalette.DEFAULT_THEME_ID`）。它就是 `'auto'` 在深色系统下的解析结果（`getDefaultDarkTheme()`）。
- `'auto'` 解析为 `matchMedia('(prefers-color-scheme: dark)')` → `gruvbox-dark` / `github-light`。
- **`web/share.html` 的内联脚本必须与 `index.html` 语义完全一致**：分享页与主应用**同源、同一把 `clawbench-settings-theme` 键**。分享链接常常是收件人第一次看到 ClawBench，所以「从未存过主题」也必须落盘 `gruvbox-dark`，否则同一台新设备会「应用是 gruvbox-dark、分享页是系统浅色」。「只改 DOM 不落盘」正是这个漂移的历史形态。`themeBootstrap.test.ts` 对两个 HTML 逐条比对（落盘、`auto` 不落盘、旧 `light`/`dark` 迁移、出厂默认值与 registry 一致）。
- **首屏不闪白**：`web/index.html` 有一段**在 CSS 加载前**跑的内联脚本读 localStorage 并设属性。它同时是**出厂主题的唯一权威**：`localStorage` 里从未存过主题 ⇒ 判定为全新安装，**当场把 `gruvbox-dark` 落盘**（不是只改 DOM），因此重启 / 刷新后依然是它。**不要**再用服务端 `config.first_run` 判定——那个标记是进程级的，`InitDB` 建库后就翻成 false，重启（docker restart / 升级 / 重启机器）会把新安装静默打回系统主题；而且它要等 `/api/config` 异步返回，首屏必然先闪一下系统主题。`clawbench-fresh-theme-guess` 与 `applyFirstRunThemeDefaults` 已随该方案一并删除。
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

**三个宿主共用同一套状态机**：`composables/useThemePicker.ts`。宿主是应用头部的 Palette 按钮（`AppHeader.vue`）、终端工具栏的主题选择器（`TerminalPanelContent.vue`）和**分享页顶栏**（`share/ShareThemeButton.vue`）。状态机承载三条会静默失效的规则：① 切换**必须等弹层 `after-leave` 再启动**圆形扩散，否则正在淡出的菜单会被拍进新快照、两段动画重叠；② 必须走 `setLocalConfig('theme', …)`（它才是把 `data-theme` / `data-theme-base` / `data-hljs-theme` 一起刷新的地方），直接写 localStorage 会留下旧配色；③ 扩散原点取按钮自身（`originFromElement`），退回 `headerThemeOrigin()`。`themeReveal.test.ts` 钉住这三条 + 「每个宿主都用共享模块、不得自己 `applyThemeWithReveal`」。

**弹层容器样式在 `css/components.css`，不在 `AppHeader.vue` 的全局块里**：`.app-menu-title` / `.app-menu-scroll` / `.app-menu-column` / `.menu-divider` / `.theme-picker` 原先声明在 AppHeader 的非 scoped `<style>` 里，能生效只是因为 AppHeader 在每个应用页面都挂着——分享页不挂 AppHeader，于是「除了唯一还需要它的宿主以外都正常」。凡是非应用宿主也要用的规则，放 `components.css`。

**分享页没有 App.vue，主题变更的两条副作用要自己接**（`share/shareTheme.ts` 的 `installShareThemeSync`）：① 响应式主题 ref（`provide('theme')` 给 OpenApiPreview 选 Swagger UI 配色）② mermaid 重渲染（配色是 `initialize({theme})` 时烘进 SVG 的，不跟 CSS）。另在 `share/main.ts` 调 `startSystemThemeWatcher()`，否则「跟随系统」只在加载时解析一次、系统切换深浅色不跟随。守卫：`share/__tests__/shareTheme.test.ts`、e2e `share-theme.spec.ts`。

### 壁纸

- 后端 `internal/wallpaper/`（校验/缩放/编码），前端 `web/src/utils/themeBackground.ts`（运行时）。
- 四种模式：`none` / `local` / `bing` / `wave`。**选择是每设备独立的**（localStorage）：来源 / 总开关 / 本地选中项都不进服务端配置，服务端只持有共享资源（图库文件、Bing 缓存）。
  - `wave` 读作「**动态**」而非某一种动态风格：它下面还有 `wallpaperAnimatedStyle`（`xmb` / `silk`）选具体风格。模式值刻意保留 `'wave'`（改它会作废所有已存值，而它本来就是「动态」的意思）。
- **动态壁纸是多风格的注册表**（`web/src/utils/animatedWallpapers/`）：一个风格 = 一个模块（`id` / `labelKey` / `params` / `speedRange` / 纯函数 `draw(frame)`）。**加一种风格只做三件事**：新建模块、注册进 `index.ts`、补 i18n 标签。设置面板的风格选择器与全部滑块都由 `params` 渲染，渲染组件按 `id` 派发，二者都不用改。
  - **生命周期属于宿主组件**（`AnimatedWallpaper.vue`：rAF 30fps 上限 / `MAX_DT` / visibilitychange 暂停 / ResizeObserver / reduced-motion 单帧 / 固定 1.5× 超采样），`draw` 只有绘制。这样新风格不会漏掉生命周期，也不会泄漏循环。
  - **风格参数存在独立 localStorage 模块**（`useAnimatedWallpaperParams`，`Record<styleId, Record<key, number|boolean>>`），**不走 `localConfig`**——后者是 `string|boolean|number|null` 标量管线，存对象会被 legacy 分支写成 `"[object Object]"`。
  - `draw` 的**三条硬性守卫**（原型里踩过的真实缺陷）：`cssW<=0` 必须提前 return（否则 `u=x/cssW` 变 NaN，`createLinearGradient` 抛错中断整帧）；坐标必须 `Number.isFinite` 检查（canvas 对 NaN 路径**静默丢弃**，表现为「风格凭空消失」且控制台干净）；颜色解析必须回退（canvas 对非法 `fillStyle` 静默保留上一次的值）。
  - 两种风格的**速度倍率上限不同**（`xmb` 0.2–2.0× = 线上既有手感，`silk` 0.25–2.5× = 原型值），所以 `speedRange` 是**每风格**的，共用同一个 10–100 速度滑块。
  - XMB 的 `fadeEdges` 是 canvas 的 `destination-in` 横向渐变，**与图片壁纸的「边缘柔化」（`wallpaperEdgeFade`，CSS `mask-image`）是两回事**，故键名与标签都分开（两者 mode 互斥，不会同时出现）。
- 壁纸渲染成 **`<img>`**（不是 CSS `background-image`）——Android WebView 里 `<img>` 换 `src` 能可靠重解码，而 CSS 自定义属性驱动的 `background-image` 换图可能静默留在旧帧直到重启。
- 层次（`base.css:39`）：`.wallpaper-layer` 在 `z-index:0`，`.main-content` / `.bottom-dock-wrapper` 被提到 `z-index:1`。
- **新增 `.app-container` 的直接子元素必须补进 `base.css:99` 那条 `z-index:1` 规则**，否则会被壁纸盖住。
- 开启壁纸后，工作面板通过 `--panel-alpha` 半透明；**整页根节点转为全透明**，只留 `.tab-panel` 一层可见表面——避免多层 alpha 叠乘。
- 遮罩强度：深色 `rgba(0,0,0,0.35)`，浅色 `rgba(0,0,0,0.12)`；面板不透明度默认 0.7，滑块范围 0–100%。
- 设置面板的**高斯模糊 / 边缘柔化只作用于图片**：动态模式下这两行**整行移除**（不是置灰）——对当前背景永不生效的控件是噪音。面板不透明度对动态壁纸同样有效，保留；动态风格自己的参数行只在动态模式下出现，且**按 `params` 渲染**（加风格不加 UI 代码）。

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

### 软键盘与底部 dock

- **键盘弹起时隐藏 dock**（`v-show` 上挂 `!isSoftKeyboardOpen`，状态来自 `web/src/composables/useSoftKeyboard.ts`），**两端一致，含 Android WebView**。
- 为什么不是把 dock 顶上去：手机浏览器弹键盘只缩**视觉视口**，`position:fixed; inset:0` 的 `.app-container` 与其中的 dock 仍按完整布局高度排布；用 `bottom: <键盘高>px` 补偿实测会差约 20%，仍被遮住。Android WebView 走 `adjustResize`（布局视口本身缩短）所以从不复现——别用「安卓没事」推断浏览器也没事。
- 判定要点：`focusin/focusout` 认**任意可编辑元素**（不只聊天框/终端）+ 阈值（≥120px，排除浏览器地址栏）+ 轮询（部分 WebView 不发 resize 事件）。
- 隐藏是 `display:none` 切换，Android WebView 可能不派发该转变的 ResizeObserver 回调，故必须保留键盘关闭后 `nextTick` 重测 dock 宽度的安全网（否则溢出布局按隐藏期的宽度算）。
- `useChatKeyboard` / `useTerminalKeyboard` 仍在，但它们只负责**内容区**不被键盘遮住（`.chat-keyboard-open` / `.terminal-keyboard-open` 的 `bottom` 收缩），与 dock 可见性是两件事。
- **三个键盘探测都必须有 ≥120px 阈值**（`useSoftKeyboard` / `useChatKeyboard` / `useTerminalViewport` 各自持有 `KEYBOARD_MIN_HEIGHT`）。原始差值 `innerHeight - visualViewport.height - offsetTop` **不是键盘专属**：桌面端经典横向滚动条就会让它变成 ~15px，浏览器工具栏同理。一旦少了阈值，这个值会被写成 `.chat-keyboard-open { bottom: 15px }`（或 terminal 那版），**收缩 `.app-container` → 动态壁纸画布被 resize → 清空一帧 → 闪一下**。这正是「只有聊天输入框聚焦会闪、文件管理器搜索框不会」的原因——只有聊天框会调 `useChatKeyboard`。
  - 同理，`useTerminalViewport` 的 `setAdjustResize(resizeKeyboard > 0)` 也必须用同一阈值：滚动条造成的 15px 会被误判成 Android adjustResize，从而**抑制真正的键盘补偿**。
- **浮动元素（如选区工具条）跟随键盘要按「两种视口模式」分别处理**（`useVisualViewportInset.ts`）：`position: fixed` 的偏移按**布局视口**计算，而移动浏览器弹键盘只缩**视觉视口**，故需补偿 `innerHeight - (visualViewport.offsetTop + visualViewport.height)`；但 Android WebView 走 `adjustResize`（布局视口本身缩短），此时补偿必须为 **0**——两种模式必须分开判，否则 adjustResize 下会重复补偿、浮动框被顶飞。按 `visualViewport.scale` 排除 pinch-zoom 误判。**关键陷阱：`getZoomedViewport()` 直读 `innerHeight`、不是响应式的**，adjustResize 下 inset 恒 0 致依赖它的 `computed` **永不重算**——故须额外暴露一个 `viewportVersion`（签名**必须包含布局 `innerWidth`/`innerHeight`**）并由消费方 `void` 读取以建立依赖。

### 宽屏 vs 窄屏

- 阈值（`useWideScreenLayout.ts` 的 `WIDE_SCREEN_MIN_WIDTH`）：**CSS 视口宽度 ≥960px**，仅此一条。
- **为什么是纯 CSS 宽度、且不带物理像素回退**：布局实际能用的只有 CSS 像素，所以「放得下第二栏吗」只在 CSS 像素里有意义。早期版本带一条「物理宽度 ≥1280 且横屏」的回退，本意是救高 DPR 平板（2400 物理 px / DPR 2.5 = 960 CSS px），但副作用是拿「屏幕玻璃尺寸」越权替「可用布局空间」做决定，产出自相矛盾的结果——横屏手机（CSS 698px）被判宽屏，而桌面 960px 窗口反而不算。它也无法用 CSS 表达（媒体查询只认 CSS 像素），会让 JS 信号与 CSS 媒体查询在边界处分歧。把阈值从 1024 降到 960 就把平板那个 case 直接并进阈值里，回退条款可整体删除。
- **UI 缩放影响判定**：判据是**有效宽度** `innerWidth / uiScale`。面板本身随缩放变大，所以同一个窗口在 1.5 倍下只剩约 933 设计 px，确实放不下两栏（1400px 窗口会掉成单栏，1440px 正好 960 保持两栏）。`computeIsWideScreen(cssWidth, uiScale)` 两个参数缺一不可。
  - **缩放变化不会触发 `resize`**：rem 缩放改的是元素尺寸不是视口，所以 `applyUIScale` 会广播 `clawbench-ui-scale-change`（`utils/uiScale.ts` 的 `UI_SCALE_EVENT`），布局监听它重算。少了这条，布局会一直沿用上一个缩放档位的判定。
- 生效方式是给 `.main-content` 加 `.wide-screen` 类，`wide-screen.css` 切成 `flex-direction: row`。
- **不要拿宽屏阈值当「布局放得下吗」的通用判据**：那是另一个问题、另一个阈值。例如头部胶囊的段标签用 `@media (min-width: 600px)`（见 `AppHeader.vue` 的 `.seg-label`）——带标签的胶囊实测仅约 215px，600 就够，用 960 反而会在有空间的横屏手机上隐藏标签。
- **默认分屏比例是 4:6（左侧稍窄）**，常量 `DEFAULT_RATIO` 在 `utils/splitRatio.ts`——composable 初值与 `resetWideScreenState` 都引用它，不要把 0.4 散落成魔数。用户已持久化的自定义比例不受影响（localStorage 优先）。

### 聊天区阅读栏（超宽屏收窄）

- **一个度量管整个聊天窗格**：`--chat-content-max-width`（900 设计 px，与 Markdown 预览阅读栏、分享页 `.session-share-column` 同宽）。消息列、输入区整块（动作条 / 推荐条 / 输入框 / 会话信息条）以及输入框上方的堆叠卡片（计划 / 团队 / 排队）都按它收窄；**标题栏不收窄**——它是窗口结构（底部分隔线、重命名按钮都属于窗格），收窄后会变成一根悬在半途的线。
- **收窄通过 `max(<floor>, (100% - 度量) / 2)` 的花括号实现，不是 `max-width` + `margin: auto`**：堆叠卡片是拉伸 flex 列里的 `width: auto` 元素，`max-width` 一旦不生效就同时丢了左右留白。三个 floor 刻意不同，所以是三个 token：`--chat-content-inset`（消息列，floor `0px`）、`--chat-content-gutter`（输入区，floor `--space-4`）、`--chat-content-card-gutter`（堆叠卡片，floor `--space-5`）。**窄于度量时一切保持原状**（消息区仍然两边贴边、输入区仍然 8px、卡片仍然 10px）。
- **消息列的留白花在滚动容器 `.chat-messages` 的 `padding` 上，绝不能用 `margin`**：`padding` 让滚动容器自己的盒子仍占满窗格，滚动条因此画在**会话区域的右边缘**；改成 `margin` 会把整个盒子收窄，滚动条跟着内移、贴到消息列边上（线上被指出「滚动条贴着消息」）。百分比 padding 相对包含块（窗格）解析，左边缘仍与输入区对齐。
- **滚动条占的宽度必须在右侧内边距里补回来，而且只能运行时测**：经典滚动条会从滚动容器的布局盒里扣掉自己的宽度，于是消息列内容盒比输入岛短一条滚动条宽、整列还偏左半条宽（线上被指出「消息跟输入框没对齐」）。**这个宽度写不进 CSS**：overlay 滚动条平台（macOS/iOS 默认、多数 Android WebView、headless Chromium）是 0，经典滚动条是 10，粗指针是 4，而 WebKit 的 `scrollbar-gutter: stable` 只在内容真的溢出后才预留（短会话报 0、长会话报 10）。所以 `ChatMessageList` 用 `offsetWidth - clientWidth`（恰好等于盒子损失量）实测后写到元素的 `--chat-scrollbar-lane` 上，右侧内边距 `max(0px, inset - lane)`，左侧不减（左侧也减会让整列左移）。重测时机：挂载、滚动容器 resize、内容增长（WebKit 下内容增长才让滚动条出现）。
- **`scrollbar-gutter: stable` 仍要保留**：它让滚动条在长短会话之间不出现/消失，否则列的右边缘会在会话刚超过一屏时跳一条滚动条宽。Chromium/Firefox 都完整支持；WebKit 只在溢出后生效（上一条已用运行时测量兜住）。
- **形状随留白翻转，判定用 container query 而不是 `@media`**：`ChatPanelContent` 的 `.chat-panel-content` 是 `container-type: inline-size; container-name: chat-pane`，阈值写 `calc(60rem + 1px)`——`60rem` 就是 `--chat-content-max-width`（`--dp` = 1rem/15，900/15 = 60），所以翻转点精确等于「留白不再是 0」。**必须用 rem 不能写 px**：container query 里的 `rem` 按根字号解析（三引擎实测），因此跟随 UI 缩放；写 px 会在大字号下错位。分屏能把窗格宽度和窗口宽度解耦，所以 `@media` 会在错误时刻触发。
  - **通宽（窗格 ≤ 度量）**：助手回复是**直角通栏色带**，输入区是**扁平通栏条**（顶部一条 `inset` 发丝线）。圆角贴边会被窗格切掉，所以这里必须直角。
  - **有留白（窗格 > 度量）**：助手回复变**圆角卡片**（`--radius-lg`），输入区变**底部 dock 岛**——按同一个 gutter 两侧内缩、贴窗格底边、**只圆上面两角**（下面那条边就是窗格边，四角全圆会显得岛在悬空）、去底部边框、`box-shadow: none`（岛自己的顶部边框取代发丝线）。岛的底色用 `color-mix(in srgb, var(--text-primary) 4%, var(--bg-secondary))` 再乘 `--panel-alpha`，**不能用 `--bg-tertiary`/`--bg-elevated`**：36 套主题里 15 套 secondary↔tertiary 对比度低于 1.12（ayu-dark 只有 1.062），岛会直接消失（见红线 4）。
- **`container-type` 与 App 模式的 `contain: paint` 可共存**，实测三引擎：`container-type` 单独**不会**让元素成为 `position: fixed` 子孙的包含块，叠加 `contain: paint` 也不改变既有行为（真正起作用的是后者，app-mode 本来就有）。表格复制菜单的位置仍由运行时测量决定（`utils/fixedPosition.ts`）。
- 守卫：`components/chat/__tests__/chatContentMeasure.css.test.ts`（度量与阈值换算、三个 gutter 的 floor、消费点不得自己重算公式、滚动容器承担留白、container 声明、形状随留白翻转、岛只圆上两角、底色必须是 text 混色、标题栏不得收窄）+ `e2e/specs/chat-reading-measure.spec.ts`（真实引擎里量两条列的左右边缘是否重合、两种形状的 computed 值）。

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
- **设置面板内的按钮一律复用 `.fbtn`**（不新建按钮类）：行内动作按钮、编辑器确认按钮、主题重试按钮等。
  30px 高是设置面板的**控件基准高度**——开关、滑块行、分段按钮都对齐它。
  scoped 块里只留布局（`flex-shrink` / `flex` / `margin`），形状与配色全交给 `.fbtn`。
  **组件若独立使用 `.fbtn`（而非经 ModalDialog 继承），必须自己 `import '@/assets/modal-footer-btn.css'`**，
  不能依赖祖先恰好导入过。

### 设置面板控件（`.settings-item__switch` / `__slider*`）

开关、滑块、滑块重置按钮的**形状只定义一次**，在全局 `web/css/components.css` 的
「Settings controls (shared)」段——它们原先在 `SettingsItem.vue` 与 `WallpaperSetting.vue`
里逐字重复（开关还多出第三份 `.group-panel__switch`），改尺寸要改 2~3 处且漏一处就静默发散。

- 开关 **44×26**（滑块 22px、位移 18px = 44−22−2×2）；滑块宽 **100px**；重置按钮 **26×26**。
- **必须全局，不能 scoped**：scoped 规则编译成 `.foo[data-v-x]`（0,2,0），特异性**高于**全局单类（0,1,0），
  scoped 里残留的任何几何声明都会静默压过共享尺寸（同[红线 2](#红线-2共享类必须全局而且基规则也必须全局)）。
  组件里只留布局。守卫 `settingsControls.css.test.ts` 同时钉「形状只在全局」与「scoped 不得重加几何」。
- **输入框是刻意的例外：保持圆角矩形**（`--radius-sm`），不跟随药丸——否则「可输入」与「可点击」在形状上无法区分。
  高度仍对齐 30px 基准；**字号不缩**（输入值不得小于其标签，见 `settingsRowTypography.css.test.ts`）。
- **非设置页也能复用**：任务表单的门控脚本开关（`TaskFormPage.vue`）直接用
  `.settings-item__switch` / `-input` / `-track` 三个类，只在自己的 scoped 块里加
  行布局（`.script-switch-row`），不复制几何。需要开关时照此办理，别再写第四份。

### 自定义卡片块的横向内边距（不要和 `SettingsItem` 叠一层）

设置卡片里混排「`SettingsItem` 行」与「自定义块」时，**容器不要再加横向内边距**：
`SettingsItem` 自带 `padding: 12px 16px`，卡片行都对齐在这 16px 上；容器若再加一层
（例如 `padding: 0 16px`），开关/文本行的文字就变成 32px，比相邻普通行明显更深
（技能设置页的「启用技能注入」实测如此）。正解：容器 `padding: <纵> 0`，由各自定义块
自己写 `padding: 0 var(--space-7)` 对齐到同一 16px。`WallpaperSetting` 是同一范式。

### 行内重置按钮（`.settings-item__slider-reset`）

滑块行右侧的 ↺ 重置按钮**常驻显示**，不用 `v-if` 按「当前值 ≠ 默认值」开关。

- **为什么**：按值出现/消失会让控件簇宽度变化——重置的瞬间按钮消失，滑块和数值标签横向跳动（用户实测「很难受」）。
- **做法**：`:disabled="当前值 === 默认值"`，CSS 用 `opacity: var(--opacity-disabled)` + `cursor: not-allowed` 灰显。按钮留在文档流里，宽度恒定。
- **唯一允许的 `v-if` 是 `defaultValue !== undefined`**：整行没有可重置的目标时，按钮应当是**不存在**而不是永久禁用（永不生效的控件是噪音，同 §壁纸那条）。判据是「该控件**能否**生效」，不是「此刻**是否**已生效」。
- 形状只在 `css/components.css` 定义一次（见 §设置面板控件）；两个组件只保留模板与 `:disabled` 绑定。`sliderResetResident.css.test.ts` 守住（钉「presence 不得依赖当前值」「必须有 :disabled」「必须有灰显规则」）。
### 复制按钮

**`CopyButton.vue`**（`web/src/components/common/CopyButton.vue`）是全站唯一的复制按钮。**反馈是图标互换**（图标临时变成对勾）**而非文字标签**，也**不弹 toast**——对勾本身就是反馈。文字标签被刻意排除：按钮通常钉在容器边缘或定宽工具栏里，变宽的标签会挤进旁边的内容，而要保持不挤就必须预留最宽的那条翻译（中文「已复制」与日文「コピーしました」宽度差约 2×）；恒定占位则两个问题都没有。两种模式：非受控传 `text`（组件自己复制并闪一下）、受控传 `copied`（宿主掌握剪贴板写入与计时，用于状态已在父组件的场景如代码预览工具栏）。此前全仓有四套互不一致的反馈机制（图标互换 / 文字替换 / 仅 toast / 仅变色），现已统一。

### Toast

**图标是 toast 的内置结构，不是可选装饰**：每个 toast 都渲染一个原生（lucide）图标，**调用方不再传 emoji**。

- **语义键**：`icon` 传 `ToastIconKey`（`useToast.ts` 导出），如 `attach` / `link` / `copy` / `star` / `cut` / `blocked`。键 → 字形表在 `components/common/toastIcons.ts`（**唯一**来源，composable 不 import 图标库）。
- **类型兜底**：不传键时按 `type` 取默认（`success`→`CircleCheck` / `error`→`CircleX` / `info`→`Info`）。因此「所有提示都有图标」由渲染层保证，不依赖调用点自觉。
- **`loading` 例外**：渲染旋转环（`LoadingIndicator`）而非静态图标——它是唯一区分「在跑」与「卡住」的通道；环与图标互斥渲染（两个信号同时出现是矛盾）。
- **悬停暂停**：指针进入 toast 时 `pause()` 挂起自动关闭倒计时，离开时 `resume()` 用剩余时间继续（`duration: 0` 的手动型不受影响）。文案 `user-select: text` 供复制；**结束文本选择的 click 不得同时关闭 toast**（否则复制到一半就没了）。

### 角标

**`.count-badge`**（`components.css:39`）两档：默认 16px 高 / `--font-size-2xs`，`.count-badge--md` 18px / `--font-size-xs`。高度来自 `line-height` 而非固定 `height`，所以塞进 spinner 等更富内容时会撑开而不是裁掉。

⚠️ **调用点不得在自己的 scoped 规则里重复声明 `border-radius` 和几何**——scoped 编译成 `.foo[data-v-xxx]`，特异性高于这条全局单类，会把药丸悄悄变回方块。scoped 里只留颜色和字重。`countBadge.css.test.ts` 守住。

### 聊天消息

`.chat-message` 在 `ChatMessageItem.vue` 的**非 scoped** 块里（必须穿透 `v-html`）。角色色：`.chat-message.user` / `.chat-message.assistant`。

- 助手气泡：`--bg-tertiary`，直角；用户气泡：`--user-msg-color`，`border-radius: 20px 20px 0 20px`。
- 元信息栏（复制/详情）在**气泡外**。
- **禁止**给 `.chat-message` 加 `content-visibility: auto`——见[红线 5](#红线-5content-visibility-会造成滚动跳变)。
- **系统消息（`role='system'`，群聊成员加入/离开等）统一用 `Megaphone` 图标**，四处一致：聊天气泡的居中细条 `.chat-system-row`（图标 `.chat-system-icon` + 文本 `.chat-system-text`）、会话索引 / 分享目录的 role chip（`MessageIndexRow` 的 `.msg-role-tag.role-system` + 行修饰类 `.msg-item--system`）、分享选择弹窗的角色标签（`.session-share-dialog-role.role-system`）。中性色（`--text-muted`）以示「公告」而非「参与者」。**不得复用 User/Bot 图标**——那会让系统行在列表里被误读成用户消息。图标装饰性（`aria-hidden`），语义由 chip 的 `title`/`aria-label`（`conversationIndexRoleSystem` / `sessionShare.roleSystem`）承载。

### 弹层 / 菜单

- **`.context-menu`**（`components.css:188`）：`position:fixed`、`--z-context-menu`、`--radius-md`、`--shadow-md`、`min-width:140px`。
- **`PopupMenu.vue`**：`Teleport to="body"` + `<Transition name="menu-fade">`，定位走 `@/utils/popupMenuPosition`。
- **弹层里的固定标题用 `position: sticky`，且底色必须等于弹层表面**：`PopupMenu` 的根节点自己就是滚动容器（内联 `overflow-y: auto`），所以第一个子元素可以 `sticky; top: 0` 钉在顶部。三个必守：①`background` 必须是**不透明**且等于 `--bg-secondary`（PopupMenu 的底色）——透明或 `color-mix` 会让滚动中的行从标题下面透出来；②要 `z-index: 1`，因为菜单行常是 `position: relative`（快捷发送行的尾部注入按钮需要），否则行会盖住标题；③这类共享弹层样式放 `css/components.css`，不要放在某一个组件的非 scoped 块里——`.quick-send-title` 原先在 `ChatInputBar.vue` 的全局块，终端快捷指令弹层只是「因为聊天面板先挂载过」才拿到样式，正是上文「弹层容器样式在 `css/components.css`，不在 `AppHeader.vue` 的全局块里」那条陷阱的同型复发。当前实例：`.app-menu-title`（appSurface 列）与 `.quick-send-title`（快捷发送 / 快捷指令），守卫见 `components/chat/__tests__/quickSendMenuStickyTitle.css.test.ts`。
- **`position: fixed` 不等于「视口就是包含块」——定位必须实测包含块**：全站浮层（PopupMenu 及其十余个消费方、表格复制菜单）都用 `fixed` 以免被祖先的 `overflow: hidden` 裁剪，但这笔便利换来一个错误假设。本仓有**两处**会打破它：`.chat-message` 在 App 模式下带 `contain: paint`（Android WebView 的 GPU 重影修复），而 paint containment **会为 fixed 后代建立包含块**并裁剪它——表格复制菜单打开时因此把 `contain` 降级为 `style` 并临时补 `will-change: transform` 顶上（实测：按 `top: rect.bottom + 4` 写入的菜单低了 60px，误差随祖先自身的偏移增大）；以及页面可能仍带着 CSS `zoom`——当前构建的界面缩放走根字号、坐标空间不变，但用户样式表、浏览器扩展、或升级前遗留的旧标签页都可能留下 zoom。两者都无法从 `getBoundingClientRect()` 单独看出来，因此统一由 `utils/fixedPosition.ts` 的 `measureFixedContainingBlock` **测量**：先写一个已知偏移、观察元素实际落在哪，再解出原点与缩放比（探针量程 100px，大到取整误差压不掉比例、小到同步布局期间不出屏）。**它解得的是缩放比而不是假定 1**，正因如此上面两种来源都不需要单独分支。`popupMenuPosition` 在调用方传入 `containingBlock` 时把每个坐标都经它换算，未传时退回「视口即包含块 + zoom 换算」的旧路径。**测量本身有三个坑**：①**必须用克隆**探测，不能挪动真实菜单（否则用户会看到菜单先跳到角落再回来）；②克隆必须**清空 `className` 与 transition**——菜单被 `<Transition>` 包裹，`*-enter-active` 声明的 `transform` 让元素自己成为 fixed 后代的包含块，克隆继承同一组类时量到的是菜单自身的 380×59 而不是视口（实测 `bottom: -596.6px`）；③盒子尚未布局（0×0）时测出的包含块是垃圾值，直接返回 `null` 让调用方回退。**最后一道兜底必须判双向**：算完若把菜单放到视口外就丢弃重算纯 zoom 版本——只拒绝「bottom 为负」（菜单掉到下方）会漏掉相反方向（实测群聊 `@` 菜单落在 `top: -542` / `bottom: 962`，视口只有 720），因此要求 `top`/`bottom`/`left`/`right` **全部**落在 `[0, 视口尺寸]` 内。**单一实现**：`fixedPosition.ts` 是唯一来源，导出文档（`buildCodeBlockJs` 的表格菜单）手工镜像同一套逻辑——重复实现正是两个浮层漂移的原因
- 弹窗外壳用 `ModalDialog.vue`（宽屏）/ `BottomSheet.vue`（窄屏），样式在 `modal-card.css` / `BottomSheet.vue` 的 keyframes 里。
- **宽度只有两档，都写在 token 里、不给调用方留口子**：`css/variables.css` 定义 `--modal-width-narrow`（520）与 `--modal-width-wide`（960），`ModalDialog` / `BottomSheet` 各有一个 `size="narrow" | "wide"`（默认 `narrow`），渲染成修饰类 `.modal-dialog--narrow/--wide`、`.bs-panel--narrow/--wide`，`modal-card.css` 把修饰类映射到这两个 token。调用方**选档位，不选数字**：没有 `maxWidth` prop，也**不允许**在组件里覆盖 `--modal-max-width`（`assets/__tests__/modalWidth.css.test.ts` 全仓扫描拒绝）。历史包袱两笔：曾经每处调用传 380/420/480/520/560/720 六种值再统一乘一个 1.3 的魔法系数；后来又变成「弹窗 900 + 抽屉 624 + 四个抽屉在非 scoped 样式块里各自覆盖 900/960」——同一屏两种规则，`.tool-detail-sheet` / `.session-search-sheet` / `.team-member-sheet` / `.content-search-sheet` 这些覆盖已全部删除，改传 `size="wide"`。
  - **选档判据是「多出来的宽度会不会变成信息」**：会（diff 行更少折行、代码/终端不折、消息原文更少截断、表格列更松、网格多一列）→ `wide`；不会（表单字段、开关列表、确认框、单列设置项）→ `narrow`，否则只是把一行字拉成一页宽。当前 `wide` 的是：消息推荐（含其下钻页）、工具详情、文件变更/diff、Git 历史、代码预览、文件内搜索、内容搜索、会话搜索、群成员详情、/btw 旁路回答、引用详情、快捷键表、消息元数据、表格行、头像选择、主题选择、终端按键配置。
  - **自绘外壳的弹窗也得在同一把尺子上**：`DialogOverlay`（通用确认/输入）、`UpgradeDialog`、`UpgradePromptOverlay`、`SettingsRestartDialog`、`AgentInstallDialog`、`WelcomeOverlay` 不用共享组件，但它们的 `max-width` 一律写 `var(--modal-width-narrow)`——否则同一屏又出现 320/380/420 三种宽度。
  - **窄屏（手机）不受影响**：`.modal-dialog` 是 `width: 100%`，overlay 左右各有 `--space-8`(20px) 内边距，任何 ≥ 视口宽度的 `max-width` 都不会生效。
- **共享外壳的关闭按钮与返回**：两个外壳各有一个右上角圆形关闭按钮 `.modal-close-btn`（lucide `X`、28px，与下钻返回键 `.fd-back-btn` 同尺寸；底色沿用 `.window-controls` 的 `--text-primary` 混入 `--bg-secondary` 配方，静止 16% / hover 24% / active 32%，跨 36 主题最差对比 1.110，hover 用 `@media (hover:hover)` 门控避免触屏残留）。**它必须绝对定位**——header 的右侧动作簇（如 `.bs-header-actions`）自带 `margin-left:auto`，把它作为 flex 子项会被挤走；因此 header 预留对应右内边距（窄屏 BottomSheet 用 `.bs-header-has-close` 修饰类，因 TabPanel 复用 `.bs-header` 却无关闭按钮）。**header 点击不再关闭**（避免误触），backdrop 点击与 Escape 保留。**ModalDialog 会注册返回处理器**（优先级 `PRIORITY_OVERLAY+1`，高于所有 BottomSheet 抽屉），因此 Android 返回键与左右边缘内划都能关闭弹窗——此前它没注册，弹窗对返回无响应。
- **跨面板弹层不要用 `useTabDrawer` 的单 tab 绑定**（`effectiveOpen = currentTab===tabId && open`）——窄屏下会把可从多面板触发的弹层在其它 tab 静默吞掉。正解是直绑共享 `open` + 切 tab 时显式关闭。

### 图标

- 库：**`lucide-vue-next`**，用 `:size` 属性。
- 实测最常用的尺寸：**14（311 处）**、16（144）、12（114）、13（66）。**新图标默认用 14**，除非所在位置的邻居都是别的尺寸。
- 共享类里的图标尺寸写在 CSS 里：`.chat-action-btn svg { width:14px; height:14px }`、`.fbtn svg { flex-shrink: 0 }`。
- **溢出的按钮条要支持拖拽横向滚动**：聊天 Action Bar 的按钮在窄窗格下会溢出，而滚动条是隐藏的——普通鼠标滚轮只能滚页面，够不到被挡住的按钮（触控板横滑与触摸拖拽本来就能用，只有鼠标不行）。`utils/dragScroll.ts` 在**真正溢出时**才挂载（放得下就不拦截按压、也不显示抓手光标），按下并左右拖动即滚动；形态沿用 `dragClickGuard`（独立 util + 返回 disposer + 组件挂载）
- 自定义品牌图标走 `AgentIcon.vue` / `ProviderIcon.vue`；单色图标配色在 `mono-icon-colors.css`，深浅主题各一套。**两类"隐形"陷阱**：①单色图标（`fill="currentColor"`）必须挂 `monoCssClass` 才有主题配色，漏挂时深色主题下 color 是浅色、纯白图标就"消失"；②彩色图标里若有**纯白填充**，它假定底下有一块品牌色底图——而 lobe-icons 并不提供那块底图，于是浅色主题下那部分直接看不见（Kimi 的 K 字形就是这样，只剩右上角蓝点）。正解是换单色版 + 品牌色（`#1783FF` 对白底 3.67、对 `#0d1117` 5.16，两主题都够），而不是补底衬。`agentIcons.test.ts` / `providerIcons.test.ts` 逐图标检查所有 fill 是否都贴近某个主题的底色（渐变图标跳过，其白色 fill 是画在渐变上的高光），并要求每个 `monoCssClass` 在 `mono-icon-colors.css` 里确有规则——类名打错与漏挂同样会让图标隐形。
- **`AgentIcon` 的 `size` 是字符串枚举 `sm|md|lg|xl`，不是像素数字**（改自旧版 `:size="16"`）。四档映射到 `--icon-size-*` token（见下），与所伴文字匹配：`sm`=14（密集 meta，10–12px 文字旁）、`md`=18（默认行，13–15px）、`lg`=24（较大列表/头像上下文）、`xl`=40（设置详情预览位）。**传数字会生成 `agent-icon--18` 这类匹配不到任何规则的类名并静默按继承尺寸渲染**——与 `LoadingIndicator` 的枚举同源约束。
- ⚠️ **批量替换 `:size="N"` 会误伤同文件里的 lucide 图标**（`<Bot :size="16">` 等）——lucide 的 `size` 是数字，不能改成枚举。改 AgentIcon 调用点时必须逐处确认标签名。

**图标尺寸 token（`--icon-size-*`，4 档，px）**

| Token | 值 | 用途 |
|---|---|---|
| `--icon-size-sm` | 14px | 密集行：任务卡片、分享页（伴 10–12px 文字） |
| `--icon-size-md` | 18px | 默认行：选择器、通知、任务、头栏、设置列表、群聊、**会话索引/分享目录的 role chip**（伴 13–15px 文字） |
| `--icon-size-lg` | 24px | 较大上下文：群聊成员/发言者头、聊天欢迎页 |
| `--icon-size-xl` | 40px | 设置详情头像行（唯一大图预览位） |


---

### 加载环（`.li-spinner`）

**全站唯一实现**，声明在 `css/components.css`（**必须全局**——工具调用卡 / mermaid 是 `v-html`/`innerHTML` 注入、localhost 按钮曾是 `::after`，这些 DOM 无 `data-v-*`，scoped 规则永远匹配不到，见[红线 1](#红线-1v-html-注入的内容匹配不到-scoped-规则)）。

- **两个入口**：组件 `<LoadingIndicator>`（渲染 `.li-spinner`，覆盖 72 个文件）或裸 `<span class="li-spinner">`（注入 HTML 用）。组件只拥有外壳布局（`.loading-indicator`），环的形状**不在** scoped 块里。
- **尺寸**：组件档位 `size="sm|md|lg"` = 14 / 28 / 36px。**`size` 是字符串枚举**——传数字（`:size="13"`）会生成不存在的类名并**静默回退 28px**。非档位尺寸（20px / 10px / 8px）的调用方自己在环元素上设 `--li-size`。
- **粗细由比例推导，全站只有一个比例**：`--li-border: calc(var(--li-size) / 6)`。**调用方只能设 `--li-size`，绝不要写死 `--li-border`**——写死会静默偏离比例，且不对比两个环根本看不出来。守卫 `sharedRingUnification.test.ts` 会走查全部源码，出现字面量 `--li-border` 即失败。
- **颜色**：`--li-color`（转动的弧）/ `--li-track-color`（静止底环），都回退主题 token。原 SVG 是 `stroke="currentColor"` 的站点用 `--li-color: currentColor` 承接，外观零变化。
- **速度**：默认 `--li-duration: 0.8s`；从旧实现迁移来的站点（tool-call / url-btn）保留各自的 `0.6s`，**统一形状不等于统一节奏**。
- 覆盖类规则**必须带祖先部分**（如 `.chat-url-open-btn .chat-url-open-btn-spinner`）：`.li-spinner` 是单类 (0,1,0)，写成单类会与之打平，胜负取决于打包 chunk 顺序 → 尺寸会随构建**静默回退 28px**。
- **环只用于「等待一个点状结果」，不用于「等待一块内容」**：当占位的对象是一个**有形状的块**（面板、消息区、图表）时用骨架屏——环只说「在等」，不说「要来的长什么样」，还会让容器塌成一个字形。这条已经落地在 `PanelSkeleton`（dock 面板）、`ChatSkeleton`（消息区）和 Mermaid 的加载占位（`mermaidSkeleton.test.ts`）；Mermaid 是最后一个从环迁走的。
  - **Mermaid 的占位是一个「分叉流程图」骨架**：根节点 → 分叉成两个子节点 → 汇合到一个出口节点，每个节点里有一条标签条。**不能做成几条横杠的堆叠**——那读作段落骨架，而不是图。
  - **几何必须共用同一组百分比**：子节点 `width: 38%` + `gap: 8%` ⇒ 两个中心恰在 27% / 73%，连接线的 `left/right: 27%` 由这两个数推出。第一版用 `calc(dp)` 宽度 + `max-width` 夹取，夹取把真实中心挪到 26.9% 而线还画在 25%，分叉**肉眼可见地没接上节点**。
  - **竖线归节点所有、长度为半个间隙**（`calc(var(--mm-link) / 2)`），横线落在间隙中线上，两边才严丝合缝。用两条半宽 `border-left/right` 的「臂」会把**两根竖线都放在行中央**，渲染成 T 形；把横线画在行自身边缘则被节点盖住、看起来像浮在旁边。
  - **不要同时出现环和骨架**：两者回答同一个问题，并列读作「两件事在发生」。
  - 可见条全部 `aria-hidden`，唯一的 `role="status"` 承载 `media.diagramLoading`（否则读屏用户在这几秒里听到空白）。微光响应 `prefers-reduced-motion`（与骨架原语一致）。

---

## 动效

- **时长用 token**：`--duration-fast` / `--duration-base` / `--duration-slow`（含义见[设计 token](#动效时长)）。
- **keyframes 复用现成的**：`refresh-spin`（刷新，0.8s）、`check-in`（成功弹跳，0.4s）、`modal-fadeIn/scaleIn`、`bs-slideUp/Down`、`line-flash`（跳转闪烁）、`refresh-pulse-glow`（陈旧数据脉动）。
- **新按钮不要自建旋转 keyframes**——统一用 `.refresh-spin` + `RefreshButton` 组件（19 处已收敛）。`RefreshButton` 用 WAAPI 驱动旋转并内联 `animation:none` 覆盖 CSS 动画。
- **菜单淡入**：`opacity` + `transform: translateY(-4px)`，`--duration-base`。
- **长耗时动作（分叉 / ACP 同步）用 `BusyBar` + 常驻 toast**：`components/common/BusyBar.vue` 是 3px 不确定进度条，`position:absolute` 贴在宿主顶部（不占布局高度），只接 `visible` + `label`。**为什么不用按钮内 spinner 代替**：动作栏在窄屏是横向滚动的，被点的按钮可能根本不在屏内；整宽的条与滚动位置、屏宽无关。`ChatPanelContent.vue` 的 `startBusy(kind)` 是**认领制**（已占用则返回 false），因为它和 toast 都是单例——两个动作并发会互相覆盖文案、且先结束的那个会把对方的 toast 关掉。`stopBusy(kind)` 只释放自己认领的那种。
  - 分叉按钮另有就地 spinner（`ChatMessageItem` 的 `.is-forking`）：`forkingMessageId` 从面板透传到消息项，**按 id 匹配**（消息 id 可能是数字、标记是字符串，须 `String()` 归一化）。该按钮是 `disabled` 的，而共享 `:disabled` 规则会把它压到 `--opacity-disabled`——**spinner 本身就是「点击已生效」的反馈，压暗会抵消它的意义**，所以 scoped 规则显式恢复 `opacity:1`（scoped 的 (0,3,0) 压过共享的 (0,2,0)）。
  - `BusyBar` **刻意不做 `prefers-reduced-motion` opt-out**：扫过本身就是信息（唯一区分「在跑」与「卡死」的通道），冻结会留下一个静止的半截条＝读作「传输卡住了」，正是它要消除的歧义；也与 `TransferProgressBar` 的不确定填充、以及走 WAAPI（从不查该偏好）的 `RefreshButton`/底边彗星一致。守卫 `BusyBar.test.ts` 断言该 media query 不存在**且**动画仍在（只断言前者的话，把动画整个删掉也能过）。
- **`prefers-reduced-motion` 必须逐处处理**（没有全局规则）。已处理的参考 `CompletionPopover.vue`、`ChatInputBar.vue`；`flashReducedMotion.css.test.ts` 守住闪烁类。
  - ⚠️ **但「逐处处理」不是绝对的：如果动效承载了信息、不能靠别的东西替代，就不要 opt-out。** 会话行状态槽（`.session-status`）是**刻意的例外**，它**不**响应这个偏好。理由：待审批的「脉动」是第二通道（第一通道是**形状**——盾牌 vs 圆点），冻结后形状仍能区分状态，但会丢掉「该你处理了」的强调；且 36 套主题里有 3 套 `--accent-color` 与 `--color-orange` 相同（色觉障碍读者在**任何**主题上都拿不到色相）。加回那条 media query 之前先读 `SessionList.vue` 里那段注释与 `sessionStatusSlot.css.test.ts` 的守卫。
  - **第二处刻意的例外：推荐回复采纳时的「飞入输入框」动效**（`ChatInputBar.vue` 的 `.recommendation-chip.accepted` / `recommendation-chip-fly`）。飞行**本身**就是信息（「文字被填进了这个框」），没有别的通道承载它——只剩绿色按钮的话，读作「按钮被点了」，正是这个动效要消除的歧义。曾给它加过 opt-out，后果是**所有在系统里关闭动画的用户完全看不到这个功能**（线上 Windows `reduce=true` 实测：与改动前无差异）。`ChatInputBar.test.ts` 的守卫**已反转**为断言该 opt-out 不存在（同 `sessionStatusSlot.css.test.ts` 的先例）；**不要**在未确认飞行不再承载信息的情况下「修复」回去。
  - 一致性也是原因之一：**其余加载指示器都不受该偏好影响**——`RefreshButton` 与底边彗星都走 **WAAPI**（`Element.animate`），而 WAAPI **从不查这个偏好**。所以只在这里 opt-out 会让它成为全站唯一会停的指示器，用户看到的现象就是「为什么只有这个不动」。
- **切换会话的骨架屏**：`.skeleton-block` 是**全局**原语（`web/css/components.css`，含 `skeleton-shimmer` keyframes 与圆形变体 `.skeleton-circle`），两个不同作用域的组件共用它——`App.vue` 的聊天标题栏与 `components/chat/ChatSkeleton.vue`（消息区仿真气泡）。**几何由调用方给**（`width`/`height`/`--skeleton-radius`），原语只管填充与微光。触发条件是**共享的 `switching` ref**（`useChatSession.ts` 导出、`resetChatSessionState()` 里复位）：会话切换期间身份被 `clearSessionIdentity` 清空，标题栏以前会闪出「AI 对话」占位（该兜底文案已从 `useAgents.agentHeaderTitle` 与两个 locale 中**彻底删除**，无 agent 时返回 `''`，由 `v-else-if="currentAgentId"` 门控）。骨架屏**响应** `prefers-reduced-motion`（微光不承载信息，冻结成静态灰块即预期），与 `BusyBar` 的「不 opt-out」相反。守卫：`ChatSkeleton.test.ts`、`chatTitleSkeleton.test.ts`。
- **数据统计的骨架屏**：`components/stats/StatsSkeleton.vue` 复用同一个 `.skeleton-block` 原语，按面板形状给出「总览卡 → 表格行 → 图表」三段（各段由 `cards`/`table-rows`/`chart` 按需关闭）。它**不复用** `.stats-card-panel`——那个类已经在 usage/cloc/git 三个 scoped 块里各有一份，再加第四份只会让漂移更难收；改用自带的 `.stats-skeleton-section`，几何刻意与真卡一致。可见块全部 `aria-hidden`，加载文案由唯一的 `role="status"` 承载（否则读屏用户在加载期间听到的是空白）。行宽是**固定循环**而非随机：微光已经在表达「加载中」，随机宽度会在重渲染时跳动、读作内容先到再变。
  - **触发要延迟一拍**（`composables/useDelayedFlag.ts`，默认 120ms）：统计查询是本地 SQLite 聚合，常在几十毫秒内返回，每次点周期都闪一次灰块比等待本身更扎眼；延迟后只有真正慢的加载才显示骨架屏。源标志变 false 时**立即**清除，不留尾随骨架屏。
  - **骨架屏必须替换掉旧数字**，不能覆盖在旧数字上：用户抱怨的「静默卡住」正是「控件已变、数字没变、也没有任何加载信号」——保留上一周期的数值等于把旧数据冒充新数据。
- **面板首屏骨架屏（`components/common/PanelSkeleton.vue`）**：所有 dock 面板的**首次加载**都用它，不再用居中圆环。圆环只说「在等」，不说「要来的长什么样」，而且会让整个面板塌成一个字形。三种形状 `variant`：
  - `list`（默认）——行 = `[leading 图标块] 主文本 + 副文本 [meta 块]`，`leading`/`meta` 可关。用于文件列表、会话列表、Git 提交、Forge 列表、任务、通知、代理端口、选择器。
  - `grid`——方形缩略图 + 标题，几何对齐 `.file-grid`（`minmax(90px,1fr)`）。用于文件网格与搜索结果网格。
  - `text`——段落行。用于文件预览、diff、分享页正文、OpenAPI 文档。
  - **行宽是固定循环**，与 `ChatSkeleton`/`StatsSkeleton` 同一理由：微光已在表达「加载中」，随机宽度会在重渲染时跳动。
  - **可见块全部 `aria-hidden`**，加载文案由唯一的 `role="status"` 承载。`label` 传现有 i18n 键（`common.loading` / `forge.loading` / …），不要新造文案。
  - **必须铺满容器并左对齐**：`.git-history-loading` / `.forge-loading` / `.task-loading` 等旧容器是 `display:flex` 的**默认横向**容器且 `align-items:center`，直接把骨架塞进去会缩成中间一条窄柱。做法是加 `--skeleton` 修饰类（`align-items:stretch; justify-content:flex-start`），或给骨架一个自带宽度的类。
    - **宽度必须由骨架自己声明（`width:100%`），不能只靠修饰类**：`align-items:stretch` 作用在横向容器的**交叉轴（高度）**上，给不了宽度；而骨架内部所有行宽都是百分比，父宽为 0 时全部归零 —— 实测流水线详情整块量到 **0px**（面板一片空白、看着像没接骨架屏），列表也只剩 leading/meta 的固定宽度（96px / 541px 容器）。守卫 `PanelSkeleton.test.ts` 直接钉源码里的 `width: 100%`（jsdom 不做布局，测不了几何）。
  - **只替换首屏，不替换三种「有内容在场」的加载**：①已有数据的刷新遮罩（文件管理器切目录时的 `dirLoading && displayEntries.length > 0`，旧行必须留在原地）②「加载更多」页脚 ③按钮内联 spinner / 进行中动作（如消息索引的 `jumping`）。这三类继续用 `LoadingIndicator`。
  - 守卫：`PanelSkeleton.test.ts`（形状 / a11y / 固定宽度）与 `panelSkeletonUsage.test.ts`（清单内的每个面板都必须渲染骨架；除白名单外的面板级 `LoadingIndicator` 一律失败；`.skeleton-block` 必须留在全局表且组件内不得重述）。
  - **深色背景上的骨架**：原语的两个颜色是变量 `--skeleton-fill` / `--skeleton-sheen`，`.skeleton-block` **只消费不声明**（写成 `var(--skeleton-fill, <主题兜底>)`）。因此覆盖必须写在骨架的**任意祖先**上，且能生效——变量在声明它的元素上解析，若原语自己声明了这两个变量，调用方写在祖先上的覆盖会静默失效（PDF 预览踩过：深色主题下实测仍是 `--bg-tertiary`）。覆盖时**不要**在调用处重述 `background-image`（那会把微光定义复制一份，两边必然漂移）。目前只有 `PdfPreview.vue` 需要这样做；灯箱用的是自己的 `.lb-loading-spinner`，与骨架原语无关。
- **内容图片的三态占位（等待中 / 已结算 / 加载失败）**：`media-block.css` 的 `pending` 区块 + `localMediaFallback.ts`。一张内容图在样式表里必须能区分三种状态，而标记本身只能表达一种：
  - **pending**——`data-media-settled` 与 `data-media-missing` **都不存在**。`<img>` 在拿到字节前固有尺寸是 0×0，于是整个 `.image-block-wrapper` 先塌成一根 1px 边框、图片到了再「弹」出来。缩略图让这成了常态而非例外：`/api/fs/thumb` 是**按需栅格化**，一张大 PNG 的首次绘制是真实的往返。占位复用 `.skeleton-block` 的同一套语言（`skeleton-shimmer` keyframes + `--skeleton-fill` / `--skeleton-sheen`，**只消费不声明**，理由同上一条）。
  - **settled**——`data-media-settled`，由 `localMediaFallback.ts` 的**文档级捕获**监听打上：`load` 打、`error` 也打（外部图失败没有兜底卡片，但微光必须停——否则它在承诺一个永远不来的加载）。`load` / `error` 都是 DOM 事件，任何渲染管线都看不到，所以只能这样标。监听对**所有** `<img>` 生效，由样式表负责把范围收窄到内容媒体——在这里再写一份选择器清单就是两处必然漂移。安装时还要**补扫一次** `complete && naturalWidth > 0` 的图（`load` 只发一次，命中缓存的首屏图片会永远停在 pending）；只看 `complete` 不够，它对视口外/无 src 的图也为真。
  - **missing**——`data-media-missing` + `.local-media-hidden` + 共享的 `MediaLoadError` 卡片（见下文）。
  - 三个消费面：`.image-block-wrapper` 里的图（聊天 / 文档 / 分享 / 表格弹窗）、全屏图片查看器（`.image-preview-img`）、代码链接预览卡（`.code-preview-media-img`）。**文件管理器缩略图与附件小图刻意不在内**——它们是 40dp 图标且已有自己的失败兜底，微光只会是噪音。
  - **`min-width` 用定长、不用百分比**（figure 那一支）：figure 是 shrink-to-fit 包着图片的，百分比会重新引入 figure↔media 循环，把替换元素压回 300px 默认宽。两个单图面板相反，容器由 flex 给了确定宽度，`min(…, 100%)` 才不会在窄分栏里溢出。
  - **隐藏靠 `backwards` 填充的 0s 延迟 keyframe，不用基础 `opacity: 0`**：HTML 导出会序列化命中规则但只带少数几个具名 keyframes，基础 `opacity: 0` 一旦被导出而 keyframes 没跟过去，导出的文档里**每张图都是隐形的**；这样写缺 keyframes 时退化成「可见、无微光」。延迟 **200ms** 是刻意的：本地图片通常几毫秒就到，闪一帧的占位比没有更糟；延迟期间框是透明的但仍**占位**，布局只落一次。
  - **换 `src` 必须清掉 `data-media-settled`**：`useMediaWatch.patchImagesForPath`（后台重写同一路径）与两个 Vue 查看器的 `:key`（切到同目录另一张图）。不清的话新图在途时仍显示旧帧像素、且没有任何「在加载」的迹象。导出的静态文档没有 `load` 监听，由 `exportMarkdownHtml.ts` 无条件补上该标记，否则导出件看起来全是空的。
  - 守卫：`localMediaFallback.test.ts`（打标 / 补扫 / 可逆）、`localMediaFallback.css.test.ts`（不用基础 opacity、排除 missing、复用变量、reduced-motion 只停微光、定长、覆盖两个面板）、`useMediaWatch.test.ts`（清标只作用于被 patch 的图）、`exportMarkdownHtml.test.ts`（导出必带标记）。
- **分隔条的折叠手柄：离开是「先变半透明、停一会儿、再淡出」两步**（`SplitDivider.vue`）。三档：
  1. **静止**：完全不可见（`opacity: 0` / `pointer-events: none`），触屏同理（点击细线才 `--revealed`）。
  2. **活跃**：`:hover` / `:focus-within` → `opacity: 1`。
  3. **离开**：指针移开后挂 `--lingering`，先降到 `opacity: var(--opacity-soft)`（0.7）并**保持 `HANDLE_LINGER_MS`（1000ms）**，然后清类，由基规则的 `opacity: 0` + `--duration-base` 过渡**淡出到消失**。

  两步是关键：单段 1 → 0 的 150ms 淡出会在用户（站得稍远）还没移到按钮上时就把目标抹掉。中间那档半透明既是「正在退场」的视觉信号，也是**仍然可点**的窗口——所以 `--lingering` 的 `pointer-events` 是 `auto`；可见却点不动才是真的坏。**不要把它做成静止常驻**：把 0.7 写成 `(hover: hover)` 下的基规则，结果就是「鼠标没碰过也一直悬浮」。只是横穿分隔条（停留不到 `HOVER_DWELL_MS` 200ms）不进入离开序列，避免闪一下。守卫：`SplitDivider.test.ts` 的 `leaves in two steps`、`holds the dimmed state for the whole grace period, then hides`、`keeps the handles invisible and inert at rest`。
- **折叠手柄的图形是「面板 + 内箭头」，不是裸箭头；按钮就是图标本身**。裸 chevron 会被读成拖拽把手（滚动条/滑块的图形），而这里做的是隐藏整个窗格。用的是 lucide `panel-{left,right,top,bottom}-close` 的形状（圆角矩形框 + 窗格分隔线 + 指向被隐藏窗格的 chevron），**以 inline SVG 内联**而不是 `import` lucide 组件——本文件挂在 `SplitView` 下，多个测试对 `lucide-vue-next` 做了窄 mock，新 import 会拿到 `undefined` 直接崩渲染。**不得再包圆盘**（曾经的 28px 圆形 + `color-mix` 底色 + 1px 环）：它读作浮在窗格上的一个 UI 芯片，还跟旁边的分隔线抢视觉。现在 `background: none` / `border: 0` / `box-shadow: none`，`svg` 占满按钮盒（`100%`），所以「按钮尺寸 = 图标尺寸」，没有内边距在下面垫出一个面。**尺寸要按「画出来的框」算**：viewBox 里 rect 只占 18/24（75%），盒子给 32dp 才等于把原来的 12dp 图形放大一倍（32×0.75=24dp），按 24dp 给盒子等于没放大。**离窗格内容的间距**是 `min(var(--space-4), 12px)`（8dp，cap 12px）——原来是 4dp/6px，40px 的图形贴边会挤到内容第一行。**没有底色就没有承载对比度的面**，靠 `--text-primary` 描边 + 两圈 `drop-shadow(0 0 2px/1px var(--bg-secondary))` 光晕把笔画从窗格内容里分离——光晕必须取**窗格色**，36 套主题明暗各半，固定黑/白会有一半主题直接消失。
- 非 CSS 动效：running 彗星走 WAAPI 指令 `directives/runningSweep.ts`（1500ms，`cubic-bezier(.45,.05,.55,.95)`，与文档时间轴相位锁定）。
- **会话行底边只有一层效果**：3px 平轨道 + 38% 彗星（`--running-track` / `--running-comet` / `--running-head`）。**不要再叠第二层**——曾经是「14px 带 mask 的光晕 + 80% 扫过光带」两层，看起来像两个效果打架、且光晕把光带糊成环境光。待审批时彗星停止并变成整条琥珀呼吸（`--pending-track` / `--pending-comet`）。**被阻塞的行必须换一个不带指令的元素**：指令用 WAAPI 写 `transform`，优先级高于普通 CSS `transform`，同一元素无法靠样式停下。守卫：`runningSweepTheme.css.test.ts`。
- **会话行状态槽（`.session-status`）用「形状」为主、「动效/颜色」为辅区分状态**：待审批＝**盾牌图标**（lucide `ShieldAlert`，原地脉动）、未读＝单点完全静止的圆点。**审批/权限相关的图标统一为盾牌**（同一盾牌还用在通知中心待审批事件芯片、聊天底栏模式芯片与群聊自动批准芯片、模式切换弹窗的 Mode 页签、权限结果芯片的 `ShieldCheck`/`ShieldX`、`PermissionApproval` 工具图标）。形状领先于色相的理由是可测量的——36 套主题里有 3 套（ayu-light / gruvbox-light / gruvbox-dark）的 `--accent-color` 与 `--color-orange` **完全相同**，色相本就无法承载区分；且色觉障碍读者拿不到色相信息。待审批盾牌用 `color: var(--status-dot-pending)` 着色（`currentColor` 描边）；未读仍是 14px 槽内的 8px `radial-gradient` 填充（`--status-dot`）；两者**都不得用 `border`**——那正是已移除的环的画法。**运行中在槽里什么都不显示**：底边彗星已经在表达「正在推进」，右侧再放一个环是同一事实说两遍（这也是槽原先唯一需要 `border` 的原因）；且槽是 `v-if` 的，无状态即不渲染元素，不占宽度（「没有东西展示时该空间不要占地方」）。因此**没有 `--running-ring` / `--running-ring-track` 这两个 token，也不得有 `.session-status.is-running` 规则或 `session-status-spin` keyframes**（守卫显式断言它们不存在）。优先级 pending > unread（`rowStatus()`）；运行中 + 有未读的行会显示未读点，因为彗星与点走两条通道。被阻塞时底部彗星停止并变为整条琥珀呼吸，且**必须换一个不带指令的元素**（`v-if`/`v-else` 两个 `<i>`）——指令用 WAAPI 写 `transform`，`transform:none` 压不过它。**槽必须与标题保持间距**：`.session-status` 自带 `margin-left: var(--space-4)`，同时 `.session-item` / `.cross-session-item` 的右内边距收到 `var(--space-4)`（其余三边仍是 `var(--space-6)`）——原先 12px 右内边距 + 无 margin 会让槽的左边缘**正好落在标题省略号边界上**（线上实测 0px），而槽与 ⋮ 之间却空着 ~10px；现在两侧各约 11px。守卫：`sessionStatusSlot.css.test.ts`。

---

## 七条红线（先读这节再写 CSS）

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

**`app-region` 命中取 DOM 顺序最后一个，与绘制顺序/z-index/`pointer-events` 无关。** Chromium 对某点求 `-webkit-app-region` 时取 DOM 顺序中**最后**覆盖该点的元素。桌面登录页的真实事故：`<body>` 是 `drag`，两个全屏装饰层 `.bg-gradient` / `.bg-grid` 继承 `drag` 且原本排在 `.splash` / `#versionGate` **之后**，于是吃掉整个视口的点击——版本 gate 的「仍然继续」「下载」与 splash 的「取消连接」全部无响应，而 `elementsFromPoint` 仍报告命中的是按钮本身（骗人）。**`z-index` 与 `pointer-events:none` 都不豁免**。症状不对称是判据：排在装饰层之后的 `.window-close`、登录表单正常。修法是把装饰层排到最前，交互浮层一律排其后（像素 diff = 0）。验证必须真实 OS 点击（Xvfb + openbox + XTEST），`elementFromPoint` 会误报。

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

- **合成分层只有一个分界，且不在会变高的元素上**。Android WebView 的合成器有个缺陷：被提升为独立合成层的元素**自身尺寸变化**时，它不重绘增量区域，而是把旧纹理拉伸后画进去。所以「给刚出问题的元素加 `will-change`」是**反向的**——那个元素恰恰是回合结束时在变高的东西，补丁本身制造了一块会 resize 的纹理。项目追了几周的残影（跨消息的元数据文字、Dock 橙色圆盘、气泡下 9px 的 `--bg-tertiary` 带、操作条下方 4px 被纵向压扁的「耗时 · 时间」副本）全是这一个失败。
  正解是结构性的：**每个尺寸稳定的容器一个层边界，内部零提升**。唯一分界在 `.chat-panel-content`（App 模式，`will-change: transform` + `contain: paint`）——它的高度是窗格高度，回复结束、操作条出现、工具卡展开时都不变，因此这一层不在触发路径上 resize。**聊天子树内一律不得再加 `will-change` / `backdrop-filter`**（`.chat-message` 只用 `contain: paint` 做跨行隔离，不再提升；`.bottom-dock-wrapper` 同样只裁剪）。唯一例外是行内表格复制菜单打开时（`:has(.table-block-copy-menu.is-open)`）临时提升，因为 `contain: paint` 解除后仍需一个 fixed 包含块，否则菜单会跳到视口。全部只在 `[data-app-mode]` 下生效：桌面浏览器没有该 bug，无条件提升会让文字失去次像素抗锯齿。
  排查时**判据是颜色归属**——残影色等于气泡底色 ⇒ 自身层溢出、等于 Dock 色 ⇒ 兄弟层、等于背景色 ⇒ 根本没重绘。守卫：`chatCompositingBoundary.css.test.ts`（分界存在且只在 App 模式、子树内除菜单例外外无 `will-change`/`backdrop-filter`）、`chatRowLayerIsolation.css.test.ts` 与 `bottomDockLayerIsolation.css.test.ts`（只裁剪、**不**提升——它们断言 `will-change` 不存在，删掉提升才是修复）。
- **表单控件行高必须是整数 px**：`--input-line-height: 18px`。textarea 文字顶对齐，内容盒高度恰是一行时没有余量；Android WebView 会**独立地**四舍五入行盒和内容盒高度，CJK 字体升降部更大，分数行高下两次舍入不再抵消，光标会明显偏高。桌面 Chrome 恰好舍入一致，所以**只在 WebView 复现**。
- **畸形表单标签要重置 `white-space`**：DOMPurify 会保留真实 `<option>` / `<optgroup>` / `<select>`，而 UA 样式给 `<option>` 加了 `nowrap`，会静默撑破气泡（`overflow:hidden` 裁掉）。`ChatMessageItem.vue:641` 逐标签重置。

### 红线 7：`scrollbar-color` / `scrollbar-width` 会让 Chromium 弃用 `::-webkit-scrollbar-*`

只要给某个元素（或 `*`）设了**标准属性** `scrollbar-color` 或 `scrollbar-width`，Chromium 121+ 就**整体忽略**该元素的全部 `::-webkit-scrollbar-*` 规则——包括 `::-webkit-scrollbar-button { display: none }`。表现：自定义的细滚动条**退回 15px 原生条**，并且**上下箭头按钮复活**（实测 headed Chrome：17px 带箭头 vs 6px 无箭头）。

原先把 `* { scrollbar-color: … }` 写成了无门控的全局规则，于是紧跟其上的 webkit 细条与 `-button` 规则全部失效——**箭头就是这么冒出来的**。

正解是把标准属性**只留给没有 webkit 伪元素的引擎**（Firefox）：

```css
@supports not selector(::-webkit-scrollbar) {
    * { scrollbar-color: var(--scrollbar-thumb, #c1c1c1) var(--scrollbar-track, transparent); }
}
```

**唯一定义在 `web/css/scrollbar.css`**。四个消费方各自内联它——`web/css/base.css` 用 `@import`（同时覆盖 share SPA），`src/utils/swaggerHtml.ts`（Swagger 预览 srcdoc）、`src/utils/exportMarkdownHtml.ts`（导出 HTML）用 `?raw` 内联，`src/utils/htmlPreview.ts`（HTML 文件预览）用 `?raw` 注入预览文档。**为什么用文件而不是 `--scrollbar-size` token**：后三者是独立文档（iframe srcdoc / `file://` 导出 / 用户 HTML），看不到 app 的 `:root`，token 在那里是 undefined ⇒ 宽度声明整条消失 ⇒ 静默退回原生条。只有内联同一份文件才算真正收敛。每个文档自己的配色走 `--scrollbar-thumb` / `--scrollbar-track` 两个变量（**必须带 fallback**，否则在独立文档里同样失效）。

**两档宽度，按输入设备而非宿主/视口**：`(pointer: coarse)`（触屏/笔）**4px**（移动端原样），其余（鼠标/触控板）**10px**。**必须是整数 CSS 像素**：Chromium 会把计算出的条宽截断到整像素（实测 9.6px 只画出 9px），所以「比 8px 加 20%」只能落成 10px——写 9.6px 会静默吃掉这次加宽。选 `(pointer: coarse)` 是因为它是唯一能随样式表走的判据——iframe 与导出文档既无宿主信息也无可靠视口宽度，而媒体特性由浏览器在各文档内各自求值；它也是分割条触屏感应区用的同一条轴。守卫 `__tests__/scrollbarNoArrowButtons.test.ts` 拒绝任何重新引入的分档：任何一份文档重写 `::-webkit-scrollbar { width }`、出现 `--scrollbar-size`、变量缺 fallback、或少了 coarse 覆盖都会失败。

**HTML 文件预览套用同一套逻辑**：预览文档按 URL 加载（独立文档，链接不到 app 样式表），`load` 时由 `installPreviewScrollbar` 注入共享样式表。**插在 `<head>` 最前**（不是 append），因此预览文件自己写的滚动条规则（内联 / `<style>` / 外链）在源码顺序上更靠后、**优先级更高**——契约是「仅在预览文件未自定义滚动条时生效」，预览不该覆盖被预览文档自己的样式。注入是幂等的（按 `data-clawbench-scrollbar` 标记查重），两个预览面（全屏 FileViewer 与代码链接预览 `HtmlPreviewBody`）都接了 `@load`。

**预览的滚动条要连配色一起套，否则只是「同款几何、别人的颜色」**：共享文件里的 `var(--scrollbar-thumb, …)` 在预览文档里取不到 app 的 `:root`，只剩中性灰 fallback。因此 `readHostScrollbarPalette()` 从宿主 `getComputedStyle(documentElement)` 读出三个值并**烘进注入的样式表**：

| 槽位 | 取自 | 为什么 |
|---|---|---|
| 滑块 | `--scrollbar-thumb` | 与 app 同一个 token |
| 悬停 | **`--text-muted`** | 共享文件的 hover 规则读的就是它（不是 `--scrollbar-thumb-hover`），照抄才能同款变亮 |
| 轨道 | **`--bg-primary`**（面板底色） | 用户明确要求：滚动条那一列属于 app 表面，不跟随被预览网页自己的底色 |

配色写成**具体色值规则**追加在共享几何之后，而不是在预览文档的 `:root` 上设同名变量：被预览页面可能自己定义 `--scrollbar-thumb` / `--text-muted`，设变量要么被它的 `:root` 按源码顺序压掉，要么连带改掉页面里读到同名变量的其它元素。Firefox 的 `scrollbar-color` 门控要在配色段里**再写一遍**（共享段那份带的是 fallback 变量）。

**注入必须是「重写」而非「查重后早退」**：色值是解析后的具体值，早退会让主题切换永远停在旧配色。`installPreviewStyle` 因此对已存在的 `style[attr]` **原地改 `textContent`**（保留原位置——重新 insert 会改变它和被预览文档自身规则的胜负关系）。配套 `watchPreviewTheme(getIframe)` 监听 `clawbench-theme-change`，同时重刷滚动条与查找高亮（后者也内联了 accent）；两个预览面都在 `@load` 里重新注册、卸载时 dispose（重新注册前先 dispose，否则每次 reload 叠一个监听）。

> 验证必须 headed（headless 的 overlay 滚动条量不到 gutter）：实测注入后 gutter=8px、滑块像素 `#928374`、轨道 `#282828`，无箭头。

**分割条侧的冲突解法**：拖拽分隔条的高亮**只能是那条细线本身**——host 从 1px 长到 2px（`margin: ±0.5px` 保持外footprint不变），共享的 `.resize-divider__line` 填满它并染成 `--accent-color`。**不得再画半透明/不透明色带**：色带以分隔条为轴跨到两侧窗格上，而窗格紧贴分隔条处就有滚动条，色带会把它盖住。可点击感应区（`::before`）在鼠标面为分隔条两侧各 4px（共 9px），触屏面（`pointer: coarse`）仍是各 12px。守卫 `resizeDivider.css.test.ts` 断言共享文件里**没有** `color-mix`、没有 `calc((100% - 2px) / 2)` 居中偏移，且两个消费组件的触发态规则块**不得**声明 `background`。

> Firefox 只有 `auto | thin | none`，没有 px 控制，所以统一用 `scrollbar-width: thin` 近似（放在已有的 webkit 门控里）。

> 验证必须在 **headed** 浏览器里看（headless 用 overlay 滚动条，量不出宽度也画不出箭头）。

---

## 改动检查清单

### 任务详情页卡片（`.overview-card` 等）

任务详情页叠了好几张卡（提示词预览、执行计划/事件触发、门控脚本、事件上下文），
形状**只在 `web/src/assets/task-overview-card.css` 定义一次**：`.overview-card`、
`.card-title`、`.card-icon`、`.card-title-text`、`.card-toggle-btn`、`.card-chevron`，
可折叠标题加 `.card-title.is-collapsible`、箭头收起态加 `.is-collapsed`。

- **必须全局**：这些类原先在 `TaskOverviewTab` / `TaskScheduleCard` / `TaskEventCard`
  各自的 `<style scoped>` 里逐字重复三份。新加的 `TaskScriptCard` 只用了类名却没声明，
  而父组件的 scoped 规则带 `[data-v-x]`、**永远匹配不到子组件根元素** ⇒ 那张卡完全
  没样式（无背景/边框/内边距），和旁边的提示词卡长得完全不一样。
- 消费方只保留自己的布局（如 `.script-body` 的 `padding-top`），**不得**在 scoped 块里
  重声明上述选择器。守卫 `taskOverviewCard.css.test.ts` 同时钉「只在全局定义」
  「scoped 不得重复」「四个消费组件都必须 import 该 css」。
- 守卫注意：判 import 必须匹配 **import 语句**（`/^\s*import\s+['"]@\/assets\/…['"]\s*$/m`），
  不能只 `toContain(文件名)`——好几个文件在**注释里**提到该路径，删掉真 import 后
  注释仍会让断言通过（实际踩过）。

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
- [ ] 处理 `prefers-reduced-motion`——**除非该动效承载信息且无法替代**（如会话行状态槽，见「动效」一节）
- [ ] 闪烁/跳转类加源码守卫测试

### 任何 UI 改动

- [ ] 只用了 token，没有魔法数字（尤其颜色和 z-index）
- [ ] 几何尺寸写成 `calc(N * var(--dp))`，**不是**裸 px（否则该元素不跟随界面缩放）；发丝线除外，保持字面 `1px`
- [ ] **在浅色和深色主题下各看一眼**（至少 `github-light` + `github-dark`）
- [ ] 没碰 `--font-size-xl` / `--input-line-height` 的语义
- [ ] 新增 `.app-container` 直接子元素已补进 `base.css` 的 `z-index:1` 规则
- [ ] 纯前端改动跑 `npm run build`

---

## 守卫测试索引

这些是「设计系统契约」的可执行形式。改到相关区域时它们会先失败——**先读测试里的注释**，那里写着当初为什么这么定。

| 测试 | 守什么 |
|---|---|
| `components/common/__tests__/designTokens.css.test.ts` | token 设计值、主题块完整性、未定义 token 引用、窗口控件对比度配方 |
| `__tests__/fontDrivenScaling.test.ts` | 根字号是唯一缩放入口（`--dp` 唯一声明、html 不得用 `--font-size-*`）、全仓无 `zoom`、Electron 不再走 `setZoomFactor`、token 都走 `--dp`、发丝线不被缩放、lucide 属性选择器齐备、宽屏断点用有效宽度且监听缩放事件、**全仓 `<style>`（含 `src/assets/*.css`）无裸 `font-size: Npx` / `letter-spacing: Npx`**、`designToCss` 唯一实现且被各阈值比较点消费 |
| `components/common/__tests__/countBadge.css.test.ts` | 角标几何全局唯一、scoped 未重加圆角 |
| `components/common/__tests__/wrapCheck.css.test.ts` | 菜单「该项当前是开的」勾选 `.wrap-check` 全局唯一、两个菜单共用同一 class |
| `components/chat/__tests__/quickSendMenuStickyTitle.css.test.ts` | 快捷发送 / 快捷指令弹层标题 `.quick-send-title` 在 `components.css` 全局声明（sticky + 不透明 `--bg-secondary` + `z-index`），两个宿主不得各自重声明，且两个菜单的 `:max-height` 一致且 ≥400 |
| `components/common/__tests__/modalFooterBtn.theme.css.test.ts` | `.fbtn` 深色主题前景提亮 |
| `assets/__tests__/modalWidth.css.test.ts` | **宽度只有两档**：`--modal-width-narrow/wide` 各一份且窄 < 宽、`modal-card.css` 只把修饰类映射到 token、全仓组件**不得**自设 `--modal-max-width` 或给弹窗外壳写裸 `max-width` 数字 |
| `components/common/__tests__/wallpaperSurfaceTransparency.css.test.ts` | 壁纸开启后各表面透明度不叠乘 |
| `components/common/__tests__/wallpaperBlurCost.css.test.ts` | 壁纸模糊不留 `will-change` |
| `components/common/__tests__/resizeDivider.css.test.ts` | 拖拽分隔条外观 |
| `components/chat/__tests__/chatContentMeasure.css.test.ts` | 聊天区阅读栏：度量与 container query 阈值换算（`60rem` = 900 设计 px）、三个 gutter 的 floor（消息列 `0px` / 输入区 `--space-4` / 卡片 `--space-5`）、消费点不得自己重算半宽公式、留白花在滚动容器的 **padding**（用 margin 会把滚动条从会话区边缘拽到消息列边上）、**滚动条宽度只能运行时测**（右侧内边距减去 lane、左侧不减、CSS 不得硬编码、`scrollbar-gutter: stable` 必须在）、`container-type: inline-size` 且不是 `size`、形状随留白翻转（通宽直角色带 + 扁平条 / 有留白圆角卡片 + 只圆上两角的 dock 岛）、岛底色必须是 text 混色且带 `--panel-alpha`、标题栏不收窄 |
| `components/chat/__tests__/chatCompositingBoundary.css.test.ts` | 聊天树里只有一个合成分界（尺寸稳定的 `.chat-panel-content`），内部禁止 `will-change`/`backdrop-filter`（仅表格菜单例外） |
| `components/chat/__tests__/chatRowLayerIsolation.css.test.ts` | 聊天行在 App 模式下只 `contain: paint`、**不**提升为合成层 |
| `components/__tests__/bottomDockLayerIsolation.css.test.ts` | 底部 Dock 包装层在 App 模式下只裁剪、不提升，且只在 `[data-app-mode]` 下 |
| `__tests__/forgeDockTabTitleWiring.test.ts` | forge 页签标题按绑定平台取值、三条标题路径（溢出菜单项 / 内联 tooltip / 宽屏 Dock tooltip）都接线、标签与图标永不矛盾 |
| `testUtils/__tests__/designLength.test.ts` | `designPx()` / `normalizeDp()` 本身：`calc(N * var(--dp))` 与裸 `Npx` 等价、属性名边界不被 `min-width` 满足 |
| `components/file/__tests__/flashReducedMotion.css.test.ts` | 闪烁动效遵守 `prefers-reduced-motion` |
| `components/common/__tests__/BusyBar.test.ts` | 长动作进度条存在且动画在、**无** reduced-motion opt-out（扫过即信息）、不吞指针事件 |
| `components/common/__tests__/sharedRingUnification.test.ts` | 加载环全局唯一、调用方不得重述形状、**全仓只有一个 `--li-border` 比例**（走查源码）、覆盖类必须带祖先 |
| `components/common/__tests__/spinnerUnification.test.ts` | 迁移到 LoadingIndicator 的 6 处保留各自 `--li-color`、不再自带 animation/keyframes、`size` 用档位而非数字 |
| `components/common/__tests__/PanelSkeleton.test.ts` | 骨架屏三种形状、`leading`/`meta` 可关、可见块 `aria-hidden` + 唯一 `role=status`、行宽固定循环 |
| `components/common/__tests__/panelSkeletonUsage.test.ts` | 清单内的面板首屏必须渲染 `PanelSkeleton`；白名单外的面板级 `LoadingIndicator` 失败；`.skeleton-block` 必须留在全局表且组件内不得重述 |
| `components/chat/__tests__/chatPanelBusyWiring.test.ts` | `startBusy` 认领制（不抢占）、各自只释放自己的 kind、BusyBar 已挂载、卸载清 ticker |
| `components/file/__tests__/dockedPaneStacking.css.test.ts` | 停靠预览窗格层级 |
| `components/forge/__tests__/forgeDetailChrome.css.test.ts` | forge 面板 chrome 全局 |
| `components/git/__tests__/gitHistoryChrome.css.test.ts` | git 历史 chrome 全局 |
| `components/settings/__tests__/settingsRowTypography.css.test.ts` | 设置行字号层级 |
| `components/settings/__tests__/skillsCardsStyles.css.test.ts` | 技能设置页两张卡的 chrome 全局唯一（scoped 不得重复） |
| `components/chat/__tests__/permissionResultChip.css.test.ts` | 权限审批卡片：label 吸附命令框、按钮一体化、结果芯片（自动批准徽标与「已批准」同规格） |
| `components/settings/__tests__/settingsHeaderAlignment.css.test.ts` | 设置页头部对齐 |
| `components/settings/__tests__/sliderResetResident.css.test.ts` | 滑块重置按钮常驻 + 灰显（不按当前值出现/消失） |
| `components/settings/__tests__/settingsControls.css.test.ts` | 设置控件形状全局唯一（开关/滑块/重置）、尺寸对齐 30px、scoped 不得重加几何、按钮复用 `.fbtn` |
| `components/task/__tests__/taskOverviewCard.css.test.ts` | 任务详情页卡片 chrome 全局唯一（scoped 不得重复）、四个消费组件都必须 import |
| `utils/__tests__/codeHighlightStyle.test.ts` | CodeMirror 语法高亮映射全局唯一（不得在组件里重复 `HighlightStyle.define`）、颜色必须来自 CSS 变量 |
| `components/__tests__/wideDockIconSize.css.test.ts` | 宽屏 dock 图标尺寸 |
| `assets/__tests__/themePicker.css.test.ts` | 主题选择器中性底 + 色点载体 |
| `utils/__tests__/themeReveal.test.ts` | 主题圆形扩散（百分比 clip、reduced-motion 回退、in-flight 守卫）+ **三个宿主共用 `useThemePicker`**：切换只在 `after-leave` 生效、必须走 `setLocalConfig`、原点取按钮；宿主不得自己调 `applyThemeWithReveal` |
| `utils/__tests__/themeBootstrap.test.ts` | `index.html` / `share.html` 的内联主题脚本与 registry 同步：`DARK_IDS`、`SB_COLORS`、出厂默认值，且**两页语义一致**（新设备落盘 `gruvbox-dark`、`auto` 不落盘、旧 `light`/`dark` 迁移） |
| `share/__tests__/shareTheme.test.ts` | 分享页主题变更的两条副作用：响应式主题 ref（provide 给 iframe 查看器）+ mermaid 重渲染；不得自己再刷一遍属性 |
| `share/__tests__/ShareThemeButton.test.ts` | 分享顶栏选择器：行数 = registry + auto、选中行跟随存储值、点击只关菜单、`after-leave` 才落 `setLocalConfig` |
| `share/__tests__/shareMode.test.ts` | `shareDocUrl`/`encodeShareDocDir` 与 Go 侧 base64url 同字节（无 `=`/`+`/`/`）、非 ASCII 目录经 UTF-8 往返一致——不一致会让分享页每个相对资源 404 |
| e2e `share-page.spec.ts`「Shared HTML document」 | 分享的 HTML 以真实 URL 加载（`text/html`）、同目录样式表 `text/css` 且 200、所有资源同一 `dirKey`、iframe 仍是不透明源且 sandbox 无 `allow-same-origin` |
| `assets/__tests__/annotationButtons.css.test.ts` | 标注按钮全局作用域 |
| `__tests__/scrollbarNoArrowButtons.test.ts` | 滚动条唯一定义在 `css/scrollbar.css`（四文档内联：应用/OpenAPI/导出/HTML 预览）、两档宽度按 `(pointer: coarse)`（移动 4px / 桌面 10px，须整数像素——Chromium 截断小数宽度）、`scrollbar-color` 必须关在 `@supports not selector(::-webkit-scrollbar)` 里（否则 Chromium 弃用 `::-webkit-scrollbar-*`，箭头按钮复活）、任何文档不得重写宽度、变量必须带 fallback、HTML 预览注入须插在 head 最前且幂等、预览配色须从宿主读（thumb/`--text-muted`/`--bg-primary` 三槽 + Firefox 门控）且两个预览面都接主题监听 |
| `__tests__/platformAxes.test.ts` | 三条平台轴保持正交：`isPC` 不得回归、共享宿主谓词不得各组件重声明、iPadOS 陷阱（Macintosh UA + `maxTouchPoints>0`）不得重开 |

**写新守卫时注意两个坑**（都实际栽过）：
1. **jsdom 不解析 `var()` 和 `color-mix()`**——`getComputedStyle` 会把 `var(--x)` 原样返回。所以 token 类断言必须**读源码**，不能读计算样式。
2. **一个 `.vue` 文件可以有多个 `<style>` 块**，且**选择器可能是逗号组**——源码嗅探要 `matchAll` + `split(',')`，否则漏匹配。
