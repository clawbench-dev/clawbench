import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

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
  for (const id of pendingTimers) { clearTimeout(id) }
  pendingTimers.length = 0
  for (const id of pendingIntervals) { clearInterval(id) }
  pendingIntervals.length = 0
})

// ── Mocks ──

const mockApiGet = vi.fn()
const mockApiPost = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: (...args: any[]) => mockApiGet(...args),
  apiPost: (...args: any[]) => mockApiPost(...args),
}))

const mockOnEvent = vi.fn()
const mockConnected = { value: true }
vi.mock('@/composables/useGlobalEvents', () => ({
  useGlobalEvents: () => ({
    onEvent: (...args: any[]) => mockOnEvent(...args),
    connected: mockConnected,
  }),
}))

const mockServerConfig = { value: { version: 'v1.0.0' } }
const mockLoadConfig = vi.fn().mockResolvedValue(undefined)
vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    serverConfig: mockServerConfig,
    loadConfig: (...args: any[]) => mockLoadConfig(...args),
  }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: {
    d: vi.fn(),
    i: vi.fn(),
    w: vi.fn(),
    e: vi.fn(),
  },
}))

vi.mock('@/utils/version', () => ({
  compareVersions: (a: string, b: string) => {
    // Simple mock: strip v prefix and compare
    const parse = (v: string) => v.replace(/^v/, '').split('.').map(Number)
    const ap = parse(a)
    const bp = parse(b)
    for (let i = 0; i < Math.max(ap.length, bp.length); i++) {
      const an = ap[i] ?? 0
      const bn = bp[i] ?? 0
      if (an < bn) return -1
      if (an > bn) return 1
    }
    return 0
  },
}))

// ── Import after mocks ──

import { useUpgrade } from '@/composables/useUpgrade'

describe('useUpgrade', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockApiGet.mockReset()
    mockApiPost.mockReset()
    mockOnEvent.mockReset()
    mockConnected.value = true
    mockServerConfig.value = { version: 'v1.0.0' }
    mockLoadConfig.mockResolvedValue(undefined)

    // Reset localStorage
    localStorage.clear()
  })

  // ── checkUpgrade ──

  describe('checkUpgrade', () => {
    it('sets checking to true during check and false after', async () => {
      mockApiGet.mockResolvedValue({
        current_version: 'v1.0.0',
        latest_version: 'v1.0.1',
        has_upgrade: true,
      })

      const upgrade = useUpgrade()
      expect(upgrade.checking.value).toBe(false)

      const promise = upgrade.checkUpgrade()
      expect(upgrade.checking.value).toBe(true)

      await promise
      expect(upgrade.checking.value).toBe(false)
    })

    it('updates state and hasUpgrade when upgrade is available', async () => {
      mockApiGet.mockResolvedValue({
        current_version: 'v1.0.0',
        latest_version: 'v1.1.0',
        has_upgrade: true,
      })

      const upgrade = useUpgrade()
      await upgrade.checkUpgrade()

      expect(upgrade.state.current_version).toBe('v1.0.0')
      expect(upgrade.state.latest_version).toBe('v1.1.0')
      expect(upgrade.hasUpgrade.value).toBe(true)
    })

    it('sets hasUpgrade to false when no upgrade is available', async () => {
      mockApiGet.mockResolvedValue({
        current_version: 'v1.0.0',
        latest_version: 'v1.0.0',
        has_upgrade: false,
      })

      const upgrade = useUpgrade()
      await upgrade.checkUpgrade()

      expect(upgrade.hasUpgrade.value).toBe(false)
    })

    it('sets hasUpgrade to false on API error', async () => {
      mockApiGet.mockRejectedValue(new Error('Network error'))

      const upgrade = useUpgrade()
      await upgrade.checkUpgrade()

      expect(upgrade.hasUpgrade.value).toBe(false)
      expect(upgrade.checking.value).toBe(false)
    })
  })

  // ── startUpgrade ──

  describe('startUpgrade', () => {
    it('shows progress dialog and calls API', async () => {
      mockApiPost.mockResolvedValue({})

      const upgrade = useUpgrade()
      await upgrade.startUpgrade()

      expect(upgrade.showProgressDialog.value).toBe(true)
      expect(mockApiPost).toHaveBeenCalledWith('/api/upgrade/start', {})
    })

    it('still shows progress dialog even when API fails', async () => {
      mockApiPost.mockRejectedValue(new Error('Start failed'))

      const upgrade = useUpgrade()
      await upgrade.startUpgrade()

      expect(upgrade.showProgressDialog.value).toBe(true)
    })
  })

  // ── clearShowProgressDialog ──

  describe('clearShowProgressDialog', () => {
    it('clears the show progress dialog flag', async () => {
      mockApiPost.mockResolvedValue({})
      const upgrade = useUpgrade()
      await upgrade.startUpgrade()
      expect(upgrade.showProgressDialog.value).toBe(true)

      upgrade.clearShowProgressDialog()
      expect(upgrade.showProgressDialog.value).toBe(false)
    })
  })

  // ── fetchStatus ──

  describe('fetchStatus', () => {
    it('updates state from status API', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'downloading',
        current_version: 'v1.0.0',
        latest_version: 'v1.1.0',
        progress: 50,
        message: 'Downloading...',
        backup_path: '/tmp/backup',
        error: '',
      })

      const upgrade = useUpgrade()
      await upgrade.fetchStatus()

      expect(upgrade.state.phase).toBe('downloading')
      expect(upgrade.state.progress).toBe(50)
      expect(upgrade.state.message).toBe('Downloading...')
      expect(upgrade.state.backup_path).toBe('/tmp/backup')
    })

    it('does not throw on API error', async () => {
      mockApiGet.mockRejectedValue(new Error('Server restarting'))

      const upgrade = useUpgrade()
      await expect(upgrade.fetchStatus()).resolves.toBeUndefined()
    })
  })

  // ── checkForUpgradePrompt ──

  describe('checkForUpgradePrompt', () => {
    it('returns latest version when upgrade is available and not skipped', async () => {
      mockApiGet.mockResolvedValue({
        current_version: 'v1.0.0',
        latest_version: 'v1.1.0',
        has_upgrade: true,
      })

      const upgrade = useUpgrade()
      const result = await upgrade.checkForUpgradePrompt()

      expect(result).toBe('v1.1.0')
    })

    it('returns null when no upgrade is available', async () => {
      mockApiGet.mockResolvedValue({
        current_version: 'v1.0.0',
        latest_version: 'v1.0.0',
        has_upgrade: false,
      })

      const upgrade = useUpgrade()
      const result = await upgrade.checkForUpgradePrompt()

      expect(result).toBeNull()
    })

    it('returns null when upgrade version was skipped', async () => {
      mockApiGet.mockResolvedValue({
        current_version: 'v1.0.0',
        latest_version: 'v1.1.0',
        has_upgrade: true,
      })
      localStorage.setItem('clawbench-upgrade-skip', 'v1.1.0')

      const upgrade = useUpgrade()
      const result = await upgrade.checkForUpgradePrompt()

      expect(result).toBeNull()
    })

    it('returns latest version when a different version was skipped', async () => {
      mockApiGet.mockResolvedValue({
        current_version: 'v1.0.0',
        latest_version: 'v1.2.0',
        has_upgrade: true,
      })
      localStorage.setItem('clawbench-upgrade-skip', 'v1.1.0')

      const upgrade = useUpgrade()
      const result = await upgrade.checkForUpgradePrompt()

      expect(result).toBe('v1.2.0')
    })

    it('returns null on error', async () => {
      mockApiGet.mockRejectedValue(new Error('Network error'))

      const upgrade = useUpgrade()
      const result = await upgrade.checkForUpgradePrompt()

      expect(result).toBeNull()
    })
  })

  // ── skipVersion ──

  describe('skipVersion', () => {
    it('stores skipped version in localStorage', () => {
      const upgrade = useUpgrade()
      upgrade.skipVersion('v1.1.0')

      expect(localStorage.getItem('clawbench-upgrade-skip')).toBe('v1.1.0')
    })
  })

  // ── computed properties ──

  describe('computed properties', () => {
    it('isInProgress is true for downloading phase', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'downloading',
        current_version: '',
        latest_version: '',
        progress: 30,
        message: '',
        backup_path: '',
        error: '',
      })

      const upgrade = useUpgrade()
      await upgrade.fetchStatus()

      expect(upgrade.isInProgress.value).toBe(true)
    })

    it('isInProgress is false for completed phase', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'completed',
        current_version: '',
        latest_version: '',
        progress: 100,
        message: '',
        backup_path: '',
        error: '',
      })

      const upgrade = useUpgrade()
      await upgrade.fetchStatus()

      expect(upgrade.isInProgress.value).toBe(false)
    })

    it('isInProgress is false for failed phase', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'failed',
        current_version: '',
        latest_version: '',
        progress: 0,
        message: '',
        backup_path: '',
        error: 'something went wrong',
      })

      const upgrade = useUpgrade()
      await upgrade.fetchStatus()

      expect(upgrade.isInProgress.value).toBe(false)
    })

    it('isInProgress is false for empty phase', () => {
      const upgrade = useUpgrade()
      expect(upgrade.isInProgress.value).toBe(false)
    })

    it('isRestarting is true for restarting phase', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'restarting',
        current_version: '',
        latest_version: '',
        progress: 80,
        message: '',
        backup_path: '',
        error: '',
      })

      const upgrade = useUpgrade()
      await upgrade.fetchStatus()

      expect(upgrade.isRestarting.value).toBe(true)
    })

    it('isCompleted is true for completed phase', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'completed',
        current_version: '',
        latest_version: '',
        progress: 100,
        message: '',
        backup_path: '',
        error: '',
      })

      const upgrade = useUpgrade()
      await upgrade.fetchStatus()

      expect(upgrade.isCompleted.value).toBe(true)
    })

    it('isFailed is true for failed phase', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'failed',
        current_version: '',
        latest_version: '',
        progress: 0,
        message: '',
        backup_path: '',
        error: 'timeout',
      })

      const upgrade = useUpgrade()
      await upgrade.fetchStatus()

      expect(upgrade.isFailed.value).toBe(true)
    })
  })

  // ── releaseNotesUrl ──

  describe('releaseNotesUrl', () => {
    it('builds a v-prefixed tag URL from the latest version', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'completed',
        current_version: '',
        latest_version: '1.2.0',
        progress: 100,
        message: '',
        backup_path: '',
        error: '',
      })

      const upgrade = useUpgrade()
      await upgrade.fetchStatus()

      expect(upgrade.releaseNotesUrl.value).toBe(
        'https://github.com/xulongzhe/clawbench/releases/tag/v1.2.0',
      )
    })

    it('does not double the v prefix when latest already has one', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'completed',
        current_version: '',
        latest_version: 'v1.2.0',
        progress: 100,
        message: '',
        backup_path: '',
        error: '',
      })

      const upgrade = useUpgrade()
      await upgrade.fetchStatus()

      expect(upgrade.releaseNotesUrl.value).toBe(
        'https://github.com/xulongzhe/clawbench/releases/tag/v1.2.0',
      )
    })

    it('returns empty when latest version is empty', () => {
      const upgrade = useUpgrade()
      upgrade.state.latest_version = ''
      expect(upgrade.releaseNotesUrl.value).toBe('')
    })
  })

  // ── verifyUpgrade ──

  describe('verifyUpgrade', () => {
    it('returns true when current version >= latest version', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'completed',
        current_version: 'v1.1.0',
        latest_version: 'v1.1.0',
        progress: 100,
        message: '',
        backup_path: '',
        error: '',
      })
      mockServerConfig.value = { version: 'v1.1.0' }

      const upgrade = useUpgrade()
      upgrade.state.latest_version = 'v1.1.0'
      const result = await upgrade.verifyUpgrade()

      expect(result).toBe(true)
    })

    it('returns true when current version > latest version', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'completed',
        current_version: 'v1.2.0',
        latest_version: 'v1.1.0',
        progress: 100,
        message: '',
        backup_path: '',
        error: '',
      })
      mockServerConfig.value = { version: 'v1.2.0' }

      const upgrade = useUpgrade()
      upgrade.state.latest_version = 'v1.1.0'
      const result = await upgrade.verifyUpgrade()

      expect(result).toBe(true)
    })

    it('returns false when current version < latest version', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'completed',
        current_version: 'v1.0.0',
        latest_version: 'v1.1.0',
        progress: 100,
        message: '',
        backup_path: '',
        error: '',
      })
      mockServerConfig.value = { version: 'v1.0.0' }

      const upgrade = useUpgrade()
      upgrade.state.latest_version = 'v1.1.0'
      const result = await upgrade.verifyUpgrade()

      expect(result).toBe(false)
    })

    it('returns false when server config version is empty', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'completed',
        current_version: 'v1.1.0',
        latest_version: 'v1.1.0',
        progress: 100,
        message: '',
        backup_path: '',
        error: '',
      })
      mockServerConfig.value = { version: '' }

      const upgrade = useUpgrade()
      upgrade.state.latest_version = 'v1.1.0'
      const result = await upgrade.verifyUpgrade()

      expect(result).toBe(false)
    })

    it('returns false when latest version is empty', async () => {
      mockApiGet.mockResolvedValue({
        phase: 'completed',
        current_version: '',
        latest_version: '',
        progress: 100,
        message: '',
        backup_path: '',
        error: '',
      })
      mockServerConfig.value = { version: 'v1.0.0' }

      const upgrade = useUpgrade()
      upgrade.state.latest_version = ''
      const result = await upgrade.verifyUpgrade()

      expect(result).toBe(false)
    })

    it('returns false on fetchStatus error', async () => {
      mockApiGet.mockRejectedValue(new Error('Server not ready'))

      const upgrade = useUpgrade()
      upgrade.state.latest_version = 'v1.1.0'
      const result = await upgrade.verifyUpgrade()

      expect(result).toBe(false)
    })
  })

  // ── WS event listener ──

  describe('WS event listener', () => {
    it('registers a WS event listener on first call', () => {
      mockOnEvent.mockReturnValue(vi.fn()) // unsubscribe function

      const upgrade = useUpgrade()

      expect(mockOnEvent).toHaveBeenCalled()
    })

    it('does not register duplicate listeners on subsequent calls', () => {
      mockOnEvent.mockReturnValue(vi.fn())
      const callCountBefore = mockOnEvent.mock.calls.length

      useUpgrade()
      useUpgrade()

      // Should not have registered additional listeners
      // (first call in this test is from the import/module init)
      expect(mockOnEvent.mock.calls.length).toBeLessThanOrEqual(callCountBefore + 1)
    })
  })
})
