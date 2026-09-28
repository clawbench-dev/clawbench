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

  /**
   * Self-heal for a dangling selection.
   *
   * The selected gallery image is this device's own pointer, but the file lives
   * on the server where another device can delete it. Nothing notifies us, so
   * the pointer goes stale and the <img> fails. An <img>'s error event carries
   * no status, so the two causes — "the file is gone" (heal) and "the network
   * hiccuped" (keep) — must be told apart with a HEAD probe.
   */
  describe('dangling selection self-heal', () => {
    function errorHandler(src: string): string {
      const m = src.match(/async function onWallpaperError\(\)[\s\S]*?\n\}/)
      if (!m) throw new Error('onWallpaperError not found in App.vue')
      return m[0]
    }

    it('probes with HEAD, which is the only way to learn the status', () => {
      // A bare @error handler cannot distinguish 404 from a transport failure.
      expect(errorHandler(readWebFile(APP))).toMatch(/method:\s*'HEAD'/)
    })

    it('clears the local selection only on a 404', () => {
      const handler = errorHandler(readWebFile(APP))
      expect(handler).toMatch(/status\s*!==\s*404/)
      expect(handler).toContain("setSetting('wallpaperLocalSelected', '')")
    })

    it('keeps the selection on a transport failure', () => {
      // A thrown fetch (offline, DNS) must fall through to the catch without
      // clearing: the file may well still exist, and clearing would silently
      // lose the user's choice.
      const handler = errorHandler(readWebFile(APP))
      const catchBlock = handler.slice(handler.indexOf('catch'))
      expect(catchBlock).not.toContain("setSetting('wallpaperLocalSelected'")
    })

    it('heals only the local source, never the Bing cache', () => {
      // A missing Bing file is a server-side cache problem, not a stale pointer
      // on this device — clearing the local selection there would discard an
      // unrelated choice.
      expect(errorHandler(readWebFile(APP))).toMatch(/wallpaperMode\.value\s*!==\s*'local'/)
    })

    it('does not re-enter while a probe is in flight', () => {
      // Without the guard, an <img> that keeps failing re-probes on every
      // re-render and hammers the endpoint.
      const handler = errorHandler(readWebFile(APP))
      expect(handler).toMatch(/if\s*\(!url\s*\|\|\s*healingDanglingSelection\)\s*return/)
    })
  })
})
