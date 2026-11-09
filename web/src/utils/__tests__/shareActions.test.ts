import { describe, expect, it, vi, beforeEach } from 'vitest'

/**
 * Host-capability routing for "send this link somewhere else".
 *
 * The point of these tests is the SILENT-FAILURE cases: a wrong guess here is a
 * tap that does nothing (Android swallows window.open), or a button that claims
 * to open a share sheet on a host that has none. So the assertions are about
 * which primitive was called, not just that something happened.
 */

const h = vi.hoisted(() => ({
  host: 'web' as 'web' | 'android' | 'harmonyos' | 'electron',
  shareText: vi.fn().mockResolvedValue(undefined),
  copyText: vi.fn(),
  openExternalUrl: vi.fn(),
}))

vi.mock('@/utils/clawbenchNative', () => ({
  getNativeHostPlatform: () => h.host,
  getNative: () =>
    h.host === 'web'
      ? undefined
      : { shareText: h.shareText },
}))

vi.mock('@/utils/clipboard', () => ({ copyText: (t: string) => h.copyText(t) }))
vi.mock('@/utils/externalLink', () => ({ openExternalUrl: (u: string) => h.openExternalUrl(u) }))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))

import {
  builtInMailAvailable,
  mailtoUrl,
  openSystemMail,
  shareCapability,
  shareLink,
} from '@/utils/shareActions'

const URL = 'http://10.0.0.5:20000/share/abc'

beforeEach(() => {
  vi.clearAllMocks()
  h.shareText.mockResolvedValue(undefined)
  h.host = 'web'
  // jsdom has no Web Share API by default; tests that need it install one.
  delete (navigator as unknown as Record<string, unknown>).share
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
})

describe('shareCapability', () => {
  it('reports a sheet on the phone shells', () => {
    h.host = 'android'
    expect(shareCapability()).toBe('sheet')
    h.host = 'harmonyos'
    expect(shareCapability()).toBe('sheet')
  })

  it('reports COPY on desktop — the shell has no system share sheet', () => {
    // The desktop bridge implements shareText by writing to the clipboard. A
    // probe of the method would therefore claim "sheet" on the one host that
    // has none, which is why the host is checked by name.
    h.host = 'electron'
    expect(shareCapability()).toBe('copy')
  })

  it('needs a secure context for the browser share sheet', () => {
    h.host = 'web'
    ;(navigator as unknown as Record<string, unknown>).share = vi.fn()
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true })
    // An http install on the LAN has no secure context, and navigator.share
    // rejects there — reporting "sheet" would promise something that fails.
    expect(shareCapability()).toBe('copy')

    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
    expect(shareCapability()).toBe('sheet')
  })
})

describe('shareLink', () => {
  it('uses the native share sheet on Android', async () => {
    h.host = 'android'
    const result = await shareLink(URL, 'report.md')
    expect(result).toBe('shared')
    expect(h.shareText).toHaveBeenCalledWith(URL)
    expect(h.copyText).not.toHaveBeenCalled()
  })

  it('falls back to the clipboard when the shell share call fails', async () => {
    h.host = 'android'
    h.shareText.mockRejectedValue(new Error('chooser failed'))
    const result = await shareLink(URL, 'report.md')
    // A broken chooser must not lose the link.
    expect(result).toBe('copied')
    expect(h.copyText).toHaveBeenCalledWith(URL)
  })

  it('copies on desktop and says so', async () => {
    h.host = 'electron'
    const result = await shareLink(URL, 'report.md')
    expect(result).toBe('copied')
    expect(h.copyText).toHaveBeenCalledWith(URL)
    expect(h.shareText).not.toHaveBeenCalled()
  })

  it('uses navigator.share in a secure browser', async () => {
    h.host = 'web'
    const share = vi.fn().mockResolvedValue(undefined)
    ;(navigator as unknown as Record<string, unknown>).share = share

    const result = await shareLink(URL, 'report.md')
    expect(result).toBe('shared')
    expect(share).toHaveBeenCalledWith({ title: 'report.md', url: URL })
  })

  it('treats the user closing the sheet as "shared", not as a failure', async () => {
    h.host = 'web'
    const abort = Object.assign(new Error('aborted'), { name: 'AbortError' })
    ;(navigator as unknown as Record<string, unknown>).share = vi.fn().mockRejectedValue(abort)

    const result = await shareLink(URL, 'report.md')
    // Copying something the user just declined to share would be a surprise.
    expect(result).toBe('shared')
    expect(h.copyText).not.toHaveBeenCalled()
  })

  it('copies when navigator.share fails for a real reason', async () => {
    h.host = 'web'
    ;(navigator as unknown as Record<string, unknown>).share = vi
      .fn()
      .mockRejectedValue(new Error('not allowed'))

    const result = await shareLink(URL, 'report.md')
    expect(result).toBe('copied')
    expect(h.copyText).toHaveBeenCalledWith(URL)
  })
})

describe('mail helpers', () => {
  it('reads the mail switch from server config, defaulting to available', () => {
    expect(builtInMailAvailable({})).toBe(true)
    expect(builtInMailAvailable({ mail: {} })).toBe(true)
    expect(builtInMailAvailable({ mail: { enabled: false } })).toBe(false)
  })

  it('encodes the subject and body so "&" and "#" survive', () => {
    const url = mailtoUrl('A & B', 'see http://x/share/1#top')
    expect(url).toContain('subject=A%20%26%20B')
    expect(url).toContain('body=see%20http%3A%2F%2Fx%2Fshare%2F1%23top')
  })

  it('opens the system mail app through the shared external-link primitive', () => {
    // window.open is swallowed by the Android WebView, so a bare anchor would
    // be a dead tap on the host where the system mail app is the fallback.
    openSystemMail('s', 'b')
    expect(h.openExternalUrl).toHaveBeenCalledWith('mailto:?subject=s&body=b')
  })
})
