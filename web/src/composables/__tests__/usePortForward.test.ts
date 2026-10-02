import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'

// ── Timer leak prevention ──

const pendingTimers: ReturnType<typeof setTimeout>[] = []
const _origSetTimeout = setTimeout
globalThis.setTimeout = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetTimeout(fn, ms, ...args)
  pendingTimers.push(id)
  return id
}) as typeof setTimeout

const pendingIntervals: ReturnType<typeof setInterval>[] = []
const _origSetInterval = setInterval
globalThis.setInterval = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetInterval(fn, ms, ...args)
  pendingIntervals.push(id)
  return id
}) as typeof setInterval

afterEach(() => {
  for (const id of pendingTimers) {
    clearTimeout(id)
  }
  pendingTimers.length = 0
  for (const id of pendingIntervals) {
    clearInterval(id)
  }
  pendingIntervals.length = 0
})

// Mock API utilities
const mockApiGet = vi.fn()
const mockApiPost = vi.fn()
const mockApiPut = vi.fn()
const mockApiDelete = vi.fn()

vi.mock('@/utils/api', () => ({
    apiGet: (...args: any[]) => mockApiGet(...args),
    apiPost: (...args: any[]) => mockApiPost(...args),
    apiPut: (...args: any[]) => mockApiPut(...args),
    apiDelete: (...args: any[]) => mockApiDelete(...args),
}))

const mockStore = vi.hoisted(() => ({ state: {} as Record<string, unknown> }))
vi.mock('@/stores/app', () => ({
    store: mockStore,
}))

const mockIsAppMode = ref(false)
vi.mock('@/composables/useAppMode', () => ({
    useAppMode: () => ({ isAppMode: mockIsAppMode, isDesktopApp: { value: false } }),
}))

// The transport preference lives in the server config. This mock mirrors the
// reader (`getServerValue`) the web health gate uses to decide whether an h2
// path exists when the SSH listener is off, so it can be driven per test.
const mockServerConfig = ref<Record<string, unknown>>({})
const mockServerDefaults: Record<string, unknown> = { 'port_forward.transport': 'both' }
function readDotPath(source: Record<string, unknown>, dotPath: string): unknown {
    const parts = dotPath.split('.')
    let current: unknown = source
    for (const p of parts) {
        if (current == null || typeof current !== 'object') return undefined
        current = (current as Record<string, unknown>)[p]
    }
    return current
}
vi.mock('@/composables/useSettingsConfig', () => ({
    useSettingsConfig: () => ({
        serverConfig: mockServerConfig,
        getServerValue: (dotPath: string) => readDotPath(mockServerConfig.value, dotPath),
        getServerValueWithDefault: (dotPath: string) => {
            const value = readDotPath(mockServerConfig.value, dotPath)
            return value !== undefined ? value : mockServerDefaults[dotPath]
        },
    }),
}))

// Identity by default (the rest of this file asserts on bare i18n keys). The
// transportAnnotation tests below swap in a composing implementation so the
// real `gt('proxy.transportAnnotation', { transport: gt(labelKey) })` wrapper
// is observable — under identity both wires would collapse to one string and a
// dropped wrapper or wrong label key would go unnoticed.
let gtImpl: (key: string, params?: Record<string, unknown>) => string = (key) => key
vi.mock('@/composables/useLocale', () => ({
    gt: (key: string, params?: Record<string, unknown>) => gtImpl(key, params),
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
    useToast: () => ({ show: mockToastShow, dismiss: vi.fn(), visible: ref(false), message: ref(''), icon: ref(''), type: ref('success'), onClick: ref(null) }),
}))

vi.mock('@/composables/useSessionIdentity', () => ({
    useSessionIdentity: () => ({ currentSessionId: ref('test-session-id') }),
}))

const mockTunnelStatusFromPorts = vi.fn(() => 'ok')

vi.mock('@/utils/portForwardUtils', () => ({
    // Forward the ports array so tests can inspect what effectivePorts() produced.
    tunnelStatusFromPorts: (...args: unknown[]) => mockTunnelStatusFromPorts(...args),
    buildPortUrl: (port: number, protocol?: string, path?: string) => {
        const scheme = protocol || 'http'
        const urlPath = path || '/'
        if ((scheme === 'http' && port === 80) || (scheme === 'https' && port === 443)) {
            return `${scheme}://localhost${urlPath}`
        }
        return `${scheme}://localhost:${port}${urlPath}`
    },
    buildServerAddress: (serverPort: number, protocol?: string) => {
        const scheme = protocol === 'https' ? 'https' : 'http'
        if ((scheme === 'http' && serverPort === 80) || (scheme === 'https' && serverPort === 443)) {
            return `${scheme}://127.0.0.1`
        }
        return `${scheme}://127.0.0.1:${serverPort}`
    },
    isReversePort: (p: { direction?: string }) => p.direction === 'reverse',
}))

describe('usePortForward', () => {
    beforeEach(() => {
        vi.resetModules()
        mockApiGet.mockReset()
        mockApiPost.mockReset()
        mockApiPut.mockReset()
        mockApiDelete.mockReset()
        mockIsAppMode.value = false
        mockToastShow.mockReset()
        mockTunnelStatusFromPorts.mockReset()
        mockTunnelStatusFromPorts.mockImplementation(() => 'ok')
        mockServerConfig.value = {}
        delete (window as any).ClawBenchNative
    })

    describe('loadSSHInfo', () => {
        it('fetches SSH info and stores in sshInfo ref', async () => {
            const sshInfoResponse = {
                enabled: true,
                host: 'example.com',
                port: 20001,
                username: 'clawbench',
                fingerprint: 'SHA256:abc',
                command: 'ssh -L ...',
                connectionStats: { connected: true, clientCount: 1, activeChannels: 1 },
            }
            mockApiGet.mockResolvedValue(sshInfoResponse)

            const { usePortForward } = await import('@/composables/usePortForward')
            const { loadSSHInfo, sshInfo } = usePortForward()

            await loadSSHInfo()

            expect(mockApiGet).toHaveBeenCalledWith('/api/ssh/info/full')
            expect(sshInfo.value).toEqual(sshInfoResponse)
        })

        it('sets sshInfo to null on API failure', async () => {
            mockApiGet.mockRejectedValue(new Error('Network error'))

            const { usePortForward } = await import('@/composables/usePortForward')
            const { loadSSHInfo, sshInfo } = usePortForward()

            await loadSSHInfo()

            expect(sshInfo.value).toBeNull()
        })
    })

    describe('loadPorts', () => {
        it('fetches ports and stores in ports ref', async () => {
            const portsResponse = {
                ports: [
                    { port: 3000, name: 'App', protocol: 'http', active: true },
                ],
            }
            mockApiGet.mockResolvedValue(portsResponse)

            const { usePortForward } = await import('@/composables/usePortForward')
            const { loadPorts, ports } = usePortForward()

            await loadPorts()

            expect(mockApiGet).toHaveBeenCalledWith('/api/proxy/ports')
            expect(ports.value).toHaveLength(1)
            expect(ports.value[0].port).toBe(3000)
        })

        it('counts enabled ports (not just active) for the dock badge', async () => {
            const portsResponse = {
                ports: [
                    { port: 3000, localPort: 3000, name: 'A', protocol: 'http', active: true, enabled: true },
                    { port: 4000, localPort: 4000, name: 'B', protocol: 'http', active: false, enabled: true },
                    { port: 5000, localPort: 5000, name: 'C', protocol: 'http', active: true, enabled: false },
                ],
            }
            mockApiGet.mockResolvedValue(portsResponse)

            const { usePortForward } = await import('@/composables/usePortForward')
            const { loadPorts } = usePortForward()

            await loadPorts()
            await nextTick()

            // Enabled (3000 + 4000) even though 4000 is not active; 5000 excluded because disabled.
            expect(mockStore.state.portForwardEnabledCount).toBe(2)
        })

        it('sets loading state when not silent', async () => {
            mockApiGet.mockResolvedValue({ ports: [] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { loadPorts, loading } = usePortForward()

            const promise = loadPorts()
            // Loading should be true during the API call
            expect(loading.value).toBe(true)

            await promise
            expect(loading.value).toBe(false)
        })

        it('does not set loading state when silent', async () => {
            mockApiGet.mockResolvedValue({ ports: [] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { loadPorts, loading } = usePortForward()

            await loadPorts(true)
            expect(loading.value).toBe(false)
        })

        it('clears connectingPorts for active ports in web mode', async () => {
            mockApiGet.mockResolvedValue({
                ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: true }],
            })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { loadPorts, connectingPorts } = usePortForward()

            // Simulate port in connecting state
            connectingPorts.value.add(3000)
            connectingPorts.value = new Set(connectingPorts.value)
            expect(connectingPorts.value.has(3000)).toBe(true)

            await loadPorts(true)

            // Should be cleared since backend reports active=true
            expect(connectingPorts.value.has(3000)).toBe(false)
        })
    })

    describe('checkTunnelHealth', () => {
        it('returns early when SSH is disabled', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return { enabled: false, host: '', port: 0, username: '', fingerprint: '', command: '', connectionStats: null }
                return {}
            })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelChecking } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelChecking.value).toBe(false)
        })

        it('sets disconnected when SSH is enabled but not connected', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return {
                    enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c',
                    connectionStats: { connected: false, clientCount: 0, activeChannels: 0 },
                }
                return {}
            })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelChecking } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelChecking.value).toBe(false)
            expect(tunnelStatus.value).toBe('disconnected')
        })

        it('sets ok when SSH is connected with active ports', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [{ port: 3000, name: 'App', protocol: 'http', active: true }] }
                if (url === '/api/ssh/info/full') return {
                    enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c',
                    connectionStats: { connected: true, clientCount: 1, activeChannels: 1 },
                }
                return {}
            })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelChecking } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelChecking.value).toBe(false)
            expect(tunnelStatus.value).toBe('ok')
        })

        it('sets ok when SSH reports disconnected but ports are active', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [{ port: 3000, name: 'App', protocol: 'http', active: true, enabled: true }] }
                if (url === '/api/ssh/info/full') return {
                    enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c',
                    connectionStats: { connected: false, clientCount: 0, activeChannels: 0 },
                }
                return {}
            })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelStatus.value).toBe('ok')
        })

        it('resets tunnel state before checking', async () => {
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return { enabled: false, host: '', port: 0, username: '', fingerprint: '', command: '', connectionStats: null }
                return {}
            })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelError } = usePortForward()

            // Pre-set some state
            tunnelStatus.value = 'ok' as any
            tunnelError.value = 'old error'

            await checkTunnelHealth()

            // Should be reset
            expect(tunnelError.value).toBe('')
        })
    })

    /**
     * The SSH gate used to be `if (!info?.enabled) return`, which skipped the
     * whole health check whenever the server had SSH disabled — so an h2-only
     * install (the transport that does NOT need the SSH listener) never showed
     * any tunnel status. The gate must now be "SSH enabled OR the transport
     * allows h2".
     */
    describe('tunnel health gate (ssh OR h2)', () => {
        it('still checks health when SSH is disabled but the transport allows h2', async () => {
            mockIsAppMode.value = true
            // SSH listener off; transport left at its server default ('both').
            mockServerConfig.value = { port_forward: { transport: 'both' } }
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return { enabled: false, host: '', port: 0, username: '', fingerprint: '', command: '', connectionStats: null }
                return {}
            })
            const mockIsTunnelConnected = vi.fn(async () => true)
            ;(window as any).ClawBenchNative = { isTunnelConnected: mockIsTunnelConnected }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelChecking } = usePortForward()

            await checkTunnelHealth()

            // The gate let it through, so the native (h2) status was consulted
            // and the tunnel reported healthy.
            expect(mockIsTunnelConnected).toHaveBeenCalled()
            expect(tunnelStatus.value).toBe('ok')
            expect(tunnelChecking.value).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('still checks health with an explicit h2-only preference', async () => {
            mockIsAppMode.value = true
            mockServerConfig.value = { port_forward: { transport: 'h2' } }
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return { enabled: false, host: '', port: 0, username: '', fingerprint: '', command: '', connectionStats: null }
                return {}
            })
            const mockIsTunnelConnected = vi.fn(async () => true)
            ;(window as any).ClawBenchNative = { isTunnelConnected: mockIsTunnelConnected }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus } = usePortForward()

            await checkTunnelHealth()

            expect(mockIsTunnelConnected).toHaveBeenCalled()
            expect(tunnelStatus.value).toBe('ok')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('skips the health check when SSH is disabled and the transport is ssh-only', async () => {
            mockIsAppMode.value = true
            mockServerConfig.value = { port_forward: { transport: 'ssh' } }
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return { enabled: false, host: '', port: 0, username: '', fingerprint: '', command: '', connectionStats: null }
                return {}
            })
            const mockIsTunnelConnected = vi.fn(async () => true)
            ;(window as any).ClawBenchNative = { isTunnelConnected: mockIsTunnelConnected }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelChecking } = usePortForward()

            await checkTunnelHealth()

            // No SSH listener and no h2 on the wire: nothing to check.
            expect(mockIsTunnelConnected).not.toHaveBeenCalled()
            expect(tunnelStatus.value).toBe('unknown')
            expect(tunnelChecking.value).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('treats an unrecognized transport as ssh-only rather than assuming h2', async () => {
            mockIsAppMode.value = true
            // A value from a newer build the client cannot interpret: falling
            // back to "assume h2" would probe a transport that may not exist.
            mockServerConfig.value = { port_forward: { transport: 'quic' } }
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return { enabled: false, host: '', port: 0, username: '', fingerprint: '', command: '', connectionStats: null }
                return {}
            })
            const mockIsTunnelConnected = vi.fn(async () => true)
            ;(window as any).ClawBenchNative = { isTunnelConnected: mockIsTunnelConnected }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus } = usePortForward()

            await checkTunnelHealth()

            expect(mockIsTunnelConnected).not.toHaveBeenCalled()
            expect(tunnelStatus.value).toBe('unknown')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('does not crash when the SSH info fetch fails but h2 keeps the check alive', async () => {
            // /api/ssh/info/full is authenticated; a transient failure nulls
            // sshInfo while the transport still allows h2. The old code read
            // info.connectionStats unguarded, so this path must stay null-safe.
            mockIsAppMode.value = true
            mockServerConfig.value = { port_forward: { transport: 'both' } }
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') throw new Error('network')
                return {}
            })
            // No native tunnel status available → falls through to server stats.
            ;(window as any).ClawBenchNative = {}

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelChecking } = usePortForward()

            await expect(checkTunnelHealth()).resolves.toBeUndefined()

            // No stats to consult: leave the status unknown rather than
            // reporting a tunnel state the server never told us about.
            expect(tunnelStatus.value).toBe('unknown')
            expect(tunnelChecking.value).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('keeps checking when SSH is enabled regardless of the transport', async () => {
            // Regression guard: the widened gate must not *replace* the SSH
            // path — an SSH-enabled install still runs the check even if the
            // config says h2-only (e.g. a stale SSH client).
            mockServerConfig.value = { port_forward: { transport: 'h2' } }
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return {
                    enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c',
                    connectionStats: { connected: false, clientCount: 0, activeChannels: 0 },
                }
                return {}
            })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelStatus.value).toBe('disconnected')
        })
    })

    /**
     * The panel's "port mapping unavailable" banner keys off this exported
     * computed instead of calling the raw helper, so it recomputes when
     * `/api/config` resolves. Pin the values the banner depends on.
     */
    describe('transportAllowsH2', () => {
        it('is true for h2 and both, false for ssh and unknown values', async () => {
            mockServerConfig.value = { port_forward: { transport: 'both' } }
            const { usePortForward } = await import('@/composables/usePortForward')
            const { transportAllowsH2 } = usePortForward()

            expect(transportAllowsH2.value).toBe(true)

            mockServerConfig.value = { port_forward: { transport: 'h2' } }
            expect(transportAllowsH2.value).toBe(true)

            mockServerConfig.value = { port_forward: { transport: 'ssh' } }
            expect(transportAllowsH2.value).toBe(false)

            // Missing (config not loaded yet) and unrecognized both mean
            // "do not assume h2" — the conservative false the banner relies on.
            mockServerConfig.value = {}
            expect(transportAllowsH2.value).toBe(false)

            mockServerConfig.value = { port_forward: { transport: 'quic' } }
            expect(transportAllowsH2.value).toBe(false)
        })
    })

    describe('activeTransport', () => {
        it('reports the transport that actually carried the session', async () => {
            mockIsAppMode.value = true
            ;(window as any).ClawBenchNative = {
                getActiveTunnelTransport: async () => 'h2',
                getTunnelTransport: async () => 'ssh',
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { activeTransport, refreshActiveTransport } = usePortForward()

            await refreshActiveTransport()

            // The live wire wins over the stored preference.
            expect(activeTransport.value).toBe('h2')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('falls back to the preference when no transport has won yet', async () => {
            mockIsAppMode.value = true
            ;(window as any).ClawBenchNative = {
                getActiveTunnelTransport: async () => '',
                getTunnelTransport: () => 'h2',
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { activeTransport, refreshActiveTransport } = usePortForward()

            await refreshActiveTransport()

            expect(activeTransport.value).toBe('h2')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('rejects the retired "both" value rather than surfacing it', async () => {
            // No shipped host reports 'both' any more (Electron migrates a
            // stored one to 'h2', Android derives the family from the live
            // session). If an unknown host ever sends it, it must read as
            // unknown — not reach the panel as a label-able wire.
            mockIsAppMode.value = true
            ;(window as any).ClawBenchNative = {
                getActiveTunnelTransport: async () => 'both',
                getTunnelTransport: async () => 'both',
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { activeTransport, refreshActiveTransport } = usePortForward()

            await refreshActiveTransport()

            expect(activeTransport.value).toBe('')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('degrades to unknown when the host predates the transport methods', async () => {
            mockIsAppMode.value = true
            // Android / older Electron: neither method exists. Must not throw
            // and must not invent a value.
            ;(window as any).ClawBenchNative = { isTunnelConnected: async () => true }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { activeTransport, refreshActiveTransport } = usePortForward()

            await expect(refreshActiveTransport()).resolves.toBeUndefined()
            expect(activeTransport.value).toBe('')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('degrades to unknown when the bridge throws', async () => {
            mockIsAppMode.value = true
            ;(window as any).ClawBenchNative = {
                getActiveTunnelTransport: async () => { throw new Error('bridge') },
                getTunnelTransport: async () => { throw new Error('bridge') },
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { activeTransport, refreshActiveTransport } = usePortForward()

            await refreshActiveTransport()

            expect(activeTransport.value).toBe('')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('ignores a malformed transport value', async () => {
            mockIsAppMode.value = true
            ;(window as any).ClawBenchNative = {
                getActiveTunnelTransport: async () => 42,
                getTunnelTransport: async () => 'websocket',
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { activeTransport, refreshActiveTransport } = usePortForward()

            await refreshActiveTransport()

            expect(activeTransport.value).toBe('')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('reports unknown in web mode (no native tunnel exists)', async () => {
            mockIsAppMode.value = false
            ;(window as any).ClawBenchNative = { getActiveTunnelTransport: async () => 'h2' }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { activeTransport, refreshActiveTransport } = usePortForward()

            await refreshActiveTransport()

            expect(activeTransport.value).toBe('')

            delete (window as any).ClawBenchNative
        })
    })

    /**
     * The real `transportAnnotation()` — imported from the composable and driven
     * by the real `transportLabelKey`, NOT re-implemented. Two render tests
     * elsewhere hand-roll the same logic (one behind a `vi.mock` of this very
     * composable), so without this the function could be deleted or return the
     * wrong key and every test would still pass.
     *
     * `gt` is stubbed with a composing implementation that resolves the label
     * key to a wire marker, so the annotation is `(wire)` only when the real
     * `proxy.transportAnnotation` wrapper is applied around the right label key.
     */
    describe('transportAnnotation', () => {
        beforeEach(() => {
            gtImpl = (key, params) => {
                const labels: Record<string, string> = {
                    'proxy.transportSsh': 'SSH',
                    'proxy.transportH2': 'HTTP/2',
                }
                if (labels[key]) return labels[key]
                if (key === 'proxy.transportAnnotation') {
                    return `(${String(params?.transport ?? '')})`
                }
                return key
            }
        })

        afterEach(() => {
            gtImpl = (key) => key
        })

        async function setActiveTransport(value: 'ssh' | 'h2' | '') {
            mockIsAppMode.value = true
            ;(window as any).ClawBenchNative = {
                getActiveTunnelTransport: async () => value,
                getTunnelTransport: async () => value,
            }
            const { usePortForward } = await import('@/composables/usePortForward')
            const pf = usePortForward()
            await pf.refreshActiveTransport()
            return pf
        }

        it('wraps the SSH label through proxy.transportAnnotation for an ssh wire', async () => {
            const { transportAnnotation } = await setActiveTransport('ssh')
            expect(transportAnnotation()).toBe('(SSH)')
            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('wraps the HTTP/2 label through proxy.transportAnnotation for an h2 wire', async () => {
            const { transportAnnotation } = await setActiveTransport('h2')
            expect(transportAnnotation()).toBe('(HTTP/2)')
            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('returns empty when no single wire is known', async () => {
            const { transportAnnotation } = await setActiveTransport('')
            expect(transportAnnotation()).toBe('')
            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })
    })

    describe('openPort', () => {
        it('calls window.open in web mode', async () => {
            const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPort } = usePortForward()

            openPort(3000, 'http')

            expect(openSpy).toHaveBeenCalledWith('http://localhost:3000/', '_blank')

            openSpy.mockRestore()
        })

        it('opens WebView immediately in app mode (no testPortReachable)', async () => {
            mockIsAppMode.value = true
            const mockOpenInSandbox = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { openInSandbox: mockOpenInSandbox }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPort } = usePortForward()

            openPort(3000, 'http')

            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '', '', 'test-session-id')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('opens WebView immediately even when testPortReachable returns false', async () => {
            mockIsAppMode.value = true
            const mockOpenInSandbox = vi.fn().mockResolvedValue(undefined)
            const mockTestPortReachable = vi.fn().mockResolvedValue(false)
            ;(window as any).ClawBenchNative = { openInSandbox: mockOpenInSandbox, testPortReachable: mockTestPortReachable }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPort } = usePortForward()

            openPort(3000, 'http')

            // Should NOT call testPortReachable — just open WebView directly
            expect(mockTestPortReachable).not.toHaveBeenCalled()
            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '', '', 'test-session-id')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('passes host parameter to native sandbox browser', async () => {
            mockIsAppMode.value = true
            const mockOpenInSandbox = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { openInSandbox: mockOpenInSandbox }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPort } = usePortForward()

            openPort(3000, 'http', '192.168.1.1')

            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '192.168.1.1', '', 'test-session-id')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('falls back to openInBrowser when sandbox not available', async () => {
            mockIsAppMode.value = true
            const mockOpenInBrowser = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { openInBrowser: mockOpenInBrowser }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPort } = usePortForward()

            openPort(3000, 'https')

            expect(mockOpenInBrowser).toHaveBeenCalledWith(3000, 'https', '', '')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })
    })

    describe('openPortWithCheck', () => {
        it('calls window.open in web mode', async () => {
            const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck } = usePortForward()

            await openPortWithCheck(3000, 'http')

            expect(openSpy).toHaveBeenCalledWith('http://localhost:3000/', '_blank')

            openSpy.mockRestore()
        })

        it('opens immediately when port is reachable', async () => {
            mockIsAppMode.value = true
            const mockOpenInSandbox = vi.fn().mockResolvedValue(undefined)
            const mockTestPortReachable = vi.fn().mockResolvedValue(true)
            ;(window as any).ClawBenchNative = { openInSandbox: mockOpenInSandbox, testPortReachable: mockTestPortReachable }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck } = usePortForward()

            await openPortWithCheck(3000, 'http')

            expect(mockTestPortReachable).toHaveBeenCalledWith(3000)
            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '', '', 'test-session-id')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('opens directly in connecting state without testing reachability', async () => {
            mockIsAppMode.value = true
            const mockOpenInSandbox = vi.fn().mockResolvedValue(undefined)
            const mockTestPortReachable = vi.fn().mockResolvedValue(true)
            ;(window as any).ClawBenchNative = { openInSandbox: mockOpenInSandbox, testPortReachable: mockTestPortReachable }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck, connectingPorts } = usePortForward()

            connectingPorts.value.add(3000)
            connectingPorts.value = new Set(connectingPorts.value)

            await openPortWithCheck(3000, 'http')

            expect(mockTestPortReachable).not.toHaveBeenCalled()
            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '', '', 'test-session-id')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('reconnects and opens when port unreachable then reachable after reconnect', async () => {
            mockIsAppMode.value = true
            const mockOpenInSandbox = vi.fn().mockResolvedValue(undefined)
            const mockTestPortReachable = vi.fn()
                .mockResolvedValueOnce(false)  // initial check
                .mockResolvedValueOnce(true)   // after reconnect
            const mockReconnectTunnel = vi.fn().mockReturnValue(true)
            ;(window as any).ClawBenchNative = { openInSandbox: mockOpenInSandbox, testPortReachable: mockTestPortReachable, reconnectTunnel: mockReconnectTunnel }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck } = usePortForward()

            await openPortWithCheck(3000, 'http')

            expect(mockReconnectTunnel).toHaveBeenCalled()
            expect(mockToastShow).toHaveBeenCalledWith('portForward.tunnelReconnected', expect.objectContaining({ type: 'success' }))
            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '', '', 'test-session-id')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('shows error toast when port unreachable after reconnect', async () => {
            mockIsAppMode.value = true
            const mockTestPortReachable = vi.fn().mockResolvedValue(false)
            const mockReconnectTunnel = vi.fn().mockReturnValue(true)
            ;(window as any).ClawBenchNative = { testPortReachable: mockTestPortReachable, reconnectTunnel: mockReconnectTunnel }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck } = usePortForward()

            await openPortWithCheck(3000, 'http')

            expect(mockToastShow).toHaveBeenCalledWith('portForward.portUnreachable', expect.objectContaining({ type: 'error' }))

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('shows error toast when reconnect fails', async () => {
            mockIsAppMode.value = true
            const mockTestPortReachable = vi.fn().mockResolvedValue(false)
            const mockReconnectTunnel = vi.fn().mockReturnValue(false)
            ;(window as any).ClawBenchNative = { testPortReachable: mockTestPortReachable, reconnectTunnel: mockReconnectTunnel }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck } = usePortForward()

            await openPortWithCheck(3000, 'http')

            expect(mockToastShow).toHaveBeenCalledWith('portForward.portUnreachable', expect.objectContaining({ type: 'error' }))

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('falls back to direct open when testPortReachable not available (old APK)', async () => {
            mockIsAppMode.value = true
            const mockOpenInSandbox = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { openInSandbox: mockOpenInSandbox }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck } = usePortForward()

            await openPortWithCheck(3000, 'http')

            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '', '', 'test-session-id')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })
    })

    describe('reconnectPort', () => {
        it('shows success toast and refreshes when port is already reachable', async () => {
            mockIsAppMode.value = true
            const mockTestPortReachable = vi.fn().mockResolvedValue(true)
            mockApiGet.mockResolvedValue({ ports: [] })
            ;(window as any).ClawBenchNative = { testPortReachable: mockTestPortReachable }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { reconnectPort } = usePortForward()

            await reconnectPort(3000)

            expect(mockTestPortReachable).toHaveBeenCalledWith(3000)
            expect(mockToastShow).toHaveBeenCalledWith('portForward.tunnelReconnected', expect.objectContaining({ type: 'success' }))
        })

        it('reconnects and shows success toast when port becomes reachable', async () => {
            mockIsAppMode.value = true
            // First: false (initial check), then true (after reconnect)
            const mockTestPortReachable = vi.fn()
                .mockResolvedValueOnce(false)  // initial check
                .mockResolvedValueOnce(true)   // after reconnect
            const mockReconnectTunnel = vi.fn().mockReturnValue(true)
            mockApiGet.mockResolvedValue({ ports: [] })
            ;(window as any).ClawBenchNative = { testPortReachable: mockTestPortReachable, reconnectTunnel: mockReconnectTunnel }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { reconnectPort } = usePortForward()

            await reconnectPort(3000)

            expect(mockReconnectTunnel).toHaveBeenCalled()
            expect(mockToastShow).toHaveBeenCalledWith('portForward.tunnelReconnected', expect.objectContaining({ type: 'success' }))
        })

        it('shows error toast when port still unreachable after reconnect', async () => {
            mockIsAppMode.value = true
            const mockTestPortReachable = vi.fn().mockResolvedValue(false)
            const mockReconnectTunnel = vi.fn().mockReturnValue(true)
            mockApiGet.mockResolvedValue({ ports: [] })
            ;(window as any).ClawBenchNative = { testPortReachable: mockTestPortReachable, reconnectTunnel: mockReconnectTunnel }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { reconnectPort } = usePortForward()

            await reconnectPort(3000)

            expect(mockToastShow).toHaveBeenCalledWith('portForward.portUnreachable', expect.objectContaining({ type: 'error' }))
        })

        it('shows error toast when reconnect fails', async () => {
            mockIsAppMode.value = true
            const mockTestPortReachable = vi.fn().mockResolvedValue(false)
            const mockReconnectTunnel = vi.fn().mockReturnValue(false)
            mockApiGet.mockResolvedValue({ ports: [] })
            ;(window as any).ClawBenchNative = { testPortReachable: mockTestPortReachable, reconnectTunnel: mockReconnectTunnel }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { reconnectPort } = usePortForward()

            await reconnectPort(3000)

            expect(mockToastShow).toHaveBeenCalledWith('portForward.portUnreachable', expect.objectContaining({ type: 'error' }))
        })

        it('refreshes port list even without native bridge (web mode)', async () => {
            mockIsAppMode.value = false
            mockApiGet.mockResolvedValue({ ports: [] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { reconnectPort } = usePortForward()

            await reconnectPort(3000)

            expect(mockApiGet).toHaveBeenCalledWith('/api/proxy/ports')
        })
    })

    describe('openInExternalBrowser', () => {
        it('calls native openInBrowser in app mode', async () => {
            mockIsAppMode.value = true
            const mockOpenInBrowser = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { openInBrowser: mockOpenInBrowser }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openInExternalBrowser } = usePortForward()

            openInExternalBrowser(3000, 'https')

            expect(mockOpenInBrowser).toHaveBeenCalledWith(3000, 'https', '', '')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('passes host parameter to native openInBrowser', async () => {
            mockIsAppMode.value = true
            const mockOpenInBrowser = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { openInBrowser: mockOpenInBrowser }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openInExternalBrowser } = usePortForward()

            openInExternalBrowser(3000, 'https', '192.168.1.1')

            expect(mockOpenInBrowser).toHaveBeenCalledWith(3000, 'https', '192.168.1.1', '')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('opens window in web mode', async () => {
            const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null)

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openInExternalBrowser } = usePortForward()

            openInExternalBrowser(3000, 'http')

            expect(openSpy).toHaveBeenCalledWith('http://localhost:3000/', '_blank')

            openSpy.mockRestore()
        })
    })

    describe('registerPort', () => {
        it('posts port to API, refreshes, and returns localPort', async () => {
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts } = usePortForward()

            const result = await registerPort(3000, 'App', 'http')

            expect(mockApiPost).toHaveBeenCalledWith('/api/proxy/ports', {
                port: 3000, host: '', name: 'App', protocol: 'http', direction: 'forward',
            })
            // Port should be in connecting state
            expect(connectingPorts.value.has(3000)).toBe(true)
            // Should return the localPort from the API
            expect(result).toBe(3000)
        })

        it('returns remapped localPort for privileged ports', async () => {
            mockApiPost.mockResolvedValue({ localPort: 1024 })
            mockApiGet.mockResolvedValue({ ports: [] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort } = usePortForward()

            const result = await registerPort(80, 'HTTP', 'http')

            expect(result).toBe(1024)
        })

        it('passes host parameter to API and native layer in app mode', async () => {
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [] })
            const mockAddForwardedPort = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort } = usePortForward()

            const result = await registerPort(3000, 'App', 'http', '192.168.1.1')

            expect(mockApiPost).toHaveBeenCalledWith('/api/proxy/ports', {
                port: 3000, host: '192.168.1.1', name: 'App', protocol: 'http', direction: 'forward',
            })
            expect(mockAddForwardedPort).toHaveBeenCalledWith(3000, 3000, '192.168.1.1')
            expect(result).toBe(3000)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('defaults host to empty string when not provided', async () => {
            mockApiPost.mockResolvedValue({})
            mockApiGet.mockResolvedValue({ ports: [] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort } = usePortForward()

            const result = await registerPort(3000)

            expect(mockApiPost).toHaveBeenCalledWith('/api/proxy/ports', {
                port: 3000, host: '', name: '', protocol: 'http', direction: 'forward',
            })
            // When API returns no localPort, falls back to port number
            expect(result).toBe(3000)
        })
    })

    describe('updatePort', () => {
        it('puts updated port with host to API', async () => {
            mockApiPut.mockResolvedValue({})
            mockApiGet.mockResolvedValue({ ports: [] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { updatePort } = usePortForward()

            await updatePort(3000, 3000, '192.168.1.1', 'App', 'http')

            expect(mockApiPut).toHaveBeenCalledWith('/api/proxy/ports', {
                localPort: 3000, port: 3000, host: '192.168.1.1', name: 'App', protocol: 'http', direction: 'forward',
            })
        })

        it('syncs native layer after update in app mode', async () => {
            mockIsAppMode.value = true
            mockApiPut.mockResolvedValue({})
            mockApiGet.mockResolvedValue({ ports: [] })
            const mockRemove = vi.fn().mockResolvedValue(undefined).mockResolvedValue(undefined)
            const mockAdd = vi.fn().mockResolvedValue(undefined).mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { removeForwardedPort: mockRemove, addForwardedPort: mockAdd }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { updatePort } = usePortForward()

            await updatePort(3000, 4000, '10.0.0.1', 'NewApp', 'https')

            expect(mockRemove).toHaveBeenCalledWith(3000)
            expect(mockAdd).toHaveBeenCalledWith(3000, 4000, '10.0.0.1')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })
    })

    describe('unregisterPort', () => {
        it('deletes port from API and refreshes', async () => {
            mockApiDelete.mockResolvedValue({})
            mockApiGet.mockResolvedValue({ ports: [] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { unregisterPort } = usePortForward()

            await unregisterPort(3000)

            expect(mockApiDelete).toHaveBeenCalledWith('/api/proxy/ports?port=3000')
        })
    })

    describe('detectPorts', () => {
        it('fetches detected ports from API', async () => {
            const detected = [
                { port: 8080, protocol: 'http', processName: 'node', processArgs: '' },
            ]
            mockApiGet.mockResolvedValue({ ports: detected })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { detectPorts, detectedPorts, hasScanned } = usePortForward()

            await detectPorts()

            expect(mockApiGet).toHaveBeenCalledWith('/api/proxy/detect', { timeoutMs: 60_000 })
            expect(detectedPorts.value).toHaveLength(1)
            expect(hasScanned.value).toBe(true)
        })

        it('clears scanError on a successful scan', async () => {
            mockApiGet.mockRejectedValueOnce(new Error('boom'))
            const { usePortForward } = await import('@/composables/usePortForward')
            const { detectPorts, scanError } = usePortForward()
            await detectPorts()
            expect(scanError.value).toBe('boom')

            mockApiGet.mockResolvedValue({ ports: [{ port: 8080, protocol: 'http', processName: 'node', processArgs: '' }] })
            await detectPorts()
            expect(scanError.value).toBe('')
        })

        it('records the failure in scanError instead of rejecting', async () => {
            // The scan is invoked unawaited from the drawer-open handler and from
            // a @click handler, so a rejection would only reach the global
            // unhandledrejection logger. detectPorts must swallow it.
            mockApiGet.mockRejectedValue(new Error('Request timed out'))

            const { usePortForward } = await import('@/composables/usePortForward')
            const { detectPorts, scanError, detectedPorts } = usePortForward()

            await expect(detectPorts()).resolves.toBeUndefined()

            expect(scanError.value).toBe('Request timed out')
            expect(detectedPorts.value).toEqual([])
        })

        it('does not mark hasScanned after a failure so the first-open auto-scan retries', async () => {
            mockApiGet.mockRejectedValue(new Error('timed out'))

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openScanDrawer, hasScanned, scanError } = usePortForward()

            await openScanDrawer()

            expect(scanError.value).toBe('timed out')
            expect(hasScanned.value).toBe(false)

            // A second open must retry rather than silently do nothing.
            const callsBefore = mockApiGet.mock.calls.length
            await openScanDrawer()
            expect(mockApiGet.mock.calls.length).toBeGreaterThan(callsBefore)
        })

        it('clears a previous error before retrying', async () => {
            mockApiGet.mockRejectedValueOnce(new Error('timed out'))

            const { usePortForward } = await import('@/composables/usePortForward')
            const { rescanPorts, scanError } = usePortForward()

            await rescanPorts()
            expect(scanError.value).toBe('timed out')

            mockApiGet.mockResolvedValue({ ports: [] })
            await rescanPorts()
            expect(scanError.value).toBe('')
        })
    })

    describe('scan drawer', () => {
        it('openScanDrawer opens the drawer', async () => {
            mockApiGet.mockResolvedValue({ ports: [] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openScanDrawer, closeScanDrawer, scanDrawerOpen } = usePortForward()

            await openScanDrawer()
            expect(scanDrawerOpen.value).toBe(true)
        })

        it('openScanDrawer auto-scans on first open', async () => {
            mockApiGet.mockResolvedValue({ ports: [{ port: 5173, protocol: 'http', processName: 'node', processArgs: '' }] })
            const { usePortForward } = await import('@/composables/usePortForward')
            const { openScanDrawer, detectedPorts, hasScanned } = usePortForward()

            await openScanDrawer()

            expect(mockApiGet).toHaveBeenCalledWith('/api/proxy/detect', { timeoutMs: 60_000 })
            expect(detectedPorts.value).toHaveLength(1)
            expect(hasScanned.value).toBe(true)
        })

        it('openScanDrawer does not re-scan if already scanned', async () => {
            mockApiGet.mockResolvedValue({ ports: [{ port: 8080, protocol: 'http', processName: 'node', processArgs: '' }] })
            const { usePortForward } = await import('@/composables/usePortForward')
            const { openScanDrawer } = usePortForward()

            await openScanDrawer()
            const callsAfterFirstScan = mockApiGet.mock.calls.length
            await openScanDrawer()
            expect(mockApiGet.mock.calls.length).toBe(callsAfterFirstScan)
        })

        it('rescanPorts forces a new scan', async () => {
            mockApiGet.mockResolvedValue({ ports: [] })
            const { usePortForward } = await import('@/composables/usePortForward')
            const { openScanDrawer, rescanPorts, detectedPorts } = usePortForward()

            await openScanDrawer()
            mockApiGet.mockResolvedValue({ ports: [{ port: 9090, protocol: 'http', processName: 'go', processArgs: '' }] })
            await rescanPorts()
            expect(detectedPorts.value).toHaveLength(1)
        })

        it('closeScanDrawer closes the drawer', async () => {
            mockApiGet.mockResolvedValue({ ports: [] })
            const { usePortForward } = await import('@/composables/usePortForward')
            const { openScanDrawer, closeScanDrawer, scanDrawerOpen } = usePortForward()

            await openScanDrawer()
            closeScanDrawer()
            expect(scanDrawerOpen.value).toBe(false)
        })
    })

    describe('setPortEnabled', () => {
        it('calls the enabled endpoint and refreshes ports', async () => {
            mockApiPut.mockResolvedValue({ status: 'ok' })
            mockApiGet.mockResolvedValue({ ports: [] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { setPortEnabled } = usePortForward()

            await setPortEnabled(8080, false)

            expect(mockApiPut).toHaveBeenCalledWith('/api/proxy/ports/enabled', { localPort: 8080, enabled: false })
            expect(mockApiGet).toHaveBeenCalledWith('/api/proxy/ports')
        })

        it('removes the native forward when disabling in app mode', async () => {
            mockIsAppMode.value = true
            mockApiPut.mockResolvedValue({ status: 'ok' })
            mockApiGet.mockResolvedValue({
                ports: [{ port: 8080, localPort: 8080, host: '', name: 'API', protocol: 'http', active: true, enabled: false }],
            })
            const mockRemove = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { removeForwardedPort: mockRemove }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { setPortEnabled } = usePortForward()

            await setPortEnabled(8080, false)

            expect(mockRemove).toHaveBeenCalledWith(8080)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('adds the native forward when enabling in app mode', async () => {
            mockIsAppMode.value = true
            mockApiPut.mockResolvedValue({ status: 'ok' })
            mockApiGet.mockResolvedValue({
                ports: [{ port: 8080, localPort: 8080, host: '192.168.1.1', name: 'API', protocol: 'http', active: true, enabled: true }],
            })
            const mockAdd = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAdd }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { setPortEnabled } = usePortForward()

            await setPortEnabled(8080, true)

            expect(mockAdd).toHaveBeenCalledWith(8080, 8080, '192.168.1.1')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('does not touch native layer in web mode', async () => {
            mockIsAppMode.value = false
            mockApiPut.mockResolvedValue({ status: 'ok' })
            mockApiGet.mockResolvedValue({
                ports: [{ port: 8080, localPort: 8080, host: '', name: 'API', protocol: 'http', active: true, enabled: false }],
            })
            const mockRemove = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { removeForwardedPort: mockRemove }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { setPortEnabled } = usePortForward()

            await setPortEnabled(8080, false)

            expect(mockRemove).not.toHaveBeenCalled()

            delete (window as any).ClawBenchNative
        })
    })

    describe('setPortEnabled', () => {
        it('does not throw when the Android synchronous bridge returns undefined', async () => {
            // Android @JavascriptInterface writes are synchronous and return plain
            // undefined (no Promise), unlike Electron. Regression: a .catch() chained
            // directly on the call used to throw "Cannot read properties of undefined".
            mockIsAppMode.value = true
            mockApiPut.mockResolvedValue({ status: 'ok' })
            mockApiGet.mockResolvedValue({
                ports: [{ port: 8080, localPort: 8080, host: '192.168.1.1', name: 'API', protocol: 'http', active: true, enabled: true }],
            })
            const mockAdd = vi.fn(() => undefined)
            const mockRemove = vi.fn(() => undefined)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAdd, removeForwardedPort: mockRemove }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { setPortEnabled } = usePortForward()

            await expect(setPortEnabled(8080, true)).resolves.toBeUndefined()
            expect(mockAdd).toHaveBeenCalledWith(8080, 8080, '192.168.1.1')

            await expect(setPortEnabled(8080, false)).resolves.toBeUndefined()
            expect(mockRemove).toHaveBeenCalledWith(8080)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })
    })

    describe('ensurePortRegistered', () => {
        it('returns existing localPort if port already exists', async () => {
            mockApiGet.mockResolvedValue({
                ports: [{ port: 3000, name: 'App', protocol: 'http', active: true, enabled: true, localPort: 3000, host: '' }],
            })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { ensurePortRegistered, loadPorts } = usePortForward()

            // First load ports so the port exists
            await loadPorts()

            // Should not call registerPort
            const result = await ensurePortRegistered(3000, 'http')

            expect(mockApiPost).not.toHaveBeenCalled()
            expect(result).toBe(3000)
        })

        it('re-enables an existing port that is currently disabled', async () => {
            mockApiGet.mockResolvedValue({
                ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: false, enabled: false }],
            })
            mockApiPut.mockResolvedValue({ status: 'ok' })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { ensurePortRegistered, loadPorts } = usePortForward()

            await loadPorts()

            const result = await ensurePortRegistered(3000, 'http')

            // Should not re-register, but re-enable the disabled port so it can be opened.
            expect(mockApiPost).not.toHaveBeenCalled()
            expect(mockApiPut).toHaveBeenCalledWith('/api/proxy/ports/enabled', { localPort: 3000, enabled: true })
            expect(result).toBe(3000)
        })

        it('does not re-enable an already-enabled port', async () => {
            mockApiGet.mockResolvedValue({
                ports: [{ port: 3000, localPort: 3000, name: 'App', protocol: 'http', active: true, enabled: true }],
            })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { ensurePortRegistered, loadPorts } = usePortForward()

            await loadPorts()
            const result = await ensurePortRegistered(3000, 'http')

            expect(mockApiPut).not.toHaveBeenCalled()
            expect(result).toBe(3000)
        })

        it('registers and returns localPort if port not yet registered', async () => {
            mockApiGet.mockResolvedValue({ ports: [] })
            mockApiPost.mockResolvedValue({ localPort: 5173 })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { ensurePortRegistered } = usePortForward()

            const result = await ensurePortRegistered(5173, 'http')

            expect(mockApiPost).toHaveBeenCalledWith('/api/proxy/ports', {
                port: 5173, host: '', name: '', protocol: 'http', direction: 'forward',
            })
            expect(result).toBe(5173)
        })

        it('matches both port and host when finding existing entry', async () => {
            mockApiGet.mockResolvedValue({
                ports: [
                    { port: 3000, localPort: 3000, host: '', name: 'Local', protocol: 'http', active: true, enabled: true },
                    { port: 3000, localPort: 3001, host: '192.168.1.1', name: 'Remote', protocol: 'http', active: true, enabled: true },
                ],
            })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { ensurePortRegistered, loadPorts } = usePortForward()

            await loadPorts()

            // Should find the entry with matching host
            const result = await ensurePortRegistered(3000, 'http', '192.168.1.1')
            expect(result).toBe(3001)

            // Empty host should find the other entry
            const result2 = await ensurePortRegistered(3000, 'http')
            expect(result2).toBe(3000)
        })
    })

    describe('syncToNative', () => {
        it('stops native service when no enabled ports are registered', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({ ports: [] })
            const mockStop = vi.fn()
            ;(window as any).ClawBenchNative = { stopBackgroundService: mockStop }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { syncToNative } = usePortForward()

            await syncToNative()

            expect(mockStop).toHaveBeenCalled()

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        /**
         * The no-ports branch is destructive on Android: it clears the persisted
         * forwarded_ports list so a cold start cannot restore a service that can
         * never connect. A transient fetch failure must NOT reach it — otherwise
         * a flaky network would silently drop the user's port forwards.
         */
        it('does not stop native service when the port list fetch fails', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockRejectedValue(new Error('network down'))
            const mockStop = vi.fn()
            ;(window as any).ClawBenchNative = { stopBackgroundService: mockStop }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { syncToNative } = usePortForward()

            await expect(syncToNative()).rejects.toThrow('network down')

            expect(mockStop).not.toHaveBeenCalled()

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('registers each port with host to native layer', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({
                ports: [
                    { port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: true, enabled: true },
                    { port: 8080, localPort: 8080, host: '192.168.1.1', name: 'API', protocol: 'http', active: true, enabled: true },
                ],
            })
            const mockAdd = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAdd }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { syncToNative } = usePortForward()

            await syncToNative()

            expect(mockAdd).toHaveBeenCalledWith(3000, 3000, '')
            expect(mockAdd).toHaveBeenCalledWith(8080, 8080, '192.168.1.1')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('skips disabled ports when syncing to native layer', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({
                ports: [
                    { port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: true, enabled: false },
                    { port: 8080, localPort: 8080, host: '', name: 'API', protocol: 'http', active: true, enabled: true },
                ],
            })
            const mockAdd = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAdd }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { syncToNative } = usePortForward()

            await syncToNative()

            expect(mockAdd).not.toHaveBeenCalledWith(3000, 3000, '')
            expect(mockAdd).toHaveBeenCalledWith(8080, 8080, '')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('does nothing when not in app mode', async () => {
            mockIsAppMode.value = false
            mockApiGet.mockResolvedValue({ ports: [] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { syncToNative } = usePortForward()

            // Should not throw or call any native methods
            await syncToNative()

            expect(mockApiGet).not.toHaveBeenCalled()
        })

        it('removes stale native forwards that are no longer enabled on the server', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({
                ports: [
                    { port: 8080, localPort: 8080, host: '', name: 'API', protocol: 'http', active: true, enabled: true },
                ],
            })
            const mockAdd = vi.fn().mockResolvedValue(undefined)
            const mockRemove = vi.fn().mockResolvedValue(undefined)
            // Native still has a stale 3000 forward from before it was disabled/removed.
            ;(window as any).ClawBenchNative = {
                addForwardedPort: mockAdd,
                removeForwardedPort: mockRemove,
                getForwardedPorts: async () => JSON.stringify([{ port: 8080, host: '' }, { port: 3000, host: '' }]),
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { syncToNative } = usePortForward()

            await syncToNative()

            // Stale 3000 is removed, 8080 (enabled) is added.
            expect(mockRemove).toHaveBeenCalledWith(3000)
            expect(mockRemove).not.toHaveBeenCalledWith(8080)
            expect(mockAdd).toHaveBeenCalledWith(8080, 8080, '')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('tolerates malformed getForwardedPorts response', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({
                ports: [
                    { port: 8080, localPort: 8080, host: '', name: 'API', protocol: 'http', active: true, enabled: true },
                ],
            })
            const mockAdd = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = {
                addForwardedPort: mockAdd,
                removeForwardedPort: vi.fn(),
                getForwardedPorts: async () => 'not-json',
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { syncToNative } = usePortForward()

            await syncToNative()

            // Malformed JSON is ignored; enabled port is still added.
            expect(mockAdd).toHaveBeenCalledWith(8080, 8080, '')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })
    })

    describe('connectingPorts', () => {
        it('adds port to connectingPorts after registerPort', async () => {
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: false }] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts } = usePortForward()

            await registerPort(3000, 'App', 'http')

            expect(connectingPorts.value.has(3000)).toBe(true)
        })

        it('removes port from connectingPorts on native callback success', async () => {
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            // Backend initially reports inactive — port stays in connectingPorts
            // until the native callback confirms success or backend becomes active.
            mockApiGet.mockResolvedValue({ ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: false }] })
            const mockAddForwardedPort = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts } = usePortForward()

            await registerPort(3000, 'App', 'http')
            expect(connectingPorts.value.has(3000)).toBe(true)

            // Simulate native callback dispatching the CustomEvent
            const event = new CustomEvent('clawbench-port-forward-result', {
                detail: { localPort: 3000, success: true }
            })
            window.dispatchEvent(event)

            // Port should be removed from connectingPorts
            expect(connectingPorts.value.has(3000)).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('clears connectingPorts when the native bind resolves true (Electron)', async () => {
            // Electron's addForwardedPort returns Promise<boolean> and resolves
            // true once the local listener is bound. Android returns void and
            // reports through the CustomEvent instead, so this is the ONLY
            // success signal the desktop shell has — without it the dot stayed
            // amber until the user pressed reconnect, even though the port was
            // genuinely listening.
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: false }] })
            const mockAddForwardedPort = vi.fn().mockResolvedValue(true)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts } = usePortForward()

            await registerPort(3000, 'App', 'http')
            // The bind promise resolved during the await, so the pending
            // indicator is already gone — no reconnect click needed.
            expect(connectingPorts.value.has(3000)).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('keeps connectingPorts pending when the native bind returns void (Android)', async () => {
            // Android's addForwardedPort is synchronous void: it cannot report
            // success inline, so the port must stay pending until the native
            // CustomEvent arrives. Pins that the Electron success branch above
            // does not accidentally clear the Android case.
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: false }] })
            const mockAddForwardedPort = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts } = usePortForward()

            await registerPort(3000, 'App', 'http')
            await Promise.resolve()
            await Promise.resolve()
            expect(connectingPorts.value.has(3000)).toBe(true)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('removes port from connectingPorts on native callback failure', async () => {
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: false }] })
            const mockAddForwardedPort = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts } = usePortForward()

            await registerPort(3000, 'App', 'http')
            expect(connectingPorts.value.has(3000)).toBe(true)

            // Simulate native callback with failure
            const event = new CustomEvent('clawbench-port-forward-result', {
                detail: { localPort: 3000, success: false }
            })
            window.dispatchEvent(event)

            // Port should be removed from connectingPorts even on failure
            expect(connectingPorts.value.has(3000)).toBe(false)
            // Error toast should be shown
            expect(mockToastShow).toHaveBeenCalledWith('portForward.portUnreachable', expect.objectContaining({ type: 'error' }))

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('clears connectingPorts when backend reports active during loadPorts (app mode)', async () => {
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            // registerPort calls loadPorts(true) + loadSSHInfo(), then our explicit loadPorts(true)
            // loadSSHInfo also calls apiGet, so we need enough mock responses
            mockApiGet
                .mockResolvedValueOnce({ ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: false }] })  // loadPorts in registerPort
                .mockResolvedValueOnce({ enabled: false, host: '', port: 0, username: '', fingerprint: '', command: '', connectionStats: null })  // loadSSHInfo in registerPort
                .mockResolvedValueOnce({ ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: true }] })  // explicit loadPorts

            const mockAddForwardedPort = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts, loadPorts } = usePortForward()

            await registerPort(3000, 'App', 'http')
            expect(connectingPorts.value.has(3000)).toBe(true)

            // loadPorts with active=true should clear connectingPorts even in app mode
            await loadPorts(true)

            expect(connectingPorts.value.has(3000)).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('removes port from connectingPorts when backend reports active in web mode', async () => {
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            // registerPort calls loadPorts(true) + loadSSHInfo(), then our explicit loadPorts(true)
            // loadSSHInfo also calls apiGet, so we need enough mock responses
            mockApiGet
                .mockResolvedValueOnce({ ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: false }] })  // loadPorts in registerPort
                .mockResolvedValueOnce({ enabled: false, host: '', port: 0, username: '', fingerprint: '', command: '', connectionStats: null })  // loadSSHInfo in registerPort
                .mockResolvedValueOnce({ ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: true }] })  // explicit loadPorts

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts, loadPorts } = usePortForward()

            await registerPort(3000, 'App', 'http')
            expect(connectingPorts.value.has(3000)).toBe(true)

            // loadPorts with active=true should clear connectingPorts
            await loadPorts(true)

            expect(connectingPorts.value.has(3000)).toBe(false)
        })

        it('re-keys the server and the pending set when the native bind lands on another port', async () => {
            // Desktop returns the ACTUAL port when the requested one was taken
            // locally. The server registry (DELETE/PUT/enable are keyed by it)
            // and the UI URL must follow, or they point at a port nothing
            // listens on.
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [{ port: 3000, localPort: 3001, host: '', name: 'App', protocol: 'http', active: false }] })
            const mockAddForwardedPort = vi.fn().mockResolvedValue({ ok: true, port: 3001 })
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts } = usePortForward()

            const actual = await registerPort(3000, 'App', 'http')

            // The caller gets the real port (a localhost-URL click must open it).
            expect(actual).toBe(3001)
            // The rebind endpoint was called with (requested, actual).
            expect(mockApiPost).toHaveBeenCalledWith('/api/proxy/ports/rebind', { localPort: 3000, newLocalPort: 3001 })
            // The pending indicator followed the move rather than clearing on the
            // now-unused requested port.
            expect(connectingPorts.value.has(3001)).toBe(true)
            expect(connectingPorts.value.has(3000)).toBe(false)
            // No misleading "check the service" toast for a local port conflict.
            expect(mockToastShow).not.toHaveBeenCalledWith('portForward.portUnreachable', expect.anything())

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('releases the rebound listener and reports a conflict when the server refuses the re-key', async () => {
            // Another mapping may take the port between our local bind and the
            // rebind call (409). The listener we bound must be released, and the
            // user must see the port-specific copy, not "check the service".
            mockIsAppMode.value = true
            mockApiPost.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports/rebind') return Promise.reject(new Error('409'))
                return Promise.resolve({ localPort: 3000 })
            })
            mockApiGet.mockResolvedValue({ ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: false }] })
            const mockAddForwardedPort = vi.fn().mockResolvedValue({ ok: true, port: 3001 })
            const mockRemoveForwardedPort = vi.fn()
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAddForwardedPort, removeForwardedPort: mockRemoveForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts } = usePortForward()

            const actual = await registerPort(3000, 'App', 'http')

            expect(actual).toBe(3000)
            // The orphaned listener is torn down — never left bound while the
            // registry has no entry for it.
            expect(mockRemoveForwardedPort).toHaveBeenCalledWith(3001)
            expect(mockToastShow).toHaveBeenCalledWith('portForward.portConflict', expect.objectContaining({ type: 'error' }))
            expect(mockToastShow).not.toHaveBeenCalledWith('portForward.portUnreachable', expect.anything())
            expect(connectingPorts.value.has(3000)).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('reports a local bind conflict with the port-specific toast', async () => {
            // The desktop main process distinguishes an occupied port from an
            // unreachable tunnel; the conflict copy must not be the
            // "check if the service is running" one.
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [] })
            const mockAddForwardedPort = vi.fn().mockResolvedValue({ ok: false, reason: 'conflict' })
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort } = usePortForward()

            await registerPort(3000, 'App', 'http')

            expect(mockToastShow).toHaveBeenCalledWith('portForward.portConflict', expect.objectContaining({ type: 'error' }))
            expect(mockToastShow).not.toHaveBeenCalledWith('portForward.portUnreachable', expect.anything())

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('keeps the "unreachable" copy for a non-conflict bind failure', async () => {
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [] })
            const mockAddForwardedPort = vi.fn().mockResolvedValue({ ok: false, reason: 'unreachable' })
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort } = usePortForward()

            await registerPort(3000, 'App', 'http')

            expect(mockToastShow).toHaveBeenCalledWith('portForward.portUnreachable', expect.objectContaining({ type: 'error' }))
            expect(mockToastShow).not.toHaveBeenCalledWith('portForward.portConflict', expect.anything())

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('re-keys on the Android event when the bound port differs from the requested one', async () => {
            // Android's verdict arrives as a CustomEvent carrying both ports.
            // When the listener had to move, the server registry and the pending
            // set must follow, and the user must be told which port is now live.
            mockIsAppMode.value = true
            mockApiPost.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports/rebind') return Promise.resolve({ status: 'ok' })
                return Promise.resolve({ localPort: 3000 })
            })
            mockApiGet.mockResolvedValue({ ports: [] })
            const mockAddForwardedPort = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts } = usePortForward()

            await registerPort(3000, 'App', 'http')
            window.dispatchEvent(new CustomEvent('clawbench-port-forward-result', {
                detail: { localPort: 3001, requestedLocalPort: 3000, success: true },
            }))
            await Promise.resolve()
            await Promise.resolve()
            await Promise.resolve()

            expect(mockApiPost).toHaveBeenCalledWith('/api/proxy/ports/rebind', { localPort: 3000, newLocalPort: 3001 })
            expect(mockToastShow).toHaveBeenCalledWith(
                'portForward.portRebound',
                expect.objectContaining({ type: 'info' }),
            )
            expect(connectingPorts.value.has(3000)).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('shows the conflict copy on an Android bind conflict', async () => {
            // The Android host reports `reason: 'conflict'` when no free port
            // could be bound; that must not surface as "check the service".
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [] })
            const mockAddForwardedPort = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort } = usePortForward()

            await registerPort(3000, 'App', 'http')
            window.dispatchEvent(new CustomEvent('clawbench-port-forward-result', {
                detail: { localPort: 3000, success: false, reason: 'conflict' },
            }))

            expect(mockToastShow).toHaveBeenCalledWith('portForward.portConflict', expect.objectContaining({ type: 'error' }))
            expect(mockToastShow).not.toHaveBeenCalledWith('portForward.portUnreachable', expect.anything())

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })
    })

    describe('tunnel health native & polling', () => {
        it('unregisterPort removes the native forward in app mode', async () => {
            mockIsAppMode.value = true
            mockApiDelete.mockResolvedValue({})
            mockApiGet.mockResolvedValue({ ports: [] })
            const mockRemove = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = { removeForwardedPort: mockRemove }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { unregisterPort } = usePortForward()

            await unregisterPort(3000)

            expect(mockRemove).toHaveBeenCalledWith(3000)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('checkTunnelHealth marks degraded in app mode when native connected but ports degraded', async () => {
            mockIsAppMode.value = true
            mockTunnelStatusFromPorts.mockImplementation(() => 'degraded')
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [{ port: 3000, name: 'App', protocol: 'http', active: true }] }
                if (url === '/api/ssh/info/full') return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: null }
                return {}
            })
            ;(window as any).ClawBenchNative = { isTunnelConnected: async () => true }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelMessage, tunnelChecking } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelStatus.value).toBe('degraded')
            expect(tunnelMessage.value).toBe('portForward.tunnelDegraded')
            expect(tunnelChecking.value).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('checkTunnelHealth marks ok in app mode when native connected and ports ok', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [{ port: 3000, name: 'App', protocol: 'http', active: true }] }
                if (url === '/api/ssh/info/full') return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: null }
                return {}
            })
            ;(window as any).ClawBenchNative = { isTunnelConnected: async () => true }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelChecking } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelStatus.value).toBe('ok')
            expect(tunnelChecking.value).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('checkTunnelHealth marks disconnected in app mode with native error details', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: true, clientCount: 0, activeChannels: 0 } }
                return {}
            })
            ;(window as any).ClawBenchNative = {
                isTunnelConnected: async () => false,
                getTunnelError: async () => 'native ssh error',
                getTunnelErrorType: async () => 'auth',
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelMessage, tunnelError, tunnelErrorType, tunnelChecking } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelStatus.value).toBe('disconnected')
            expect(tunnelMessage.value).toBe('portForward.tunnelDisconnected')
            expect(tunnelError.value).toBe('native ssh error')
            expect(tunnelErrorType.value).toBe('auth')
            expect(tunnelChecking.value).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('checkTunnelHealth falls back to server stats when native tunnel status unavailable', async () => {
            mockIsAppMode.value = true
            // Native has no isTunnelConnected — getNativeTunnelStatus returns null.
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [{ port: 3000, name: 'App', protocol: 'http', active: true, enabled: true }] }
                if (url === '/api/ssh/info/full') return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: true, clientCount: 1, activeChannels: 1 } }
                return {}
            })
            ;(window as any).ClawBenchNative = {}

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelStatus.value).toBe('ok')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('checkTunnelHealth tolerates native isTunnelConnected throwing', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: true, clientCount: 1, activeChannels: 1 } }
                return {}
            })
            ;(window as any).ClawBenchNative = { isTunnelConnected: async () => { throw new Error('bridge') } }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelStatus.value).toBe('ok')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('checkTunnelHealth treats non-boolean native tunnel status as unavailable', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: true, clientCount: 1, activeChannels: 1 } }
                return {}
            })
            ;(window as any).ClawBenchNative = { isTunnelConnected: async () => 'yes' as any }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelStatus.value).toBe('ok')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('checkTunnelHealth handles native error type getter throwing', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: true, clientCount: 0, activeChannels: 0 } }
                return {}
            })
            ;(window as any).ClawBenchNative = {
                isTunnelConnected: async () => false,
                getTunnelError: async () => 'err',
                getTunnelErrorType: async () => { throw new Error('boom') },
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelErrorType } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelStatus.value).toBe('disconnected')
            expect(tunnelErrorType.value).toBe('')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('checkTunnelHealth handles native error getter failures and invalid error types', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: true, clientCount: 0, activeChannels: 0 } }
                return {}
            })
            ;(window as any).ClawBenchNative = {
                isTunnelConnected: async () => false,
                getTunnelError: async () => { throw new Error('boom') },
                getTunnelErrorType: async () => 'bogus' as any,
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelError, tunnelErrorType } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelStatus.value).toBe('disconnected')
            expect(tunnelError.value).toBe('')
            expect(tunnelErrorType.value).toBe('')

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('checkTunnelHealth returns early when SSH enabled but connectionStats null', async () => {
            mockIsAppMode.value = false
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                if (url === '/api/ssh/info/full') return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: null }
                return {}
            })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelChecking } = usePortForward()

            tunnelStatus.value = 'ok' as any

            await checkTunnelHealth()

            expect(tunnelStatus.value).toBe('unknown')
            expect(tunnelChecking.value).toBe(false)
        })

        it('checkTunnelHealth marks degraded from server stats when ports degraded', async () => {
            mockIsAppMode.value = false
            mockTunnelStatusFromPorts.mockImplementation(() => 'degraded')
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [{ port: 3000, name: 'App', protocol: 'http', active: false, enabled: true }] }
                if (url === '/api/ssh/info/full') return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: true, clientCount: 1, activeChannels: 1 } }
                return {}
            })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelMessage } = usePortForward()

            await checkTunnelHealth()

            expect(tunnelStatus.value).toBe('degraded')
            expect(tunnelMessage.value).toBe('portForward.tunnelDegraded')
        })
    })

    describe('tunnel poll timer', () => {
        it('poll stops when native reports connected and status is ok', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: true, clientCount: 0, activeChannels: 0 } }
            })
            ;(window as any).ClawBenchNative = { isTunnelConnected: async () => true }

            vi.useFakeTimers()
            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelMessage } = usePortForward()

            // Force a degraded state so a poll is started.
            mockTunnelStatusFromPorts.mockImplementation(() => 'degraded')
            await checkTunnelHealth()
            expect(tunnelStatus.value).toBe('degraded')

            // Native now reports connected and status recovers to ok → poll stops.
            mockTunnelStatusFromPorts.mockImplementation(() => 'ok')
            await vi.advanceTimersByTimeAsync(5000)

            expect(tunnelStatus.value).toBe('ok')
            expect(tunnelMessage.value).toBe('')

            vi.useRealTimers()
            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('poll recovers via server-side check when native status is null', async () => {
            mockIsAppMode.value = true
            mockApiGet
                .mockResolvedValueOnce({ ports: [] })                                                       // checkTunnelHealth loadPorts
                .mockResolvedValueOnce({ enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: false, clientCount: 0, activeChannels: 0 } }) // checkTunnelHealth loadSSHInfo → disconnected, starts poll
                .mockResolvedValueOnce({ enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: true, clientCount: 1, activeChannels: 1 } })   // poll loadSSHInfo → connected
                .mockResolvedValueOnce({ ports: [] })                                                       // poll loadPorts
            ;(window as any).ClawBenchNative = { isTunnelConnected: async () => null }

            vi.useFakeTimers()
            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelMessage } = usePortForward()

            await checkTunnelHealth()
            expect(tunnelStatus.value).toBe('disconnected')

            await vi.advanceTimersByTimeAsync(5000)

            expect(tunnelStatus.value).toBe('ok')
            expect(tunnelMessage.value).toBe('')

            vi.useRealTimers()
            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('poll stays degraded when native connected but ports degraded', async () => {
            mockIsAppMode.value = true
            mockTunnelStatusFromPorts.mockImplementation(() => 'degraded')
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') return { ports: [] }
                return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: true, clientCount: 0, activeChannels: 0 } }
            })
            ;(window as any).ClawBenchNative = { isTunnelConnected: async () => true }

            vi.useFakeTimers()
            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelMessage } = usePortForward()

            await checkTunnelHealth()
            expect(tunnelStatus.value).toBe('degraded')

            await vi.advanceTimersByTimeAsync(5000)

            expect(tunnelStatus.value).toBe('degraded')
            expect(tunnelMessage.value).toBe('portForward.tunnelDegraded')

            vi.useRealTimers()
            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('poll stays degraded when server connected but ports degraded', async () => {
            mockIsAppMode.value = false
            mockTunnelStatusFromPorts.mockImplementation(() => 'degraded')
            mockApiGet
                .mockResolvedValueOnce({ ports: [] })
                .mockResolvedValueOnce({ enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: false, clientCount: 0, activeChannels: 0 } })
                .mockResolvedValueOnce({ enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: true, clientCount: 1, activeChannels: 1 } })
                .mockResolvedValueOnce({ ports: [] })
            ;(window as any).ClawBenchNative = {}

            vi.useFakeTimers()
            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelMessage } = usePortForward()

            await checkTunnelHealth()
            expect(tunnelStatus.value).toBe('disconnected')

            await vi.advanceTimersByTimeAsync(5000)

            expect(tunnelStatus.value).toBe('degraded')
            expect(tunnelMessage.value).toBe('portForward.tunnelDegraded')

            vi.useRealTimers()
            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('poll recovers to ok when server says disconnected but ports are active', async () => {
            mockIsAppMode.value = false
            mockApiGet
                .mockResolvedValueOnce({ ports: [] })                                                       // checkTunnelHealth loadPorts
                .mockResolvedValueOnce({ enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: false, clientCount: 0, activeChannels: 0 } }) // checkTunnelHealth loadSSHInfo → disconnected, starts poll
                .mockResolvedValueOnce({ enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: false, clientCount: 0, activeChannels: 0 } }) // poll loadSSHInfo → still disconnected
                .mockResolvedValueOnce({ ports: [{ port: 3000, name: 'App', protocol: 'http', active: true, enabled: true }] })              // poll loadPorts → active ports
            ;(window as any).ClawBenchNative = {}

            vi.useFakeTimers()
            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth, tunnelStatus, tunnelMessage } = usePortForward()

            await checkTunnelHealth()
            expect(tunnelStatus.value).toBe('disconnected')

            await vi.advanceTimersByTimeAsync(5000)

            expect(tunnelStatus.value).toBe('ok')
            expect(tunnelMessage.value).toBe('')

            vi.useRealTimers()
            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })
    })

    describe('module-level state sharing', () => {
        it('shares sshInfo ref across multiple usePortForward calls', async () => {
            const sshInfoResponse = { enabled: true, host: 'test', port: 20001, username: 'u', fingerprint: 'f', command: 'c', connectionStats: null }
            mockApiGet.mockResolvedValue(sshInfoResponse)

            const { usePortForward } = await import('@/composables/usePortForward')
            const instance1 = usePortForward()
            const instance2 = usePortForward()

            await instance1.loadSSHInfo()

            // Both instances should see the same sshInfo (module-level singleton)
            expect(instance1.sshInfo.value).toEqual(sshInfoResponse)
            expect(instance2.sshInfo.value).toEqual(sshInfoResponse)
        })
    })

    describe('native forward failure reporting', () => {
        it('reports failure when addForwardedPort resolves false (Electron shape)', async () => {
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [] })
            // Electron returns a Promise<boolean>; false = listener could not bind.
            const mockAdd = vi.fn().mockResolvedValue(false)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAdd }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts } = usePortForward()

            await registerPort(3000, 'App', 'http')
            // Let the fire-and-forget .then() settle.
            await Promise.resolve()
            await Promise.resolve()

            expect(mockToastShow).toHaveBeenCalledWith(
                'portForward.portUnreachable',
                expect.objectContaining({ type: 'error' }),
            )
            // The pending yellow dot must not be left spinning on a dead forward.
            expect(connectingPorts.value.has(3000)).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('does NOT report failure when addForwardedPort returns undefined (Android shape)', async () => {
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [] })
            // Android's @JavascriptInterface returns void → undefined. Treating
            // that as failure would pop a false error on every Android call.
            const mockAdd = vi.fn().mockReturnValue(undefined)
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAdd }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort } = usePortForward()

            await registerPort(3000, 'App', 'http')
            await Promise.resolve()
            await Promise.resolve()

            expect(mockToastShow).not.toHaveBeenCalledWith(
                'portForward.portUnreachable',
                expect.anything(),
            )

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('reports failure when addForwardedPort rejects', async () => {
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [] })
            const mockAdd = vi.fn().mockRejectedValue(new Error('bridge down'))
            ;(window as any).ClawBenchNative = { addForwardedPort: mockAdd }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort } = usePortForward()

            await registerPort(3000, 'App', 'http')
            await Promise.resolve()
            await Promise.resolve()

            expect(mockToastShow).toHaveBeenCalledWith(
                'portForward.portUnreachable',
                expect.objectContaining({ type: 'error' }),
            )

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })
    })

    describe('local reachability probing', () => {        it('fills localReachable from testPortReachable in app mode', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({
                ports: [
                    { port: 3000, localPort: 3000, host: '', name: 'A', protocol: 'http', active: true, enabled: true },
                    { port: 8080, localPort: 8080, host: '', name: 'B', protocol: 'http', active: true, enabled: true },
                ],
            })
            // 3000's tunnel is dead locally even though the server says the
            // target port is up — this is the false-green the dot must expose.
            const mockTest = vi.fn(async (p: number) => p === 8080)
            ;(window as any).ClawBenchNative = { testPortReachable: mockTest }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { loadPorts, localReachable } = usePortForward()

            await loadPorts()

            expect(mockTest).toHaveBeenCalledWith(3000)
            expect(mockTest).toHaveBeenCalledWith(8080)
            expect(localReachable.value.get(3000)).toBe(false)
            expect(localReachable.value.get(8080)).toBe(true)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('does not probe in web mode (no local tunnel exists)', async () => {
            mockIsAppMode.value = false
            mockApiGet.mockResolvedValue({
                ports: [{ port: 3000, localPort: 3000, host: '', name: 'A', protocol: 'http', active: true, enabled: true }],
            })
            const mockTest = vi.fn()
            ;(window as any).ClawBenchNative = { testPortReachable: mockTest }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { loadPorts, localReachable } = usePortForward()

            await loadPorts()

            expect(mockTest).not.toHaveBeenCalled()
            expect(localReachable.value.size).toBe(0)

            delete (window as any).ClawBenchNative
        })

        it('treats an unreachable local listener as degraded even when the server reports active', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({
                ports: [{ port: 3000, localPort: 3000, host: '', name: 'A', protocol: 'http', active: true, enabled: true }],
            })
            // The pure util is mocked, so capture the ports array it was handed:
            // that array is what effectivePorts() produced.
            let seen: Array<{ active: boolean }> = []
            mockTunnelStatusFromPorts.mockImplementation((p: Array<{ active: boolean }>) => {
                seen = p
                return 'degraded'
            })
            ;(window as any).ClawBenchNative = { testPortReachable: vi.fn(async () => false) }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { loadPorts, checkTunnelHealth } = usePortForward()

            mockApiGet.mockImplementation(async (url: string) => {
                if (url === '/api/proxy/ports') {
                    return { ports: [{ port: 3000, localPort: 3000, host: '', name: 'A', protocol: 'http', active: true, enabled: true }] }
                }
                return { enabled: true, host: 'h', port: 20001, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: true, clientCount: 1, activeChannels: 1 } }
            })
            await loadPorts()
            await checkTunnelHealth()

            // Server says active, but the local listener is dead → not active.
            expect(seen.length).toBeGreaterThan(0)
            expect(seen.every(p => p.active === false)).toBe(true)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })
    })

    describe('poll-driven reconnect', () => {
        it('syncToNative arms the health poll so a later drop self-heals', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') {
                    return { ports: [{ port: 3000, localPort: 3000, host: '', name: 'A', protocol: 'http', active: true, enabled: true }] }
                }
                return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: null }
            })
            // Native accepts the forward, then reports the tunnel as down.
            const mockReconnect = vi.fn().mockResolvedValue(true)
            ;(window as any).ClawBenchNative = {
                addForwardedPort: vi.fn().mockResolvedValue(true),
                isTunnelConnected: async () => false,
                reconnectTunnelAsync: mockReconnect,
                getTunnelError: async () => '',
                getTunnelErrorType: async () => '',
            }

            vi.useFakeTimers()
            const { usePortForward } = await import('@/composables/usePortForward')
            const { syncToNative } = usePortForward()

            await syncToNative()
            mockReconnect.mockClear()

            // The poll must have been armed by the sync — otherwise a drop after
            // startup would never be noticed until the user opened the panel.
            await vi.advanceTimersByTimeAsync(5000)

            expect(mockReconnect).toHaveBeenCalled()

            vi.useRealTimers()
            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('asks native to reconnect when the tunnel is down but ports are enabled', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') {
                    return { ports: [{ port: 3000, localPort: 3000, host: '', name: 'A', protocol: 'http', active: false, enabled: true }] }
                }
                return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: null }
            })
            const mockReconnect = vi.fn().mockResolvedValue(true)
            ;(window as any).ClawBenchNative = {
                isTunnelConnected: async () => false,
                reconnectTunnelAsync: mockReconnect,
                getTunnelError: async () => '',
                getTunnelErrorType: async () => '',
            }

            vi.useFakeTimers()
            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth } = usePortForward()

            // Arming the poll requires a disconnected status.
            await checkTunnelHealth()
            mockReconnect.mockClear()

            await vi.advanceTimersByTimeAsync(5000)

            // Recovery must not depend on the user pressing retry.
            expect(mockReconnect).toHaveBeenCalled()

            vi.useRealTimers()
            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('does not reconnect when native status is unknown (null)', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') {
                    return { ports: [{ port: 3000, localPort: 3000, host: '', name: 'A', protocol: 'http', active: false, enabled: true }] }
                }
                return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: { connected: false, clientCount: 0, activeChannels: 0 } }
            })
            const mockReconnect = vi.fn().mockResolvedValue(true)
            // No isTunnelConnected → getNativeTunnelStatus() resolves null.
            ;(window as any).ClawBenchNative = { reconnectTunnelAsync: mockReconnect }

            vi.useFakeTimers()
            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth } = usePortForward()

            await checkTunnelHealth()
            await vi.advanceTimersByTimeAsync(20000)

            // null means "no native status", not "disconnected" — calling
            // reconnect there would be meaningless.
            expect(mockReconnect).not.toHaveBeenCalled()

            vi.useRealTimers()
            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('does not reconnect when no enabled ports exist', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockImplementation((url: string) => {
                if (url === '/api/proxy/ports') {
                    return { ports: [{ port: 3000, localPort: 3000, host: '', name: 'A', protocol: 'http', active: false, enabled: false }] }
                }
                return { enabled: true, host: 'test', port: 22, username: 'u', fingerprint: 'f', command: 'c', connectionStats: null }
            })
            const mockReconnect = vi.fn().mockResolvedValue(true)
            ;(window as any).ClawBenchNative = {
                isTunnelConnected: async () => false,
                reconnectTunnelAsync: mockReconnect,
                getTunnelError: async () => '',
                getTunnelErrorType: async () => '',
            }

            vi.useFakeTimers()
            const { usePortForward } = await import('@/composables/usePortForward')
            const { checkTunnelHealth } = usePortForward()

            await checkTunnelHealth()
            await vi.advanceTimersByTimeAsync(20000)

            // Nothing enabled to maintain → do not churn the tunnel.
            expect(mockReconnect).not.toHaveBeenCalled()

            vi.useRealTimers()
            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })
    })
    describe('reverse direction (ssh -R)', () => {
        it('sends direction=reverse when registering a reverse mapping', async () => {
            mockApiPost.mockResolvedValue({ localPort: 9000 })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort } = usePortForward()

            const localPort = await registerPort(3000, 'svc', 'http', '', 'reverse')

            expect(mockApiPost).toHaveBeenCalledWith('/api/proxy/ports', {
                port: 3000, host: '', name: 'svc', protocol: 'http', direction: 'reverse',
            })
            expect(localPort).toBe(9000)
        })

        it('uses the reverse native channel in app mode', async () => {
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 9000 })
            const mockAddReverse = vi.fn().mockResolvedValue(true)
            const mockAddForward = vi.fn().mockResolvedValue(true)
            ;(window as any).ClawBenchNative = {
                addForwardedPort: mockAddForward,
                removeForwardedPort: vi.fn(),
                addReverseForwardedPort: mockAddReverse,
                removeReverseForwardedPort: vi.fn(),
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort } = usePortForward()

            await registerPort(3000, 'svc', 'http', '', 'reverse')

            expect(mockAddReverse).toHaveBeenCalledWith(9000, 3000, '')
            expect(mockAddForward).not.toHaveBeenCalled()

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('tears down a reverse mapping through the reverse native channel', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({
                ports: [
                    { port: 3000, localPort: 9000, host: '', name: 'svc', protocol: 'http', direction: 'reverse', active: true, enabled: true },
                ],
            })
            mockApiDelete.mockResolvedValue({})
            const mockRemoveReverse = vi.fn().mockResolvedValue(undefined)
            const mockRemoveForward = vi.fn().mockResolvedValue(undefined)
            ;(window as any).ClawBenchNative = {
                addForwardedPort: vi.fn(),
                removeForwardedPort: mockRemoveForward,
                addReverseForwardedPort: vi.fn(),
                removeReverseForwardedPort: mockRemoveReverse,
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { loadPorts, unregisterPort } = usePortForward()
            await loadPorts()

            await unregisterPort(9000)

            expect(mockRemoveReverse).toHaveBeenCalledWith(9000)
            expect(mockRemoveForward).not.toHaveBeenCalled()

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('does not probe reachability for reverse mappings', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({
                ports: [
                    { port: 3000, localPort: 9000, host: '', name: 'svc', protocol: 'http', direction: 'reverse', active: true, enabled: true },
                    { port: 8080, localPort: 8080, host: '', name: 'API', protocol: 'http', direction: 'forward', active: true, enabled: true },
                ],
            })
            const mockProbe = vi.fn().mockResolvedValue(true)
            ;(window as any).ClawBenchNative = {
                addForwardedPort: vi.fn(),
                removeForwardedPort: vi.fn(),
                testPortReachable: mockProbe,
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { loadPorts, localReachable } = usePortForward()
            await loadPorts()

            // Only the forward mapping has a local listener worth probing.
            expect(mockProbe).toHaveBeenCalledTimes(1)
            expect(mockProbe).toHaveBeenCalledWith(8080)
            expect(localReachable.value.has(9000)).toBe(false)

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('syncs reverse mappings via addReverseForwardedPort', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({
                ports: [
                    { port: 3000, localPort: 9000, host: '', name: 'svc', protocol: 'http', direction: 'reverse', active: false, enabled: true },
                ],
            })
            const mockAddReverse = vi.fn().mockResolvedValue(true)
            const mockAddForward = vi.fn().mockResolvedValue(true)
            ;(window as any).ClawBenchNative = {
                addForwardedPort: mockAddForward,
                removeForwardedPort: vi.fn(),
                addReverseForwardedPort: mockAddReverse,
                removeReverseForwardedPort: vi.fn(),
                getForwardedPorts: async () => JSON.stringify([]),
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { syncToNative } = usePortForward()
            await syncToNative()

            expect(mockAddReverse).toHaveBeenCalledWith(9000, 3000, '')
            expect(mockAddForward).not.toHaveBeenCalled()

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('skips reverse mappings when the host predates reverse support', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({
                ports: [
                    { port: 3000, localPort: 9000, host: '', name: 'svc', protocol: 'http', direction: 'reverse', active: false, enabled: true },
                ],
            })
            const mockAddForward = vi.fn().mockResolvedValue(true)
            ;(window as any).ClawBenchNative = {
                addForwardedPort: mockAddForward,
                removeForwardedPort: vi.fn(),
                getForwardedPorts: async () => JSON.stringify([]),
            }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { syncToNative } = usePortForward()

            // Must not throw, and must not fall back to the forward channel.
            await syncToNative()
            expect(mockAddForward).not.toHaveBeenCalled()

            delete (window as any).ClawBenchNative
            mockIsAppMode.value = false
        })

        it('no longer exposes copyServerAddress — the port item owns it', async () => {
            // The copy moved into ProxyPortItem, which renders the shared
            // CopyButton so the button itself can flash a check. The composable
            // only had a toast available (no button element to update), so it
            // was removed rather than left as a second, divergent implementation.
            const { usePortForward } = await import('@/composables/usePortForward')
            const api = usePortForward()
            expect((api as any).copyServerAddress).toBeUndefined()
        })
    })

})
