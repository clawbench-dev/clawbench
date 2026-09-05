import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getCustomFontChoices, isCustomFontId } from '@/utils/fontConfig'

// Mock the API layer so loadCustomFonts() never performs a real network call.
vi.mock('@/utils/api', () => ({
  apiGet: vi.fn(),
}))

import { loadCustomFonts, getCustomFonts, _resetCustomFonts } from '@/utils/customFonts'

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

    await loadCustomFonts()

    expect(apiGet).toHaveBeenCalledWith('/api/fonts/list')
    // Registry populated with the custom families.
    const ids = getCustomFontChoices().map(c => c.id)
    expect(ids).toEqual(['Sarasa Mono SC', 'Zhuque Fangsong'])
    expect(isCustomFontId('Sarasa Mono SC')).toBe(true)
    expect(isCustomFontId('Ghost')).toBe(false)

    // @font-face style injected with URL-encoded file names + escaped families.
    const style = document.getElementById('clawbench-custom-fonts') as HTMLStyleElement
    expect(style).toBeTruthy()
    const css = style.textContent ?? ''
    expect(css).toContain(`url('/api/fonts/file?name=${encodeURIComponent('Sarasa Mono SC.woff2')}') format('woff2')`)
    expect(css).toContain(`url('/api/fonts/file?name=${encodeURIComponent('Zhuque Fangsong.ttf')}') format('truetype')`)
    expect(css).toContain(`font-family:'Sarasa Mono SC'`)

    expect(dispatched).toContain('font-change')
  })

  it('degrades silently on API failure (no style, no registry)', async () => {
    const apiGet = (await import('@/utils/api')).apiGet as ReturnType<typeof vi.fn>
    apiGet.mockRejectedValue(new Error('unreachable'))

    await loadCustomFonts()

    expect(getCustomFonts().loaded).toBe(false)
    expect(document.getElementById('clawbench-custom-fonts')).toBeNull()
    expect(getCustomFontChoices()).toEqual([])
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
