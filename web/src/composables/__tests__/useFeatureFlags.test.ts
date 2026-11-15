/**
 * The feature switches are the only thing standing between a disabled server
 * feature and a button that can only fail.
 *
 * The six master switches answer from the server's plugin registry
 * (`GET /api/features`, pushed in by `loadConfig`), NOT from `/api/config` — the
 * frontend keeps no default-value mirror. The per-kind share permissions are
 * sub-options of the share feature and still come from the config snapshot.
 *
 * These tests pin the states the reader has to distinguish: an explicit `false`
 * (off), a missing row/key (must stay ON), and a malformed value (must fall back
 * to ON rather than silently disabling a feature).
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

const toastShow = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: toastShow }),
}))

const {
  isFeatureEnabled,
  serverFeature,
  syncFeatureConfig,
  syncServerFeatures,
  useFeatureFlag,
  useFeatureFlags,
  useShareKindAllowed,
  notifyFeatureDisabled,
} = await import('@/composables/useFeatureFlags')

const { DOCK_FEATURES } = await import('@/composables/dockTabs')

/** Build one `/api/features` row. */
function feature(id: string, enabled: boolean, available = true) {
  return {
    id,
    enabled,
    available,
    reason: !enabled ? ('disabled' as const) : !available ? ('capability' as const) : ('' as const),
    defaultOn: true,
  }
}

beforeEach(() => {
  syncFeatureConfig({})
  syncServerFeatures([])
  toastShow.mockClear()
})

describe('isFeatureEnabled', () => {
  it('treats every master switch as ON before the registry lands', () => {
    for (const key of DOCK_FEATURES) expect(isFeatureEnabled(key)).toBe(true)
  })

  it('reads an explicit false as off', () => {
    syncServerFeatures([feature('share', false), feature('tasks', false)])
    expect(isFeatureEnabled('share.enabled')).toBe(false)
    expect(isFeatureEnabled('tasks.enabled')).toBe(false)
    // Features the payload does not mention keep the "missing means on" rule.
    expect(isFeatureEnabled('mail.enabled')).toBe(true)
  })

  it('ignores the config snapshot for a master switch', () => {
    // The registry is the authority; a stale /api/config must not resurrect a
    // feature the server has switched off (or hide one it has switched on).
    syncFeatureConfig({ share: { enabled: true } })
    syncServerFeatures([feature('share', false)])
    expect(isFeatureEnabled('share.enabled')).toBe(false)
  })

  it('keeps sub-options on the config snapshot, defaulting to ON', () => {
    syncServerFeatures([feature('share', true)])
    expect(isFeatureEnabled('share.allow_files')).toBe(true)

    syncFeatureConfig({ share: { allow_files: false } })
    expect(isFeatureEnabled('share.allow_files')).toBe(false)
  })

  it('falls back to ON for a malformed sub-option value', () => {
    syncFeatureConfig({ share: { allow_download: 'no' } })
    expect(isFeatureEnabled('share.allow_download')).toBe(true)
  })

  it('survives a null config (server returned nothing usable)', () => {
    syncFeatureConfig(null)
    expect(isFeatureEnabled('share.allow_files')).toBe(true)
    expect(isFeatureEnabled('share.enabled')).toBe(true)
  })

  it('exposes the registry row for availability/reason consumers', () => {
    syncServerFeatures([feature('terminal', true, false)])
    expect(serverFeature('terminal')).toMatchObject({ enabled: true, available: false, reason: 'capability' })
    expect(serverFeature('nope')).toBeUndefined()
  })
})

describe('useFeatureFlag', () => {
  it('re-evaluates when the registry snapshot is replaced', async () => {
    const shareEnabled = useFeatureFlag('share.enabled')
    expect(shareEnabled.value).toBe(true)

    syncServerFeatures([feature('share', false)])
    await nextTick()
    expect(shareEnabled.value).toBe(false)

    syncServerFeatures([feature('share', true)])
    await nextTick()
    expect(shareEnabled.value).toBe(true)
  })

  it('exposes the download permission (default ON, independent of the kind flags)', () => {
    expect(useFeatureFlags().shareAllowDownload.value).toBe(true)

    syncFeatureConfig({ share: { enabled: true, allow_download: false } })
    const flags = useFeatureFlags()
    expect(flags.shareAllowDownload.value).toBe(false)
    // Downloading is not a way of creating a link, so the kind permissions and
    // the master switch stay untouched by it.
    expect(flags.shareAllowFiles.value).toBe(true)
    expect(flags.shareEnabled.value).toBe(true)
  })

  it('exposes every switch from useFeatureFlags', () => {
    syncServerFeatures([feature('forge', false)])
    const flags = useFeatureFlags()
    expect(flags.forgeEnabled.value).toBe(false)
    expect(flags.mailEnabled.value).toBe(true)
    expect(flags.tasksEnabled.value).toBe(true)
    expect(flags.shareEnabled.value).toBe(true)
    expect(flags.kbEnabled.value).toBe(true)
    expect(flags.terminalEnabled.value).toBe(true)
  })
})

describe('notifyFeatureDisabled', () => {
  it('shows the one "turned off, fix it in the manager" message', async () => {
    const i18n = (await import('@/i18n')).default
    const previous = i18n.global.locale.value
    i18n.global.locale.value = 'zh'
    try {
      notifyFeatureDisabled()
      expect(toastShow).toHaveBeenCalledTimes(1)
      const [message, options] = toastShow.mock.calls[0]
      expect(message).toBe('功能已关闭，去功能编排打开')
      expect(options).toMatchObject({ icon: 'blocked', type: 'info' })
    } finally {
      i18n.global.locale.value = previous
    }
  })
})

// 分类型分享权限：每类都要 AND 上总开关。把它们当成独立开关是最容易犯的错——
// 总开关关掉后，只按 allow_* 判断会让入口重新出现。
describe('useShareKindAllowed', () => {
  it('requires both the master switch and the kind permission', async () => {
    syncServerFeatures([feature('share', true)])
    syncFeatureConfig({ share: { allow_files: true } })
    await nextTick()
    expect(useShareKindAllowed('files').value).toBe(true)

    syncServerFeatures([feature('share', false)])
    await nextTick()
    expect(useShareKindAllowed('files').value).toBe(false)

    syncServerFeatures([feature('share', true)])
    syncFeatureConfig({ share: { allow_files: false } })
    await nextTick()
    expect(useShareKindAllowed('files').value).toBe(false)
  })

  it('keeps the three kinds independent', async () => {
    syncServerFeatures([feature('share', true)])
    syncFeatureConfig({ share: { allow_sessions: false } })
    await nextTick()
    expect(useShareKindAllowed('sessions').value).toBe(false)
    expect(useShareKindAllowed('files').value).toBe(true)
    expect(useShareKindAllowed('meetings').value).toBe(true)
  })

  it('treats a missing per-kind key as allowed (config predates it)', () => {
    syncServerFeatures([feature('share', true)])
    syncFeatureConfig({ share: {} })
    expect(useShareKindAllowed('files').value).toBe(true)
    expect(useShareKindAllowed('sessions').value).toBe(true)
    expect(useShareKindAllowed('meetings').value).toBe(true)
  })
})
