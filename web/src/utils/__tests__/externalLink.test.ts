import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest'
import { openExternalUrl } from '@/utils/externalLink'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

beforeEach(() => {
  // The anchor fallback schedules its own removal. Fake timers keep that timer
  // pending, so it is neither a leaked handle nor a race against the assertion.
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  delete (window as any).ClawBenchNative
  document.body.querySelectorAll('a').forEach(a => a.remove())
})

describe('openExternalUrl', () => {
  it('prefers the native bridge when it exists', async () => {
    const bridge = vi.fn().mockResolvedValue(undefined)
    ;(window as any).ClawBenchNative = { openExternalUrl: bridge }

    openExternalUrl('https://github.com/xulongzhe/clawbench')
    await Promise.resolve()

    expect(bridge).toHaveBeenCalledWith('https://github.com/xulongzhe/clawbench')
    // The bridge path must NOT also click an anchor — on Android that click is
    // swallowed and would look like a second, dead attempt.
    expect(document.body.querySelectorAll('a')).toHaveLength(0)
  })

  it('falls back to an anchor click in the browser', () => {
    openExternalUrl('https://github.com/xulongzhe/clawbench/issues/new/choose')

    const anchor = document.body.querySelector('a') as HTMLAnchorElement | null
    expect(anchor).not.toBeNull()
    expect(anchor!.getAttribute('href')).toBe('https://github.com/xulongzhe/clawbench/issues/new/choose')
    // A new tab, and no window.opener handle for the target page.
    expect(anchor!.getAttribute('target')).toBe('_blank')
    expect(anchor!.getAttribute('rel')).toContain('noopener')
    expect(anchor!.getAttribute('rel')).toContain('noreferrer')
  })

  it('falls back to the anchor when an older host lacks the method', () => {
    // Older Android builds ship a bridge without openExternalUrl; the call must
    // not throw and the anchor path must still run.
    ;(window as any).ClawBenchNative = { isNativeApp: () => true }

    expect(() => openExternalUrl('https://github.com/xulongzhe/clawbench')).not.toThrow()
    expect(document.body.querySelectorAll('a')).toHaveLength(1)
  })

  it('does nothing for an empty URL', () => {
    openExternalUrl('')
    expect(document.body.querySelectorAll('a')).toHaveLength(0)
  })

  it('swallows a rejected bridge call instead of throwing', async () => {
    const bridge = vi.fn().mockRejectedValue(new Error('no browser'))
    ;(window as any).ClawBenchNative = { openExternalUrl: bridge }

    expect(() => openExternalUrl('https://github.com/xulongzhe/clawbench')).not.toThrow()
    // Unhandled rejections fail the vitest run, so let the catch settle.
    await Promise.resolve()
    await Promise.resolve()
    expect(bridge).toHaveBeenCalled()
  })
})
