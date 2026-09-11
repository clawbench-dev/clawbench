import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
/**
 * Ensures every custom-wallpaper settings key exists and is non-empty in both
 * en and zh locales (the WallpaperSetting rows and App strings read these).
 */
describe('i18n wallpaper keys completeness', () => {
  // Keys for the two wallpaper sources (local gallery + Bing daily) and the
  // display options below them.
  const keys = [
    // Legacy single-file keys still referenced by the file viewer action.
    'wallpaper',
    'wallpaperDesc',
    'wallpaperSetOk',
    'wallpaperUploadFailed',
    'wallpaperRemoveFailed',
    'wallpaperSaveFailed',
    'wallpaperPreview',
    // Global switch.
    'wallpaperEnable',
    'wallpaperEnableDesc',
    // Source selection.
    'wallpaperSource',
    'wallpaperSourceDesc',
    'wallpaperModeLocal',
    'wallpaperModeBing',
    // Bing daily wallpaper.
    'wallpaperBingFollow',
    'wallpaperBingFollowDesc',
    'wallpaperBingSync',
    'wallpaperBingSyncing',
    'wallpaperBingSynced',
    'wallpaperBingPending',
    'wallpaperBingStatus',
    'wallpaperBingSyncedAt',
    'wallpaperBingNoImage',
    'wallpaperBingFailed',
    // Local gallery.
    'wallpaperGallery',
    'wallpaperGalleryDesc',
    'wallpaperGalleryUpload',
    'wallpaperGalleryEmpty',
    'wallpaperGalleryCount',
    'wallpaperGalleryDelete',
    'wallpaperGalleryLimit',
    'wallpaperUploadPartial',
    'wallpaperUploadTooMany',
    'wallpaperSetFailed',
    // Display options.
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
  it('keeps the en and zh wallpaper key sets in sync', () => {
    const enItems = en.settings.items as Record<string, unknown>
    const zhItems = zh.settings.items as Record<string, unknown>
    const enKeys = Object.keys(enItems).filter((k) => k.startsWith('wallpaper')).sort()
    const zhKeys = Object.keys(zhItems).filter((k) => k.startsWith('wallpaper')).sort()
    expect(zhKeys).toEqual(enKeys)
  })
})
