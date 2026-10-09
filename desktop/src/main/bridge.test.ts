import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

/**
 * The tunnel-transport IPC surface added by T8.
 *
 * `registerBridge()` is the only thing under test: it is a long list of
 * `ipcMain.handle` calls, and the failure mode this suite guards against is a
 * channel name that does not match what `preload/index.ts` invokes — the
 * renderer then gets "No handler registered" at runtime, which no type checker
 * can see (the two sides are separate files).
 *
 * `./tunnel` is mocked because importing the real module drags in ssh2, the
 * cookie jar and node:http2; its own suites cover the transport selection
 * itself. Everything else is mocked only as far as `registerBridge()` touches
 * it on the way to the handlers we care about.
 */
const handlers = vi.hoisted(() => new Map<string, (...args: unknown[]) => unknown>())
const onHandlers = vi.hoisted(() => new Map<string, (...args: unknown[]) => void>())

const tunnelMock = vi.hoisted(() => ({
  getTransportPreference: vi.fn(() => 'both' as string),
  getActiveTransport: vi.fn(() => 'h2' as string),
  initTransportPreference: vi.fn(),
  persistTransportPreference: vi.fn(() => true),
}))

vi.mock('electron', () => ({
  app: { getVersion: () => '1.2.3', getLocale: () => 'en', getPath: () => '/tmp/clawbench-test' },
  ipcMain: {
    handle: (channel: string, fn: (...args: unknown[]) => unknown) => { handlers.set(channel, fn) },
    on: (channel: string, fn: (...args: unknown[]) => void) => { onHandlers.set(channel, fn) },
  },
  shell: { openPath: () => undefined, openExternal: () => undefined },
  clipboard: { writeText: () => undefined },
  nativeTheme: { themeSource: 'dark' },
  BrowserWindow: class {},
  Menu: class {},
  MenuItem: class {},
  dialog: { showSaveDialog: async () => ({ canceled: true }) },
  Notification: class { static isSupported() { return false } },
  powerSaveBlocker: { start: () => 1, stop: () => undefined },
  session: { defaultSession: { cookies: { get: async () => [] }, clearStorageData: async () => undefined } },
}))

vi.mock('electron-store', () => ({
  default: class {
    // `servers` must default to an array: migratePasswords() iterates it during
    // registerBridge(), and undefined would throw before any handler registers.
    private data: Record<string, unknown> = { servers: [] }
    get(k: string) { return this.data[k] }
    set(k: string, v: unknown) { this.data[k] = v }
  },
}))

vi.mock('./tunnel', () => ({
  addForwardedPort: vi.fn(),
  removeForwardedPort: vi.fn(),
  addReverseForwardedPort: vi.fn(),
  removeReverseForwardedPort: vi.fn(),
  getForwardedPorts: vi.fn(() => []),
  isTunnelConnected: vi.fn(() => false),
  getTunnelError: vi.fn(() => ''),
  getTunnelErrorType: vi.fn(() => ''),
  testPortReachable: vi.fn(async () => false),
  reconnectTunnel: vi.fn(async () => true),
  getTransportPreference: tunnelMock.getTransportPreference,
  getActiveTransport: tunnelMock.getActiveTransport,
  initTransportPreference: tunnelMock.initTransportPreference,
  persistTransportPreference: tunnelMock.persistTransportPreference,
}))

import { registerBridge } from './bridge'

/** Invoke a captured ipcMain.handle channel as the renderer would. */
function invoke(channel: string, ...args: unknown[]): unknown {
  const fn = handlers.get(channel)
  if (!fn) throw new Error(`no ipcMain.handle registered for ${channel}`)
  // Electron passes the IpcMainInvokeEvent first; the handlers ignore it.
  return fn({}, ...args)
}

describe('bridge: tunnel transport IPC', () => {
  beforeEach(() => {
    handlers.clear()
    onHandlers.clear()
    vi.clearAllMocks()
    tunnelMock.getTransportPreference.mockReturnValue('both')
    tunnelMock.getActiveTransport.mockReturnValue('h2')
    registerBridge()
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('registers the two transport channels the preload invokes', () => {
    expect(handlers.has('native:get-tunnel-transport')).toBe(true)
    expect(handlers.has('native:get-active-tunnel-transport')).toBe(true)
  })

  it('get-tunnel-transport reports the configured preference', () => {
    tunnelMock.getTransportPreference.mockReturnValue('ssh')
    expect(invoke('native:get-tunnel-transport')).toBe('ssh')
  })

  it('get-active-tunnel-transport reports the transport that carried the session', () => {
    // Distinct from the preference: a 'both' client reports whichever won.
    tunnelMock.getActiveTransport.mockReturnValue('h2')
    expect(invoke('native:get-active-tunnel-transport')).toBe('h2')
  })

  it('hydrates the persisted preference at registration time', () => {
    // Must run before any window exists, so the first connect already uses the
    // user's saved choice instead of the SSH default.
    expect(tunnelMock.initTransportPreference).toHaveBeenCalled()
  })

  it('registers the setter the settings row writes through', () => {
    expect(handlers.has('native:set-tunnel-transport')).toBe(true)
  })

  it('set-tunnel-transport forwards the value to the persisting writer', () => {
    tunnelMock.persistTransportPreference.mockReturnValue(true)
    expect(invoke('native:set-tunnel-transport', 'both')).toBe(true)
    expect(tunnelMock.persistTransportPreference).toHaveBeenCalledWith('both')
  })

  it('set-tunnel-transport surfaces a rejection instead of pretending success', () => {
    // The row reverts its optimistic value when this is false, so the handler
    // must not swallow the writer's answer.
    tunnelMock.persistTransportPreference.mockReturnValue(false)
    expect(invoke('native:set-tunnel-transport', 'garbage')).toBe(false)
  })
})

describe('bridge: native notification IPC', () => {
  beforeEach(() => {
    handlers.clear()
    onHandlers.clear()
    vi.clearAllMocks()
    registerBridge()
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('registers the dismiss channel the preload invokes', () => {
    // The preload calls `native:dismiss-notification`; a rename on either side
    // would make dismissal a silent no-op ("No handler registered"), which no
    // type checker can see — the two sides are separate files.
    expect(handlers.has('native:dismiss-notification')).toBe(true)
  })

  it('dismiss-notification resolves without throwing for any subject', async () => {
    // Best-effort contract: an unknown subject is a no-op, never a rejection.
    await expect(invoke('native:dismiss-notification', '', 's1')).resolves.toBeUndefined()
    await expect(invoke('native:dismiss-notification', '7', '')).resolves.toBeUndefined()
  })
})
