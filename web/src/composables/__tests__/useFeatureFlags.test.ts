/**
 * The feature switches are the only thing standing between a disabled server
 * feature and a button that can only fail. These tests pin the three states the
 * reader has to distinguish: an explicit `false` (off), a missing key (an old
 * config file — must stay ON), and a malformed value (must fall back to the
 * default rather than silently disabling a feature).
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

const toastShow = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: toastShow }),
}))

const {
  FEATURE_FLAG_DEFAULTS,
  isFeatureEnabled,
  syncFeatureConfig,
  useFeatureFlag,
  useFeatureFlags,
  useShareKindAllowed,
  notifyFeatureDisabled,
} = await import('@/composables/useFeatureFlags')

beforeEach(() => {
  syncFeatureConfig({})
  toastShow.mockClear()
})

describe('isFeatureEnabled', () => {
  it('treats every switch as ON before the config loads', () => {
    for (const key of Object.keys(FEATURE_FLAG_DEFAULTS) as (keyof typeof FEATURE_FLAG_DEFAULTS)[]) {
      expect(isFeatureEnabled(key)).toBe(true)
    }
  })

  it('reads an explicit false as off', () => {
    syncFeatureConfig({ share: { enabled: false }, tasks: { enabled: false } })
    expect(isFeatureEnabled('share.enabled')).toBe(false)
    expect(isFeatureEnabled('tasks.enabled')).toBe(false)
    // Untouched sections keep their defaults.
    expect(isFeatureEnabled('mail.enabled')).toBe(true)
  })

  it('keeps a feature ON when its section is missing (config predates the key)', () => {
    syncFeatureConfig({ server: { port: 20000 } })
    expect(isFeatureEnabled('tasks.enabled')).toBe(true)
    expect(isFeatureEnabled('share.enabled')).toBe(true)
  })

  it('falls back to the default for a malformed value instead of disabling', () => {
    syncFeatureConfig({ share: { enabled: 'no' } })
    expect(isFeatureEnabled('share.enabled')).toBe(true)
  })

  it('survives a null config (server returned nothing usable)', () => {
    syncFeatureConfig(null)
    expect(isFeatureEnabled('share.enabled')).toBe(true)
  })
})

describe('useFeatureFlag', () => {
  it('re-evaluates when a new server config is synced', async () => {
    const shareEnabled = useFeatureFlag('share.enabled')
    expect(shareEnabled.value).toBe(true)

    syncFeatureConfig({ share: { enabled: false } })
    await nextTick()
    expect(shareEnabled.value).toBe(false)

    syncFeatureConfig({ share: { enabled: true } })
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
    syncFeatureConfig({ forge: { enabled: false } })
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
    syncFeatureConfig({ share: { enabled: true, allow_files: true } })
    await nextTick()
    expect(useShareKindAllowed('files').value).toBe(true)

    syncFeatureConfig({ share: { enabled: false, allow_files: true } })
    await nextTick()
    expect(useShareKindAllowed('files').value).toBe(false)

    syncFeatureConfig({ share: { enabled: true, allow_files: false } })
    await nextTick()
    expect(useShareKindAllowed('files').value).toBe(false)
  })

  it('keeps the three kinds independent', async () => {
    syncFeatureConfig({ share: { enabled: true, allow_sessions: false } })
    await nextTick()
    expect(useShareKindAllowed('sessions').value).toBe(false)
    expect(useShareKindAllowed('files').value).toBe(true)
    expect(useShareKindAllowed('meetings').value).toBe(true)
  })

  it('treats a missing per-kind key as allowed (config predates it)', () => {
    syncFeatureConfig({ share: { enabled: true } })
    expect(useShareKindAllowed('files').value).toBe(true)
    expect(useShareKindAllowed('sessions').value).toBe(true)
    expect(useShareKindAllowed('meetings').value).toBe(true)
  })
})
