import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getCustomFontChoices, isCustomFontId } from '@/utils/fontConfig'

// Mock the API layer so loadCustomFonts() never performs a real network call.
vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(),
}))

import { loadCustomFonts, getCustomFonts, _resetCustomFonts } from '@/utils/customFonts'

/** Returns a controllable deferred so tests can sequence in-flight scans. */
function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('customFonts loader', () => {
  beforeEach(() => {
    _resetCustomFonts()
  })

  afterEach(() => {
    _resetCustomFonts()
    vi.clearAllMocks()
  })

  it('registers families, injects @font-face and dispatches font-change', async () => {
    const apiGet = (await import('@/utils/api')).apiGet as ReturnType<typeof vi.fn>
    apiGet.mockResolvedValue({
      dir: '/data/fonts',
      fonts: [
        { family: 'Sarasa Mono SC', file: 'Sarasa Mono SC.woff2', ext: '.woff2', size: 1, mod_time: 'x' },
        { family: 'Zhuque Fangsong', file: 'Zhuque Fangsong.ttf', ext: '.ttf', size: 2, mod_time: 'x' },
      ],
    })
    const dispatched: string[] = []
    window.addEventListener('clawbench-font-change', () => dispatched.push('font-change'))

    const result = await loadCustomFonts()

    expect(result).toEqual({ ok: true, stale: false })
    expect(apiGet).toHaveBeenCalledWith('/api/fonts/list')
    // Registry populated with the custom families.
    const ids = getCustomFontChoices().map(c => c.id)
    expect(ids).toEqual(['Sarasa Mono SC', 'Zhuque Fangsong'])
    expect(isCustomFontId('Sarasa Mono SC')).toBe(true)
    expect(isCustomFontId('Ghost')).toBe(false)
    expect(getCustomFonts().error).toBeNull()

    // @font-face style injected with URL-encoded file names + escaped families.
    const style = document.getElementById('clawbench-custom-fonts') as HTMLStyleElement
    expect(style).toBeTruthy()
    const css = style.textContent ?? ''
    expect(css).toContain(`url('/api/fonts/file?name=${encodeURIComponent('Sarasa Mono SC.woff2')}') format('woff2')`)
    expect(css).toContain(`url('/api/fonts/file?name=${encodeURIComponent('Zhuque Fangsong.ttf')}') format('truetype')`)
    expect(css).toContain(`font-family:'Sarasa Mono SC'`)

    expect(dispatched).toContain('font-change')
  })

  it('surfaces API failure in state.error and keeps previous fonts intact', async () => {
    const apiGet = (await import('@/utils/api')).apiGet as ReturnType<typeof vi.fn>
    // First scan succeeds and populates the registry.
    apiGet.mockResolvedValueOnce({
      dir: '/data/fonts',
      fonts: [{ family: 'Kept', file: 'Kept.woff2', ext: '.woff2', size: 1, mod_time: 'x' }],
    })
    await loadCustomFonts()
    expect(getCustomFontChoices().map(c => c.id)).toEqual(['Kept'])

    // Second scan fails — previous fonts must not be wiped.
    apiGet.mockRejectedValueOnce(new Error('unreachable'))
    const result = await loadCustomFonts()

    expect(result).toEqual({ ok: false, stale: false })
    expect(getCustomFonts().error).toBe('unreachable')
    expect(getCustomFonts().loaded).toBe(true)
    expect(getCustomFonts().fonts.map(f => f.family)).toEqual(['Kept'])
    expect(getCustomFontChoices().map(c => c.id)).toEqual(['Kept'])
  })

  it('discards a stale response when a newer scan supersedes it', async () => {
    const apiGet = (await import('@/utils/api')).apiGet as ReturnType<typeof vi.fn>
    const first = deferred<{ dir: string; fonts: { family: string; file: string; ext: string; size: number; mod_time: string }[] }>()
    apiGet.mockReturnValueOnce(first.promise) // scan A — slow
    apiGet.mockResolvedValueOnce({ // scan B — fast, wins
      dir: '/data/fonts',
      fonts: [{ family: 'Newest', file: 'Newest.woff2', ext: '.woff2', size: 1, mod_time: 'x' }],
    })

    const scanA = loadCustomFonts()
    const scanB = loadCustomFonts()
    // Resolve the fast scan first (it becomes the latest), then the slow one.
    await scanB
    first.resolve({
      dir: '/data/fonts',
      fonts: [{ family: 'Old', file: 'Old.woff2', ext: '.woff2', size: 1, mod_time: 'x' }],
    })

    const resultA = await scanA

    // Stale scan is flagged and did NOT overwrite the newer registry.
    expect(resultA).toEqual({ ok: false, stale: true })
    expect(getCustomFonts().fonts.map(f => f.family)).toEqual(['Newest'])
    expect(getCustomFontChoices().map(c => c.id)).toEqual(['Newest'])
  })

  it('replaces the previous style content on rescan', async () => {
    const apiGet = (await import('@/utils/api')).apiGet as ReturnType<typeof vi.fn>
    apiGet.mockResolvedValue({
      dir: '/data/fonts',
      fonts: [{ family: 'First', file: 'First.woff2', ext: '.woff2', size: 1, mod_time: 'x' }],
    })
    await loadCustomFonts()
    const style1 = document.getElementById('clawbench-custom-fonts')
    expect(style1).toBeTruthy()

    apiGet.mockResolvedValue({
      dir: '/data/fonts',
      fonts: [{ family: 'Second', file: 'Second.woff2', ext: '.woff2', size: 1, mod_time: 'x' }],
    })
    await loadCustomFonts()
    const style2 = document.getElementById('clawbench-custom-fonts')
    expect(style2).toBe(style1) // same node reused
    expect(style2?.textContent).toContain('Second')
    expect(style2?.textContent).not.toContain('First')
  })
})
