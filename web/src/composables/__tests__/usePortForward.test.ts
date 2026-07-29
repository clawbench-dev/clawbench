import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'

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

const mockIsAppMode = ref(false)
vi.mock('@/composables/useAppMode', () => ({
    useAppMode: () => ({ isAppMode: mockIsAppMode }),
}))

vi.mock('@/composables/useLocale', () => ({
    gt: (key: string) => key,
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
    useToast: () => ({ show: mockToastShow, dismiss: vi.fn(), visible: ref(false), message: ref(''), icon: ref(''), type: ref('success'), onClick: ref(null) }),
}))

vi.mock('@/composables/useSessionIdentity', () => ({
    useSessionIdentity: () => ({ currentSessionId: ref('test-session-id') }),
}))

vi.mock('@/utils/portForwardUtils', () => ({
    tunnelStatusFromPorts: () => 'ok',
    buildPortUrl: (port: number, protocol?: string, path?: string) => {
        const scheme = protocol || 'http'
        const urlPath = path || '/'
        if ((scheme === 'http' && port === 80) || (scheme === 'https' && port === 443)) {
            return `${scheme}://localhost${urlPath}`
        }
        return `${scheme}://localhost:${port}${urlPath}`
    },
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
        delete (window as any).AndroidNative
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

            expect(mockApiGet).toHaveBeenCalledWith('/api/ssh/info')
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
                if (url === '/api/ssh/info') return { enabled: false, host: '', port: 0, username: '', fingerprint: '', command: '', connectionStats: null }
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
                if (url === '/api/ssh/info') return {
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
                if (url === '/api/ssh/info') return {
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
                if (url === '/api/proxy/ports') return { ports: [{ port: 3000, name: 'App', protocol: 'http', active: true }] }
                if (url === '/api/ssh/info') return {
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
                if (url === '/api/ssh/info') return { enabled: false, host: '', port: 0, username: '', fingerprint: '', command: '', connectionStats: null }
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
            const mockOpenInSandbox = vi.fn()
            ;(window as any).AndroidNative = { openInSandbox: mockOpenInSandbox }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPort } = usePortForward()

            openPort(3000, 'http')

            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '', '', 'test-session-id')

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })

        it('opens WebView immediately even when testPortReachable returns false', async () => {
            mockIsAppMode.value = true
            const mockOpenInSandbox = vi.fn()
            const mockTestPortReachable = vi.fn().mockReturnValue(false)
            ;(window as any).AndroidNative = { openInSandbox: mockOpenInSandbox, testPortReachable: mockTestPortReachable }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPort } = usePortForward()

            openPort(3000, 'http')

            // Should NOT call testPortReachable — just open WebView directly
            expect(mockTestPortReachable).not.toHaveBeenCalled()
            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '', '', 'test-session-id')

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })

        it('passes host parameter to native sandbox browser', async () => {
            mockIsAppMode.value = true
            const mockOpenInSandbox = vi.fn()
            ;(window as any).AndroidNative = { openInSandbox: mockOpenInSandbox }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPort } = usePortForward()

            openPort(3000, 'http', '192.168.1.1')

            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '192.168.1.1', '', 'test-session-id')

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })

        it('falls back to openInBrowser when sandbox not available', async () => {
            mockIsAppMode.value = true
            const mockOpenInBrowser = vi.fn()
            ;(window as any).AndroidNative = { openInBrowser: mockOpenInBrowser }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPort } = usePortForward()

            openPort(3000, 'https')

            expect(mockOpenInBrowser).toHaveBeenCalledWith(3000, 'https', '', '')

            delete (window as any).AndroidNative
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
            const mockOpenInSandbox = vi.fn()
            const mockTestPortReachable = vi.fn().mockReturnValue(true)
            ;(window as any).AndroidNative = { openInSandbox: mockOpenInSandbox, testPortReachable: mockTestPortReachable }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck } = usePortForward()

            await openPortWithCheck(3000, 'http')

            expect(mockTestPortReachable).toHaveBeenCalledWith(3000)
            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '', '', 'test-session-id')

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })

        it('opens directly in connecting state without testing reachability', async () => {
            mockIsAppMode.value = true
            const mockOpenInSandbox = vi.fn()
            const mockTestPortReachable = vi.fn().mockReturnValue(true)
            ;(window as any).AndroidNative = { openInSandbox: mockOpenInSandbox, testPortReachable: mockTestPortReachable }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck, connectingPorts } = usePortForward()

            connectingPorts.value.add(3000)
            connectingPorts.value = new Set(connectingPorts.value)

            await openPortWithCheck(3000, 'http')

            expect(mockTestPortReachable).not.toHaveBeenCalled()
            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '', '', 'test-session-id')

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })

        it('reconnects and opens when port unreachable then reachable after reconnect', async () => {
            mockIsAppMode.value = true
            const mockOpenInSandbox = vi.fn()
            const mockTestPortReachable = vi.fn()
                .mockReturnValueOnce(false)  // initial check
                .mockReturnValueOnce(true)   // after reconnect
            const mockReconnectTunnel = vi.fn().mockReturnValue(true)
            ;(window as any).AndroidNative = { openInSandbox: mockOpenInSandbox, testPortReachable: mockTestPortReachable, reconnectTunnel: mockReconnectTunnel }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck } = usePortForward()

            await openPortWithCheck(3000, 'http')

            expect(mockReconnectTunnel).toHaveBeenCalled()
            expect(mockToastShow).toHaveBeenCalledWith('portForward.tunnelReconnected', expect.objectContaining({ type: 'success' }))
            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '', '', 'test-session-id')

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })

        it('shows error toast when port unreachable after reconnect', async () => {
            mockIsAppMode.value = true
            const mockTestPortReachable = vi.fn().mockReturnValue(false)
            const mockReconnectTunnel = vi.fn().mockReturnValue(true)
            ;(window as any).AndroidNative = { testPortReachable: mockTestPortReachable, reconnectTunnel: mockReconnectTunnel }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck } = usePortForward()

            await openPortWithCheck(3000, 'http')

            expect(mockToastShow).toHaveBeenCalledWith('portForward.portUnreachable', expect.objectContaining({ type: 'error' }))

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })

        it('shows error toast when reconnect fails', async () => {
            mockIsAppMode.value = true
            const mockTestPortReachable = vi.fn().mockReturnValue(false)
            const mockReconnectTunnel = vi.fn().mockReturnValue(false)
            ;(window as any).AndroidNative = { testPortReachable: mockTestPortReachable, reconnectTunnel: mockReconnectTunnel }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck } = usePortForward()

            await openPortWithCheck(3000, 'http')

            expect(mockToastShow).toHaveBeenCalledWith('portForward.portUnreachable', expect.objectContaining({ type: 'error' }))

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })

        it('falls back to direct open when testPortReachable not available (old APK)', async () => {
            mockIsAppMode.value = true
            const mockOpenInSandbox = vi.fn()
            ;(window as any).AndroidNative = { openInSandbox: mockOpenInSandbox }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openPortWithCheck } = usePortForward()

            await openPortWithCheck(3000, 'http')

            expect(mockOpenInSandbox).toHaveBeenCalledWith(3000, 'http', '', '', 'test-session-id')

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })
    })

    describe('reconnectPort', () => {
        it('shows success toast and refreshes when port is already reachable', async () => {
            mockIsAppMode.value = true
            const mockTestPortReachable = vi.fn().mockReturnValue(true)
            mockApiGet.mockResolvedValue({ ports: [] })
            ;(window as any).AndroidNative = { testPortReachable: mockTestPortReachable }

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
                .mockReturnValueOnce(false)  // initial check
                .mockReturnValueOnce(true)   // after reconnect
            const mockReconnectTunnel = vi.fn().mockReturnValue(true)
            mockApiGet.mockResolvedValue({ ports: [] })
            ;(window as any).AndroidNative = { testPortReachable: mockTestPortReachable, reconnectTunnel: mockReconnectTunnel }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { reconnectPort } = usePortForward()

            await reconnectPort(3000)

            expect(mockReconnectTunnel).toHaveBeenCalled()
            expect(mockToastShow).toHaveBeenCalledWith('portForward.tunnelReconnected', expect.objectContaining({ type: 'success' }))
        })

        it('shows error toast when port still unreachable after reconnect', async () => {
            mockIsAppMode.value = true
            const mockTestPortReachable = vi.fn().mockReturnValue(false)
            const mockReconnectTunnel = vi.fn().mockReturnValue(true)
            mockApiGet.mockResolvedValue({ ports: [] })
            ;(window as any).AndroidNative = { testPortReachable: mockTestPortReachable, reconnectTunnel: mockReconnectTunnel }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { reconnectPort } = usePortForward()

            await reconnectPort(3000)

            expect(mockToastShow).toHaveBeenCalledWith('portForward.portUnreachable', expect.objectContaining({ type: 'error' }))
        })

        it('shows error toast when reconnect fails', async () => {
            mockIsAppMode.value = true
            const mockTestPortReachable = vi.fn().mockReturnValue(false)
            const mockReconnectTunnel = vi.fn().mockReturnValue(false)
            mockApiGet.mockResolvedValue({ ports: [] })
            ;(window as any).AndroidNative = { testPortReachable: mockTestPortReachable, reconnectTunnel: mockReconnectTunnel }

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
            const mockOpenInBrowser = vi.fn()
            ;(window as any).AndroidNative = { openInBrowser: mockOpenInBrowser }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openInExternalBrowser } = usePortForward()

            openInExternalBrowser(3000, 'https')

            expect(mockOpenInBrowser).toHaveBeenCalledWith(3000, 'https', '', '')

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })

        it('passes host parameter to native openInBrowser', async () => {
            mockIsAppMode.value = true
            const mockOpenInBrowser = vi.fn()
            ;(window as any).AndroidNative = { openInBrowser: mockOpenInBrowser }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { openInExternalBrowser } = usePortForward()

            openInExternalBrowser(3000, 'https', '192.168.1.1')

            expect(mockOpenInBrowser).toHaveBeenCalledWith(3000, 'https', '192.168.1.1', '')

            delete (window as any).AndroidNative
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
                port: 3000, host: '', name: 'App', protocol: 'http',
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
            const mockAddForwardedPort = vi.fn()
            ;(window as any).AndroidNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort } = usePortForward()

            const result = await registerPort(3000, 'App', 'http', '192.168.1.1')

            expect(mockApiPost).toHaveBeenCalledWith('/api/proxy/ports', {
                port: 3000, host: '192.168.1.1', name: 'App', protocol: 'http',
            })
            expect(mockAddForwardedPort).toHaveBeenCalledWith(3000, 3000, '192.168.1.1')
            expect(result).toBe(3000)

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })

        it('defaults host to empty string when not provided', async () => {
            mockApiPost.mockResolvedValue({})
            mockApiGet.mockResolvedValue({ ports: [] })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort } = usePortForward()

            const result = await registerPort(3000)

            expect(mockApiPost).toHaveBeenCalledWith('/api/proxy/ports', {
                port: 3000, host: '', name: '', protocol: 'http',
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
                localPort: 3000, port: 3000, host: '192.168.1.1', name: 'App', protocol: 'http',
            })
        })

        it('syncs native layer after update in app mode', async () => {
            mockIsAppMode.value = true
            mockApiPut.mockResolvedValue({})
            mockApiGet.mockResolvedValue({ ports: [] })
            const mockRemove = vi.fn()
            const mockAdd = vi.fn()
            ;(window as any).AndroidNative = { removeForwardedPort: mockRemove, addForwardedPort: mockAdd }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { updatePort } = usePortForward()

            await updatePort(3000, 4000, '10.0.0.1', 'NewApp', 'https')

            expect(mockRemove).toHaveBeenCalledWith(3000)
            expect(mockAdd).toHaveBeenCalledWith(3000, 4000, '10.0.0.1')

            delete (window as any).AndroidNative
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
            const { detectPorts, detectedPorts } = usePortForward()

            await detectPorts()

            expect(mockApiGet).toHaveBeenCalledWith('/api/proxy/detect')
            expect(detectedPorts.value).toHaveLength(1)
        })
    })

    describe('ensurePortRegistered', () => {
        it('returns existing localPort if port already exists', async () => {
            mockApiGet.mockResolvedValue({
                ports: [{ port: 3000, name: 'App', protocol: 'http', active: true, localPort: 3000, host: '' }],
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

        it('registers and returns localPort if port not yet registered', async () => {
            mockApiGet.mockResolvedValue({ ports: [] })
            mockApiPost.mockResolvedValue({ localPort: 5173 })

            const { usePortForward } = await import('@/composables/usePortForward')
            const { ensurePortRegistered } = usePortForward()

            const result = await ensurePortRegistered(5173, 'http')

            expect(mockApiPost).toHaveBeenCalledWith('/api/proxy/ports', {
                port: 5173, host: '', name: '', protocol: 'http',
            })
            expect(result).toBe(5173)
        })

        it('matches both port and host when finding existing entry', async () => {
            mockApiGet.mockResolvedValue({
                ports: [
                    { port: 3000, localPort: 3000, host: '', name: 'Local', protocol: 'http', active: true },
                    { port: 3000, localPort: 3001, host: '192.168.1.1', name: 'Remote', protocol: 'http', active: true },
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
        it('stops native service when no ports are registered', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({ ports: [] })
            const mockStop = vi.fn()
            ;(window as any).AndroidNative = { stopBackgroundService: mockStop }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { syncToNative } = usePortForward()

            await syncToNative()

            expect(mockStop).toHaveBeenCalled()

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })

        it('registers each port with host to native layer', async () => {
            mockIsAppMode.value = true
            mockApiGet.mockResolvedValue({
                ports: [
                    { port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: true },
                    { port: 8080, localPort: 8080, host: '192.168.1.1', name: 'API', protocol: 'http', active: true },
                ],
            })
            const mockAdd = vi.fn()
            ;(window as any).AndroidNative = { addForwardedPort: mockAdd }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { syncToNative } = usePortForward()

            await syncToNative()

            expect(mockAdd).toHaveBeenCalledWith(3000, 3000, '')
            expect(mockAdd).toHaveBeenCalledWith(8080, 8080, '192.168.1.1')

            delete (window as any).AndroidNative
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
            const mockAddForwardedPort = vi.fn()
            ;(window as any).AndroidNative = { addForwardedPort: mockAddForwardedPort }

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

            delete (window as any).AndroidNative
            mockIsAppMode.value = false
        })

        it('removes port from connectingPorts on native callback failure', async () => {
            mockIsAppMode.value = true
            mockApiPost.mockResolvedValue({ localPort: 3000 })
            mockApiGet.mockResolvedValue({ ports: [{ port: 3000, localPort: 3000, host: '', name: 'App', protocol: 'http', active: false }] })
            const mockAddForwardedPort = vi.fn()
            ;(window as any).AndroidNative = { addForwardedPort: mockAddForwardedPort }

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

            delete (window as any).AndroidNative
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

            const mockAddForwardedPort = vi.fn()
            ;(window as any).AndroidNative = { addForwardedPort: mockAddForwardedPort }

            const { usePortForward } = await import('@/composables/usePortForward')
            const { registerPort, connectingPorts, loadPorts } = usePortForward()

            await registerPort(3000, 'App', 'http')
            expect(connectingPorts.value.has(3000)).toBe(true)

            // loadPorts with active=true should clear connectingPorts even in app mode
            await loadPorts(true)

            expect(connectingPorts.value.has(3000)).toBe(false)

            delete (window as any).AndroidNative
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
})
