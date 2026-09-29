import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  applyWallpaper,
  applyWallpaperScrim,
  resolveWallpaperState,
  resolvePanelOpacity,
  resolveWallpaperUrl,
  resetWallpaperUrlCache,
  currentThemeIsDark,
  galleryImageUrl,
  THUMB_WIDTH,
  invalidateGalleryImageUrls,
  resolveWallpaperMode,
  isWaveActive,
  resolveActiveFile,
  resolveGalleryItems,
  resolveBingStatus,
  isBingFirstImagePending,
  uploadGalleryImages,
  deleteGalleryItem,
  setWallpaperFromPath,
  syncBingNow,
  fetchBingStatus,
} from '../themeBackground'

// appLog relays to native/server; keep it inert in unit tests.
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

describe('themeBackground', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('wallpaper-active')
    document.documentElement.style.removeProperty('--wallpaper-url')
    document.documentElement.style.removeProperty('--wallpaper-scrim')
    document.documentElement.style.removeProperty('--panel-alpha')
    resetWallpaperUrlCache()
    invalidateGalleryImageUrls()
  })

  describe('resolveWallpaperState', () => {
    it('is unset when the device turned the wallpaper off', () => {
      expect(resolveWallpaperState('local', false, 'local-1-a.png', true)).toBe('unset')
      expect(resolveWallpaperState('wave', false, '', true)).toBe('unset')
    })

    it('is unset for the wave, which is a background but not an image', () => {
      expect(resolveWallpaperState('wave', true, '', true)).toBe('unset')
    })

    it('is set for a local image once one is selected', () => {
      expect(resolveWallpaperState('local', true, 'local-1-a.png', true)).toBe('set')
    })

    it('is unset for the local source with nothing selected', () => {
      expect(resolveWallpaperState('local', true, '', true)).toBe('unset')
    })

    it('is unknown for Bing only until the server config loads', () => {
      // The Bing file is the one asynchronous input: before /api/config
      // resolves we cannot know whether an image is cached.
      expect(resolveWallpaperState('bing', true, '', false)).toBe('unknown')
      expect(resolveWallpaperState('bing', true, '', true)).toBe('unset')
      expect(resolveWallpaperState('bing', true, 'bing-1.jpg', true)).toBe('set')
    })

    it('is unset for the none mode', () => {
      expect(resolveWallpaperState('none', true, '', true)).toBe('unset')
    })
  })

  describe('resolveWallpaperMode', () => {
    it('maps a stored local value', () => {
      expect(resolveWallpaperMode('local')).toBe('local')
      expect(resolveWallpaperMode('bing')).toBe('bing')
      expect(resolveWallpaperMode('wave')).toBe('wave')
    })

    it('is none when unset or unknown', () => {
      expect(resolveWallpaperMode(undefined)).toBe('none')
      expect(resolveWallpaperMode(null)).toBe('none')
      expect(resolveWallpaperMode('')).toBe('none')
      expect(resolveWallpaperMode('nonsense')).toBe('none')
    })
  })

  describe('isWaveActive', () => {
    it('is true only for the wave while enabled', () => {
      expect(isWaveActive('wave', true)).toBe(true)
      expect(isWaveActive('wave', false)).toBe(false)
    })

    it('is false for the image modes and for none', () => {
      expect(isWaveActive('local', true)).toBe(false)
      expect(isWaveActive('bing', true)).toBe(false)
      expect(isWaveActive('none', true)).toBe(false)
    })

    it('does not depend on a file, which the wave does not have', () => {
      // The wave has no file, so resolveWallpaperState reports 'unset' for it.
      // Detecting the wave therefore cannot go through the active file.
      expect(resolveWallpaperState('wave', true, '', true)).toBe('unset')
      expect(isWaveActive('wave', true)).toBe(true)
    })
  })

  describe('resolveActiveFile', () => {
    it('resolves the local source from this device\'s own selection', () => {
      expect(resolveActiveFile('local', true, 'local-1-a.png', 'bing-1.jpg')).toBe('local-1-a.png')
    })

    it('resolves the Bing source from the server cache', () => {
      expect(resolveActiveFile('bing', true, 'local-1-a.png', 'bing-1.jpg')).toBe('bing-1.jpg')
    })

    it('is empty for wave / none, which have no file', () => {
      expect(resolveActiveFile('wave', true, 'local-1-a.png', 'bing-1.jpg')).toBe('')
      expect(resolveActiveFile('none', true, 'local-1-a.png', 'bing-1.jpg')).toBe('')
    })

    it('is empty for every source while the wallpaper is switched off', () => {
      // The returned name is what activates the translucent-panel rules, so a
      // disabled wallpaper must resolve to no file: otherwise turning the
      // wallpaper off hid the image but left the panels see-through over an
      // empty background. The server-side ResolveActive this replaced checked
      // the switch before resolving any source.
      expect(resolveActiveFile('local', false, 'local-1-a.png', 'bing-1.jpg')).toBe('')
      expect(resolveActiveFile('bing', false, 'local-1-a.png', 'bing-1.jpg')).toBe('')
      expect(resolveActiveFile('wave', false, 'local-1-a.png', 'bing-1.jpg')).toBe('')
      expect(resolveActiveFile('none', false, 'local-1-a.png', 'bing-1.jpg')).toBe('')
    })
  })

  describe('resolveGalleryItems', () => {
    it('returns the items list', () => {
      const items = [{ file: 'local-1-a.png', name: 'a.png', uploaded_at: 1, size: 2 }]
      expect(resolveGalleryItems({ local: { items } })).toEqual(items)
    })

    it('is empty when absent or malformed', () => {
      expect(resolveGalleryItems(undefined)).toEqual([])
      expect(resolveGalleryItems({})).toEqual([])
      expect(resolveGalleryItems({ local: {} })).toEqual([])
      expect(resolveGalleryItems({ local: { items: 'nope' } })).toEqual([])
    })
  })

  describe('resolveBingStatus', () => {
    it('returns the bing section', () => {
      const bing = resolveBingStatus({ bing: { file: 'bing-20260910.jpg', copyright: '© x' } })
      expect(bing.file).toBe('bing-20260910.jpg')
      expect(bing.copyright).toBe('© x')
    })

    it('fills in defaults for missing fields', () => {
      const bing = resolveBingStatus(undefined)
      expect(bing.file).toBe('')
      expect(bing.last_error).toBe('')
    })
  })

  describe('isBingFirstImagePending', () => {
    it('is true when this device wants Bing and the cache is still empty', () => {
      expect(isBingFirstImagePending('bing', true, '', true)).toBe(true)
    })

    it('is false once an image is cached', () => {
      expect(isBingFirstImagePending('bing', true, 'bing-20260910.jpg', true)).toBe(false)
    })

    it('is false when the wallpaper is disabled', () => {
      expect(isBingFirstImagePending('bing', false, '', true)).toBe(false)
    })

    it('is false for the other sources', () => {
      expect(isBingFirstImagePending('local', true, '', true)).toBe(false)
      expect(isBingFirstImagePending('wave', true, '', true)).toBe(false)
    })

    it('is false before the config loads, so it does not poll blind', () => {
      // Without the server's answer we cannot tell "no image yet" from "not
      // loaded yet"; polling on the latter would spin for a result already there.
      expect(isBingFirstImagePending('bing', true, '', false)).toBe(false)
    })
  })

  describe('resolvePanelOpacity', () => {
    it('defaults to 0.7 for missing / non-numeric values', () => {
      expect(resolvePanelOpacity(undefined)).toBe(0.7)
      expect(resolvePanelOpacity(null)).toBe(0.7)
      expect(resolvePanelOpacity('')).toBe(0.7)
      expect(resolvePanelOpacity('abc')).toBe(0.7)
    })

    it('accepts any value across the whole 0-1 range', () => {
      expect(resolvePanelOpacity(0)).toBe(0)
      expect(resolvePanelOpacity(0.35)).toBe(0.35)
      expect(resolvePanelOpacity(1)).toBe(1)
      // A stringified value (hand-edited localStorage) is coerced.
      expect(resolvePanelOpacity('0.6')).toBe(0.6)
    })

    it('rejects out-of-range values', () => {
      expect(resolvePanelOpacity(-0.2)).toBe(0.7)
      expect(resolvePanelOpacity(1.5)).toBe(0.7)
    })
  })

  describe('currentThemeIsDark', () => {
    it('follows auto via system preference', () => {
      // jsdom lacks matchMedia — stub it to a dark preference.
      vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
      try {
        expect(currentThemeIsDark('auto')).toBe(true)
      } finally {
        vi.unstubAllGlobals()
      }
    })

    it('maps concrete theme ids', () => {
      expect(currentThemeIsDark('github-dark')).toBe(true)
      expect(currentThemeIsDark('github-light')).toBe(false)
    })
  })

  describe('applyWallpaper', () => {
    it('activates the wallpaper and injects image URL + scrim + alpha', () => {
      applyWallpaper('background.png', 0.9, false)
      const html = document.documentElement
      expect(html.classList.contains('wallpaper-active')).toBe(true)
      expect(html.style.getPropertyValue('--wallpaper-url')).toContain('url("')
      expect(html.style.getPropertyValue('--wallpaper-url')).toContain('/api/file/theme-wallpaper?name=background.png')
      expect(html.style.getPropertyValue('--wallpaper-scrim')).toBe('rgba(0, 0, 0, 0.12)')
      expect(html.style.getPropertyValue('--panel-alpha')).toBe('90%')
    })

    it('uses a stronger scrim for dark themes', () => {
      applyWallpaper('background.png', 0.85, true)
      expect(document.documentElement.style.getPropertyValue('--wallpaper-scrim')).toBe('rgba(0, 0, 0, 0.35)')
    })

    it('clamps the alpha to the valid range', () => {
      applyWallpaper('background.png', 5, false)
      expect(document.documentElement.style.getPropertyValue('--panel-alpha')).toBe('100%')
      applyWallpaper('background.png', -1, false)
      expect(document.documentElement.style.getPropertyValue('--panel-alpha')).toBe('0%')
    })

    it('clears the effect when the wallpaper file is empty', () => {
      applyWallpaper('background.png', 0.85, false)
      applyWallpaper('', 0.85, false)
      const html = document.documentElement
      expect(html.classList.contains('wallpaper-active')).toBe(false)
      expect(html.style.getPropertyValue('--wallpaper-url')).toBe('none')
      expect(html.style.getPropertyValue('--wallpaper-scrim')).toBe('transparent')
    })

    it('activates the translucent panels for the wave, which has no file', () => {
      // The wave is a background with no image, so the 5th argument is the only
      // signal that the surfaces must go translucent.
      applyWallpaper('', 0.7, false, false, true)
      const html = document.documentElement
      expect(html.classList.contains('wallpaper-active')).toBe(true)
      expect(html.style.getPropertyValue('--wallpaper-url')).toBe('none')
      expect(html.style.getPropertyValue('--panel-alpha')).toBe('70%')
    })

    it('does not write a scrim for the wave', () => {
      // The scrim darkens an image for contrast; over the wave it would just
      // dim the background for no reason.
      applyWallpaper('', 0.85, true, false, true)
      expect(document.documentElement.style.getPropertyValue('--wallpaper-scrim')).toBe('transparent')
    })

    it('clears the wave effect when waveActive goes false', () => {
      applyWallpaper('', 0.85, false, false, true)
      expect(document.documentElement.classList.contains('wallpaper-active')).toBe(true)
      applyWallpaper('', 0.85, false, false, false)
      expect(document.documentElement.classList.contains('wallpaper-active')).toBe(false)
    })

    it('keeps 4-argument calls behaving exactly as before', () => {
      // The wave flag is optional precisely so existing callers and tests are
      // unaffected; an image still activates and still gets its scrim.
      applyWallpaper('background.png', 0.85, true)
      const html = document.documentElement
      expect(html.classList.contains('wallpaper-active')).toBe(true)
      expect(html.style.getPropertyValue('--wallpaper-scrim')).toBe('rgba(0, 0, 0, 0.35)')
    })

    it('still writes a scrim when both a file and the wave flag are given', () => {
      // Defensive: a file is authoritative, so the scrim follows the image.
      applyWallpaper('background.png', 0.85, false, false, true)
      expect(document.documentElement.style.getPropertyValue('--wallpaper-scrim')).toBe('rgba(0, 0, 0, 0.12)')
    })

    it('does not regenerate the image URL on alpha-only updates (slider drag)', () => {
      applyWallpaper('background.png', 0.9, false)
      const urlBefore = document.documentElement.style.getPropertyValue('--wallpaper-url')
      expect(urlBefore).toContain('/api/file/theme-wallpaper?name=background.png')

      // Alpha/scrim-only refresh (simulates an opacity-slider tick) must keep
      // the SAME URL — a fresh one would re-download the wallpaper every tick.
      applyWallpaper('background.png', 0.8, true)
      const urlAfter = document.documentElement.style.getPropertyValue('--wallpaper-url')
      expect(urlAfter).toBe(urlBefore)
      expect(document.documentElement.style.getPropertyValue('--panel-alpha')).toBe('80%')
      expect(document.documentElement.style.getPropertyValue('--wallpaper-scrim')).toBe('rgba(0, 0, 0, 0.35)')
    })

    it('regenerates the URL when the wallpaper file changes', () => {
      applyWallpaper('background.png', 0.85, false)
      const urlPng = document.documentElement.style.getPropertyValue('--wallpaper-url')
      applyWallpaper('background.jpg', 0.85, false)
      const urlJpg = document.documentElement.style.getPropertyValue('--wallpaper-url')
      expect(urlJpg).not.toBe(urlPng)
    })

    it('regenerates the URL on forceBust (same-name re-upload)', () => {
      applyWallpaper('background.png', 0.85, false)
      const urlBefore = document.documentElement.style.getPropertyValue('--wallpaper-url')
      applyWallpaper('background.png', 0.85, false, true)
      const urlAfter = document.documentElement.style.getPropertyValue('--wallpaper-url')
      expect(urlAfter).not.toBe(urlBefore)
    })
  })

  describe('applyWallpaperScrim', () => {
    it('updates only the scrim when a wallpaper is active', () => {
      applyWallpaper('background.png', 0.85, false)
      applyWallpaperScrim(true)
      expect(document.documentElement.style.getPropertyValue('--wallpaper-scrim')).toBe('rgba(0, 0, 0, 0.35)')
    })

    it('is a no-op when no wallpaper is active', () => {
      applyWallpaperScrim(true)
      expect(document.documentElement.style.getPropertyValue('--wallpaper-scrim')).toBe('')
    })
  })


  describe('galleryImageUrl', () => {
    it('points at the by-name wallpaper endpoint', () => {
      expect(galleryImageUrl('local-1-a.png')).toContain('/api/file/theme-wallpaper?name=local-1-a.png')
    })

    it('encodes the file name', () => {
      expect(galleryImageUrl('local 1 a.png')).toContain('name=local%201%20a.png')
    })

    it('caches per file so re-renders do not re-download the gallery', () => {
      // Gallery tiles call this on every render; a fresh URL each time would
      // bypass the server's immutable caching and re-download every image.
      const first = galleryImageUrl('local-1-a.png')
      expect(galleryImageUrl('local-1-a.png')).toBe(first)
      expect(galleryImageUrl('local-2-b.png')).not.toBe(first)
    })

    it('regenerates after invalidateGalleryImageUrls', () => {
      const before = galleryImageUrl('local-1-a.png')
      invalidateGalleryImageUrls(['local-1-a.png'])
      expect(galleryImageUrl('local-1-a.png')).not.toBe(before)
    })

    it('regenerates for all files when no names are given', () => {
      const a = galleryImageUrl('local-1-a.png')
      const b = galleryImageUrl('local-2-b.png')
      invalidateGalleryImageUrls()
      expect(galleryImageUrl('local-1-a.png')).not.toBe(a)
      expect(galleryImageUrl('local-2-b.png')).not.toBe(b)
    })

    it('uses the server thumbnail endpoint when an absolute path is given', () => {
      // Regression: the tile is 72px wide but the full image was downloaded —
      // 2.4MB for the Bing 4K wallpaper, ~470x more than needed.
      invalidateGalleryImageUrls()
      const url = galleryImageUrl('local-1-a.png', '/data/theme/local/local-1-a.png')

      expect(url).toContain('/api/fs/thumb?target=')
      expect(url).toContain(encodeURIComponent('/data/theme/local/local-1-a.png'))
      expect(url).toContain(`w=${THUMB_WIDTH}`)
      expect(url).not.toContain('theme-wallpaper')
    })

    it('falls back to the full-size endpoint without an absolute path', () => {
      // An older server (or an unresolvable name) sends no abs_path.
      invalidateGalleryImageUrls()
      const url = galleryImageUrl('local-1-a.png')

      expect(url).toContain('/api/file/theme-wallpaper?name=local-1-a.png')
      expect(url).not.toContain('/api/fs/thumb')
    })

    it('falls back to the full-size endpoint for SVG', () => {
      // /api/fs/thumb only rasterizes png/jpg/gif; SVG would 404, so it must
      // keep using the wallpaper endpoint (which serves it under a sandbox CSP).
      invalidateGalleryImageUrls()
      const url = galleryImageUrl('local-1-a.svg', '/data/theme/local/local-1-a.svg')

      expect(url).toContain('/api/file/theme-wallpaper?name=local-1-a.svg')
      expect(url).not.toContain('/api/fs/thumb')
    })

    it('caches the thumbnail URL per file', () => {
      invalidateGalleryImageUrls()
      const first = galleryImageUrl('local-1-a.png', '/data/theme/local/local-1-a.png')
      expect(galleryImageUrl('local-1-a.png', '/data/theme/local/local-1-a.png')).toBe(first)
    })
  })

  describe('gallery API', () => {
    it('uploads all files under the files field', async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ items: [{ file: 'local-1-a.png' }], errors: [] }),
      })
      vi.stubGlobal('fetch', fetchMock)
      try {
        const f1 = new File(['a'], 'a.png', { type: 'image/png' })
        const f2 = new File(['b'], 'b.png', { type: 'image/png' })
        const out = await uploadGalleryImages([f1, f2])

        expect(fetchMock).toHaveBeenCalledTimes(1)
        const [url, init] = fetchMock.mock.calls[0]
        expect(url).toBe('/api/theme/local/upload')
        expect(init.method).toBe('POST')
        const form = init.body as FormData
        expect(form.getAll('files')).toHaveLength(2)
        expect(out.items).toHaveLength(1)
      } finally {
        vi.unstubAllGlobals()
      }
    })

    it('throws when the upload request fails', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 400 }))
      try {
        await expect(uploadGalleryImages([new File(['a'], 'a.png')])).rejects.toThrow('400')
      } finally {
        vi.unstubAllGlobals()
      }
    })

    it('deletes by name in the query string', async () => {
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ items: [] }) })
      vi.stubGlobal('fetch', fetchMock)
      try {
        await deleteGalleryItem('local-1-a.png')
        const [url, init] = fetchMock.mock.calls[0]
        expect(url).toBe('/api/theme/local/item?name=local-1-a.png')
        expect(init.method).toBe('DELETE')
      } finally {
        vi.unstubAllGlobals()
      }
    })

    it('sets a wallpaper from a server file by reading bytes, uploading, then selecting locally', async () => {
      const blob = new Blob(['png-bytes'], { type: 'image/png' })
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, blob: async () => blob })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ items: [{ file: 'local-9-z.png' }], errors: [] }) })
      vi.stubGlobal('fetch', fetchMock)
      const writes: Record<string, string> = {}
      try {
        await setWallpaperFromPath('assets/wall.png', (k, v) => { writes[k] = v })

        // 1. Read the source file bytes through the local-file endpoint.
        expect(fetchMock.mock.calls[0][0]).toContain('/api/fs/raw/assets/wall.png')
        // 2. Upload the bytes into the gallery.
        expect(fetchMock.mock.calls[1][0]).toBe('/api/theme/local/upload')
        const form = fetchMock.mock.calls[1][1].body as FormData
        expect((form.getAll('files')[0] as File).name).toBe('wall.png')
        // 3. Adopt it locally — the server no longer selects on upload, so this
        //    device is the only place the new image can become the wallpaper.
        expect(writes).toEqual({ wallpaperLocalSelected: 'local-9-z.png', wallpaperMode: 'local' })
      } finally {
        vi.unstubAllGlobals()
      }
    })

    it('throws when the gallery upload rejects the source image', async () => {
      const blob = new Blob(['x'], { type: 'image/png' })
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, blob: async () => blob })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ items: [], errors: [{ name: 'wall.png', error: 'unsupported image' }] }) })
      vi.stubGlobal('fetch', fetchMock)
      try {
        await expect(setWallpaperFromPath('assets/wall.png', () => {})).rejects.toThrow('unsupported image')
      } finally {
        vi.unstubAllGlobals()
      }
    })

    it('syncs Bing and reads the status', async () => {
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ enabled: true, file: 'bing-1.jpg' }) })
      vi.stubGlobal('fetch', fetchMock)
      try {
        const sync = await syncBingNow()
        expect(fetchMock.mock.calls[0][0]).toBe('/api/theme/bing/sync')
        expect(fetchMock.mock.calls[0][1].method).toBe('POST')
        expect(sync.file).toBe('bing-1.jpg')

        const status = await fetchBingStatus()
        expect(fetchMock.mock.calls[1][0]).toBe('/api/theme/bing/status')
        expect(status.enabled).toBe(true)
      } finally {
        vi.unstubAllGlobals()
      }
    })

    it('throws when a Bing sync fails', async () => {
      vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500 }))
      try {
        await expect(syncBingNow()).rejects.toThrow('500')
      } finally {
        vi.unstubAllGlobals()
      }
    })
  })

  describe('resolveWallpaperUrl', () => {
    it('returns an empty URL when no file is set', () => {
      expect(resolveWallpaperUrl('')).toBe('')
    })

    it('reuses the cached URL for the same file', () => {
      const a = resolveWallpaperUrl('background.png')
      const b = resolveWallpaperUrl('background.png')
      expect(a).toContain('/api/file/theme-wallpaper?name=background.png&v=')
      expect(b).toBe(a)
    })

    it('regenerates the URL when the file changes', () => {
      const png = resolveWallpaperUrl('background.png')
      const jpg = resolveWallpaperUrl('background.jpg')
      expect(jpg).not.toBe(png)
    })

    it('regenerates on forceBust even for the same file (same-name re-upload)', () => {
      const a = resolveWallpaperUrl('background.png')
      const b = resolveWallpaperUrl('background.png', true)
      expect(b).not.toBe(a)
    })

    it('resets to empty when the file is cleared', () => {
      const a = resolveWallpaperUrl('background.png')
      expect(a).toBeTruthy()
      expect(resolveWallpaperUrl('')).toBe('')
    })
  })
})
