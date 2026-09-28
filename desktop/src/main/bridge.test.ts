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
  setTransportPreference: vi.fn(),
  getTransportPreference: vi.fn(() => 'both' as string),
  getActiveTransport: vi.fn(() => 'h2' as string),
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
  setTransportPreference: tunnelMock.setTransportPreference,
  getTransportPreference: tunnelMock.getTransportPreference,
  getActiveTransport: tunnelMock.getActiveTransport,
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

  it('registers the three transport channels the preload invokes', () => {
    expect(handlers.has('native:set-tunnel-transport')).toBe(true)
    expect(handlers.has('native:get-tunnel-transport')).toBe(true)
    expect(handlers.has('native:get-active-tunnel-transport')).toBe(true)
  })

  it('forwards ssh to setTransportPreference', () => {
    invoke('native:set-tunnel-transport', 'ssh')
    expect(tunnelMock.setTransportPreference).toHaveBeenCalledWith('ssh')
  })

  // Electron is hard-wired to SSH: h2/both must be dropped at the IPC boundary
  // so a stale renderer cannot switch the main process off SSH.
  it.each(['h2', 'both'])('clamps %s to a no-op (Electron stays on ssh)', (pref) => {
    invoke('native:set-tunnel-transport', pref)
    expect(tunnelMock.setTransportPreference).not.toHaveBeenCalled()
  })

  it.each([
    ['an unknown string', 'quic'],
    ['an empty string', ''],
    ['a number', 2],
    ['null', null],
    ['undefined', undefined],
    ['an object', { transport: 'h2' }],
  ])('ignores %s instead of poisoning the transport chain', (_name, value) => {
    invoke('native:set-tunnel-transport', value)
    expect(tunnelMock.setTransportPreference).not.toHaveBeenCalled()
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
})
