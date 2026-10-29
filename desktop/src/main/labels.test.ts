import { describe, it, expect, vi, beforeEach } from 'vitest'

// labels.ts imports electron (app.getLocale) and the store at module scope.
const mockLocale = { value: 'en-US' }
const mockStored = { value: '' }
vi.mock('electron', () => ({
  app: { getLocale: () => mockLocale.value },
}))
vi.mock('./store', () => ({
  getStore: () => ({ get: () => mockStored.value }),
}))

import {
  normalizeNativeLanguage,
  resolveNativeLanguage,
  versionGateLabels,
  certificateLabels,
  floatingCapsuleLabels,
} from './labels'

describe('normalizeNativeLanguage', () => {
  it('maps any zh variant to Chinese', () => {
    for (const l of ['zh', 'zh-CN', 'zh_TW', 'ZH']) {
      expect(normalizeNativeLanguage(l)).toBe('zh')
    }
  })

  it('maps everything else (including empty) to English', () => {
    for (const l of ['en', 'en-US', 'de', '', 'fr']) {
      expect(normalizeNativeLanguage(l)).toBe('en')
    }
  })
})

describe('resolveNativeLanguage', () => {
  beforeEach(() => {
    mockStored.value = ''
    mockLocale.value = 'en-US'
  })

  it('prefers the web UI choice over the OS locale', () => {
    mockStored.value = 'zh'
    mockLocale.value = 'en-US'
    expect(resolveNativeLanguage()).toBe('zh')
  })

  it('falls back to the OS locale before the web app has reported one', () => {
    mockStored.value = ''
    mockLocale.value = 'zh-CN'
    expect(resolveNativeLanguage()).toBe('zh')
  })
})

describe('versionGateLabels', () => {
  it('returns Chinese wording for zh locales', () => {
    const l = versionGateLabels('zh-CN')
    expect(l.ok).toBe('确定')
    expect(l.downloadFailedTitle).toBe('下载失败')
    expect(l.versionReadyTitle).toBe('版本已就绪')
    expect(l.restartFailedTitle).toBe('重启失败')
  })

  it('returns English wording for non-Chinese locales', () => {
    const l = versionGateLabels('en')
    expect(l.ok).toBe('OK')
    expect(l.downloadFailedTitle).toBe('Download Failed')
    expect(l.versionReadyTitle).toBe('Version Ready')
    expect(l.restartFailedTitle).toBe('Restart Failed')
  })

  it('keeps the {version} placeholder in both locales so the message can interpolate', () => {
    // A translator dropping the placeholder would silently ship a message with
    // no version number — the only fact the dialog exists to convey.
    expect(versionGateLabels('en').versionReadyMessage).toContain('{version}')
    expect(versionGateLabels('zh').versionReadyMessage).toContain('{version}')
  })

  it('has no hardcoded Chinese left in the English table', () => {
    const l = versionGateLabels('en')
    for (const v of Object.values(l)) {
      expect(v).not.toMatch(/[\u4e00-\u9fff]/)
    }
  })
})

describe('floatingCapsuleLabels', () => {
  it('returns Chinese wording for zh locales', () => {
    const l = floatingCapsuleLabels('zh-CN')
    expect(l.running).toBe('执行中')
    expect(l.pending).toBe('待审批')
    expect(l.unread).toBe('未读')
    expect(l.idle).toBe('空闲')
  })

  it('returns English wording for non-Chinese locales', () => {
    const l = floatingCapsuleLabels('en')
    expect(l.running).toBe('Running')
    expect(l.pending).toBe('Pending')
    expect(l.unread).toBe('Unread')
    expect(l.idle).toBe('Idle')
  })

  it('has no hardcoded Chinese left in the English table', () => {
    // Every value here reaches an English user somewhere: the counter words and
    // `hint` on the capsule, the rest in the tray and right-click menus.
    const l = floatingCapsuleLabels('en')
    for (const [key, v] of Object.entries(l)) {
      expect(v, `floatingCapsuleLabels('en').${key} must be English`).not.toMatch(/[\u4e00-\u9fff]/)
    }
  })

  it('carries every slot the capsule and its menus render', () => {
    // A missing key would render `undefined` into the DOM (the document assigns
    // textContent directly), so pin the full set rather than a sample.
    const l = floatingCapsuleLabels('en')
    for (const key of [
      'running', 'pending', 'unread', 'idle',
      'openMain', 'hideMain', 'showFloating', 'hideFloating',
      'disableFloating', 'resetPosition', 'hide', 'quit', 'hint',
    ] as const) {
      expect(l[key], `floatingCapsuleLabels is missing ${key}`).toBeTruthy()
    }
  })
})

describe('certificateLabels', () => {
  it('returns Chinese wording for zh locales', () => {
    const l = certificateLabels('zh-CN')
    expect(l.title).toBe('SSL 证书验证失败')
    expect(l.trust).toBe('信任并继续')
    expect(l.cancel).toBe('取消连接')
  })

  it('returns English wording for non-Chinese locales', () => {
    const l = certificateLabels('en')
    expect(l.title).toBe('SSL certificate verification failed')
    expect(l.trust).toBe('Trust and continue')
    expect(l.cancel).toBe('Cancel connection')
  })

  it('has no hardcoded Chinese left in the English table', () => {
    const l = certificateLabels('en')
    for (const v of Object.values(l)) {
      expect(v).not.toMatch(/[\u4e00-\u9fff]/)
    }
  })
})
