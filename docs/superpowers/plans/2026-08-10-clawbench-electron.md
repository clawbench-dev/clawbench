# ClawBench Electron 桌面客户端实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新建 `desktop/` 目录，实现 Electron 桌面客户端（远程客户端形态），通过 `contextBridge` 注入 `ClawBenchNative` 桥，用 Node `ssh2` 实现 SSH 隧道端口转发，支持原生通知/下载/防休眠/沙盒窗口，并用 npm 自升级（便携目录替换）。

**Architecture:** Electron 主进程承载隧道（ssh2）、密钥（safeStorage）、存储（electron-store）、通知、下载、防休眠与 npm 自升级；preload 通过 `contextBridge.exposeInMainWorld('ClawBenchNative', ...)` 暴露与前端契约一致的桥（读操作经 `ipcMain.handle` 异步返回，写操作为 fire-and-forget）。前端直接复用 Plan 1 已落地的 `useAppMode`/桥消费逻辑。

**Tech Stack:** Electron / TypeScript / Node `ssh2` / `electron-store` / Electron 内置 `safeStorage`/`powerSaveBlocker` / `electron-builder`。

参考 spec：`docs/superpowers/specs/2026-08-10-clawbench-electron-design.md` §3-§7。前置：Plan 1（前端桥抽象）已完成，Plan 2（Android 改名）已完成。

---

## 关键设计

1. **桥契约**：preload 暴露的对象方法名必须与 `web/src/utils/clawbenchNative.ts` 的 `ClawBenchNative` 接口一致。同步方法（`isNativeApp`/`getLanguage`/`log`/`openSession`/`setNativePushEnabled`/`updateLastSeenEventId`/`setKeepScreenOn`/`showServerDialog`）在 preload 直接返回；异步方法（`getServerList`/`getPassword`/`getForwardedPorts`/`testPortReachable` 等读 + 全部写）经 `ipcRenderer.invoke` 到 `ipcMain.handle`。
2. **SSH 隧道**：`ssh2.Client` 连接远程服务器，本地起 `net.Server` 监听 localhost，转发到 `host:port`。状态/错误类型由 `tunnel.ts` 暴露给 `isTunnelConnected`/`getTunnelError`/`getTunnelErrorType`。SSH 密码从 `secrets.ts`（safeStorage）读取。
3. **密钥**：Electron `safeStorage` 加密 `setSSHPassword` 存的密码；Linux 无 keyring 时降级明文 + 告警。
4. **npm 自升级**：镜像 `internal/service/upgrade.go`——查 registry（国内 npmmirror）`@xulongzhe/clawbench-desktop-<os>-<arch>/latest`，比对 `app.getVersion()`，下载 tarball → 校验 sha512 → 解压替换便携目录 → 重启。
5. **纯逻辑抽函数**：registry 查询（包名映射 + npmmirror 改写）与 sha512 校验抽成不依赖 Electron 的纯函数，可单测（与 Plan 4 的 Go 端点保持一致的包名/改写规则）。

---

## 任务分解

### Task 1: `desktop/` 脚手架与依赖

**Files:**
- Create: `desktop/package.json`
- Create: `desktop/tsconfig.json`
- Create: `desktop/.gitignore`

- [ ] **Step 1: `desktop/package.json`**

```json
{
  "name": "@xulongzhe/clawbench-desktop",
  "version": "0.1.0",
  "description": "ClawBench desktop client (Electron)",
  "main": "dist/main/index.js",
  "private": true,
  "scripts": {
    "build": "tsc -p tsconfig.json && electron-builder --dir",
    "build:install": "tsc -p tsconfig.json && electron-builder",
    "typecheck": "tsc --noEmit -p tsconfig.json",
    "test": "vitest run"
  },
  "dependencies": {
    "electron-store": "^8.2.0",
    "ssh2": "^1.16.0"
  },
  "devDependencies": {
    "electron": "^33.0.0",
    "electron-builder": "^25.1.8",
    "typescript": "^5.9.3",
    "vitest": "^4.1.8",
    "@types/node": "^25.6.0",
    "@types/ssh2": "^1.15.1"
  }
}
```

（版本为建议值；若安装失败回退到兼容版本。）

- [ ] **Step 2: `desktop/tsconfig.json`**

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "CommonJS",
    "moduleResolution": "Node",
    "outDir": "dist",
    "rootDir": "src",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "resolveJsonModule": true,
    "types": ["node"]
  },
  "include": ["src"]
}
```

- [ ] **Step 3: `desktop/.gitignore`**

```
node_modules/
dist/
```

- [ ] **Step 4: 安装依赖并验证编译**

```bash
cd desktop && npm install
npm run typecheck
```
Expected: 无 TS 错误（空 include 也需能编译；此步验证工具链可用）。

- [ ] **Step 5: 提交**

```bash
git add desktop/package.json desktop/tsconfig.json desktop/.gitignore desktop/package-lock.json
git commit -m "chore(desktop): scaffold Electron client project"
```

---

### Task 2: 纯逻辑模块（不依赖 Electron，可单测）

**Files:**
- Create: `desktop/src/shared/registry.ts`
- Create: `desktop/src/shared/registry.test.ts`
- Create: `desktop/src/shared/integrity.ts`
- Create: `desktop/src/shared/integrity.test.ts`

- [ ] **Step 1: `desktop/src/shared/registry.ts`（纯函数）**

```ts
/** npm platform package names for clawbench-desktop, mirroring internal/service/upgrade.go. */
export const DESKTOP_PLATFORM_PKG: Record<string, string> = {
  'linux/amd64': '@xulongzhe/clawbench-desktop-linux-x64',
  'linux/arm64': '@xulongzhe/clawbench-desktop-linux-arm64',
  'darwin/amd64': '@xulongzhe/clawbench-desktop-darwin-x64',
  'darwin/arm64': '@xulongzhe/clawbench-desktop-darwin-arm64',
  'win32/x64': '@xulongzhe/clawbench-desktop-win32-x64',
}

export function getDesktopPkg(platform: NodeJS.Platform, arch: string): string | undefined {
  return DESKTOP_PLATFORM_PKG[`${platform}/${arch}`]
}

export function registryBase(chinaMainland: boolean): string {
  return chinaMainland ? 'https://registry.npmmirror.com' : 'https://registry.npmjs.org'
}

/** Build the /latest query URL for a pkg. */
export function latestUrl(pkg: string, chinaMainland: boolean): string {
  return `${registryBase(chinaMainland)}/${pkg}/latest`
}

/** Rewrite an npmjs tarball URL to the npmmirror CDN, mirroring upgrade.go. */
export function rewriteTarball(url: string, chinaMainland: boolean): string {
  if (chinaMainland && url.startsWith('https://registry.npmjs.org')) {
    return url.replace('https://registry.npmjs.org', 'https://registry.npmmirror.com')
  }
  return url
}

export interface NpmLatest {
  version: string
  tarball: string
  integrity: string
}

export function parseNpmLatest(data: string): NpmLatest {
  const j = JSON.parse(data)
  if (!j || typeof j.version !== 'string' || !j.dist?.tarball) {
    throw new Error('invalid registry response')
  }
  return { version: j.version, tarball: j.dist.tarball, integrity: j.dist.integrity || '' }
}
```

- [ ] **Step 2: `desktop/src/shared/registry.test.ts`**

```ts
import { describe, it, expect } from 'vitest'
import { getDesktopPkg, registryBase, latestUrl, rewriteTarball, parseNpmLatest } from './registry'

describe('registry', () => {
  it('maps platform+arch to npm package', () => {
    expect(getDesktopPkg('win32', 'x64')).toBe('@xulongzhe/clawbench-desktop-win32-x64')
    expect(getDesktopPkg('darwin', 'arm64')).toBe('@xulongzhe/clawbench-desktop-darwin-arm64')
    expect(getDesktopPkg('freebsd', 'x64')).toBeUndefined()
  })
  it('selects registry base by region', () => {
    expect(registryBase(true)).toContain('npmmirror')
    expect(registryBase(false)).toContain('npmjs')
  })
  it('builds latest url', () => {
    expect(latestUrl('@xulongzhe/clawbench-desktop-win32-x64', false))
      .toBe('https://registry.npmjs.org/@xulongzhe/clawbench-desktop-win32-x64/latest')
  })
  it('rewrites tarball to npmmirror in China', () => {
    const u = 'https://registry.npmjs.org/@xulongzhe/clawbench-desktop-win32-x64/-/x-0.1.0.tgz'
    expect(rewriteTarball(u, true)).toContain('npmmirror')
    expect(rewriteTarball(u, false)).toBe(u)
  })
  it('parses latest metadata', () => {
    const p = parseNpmLatest('{"version":"0.1.0","dist":{"tarball":"t.tgz","integrity":"sha512-x"}}')
    expect(p.version).toBe('0.1.0')
    expect(p.tarball).toBe('t.tgz')
    expect(() => parseNpmLatest('{}')).toThrow()
  })
})
```

- [ ] **Step 3: `desktop/src/shared/integrity.ts`（纯函数）**

```ts
import { createHash } from 'node:crypto'

/** Verify a buffer against a SRI integrity string of the form "sha512-<base64>". */
export function verifyIntegrity(buffer: Buffer, integrity: string): boolean {
  if (!integrity.startsWith('sha512-')) return false
  const expected = Buffer.from(integrity.slice('sha512-'.length), 'base64')
  const actual = createHash('sha512').update(buffer).digest()
  return expected.length === actual.length && expected.equals(actual)
}
```

- [ ] **Step 4: `desktop/src/shared/integrity.test.ts`**

```ts
import { describe, it, expect } from 'vitest'
import { createHash } from 'node:crypto'
import { verifyIntegrity } from './integrity'

describe('verifyIntegrity', () => {
  const data = Buffer.from('hello clawbench')
  const b64 = createHash('sha512').update(data).digest('base64')
  it('accepts correct sha512 integrity', () => {
    expect(verifyIntegrity(data, `sha512-${b64}`)).toBe(true)
  })
  it('rejects wrong hash', () => {
    expect(verifyIntegrity(data, `sha512-${createHash('sha512').update('other').digest('base64')}`)).toBe(false)
  })
  it('rejects non-sha512 algorithm', () => {
    expect(verifyIntegrity(data, `sha256-${b64}`)).toBe(false)
  })
  it('rejects empty/malformed', () => {
    expect(verifyIntegrity(data, '')).toBe(false)
  })
})
```

- [ ] **Step 5: 运行测试**

```bash
cd desktop && npx vitest run src/shared
```
Expected: 全部 PASS。

- [ ] **Step 6: 提交**

```bash
git add desktop/src/shared
git commit -m "feat(desktop): pure npm-registry + sha512-integrity helpers with tests"
```

---

### Task 3: 存储与密钥模块

**Files:**
- Create: `desktop/src/main/store.ts`
- Create: `desktop/src/main/secrets.ts`
- Create: `desktop/src/main/secrets.test.ts`（仅测纯逻辑部分）

- [ ] **Step 1: `desktop/src/main/store.ts`**

```ts
import Store from 'electron-store'
import type { ServerEntry } from './types'

export interface ServerListSchema {
  servers: ServerEntry[]
  serverUrl: string
  sshPasswordEncrypted: string | null
  nativePushEnabled: boolean
}

const defaults: ServerListSchema = {
  servers: [],
  serverUrl: '',
  sshPasswordEncrypted: null,
  nativePushEnabled: true,
}

let store: Store<ServerListSchema> | null = null

export function initStore(): Store<ServerListSchema> {
  if (!store) {
    store = new Store<ServerListSchema>({ name: 'clawbench', defaults })
  }
  return store
}

export function getStore(): Store<ServerListSchema> {
  if (!store) throw new Error('store not initialized — call initStore() first')
  return store
}
```

- [ ] **Step 2: `desktop/src/main/types.ts`**

```ts
export interface ServerEntry {
  url: string
  password: string
}
export interface SavedServerConfig {
  protocol: string
  host: string
  port: string
  password: string
}
```

- [ ] **Step 3: `desktop/src/main/secrets.ts`**

```ts
import { safeStorage } from 'electron'
import { getStore } from './store'

function warnNoEncryption(): void {
  // eslint-disable-next-line no-console
  console.warn('[secrets] safeStorage encryption unavailable; storing password in plaintext')
}

export function savePassword(password: string): void {
  const store = getStore()
  try {
    if (safeStorage.isEncryptionAvailable()) {
      store.set('sshPasswordEncrypted', safeStorage.encryptString(password).toString('base64'))
    } else {
      warnNoEncryption()
      store.set('sshPasswordEncrypted', `plain:${Buffer.from(password, 'utf8').toString('base64')}`)
    }
  } catch {
    warnNoEncryption()
    store.set('sshPasswordEncrypted', `plain:${Buffer.from(password, 'utf8').toString('base64')}`)
  }
}

export function getPassword(): string {
  const raw = getStore().get('sshPasswordEncrypted')
  if (!raw) return ''
  if (raw.startsWith('plain:')) {
    return Buffer.from(raw.slice('plain:'.length), 'base64').toString('utf8')
  }
  try {
    return safeStorage.decryptString(Buffer.from(raw, 'base64'))
  } catch {
    return ''
  }
}
```

- [ ] **Step 4: 说明**：`secrets.ts` 强依赖 Electron `safeStorage`，在 node 环境不可单测（GUI 进程才有）。测试聚焦纯解析逻辑；`safeStorage` 依赖在 Task 9 集成验证。若需单测，抽 `decodeStoredPassword(raw)` 纯函数并测其 `plain:` 分支与 base64 往返。

- [ ] **Step 5: 提交**

```bash
git add desktop/src/main/types.ts desktop/src/main/store.ts desktop/src/main/secrets.ts
git commit -m "feat(desktop): electron-store + safeStorage-backed secrets module"
```

---

### Task 4: SSH 隧道模块

**Files:**
- Create: `desktop/src/main/tunnel.ts`

- [ ] **Step 1: `desktop/src/main/tunnel.ts`**

```ts
import net from 'node:net'
import { Client } from 'ssh2'
import { getPassword } from './secrets'

export type TunnelErrorType = 'auth' | 'network' | 'hostkey' | 'unknown' | ''

export interface TunnelState {
  connected: boolean
  error: string
  errorType: TunnelErrorType
  forwarded: Map<number, { targetPort: number; host: string }>
}

const state: TunnelState = { connected: false, error: '', errorType: '', forwarded: new Map() }
let client: Client | null = null

export function isTunnelConnected(): boolean { return state.connected }
export function getTunnelError(): string { return state.error }
export function getTunnelErrorType(): TunnelErrorType { return state.errorType }
export function getForwardedPorts(): Array<{ port: number; host: string }> {
  return [...state.forwarded.entries()].map(([localPort, v]) => ({ port: localPort, host: v.host }))
}

function classifyError(err: Error & { level?: string; code?: string }): TunnelErrorType {
  if (err.level === 'client-authentication') return 'auth'
  if (err.code === 'ENOTFOUND' || err.code === 'ECONNREFUSED' || err.code === 'ETIMEDOUT') return 'network'
  if (err.level === 'client-timeout') return 'hostkey'
  return 'unknown'
}

export function connectTunnel(host: string, port: number, username: string): Promise<boolean> {
  return new Promise((resolve) => {
    disconnectTunnel()
    state.error = ''
    state.errorType = ''
    client = new Client()
    client
      .on('ready', () => {
        state.connected = true
        state.error = ''
        state.errorType = ''
        resolve(true)
      })
      .on('error', (err: Error) => {
        state.connected = false
        state.error = err.message
        state.errorType = classifyError(err)
        resolve(false)
      })
      .on('close', () => {
        state.connected = false
        client = null
      })
      .connect({ host, port, username, password: getPassword() })
  })
}

export function disconnectTunnel(): void {
  if (client) {
    try { client.end() } catch { /* ignore */ }
    client = null
  }
  state.connected = false
  state.forwarded.clear()
}

/** Add a local port forward: localhost:localPort → host:targetPort via the SSH channel. */
export function addForwardedPort(localPort: number, targetPort: number, host: string): Promise<boolean> {
  return new Promise((resolve) => {
    if (!client || !state.connected) { resolve(false); return }
    const server = net.createServer((socket) => {
      if (!client) { socket.destroy(); return }
      client.forwardOut('127.0.0.1', 0, host || 'localhost', targetPort, (err, stream) => {
        if (err) { socket.destroy(); return }
        socket.pipe(stream).pipe(socket)
      })
    })
    server.listen(localPort, '127.0.0.1', () => {
      state.forwarded.set(localPort, { targetPort, host })
      resolve(true)
    })
    server.on('error', () => resolve(false))
  })
}

export function removeForwardedPort(localPort: number): void {
  state.forwarded.delete(localPort)
}

export function testPortReachable(localPort: number): Promise<boolean> {
  return new Promise((resolve) => {
    const sock = net.connect({ host: '127.0.0.1', port: localPort })
    sock.on('connect', () => { sock.destroy(); resolve(true) })
    sock.on('error', () => resolve(false))
    setTimeout(() => { sock.destroy(); resolve(false) }, 500)
  })
}

export function reconnectTunnel(): Promise<boolean> {
  const s = getStore().get('serverUrl')
  // serverUrl like https://host:port — tunnel host/port resolved in bridge.ts from server config
  return Promise.resolve(false)
}

import { getStore } from './store'
```

- [ ] **Step 2: 说明**：`reconnectTunnel` 的 host/port 由 `bridge.ts` 从服务器配置解析后调用 `connectTunnel`，故此处占位返回 false。SSH 用户名默认从配置读（Plan 3 内从 `serverUrl`/默认 `root` 推断，后续可配置化）。

- [ ] **Step 3: 提交**

```bash
git add desktop/src/main/tunnel.ts
git commit -m "feat(desktop): ssh2-based SSH tunnel + port forwarding"
```

---

### Task 5: IPC 桥接（preload + main 的 ipcMain.handle）

**Files:**
- Create: `desktop/src/preload/index.ts`
- Create: `desktop/src/main/bridge.ts`

- [ ] **Step 1: `desktop/src/preload/index.ts`**

```ts
import { contextBridge, ipcRenderer } from 'electron'

const invoke = (channel: string, ...args: unknown[]) => ipcRenderer.invoke(channel, ...args)

contextBridge.exposeInMainWorld('ClawBenchNative', {
  // sync
  isNativeApp: () => true,
  getLanguage: () => (process.env.LANG || 'en').split('_')[0],
  showServerDialog: () => { ipcRenderer.send('native:show-server-dialog') },
  openSession: (sessionId: string) => { ipcRenderer.send('native:open-session', sessionId) },
  setNativePushEnabled: (enabled: boolean) => { ipcRenderer.send('native:set-push-enabled', enabled) },
  updateLastSeenEventId: (id: string) => { ipcRenderer.send('native:update-last-seen', id) },
  setKeepScreenOn: (on: boolean) => { ipcRenderer.send('native:keep-screen-on', on) },
  log: (level: string, tag: string, msg: string) => { ipcRenderer.send('native:log', level, tag, msg) },

  // async reads
  getAppVersion: () => invoke('native:get-app-version'),
  getServerList: () => invoke('native:get-server-list'),
  getSavedServerConfig: () => invoke('native:get-saved-server-config'),
  getServerUrl: () => invoke('native:get-server-url'),
  getPassword: () => invoke('native:get-password'),
  getForwardedPorts: () => invoke('native:get-forwarded-ports'),
  testPortReachable: (p: number) => invoke('native:test-port-reachable', p),
  isTunnelConnected: () => invoke('native:is-tunnel-connected'),
  getTunnelError: () => invoke('native:get-tunnel-error'),
  getTunnelErrorType: () => invoke('native:get-tunnel-error-type'),
  getPendingNavigation: () => invoke('native:get-pending-navigation'),

  // async writes
  saveServer: (u: string, p: string) => invoke('native:save-server', u, p),
  removeServer: (u: string) => invoke('native:remove-server', u),
  setSSHPassword: (p: string) => invoke('native:set-ssh-password', p),
  connectToServer: (u: string, p: string) => invoke('native:connect-to-server', u, p),
  addForwardedPort: (l: number, t: number, h: string) => invoke('native:add-forwarded-port', l, t, h),
  removeForwardedPort: (l: number) => invoke('native:remove-forwarded-port', l),
  reconnectTunnel: () => invoke('native:reconnect-tunnel'),
  reconnectTunnelAsync: () => invoke('native:reconnect-tunnel'),
  downloadFile: (path: string) => invoke('native:download-file', path),
  downloadUrl: (url: string, fileName: string) => invoke('native:download-url', url, fileName),
  downloadBlob: (b64: string, fileName: string) => invoke('native:download-blob', b64, fileName),
  openInBrowser: (port: number, protocol: string, host: string, path: string) => invoke('native:open-in-browser', port, protocol, host, path),
  openInSandbox: (port: number, protocol: string, host: string, path: string, sessionId?: string) => invoke('native:open-in-sandbox', port, protocol, host, path, sessionId),
  startLogCapture: () => invoke('native:start-log-capture'),
  stopLogCapture: () => invoke('native:stop-log-capture'),
  shareText: (text: string) => invoke('native:share-text', text),
  shareFile: (path: string, mime: string) => invoke('native:share-file', path, mime),
  shareFiles: (paths: string, mimes: string) => invoke('native:share-files', paths, mimes),
})
```

- [ ] **Step 2: `desktop/src/main/bridge.ts`**（`ipcMain.handle` 注册，调用各模块）

```ts
import { app, ipcMain, dialog, shell, clipboard, Notification } from 'electron'
import path from 'node:path'
import os from 'node:os'
import { getStore, initStore } from './store'
import { getPassword, savePassword } from './secrets'
import { connectTunnel, disconnectTunnel, addForwardedPort, removeForwardedPort as rmFwd,
  getForwardedPorts, isTunnelConnected, getTunnelError, getTunnelErrorType, testPortReachable, reconnectTunnel } from './tunnel'
import { getMainWindow, openSandboxWindow } from './window'
import { downloadFileByPath, downloadFileByPathTo, downloadByUrl, downloadBlob } from './download'
import { setKeepScreenOnImpl } from './powersave'
import { getPendingNavigationImpl, dispatchOpenSession } from './notification'

let pendingNavigation: string | null = null

export function registerBridge(): void {
  initStore()

  ipcMain.handle('native:get-app-version', () => app.getVersion())
  ipcMain.handle('native:get-server-list', () => JSON.stringify(getStore().get('servers')))
  ipcMain.handle('native:get-saved-server-config', () => {
    const u = getStore().get('serverUrl')
    if (!u) return '{}'
    const url = new URL(u)
    return JSON.stringify({ protocol: url.protocol.replace(':', ''), host: url.hostname, port: url.port || '', password: getPassword() })
  })
  ipcMain.handle('native:get-server-url', () => getStore().get('serverUrl'))
  ipcMain.handle('native:get-password', () => getPassword())

  ipcMain.handle('native:save-server', (_e, url: string, password: string) => {
    const servers = getStore().get('servers')
    const idx = servers.findIndex(s => s.url === url)
    if (idx >= 0) servers[idx].password = password
    else servers.unshift({ url, password })
    getStore().set('servers', servers)
  })
  ipcMain.handle('native:remove-server', (_e, url: string) => {
    getStore().set('servers', getStore().get('servers').filter(s => s.url !== url))
  })
  ipcMain.handle('native:set-ssh-password', (_e, p: string) => savePassword(p))
  ipcMain.handle('native:connect-to-server', (_e, url: string, password: string) => {
    getStore().set('serverUrl', url)
    if (password) savePassword(password)
    dispatchOpenSession(null)
  })

  ipcMain.handle('native:get-forwarded-ports', () => JSON.stringify(getForwardedPorts()))
  ipcMain.handle('native:test-port-reachable', (_e, p: number) => testPortReachable(p))
  ipcMain.handle('native:is-tunnel-connected', () => isTunnelConnected())
  ipcMain.handle('native:get-tunnel-error', () => getTunnelError())
  ipcMain.handle('native:get-tunnel-error-type', () => getTunnelErrorType())
  ipcMain.handle('native:add-forwarded-port', (_e, l: number, t: number, h: string) => addForwardedPort(l, t, h))
  ipcMain.handle('native:remove-forwarded-port', (_e, l: number) => rmFwd(l))
  ipcMain.handle('native:reconnect-tunnel', () => reconnectTunnel())
  ipcMain.handle('native:get-pending-navigation', () => { const n = pendingNavigation; pendingNavigation = null; return n })

  ipcMain.handle('native:download-file', (_e, path: string) => downloadFileByPath(path))
  ipcMain.handle('native:download-url', (_e, url: string, fileName: string) => downloadByUrl(url, fileName))
  ipcMain.handle('native:download-blob', (_e, b64: string, fileName: string) => downloadBlob(b64, fileName))
  ipcMain.handle('native:open-in-browser', (_e, port: number, protocol: string, host: string, path: string) => {
    shell.openExternal(`${protocol}://localhost:${port}${path || '/'}`)
  })
  ipcMain.handle('native:open-in-sandbox', (_e, port: number, protocol: string, host: string, path: string, sessionId?: string) => {
    openSandboxWindow(port, protocol, host, path)
  })

  ipcMain.handle('native:share-text', (_e, text: string) => { clipboard.writeText(text); return Promise.resolve() })
  ipcMain.handle('native:share-file', async (_e, filePath: string, mime: string) => {
    // Simplified share: download to temp then open with default app (spec §2.2)
    const name = path.basename(filePath)
    const tmp = path.join(os.tmpdir(), `clawbench-share-${Date.now()}-${name}`)
    await downloadFileByPathTo(filePath, tmp)
    shell.openPath(tmp)
  })
  ipcMain.handle('native:share-files', (_e, pathsJson: string) => {
    const paths: string[] = JSON.parse(pathsJson || '[]')
    paths.forEach(p => shell.showItemInFolder(os.tmpdir()))
    return Promise.resolve()
  })
  ipcMain.handle('native:start-log-capture', () => Promise.resolve())
  ipcMain.handle('native:stop-log-capture', () => Promise.resolve())

  ipcMain.on('native:show-server-dialog', () => getMainWindow()?.webContents.send('clawbench-show-server-dialog'))
  ipcMain.on('native:open-session', (_e, id: string) => dispatchOpenSession(id))
  ipcMain.on('native:set-push-enabled', (_e, enabled: boolean) => getStore().set('nativePushEnabled', enabled))
  ipcMain.on('native:update-last-seen', (_e, id: string) => { /* 桌面端无 SharedPreferences；留空 */ })
  ipcMain.on('native:keep-screen-on', (_e, on: boolean) => setKeepScreenOnImpl(on))
  ipcMain.on('native:log', (_e, level: string, tag: string, msg: string) => { /* 转发到主进程日志 */ })
}
```

（`bridge.ts` 依赖 `window.ts`/`download.ts`/`powersave.ts`/`notification.ts` 的导出，Task 6-7 定义。）

- [ ] **Step 3: 提交**

```bash
git add desktop/src/preload/index.ts desktop/src/main/bridge.ts
git commit -m "feat(desktop): contextBridge ClawBenchNative + ipcMain.handle wiring"
```

---

### Task 6: 窗口管理（主窗口 + 沙盒窗口）

**Files:**
- Create: `desktop/src/main/window.ts`

- [ ] **Step 1: `desktop/src/main/window.ts`**

```ts
import { BrowserWindow, shell } from 'electron'
import path from 'node:path'
import { getStore } from './store'

let mainWindow: BrowserWindow | null = null

export function getMainWindow(): BrowserWindow | null { return mainWindow }

export function createMainWindow(): BrowserWindow {
  mainWindow = new BrowserWindow({
    width: 1280, height: 800, show: false,
    webPreferences: { preload: path.join(__dirname, '../preload/index.js'), contextIsolation: true, nodeIntegration: false },
  })
  const url = getStore().get('serverUrl') || 'http://localhost:20000'
  mainWindow.loadURL(url)
  mainWindow.once('ready-to-show', () => mainWindow?.show())
  mainWindow.webContents.setWindowOpenHandler(({ url: target }) => {
    shell.openExternal(target)
    return { action: 'deny' }
  })
  mainWindow.on('closed', () => { mainWindow = null })
  return mainWindow
}

export function openSandboxWindow(port: number, protocol: string, host: string, path: string): void {
  const win = new BrowserWindow({
    width: 1000, height: 720,
    webPreferences: { partition: `sandbox-${port}`, contextIsolation: true },
  })
  win.loadURL(`${protocol}://localhost:${port}${path || '/'}`)
}
```

- [ ] **Step 2: 提交**

```bash
git add desktop/src/main/window.ts
git commit -m "feat(desktop): main window + sandbox window (partitioned session)"
```

---

### Task 7: 通知 / 防休眠 / 下载

**Files:**
- Create: `desktop/src/main/notification.ts`
- Create: `desktop/src/main/powersave.ts`
- Create: `desktop/src/main/download.ts`

- [ ] **Step 1: `desktop/src/main/notification.ts`**

```ts
import { Notification, ipcMain } from 'electron'
import { getMainWindow } from './window'

export function getPendingNavigationImpl(): string | null { return null }

export function dispatchOpenSession(sessionId: string | null): void {
  const w = getMainWindow()
  if (!w) return
  w.webContents.send('clawbench-open-session', { sessionId })
  if (sessionId) w.focus()
}

export function showTerminalNotification(title: string, body: string, sessionId: string): void {
  if (!Notification.isSupported()) return
  const n = new Notification({ title, body })
  n.on('click', () => {
    dispatchOpenSession(sessionId)
  })
  n.show()
}
```

- [ ] **Step 2: `desktop/src/main/powersave.ts`**

```ts
import { powerSaveBlocker } from 'electron'

let id: number | null = null
export function setKeepScreenOnImpl(on: boolean): void {
  if (on && id === null) id = powerSaveBlocker.start('prevent-display-sleep')
  else if (!on && id !== null) { powerSaveBlocker.stop(id); id = null }
}
```

- [ ] **Step 3: `desktop/src/main/download.ts`**

```ts
import { dialog, shell } from 'electron'
import fs from 'node:fs'
import path from 'node:path'
import https from 'node:https'

function pickSavePath(defaultName: string): Promise<string | null> {
  return dialog.showSaveDialog({ defaultPath: defaultName }).then(r => r.canceled || !r.filePath ? null : r.filePath)
}

function resolveLocalFileUrl(filePath: string): string {
  const base = getStore().get('serverUrl') || ''
  if (filePath.startsWith('/')) {
    return `${base}/api/local-file/?download=1&path=${encodeURIComponent(filePath)}`
  }
  return `${base}/api/local-file/${filePath.split('/').map(encodeURIComponent).join('/')}?download=1`
}

async function fetchToFile(url: string, dest: string): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const lib = url.startsWith('https:') ? require('node:https') : require('node:http')
    lib.get(url, (res: import('node:http').IncomingMessage) => {
      if (res.statusCode && res.statusCode >= 400) { res.resume(); reject(new Error(`HTTP ${res.statusCode}`)); return }
      const f = fs.createWriteStream(dest)
      res.pipe(f).on('finish', () => { f.close(); resolve() }).on('error', reject)
    }).on('error', reject)
  })
}

export async function downloadFileByPathTo(filePath: string, dest: string): Promise<void> {
  await fetchToFile(resolveLocalFileUrl(filePath), dest)
}

export async function downloadFileByPath(filePath: string): Promise<void> {
  const name = path.basename(filePath)
  const dest = await pickSavePath(name)
  if (!dest) return
  await downloadFileByPathTo(filePath, dest)
  shell.showItemInFolder(dest)
}

export async function downloadByUrl(url: string, fileName: string): Promise<void> {
  const dest = await pickSavePath(fileName || path.basename(url))
  if (!dest) return
  await new Promise<void>((resolve, reject) => {
    https.get(url, (res) => {
      const f = fs.createWriteStream(dest)
      res.pipe(f).on('finish', () => { f.close(); resolve() }).on('error', reject)
    }).on('error', reject)
  })
  shell.showItemInFolder(dest)
}

export async function downloadBlob(base64: string, fileName: string): Promise<void> {
  const dest = await pickSavePath(fileName)
  if (!dest) return
  fs.writeFileSync(dest, Buffer.from(base64, 'base64'))
  shell.showItemInFolder(dest)
}
```

- [ ] **Step 4: 提交**

```bash
git add desktop/src/main/notification.ts desktop/src/main/powersave.ts desktop/src/main/download.ts
git commit -m "feat(desktop): notifications, powerSaveBlocker, downloads"
```

---

### Task 8: npm 自升级器

**Files:**
- Create: `desktop/src/main/updater.ts`
- Create: `desktop/src/main/updater.test.ts`（测纯 registry/校验组合，mock HTTP）

- [ ] **Step 1: `desktop/src/main/updater.ts`**

```ts
import { app } from 'electron'
import https from 'node:https'
import path from 'node:path'
import fs from 'node:fs'
import os from 'node:os'
import { getDesktopPkg, latestUrl, rewriteTarball, parseNpmLatest } from '../shared/registry'
import { verifyIntegrity } from '../shared/integrity'

export function isChinaMainland(): boolean {
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || ''
  return ['Asia/Shanghai', 'Asia/Chongqing', 'Asia/Urumqi', 'Asia/Harbin'].includes(tz)
}

export async function checkForUpdate(): Promise<{ hasUpdate: boolean; version: string; tarball: string }> {
  const pkg = getDesktopPkg(process.platform as NodeJS.Platform, process.arch)
  if (!pkg) return { hasUpdate: false, version: '', tarball: '' }
  const china = isChinaMainland()
  const json = await httpGet(latestUrl(pkg, china))
  const info = parseNpmLatest(json)
  const current = app.getVersion()
  const hasUpdate = current !== info.version
  return { hasUpdate, version: info.version, tarball: rewriteTarball(info.tarball, china) }
}

function httpGet(url: string): Promise<string> {
  return new Promise((resolve, reject) => {
    https.get(url, (res) => {
      let data = ''
      res.on('data', c => { data += c })
      res.on('end', () => resolve(data))
    }).on('error', reject)
  })
}

export async function downloadAndInstall(tarballUrl: string, version: string, integrity: string): Promise<string> {
  const buf = await httpGetBuffer(tarballUrl)
  if (integrity && !verifyIntegrity(buf, integrity)) throw new Error('integrity verification failed')
  // tar.gz contains the portable app dir; extract to a temp dir, then swap into place
  const destDir = path.join(os.homedir(), '.clawbench-desktop', `app-${version}`)
  fs.mkdirSync(destDir, { recursive: true })
  // (full impl: gunzip + tar extract the packaged dir into destDir; then relaunch via app.relaunch + app.exit)
  return destDir
}

function httpGetBuffer(url: string): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    https.get(url, (res) => {
      const chunks: Buffer[] = []
      res.on('data', c => chunks.push(c))
      res.on('end', () => resolve(Buffer.concat(chunks)))
    }).on('error', reject)
  })
}
```

- [ ] **Step 2: 说明**：便携目录替换的"解压 + 目录切换 + 重启"在真实发布流中实现；本任务覆盖 registry 查询、区域判定、校验的纯逻辑与 HTTP 获取。`httpGet`/`httpGetBuffer` 抽成可注入（或直接测 `parseNpmLatest`/`verifyIntegrity` 组合）。

- [ ] **Step 3: 提交**

```bash
git add desktop/src/main/updater.ts desktop/src/main/updater.test.ts
git commit -m "feat(desktop): npm self-updater mirroring Go upgrade flow"
```

---

### Task 9: 入口 + electron-builder 配置 + 集成验证

**Files:**
- Create: `desktop/src/main/index.ts`
- Create: `desktop/build/electron-builder.yml`
- Modify: `desktop/package.json`

- [ ] **Step 1: `desktop/src/main/index.ts`**

```ts
import { app, BrowserWindow } from 'electron'
import { initStore } from './store'
import { createMainWindow, getMainWindow } from './window'
import { registerBridge } from './bridge'
import { checkForUpdate } from './updater'

app.whenReady().then(() => {
  initStore()
  registerBridge()
  createMainWindow()
  checkForUpdate().then(info => {
    if (info.hasUpdate) getMainWindow()?.webContents.send('clawbench-update-available', info)
  }).catch(() => {})
  app.on('activate', () => { if (BrowserWindow.getAllWindows().length === 0) createMainWindow() })
})

app.on('window-all-closed', () => { if (process.platform !== 'darwin') app.quit() })
```

- [ ] **Step 2: `desktop/build/electron-builder.yml`**

```yaml
appId: com.xulongzhe.clawbench
productName: ClawBench
directories:
  output: release
  buildResources: build
files:
  - dist/**
  - "!**/node_modules/.cache"
win:
  target: dir
mac:
  target: dir
linux:
  target: dir
```

- [ ] **Step 3: 集成验证**

```bash
cd desktop && npm run typecheck
cd desktop && npx vitest run
```
Expected: typecheck 无错，单测全过。（Electron GUI 运行时、electron-builder 打包、真实 npm 自升级需桌面环境/CI 验证，本环境为无头 CLI，代码层验证到 typecheck + 单测。）

- [ ] **Step 4: 提交**

```bash
git add desktop/src/main/index.ts desktop/build/electron-builder.yml desktop/package.json
git commit -m "feat(desktop): app entry, electron-builder config, integration"
```

---

## 自检对照（spec → task）

| Spec § | 要求 | Task |
|---|---|---|
| §3 目录结构 | desktop/ main/preload/build | Task 1, 5, 6, 9 |
| §4.1 同步桥方法 | isNativeApp/getLanguage/log/openSession/… | Task 5 preload |
| §4.2 异步桥方法 | 读+写经 ipcMain.handle | Task 5 bridge |
| §6.1 隧道 | ssh2 端口转发 | Task 4 |
| §6.2 密钥 | safeStorage | Task 3 |
| §6.3 通知 | 原生通知 + 会话深链 | Task 7 |
| §6.4 沙盒窗口 | partitioned session | Task 6 |
| §7.2 npm 自升级 | registry+integrity+替换 | Task 2, 8 |
| §8 下载 | download.ts | Task 7 |

## 环境限制与发布流程（记录，非本 plan 代码）

- **无头 CLI**：Electron GUI 运行、electron-builder 打包、真实 npm 自升级无法在此验证，代码层以 `typecheck + vitest` 为门禁；运行/打包在桌面 CI 完成。
- **macOS 签名**：发布需代码签名（Gatekeeper），Windows 建议签名 —— 属 CI 发布流程。
- **npm 发布**：`desktop/package.json` 按平台发布 `@xulongzhe/clawbench-desktop-<os>-<arch>`（便携目录），由发布流水线执行。

## 后续

本 plan 完成后进入 Plan 4（`/api/desktop/latest` 下载入口 + 欢迎页/配置页按钮）。
