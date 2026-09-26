# 动态波浪壁纸（XMB Wave）设计方案

日期：2026-09-26
状态：v1（已通过原型验证，待评审）

原型：`test/xmb-wave/index.html`（可交互 demo，控制面板可实时调参 + 切换主题）

## 概述

在外观设置的壁纸来源中新增第三种模式「动态壁纸」，与现有「本地图片 / Bing」平级。选中后渲染一条**超低频、大振幅、边缘清晰的波浪**作为背景，观感参考 PSP XMB 系统的经典波浪。

- 波浪由 `<canvas>` 逐帧绘制，**不是图片**，故无磁盘文件、无网络请求
- 配色跟随当前主题（从 CSS 变量取色），换主题自动变色
- 复用现有 `wallpaper-active` 半透明机制，波浪透过面板隐约可见
- 模式选择走**服务端持久化**（多端一致），速度等显示微调走**本地偏好**

## 决策记录

经需求收敛确认以下决策：

| 维度 | 决策 |
|---|---|
| 定位 | 壁纸的第三种**模式**，与 `local` / `bing` 平级；共用现有全局开关 `wallpaper_enabled` |
| 渲染 | Canvas 2D；**固定 1.5× 超采样**（非 `min(1.5, dpr)`，理由见下） |
| 帧率 | 30fps 上限（rAF 内累加 dt 门控）；页面隐藏即暂停 |
| 波形 | 3 条，超低频正弦 + 三处轻微扰动（相位扭曲 / 二次谐波 / 振幅包络） |
| 清晰度 | 实心填充 + 1.5px 波峰高亮描边；**不做模糊柔化** |
| 波长 | 一个完整周期跨 1.9–2.7 页宽 → 整页仅约 0.4–0.5 个周期 |
| 渐变 | 背景一层竖直渐变（上暗→下亮）；每条波内部各一层竖直渐变（波峰最亮→向下渐隐） |
| 配色 | 从 `--accent-color` / `--bg-primary` 推导四色；主题可显式覆盖 |
| 取色方式 | **每帧直读 CSS 变量，不缓存、不监听事件**（实测 0.0007ms/帧，理由见下） |
| 面板半透明 | 复用现有 `wallpaper-active` + `--panel-alpha`，零新增机制 |
| 模式存储 | 服务端 `appearance.wallpaper_mode = "wave"` |
| 速度存储 | **本地偏好** `wallpaperWaveSpeed`（与 `wallpaperBlur` / `wallpaperEdgeFade` 同级） |
| 暴露参数 | 仅一个速度滑块（10–100，50 = 1×，实际 0.2×–2×） |
| 交互 | 纯环境动效，**不响应鼠标/触摸/滚动** |
| reduced-motion | `prefers-reduced-motion: reduce` → 只绘制一帧静态图，不进入循环 |
| 生命周期载体 | **独立子组件 `<WaveBackground>`**，靠 `onUnmounted` 自动清理（理由见下） |

## 关键技术决策与理由

### 1. 固定 1.5× 超采样，而非 `min(1.5, dpr)`

```js
scale = ui.hiDpi ? 1.5 : 1
```

**理由**：超采样的本质是「按高于显示的分辨率绘制，由浏览器双线性缩小」，**缩小过程本身就是抗锯齿**，所以 DPR1 屏同样受益。

若写成 `min(1.5, dpr)`，DPR1 屏会得到 `scale = 1`，**等于完全没有改善** —— 而锯齿问题恰恰出现在低 DPR 屏上。这是原型阶段差点踩进去的坑。

代价对比（1440×900）：

| 倍率 | backing store | 像素量 | 相对 1× 填充 |
|---|---|---|---|
| 1.0（关） | 1440×900 | 1.30 Mpix | 1.00× |
| **1.5（默认）** | 2160×1350 | 3.50 Mpix | 2.25× |
| 2.0 | 2880×1800 | 5.18 Mpix | 4.00× |

1.5× 相比 2× 省 **32%** 填充量，而锯齿已基本不可见。

### 2. 采样步长以 **backing 像素**为单位

```js
const STEP_BACKING = 1.5
const stepCss = STEP_BACKING / scale
```

**理由**：若步长固定为 CSS 像素，提高 `scale` 只是把**同样多的顶点**画在更多像素上，锯齿依旧存在。固定 backing 单位才能让超采样真正增加顶点密度。这是「改了分辨率却没改善锯齿」的典型陷阱。

### 3. 每帧直读 CSS 变量，不缓存、不监听事件

```js
// 每帧都这样读，不做缓存
const cs = getComputedStyle(document.documentElement)
const accent = parseHex(cs.getPropertyValue('--accent-color'))
const bg     = parseHex(cs.getPropertyValue('--bg-primary'))
```

**理由一：直读足够便宜。** 实测两次 `getPropertyValue` 合计 **0.0007 ms/帧**，相对绘制本体（0.83 ms）占 0.08%，可忽略。

**理由二：缓存方案会引入一个冷启动死区。** `clawbench-theme-change` 事件在冷启动时**永远不会触发**，且这是刻意的 —— `useSettingsConfig.ts:701-703` 注释原文：

> Deliberately does not dispatch `clawbench-theme-change`: at mount time App.vue's listener is not registered yet (it is installed later in initializeApp)

监听器注册在 `App.vue:1829`（`initializeApp` 内），而主题在 `App.vue:3087` 的 `applyStoredThemeOnMount()` 就已应用。所以事件只覆盖「运行中切换主题」这一种情况，**初始取色必须另找路径**。

**理由三：无缓存即无过期。** 直读方案下：

- 初始取色天然正确（画第一帧就读到当前值）
- 运行中切换主题，下一帧自动跟上，**零事件处理**
- 「主题切换与重绘的时序」这个风险项直接消失

代价是每帧重算 4 个颜色（几次 `parseHex` + `mix`），同样在 0.0007ms 量级。**结论：删掉事件监听器，比原方案严格更简单也更健壮。**

### 4. 生命周期由子组件承担，而非 App.vue 内的 composable

`App.vue:10` 的根节点是：

```html
<div v-else class="app-container" :key="projectKey">
```

而 `App.vue:749` 在项目切换时执行 `projectKey.value = newProjectPath`，**整个 `.app-container` 子树被销毁重建**，包含 `.wallpaper-layer` 及其中的 canvas。

问题在于 **App.vue 自身不会卸载**，只卸载它的子组件：

| 实现方式 | 项目切换时 | 后果 |
|---|---|---|
| App.vue 内 composable + `onUnmounted` | `onUnmounted` **不触发** | **每次切换泄漏一个 rAF 循环**，且它继续往已销毁的 canvas 上绘制 |
| 独立子组件 `<WaveBackground>` | 组件卸载 → `onUnmounted` **自动触发** | 生命周期天然正确 |

这与 `web/src/directives/runningSweep.ts` 的设计哲学一致（注释原文）：

> Put it on an element that exists ONLY while the session is running; it starts the sweep on mount and cancels it on unmount, **so there is no state to keep in sync by hand**.

**决策**：做成子组件，mount → `start()`，unmount → `stop()`。项目切换自动处理，**不需要手动 watch `projectKey`**。

### 5. 颜色解析必须带空值兜底

`--accent-color` / `--bg-primary` **没有 `:root` 兜底**，只在 `[data-theme="..."]` 下定义（`variables.css:864` 起；22–140 行的 `:root` 块内不含这两个变量）。

若取到空字符串，失败链是**静默的**：

```js
parseInt('', 16)      → NaN
rgba(NaN, NaN, NaN, a) → 无效颜色字符串
ctx.fillStyle = 无效值 → canvas 忽略赋值，保留上一次的值（不抛错）
```

这与原型阶段踩过的 NaN 坐标是**同一类失败：canvas 不报错，只是不画**（见「原型阶段已修的真实缺陷」#1）。

实践中 `web/index.html` 的同步内联脚本总会先设 `data-theme`，触发概率低；但鉴于 canvas 的静默行为，解析函数必须对空值/畸形值回退到硬编码默认色。

### 6. 用描边而非模糊来保证清晰度

早期原型用「5 层厚度递减填充叠加」做柔光带，结果波形糊成一团。**清晰与柔化是相互矛盾的诉求**：

- 波峰 `stroke` 1.5px 高亮描边 —— 清晰度的直接来源
- 渐变 stop 收紧到 **2%**（超过约 2% 波高的过渡带会让边缘读起来发虚）
- 取消早期版本的 `filter: blur()` 思路

### 7. 性能特征（实测）

绘制耗时（JS 主线程，软件光栅化环境下测，真机只会更快）：

| 配置 | backing store | 采样点/帧 | 每帧绘制 |
|---|---|---|---|
| 桌面 1440×900 @DPR1 | 1440×900 | 2883 | 0.43 ms |
| 桌面 1440×900 @DPR2 | 2160×1350 | 4323 | **0.83 ms** |
| 手机 390×844 @DPR3 | 585×1266 | 1563 | 0.25 ms |

主线程占 30fps 预算（33.3ms）的 **2.5%**。

**瓶颈在填充率（GPU），不在 JS**。原型环境是 SwiftShader 软件光栅化，帧率数字不可作为真机参考。

**已测出的真实风险**：在动画 canvas 上层使用 `backdrop-filter` 会额外吃掉约 40% 帧率（13→22fps）。ClawBench 主面板不使用 `backdrop-filter`（仅弹层使用），故常态无影响；但**波浪模式下打开对话框时会有瞬时掉帧**，需在实现时确认可接受。

## 后端改动（4 处，均为枚举放宽）

1. `internal/handler/theme_gallery.go:403` — 模式校验增加 `"wave"`
2. `internal/handler/settings.go:1308` — PATCH 枚举校验同步放宽
3. `internal/handler/settings.go:423` — `wallpaper_mode` 注释更新
4. `internal/api/openapi.yaml` — `wallpaper_mode` 的 enum / description 同步

### 关键取舍：`ResolveActive` 对 wave 返回 `("", false)`

`internal/wallpaper/wallpaper.go:177` 的 `ResolveActive` 对 wave 返回 `("", false)`，即 `active_file` 保持**空串**。

**理由**：wave 没有磁盘文件。硬塞一个伪文件名会污染 `FilePath` / 缩略图 / `ReconcileLocalGallery` 清理逻辑 —— 一个不存在的文件会被当作有效条目反复校验。

**因此「当前是波浪」必须靠 `wallpaper_mode === "wave"` 判断**，前端需要新增独立判据（见下节）。这是本设计最容易漏的地方。

`internal/model/defaults.go` **不动** —— 只有用户主动选择才进入 wave，绝不改变现有安装的 Bing 默认。

## 前端改动

### 判据拆分（最容易漏的一环）

现有代码把「背景是否激活」等同于「`active_file` 非空」。该等式在 wave 下**不成立**，有 5 个消费点必须一起改，否则面板不会变半透明、设置里的滑块会全灰：

| 位置 | 改动 |
|---|---|
| `resolveWallpaperMode` | 联合类型加 `'wave'`，返回 `'wave'` |
| 新增 `isWaveActive(appearance)` | `mode === 'wave' && enabled` |
| `applyWallpaper(...)` | 加**可选第 5 参** `waveActive = false`，内部 `const active = !!file \|\| waveActive` 决定 `wallpaper-active` 类 |
| `App.vue` `wallpaperActive` | `state === 'set' \|\| waveActive` |
| `WallpaperSetting.vue` | 模式分支链改为 `v-if="mode === 'wave'"` / `v-else-if="mode === 'bing'"` / **`v-else`**（画廊兜底，见下方警示） |

**第 5 参带默认值**，现有测试的 4 参调用行为完全不变，无需改存量测试。

`--wallpaper-scrim` **仍只在有图片时写**（wave 不需要遮罩）；`--panel-alpha` 本就无条件写，直接复用。

**另需修一处：`App.vue:17` 的 `<img v-if="wallpaperActive" :src="wallpaperUrl">` 要改成 `v-if="wallpaperUrl"`。** wave 模式下 `wallpaperActive` 为真但 `wallpaperUrl` 是空串，会渲染出 `<img src="">`；空 src 在部分浏览器会触发对当前页面 URL 的请求。

**`resolveWallpaperState` 的语义要注意**：它对 wave 返回 `'unset'`。这个命名有误导性 —— `'unset'` 的实际含义是「没有图片」，不是「没有背景」。现有消费点（`App.vue:1250`、`WallpaperSetting.vue:297`）都按「有无图片」理解，所以 wave 下判为 false 是**预期行为**。建议顺手给该函数补注释澄清语义。

### 设置 UI

- 模式控件加第三个按钮「动态壁纸」
- 模式分支链：`v-if="mode === 'wave'"` → `v-else-if="mode === 'bing'"` → **`v-else`（画廊）**
- wave 段：仅一行**速度滑块**（本地偏好，10–100，默认 50）
- **拆分两个判据**：面板不透明度用 `hasActiveBackground`（图片或 wave）；模糊 / 边缘淡出用 `hasImageWallpaper`（仅图片，否则 wave 下滑块可拖动却无效果）

> **警示：画廊必须留在最后的 `v-else`，不能改成 `v-else-if="mode === 'local'"`。**
> 现有安装若从未选择过来源，`wallpaper_mode` 是空串（`defaults.go:146` 只在**全新安装**时默认 `bing`，`defaults_test.go:289` 明确断言既有安装保持空串），经 `resolveWallpaperMode` 解析为 `'none'`。若把画廊门控在 `mode === 'local'`，**所有既有用户的画廊都会消失** —— 而画廊正是他们挑选图片的唯一入口。
> 本次实现中我一度写成 `v-else-if="mode === 'local'"`，靠变异测试才发现（`waveBackgroundWiring.test.ts` 有一条专门断言该分支是 `v-else`）。

### 渲染接线

新增子组件 `web/src/components/WaveBackground.vue`（与 `WelcomeOverlay.vue` / `TocPanel.vue` 等同级 —— 这些是 App.vue 直接挂载的组件，仓库惯例是放在 `components/` 根而非分组子目录）。放在 `.wallpaper-layer` **内部**，因此**不新增 `.app-container` 直接子元素**，`base.css:99-104` 的 z-index 契约无需改动。

```html
<!-- App.vue 的 .wallpaper-layer 内，与 <img> 并列且互斥 -->
<img v-if="wallpaperUrl" :src="wallpaperUrl" ... />
<WaveBackground v-else-if="waveActive" :speed="waveSpeed" />
```

组件内部：

```js
onMounted(() => { /* 判 reduced-motion：静态一帧 or start() */ })
onUnmounted(() => stop())          // 项目切换 / 模式切换时自动清理
watch(() => props.speed, setSpeed) // 只改 timeScale，不重建画布、不重置相位
```

**不手动 watch `projectKey`** —— `:key="projectKey"` 重挂载会让 `onUnmounted` 自然触发，这正是做成子组件的理由（见「关键技术决策」#4）。

`prefers-reduced-motion` 需在 JS 里判断，全仓无 JS 先例（现有用法都是 CSS `@media`）。可参照 `SessionShareView.vue:256` / `HintTooltip.vue:68` 的守卫写法处理 jsdom（无 `matchMedia`）。

## 测试计划

**Go**
- `ServeThemeWallpaperMode` 接受 `"wave"` 且**不触发 Bing 抓取**
- PATCH 接受 `"wave"`；`"nonsense"` 仍返回 400
- `ResolveActive` 对 wave 返回 `("", false)`
- `buildConfigAppearance` 对 wave 输出空 `active_file` + `wallpaper_mode: "wave"`

**前端**
- `resolveWallpaperMode` 认 `'wave'`
- `applyWallpaper` 第 5 参为 true 时挂 `wallpaper-active`，且**不写 scrim**
- `waveTimeScale` 边界钳制（0.2×–2×）
- `parseHexColor` 对空串/畸形值回退默认色（**不得返回 NaN**，见「关键技术决策」#5）
- `WaveBackground` 组件：`onUnmounted` 确实停掉 rAF（**项目切换会重挂载，这是泄漏高发点**）
- 组件测试：第三个按钮存在、点击发 `{ mode: 'wave' }`、速度滑块写本地配置
- 画廊段在 wave 模式下**不渲染**

**测试注意**
- canvas 在 jsdom 下 `getContext` 返回 `null`，组件必须静默退出而非抛错
- 纯数学（层定义、`timeScale`、`centerline`、颜色解析）应**单独导出测试**，避免用假 canvas 做同义反复
- `matchMedia` 在 jsdom 下不存在，`prefers-reduced-motion` 分支需守卫

## 原型阶段已修的真实缺陷

记录在案，实现时勿重犯：

1. **`centerline` 返回 NaN → 光带整体消失且无报错**。层定义漏写 `base` 字段导致 `undefined + number = NaN`，而 **canvas 对 NaN 坐标静默丢弃整条路径**（不抛错、不警告）。表现是「什么都看不见」但控制台干净，极难排查。已在原型加显式守卫（检出非有限坐标 → `console.warn` 一次并跳过该层），并做变异验证（删掉 `base` 后守卫确实触发）。

2. **验证指标错误导致误判「通过」**。曾用「每列最亮像素的亮度值」判断光带是否可见，但该值恒等于渐变底部的亮度，无论光带在不在都通过。正确指标是**每列最亮像素的 y 坐标** —— 一量就暴露问题（所有列钉在画布底边）。

3. **`draw()` 早于 `resize()` 调用**：`cssW = 0` → 波长计算除零得 `Infinity` → `sin(Infinity) = NaN` → `createLinearGradient` 抛错，**异常还中断了后续脚本初始化**（主题色板一个都没渲染）。尺寸未就绪时必须直接跳过绘制。

4. **alpha 预算不足致光带物理上不可见**：5 层叠加后累计仅 9.2% 不透明度，比它所在的底色还暗，不可能成为最亮处。

5. **「半分辨率」与「清晰波形」互相矛盾**：早期为柔光带选定的 0.5× 分辨率，在换成清晰硬边后变成锯齿来源。**改绘制方式时必须回头审视相关参数是否还成立** —— 这是本次改动最容易漏的耦合。

## 风险与未决

| 风险 | 说明 |
|---|---|
| 真机锯齿 | 原型环境为 SwiftShader 软件光栅化，「1.5× 锯齿是否可接受」已由用户确认，但未在真 GPU 上验证 |
| 弹层掉帧 | 波浪模式下打开含 `backdrop-filter` 的弹层时会有瞬时掉帧，需确认可接受 |
| 移动端发热 | 持续 30fps 重绘。已用可见性暂停 + 30fps 上限缓解；真机长时间功耗未实测 |
| 项目切换重挂载 | `:key="projectKey"` 会让 canvas 重建。已通过「做成子组件」让清理自动化，但**重挂载瞬间可能有单帧空白**，需观察 |

## 实现顺序建议

1. 后端枚举放宽 + 测试（4 处，改动小、可独立验证）
2. 前端判据拆分 + `applyWallpaper` 第 5 参 + `img` 的 `v-if` 修正 + 测试（**最容易漏的一环，先做**）
3. 纯函数层：颜色解析（带兜底）、`timeScale`、`centerline` + 单测
4. `WaveBackground` 组件（从原型提取渲染逻辑 + 生命周期）
5. App.vue 接线 + 设置 UI
6. 真机验证锯齿、功耗、项目切换
