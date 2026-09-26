import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the wave is a background with NO image file.
 *
 * The wallpaper layer and its `<img>` used to share a single condition
 * (`wallpaperActive`), because every background used to have a file. The
 * animated wave breaks that: `active_file` is empty for it, so `wallpaperActive`
 * and "there is an image URL" are no longer the same thing. Two separate
 * mistakes follow from getting this wrong, and both are silent:
 *
 *   1. `v-if="wallpaperActive"` on the <img> renders `<img src="">` in wave
 *      mode — an empty src makes some browsers request the current page URL.
 *   2. A single predicate for the settings rows either greys out panel opacity
 *      (which does apply to the wave) or leaves blur/edge-fade draggable
 *      (which do not).
 *
 * App.vue has no mount test (it is the entire application) and these are
 * template conditions, so they are asserted at the source level.
 */
describe('wave background wiring', () => {
  const APP = 'src/App.vue'
  const SETTING = 'src/components/settings/WallpaperSetting.vue'

  function imgTag(src: string): string {
    const m = src.match(/<img\s+v-if="[^"]+"\s+:src="wallpaperUrl"/)
    if (!m) throw new Error('wallpaper <img> opening tag not found')
    return m[0]
  }

  it('binds the wallpaper image to the URL, not to the layer visibility', () => {
    const src = readWebFile(APP)
    // An empty src is the bug; wallpaperUrl is '' exactly when there is no image.
    expect(imgTag(src)).toContain('v-if="wallpaperUrl"')
  })

  it('mounts the wave only when no image is shown', () => {
    const src = readWebFile(APP)
    expect(src).toMatch(/<WaveBackground\s+v-else-if="waveActive"/)
  })

  it('drives layer visibility from the image state OR the wave', () => {
    const src = readWebFile(APP)
    // A single `state === 'set'` would hide the layer for the wave.
    expect(src).toMatch(/wallpaperActive\.value\s*=\s*state === 'set' \|\| wave/)
  })

  it('passes the wave flag through to applyWallpaper', () => {
    const src = readWebFile(APP)
    // Without the 5th argument the translucent panels never turn on for the
    // wave, because active_file is empty. The call spans several lines.
    const call = src.match(/applyWallpaper\(\s*[\s\S]*?\)\n/)
    if (!call) throw new Error('applyWallpaper call not found in App.vue')
    expect(call[0]).toMatch(/\bwave,?\s*\)/)
  })

  it('uses the image-only predicate for blur and edge fade', () => {
    const src = readWebFile(SETTING)
    const blurRow = src.match(/settings-item--disabled': !(\w+) }">\s*<div class="settings-item__left">\s*<div class="settings-item__text">\s*<span class="settings-item__label">\{\{ t\('settings\.items\.wallpaperBlur'\) \}\}/)
    if (!blurRow) throw new Error('blur row not found')
    expect(blurRow[1]).toBe('hasImageWallpaper')
  })

  it('uses the any-background predicate for panel opacity', () => {
    const src = readWebFile(SETTING)
    const opacityRow = src.match(/settings-item--disabled': !(\w+) }">\s*<div class="settings-item__left">\s*<div class="settings-item__text">\s*<span class="settings-item__label">\{\{ t\('settings\.items\.wallpaperPanelOpacity'\) \}\}/)
    if (!opacityRow) throw new Error('panel opacity row not found')
    // Panel translucency applies to the wave too, so this must be the broader one.
    expect(opacityRow[1]).toBe('hasActiveBackground')
  })

  it('keeps the gallery as the fallback branch for an unset mode', () => {
    const src = readWebFile(SETTING)
    // Existing installs have wallpaper_mode "" → resolves to 'none'. Gating the
    // gallery on mode === 'local' would hide it for every existing user, since
    // the gallery is the only way to choose an image.
    const waveBranch = src.indexOf("v-if=\"mode === 'wave'\"")
    const bingBranch = src.indexOf("v-else-if=\"mode === 'bing'\"")
    const galleryBranch = src.indexOf('<template v-else>')
    expect(waveBranch).toBeGreaterThan(-1)
    expect(bingBranch).toBeGreaterThan(waveBranch)
    expect(galleryBranch).toBeGreaterThan(bingBranch)
    // Match the template TAG, not the explanatory comment that quotes it.
    expect(src).not.toMatch(/<template\s+v-else-if="mode === 'local'"/)
  })
})
