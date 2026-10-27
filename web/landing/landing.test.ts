/**
 * Guards for the standalone landing page (`web/landing/`).
 *
 * These are not style assertions — each one covers a defect class that the
 * page actually shipped with while being built by eye:
 *
 *   - a `data-i18n` key with no English entry silently renders Chinese text
 *     inside an English page (no error, no console warning);
 *   - `img { display:block }` overrides the UA `[hidden] { display:none }`,
 *     so both language variants of the architecture diagram rendered at once;
 *   - a renamed image leaves a `<img>` pointing at a 404, which on a dark
 *     background looks like an intentionally empty card.
 *
 * The page is loaded into a real DOM and its script executed, so these check
 * observable output rather than the source text.
 */
import { describe, expect, it, beforeAll } from 'vitest'
import { readFileSync, existsSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { JSDOM } from 'jsdom'

const here = dirname(fileURLToPath(import.meta.url))
const html = readFileSync(resolve(here, 'index.html'), 'utf8')
const script = readFileSync(resolve(here, 'js/landing.js'), 'utf8')
const css = readFileSync(resolve(here, 'css/landing.css'), 'utf8')

const CJK = /[\u3400-\u9fff]/

/** Boots the page with the script executed, at the given navigator language. */
function boot(language: string): JSDOM {
  const dom = new JSDOM(html, {
    url: 'https://clawbench.example/',
    runScripts: 'outside-only',
  })
  const { window } = dom

  // jsdom implements neither of these, and the page uses both. Without the
  // stub the script throws on its first matchMedia call and every assertion
  // below would pass vacuously against untranslated markup.
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia

  // The gallery auto-scroll loop and its visibility guard use these. jsdom has
  // neither, so without stubs the script throws partway through and every
  // assertion after that point would silently test nothing.
  const raf = (cb: FrameRequestCallback) => window.setTimeout(() => cb(0), 16)
  window.requestAnimationFrame = raf as unknown as typeof window.requestAnimationFrame
  window.cancelAnimationFrame = ((id: number) => window.clearTimeout(id)) as unknown as typeof window.cancelAnimationFrame

  class IO {
    constructor(private cb: (entries: { isIntersecting: boolean; target: Element }[]) => void) {}
    observe(el: Element) {
      this.cb([{ isIntersecting: true, target: el }])
    }
    unobserve() {}
    disconnect() {}
  }
  window.IntersectionObserver = IO as unknown as typeof window.IntersectionObserver

  Object.defineProperty(window.navigator, 'language', {
    value: language,
    configurable: true,
  })

  window.eval(script)
  return dom
}

/** Elements whose visible text should follow the active locale. */
function translatableTexts(doc: Document): string[] {
  const sel = '[data-i18n], [data-i18n-html]'
  return Array.from(doc.querySelectorAll(sel)).map((el) => el.textContent || '')
}

describe('landing page', () => {
  describe('localisation', () => {
    it('renders no Chinese text when the browser language is English', () => {
      const dom = boot('en-US')
      const doc = dom.window.document

      expect(doc.documentElement.dataset.locale).toBe('en')

      const leaked = Array.from(doc.querySelectorAll('[data-i18n], [data-i18n-html]'))
        .filter((el) => CJK.test(el.textContent || ''))
        .map((el) => `${el.getAttribute('data-i18n') || el.getAttribute('data-i18n-html')}: ${el.textContent?.slice(0, 40)}`)

      expect(leaked).toEqual([])
      dom.window.close()
    })

    it('translates the document title and description', () => {
      const dom = boot('en-US')
      const doc = dom.window.document
      expect(doc.title).not.toMatch(CJK)
      expect(doc.querySelector('meta[name="description"]')?.getAttribute('content')).not.toMatch(CJK)
      dom.window.close()
    })

    it('translates aria-labels, not just visible text', () => {
      // Screen-reader users hear the aria-label, so a Chinese label on an
      // English page is just as wrong as visible Chinese text — and the text
      // sweep above cannot see it.
      const dom = boot('en-US')
      const doc = dom.window.document
      const chinese = Array.from(doc.querySelectorAll('[aria-label]'))
        .map((el) => el.getAttribute('aria-label') || '')
        .filter((v) => CJK.test(v))
      expect(chinese).toEqual([])
      dom.window.close()
    })

    it('keeps Chinese as the default for a Chinese browser', () => {
      const dom = boot('zh-CN')
      const doc = dom.window.document
      expect(doc.documentElement.dataset.locale).toBe('zh')
      expect(translatableTexts(doc).some((t) => CJK.test(t))).toBe(true)
      dom.window.close()
    })

    it('restores the original Chinese after toggling back', () => {
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const before = translatableTexts(doc)

      const toggle = doc.getElementById('langToggle') as HTMLElement
      toggle.click()
      expect(doc.documentElement.dataset.locale).toBe('en')

      toggle.click()
      expect(doc.documentElement.dataset.locale).toBe('zh')
      // Comparing the whole list catches a partial restore, e.g. one element
      // left holding its English value.
      expect(translatableTexts(doc)).toEqual(before)
      dom.window.close()
    })

    it('shows exactly one architecture diagram per language', () => {
      for (const [lang, visible] of [['zh-CN', 'zh'], ['en-US', 'en']] as const) {
        const dom = boot(lang)
        const doc = dom.window.document
        const shown = Array.from(doc.querySelectorAll('.arch img')).filter(
          (el) => !(el as HTMLElement).hidden,
        )
        expect(shown.map((el) => (el.classList.contains('arch-en') ? 'en' : 'zh'))).toEqual([visible])
        dom.window.close()
      }
    })

    it('forces [hidden] to win over the author-level img rule', () => {
      // jsdom does not resolve the cascade, so the test above passes even
      // without this rule — it only checks the attribute. In a browser the
      // author-level `img { display: block }` beats the UA sheet's
      // `[hidden] { display: none }`, and both language variants of the
      // architecture diagram render at once. Only a source guard can catch it.
      expect(css).toMatch(/\[hidden\]\s*\{[^}]*display:\s*none\s*!important/)
    })
  })

  describe('assets', () => {
    it('references only images that exist', () => {
      const dom = new JSDOM(html)
      const doc = dom.window.document
      const refs = Array.from(doc.querySelectorAll('img[src]'))
        .map((el) => el.getAttribute('src') || '')
        .filter((src) => !/^(https?:)?\/\//.test(src))

      expect(refs.length).toBeGreaterThan(0)
      const missing = refs.filter((src) => !existsSync(resolve(here, src)))
      expect(missing).toEqual([])
    })

    it('gives every gallery card a caption', () => {
      const dom = new JSDOM(html)
      const items = Array.from(dom.window.document.querySelectorAll('.gal-item'))
      expect(items.length).toBeGreaterThanOrEqual(6)
      for (const item of items) {
        expect(item.querySelector('figcaption')?.textContent?.trim()).toBeTruthy()
      }
    })

    it('leaves no img with an empty or missing src', () => {
      // `src=""` resolves to the page URL, so the browser downloads the whole
      // document again as an image. The lightbox placeholder is the easy place
      // to introduce it; the fix is to omit the attribute until it is opened.
      // Asserting "attribute absent" rather than "value falsy" on purpose —
      // src="" and no-src both read as empty, but only the latter is correct.
      const dom = new JSDOM(html)
      const doc = dom.window.document
      const withEmptySrc = Array.from(doc.querySelectorAll('img')).filter(
        (el) => el.hasAttribute('src') && !el.getAttribute('src'),
      )
      expect(withEmptySrc.map((el) => el.id || el.className)).toEqual([])

      // The one img without a src is the lightbox placeholder, and only while
      // it is closed.
      const noSrc = Array.from(doc.querySelectorAll('img')).filter((el) => !el.hasAttribute('src'))
      expect(noSrc.map((el) => el.id)).toEqual(['lightboxImg'])
      expect((doc.getElementById('lightbox') as HTMLElement).hidden).toBe(true)
    })
  })

  describe('markup contracts', () => {
    it('declares no inline event handlers, which a strict CSP would block', () => {
      const dom = new JSDOM(html)
      const withHandlers = Array.from(dom.window.document.querySelectorAll('*')).filter((el) =>
        Array.from(el.attributes).some((a) => a.name.startsWith('on')),
      )
      expect(withHandlers.map((el) => el.tagName)).toEqual([])
    })

    it('uses only ids that the script actually queries', () => {
      // A renamed id silently disables its feature: getElementById returns
      // null and the corresponding handler block is skipped without error.
      const dom = new JSDOM(html)
      const doc = dom.window.document
      for (const id of ['nav', 'burger', 'mobileMenu', 'langToggle', 'toast', 'gal', 'lightbox', 'lightboxImg']) {
        expect(doc.getElementById(id), `missing #${id}`).toBeTruthy()
      }
    })

    it('opens the desktop screenshot in a lightbox', () => {
      // On a phone the wide screenshot collapses to a ~70px band, so the
      // only way to see the UI is to open it. This drives the real handler.
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const box = doc.getElementById('lightbox') as HTMLElement
      expect(box.hidden).toBe(true)

      ;(doc.querySelector('.desk-shot .shot-zoom') as HTMLElement).click()
      expect(box.hidden).toBe(false)
      expect(doc.getElementById('lightboxImg')?.getAttribute('src')).toBe('img/desktop.webp')
      expect(doc.body.style.overflow).toBe('hidden')

      ;(box.querySelector('.lightbox-close') as HTMLElement).click()
      expect(box.hidden).toBe(true)
      expect(doc.body.style.overflow).toBe('')
      dom.window.close()
    })

    it('closes the lightbox on Escape but not when clicking the image', () => {
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const box = doc.getElementById('lightbox') as HTMLElement
      const img = doc.getElementById('lightboxImg') as HTMLElement

      ;(doc.querySelector('.desk-shot .shot-zoom') as HTMLElement).click()
      img.dispatchEvent(new dom.window.MouseEvent('click', { bubbles: true }))
      expect(box.hidden).toBe(false)

      doc.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'Escape' }))
      expect(box.hidden).toBe(true)
      dom.window.close()
    })

    it('closes the lightbox when the backdrop is clicked', () => {
      // Separate from the image-click case on purpose: "click image keeps it
      // open" passes even if backdrop-click is broken, so the two directions
      // need their own assertions.
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const box = doc.getElementById('lightbox') as HTMLElement

      ;(doc.querySelector('.desk-shot .shot-zoom') as HTMLElement).click()
      box.dispatchEvent(new dom.window.MouseEvent('click', { bubbles: true }))
      expect(box.hidden).toBe(true)
      dom.window.close()
    })

    it('opens each screenshot with its own source', () => {
      // Every zoomable image must feed its own src into the lightbox. Sharing
      // one handler is easy to get wrong by hard-coding the desktop path,
      // which would look correct until someone opens the phone shot.
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const box = doc.getElementById('lightbox') as HTMLElement
      const src = () => doc.getElementById('lightboxImg')?.getAttribute('src')

      const buttons = Array.from(doc.querySelectorAll<HTMLElement>('[data-zoom]'))
      expect(buttons.length).toBeGreaterThanOrEqual(2)

      for (const btn of buttons) {
        btn.click()
        expect(src()).toBe(btn.dataset.zoom)
        expect(box.hidden).toBe(false)
        ;(box.querySelector('.lightbox-close') as HTMLElement).click()
      }
      dom.window.close()
    })
  })

  describe('gallery auto-scroll', () => {
    /** Width of one full set, measured the same way the page does. */
    function setWidth(doc: Document): number {
      const gal = doc.getElementById('gal') as HTMLElement
      const half = gal.children.length / 2
      const first = (gal.children[0] as HTMLElement).offsetLeft
      const clone = (gal.children[half] as HTMLElement).offsetLeft
      return clone - first
    }

    it('duplicates the cards so the loop has no visible seam', () => {
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const gal = doc.getElementById('gal') as HTMLElement
      const originals = doc.querySelectorAll('.gal-item:not(.is-clone)').length

      expect(originals).toBeGreaterThanOrEqual(6)
      expect(gal.children.length).toBe(originals * 2)
      dom.window.close()
    })

    it('hides the duplicate set from assistive tech', () => {
      // The clones exist only to bridge the loop. Announcing every screenshot
      // twice, with duplicated captions, is noise for screen readers.
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const clones = Array.from(doc.querySelectorAll('.gal-item.is-clone'))
      expect(clones.length).toBeGreaterThan(0)
      for (const clone of clones) {
        expect(clone.getAttribute('aria-hidden')).toBe('true')
        expect(clone.querySelector('img')?.getAttribute('alt')).toBe('')
      }
      dom.window.close()
    })

    it('translates the cloned captions too', () => {
      // Cloning has to happen before the i18n snapshot is taken. If it happens
      // after, the second half of the strip keeps whichever language was
      // rendered first — visible as Chinese captions on an English page.
      const dom = boot('en-US')
      const doc = dom.window.document
      const captions = Array.from(doc.querySelectorAll('.gal-item figcaption'))
      expect(captions.length).toBeGreaterThan(6)
      expect(captions.filter((c) => CJK.test(c.textContent || ''))).toEqual([])
      dom.window.close()
    })

    it('pauses on hover and resumes on leave', () => {
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const gal = doc.getElementById('gal') as HTMLElement

      gal.dispatchEvent(new dom.window.MouseEvent('pointerenter', { bubbles: false }))
      expect(gal.classList.contains('is-paused')).toBe(true)

      gal.dispatchEvent(new dom.window.MouseEvent('pointerleave', { bubbles: false }))
      expect(gal.classList.contains('is-paused')).toBe(false)
      dom.window.close()
    })

    it('disables scroll-snap while scrolling and restores it on pause', () => {
      // `scroll-snap-type: mandatory` fights a per-frame scrollLeft write:
      // the browser snaps back to the nearest card every frame, so the strip
      // advances in visible jerks. Snap must only apply while paused.
      expect(css).toMatch(/\.gal\s*\{[^}]*scroll-snap-type:\s*none/)
      expect(css).toMatch(/\.gal\.is-paused\s*\{[^}]*scroll-snap-type:\s*x\s+proximity/)
    })

    it('does not auto-scroll when reduced motion is requested', () => {
      // The loop is started from the same script; the guard has to be inside
      // it, not just in CSS, because CSS cannot stop a rAF loop.
      expect(script).toMatch(/prefers-reduced-motion:\s*reduce/)
      expect(script).toMatch(/if\s*\(galReduced\s*\|\|\s*galRaf\)\s*return/)
    })
  })
})
