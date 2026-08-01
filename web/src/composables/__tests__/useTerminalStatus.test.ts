import { describe, expect, it, vi, beforeEach } from 'vitest'
import { nextTick } from 'vue'

// Mock apiGet before importing the composable
const mockApiGet = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: (...args: any[]) => mockApiGet(...args),
}))

import { useTerminalStatus } from '../useTerminalStatus'

describe('useTerminalStatus', () => {
  beforeEach(() => {
    mockApiGet.mockReset()
    // Reset the module-level ref by re-importing isn't easy,
    // so we call loadTerminalStatus to set it
  })

  it('sets terminalRuntimeEnabled to true when API returns enabled', async () => {
    mockApiGet.mockResolvedValue({ enabled: true })
    const { terminalRuntimeEnabled, loadTerminalStatus } = useTerminalStatus()
    await loadTerminalStatus()
    expect(terminalRuntimeEnabled.value).toBe(true)
  })

  it('sets terminalRuntimeEnabled to false when API returns enabled: false', async () => {
    mockApiGet.mockResolvedValue({ enabled: false })
    const { terminalRuntimeEnabled, loadTerminalStatus } = useTerminalStatus()
    await loadTerminalStatus()
    expect(terminalRuntimeEnabled.value).toBe(false)
  })

  it('sets terminalRuntimeEnabled to false when API throws', async () => {
    mockApiGet.mockRejectedValue(new Error('network error'))
    const { terminalRuntimeEnabled, loadTerminalStatus } = useTerminalStatus()
    await loadTerminalStatus()
    expect(terminalRuntimeEnabled.value).toBe(false)
  })

  it('defaults to false when API returns missing enabled field', async () => {
    mockApiGet.mockResolvedValue({})
    const { terminalRuntimeEnabled, loadTerminalStatus } = useTerminalStatus()
    await loadTerminalStatus()
    expect(terminalRuntimeEnabled.value).toBe(false)
  })

  it('shares module-level state across multiple callers', async () => {
    mockApiGet.mockResolvedValue({ enabled: true })
    const { terminalRuntimeEnabled: ref1, loadTerminalStatus } = useTerminalStatus()
    await loadTerminalStatus()
    const { terminalRuntimeEnabled: ref2 } = useTerminalStatus()
    expect(ref1.value).toBe(true)
    expect(ref2.value).toBe(true)
  })

  it('sets platformSupported to true when API returns platform_supported: true', async () => {
    mockApiGet.mockResolvedValue({ enabled: true, platform_supported: true })
    const { platformSupported, loadTerminalStatus } = useTerminalStatus()
    await loadTerminalStatus()
    expect(platformSupported.value).toBe(true)
  })

  it('sets platformSupported to false when API returns platform_supported: false', async () => {
    mockApiGet.mockResolvedValue({ enabled: true, platform_supported: false })
    const { platformSupported, loadTerminalStatus } = useTerminalStatus()
    await loadTerminalStatus()
    expect(platformSupported.value).toBe(false)
  })

  it('defaults platformSupported to true when API omits field', async () => {
    mockApiGet.mockResolvedValue({ enabled: true })
    const { platformSupported, loadTerminalStatus } = useTerminalStatus()
    await loadTerminalStatus()
    expect(platformSupported.value).toBe(true)
  })

  it('sets platformSupported to false when API throws', async () => {
    mockApiGet.mockRejectedValue(new Error('network error'))
    const { platformSupported, loadTerminalStatus } = useTerminalStatus()
    await loadTerminalStatus()
    expect(platformSupported.value).toBe(false)
  })
})
