# 自定义背景图（Custom Wallpaper）设计方案

日期：2026-09-06
状态：v2（已吸收 code-reviewer 评审修正）

## 概述

在外观（Appearance）配置面板中新增「自定义背景图」能力：用户可上传一张本地图片，或从文件查看器中**拷贝**一张图片为全局背景。背景图铺满整个主界面，主工作面板半透明化透出背景图，营造沉浸式壁纸效果。

背景图为**服务端全局资源**（存于 `<DataDir>/theme/`），跨设备生效、刷新不丢。

## 决策记录

经需求收敛（grill-me）+ code-reviewer 评审确认以下决策：

| 维度 | 决策 |
|---|---|
| 作用范围 | 全屏沉浸式：主工作面板半透明化透出背景图 |
| 存储 | 服务端 `<DataDir>/theme/` + server config key，跨设备生效 |
| 半透明表面 | 仅主工作面板 surface（见「surface 清单」），浮层/弹窗/编辑器/终端/设置页保持不透明 |
| 主题适配 | 单张图 + 深浅自动遮罩（dark ~0.35 黑 / light ~0.12 黑），遮罩强度不开放配置 |
| 与主题关系 | 完全独立：背景图跨主题共享，仅遮罩随深浅态变化 |
| 铺法 | `cover` 铺满 + 居中 + 不重复 |
| 面板不透明度 | 引入 `--panel-alpha` 变量，默认 0.85，滑块可配（50%~100%）；**无背景图时强制 alpha=1、滑块禁用置灰** |
| 配置项 | 两个 server key：`appearance.wallpaperFile` + `appearance.panelOpacity`（YAML snake_case：`wallpaper_file`/`panel_opacity`）。**无开关**：有文件=启用，空=未设置。移除 = DELETE 端点清 key + 删文件（轻量 toast，不弹确认）。**wallpaperFile 禁止 PATCH 直写非空值**（见 C3/I3） |
| 背景切换动效 | 无渐变动效，设置后即时生效 + toast |
| 格式子集 | png/jpg/jpeg/webp/gif/svg 允许；排除 bmp/ico/tiff/avif |
| 缩放重编码 | PNG/JPEG 服务端缩放重编码（最长边 2048px，PNG 转 PNG、JPEG 转 JPEG，保透明）；webp/gif/svg 原样拷贝不缩放。**所有格式均体积校验 ≤10MB，其中 SVG ≤1MB** |
| 文件命名 | 固定基础名 `background.<ext>`，替换时写序：新文件 rename → config 更新 → 清旧扩展文件（后清理，config 失败不丢旧背景） |
| 图片入口 1 | 文件查看器 FileHeader：图片文件 More 下拉 + inline 图标按钮「设置为主题背景」→ 后端拷贝法 |
| 图片入口 2 | 外观面板 WallpaperRow：缩略图 + 上传/更换按钮 + 移除 |
| 上传端点 | 单一端点双模式：body 带 `path`（项目/root 文件拷贝）或 multipart `file`（本地上传） |
| 重复设置 | 不检测幂等，直接覆盖 |
| 源文件删除 | 拷贝独立，删源文件不影响背景 |
| 全局共享语义 | **背景是单服务器全局、跨项目/跨 cookie path 共享**。任意项目页签可设置/移除并即时影响所有页签；其他已开页签通过重新拉 config 感知（见 I4「已接受限制」） |
| Android | 不做原生适配：背景图仅 WebView 内主界面生效；原生壳/悬浮窗/登录页保持主题色 |
| 冷启动恢复 | 不做 index.html 内联脚本 / localStorage 副本；Vue 挂载后读 server config 注入 CSS 变量，首帧无图→瞬切。**前端背景状态用三态 `unknown/set/unset` 防滑块闪烁**（见 I2） |

## 交互设计

### 1. 外观设置面板：WallpaperRow

新增专用渲染分支（`ItemSpec` 扩 type 或 key 特判，参照现有先例）。行内容：

- 当前背景图缩略图（未设置时显示占位图/「未设置」文案）
- 「上传/更换」按钮 → `<input type="file">` → multipart 上传到背景端点
- 已设置时出现「移除」按钮 → 调移除动作（清 config + 删文件），toast「背景已移除」，不弹确认
- 上传/移除成功即刷新主界面背景层（即时生效）

面板不透明度滑块：

- `appearance.panelOpacity` slider，范围 50%~100%，默认 85%
- 未设置背景图时滑块禁用置灰（强制 alpha=1，UI 与现状完全一致）
- 滑块值保留，设置背景图后自动沿用
- **背景状态三态**：server config 未加载完成时滑块 disabled（unknown 态），加载后按 set/unset 决定 enabled/disabled——避免冷启动闪烁（见 I2）

### 2. 文件查看器 FileHeader 入口

- 仅当 `fileType.isImage` 且扩展名 ∈ 背景格式子集（png/jpg/jpeg/webp/gif/svg，非全量 isImage）时可见
- 图标按钮（inline）+ More 下拉一项「设置为主题背景」
- 点击 → 调背景端点（body `{path}`）→ toast「已设置为主题背景」，主界面背景层即时刷新，不关闭当前预览
- 源 path 可为项目内相对路径或外部绝对路径（与文件查看器权限域一致，见 C1）；当前文件上下文无 project cookie（分享视图）时该入口不显示

## 技术方案

### 后端（Go）

**新增文件** `internal/handler/theme_background.go`：

- `POST /api/theme-background`（鉴权）
  - 模式 A：`Content-Type: application/json`，body `{"path": "<project-relative-or-absolute>"}` → 用 `validateAndResolvePath` 解析为绝对路径（相对分支约束 project、绝对分支约束 root——与文件查看器现有权限域一致），`os.Stat` 读文件
  - 模式 B：`multipart/form-data`，字段 `file` → 取上传字节（复用 upload 大小校验）
  - 统一处理链（进程内互斥锁全程持有）：
    1. **magic-number 内容校验**（不信任扩展名，见 C2）：
       - png/jpg/jpeg：直接走第 3 步 `image.Decode`，**解码成功即真实格式白名单**
       - webp/gif：`image.DecodeConfig`（需注册 `golang.org/x/image/webp` 解码器；gif 用 stdlib）验证可解码
       - svg：字节前缀嗅探含 `<svg`，且**拒绝含 `<script>`/`foreignObject`/外部 `href`/`image` 引用**的 SVG（字符串嗅探）
       - 不匹配 → 400
    2. **解码前尺寸上限**（防解压炸弹，见 I6）：`image.DecodeConfig` 先读尺寸，宽或高 > 8000 → 400（10MB 体积上限拦不住高压缩比炸弹）
    3. 体积校验：非 SVG ≤10MB，SVG ≤1MB（读配置可覆盖默认）
    4. PNG/JPEG：`golang.org/x/image/draw` CatmullRom 缩放至最长边 2048px（复用 file_thumb.go `scaleImage` 模式）；PNG 输出 `png.Encode`（保透明）、JPEG 输出 `jpeg.Encode`；**PNG 不转 JPEG 避免丢 alpha**（见 M3）
    5. webp/gif/svg：原样字节（已通过上述内容校验）
    6. 写 `<DataDir>/theme/background.<ext>`：临时文件 + rename 原子写；**rename 成功后先写 config、config 成功后再删同目录其他 `background.*` 旧文件**（后清理；config 写失败则删新文件、旧文件与旧 config 保留，永不丢旧背景，见 review I2）
    7. 写 config：`appearance.wallpaperFile = "background.<ext>"`（隐含启用）；config 写失败时整个 in-memory ConfigInstance 快照回滚
    8. 返回 `{file: "background.<ext>"}`
- `DELETE /api/theme-background`（鉴权）→ 清 `appearance.wallpaperFile` config + 删 `<DataDir>/theme/background.*`（锁内），返回 204
- `GET /api/file/theme-background`（鉴权，定名单一路径，见 I5）→ 返回 `<DataDir>/theme/` 当前 `background.*` 字节，`Content-Type` 按扩展名 + `X-Content-Type-Options: nosniff`；SVG 额外 `Content-Security-Policy: default-src 'none'; sandbox`；响应带 `ETag` + `Cache-Control: private, max-age=…, immutable`（SVG 除外 no-store），参考 file_thumb.go ETag/304 先例

**config 承载（评审 C3 硬冲突，改动面最大）** —— `internal/model/config.go` Config struct 现无任何 appearance 段，需新增贯穿：

1. `config.go`：Config struct 增 `Appearance AppearanceConfig` 段；`AppearanceConfig { WallpaperFile string; PanelOpacity float64 }`（YAML snake_case：`wallpaper_file` / `panel_opacity`，PATCH/GET JSON 用 camelCase `wallpaperFile`/`panelOpacity`，命名统一见 M4）
2. `defaults.go`：`ApplyDefaults` 补 `appearance.panel_opacity: 0.85`（默认值权威在服务端），**presence-map 语义防零值覆盖**（参考 RAG/tls 先例）
3. `settings.go` `PatchableConfigPaths` 白名单补 `appearance.wallpaperFile`、`appearance.panelOpacity`
4. `settings.go` `validatePatchValues`：`appearance.panelOpacity` 范围校验 0.5~1.0（参考 frp.server_port 先例）；**`appearance.wallpaperFile` 非空值一律拒绝**（仅允许 `""` 表示移除；实际移除走 DELETE 端点清文件后置空，PATCH 直写非空 = 路径穿越入口，见 I3）
5. `settings.go` `hotReloadFields`：`appearance.panelOpacity` 列入（PATCH 后即时生效）；`wallpaperFile` 不依赖 PATCH（专用端点写）
6. `settings.go` `configResponse`：GET /api/config 输出两 key（嵌套 `appearance` 段）
7. `handler.go`：注册 `POST /api/theme-background`、`DELETE /api/theme-background`、`GET /api/file/theme-background` 路由

### 前端（Vue）

**CSS 层**：

- `web/css/variables.css`：新增 `--bg-scrim`（默认 `transparent`）、`--panel-alpha`（默认 1）
- 背景层实现（评审 I1：**不存在单一主面板容器，背景层用 fixed 层 + body 透出**）：
  - `.wallpaper-layer`：`position: fixed; inset: 0; z-index: 0;` 挂 `.app-container` 根下，`background-image: var(--bg-image-url); background-size: cover; background-position: center; background-repeat: no-repeat`；其上 `.wallpaper-scrim` 叠 `background: var(--bg-scrim)`
  - 所有工作面板 surface 的 `background: var(--bg-secondary)` 改为 `color-mix(in srgb, var(--bg-secondary) calc(var(--panel-alpha) * 100%), transparent)`；**无背景图时用 class 切换直接写 `var(--bg-secondary)` 原值**（不走 color-mix，见 M1）
- **surface 清单**（逐面决策「透出/不透明」，见 I1）：
  - 透出：`.tab-panel`（TabPanel.vue）、`.header` 顶栏（layout.css）、`.file-overlay`（FileOverlay.vue）、FileHeader/`bs-header` 自身底色、空状态占位、消息/文件内容滚动区透明化后透出的容器
  - 保持不透明：ModalDialog、下拉菜单、命令面板、编辑器、终端、设置面板、ContextMenu、toast——这些层 z-index 高于背景层且不接 `--panel-alpha`
  - 实现按 `--panel-alpha` 原子化，避免逐个手写 rgba 导致难维护
- 面板 z-index 与背景层隔离核对：`.wallpaper-layer` 固定 `z-index:0` 置于 `.app-container` 根、各 absolute TabPanel 之上；TabPanel 自身 `isolation` 语义不受影响

**设置管线**：

- `web/src/utils/themeBackground.ts`（新）：`wallpaperState`（`'unknown'|'set'|'unset'`，serverConfig 的 `appearance` 段缺失/空 = unknown）、`applyWallpaper(cfg)`——有 wallpaperFile 时注入 `--bg-image-url: url(/api/file/theme-background?v=<ts>)` + 按当前 resolved dark 态注入 `--bg-scrim`；无文件时清 `--bg-image-url` + `--bg-scrim: transparent`
- `web/src/composables/useSettingsConfig.ts`：`serverDefaults` 补 `appearance.wallpaperFile: ''`、`appearance.panelOpacity: 0.85`（镜像后端默认）
- `web/src/components/settings/settingsFieldMap.ts`：外观分类新增 section 与行（wallpaper 专用行 + panelOpacity slider）
- `web/src/components/settings/SettingsItem.vue` / `SettingsCategory.vue`：扩 `wallpaper` type（WallpaperRow 分支，含上传/缩略图/移除/三态）与 `panelOpacity` slider 分支（禁用条件 = `wallpaperState !== 'set'`，**需 key 特判**，ItemSpec `disableUnless` 无法表达「非空」，见 I7）；`serverDefaults` 读嵌套段
- `web/src/components/app/App.vue`（或全局根组件）：初始化读 server config → `applyWallpaper`；监听 `clawbench-theme-change`（深浅态）重算 scrim；监听 wallpaper/panelOpacity 变化即时生效
- 深浅遮罩：复用现有 `clawbench-theme-change` 事件 / resolveThemeId

**FileHeader 入口**：

- `web/src/components/file/FileHeader.vue`：isImage 且扩展名 ∈ 背景格式子集、且当前有项目上下文时渲染「设为背景」inline 图标与 More 项 → emit
- 事件链 FileHeader → FileViewer → FileOverlay → App.vue handler → `POST /api/theme-background {path}` → toast + 刷背景

**i18n**：`zh.ts` / `en.ts` `settings.items` 与 `file.header` 补 label/description

### Android

无原生改动。背景图仅 WebView 内主界面生效。`/share/*` 独立 SPA（无鉴权）不显示背景，属预期（见 M5）。

### 已接受的限制（评审 I4 产品决策定稿）

1. **全局共享**：背景图跨项目、跨 cookie path 共享——任意项目页签可改并影响全局。本会话所在页签即时刷新；**其他已开页签需重新拉 config 才感知**（不做跨页签实时事件广播），记为已接受限制。若未来要「每项目独立背景」需重设计（per-project key 存储 + 事件同步），不在本次范围。
2. **外部绝对路径可设为背景**：源 path 允许 root 下任意文件（与文件查看器展示外部文件的权限域一致），体积/内容校验在读取后、写盘前执行。
3. **冷启动无图瞬切**：不做 index.html 内联/localStorage 副本，首帧无图。
4. **EXIF 旋转不校正**（已知限制）：PNG/JPEG 重编码不读 EXIF orientation，手机竖拍带 orientation 标记的照片在重编码后会横置（Go image/jpeg 不自动校正）。与既有 file_thumb.go 行为一致；如需校正需引入额外图像处理。
5. **SVG 拒绝内部 href 引用**（已知限制）：为安全拒绝含 `href=` 的 SVG，误伤内部 `<use href="#symbol">`/`<a>` 的图标类 SVG（如无恶意则用户需转 PNG 上传）。计划刻意选择保守规则。

## 测试面

- 后端：`theme_background_test.go`
  - 拷贝/上传两模式、扩展名/内容白名单（含 `.png` 扩展名内嵌 SVG/HTML 的伪装拒绝）
  - **解码尺寸上限**（超 8000px 拒绝）、体积校验、SVG 拒绝规则（script/foreignObject/外部引用）
  - PNG/JPEG 缩放重编码（含 PNG 透明保留）、旧扩展**后清理**、config 写入
  - GET：Content-Type/nosniff/缓存头/ETag 304
  - settings PATCH：panelOpacity 范围校验、wallpaperFile 非空值拒绝、GET config 含 appearance 段
- 前端：`FileHeader.test.ts`（按钮可见性/emit）、`useSettingsConfig`/`themeBackground`（三态、默认值、apply 注入、config 加载前后行为）

## 影响文件清单

| 层 | 文件 |
|---|---|
| 后端新 | `internal/handler/theme_background.go` |
| 后端改 | `internal/model/config.go`、`internal/model/defaults.go`、`internal/handler/settings.go`（白名单/校验/hotReload/configResponse）、`internal/handler/handler.go`；go.mod `golang.org/x/image` 转 direct require |
| 前端改 | `web/css/variables.css`、`web/css/base.css`、`web/css/layout.css`、`web/src/components/settings/settingsFieldMap.ts`、`SettingsItem.vue`、`SettingsCategory.vue`、`web/src/composables/useSettingsConfig.ts`、`web/src/components/file/FileHeader.vue`、FileViewer/FileOverlay 事件链、App.vue、各 surface CSS（TabPanel/FileOverlay/FileHeader 等）、`web/src/i18n/locales/zh.ts`、`en.ts` |
| 前端新 | `web/src/utils/themeBackground.ts` |
| 测试 | 后端 theme_background_test.go + settings PATCH 用例；前端 FileHeader/useSettingsConfig/themeBackground `.test.ts` |
