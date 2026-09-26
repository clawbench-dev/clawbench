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
  resolveWallpaperEnabled,
  isWaveActive,
  resolveActiveFile,
  resolveGalleryItems,
  resolveGallerySelected,
  resolveBingStatus,
  isBingFirstImagePending,
  bingMktForLocale,
  uploadGalleryImages,
  deleteGalleryItem,
  selectGalleryItem,
  setWallpaperMode,
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
    it('is unknown before the server config loads', () => {
      expect(resolveWallpaperState(undefined)).toBe('unknown')
      expect(resolveWallpaperState({})).toBe('unknown')
    })

    it('is set when active_file is non-empty', () => {
      expect(resolveWallpaperState({ active_file: 'local-1-a.png' })).toBe('set')
    })

    it('is unset when active_file is empty', () => {
      expect(resolveWallpaperState({ active_file: '' })).toBe('unset')
    })

    it('is unset when the wallpaper is globally disabled', () => {
      // The server clears active_file while disabled, so the gallery can be
      // retained without the wallpaper being shown.
      expect(resolveWallpaperState({ active_file: '', wallpaper_enabled: false })).toBe('unset')
    })
  })

  describe('resolveWallpaperMode', () => {
    it('maps the server mode', () => {
      expect(resolveWallpaperMode({ wallpaper_mode: 'local' })).toBe('local')
      expect(resolveWallpaperMode({ wallpaper_mode: 'bing' })).toBe('bing')
      expect(resolveWallpaperMode({ wallpaper_mode: 'wave' })).toBe('wave')
    })

    it('is none when unset or unknown', () => {
      expect(resolveWallpaperMode(undefined)).toBe('none')
      expect(resolveWallpaperMode({})).toBe('none')
      expect(resolveWallpaperMode({ wallpaper_mode: 'nonsense' })).toBe('none')
    })
  })

  describe('isWaveActive', () => {
    it('is true when the mode is wave and the wallpaper is enabled', () => {
      expect(isWaveActive({ wallpaper_mode: 'wave', wallpaper_enabled: true })).toBe(true)
    })

    it('is true when wallpaper_enabled is absent (defaults to enabled)', () => {
      expect(isWaveActive({ wallpaper_mode: 'wave' })).toBe(true)
    })

    it('is false when the global switch is off', () => {
      expect(isWaveActive({ wallpaper_mode: 'wave', wallpaper_enabled: false })).toBe(false)
    })

    it('is false for the image modes', () => {
      expect(isWaveActive({ wallpaper_mode: 'local', wallpaper_enabled: true })).toBe(false)
      expect(isWaveActive({ wallpaper_mode: 'bing', wallpaper_enabled: true })).toBe(false)
    })

    it('is false before the config loads', () => {
      expect(isWaveActive(undefined)).toBe(false)
      expect(isWaveActive({})).toBe(false)
    })

    it('does not depend on active_file, which is empty for the wave', () => {
      // The wave has no file, so resolveWallpaperState reports 'unset' for it.
      // Detecting the wave therefore cannot go through active_file.
      const appearance = { wallpaper_mode: 'wave', wallpaper_enabled: true, active_file: '' }
      expect(resolveWallpaperState(appearance)).toBe('unset')
      expect(isWaveActive(appearance)).toBe(true)
    })
  })

  describe('resolveWallpaperEnabled', () => {
    it('defaults to enabled before the config loads', () => {
      expect(resolveWallpaperEnabled(undefined)).toBe(true)
      expect(resolveWallpaperEnabled({})).toBe(true)
    })

    it('reflects an explicit disable', () => {
      expect(resolveWallpaperEnabled({ wallpaper_enabled: false })).toBe(false)
      expect(resolveWallpaperEnabled({ wallpaper_enabled: true })).toBe(true)
    })
  })

  describe('resolveActiveFile', () => {
    it('returns the server-resolved active_file', () => {
      expect(resolveActiveFile({ active_file: 'local-1-a.png' })).toBe('local-1-a.png')
    })

    it('is empty when nothing is active', () => {
      expect(resolveActiveFile(undefined)).toBe('')
      expect(resolveActiveFile({})).toBe('')
      expect(resolveActiveFile({ active_file: '' })).toBe('')
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

  describe('resolveGallerySelected', () => {
    it('returns the selection', () => {
      expect(resolveGallerySelected({ local: { selected: 'local-1-a.png' } })).toBe('local-1-a.png')
    })

    it('is empty when absent', () => {
      expect(resolveGallerySelected(undefined)).toBe('')
      expect(resolveGallerySelected({})).toBe('')
    })
  })

  describe('resolveBingStatus', () => {
    it('returns the bing section', () => {
      const bing = resolveBingStatus({ bing: { enabled: true, file: 'bing-20260910.jpg', copyright: '© x' } })
      expect(bing.enabled).toBe(true)
      expect(bing.file).toBe('bing-20260910.jpg')
      expect(bing.copyright).toBe('© x')
    })

    it('fills in defaults for missing fields', () => {
      const bing = resolveBingStatus(undefined)
      expect(bing.enabled).toBe(false)
      expect(bing.file).toBe('')
      expect(bing.last_error).toBe('')
    })
  })

  describe('bingMktForLocale', () => {
    it('maps Chinese locales to zh-CN', () => {
      expect(bingMktForLocale('zh')).toBe('zh-CN')
      expect(bingMktForLocale('zh-CN')).toBe('zh-CN')
    })

    it('maps everything else to en-US', () => {
      expect(bingMktForLocale('en')).toBe('en-US')
      expect(bingMktForLocale('fr')).toBe('en-US')
      expect(bingMktForLocale('')).toBe('en-US')
    })
  })

  describe('isBingFirstImagePending', () => {
    it('is true when Bing is active but no image is cached yet', () => {
      expect(isBingFirstImagePending({ wallpaper_mode: 'bing', wallpaper_enabled: true, active_file: '' })).toBe(true)
    })

    it('is false once an image is available', () => {
      expect(isBingFirstImagePending({ wallpaper_mode: 'bing', wallpaper_enabled: true, active_file: 'bing-20260910.jpg' })).toBe(false)
    })

    it('is false when the wallpaper is disabled', () => {
      expect(isBingFirstImagePending({ wallpaper_mode: 'bing', wallpaper_enabled: false, active_file: '' })).toBe(false)
    })

    it('is false for the local source', () => {
      expect(isBingFirstImagePending({ wallpaper_mode: 'local', wallpaper_enabled: true, active_file: '' })).toBe(false)
    })

    it('is false before the config loads', () => {
      expect(isBingFirstImagePending(undefined)).toBe(false)
      expect(isBingFirstImagePending({})).toBe(false)
    })
  })

  describe('resolvePanelOpacity', () => {
    it('defaults to 0.85', () => {
      expect(resolvePanelOpacity(undefined)).toBe(0.85)
      expect(resolvePanelOpacity({})).toBe(0.85)
    })

    it('returns the configured value', () => {
      expect(resolvePanelOpacity({ panel_opacity: 0.7 })).toBe(0.7)
      expect(resolvePanelOpacity({ panel_opacity: 1 })).toBe(1)
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
      applyWallpaper('background.png', 0.1, false)
      expect(document.documentElement.style.getPropertyValue('--panel-alpha')).toBe('50%')
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
      // The wave is a background with no image: active_file is empty, so the
      // 5th argument is the only signal that the surfaces must go translucent.
      applyWallpaper('', 0.85, false, false, true)
      const html = document.documentElement
      expect(html.classList.contains('wallpaper-active')).toBe(true)
      expect(html.style.getPropertyValue('--wallpaper-url')).toBe('none')
      expect(html.style.getPropertyValue('--panel-alpha')).toBe('85%')
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
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ active_file: '' }) })
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

    it('selects by JSON body', async () => {
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ active_file: 'local-1-a.png' }) })
      vi.stubGlobal('fetch', fetchMock)
      try {
        await selectGalleryItem('local-1-a.png')
        const [url, init] = fetchMock.mock.calls[0]
        expect(url).toBe('/api/theme/local/select')
        expect(JSON.parse(init.body as string)).toEqual({ name: 'local-1-a.png' })
      } finally {
        vi.unstubAllGlobals()
      }
    })

    it('sets a wallpaper from a server file by reading bytes then uploading+selecting', async () => {
      const blob = new Blob(['png-bytes'], { type: 'image/png' })
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({ ok: true, blob: async () => blob })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ items: [{ file: 'local-9-z.png' }], errors: [] }) })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ active_file: 'local-9-z.png' }) })
      vi.stubGlobal('fetch', fetchMock)
      try {
        const out = await setWallpaperFromPath('assets/wall.png')

        // 1. Read the source file bytes through the local-file endpoint.
        expect(fetchMock.mock.calls[0][0]).toContain('/api/fs/raw/assets/wall.png')
        // 2. Upload the bytes into the gallery.
        expect(fetchMock.mock.calls[1][0]).toBe('/api/theme/local/upload')
        const form = fetchMock.mock.calls[1][1].body as FormData
        expect((form.getAll('files')[0] as File).name).toBe('wall.png')
        // 3. Select the new gallery entry as the active wallpaper.
        expect(fetchMock.mock.calls[2][0]).toBe('/api/theme/local/select')
        expect(JSON.parse(fetchMock.mock.calls[2][1].body as string)).toEqual({ name: 'local-9-z.png' })
        expect(out.active_file).toBe('local-9-z.png')
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
        await expect(setWallpaperFromPath('assets/wall.png')).rejects.toThrow('unsupported image')
      } finally {
        vi.unstubAllGlobals()
      }
    })

    it('sets the mode via the wallpaper endpoint', async () => {
      const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ mode: 'bing' }) })
      vi.stubGlobal('fetch', fetchMock)
      try {
        await setWallpaperMode({ mode: 'bing', enabled: true })
        const [url, init] = fetchMock.mock.calls[0]
        expect(url).toBe('/api/theme/wallpaper')
        expect(JSON.parse(init.body as string)).toEqual({ mode: 'bing', enabled: true })
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
