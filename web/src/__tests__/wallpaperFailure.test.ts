import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: a failed wallpaper image must not paint the whole app.
 *
 * The wallpaper is a full-viewport `<img>` (see waveBackgroundWiring for why it
 * is not a CSS background). When its file is deleted or unreadable the browser
 * paints the broken-image glyph across the entire app AND its alt text — a
 * failure that is both enormous and unexplained.
 *
 * The correct degradation is to hide the layer: the wallpaper is decoration, so
 * the app should simply fall back to `--bg-primary` (which .wallpaper-layer
 * already sets as its background-color) rather than show a failure card. This
 * differs deliberately from content media, where a missing file is information
 * the reader needs and gets the shared MediaLoadError element.
 *
 * App.vue has no mount test (it is the entire application), so this is a
 * source-contract check — the same pattern waveBackgroundWiring.test.ts uses.
 */
describe('wallpaper failure handling', () => {
  const APP = 'src/App.vue'
  const BASE_CSS = 'css/base.css'

  function imgTag(src: string): string {
    const m = src.match(/<img\s+v-if="wallpaperUrl"[\s\S]*?\/>/)
    if (!m) throw new Error('wallpaper <img> not found in App.vue')
    return m[0]
  }

  it('handles the image error event', () => {
    // Without @error the broken-image glyph is painted at full viewport size.
    expect(imgTag(readWebFile(APP))).toContain('@error=')
  })

  it('hides the image (rather than showing a failure card) once it fails', () => {
    const tag = imgTag(readWebFile(APP))
    // `v-if` would remove the element, and a later successful retry could then
    // never revive it; the element must stay and be hidden.
    expect(tag).toContain('local-media-hidden')
    expect(tag).not.toContain('MediaLoadError')
  })

  it('resets the failure when a new wallpaper URL is applied', () => {
    // Switching wallpaper (or a re-upload with the same name) must clear the
    // previous failure, or the new image would stay hidden.
    const src = readWebFile(APP)
    expect(src).toMatch(/wallpaperFailed\.value\s*=\s*false/)
  })

  it('keeps the hidden state styling available outside a media figure', () => {
    // The hide rule lives in media-block.css and is deliberately context-free;
    // assert the contract from the CSS side too, since a wallpaper has no
    // .image-block-wrapper ancestor.
    const css = readWebFile('css/media-block.css').replace(/\/\*[\s\S]*?\*\//g, '')
    expect(css).toMatch(/\.local-media-hidden\s*\{[^}]*display:\s*none\s*!important/)
    // The layer must still provide a solid base so hiding the image is not blank.
    const base = readWebFile(BASE_CSS)
    expect(base).toMatch(/\.wallpaper-layer\s*\{[^}]*background-color:\s*var\(--bg-primary\)/)
  })
})
