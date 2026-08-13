# PWA Service Worker 重建（构建时自动生成）设计

日期：2026-08-13

## 背景与问题

早前版本部署过 `public/sw.js`（91 行，"Smart Workbench" SW），后在被无关提交（`5a6d12ba`、`e1de73ce`）中删除，但 `web/index.html` 里的注册脚本与 `<link rel="manifest">` 一直保留，形成"半吊子"状态：

- 新用户：`/sw.js` 404 → 注册脚本探测后跳过，无离线能力，无残留 worker。
- 老用户：浏览器仍驻留旧 SW，靠缓存喂旧 `index.html`；因服务器不再提供 sw.js，旧 SW 无法自我更新，永远停在旧版本 → 动态 import 旧 hash chunk（如 `CodeMirrorViewer-pYEW6Pkm.js`）404，代码预览等被打断。

服务器端 `internal/handler/static.go` 已正确设置缓存头（index.html `no-cache`，hash 资源 `immutable` 一年），问题仅在客户端 SW。旧 SW 的核心缺陷是**固定预缓存列表 + 缓存优先**，导致 stale index.html 卡住。

目标：彻底带上"真离线"，重建 PWA，并根治 stale-index 问题。

## 方案

构建时自动生成 `sw.js`：每次 `vite build` 生成一份与当次产物完全匹配的 SW，缓存版本号由产物内容哈希推导，新版本自动清理旧缓存。

### 组成部分

1. **`web/sw-template.js`** —— SW 运行时模板，含 `__VERSION__`、`__PRECACHE__` 占位符：

   - `install`：`caches.open(CACHE_NAME).addAll(PRECACHE)`，成功后 `self.skipWaiting()` 立即接管。
   - `activate`：删除所有匹配 `/^clawbench-/` 但**不是**当前 `CACHE_NAME`/`RUNTIME_CACHE` 的缓存（精确名匹配，避免版本哈希是另一版本子串时误删），然后 `self.clients.claim()`。
   - `fetch`：
     - 仅处理同源、`GET` 请求。
     - **导航请求（`request.mode === 'navigate'`）→ 网络优先**：联网时拿新响应并写入运行时缓存；断网回退到 `caches.match(request)`，再回退 `/index.html`。这是根治 stale-index 的关键。
     - **其余（hash JS/CSS 等）→ 缓存优先**：命中缓存直接返回；未命中则 fetch，成功后写入运行时缓存。
     - 跨域请求不处理、不缓存。

2. **`swPlugin.ts`** —— Vite 插件（`apply: 'build'`, `enforce: 'post'`），在 `generateBundle` 阶段：

   - 收集当次产物中真实存在的文件：`/index.html`、全部 `*.js`、`*.css`，以及 `manifest.json`、图标（`/favicon.png`、`/logo-180.png`、`/logo-512.png`、`/assets/*` 等）。
   - **预缓存清单只含确定存在的文件** —— 避免 `addAll` 因任一 404 导致整个 install 失败。
   - `VERSION` = 所有产物文件名拼接后的内容哈希（内容不变则版本稳定，变化则换缓存名 → 触发清旧）。
   - `CACHE_NAME = 'clawbench-' + VERSION`，运行时缓存 `RUNTIME_CACHE = 'clawbench-runtime-' + VERSION`（activate 按这两个精确名判定保留，其余 `clawbench-*` 一律删除）。
   - 用 `this.emitFile({ type:'asset', fileName:'sw.js', source })` 输出到 `public/sw.js`。
   - 若产物中缺失 `manifest.json`，同时从 `web/manifest.json` `emitFile` 一份，保证 `/manifest.json` 不再 404（当前生产环境实际 404，影响 PWA 安装）。

3. **`vite.config.ts`** —— 注册 `serviceWorkerPlugin()`。

4. **`web/src/utils/serviceWorkerCleanup.ts`** —— 现有逻辑保留：
   - `/sw.js` 为合法 JS → 正常注册（现在会走到此分支）。
   - 服务器不再提供 sw.js → 自动注销残留 worker + 清缓存（兜底，防半吊子状态）。
   - SW 更新后靠 `skipWaiting` 自动接管，无需手动清理。

### 数据流

```
vite build
  → swPlugin 收集产物清单 → 计算 VERSION → 生成 sw.js(含 PRECACHE/VERSION)
  → 输出 public/sw.js (+ manifest.json 补齐)
  → build.sh 拷 public/ → internal/frontend/dist → Go embed
浏览器
  → 探测 /sw.js (200, JS) → 注册 → 旧 SW 被 skipWaiting 新版本替换
  → activate 清掉旧版本缓存 → 在线导航拿最新 index.html；离线回退缓存
```

### 错误处理

- 预缓存含不存在文件会拒装：清单严格取自产物，规避。
- 导航网络失败：回退到运行时缓存的最近一次 index.html，再回退 `/index.html`；全无则请求失败（离线且无缓存时的合理行为）。
- 缓存写入失败：不阻塞响应返回（`caches.open().then(...)` 异步、失败静默）。

### 测试

- **`swPlugin` 集成测试**：以 Vite build 调用插件，断言：
  - 生成的 `sw.js` 版本号随产物变化而变化；
  - `PRECACHE` 内每个路径在产物中都真实存在（无 404 项）。
- **`serviceWorkerCleanup.test.ts`**：现有 5 例保持通过（注册分支现在为合法 JS 路径）。
- typecheck / lint / build 全过。

### 边界 / 非目标

- 断网时可离线打开此前访问过的页面与资源；从未访问过的懒加载资源需联网。
- 不实现后台推送/后台同步（不在范围）。
- `/manifest.json` 与图标纳入预缓存以支持 PWA 安装与离线。
