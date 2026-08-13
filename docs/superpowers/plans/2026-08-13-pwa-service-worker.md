# PWA Service Worker（构建时自动生成）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 每次 `vite build` 自动生成一份与当次产物匹配的 `sw.js`，提供真正的离线 PWA，并用"网络优先 index.html + 版本化缓存 + 精确清旧"根治 stale-index 导致的 404。

**Architecture:** 一个 Vite 插件在构建时收集产物清单、计算内容哈希版本、渲染 `web/sw-template.js` 模板并 `emitFile` 输出 `public/sw.js`（缺 `manifest.json` 时一并补出）。SW 运行时：导航网络优先、hash 资源缓存优先、activate 精确清理旧缓存。`web/src/utils/serviceWorkerCleanup.ts` 的注册/兜底逻辑保持不变。

**Tech Stack:** Vite（`vite.config.ts` 已有多个自定义插件）、Node `crypto`/`fs`、Vitest（jsdom）。

**前置:** 设计文档 `docs/superpowers/specs/2026-08-13-pwa-service-worker-design.md`（已评审通过）。

---

### Task 1: 纯函数工具 + 测试（`scripts/sw-plugin.ts`）

**Files:**
- Create: `scripts/sw-plugin.ts`
- Test: `scripts/sw-plugin.test.ts`

- [ ] **Step 1: 写失败测试**

创建 `scripts/sw-plugin.test.ts`：

```ts
import { describe, it, expect } from 'vitest'
import {
  computeVersion,
  buildPrecacheList,
  renderSw,
} from './sw-plugin'

describe('computeVersion', () => {
  it('changes when output files change', () => {
    const a = computeVersion(['index-a.js', 'vendor-b.js'])
    const b = computeVersion(['index-a.js', 'vendor-c.js'])
    expect(a).not.toBe(b)
  })
  it('is stable for the same output', () => {
    const files = ['index-a.js', 'vendor-b.js']
    expect(computeVersion(files)).toBe(computeVersion([...files]))
  })
})

describe('buildPrecacheList', () => {
  const known = new Set(['index.html', 'index-a.js', 'vendor-b.js', 'manifest.json', 'favicon.png'])
  const exists = (p: string) => known.has(p)

  it('includes index.html and entry/vendor chunks', () => {
    const precache = buildPrecacheList(
      ['index-a.js', 'vendor-b.js', 'vendor-c.js', 'sw.js'],
      exists,
    )
    expect(precache).toContain('/')
    expect(precache).toContain('/index.html')
    expect(precache).toContain('/index-a.js')
    expect(precache).toContain('/vendor-b.js')
  })

  it('excludes missing files (no 404 precache entries)', () => {
    const precache = buildPrecacheList(
      ['index-a.js', 'vendor-b.js', 'vendor-c.js', 'sw.js'],
      exists,
    )
    expect(precache).not.toContain('/vendor-c.js') // missing from `known`
    expect(precache).not.toContain('/sw.js') // never self-cache
    for (const p of precache) {
      const rel = p === '/' ? 'index.html' : p.slice(1)
      expect(known.has(rel)).toBe(true)
    }
  })
})

describe('renderSw', () => {
  it('injects version and precache array', () => {
    const template = 'const VERSION="__VERSION__";const PRECACHE=__PRECACHE__;'
    const out = renderSw(template, 'abc123', ['/', '/index.html'])
    expect(out).toContain('const VERSION="abc123"')
    expect(out).toContain('const PRECACHE=["/","/index.html"]')
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npx vitest run scripts/sw-plugin.test.ts`
Expected: FAIL — module `./sw-plugin` 不存在（import 报错）。

- [ ] **Step 3: 实现纯函数**

创建 `scripts/sw-plugin.ts`：

```ts
import { createHash } from 'crypto'

// Static files guaranteed present (index.html lives in the bundle; icons come
// from the vite publicDir `assets/`; manifest.json lives in `web/`).
export const STATIC_PRECACHE_CANDIDATES = [
  '/index.html',
  '/manifest.json',
  '/favicon.png',
  '/logo-180.png',
  '/logo-512.png',
  '/logo.png',
]

// Content-derived cache version: changes whenever the output file set changes,
// which triggers the SW to swap to a new cache name and purge old ones.
export function computeVersion(fileNames: string[]): string {
  return createHash('sha1')
    .update(fileNames.sort().join('|'))
    .digest('hex')
    .slice(0, 10)
}

// Build the precache list from the bundle's JS/CSS plus static files that pass
// the `exists` check. Only existing files are included so `addAll` never fails
// on a 404 (which would reject the whole install).
export function buildPrecacheList(
  outputFiles: string[],
  exists: (path: string) => boolean,
): string[] {
  const jsCss = outputFiles
    .filter((f) => f !== 'sw.js' && /\.(js|css)$/.test(f))
    .map((f) => '/' + f)
  const statics = STATIC_PRECACHE_CANDIDATES.filter((p) => exists(p.slice(1)))
  return ['/', ...jsCss, ...statics]
}

// Render the SW template with the injected version and precache array.
export function renderSw(template: string, version: string, precache: string[]): string {
  return template
    .replace('__VERSION__', version)
    .replace('__PRECACHE__', JSON.stringify(precache))
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `npx vitest run scripts/sw-plugin.test.ts`
Expected: PASS（3 个 describe 全绿）。

- [ ] **Step 5: 提交**

```bash
git add scripts/sw-plugin.ts scripts/sw-plugin.test.ts
git commit -m "feat(sw): pure helpers for build-time service worker generation"
```

---

### Task 2: SW 运行时模板（`web/sw-template.js`）

**Files:**
- Create: `web/sw-template.js`

- [ ] **Step 1: 创建模板**

创建 `web/sw-template.js`（占位符 `__VERSION__` / `__PRECACHE__` 由插件注入）：

```js
// ClawBench Service Worker — generated at build time.
// VERSION and PRECACHE are injected by scripts/sw-plugin.ts.
const VERSION = '__VERSION__'
const PRECACHE = __PRECACHE__

const CACHE_NAME = 'clawbench-' + VERSION
const RUNTIME_CACHE = 'clawbench-runtime-' + VERSION

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(CACHE_NAME)
      .then((cache) => cache.addAll(PRECACHE))
      .then(() => self.skipWaiting())
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter(
              (k) => /^clawbench-/.test(k) && k !== CACHE_NAME && k !== RUNTIME_CACHE
            )
            .map((k) => caches.delete(k))
        )
      )
      .then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (event) => {
  const { request } = event
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (url.origin !== self.location.origin) return

  // Navigation (index.html): network-first so redeploys are picked up
  // immediately; fall back to cache only when offline.
  if (request.mode === 'navigate') {
    event.respondWith(
      fetch(request)
        .then((res) => {
          const copy = res.clone()
          caches.open(RUNTIME_CACHE).then((c) => c.put(request, copy))
          return res
        })
        .catch(() => caches.match(request).then((hit) => hit || caches.match('/index.html')))
    )
    return
  }

  // Hashed static assets (immutable): cache-first, fetch-and-store on miss.
  event.respondWith(
    caches.match(request).then((cached) => {
      if (cached) return cached
      return fetch(request).then((res) => {
        if (res && res.ok) {
          const copy = res.clone()
          caches.open(RUNTIME_CACHE).then((c) => c.put(request, copy))
        }
        return res
      })
    })
  )
})
```

- [ ] **Step 2: 提交**

```bash
git add web/sw-template.js
git commit -m "feat(sw): runtime service worker template with versioned caches"
```

---

### Task 3: 插件胶水 + 注册到 vite.config.ts

**Files:**
- Modify: `scripts/sw-plugin.ts`（追加 `serviceWorkerPlugin()`）
- Modify: `vite.config.ts`

- [ ] **Step 1: 在 `scripts/sw-plugin.ts` 末尾追加插件**

追加以下导入与函数（保留 Task 1 的纯函数）：

```ts
import { readFileSync, existsSync } from 'fs'
import { resolve } from 'path'
import type { Plugin } from 'vite'

export function serviceWorkerPlugin(): Plugin {
  return {
    name: 'clawbench-service-worker',
    apply: 'build',
    enforce: 'post',
    generateBundle(_options, bundle) {
      const outFiles = Object.keys(bundle)
      const exists = (p: string): boolean =>
        bundle[p] !== undefined ||
        existsSync(resolve('assets', p)) ||
        existsSync(resolve('web', p))

      const version = computeVersion(outFiles)
      const precache = buildPrecacheList(outFiles, exists)
      const template = readFileSync(resolve('web/sw-template.js'), 'utf8')
      const sw = renderSw(template, version, precache)

      this.emitFile({ type: 'asset', fileName: 'sw.js', source: sw })

      // Ensure /manifest.json is served (PWA install metadata). Currently it is
      // referenced by index.html but missing from the build output (a 404 today).
      if (bundle['manifest.json'] === undefined) {
        const manifest = readFileSync(resolve('web/manifest.json'), 'utf8')
        this.emitFile({ type: 'asset', fileName: 'manifest.json', source: manifest })
      }
    },
  }
}
```

- [ ] **Step 2: 注册插件**

编辑 `vite.config.ts`：
- 文件顶部（`import { cpSync, existsSync, mkdirSync, readdirSync } from 'fs'` 之后）加：

```ts
import { serviceWorkerPlugin } from './scripts/sw-plugin'
```

- `plugins: [` 数组中追加（放在 `materialIconsCopy()` 之后）：

```ts
    serviceWorkerPlugin(),
```

- [ ] **Step 3: 验证构建产物**

Run: `npx vite build`
Expected:
- 无报错，`✓ built in ...`
- `ls public/sw.js` 存在，`grep -o 'clawbench-[a-f0-9]\{10\}' public/sw.js` 命中版本号
- `grep -o '"PRECACHE"]\|PRECACHE=\[' public/sw.js` 存在；`grep -o '/index.html' public/sw.js` 命中
- `ls public/manifest.json` 存在（此前缺失，现已补齐）
- `grep -o 'clawbench-runtime' public/sw.js` 命中运行时缓存名

- [ ] **Step 4: 提交**

```bash
git add scripts/sw-plugin.ts vite.config.ts
git commit -m "feat(sw): emit build-time service worker and manifest via vite plugin"
```

---

### Task 4: 全量验证 + 清理

**Files:**
- Modify: 无（仅验证）

- [ ] **Step 1: 运行现有 SW 相关单测**

Run: `npx vitest run scripts/sw-plugin.test.ts web/src/utils/serviceWorkerCleanup.test.ts`
Expected: 全 PASS（sw-plugin 3 组 + serviceWorkerCleanup 5 例）。

- [ ] **Step 2: typecheck + lint + 全量前端测试**

Run:
```bash
npm run typecheck
npx eslint web/src/utils/serviceWorkerCleanup.ts web/src/main.ts
npx vitest run web/src/utils/serviceWorkerCleanup.test.ts
```
Expected: 均无错误。

- [ ] **Step 3: 手工确认产物一致性**

Run:
```bash
npx vite build
node -e "const s=require('fs').readFileSync('public/sw.js','utf8'); const m=s.match(/PRECACHE=\[(.*?)\]/s); const arr=JSON.parse('['+m[1]+']'); const fs=require('fs'); const miss=arr.filter(p=>p!=='/'&&!fs.existsSync('public'+p)); console.log(miss.length?('404 entries: '+miss.join(',')):'OK: all '+arr.length+' precache entries exist');"
```
Expected: 输出 `OK: all N precache entries exist`（无 404 项）。

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "chore(sw): verify build-time service worker output"
```

---

### 自检

- **Spec 覆盖：**
  - 构建时生成 + 内容哈希版本 → Task 1（`computeVersion`）、Task 3。
  - 预缓存只含存在文件 → Task 1（`buildPrecacheList`）、Task 4 Step 3 验证。
  - `manifest.json` 补齐 → Task 3 Step 1。
  - 运行时模板（导航网络优先 / hash 缓存优先 / activate 精确清旧 / skipWaiting+claim）→ Task 2。
  - `serviceWorkerCleanup.ts` 保留 → Task 4 Step 1 复用现有测试。
- **无占位符**：每个代码步骤均含完整代码与命令。
- **类型一致**：`computeVersion(fileNames: string[])`、`buildPrecacheList(outputFiles, exists)`、`renderSw(template, version, precache)`、`serviceWorkerPlugin(): Plugin` 在各 Task 间签名一致。
