# ClawBench 前端桥层抽象（ClawBenchNative）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将前端原生桥 `window.AndroidNative` 抽象为平台无关的 `window.ClawBenchNative`，并把同步读操作统一改造为异步契约，使 Android 与 Electron 共用同一桥接口。

**Architecture:** 新建单一桥包装模块 `web/src/utils/clawbenchNative.ts` 作为契约单一来源 + 平台检测 + 同步/异步统一适配（`await` 对 Android 同步返回值、Electron Promise 均兼容）。各消费点从直接读 `window.AndroidNative` 改为 import 包装器；同步读改 `await`。同时移除 spec 明确跳过的桥方法调用（`setVolumeKeyMode`/`setTerminalSessionCount`/`dismissSplash`/`stopBackgroundService`）。

**Tech Stack:** TypeScript / Vue 3 / Vitest。

参考 spec：`docs/superpowers/specs/2026-08-10-clawbench-electron-design.md` §4（桥层契约）与 §2.3（跳过清单）。

---

## 关键设计决策

1. **契约单一来源**：`ClawBenchNative` TS 接口 + `getNative()` + `isNativeApp()` 只定义在 `web/src/utils/clawbenchNative.ts`，消费点不得再直接 `(window as any).AndroidNative`。
2. **统一异步适配**：包装器方法一律返回 `Promise`。对 Android（同步 `@JavascriptInterface` 返回值）用 `await` 包裹即可正常工作；对 Electron（返回 Promise）同样 `await`。`call()` 辅助函数统一处理"桥不存在→undefined"。
3. **重连特殊处理**：Android 的 `reconnectTunnelAsync` 是 fire-and-forget + 全局回调 `__clawbenchReconnectResult`；Electron 的实现返回 Promise。包装器的 `reconnectTunnel()` 封装两者，`usePortForward` 不再直接处理全局回调。
4. **移除范围外方法调用**：`setVolumeKeyMode`/`setTerminalSessionCount`/`dismissSplash`/`stopBackgroundService` 从各调用点删除（保留函数体其它逻辑）。
5. **`getAppVersion()` 改异步**（主进程 `app.getVersion()` 语义），消费点改为 await；`isNativeApp()`/`getLanguage()`/`log()`/`openSession()`/`setNativePushEnabled()`/`updateLastSeenEventId()`/`setKeepScreenOn()` 保持同步。

---

## 任务分解

### Task 1: 创建桥包装模块 `web/src/utils/clawbenchNative.ts`

**Files:**
- Create: `web/src/utils/clawbenchNative.ts`
- Test: `web/src/utils/__tests__/clawbenchNative.test.ts`

- [ ] **Step 1: 写失败测试**

```ts
import { describe, it, expect, vi, afterEach } from 'vitest'
import { getNative, isNativeApp, reconnectTunnel, callNative } from '../clawbenchNative'

function setNative(obj: unknown) {
  ;(window as unknown as { ClawBenchNative?: unknown }).ClawBenchNative = obj
}

afterEach(() => {
  delete (window as unknown as { ClawBenchNative?: unknown }).ClawBenchNative
  vi.restoreAllMocks()
})

describe('clawbenchNative bridge wrapper', () => {
  it('getNative returns undefined when no bridge', () => {
    expect(getNative()).toBeUndefined()
  })

  it('getNative returns the bridge object', () => {
    const fake = { isNativeApp: () => true }
    setNative(fake)
    expect(getNative()).toBe(fake)
  })

  it('isNativeApp is true only when bridge reports true', () => {
    setNative({ isNativeApp: () => true })
    expect(isNativeApp()).toBe(true)
    setNative({ isNativeApp: () => false })
    expect(isNativeApp()).toBe(false)
    expect(isNativeApp()).toBe(false)
  })

  it('callNative awaits both sync and async bridge results', async () => {
    const syncNative = { getPassword: () => 'pwd' }
    setNative(syncNative)
    expect(await callNative(n => n.getPassword())).toBe('pwd')

    const asyncNative = { getPassword: () => Promise.resolve('pwd2') }
    setNative(asyncNative)
    expect(await callNative(n => n.getPassword())).toBe('pwd2')
  })

  it('callNative resolves undefined when bridge is missing', async () => {
    expect(await callNative(n => n.getPassword())).toBeUndefined()
  })

  it('reconnectTunnel resolves via Electron-style Promise', async () => {
    const native = { reconnectTunnelAsync: () => Promise.resolve(true) }
    setNative(native)
    expect(await reconnectTunnel()).toBe(true)
  })

  it('reconnectTunnel resolves via Android-style global callback', async () => {
    const native = {
      reconnectTunnelAsync: () => {
        setTimeout(() => {
          const cb = (window as unknown as { __clawbenchReconnectResult?: (v: boolean) => void }).__clawbenchReconnectResult
          cb?.(true)
        }, 5)
      },
    }
    setNative(native)
    expect(await reconnectTunnel()).toBe(true)
  })

  it('reconnectTunnel falls back to blocking reconnectTunnel', async () => {
    const native = { reconnectTunnel: () => false }
    setNative(native)
    expect(await reconnectTunnel()).toBe(false)
  })

  it('reconnectTunnel resolves false when nothing available', async () => {
    expect(await reconnectTunnel()).toBe(false)
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd web && npx vitest run src/utils/__tests__/clawbenchNative.test.ts`
Expected: FAIL（模块不存在，import 报错）。

- [ ] **Step 3: 实现包装模块**

```ts
/**
 * Platform-agnostic native bridge wrapper.
 *
 * The host app (Android WebView or Electron) injects a `window.ClawBenchNative`
 * object implementing this contract. All methods that read state are async so a
 * synchronous Android @JavascriptInterface and an asynchronous Electron
 * ipcRenderer.invoke both work under `await`.
 */

/** Full bridge contract shared by Android and Electron. */
export interface ClawBenchNative {
  // Sync (preload / JS-interface local values)
  isNativeApp(): boolean
  getLanguage(): string
  showServerDialog(): void
  openSession(sessionId: string): void
  setNativePushEnabled(enabled: boolean): void
  updateLastSeenEventId(id: string): void
  setKeepScreenOn(on: boolean): void
  log(level: string, tag: string, msg: string): void

  // Async reads (main-process / native state)
  getAppVersion(): Promise<string>
  getServerList(): Promise<string>
  getSavedServerConfig(): Promise<string>
  getServerUrl(): Promise<string>
  getPassword(): Promise<string>
  getForwardedPorts(): Promise<string>
  testPortReachable(localPort: number): Promise<boolean>
  isTunnelConnected(): Promise<boolean>
  getTunnelError(): Promise<string>
  getTunnelErrorType(): Promise<string>
  getPendingNavigation(): Promise<string>

  // Async writes / actions
  saveServer(url: string, password: string): Promise<void>
  removeServer(url: string): Promise<void>
  setSSHPassword(pwd: string): Promise<void>
  connectToServer(url: string, password: string): Promise<void>
  addForwardedPort(localPort: number, targetPort: number, host: string): Promise<void>
  removeForwardedPort(localPort: number): Promise<void>
  reconnectTunnel(): Promise<boolean>
  reconnectTunnelAsync(): Promise<void>
  downloadFile(path: string): Promise<void>
  downloadUrl(url: string, fileName: string): Promise<void>
  downloadBlob(base64: string, fileName: string): Promise<void>
  openInBrowser(port: number, protocol: string, host: string, path: string): Promise<void>
  openInSandbox(port: number, protocol: string, host: string, path: string, sessionId?: string): Promise<void>
  startLogCapture(): Promise<void>
  stopLogCapture(): Promise<void>
  shareText(text: string): Promise<void>
  shareFile(path: string, mime: string): Promise<void>
  shareFiles(paths: string, mimes: string): Promise<void>
}

const bridgeWindow = window as unknown as { ClawBenchNative?: ClawBenchNative }

/** Get the injected bridge, or undefined when running as plain web. */
export function getNative(): ClawBenchNative | undefined {
  return bridgeWindow.ClawBenchNative
}

/** True when running inside a native host app (top-level frame). */
export function isNativeApp(): boolean {
  try {
    if (window !== window.top) return false
    return getNative()?.isNativeApp() === true
  } catch {
    return false
  }
}

/**
 * Call a method on the bridge, resolving undefined when the bridge is missing.
 * Works for both synchronous (Android) and asynchronous (Electron) results.
 */
export async function callNative<T>(fn: (n: ClawBenchNative) => T | Promise<T>): Promise<T | undefined> {
  const native = getNative()
  if (!native) return undefined
  return await fn(native)
}

const RECONNECT_CALLBACK_NAME = '__clawbenchReconnectResult'

/**
 * Reconnect the SSH tunnel, resolving with success boolean.
 * - Electron: native.reconnectTunnelAsync returns a Promise.
 * - Android legacy: fire-and-forget + global callback (window.__clawbenchReconnectResult).
 * - Android old: blocking native.reconnectTunnel returns boolean.
 */
export function reconnectTunnel(): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    const native = getNative()
    if (!native) return resolve(false)

    if (native.reconnectTunnelAsync) {
      let result: unknown
      try {
        result = native.reconnectTunnelAsync()
      } catch {
        return resolve(false)
      }
      // Electron-style Promise
      if (result && typeof (result as Promise<boolean>).then === 'function') {
        ;(result as Promise<boolean>).then(resolve).catch(() => resolve(false))
        return
      }
      // Android-style global callback + safety timeout
      let settled = false
      const done = (success: boolean) => {
        if (settled) return
        settled = true
        delete (window as unknown as Record<string, unknown>)[RECONNECT_CALLBACK_NAME]
        resolve(success)
      }
      ;(window as unknown as Record<string, unknown>)[RECONNECT_CALLBACK_NAME] = (success: boolean) => done(success)
      setTimeout(() => done(false), 16000)
      return
    }

    if (native.reconnectTunnel) {
      Promise.resolve(native.reconnectTunnel()).then(resolve).catch(() => resolve(false))
      return
    }
    resolve(false)
  })
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd web && npx vitest run src/utils/__tests__/clawbenchNative.test.ts`
Expected: PASS（9 个用例全过）。

- [ ] **Step 5: 提交**

```bash
git add web/src/utils/clawbenchNative.ts web/src/utils/__tests__/clawbenchNative.test.ts
git commit -m "feat(web): add platform-agnostic ClawBenchNative bridge wrapper"
```

---

### Task 2: `useAppMode.ts` 改用包装器

**Files:**
- Modify: `web/src/composables/useAppMode.ts`
- Test: `web/src/composables/__tests__/useAppMode.test.ts`

- [ ] **Step 1: 改写实现**

把文件内容替换为：

```ts
import { ref } from 'vue'
import { isNativeApp } from '@/utils/clawbenchNative'

// Module-level singleton — all consumers share the same state
const isAppMode = ref(false)
let initialized = false

/**
 * Detects if the app is running inside a native host (top-level frame).
 * Top-frame check is critical: a child iframe inherits the bridge but must run
 * in web mode (no port forward button, no native auto-login, etc.).
 */
export function useAppMode() {
  if (!initialized) {
    initialized = true
    try {
      if (window !== window.top) return { isAppMode }
      isAppMode.value = isNativeApp()
    } catch {
      // window.top access may throw in cross-origin iframe — treat as web mode
    }
    if (isAppMode.value) {
      document.documentElement.setAttribute('data-app-mode', '')
    }
  }
  return { isAppMode }
}
```

- [ ] **Step 2: 更新测试**

打开 `web/src/composables/__tests__/useAppMode.test.ts`，将其中所有 `(window as any).AndroidNative` / `window.AndroidNative` 的赋值替换为 `(window as any).ClawBenchNative`。`isNativeApp()` 仍返回 boolean，断言不变。

- [ ] **Step 3: 运行测试确认通过**

Run: `cd web && npx vitest run src/composables/__tests__/useAppMode.test.ts`
Expected: PASS。

- [ ] **Step 4: 提交**

```bash
git add web/src/composables/useAppMode.ts web/src/composables/__tests__/useAppMode.test.ts
git commit -m "refactor(web): useAppMode reads platform-neutral ClawBenchNative bridge"
```

---

### Task 3: `appLog.ts` 改用包装器

**Files:**
- Modify: `web/src/utils/appLog.ts`
- Test: `web/src/utils/__tests__/appLog.test.ts`

- [ ] **Step 1: 改写实现**

`relayToNative` 与 `isNativeApp` 改用包装器。把以下两处替换：

替换 `relayToNative` 函数体（第 37-49 行）为：

```ts
function relayToNative(level: string, tag: string, args: unknown[]): void {
  try {
    const native = getNative()
    if (!native || !native.log) return
    // Top-frame check to avoid iframe false positives
    if (window !== window.top) return
    const msg = args.map(safeStringify).join(' ')
    native.log(level, tag, msg)
  } catch {
    // bridge not available — silent
  }
}
```

替换 `isNativeApp` 函数体（第 57-64 行）为：

```ts
function isNativeApp(): boolean {
  try {
    return nativeBridgeIsNativeApp()
  } catch {
    return false
  }
}
```

在文件顶部 import 区加入：

```ts
import { getNative, isNativeApp as nativeBridgeIsNativeApp } from '@/utils/clawbenchNative'
```

注意：`isNativeApp()`（第 57 行局部函数）与 import 的 `isNativeApp` 冲突，故 import 用别名 `nativeBridgeIsNativeApp`。

- [ ] **Step 2: 更新测试**

打开 `web/src/utils/__tests__/appLog.test.ts`，把 setup/teardown 中的 `(window as any).AndroidNative` 全部替换为 `(window as any).ClawBenchNative`。注意该文件用一个 `fakeNative` 对象含 `{ log, isNativeApp: () => true }`，仅需改名键名。

- [ ] **Step 3: 运行测试确认通过**

Run: `cd web && npx vitest run src/utils/__tests__/appLog.test.ts`
Expected: PASS。

- [ ] **Step 4: 提交**

```bash
git add web/src/utils/appLog.ts web/src/utils/__tests__/appLog.test.ts
git commit -m "refactor(web): appLog relays via ClawBenchNative bridge wrapper"
```

---

### Task 4: `useServerList.ts` 改为异步 + 包装器

**Files:**
- Modify: `web/src/composables/useServerList.ts`
- Test: `web/src/composables/__tests__/useServerList.test.ts`

- [ ] **Step 1: 改写实现**

把 `getNative()` 改为 import 包装器，`load`/`save`/`remove` 改 `async` + `await`。替换第 10-18 行的 `getNative` 定义与第 40-91 行的 `useServerList` 函数：

删除文件内自定义 `getNative`（第 10-18 行）与相关类型注释，顶部加：

```ts
import { getNative } from '@/utils/clawbenchNative'
```

将 `useServerList` 替换为：

```ts
export function useServerList() {
  const servers = ref<ServerEntry[]>([])

  async function load() {
    const native = getNative()
    if (native?.getServerList) {
      const json = await native.getServerList()
      servers.value = json ? parseList(json) : []
    } else {
      // Fallback: localStorage (web mode, single-origin only)
      const raw = localStorage.getItem(STORAGE_KEY)
      servers.value = raw ? parseList(raw) : []
    }
  }

  async function save(url: string, password: string) {
    const native = getNative()
    if (native?.saveServer) {
      await native.saveServer(url, password)
    } else {
      const list = parseList(localStorage.getItem(STORAGE_KEY) || '[]')
      const idx = list.findIndex(e => e.url === url)
      if (idx >= 0) {
        list[idx].password = password
      } else {
        list.push({ url, password })
      }
      localStorage.setItem(STORAGE_KEY, JSON.stringify(list))
    }
    await load()
  }

  async function remove(url: string) {
    const native = getNative()
    if (native?.removeServer) {
      await native.removeServer(url)
    } else {
      const list = parseList(localStorage.getItem(STORAGE_KEY) || '[]')
      localStorage.setItem(STORAGE_KEY, JSON.stringify(list.filter(e => e.url !== url)))
    }
    await load()
  }

  function getPassword(url: string): string {
    return servers.value.find(e => e.url === url)?.password || ''
  }

  return { servers, load, save, remove, getPassword }
}
```

- [ ] **Step 2: 更新测试**

打开 `web/src/composables/__tests__/useServerList.test.ts`。测试里 `load()`/`save()`/`remove()` 现在返回 Promise——给调用点加 `await`。把 `(window as any).AndroidNative` 改名为 `(window as any).ClawBenchNative`。若测试用 `await loadServers()` 形式已同步则无需改断言；若用 `flushPromises()` 需改为 `await`。

- [ ] **Step 3: 运行测试确认通过**

Run: `cd web && npx vitest run src/composables/__tests__/useServerList.test.ts`
Expected: PASS。

- [ ] **Step 4: 提交**

```bash
git add web/src/composables/useServerList.ts web/src/composables/__tests__/useServerList.test.ts
git commit -m "refactor(web): useServerList async via ClawBenchNative wrapper"
```

---

### Task 5: `usePortForward.ts` 异步读 + 包装器 + 移除范围外调用

**Files:**
- Modify: `web/src/composables/usePortForward.ts`
- Test: `web/src/composables/__tests__/usePortForward.test.ts`

- [ ] **Step 1: 移除本地 bridge 类型与辅助，改用包装器**

删除第 13-16 行（`RECONNECT_ASYNC_TIMEOUT_MS`/`RECONNECT_CALLBACK_NAME` 常量，逻辑已移入包装器）、第 18-46 行（`reconnectTunnelAsync` 函数）、第 65-84 行（`AndroidNativeBridge` 接口与 `getAndroidNative` 函数）。

顶部 import 加入：

```ts
import { getNative, reconnectTunnel as nativeReconnectTunnel } from '@/utils/clawbenchNative'
```

`usePortForward` 内 `const { isAppMode } = useAppMode()` 保持不变（`isAppMode` 语义沿用）。

- [ ] **Step 2: 改写各 native 调用点**

将文件内所有 `getAndroidNative()` 调用替换为 `getNative()`（函数内已有 `const native = getNative()` 的保持）。逐一处理：

- `registerPort`（第 244-246 行）：`getNative()?.addForwardedPort?.(localPort, port, host || '')`（已是 fire-and-forget，无需 await）
- `updatePort`（第 258-259 行）：`getNative()?.removeForwardedPort?.(localPort)` 与 `getNative()?.addForwardedPort?.(localPort, port, host || '')`
- `unregisterPort`（第 267 行）：`getNative()?.removeForwardedPort?.(localPort)`
- `setPortEnabled`（第 291-296 行）：`const native = getNative()`
- `syncToNative`（第 323-356 行）：`getForwardedPorts()` 现返回 Promise，改为：

```ts
  async function syncToNative() {
    if (!isAppMode.value) return
    await loadPorts()
    const native = getNative()
    if (!native) return

    const enabledPorts = ports.value.filter(p => p.enabled)
    if (enabledPorts.length === 0) {
      // No enabled ports on server — nothing to reconcile (stopBackgroundService removed)
      return
    }

    const enabledLocalPorts = new Set(enabledPorts.map(p => p.localPort))

    if (typeof native.getForwardedPorts === 'function') {
      try {
        const current: Array<{ port?: number; host?: string }> = JSON.parse((await native.getForwardedPorts()) || '[]')
        for (const item of current) {
          const lp = item && item.port
          if (lp && !enabledLocalPorts.has(lp)) {
            native.removeForwardedPort?.(lp)
          }
        }
      } catch {
        // Ignore parse errors — reconciliation is best-effort.
      }
    }

    for (const p of enabledPorts) {
      native.addForwardedPort?.(p.localPort, p.port, p.host || '')
    }
  }
```

- `getNativeTunnelStatus`（第 457-468 行）改异步：

```ts
  async function getNativeTunnelStatus(): Promise<boolean | null> {
    if (!isAppMode.value) return null
    const native = getNative()
    if (!native || typeof native.isTunnelConnected !== 'function') return null
    try {
      const result = await native.isTunnelConnected()
      return typeof result === 'boolean' ? result : null
    } catch {
      return null
    }
  }
```

- `getNativeTunnelError`（第 474-484 行）改异步：

```ts
  async function getNativeTunnelError(): Promise<string> {
    if (!isAppMode.value) return ''
    const native = getNative()
    if (!native || typeof native.getTunnelError !== 'function') return ''
    try {
      const result = await native.getTunnelError()
      return typeof result === 'string' ? result : ''
    } catch {
      return ''
    }
  }
```

- `getNativeTunnelErrorType`（第 490-503 行）改异步：

```ts
  async function getNativeTunnelErrorType(): Promise<TunnelErrorType> {
    if (!isAppMode.value) return ''
    const native = getNative()
    if (!native || typeof native.getTunnelErrorType !== 'function') return ''
    try {
      const result = await native.getTunnelErrorType()
      if (typeof result === 'string' && ['auth', 'network', 'hostkey', 'unknown', ''].includes(result)) {
        return result as TunnelErrorType
      }
      return ''
    } catch {
      return ''
    }
  }
```

- `checkTunnelHealth`（第 386-413 行）中调用点改 `await`：

```ts
    if (isAppMode.value) {
      const nativeConnected = await getNativeTunnelStatus()
      if (nativeConnected === true) {
        // ... 原逻辑不变
      } else if (nativeConnected === false) {
        tunnelError.value = await getNativeTunnelError()
        tunnelErrorType.value = await getNativeTunnelErrorType()
        // ... 原逻辑不变
      }
    }
```

- `startTunnelPoll`（第 508-511 行）中 `const nativeConnected = getNativeTunnelStatus()` 改为 `const nativeConnected = await getNativeTunnelStatus()`。

- `doOpen`（第 562-568 行）参数类型 `AndroidNativeBridge` 改为 `ClawBenchNative | undefined`（import `ClawBenchNative` 类型）：

```ts
  import type { ClawBenchNative } from '@/utils/clawbenchNative'
  function doOpen(native: ClawBenchNative | undefined, localPort: number, protocol?: string, hostArg?: string, path?: string) {
    if (native?.openInSandbox) {
      native.openInSandbox(localPort, protocol === 'https' ? 'https' : 'http', hostArg || '', path || '', currentSessionId.value || '')
    } else if (native?.openInBrowser) {
      native.openInBrowser(localPort, protocol === 'https' ? 'https' : 'http', hostArg || '', path || '')
    }
  }
```

- `openPortWithCheck`（第 593-630 行）中同步读 `testPortReachable` 改 `await`。把三处 `native.testPortReachable(...)` 改为 `await native.testPortReachable(...)`，`reconnectTunnelAsync(native)` 改为 `await nativeReconnectTunnel()`：

```ts
    const native = getNative()
    const hostArg = host || ''

    if (native?.testPortReachable) {
      if (connectingPorts.value.has(localPort)) {
        doOpen(native, localPort, protocol, hostArg, path)
        return
      }
      if (await native.testPortReachable(localPort)) {
        doOpen(native, localPort, protocol, hostArg, path)
        return
      }
      const reconnected = await nativeReconnectTunnel()
      const toast = useToast()
      if (reconnected && (await native.testPortReachable(localPort))) {
        toast.show(gt('portForward.tunnelReconnected'), { icon: '🔗', type: 'success' })
        doOpen(native, localPort, protocol, hostArg, path)
        return
      }
      toast.show(gt('portForward.portUnreachable'), { icon: '🚫', type: 'error' })
      return
    }

    doOpen(native, localPort, protocol, hostArg, path)
```

- `reconnectPort`（第 637-670 行）中三处 `native.testPortReachable(...)` 改 `await`，`reconnectTunnelAsync(native)` 改 `await nativeReconnectTunnel()`。

- `openPort`/`openInExternalBrowser` 不变（`doOpen`/`openInBrowser` 保持 fire-and-forget）。

- [ ] **Step 3: 更新测试**

打开 `web/src/composables/__tests__/usePortForward.test.ts`：
- 把 `(window as any).AndroidNative` / `window.AndroidNative` 改名 `ClawBenchNative`。
- mock 的 `getForwardedPorts`/`testPortReachable`/`isTunnelConnected`/`getTunnelError`/`getTunnelErrorType` 若返回同步值，包成 `Promise.resolve(...)` 或让 `await` 吸收（同步值 await 也兼容，可不改）；但断言前需 `await` 内部调用点（测试自身是否 await 视调用方式）。
- 若测试断言 `stopBackgroundService` 被调用：删除该断言（已移除）。
- 用 `flushPromises()`/`await` 保证异步完成后再断言。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd web && npx vitest run src/composables/__tests__/usePortForward.test.ts`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add web/src/composables/usePortForward.ts web/src/composables/__tests__/usePortForward.test.ts
git commit -m "refactor(web): usePortForward async native reads + remove out-of-scope bridge calls"
```

---

### Task 6: 其余 composables 改用包装器

**Files:**
- Modify: `web/src/composables/useWakeLock.ts`
- Modify: `web/src/composables/useSettingsConfig.ts`
- Modify: `web/src/composables/useGlobalEvents.ts`
- Test: `web/src/composables/__tests__/useWakeLock.test.ts`、`web/src/composables/__tests__/useSettingsConfig.test.ts`

- [ ] **Step 1: `useWakeLock.ts`**

`_doAcquire` 与 `release` 中 `(window as any).AndroidNative` 改用 `getNative()`。替换第 56-68 行为：

```ts
  // 2. Native bridge (setKeepScreenOn) — extra safety in WebView
  try {
    const native = getNative()
    if (native?.setKeepScreenOn) {
      native.setKeepScreenOn(true)
      appLog.i(TAG, 'Native setKeepScreenOn(true)')
      if (!held.value) held.value = true
    } else if (native) {
      appLog.w(TAG, 'Native bridge exists but setKeepScreenOn method missing — host needs update')
    }
  } catch { /* not in app mode */ }
```

替换第 93-101 行为：

```ts
  // Native bridge
  try {
    const native = getNative()
    if (native?.setKeepScreenOn) {
      native.setKeepScreenOn(false)
      appLog.i(TAG, 'Native setKeepScreenOn(false)')
    }
  } catch { /* not in app mode */ }
```

顶部 import：`import { getNative } from '@/utils/clawbenchNative'`。

- [ ] **Step 2: `useSettingsConfig.ts`**

`syncPushModeToNative`（第 409-415 行）改用 `getNative()`：

```ts
  function syncPushModeToNative() {
    try {
      const pushMode = serverConfig.value.push_mode as string || 'native'
      getNative()?.setNativePushEnabled?.(pushMode === 'native')
    } catch { /* not in app mode */ }
  }
```

顶部 import：`import { getNative } from '@/utils/clawbenchNative'`。

- [ ] **Step 3: `useGlobalEvents.ts`**

两处 `;(window as any).AndroidNative?.updateLastSeenEventId(...)`（第 164、253 行）改为：

```ts
            getNative()?.updateLastSeenEventId(latestId)
```

与

```ts
                            getNative()?.updateLastSeenEventId(msg.id)
```

顶部 import：`import { getNative } from '@/utils/clawbenchNative'`。

- [ ] **Step 4: 更新测试**

`web/src/composables/__tests__/useWakeLock.test.ts`、`web/src/composables/__tests__/useSettingsConfig.test.ts` 中 `(window as any).AndroidNative` 改名 `ClawBenchNative`。`setKeepScreenOn`/`setNativePushEnabled` 保持同步 void，断言不变。

- [ ] **Step 5: 运行相关测试**

Run: `cd web && npx vitest run src/composables/__tests__/useWakeLock.test.ts src/composables/__tests__/useSettingsConfig.test.ts`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add web/src/composables/useWakeLock.ts web/src/composables/useSettingsConfig.ts web/src/composables/useGlobalEvents.ts web/src/composables/__tests__/useWakeLock.test.ts web/src/composables/__tests__/useSettingsConfig.test.ts
git commit -m "refactor(web): useWakeLock/useSettingsConfig/useGlobalEvents via ClawBenchNative"
```

---

### Task 7: 组件层改造（改名 + await + 移除范围外调用）

**Files (Modify):**
- `web/src/App.vue`
- `web/src/components/LoginView.vue`
- `web/src/components/common/AppHeader.vue`
- `web/src/components/file/FileHeader.vue`
- `web/src/components/file/FileViewer.vue`
- `web/src/components/file/FileManagerContent.vue`
- `web/src/components/VersionMismatchOverlay.vue`
- `web/src/components/terminal/TerminalPanelContent.vue`
- `web/src/components/settings/SettingsCategory.vue`
- `web/src/components/settings/settingsFieldMap.ts`

- [ ] **Step 1: `App.vue`**

顶部 import：`import { getNative } from '@/utils/clawbenchNative'`（如已 import 其它包装器用现有）。

- 第 1096 行：`window.AndroidNative?.startLogCapture?.()` 改为 `getNative()?.startLogCapture?.()`（fire-and-forget，不 await）。
- 第 1913 行：删除整行（`dismissSplash` 已移除，范围外）。
- 第 1995-2001 行（登录后自动用保存密码）：`getPassword` 改异步。替换为：

```ts
            if (isAppMode.value) {
                const savedPwd = await getNative()?.getPassword?.()
                if (savedPwd) {
                    await getNative()?.setSSHPassword?.(savedPwd)
                    // ...原后续逻辑不变
                }
            }
```

  注意：该代码处于 async 上下文中（`onMounted` 或事件处理），确保调用处已 `async`；若所在函数非 async，包成 `void (async () => { ... })()` 或确认外层 async。
- 第 2079-2086 行（冷启动待处理导航）：`getPendingNavigation` 改异步。替换为：

```ts
    if (isAppMode.value && getNative()?.getPendingNavigation) {
      const nav = await getNative()?.getPendingNavigation()
      if (nav) {
        appLog.d(TAG, 'getPendingNavigation result:', nav)
        // ...原处理逻辑不变
      }
    }
```

  确认该段处于 async 上下文。

- [ ] **Step 2: `LoginView.vue`**

顶部 import：`import { getNative } from '@/utils/clawbenchNative'`。

- 第 192-193、261-262 行：`window.AndroidNative?.connectToServer(...)` 改为 `getNative()?.connectToServer?.(...)`（fire-and-forget）。
- 第 217-218 行：`window.AndroidNative?.isNativeApp?.()` 改为 `getNative()?.isNativeApp?.()`；`window.AndroidNative.setSSHPassword(...)` 改为 `getNative()?.setSSHPassword?.(...)`（fire-and-forget）。
- 第 290-291 行：`window.AndroidNative?.showServerDialog` 改为 `getNative()?.showServerDialog?.()`。
- `useServerList` 返回的 `load/save/remove` 现在是 async：找到调用 `loadServers()`/`saveServer()`/`removeServer()` 处加 `await`（或 `.catch(()=>{})` 的 fire-and-forget，视上下文）。若在事件处理器中，包 `void loadServers()`。

- [ ] **Step 3: `AppHeader.vue`**

第 648-649 行：`(window as unknown as { AndroidNative?... }).AndroidNative` 改用 `getNative()`：

```ts
    if (getNative()?.showServerDialog) {
        getNative()?.showServerDialog()
    }
```

顶部 import：`import { getNative } from '@/utils/clawbenchNative'`。

- [ ] **Step 4: `FileHeader.vue` 与 `FileViewer.vue`**

第 393 / 713 行 `const native = window.AndroidNative` 改为：

```ts
    const native = getNative()
```

顶部 import：`import { getNative } from '@/utils/clawbenchNative'`。这两处 `native` 后续用于 `downloadFile`/`shareFile`（已由 `download.ts` 处理，确认后删掉冗余局部即可——若 `native` 仅用于 `downloadFile`，直接删，改用 `downloadFileByPath`）。

- [ ] **Step 5: `FileManagerContent.vue`**

文件中 `(window as any).AndroidNative` 出现处（`shareFile`/`shareFiles`）改用 `getNative()`。顶部 import 加入 `getNative`。`shareFile`/`shareFiles` 现在返回 Promise，若代码 await 则保留，否则 fire-and-forget 可加 `void`。

- [ ] **Step 6: `VersionMismatchOverlay.vue`**

第 49-50 行 `getAppVersion` 改异步：

```ts
    const native = getNative()
    if (!native?.getAppVersion) return ''
    return (await native.getAppVersion()) || ''
```

使所在函数 `async`。顶部 import：`import { getNative } from '@/utils/clawbenchNative'`。

- [ ] **Step 7: `TerminalPanelContent.vue`**

删除所有 native 调用（`setVolumeKeyMode`、`setTerminalSessionCount` 均范围外）。删除第 546、552、569 行整段 `const native = (window as unknown as { AndroidNative?... }).AndroidNative` 及其调用块，保留函数其它逻辑（若有空函数体，保留函数体为空或删除函数与调用点）。

- [ ] **Step 8: `SettingsCategory.vue`**

- 第 199-200 行 `getAppVersion` 改异步：

```ts
      const native = getNative()
      if (native?.getAppVersion) return (await native.getAppVersion()) ?? '-'
```

  使所在函数 `async`。
- 第 227、232 行：`startLogCapture`/`stopLogCapture` 改用 `getNative()?.startLogCapture?.()` / `getNative()?.stopLogCapture?.()`（fire-and-forget）。
- 第 252 行：`showServerDialog` 改用 `getNative()?.showServerDialog?.()`。
- 顶部 import：`import { getNative } from '@/utils/clawbenchNative'`。

- [ ] **Step 9: `settingsFieldMap.ts`**

第 208-209 行 `setNativePushEnabled` 改用 `getNative()`：

```ts
            getNative()?.setNativePushEnabled?.(values?.push_mode === 'native')
```

顶部 import：`import { getNative } from '@/utils/clawbenchNative'`。

- [ ] **Step 10: 运行类型检查**

Run: `npm run typecheck`
Expected: 无 TS 错误。若有残留 `AndroidNative` 或未 await 的 Promise 报错，逐一修复。

- [ ] **Step 11: 提交**

```bash
git add web/src/App.vue web/src/components/LoginView.vue web/src/components/common/AppHeader.vue web/src/components/file/FileHeader.vue web/src/components/file/FileViewer.vue web/src/components/file/FileManagerContent.vue web/src/components/VersionMismatchOverlay.vue web/src/components/terminal/TerminalPanelContent.vue web/src/components/settings/SettingsCategory.vue web/src/components/settings/settingsFieldMap.ts
git commit -m "refactor(web): components use ClawBenchNative; remove out-of-scope native calls"
```

---

### Task 8: 更新剩余测试文件并全局清除 AndroidNative

**Files (Modify):**
- `web/src/components/__tests__/appHeader.test.ts`
- `web/src/components/file/__tests__/FileManagerContent.test.ts`
- `web/src/components/settings/__tests__/SettingsCategory.test.ts`
- `web/src/utils/__tests__/download.test.ts`

- [ ] **Step 1: 全局替换**

```bash
cd web/src && rg -l "AndroidNative" --glob '*.{ts,vue}' | xargs sed -i 's/AndroidNative/ClawBenchNative/g'
```

- [ ] **Step 2: 处理异步化导致的测试差异**

对 `download.test.ts`：mock `downloadUrl`/`downloadBlob` 若现在返回 Promise，保持断言（调用为 fire-and-forget）不变；若测试 await 了调用，检查 mock 返回 Promise。

对 `appHeader.test.ts`：mock `showServerDialog` 为 `vi.fn()`，断言不变。

对 `FileManagerContent.test.ts`：mock `shareFile`/`shareFiles` 改为 `vi.fn()` 或返回 `Promise.resolve()`，断言不变。

对 `SettingsCategory.test.ts`：`getAppVersion` mock 改返回 `Promise.resolve('1.0.0')`，测试断言处加 `await`。

- [ ] **Step 3: 确认无残留**

Run: `cd web/src && rg -n "AndroidNative" || echo "no residual"`

Expected: 无输出（或仅注释里的历史说明）。

- [ ] **Step 4: 运行全量前端测试**

Run: `cd web && npx vitest run`
Expected: 全部 PASS。若个别因异步时序失败，用 `await`/`flushPromises()` 修正。

- [ ] **Step 5: 提交**

```bash
git add -A web/src
git commit -m "test(web): migrate bridge tests to ClawBenchNative async contract"
```

---

### Task 9: 全量校验

**Files:** 无新增（验证命令）。

- [ ] **Step 1: 类型检查 + lint + 前端测试**

Run:
```bash
npm run typecheck
cd web && npm run lint
cd web && npx vitest run
```

Expected: typecheck 无错误、lint 无 error、vitest 全 PASS。

- [ ] **Step 2: 构建前端确认契约可用**

Run: `npm run build`
Expected: 构建成功。

- [ ] **Step 3: 提交收尾（如有 lint/format 修复）**

```bash
git add -A && git commit -m "chore(web): final lint/format for ClawBenchNative bridge migration" || echo "nothing to commit"
```

---

## 自检对照（spec → task）

| Spec §4 要求 | Task |
|---|---|
| 新建 `ClawBenchNative` 契约 + 平台无关检测 | Task 1 |
| `useAppMode` 检测原生模式 | Task 2 |
| `appLog` 中继改用桥 | Task 3 |
| `useServerList` 同步→异步 | Task 4 |
| `usePortForward` 同步读→异步 + 移除 `stopBackgroundService` | Task 5 |
| `useWakeLock`/`useSettingsConfig`/`useGlobalEvents` 改用桥 | Task 6 |
| 组件层改名 + await + 移除 `setVolumeKeyMode`/`setTerminalSessionCount`/`dismissSplash` | Task 7 |
| 测试全量迁移 + 全局清除 `AndroidNative` | Task 8 |
| typecheck / lint / build 全绿 | Task 9 |

## 后续

本计划完成后，按依赖顺序进入：**Plan 2（Android 改名）**、**Plan 3（Electron 客户端）**、**Plan 4（下载入口）**。每份单独成文。
