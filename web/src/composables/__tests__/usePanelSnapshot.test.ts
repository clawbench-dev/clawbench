import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref, reactive, nextTick } from 'vue'
import { usePanelSnapshot } from '@/composables/usePanelSnapshot'
import type { GroupPanelConfig } from '@/components/settings/settingsFieldMap'

// Mock useSettingsConfig.
const mockServerConfig = ref<Record<string, unknown>>({
  terminal: { enabled: true, idle_timeout: '10m', max_sessions: 10, buffer_lines: 2000 },
})
const mockLocalConfig = reactive<Record<string, unknown>>({
  terminalFontSize: 12,
})
const mockPatchConfig = vi.fn().mockResolvedValue({ needsRestart: false, changedColdFields: [], warnings: [] })
const mockSetLocalConfig = vi.fn()

vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    serverConfig: mockServerConfig,
    localConfig: mockLocalConfig,
    patchConfig: mockPatchConfig,
    setLocalConfig: mockSetLocalConfig,
    getServerValueWithDefault: (key: string) => {
      const parts = key.split('.')
      let current: unknown = mockServerConfig.value
      for (const p of parts) {
        if (current == null || typeof current !== 'object') return undefined
        current = (current as Record<string, unknown>)[p]
      }
      return current
    },
  }),
}))

vi.mock('@/i18n', () => ({
  default: { global: { t: (key: string) => key } },
}))

function createTerminalConfig(): GroupPanelConfig {
  return {
    panelId: 'terminal',
    enableKey: 'terminal.enabled',
    enableLabelKey: 'settings.items.terminalEnabled',
    commonFields: [
      { labelKey: 'settings.items.terminalFontSize', key: 'terminalFontSize', type: 'slider', source: 'local', min: 10, max: 24, step: 1, defaultValue: 12 },
      { labelKey: 'settings.items.terminalIdleTimeout', key: 'terminal.idle_timeout', type: 'text', source: 'server' },
    ],
  }
}

function createRequiredFieldsConfig(panelId: string, enableKey: string): GroupPanelConfig {
  return {
    panelId,
    enableKey,
    enableLabelKey: 'settings.items.someEnabled',
    commonFields: [
      { labelKey: 'settings.items.someAddr', key: `${panelId}.addr`, type: 'text', source: 'server' },
    ],
    requiredFields: [`${panelId}.addr`],
  }
}

describe('usePanelSnapshot', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPatchConfig.mockResolvedValue({ needsRestart: false, changedColdFields: [], warnings: [] })
  })

  it('initializes snapshot from server and local config', () => {
    const config = createTerminalConfig()
    const { localValues, snapshot, initSnapshot } = usePanelSnapshot(config)
    initSnapshot()

    expect(snapshot.value['terminal.enabled']).toBe(true)
    expect(snapshot.value['terminalFontSize']).toBe(12)
    expect(localValues['terminal.enabled']).toBe(true)
    expect(localValues['terminalFontSize']).toBe(12)
  })

  it('detects changes when localValues differ from snapshot', () => {
    const config = createTerminalConfig()
    const { localValues, hasChanges, initSnapshot } = usePanelSnapshot(config)
    initSnapshot()

    expect(hasChanges.value).toBe(false)

    localValues['terminalFontSize'] = 16
    expect(hasChanges.value).toBe(true)
  })

  it('canSave returns true when no required fields', () => {
    const config = createTerminalConfig()
    const { canSave, initSnapshot } = usePanelSnapshot(config)
    initSnapshot()

    expect(canSave.value).toBe(true)
  })

  it('canSave returns false when required field is empty and visible', () => {
    const config = createRequiredFieldsConfig('forward', 'forward.enabled')
    const { localValues, canSave, initSnapshot } = usePanelSnapshot(config)
    initSnapshot()

    // Enable the panel first so required-field validation is active.
    localValues['forward.enabled'] = true
    localValues['forward.addr'] = ''
    expect(canSave.value).toBe(false)
  })

  it('canSave returns true when panel is disabled even with empty required fields', () => {
    const config = createRequiredFieldsConfig('forward', 'forward.enabled')
    const { localValues, canSave, initSnapshot } = usePanelSnapshot(config)
    initSnapshot()

    // Disable the panel — should skip required-field validation.
    localValues['forward.enabled'] = false
    localValues['forward.addr'] = ''
    expect(canSave.value).toBe(true)
  })

  it('handleSave patches server changes and flushes local changes', async () => {
    const config = createTerminalConfig()
    const { localValues, hasChanges, initSnapshot, handleSave } = usePanelSnapshot(config)
    initSnapshot()

    localValues['terminalFontSize'] = 16
    localValues['terminal.idle_timeout'] = '5m'
    expect(hasChanges.value).toBe(true)

    const result = await handleSave()
    expect(result.needsRestart).toBe(false)
    expect(mockSetLocalConfig).toHaveBeenCalledWith('terminalFontSize', 16)
    expect(mockPatchConfig).toHaveBeenCalled()
    // After save, snapshot should be updated.
    expect(hasChanges.value).toBe(false)
  })

  it('handleSave sets serverError on failure', async () => {
    mockPatchConfig.mockRejectedValueOnce(new Error('Network error'))
    const config = createTerminalConfig()
    const { localValues, serverError, hasFailedSave, initSnapshot, handleSave } = usePanelSnapshot(config)
    initSnapshot()

    localValues['terminal.idle_timeout'] = '5m'
    await handleSave()

    expect(serverError.value).toBeTruthy()
    expect(hasFailedSave.value).toBe(true)
  })

  it('calls config.afterSave with changed keys', async () => {
    const afterSave = vi.fn()
    const config: GroupPanelConfig = {
      panelId: 'terminal',
      enableKey: 'terminal.enabled',
      enableLabelKey: 'settings.items.terminalEnabled',
      commonFields: [
        { labelKey: 'settings.items.terminalIdleTimeout', key: 'terminal.idle_timeout', type: 'text', source: 'server' },
      ],
      afterSave,
    }
    const { localValues, initSnapshot, handleSave } = usePanelSnapshot(config)
    initSnapshot()

    localValues['terminal.enabled'] = false
    await handleSave()

    expect(afterSave).toHaveBeenCalledWith(['terminal.enabled'], expect.any(Object))
  })

  it('calls config.onInit on snapshot init', () => {
    const onInit = vi.fn()
    const config: GroupPanelConfig = {
      panelId: 'forward',
      commonFields: [],
      onInit,
    }
    const { initSnapshot } = usePanelSnapshot(config)
    initSnapshot()

    expect(onInit).toHaveBeenCalled()
  })

  it('re-syncs snapshot when serverConfig changes externally', async () => {
    const config = createTerminalConfig()
    const { localValues, snapshot, initSnapshot } = usePanelSnapshot(config)
    initSnapshot()

    // Simulate external config change.
    mockServerConfig.value = {
      ...mockServerConfig.value,
      terminal: { enabled: false, idle_timeout: '10m', max_sessions: 10, buffer_lines: 2000 },
    }
    await nextTick()

    // Snapshot and localValues should be re-synced for unchanged fields.
    expect(snapshot.value['terminal.enabled']).toBe(false)
    expect(localValues['terminal.enabled']).toBe(false)
  })
})
