# 动态壁纸多风格化（XMB 波浪 + 丝雾星轨）

日期：2026-09-28
状态：已实施

## 概述

动态壁纸从「只有一种」改成「多种中选一种」，并把两种风格的**全部外观参数**暴露到设置面板。加新风格只需三步（新建模块 / 注册 / 补 i18n），设置面板与渲染组件都不用改。

来源原型：`test/psp-wave/index.html`（= 线上已有的 XMB 波浪）、`test/ps3-wave/index.html`（= 新增的丝雾星轨）。

## 决策记录

| 项 | 决定 |
|---|---|
| 风格 id | `xmb`（XMB 波浪）、`silk`（丝雾星轨） |
| 模式值 | **保留 `'wave'`** 作为「动态」模式值；风格另存 `wallpaperAnimatedStyle`。改 `'wave'` 会作废所有已存值，而它本来就是「动态」的意思 |
| 参数存储 | 独立 localStorage 模块 `useAnimatedWallpaperParams`（**不走 `localConfig`**，见下） |
| 速度 | **共用** 10–100 滑块；`speedRange` 每风格不同 |
| 暴露范围 | 两种风格的**全部滑块参数** + XMB 的 `fadeEdges` 开关 + 已有的全局面板不透明度 |
| **不暴露** | 原型的 `reduced`（模拟 prefers-reduced-motion 的调试开关）、`throttle`（30fps 上限）、`hiDpi`（1.5× 超采样）、主题色板（原型的预览机制，App 已跟随主题） |
| 面板不透明度 | 保持全局一份（两种动态风格都适用） |

### 为什么不复用 `localConfig` 存参数

`useSettingsConfig` 的本地配置是 `string | boolean | number | null` **标量**管线：

- 存对象要放宽 `localConfig` / `setLocalConfig` / `readLocalValue` 的类型，波及全仓约 60 处 cast；
- `legacyKeys` 的 `format:'raw'` 分支对对象会写 `String(value)` → `"[object Object]"`。

仓库已有 `useRecentFiles` / `useServerList` 用独立模块存对象的先例，故照此办理。

## 参数表

**XMB（`xmb`）** — 滑块范围即原型范围（0–250 以 100 = 1× 为基准）

| key | 标签 | 范围 | 默认 |
|---|---|---|---|
| `lam` | 波长 | 50–200 | 100 |
| `amp` | 振幅 | 0–250 | 100 |
| `band` | 波内渐隐距离 | 20–260 | 100 |
| `edge` | 边缘清晰度 | 0–250 | 100 |
| `tilt` | 倾斜 | -250–250 | 100 |
| `irr` | 不规整度 | 0–250 | 100 |
| `contrast` | 背景渐变强度 | 0–250 | 100 |
| `fadeEdges` | 左右边缘淡出（开关） | on/off | on |

**丝雾星轨（`silk`）**

| key | 标签 | 范围 | 默认 |
|---|---|---|---|
| `thick` | 光带厚度 | 30–260 | 100 |
| `amp` | 波动幅度 | 0–220 | 100 |
| `lam` | 水平伸缩 | 40–260 | 100 |
| `haze` | 丝雾强度 | 0–220 | 100 |
| `fil` | 丝纹对比 | 0–220 | 100 |
| `spread` | 丝雾宽度 | 40–200 | 100 |
| `gain` | 整体亮度 | 20–200 | 100 |
| `density` | 星点数量 | 0–250 | 100 |
| `psize` | 星点尺寸 | 30–250 | 100 |
| `pglow` | 星点辉光 | 0–250 | 100 |

## 两个易踩的坑

**1. 速度倍率每风格不同**
两个原型都是 `SPEED_MIN=0.25 / SPEED_MAX=2.5`，但**线上已有的 XMB 用的是 0.2–2.0×**（`waveMath.ts` 的 `WAVE_TIME_SCALE_MIN/MAX = 10/50, 100/50`）。改成原型的值会改变既有壁纸的手感，所以 `AnimatedStyle.speedRange` 是每风格的：`xmb` 保持 0.2–2.0（= 现状），`silk` 用 0.25–2.5（= 原型）。

**2. `edgeFade` 重名**
| | 触发 | 机制 |
|---|---|---|
| 图片壁纸 `wallpaperEdgeFade`（`localConfig`） | `mode !== 'wave'` 才显示 | CSS `mask-image: radial-gradient` |
| canvas 的 `fadeEdges`（xmb 参数） | 仅动态模式 | canvas `destination-in` 横向渐变 |

两者 mode 互斥、不会同时出现，但**不能共用键**（一个是标量、一个是风格参数桶），故 canvas 侧改名 `fadeEdges`，标签用「左右边缘淡出」与图片的「边缘柔化」区分。丝雾风格**没有**这个开关（原型里也没有）。

## 架构

```
web/src/utils/canvasMath.ts              通用：hex 解析 / luminance / mixRgb / rgba / noise2 / makeRng / TAU
web/src/utils/waveMath.ts                XMB 形状：WAVE_LAYERS / centerline（通用部分 re-export 自 canvasMath）
web/src/utils/animatedWallpapers/
  types.ts                               ParamSpec（slider|switch）/ AnimatedStyle / FrameContext
  xmb.ts                                 8 个参数 + draw + 边缘淡出
  silk.ts                                10 个参数 + draw（丝雾 + 星点）
  index.ts                               ANIMATED_STYLES / getAnimatedStyle / resolveStyleParams
web/src/composables/useAnimatedWallpaperParams.ts   参数存储（独立 localStorage 模块）
web/src/components/AnimatedWallpaper.vue            生命周期宿主（原 WaveBackground.vue）
```

`AnimatedStyle` 契约：

```ts
interface AnimatedStyle {
  id: string
  labelKey: string
  params: ParamSpec[]
  speedRange: [number, number]
  draw(frame: FrameContext): void   // 纯函数
}
```

### 加一种新风格

1. 新建 `web/src/utils/animatedWallpapers/<id>.ts`，导出 `AnimatedStyle`
2. 加进 `index.ts` 的 `ANIMATED_STYLES`
3. 给 `labelKey` 和每个 `param.labelKey` 补 `en.ts` / `zh.ts`

设置面板（按 `params` 渲染）与渲染组件（按 `id` 派发）都不用动。

## 移植时保留的四个真实缺陷守卫

来自 `test/ps3-wave` 的注释与实测，都是**静默失败**：

1. **`cssW<=0` 必须提前 return** —— 否则 `u = x/cssW` 变 NaN，`createLinearGradient` 抛错并中断整帧
2. **坐标必须 `Number.isFinite` 检查** —— canvas 对含 NaN 的路径**静默丢弃**（不抛错、不警告），表现为「风格凭空消失」而控制台干净。丝雾风格有两处：主循环的波段路径，以及**星点复用的那条路径**（漏掉后者会让贴带星点跟着消失）
3. **颜色解析必须回退，绝不产生 NaN** —— canvas 对非法 `fillStyle` 静默保留上一次的值（背景永远不更新）
4. **丝雾的 alpha 预算** —— `FIL_A = 0.022` × 38 条丝叠出 ≈0.30 的丝雾；随手改 `FIL_A` 或 `FILAMENTS` 会改变整条带的亮度

另外：丝雾用 `globalCompositeOperation = 'lighter'`（叠加发光），必须包在 `save()`/`restore()` 里，否则星点也会被加色。

## 性能（实测）

描边量：`silk` 每帧约 **70 条全宽描边**（主带 38 条丝 + 分叉带 27 条 + 3 条柔光底 + 2 条填充）对 **326 个星点**；`xmb` 每帧只有 **6 条路径**。且每条丝带逐采样点正弦回调、每个大星点各建一个 `radialGradient`。

**在真实浏览器实测**（Playwright + Chromium，1600×1000 CSS px、1.5× 超采样即 2400×1500 backing store、`gruvbox-dark`、全默认参数、30 帧取均值）：

| 风格 | 帧耗时 | 占 30fps 预算（33.3ms） |
|---|---|---|
| `xmb` | **0.97 ms** | 3% |
| `silk` | **7.23 ms** | 22% |

即 **7.5×** 于 xmb（低于按描边数估计的 11×，因为细丝的线宽很窄、填充率不高）。桌面 Chromium 上 22% 的预算是安全的。

**移动端未实测**，WebView 通常比桌面慢 3–5×，届时 `silk` 可能逼近甚至超过 33ms 预算。若真机出现掉帧，按序降级：① 调低 `pglow`（已是用户可调参数）② 丝数随画布宽度自适应 ③ 丝雾整体降到 20–24fps。**不要**用 `backdrop-filter`（设计文档实测掉 ~40% fps）。

## 测试

| 文件 | 覆盖 |
|---|---|
| `utils/__tests__/canvasMath.test.ts` | 通用原语：hex NaN 守卫、luminance、mixRgb、noise2 确定性/连续性、makeRng 可复现 |
| `utils/animatedWallpapers/__tests__/registry.test.ts` | id/param key 唯一、默认值落在范围内、未知 id 回落、`speedRange` 定值、`resolveStyleParams` 的宽容读取、`fadeEdges` 不撞名 |
| `utils/animatedWallpapers/__tests__/silk.test.ts` | stub ctx 下不抛错、**NaN 参数不产生 NaN 坐标**（含「星点启用时波段 NaN」这一分支）、零尺寸提前 return、参数极值、零密度不画星、加色合成 |
| `composables/__tests__/useAnimatedWallpaperParams.test.ts` | 默认/持久化/clamp/重置隔离；**重新 import 模块**验证真实读取路径（坏 JSON、非对象、越界、类型不符、未知风格与键的剪枝） |
| `components/__tests__/AnimatedWallpaper.test.ts` | 生命周期（rAF 起停、卸载清理、null ctx、visibilitychange）；切风格/改参数不重启循环；**静态（reduced-motion）路径的 deep watch 重画** |
| `components/settings/__tests__/WallpaperSetting.test.ts` | 风格选择器渲染/高亮/切换写本地、参数行按风格渲染、开关行、改参数写 store、范围 clamp、单参数重置、整风格重置、关闭时禁用、**切走再切回保留各自调节** |
| `__tests__/waveBackgroundWiring.test.ts` | 源码契约：`<AnimatedWallpaper v-else-if="waveActive">`、三个 prop、`resolveAnimatedStyleId`、watcher 含新键 |

关键守卫均做过**变异验证**：去掉零尺寸守卫 / NaN 守卫（波段或星点）/ 静态路径 watcher / 重置按钮极性 / 风格切换写入 → 对应测试确实 FAIL。

## 未做 / 已知限制

- **不暴露** `reduced` / `throttle` / `hiDpi`：前两者是性能与无障碍策略（真实 reduced-motion 由系统提供，App 已正确响应），`hiDpi` 关掉会退回锯齿。若将来要做「高级」区，可加，但需明确它们是**画质/性能**而非外观。
- **无后端改动**：壁纸选择本来就是每设备 localStorage，`wallpaper_mode` 已从 PATCH 白名单与 config 响应中移除，故不重新引入。
- 丝雾在**浅色主题**上会明显偏暗：它是加色发光（`lighter`），为此 `readPalette` 把舞台往黑压了较多（亮背景压 0.62）。这是刻意的——加色发光需要暗底，否则会糊成白块。
