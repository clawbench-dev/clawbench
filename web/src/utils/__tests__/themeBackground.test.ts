import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  applyWallpaper,
  applyWallpaperScrim,
  resolveWallpaperState,
  resolvePanelOpacity,
  resolveWallpaperUrl,
  resetWallpaperUrlCache,
  currentThemeIsDark,
  wallpaperImageUrl,
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
  })

  describe('resolveWallpaperState', () => {
    it('is unknown before the server config loads', () => {
      expect(resolveWallpaperState(undefined)).toBe('unknown')
      expect(resolveWallpaperState({})).toBe('unknown')
    })

    it('is set when wallpaper_file is non-empty', () => {
      expect(resolveWallpaperState({ wallpaper_file: 'background.png' })).toBe('set')
    })

    it('is unset when wallpaper_file is empty', () => {
      expect(resolveWallpaperState({ wallpaper_file: '' })).toBe('unset')
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
      expect(html.style.getPropertyValue('--wallpaper-url')).toContain('/api/file/theme-background')
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

    it('does not regenerate the image URL on alpha-only updates (slider drag)', () => {
      applyWallpaper('background.png', 0.9, false)
      const urlBefore = document.documentElement.style.getPropertyValue('--wallpaper-url')
      expect(urlBefore).toContain('/api/file/theme-background')

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

  describe('wallpaperImageUrl', () => {
    it('points at the theme-background endpoint', () => {
      expect(wallpaperImageUrl()).toContain('/api/file/theme-background?v=')
    })
  })

  describe('resolveWallpaperUrl', () => {
    it('returns an empty URL when no file is set', () => {
      expect(resolveWallpaperUrl('')).toBe('')
    })

    it('reuses the cached URL for the same file', () => {
      const a = resolveWallpaperUrl('background.png')
      const b = resolveWallpaperUrl('background.png')
      expect(a).toContain('/api/file/theme-background?v=')
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
