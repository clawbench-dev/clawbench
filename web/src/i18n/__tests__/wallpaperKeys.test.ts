import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

/**
 * Ensures every custom-wallpaper settings key exists and is non-empty in both
 * en and zh locales (the WallpaperSetting rows and App strings read these).
 */
describe('i18n wallpaper keys completeness', () => {
  const keys = [
    'wallpaper',
    'wallpaperDesc',
    'wallpaperUpload',
    'wallpaperReplace',
    'wallpaperRemove',
    'wallpaperSetOk',
    'wallpaperRemoved',
    'wallpaperUploadFailed',
    'wallpaperRemoveFailed',
    'wallpaperSaveFailed',
    'wallpaperLoading',
    'wallpaperPreview',
    'wallpaperPanelOpacity',
    'wallpaperPanelOpacityDesc',
    'wallpaperBlur',
    'wallpaperBlurDesc',
    'wallpaperEdgeFade',
    'wallpaperEdgeFadeDesc',
  ]

  it('exposes the same wallpaper keys in en and zh', () => {
    for (const locale of [en, zh]) {
      const items = locale.settings.items as Record<string, unknown>
      for (const key of keys) {
        expect(items[key], `settings.items.${key} missing in locale`).toBeTruthy()
      }
    }
  })
})
