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

### 宽屏 vs 窄屏

- 阈值（`useWideScreenLayout.ts:8`）：CSS 宽度 ≥1024px，**或**物理宽度 ≥1280px **且**横屏。
- 物理宽度那条是为了高分辨率平板：2400 物理 px / DPR 2.5 = 960 CSS px，只看 CSS 宽度会漏判。
- 横屏判定用 `screen.width/height` 而非 `window` 内尺寸，防止 Android 软键盘 `adjustResize` 把竖屏平板抖成宽屏。
- 生效方式是给 `.main-content` 加 `.wide-screen` 类，`wide-screen.css` 切成 `flex-direction: row`。
- **默认分屏比例是 4:6（左侧稍窄）**，常量 `DEFAULT_RATIO` 在 `utils/splitRatio.ts`——composable 初值与 `resetWideScreenState` 都引用它，不要把 0.4 散落成魔数。用户已持久化的自定义比例不受影响（localStorage 优先）。

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
- **溢出的按钮条要支持拖拽横向滚动**：聊天 Action Bar 的按钮在窄窗格下会溢出，而滚动条是隐藏的——普通鼠标滚轮只能滚页面，够不到被挡住的按钮（触控板横滑与触摸拖拽本来就能用，只有鼠标不行）。`utils/dragScroll.ts` 在**真正溢出时**才挂载（放得下就不拦截按压、也不显示抓手光标），按下并左右拖动即滚动；形态沿用 `dragClickGuard`（独立 util + 返回 disposer + 组件挂载）
- 自定义品牌图标走 `AgentIcon.vue` / `ProviderIcon.vue`；单色图标配色在 `mono-icon-colors.css`，深浅主题各一套。

---

### 加载环（`.li-spinner`）

**全站唯一实现**，声明在 `css/components.css`（**必须全局**——工具调用卡 / mermaid 是 `v-html`/`innerHTML` 注入、localhost 按钮曾是 `::after`，这些 DOM 无 `data-v-*`，scoped 规则永远匹配不到，见[红线 1](#红线-1v-html-注入的内容匹配不到-scoped-规则)）。

- **两个入口**：组件 `<LoadingIndicator>`（渲染 `.li-spinner`，覆盖 72 个文件）或裸 `<span class="li-spinner">`（注入 HTML 用）。组件只拥有外壳布局（`.loading-indicator`），环的形状**不在** scoped 块里。
- **尺寸**：组件档位 `size="sm|md|lg"` = 14 / 28 / 36px。**`size` 是字符串枚举**——传数字（`:size="13"`）会生成不存在的类名并**静默回退 28px**。非档位尺寸（20px / 10px / 8px）的调用方自己在环元素上设 `--li-size`。
- **粗细由比例推导，全站只有一个比例**：`--li-border: calc(var(--li-size) / 6)`。**调用方只能设 `--li-size`，绝不要写死 `--li-border`**——写死会静默偏离比例，且不对比两个环根本看不出来。守卫 `sharedRingUnification.test.ts` 会走查全部源码，出现字面量 `--li-border` 即失败。
- **颜色**：`--li-color`（转动的弧）/ `--li-track-color`（静止底环），都回退主题 token。原 SVG 是 `stroke="currentColor"` 的站点用 `--li-color: currentColor` 承接，外观零变化。
- **速度**：默认 `--li-duration: 0.8s`；从旧实现迁移来的站点（mermaid / tool-call / url-btn）保留各自的 `0.6s`，**统一形状不等于统一节奏**。
- 覆盖类规则**必须带祖先部分**（如 `.mermaid .mermaid-spinner`）：`.li-spinner` 是单类 (0,1,0)，写成单类会与之打平，胜负取决于打包 chunk 顺序 → 尺寸会随构建**静默回退 28px**。

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
  - ⚠️ **但「逐处处理」不是绝对的：如果动效承载了信息、不能靠别的东西替代，就不要 opt-out。** 会话行状态槽（`.session-status`）是**刻意的例外**，它**不**响应这个偏好。理由：冻结会**合并状态**——「待审批」（脉动点）与「未读」（静止点）形状尺寸完全相同，只靠脉动与色相区分，冻结后只剩色相，而 36 套主题里有 3 套 `--accent-color` 与 `--color-orange` 相同（色觉障碍读者在**任何**主题上都拿不到色相）。加回那条 media query 之前先读 `SessionList.vue` 里那段注释与 `sessionStatusSlot.css.test.ts` 的守卫。
  - **第二处刻意的例外：推荐回复采纳时的「飞入输入框」动效**（`ChatInputBar.vue` 的 `.recommendation-chip.accepted` / `recommendation-chip-fly`）。飞行**本身**就是信息（「文字被填进了这个框」），没有别的通道承载它——只剩绿色按钮的话，读作「按钮被点了」，正是这个动效要消除的歧义。曾给它加过 opt-out，后果是**所有在系统里关闭动画的用户完全看不到这个功能**（线上 Windows `reduce=true` 实测：与改动前无差异）。`ChatInputBar.test.ts` 的守卫**已反转**为断言该 opt-out 不存在（同 `sessionStatusSlot.css.test.ts` 的先例）；**不要**在未确认飞行不再承载信息的情况下「修复」回去。
  - 一致性也是原因之一：**其余加载指示器都不受该偏好影响**——`RefreshButton` 与底边彗星都走 **WAAPI**（`Element.animate`），而 WAAPI **从不查这个偏好**。所以只在这里 opt-out 会让它成为全站唯一会停的指示器，用户看到的现象就是「为什么只有这个不动」。
- 非 CSS 动效：running 彗星走 WAAPI 指令 `directives/runningSweep.ts`（1500ms，`cubic-bezier(.45,.05,.55,.95)`，与文档时间轴相位锁定）。
- **会话行底边只有一层效果**：3px 平轨道 + 38% 彗星（`--running-track` / `--running-comet` / `--running-head`）。**不要再叠第二层**——曾经是「14px 带 mask 的光晕 + 80% 扫过光带」两层，看起来像两个效果打架、且光晕把光带糊成环境光。待审批时彗星停止并变成整条琥珀呼吸（`--pending-track` / `--pending-comet`）。**被阻塞的行必须换一个不带指令的元素**：指令用 WAAPI 写 `transform`，优先级高于普通 CSS `transform`，同一元素无法靠样式停下。守卫：`runningSweepTheme.css.test.ts`。
- **会话行状态槽（`.session-status`）用「动效」而非「颜色」区分状态**：待审批＝单点原地脉动、未读＝单点完全静止。理由是可测量的——36 套主题里有 3 套（ayu-light / gruvbox-light / gruvbox-dark）的 `--accent-color` 与 `--color-orange` **完全相同**，色相本就无法承载区分；且色觉障碍读者拿不到色相信息。两个状态都是 14px 槽内的 8px `radial-gradient` 填充（`--status-dot` / `--status-dot-pending`），**不得用 `border`**——那正是已移除的环的画法。**运行中在槽里什么都不显示**：底边彗星已经在表达「正在推进」，右侧再放一个环是同一事实说两遍（这也是槽原先唯一需要 `border` 的原因）；且槽是 `v-if` 的，无状态即不渲染元素，不占宽度（「没有东西展示时该空间不要占地方」）。因此**没有 `--running-ring` / `--running-ring-track` 这两个 token，也不得有 `.session-status.is-running` 规则或 `session-status-spin` keyframes**（守卫显式断言它们不存在）。优先级 pending > unread（`rowStatus()`）；运行中 + 有未读的行会显示未读点，因为彗星与点走两条通道。被阻塞时底部彗星停止并变为整条琥珀呼吸，且**必须换一个不带指令的元素**（`v-if`/`v-else` 两个 `<i>`）——指令用 WAAPI 写 `transform`，`transform:none` 压不过它。**槽必须与标题保持间距**：`.session-status` 自带 `margin-left: var(--space-4)`，同时 `.session-item` / `.cross-session-item` 的右内边距收到 `var(--space-4)`（其余三边仍是 `var(--space-6)`）——原先 12px 右内边距 + 无 margin 会让槽的左边缘**正好落在标题省略号边界上**（线上实测 0px），而槽与 ⋮ 之间却空着 ~10px；现在两侧各约 11px。守卫：`sessionStatusSlot.css.test.ts`。

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

- **`:root[data-app-mode] .chat-message { will-change: transform }` 不能删**（`ChatMessageItem.vue:663`）——它是跨图层像素污染（GPU ghost）的唯一解。
- **表单控件行高必须是整数 px**：`--input-line-height: 18px`。textarea 文字顶对齐，内容盒高度恰是一行时没有余量；Android WebView 会**独立地**四舍五入行盒和内容盒高度，CJK 字体升降部更大，分数行高下两次舍入不再抵消，光标会明显偏高。桌面 Chrome 恰好舍入一致，所以**只在 WebView 复现**。
- **畸形表单标签要重置 `white-space`**：DOMPurify 会保留真实 `<option>` / `<optgroup>` / `<select>`，而 UA 样式给 `<option>` 加了 `nowrap`，会静默撑破气泡（`overflow:hidden` 裁掉）。`ChatMessageItem.vue:641` 逐标签重置。

### 红线 7：`scrollbar-color` / `scrollbar-width` 会让 Chromium 弃用 `::-webkit-scrollbar-*`

只要给某个元素（或 `*`）设了**标准属性** `scrollbar-color` 或 `scrollbar-width`，Chromium 121+ 就**整体忽略**该元素的全部 `::-webkit-scrollbar-*` 规则——包括 `::-webkit-scrollbar-button { display: none }`。表现：自定义的 4px 细滚动条**退回 15px 原生条**，并且**上下箭头按钮复活**（实测 headed Chrome：17px 带箭头 vs 6px 无箭头）。

`base.css` 原先把 `* { scrollbar-color: … }` 写成了无门控的全局规则，于是紧跟其上的 webkit 细条与 `-button` 规则全部失效——**箭头就是这么冒出来的**。

正解是把标准属性**只留给没有 webkit 伪元素的引擎**（Firefox）：

```css
@supports not selector(::-webkit-scrollbar) {
    * { scrollbar-color: var(--scrollbar-thumb) transparent; }
}
```

`::-webkit-scrollbar-*` 规则本身保持无门控（Chromium 会走它）。**三处同源**：`web/css/base.css`、`src/utils/swaggerHtml.ts`（Swagger 预览 srcdoc）、`src/utils/exportMarkdownHtml.ts`（导出 HTML）——改一处必须三处同改，`__tests__/scrollbarNoArrowButtons.test.ts` 同时钉住。

**粗细全端统一为 4px**（三处同源都是字面量 `4px`，没有 token）。曾短暂按输入设备/宽屏分档（鼠标面 12px）以便更好点、也避开分割条抓取带，但**已按用户要求回退**——统一一个尺寸，跨设备观感一致；分割条那边的冲突在分割条侧解决（见 `SplitDivider.vue`）。守卫会拒绝任何重新引入的分档：出现 `--scrollbar-size` 或第二个 `12px` 宽度即失败。

> Firefox 只有 `auto | thin | none`，没有 px 控制，所以统一用 `scrollbar-width: thin` 近似 4px（放在已有的 webkit 门控里）。

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
| `components/common/__tests__/wrapCheck.css.test.ts` | 菜单「该项当前是开的」勾选 `.wrap-check` 全局唯一、两个菜单共用同一 class |
| `components/common/__tests__/modalFooterBtn.theme.css.test.ts` | `.fbtn` 深色主题前景提亮 |
| `components/common/__tests__/wallpaperSurfaceTransparency.css.test.ts` | 壁纸开启后各表面透明度不叠乘 |
| `components/common/__tests__/wallpaperBlurCost.css.test.ts` | 壁纸模糊不留 `will-change` |
| `components/common/__tests__/resizeDivider.css.test.ts` | 拖拽分隔条外观 |
| `components/file/__tests__/flashReducedMotion.css.test.ts` | 闪烁动效遵守 `prefers-reduced-motion` |
| `components/common/__tests__/BusyBar.test.ts` | 长动作进度条存在且动画在、**无** reduced-motion opt-out（扫过即信息）、不吞指针事件 |
| `components/common/__tests__/sharedRingUnification.test.ts` | 加载环全局唯一、调用方不得重述形状、**全仓只有一个 `--li-border` 比例**（走查源码）、覆盖类必须带祖先 |
| `components/common/__tests__/spinnerUnification.test.ts` | 迁移到 LoadingIndicator 的 6 处保留各自 `--li-color`、不再自带 animation/keyframes、`size` 用档位而非数字 |
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
| `assets/__tests__/annotationButtons.css.test.ts` | 标注按钮全局作用域 |
| `__tests__/scrollbarNoArrowButtons.test.ts` | `scrollbar-color` 必须关在 `@supports not selector(::-webkit-scrollbar)` 里（否则 Chromium 弃用 `::-webkit-scrollbar-*`，箭头按钮复活） |

**写新守卫时注意两个坑**（都实际栽过）：
1. **jsdom 不解析 `var()` 和 `color-mix()`**——`getComputedStyle` 会把 `var(--x)` 原样返回。所以 token 类断言必须**读源码**，不能读计算样式。
2. **一个 `.vue` 文件可以有多个 `<style>` 块**，且**选择器可能是逗号组**——源码嗅探要 `matchAll` + `split(',')`，否则漏匹配。
