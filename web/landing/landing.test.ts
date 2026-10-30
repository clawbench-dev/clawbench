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

/** Reads width/height out of a WebP container without an image library. */
function webpSize(buf: Buffer): { width: number; height: number } {
  if (buf.toString('ascii', 0, 4) !== 'RIFF' || buf.toString('ascii', 8, 12) !== 'WEBP') {
    throw new Error('not a WebP file')
  }
  const chunk = buf.toString('ascii', 12, 16)
  if (chunk === 'VP8X') {
    // 4 bytes flags/reserved, then 3-byte little-endian (width-1), (height-1)
    return {
      width: 1 + (buf[24] | (buf[25] << 8) | (buf[26] << 16)),
      height: 1 + (buf[27] | (buf[28] << 8) | (buf[29] << 16)),
    }
  }
  if (chunk === 'VP8 ') {
    // Lossy: sync code at 23..25, then 14-bit width/height at 26..29
    return {
      width: (buf[26] | (buf[27] << 8)) & 0x3fff,
      height: (buf[28] | (buf[29] << 8)) & 0x3fff,
    }
  }
  if (chunk === 'VP8L') {
    // Lossless: 14 bits each, packed after a 0x2f signature byte
    const b = buf.readUInt32LE(21)
    return {
      width: (b & 0x3fff) + 1,
      height: ((b >> 14) & 0x3fff) + 1,
    }
  }
  throw new Error(`unsupported WebP chunk: ${chunk}`)
}

/** Boots the page with the script executed, at the given navigator language. */
function boot(language: string, fetchImpl?: unknown): JSDOM {
  const dom = new JSDOM(html, {
    url: 'https://clawbench.example/',
    runScripts: 'outside-only',
  })
  const { window } = dom

  // jsdom has no fetch. The download buttons upgrade themselves to direct
  // asset URLs from this call, so the tests that exercise that path inject a
  // stub; every other test leaves it undefined and the page skips the upgrade.
  if (fetchImpl) {
    Object.defineProperty(window, 'fetch', { value: fetchImpl, configurable: true, writable: true })
  }

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

    it('sizes each device screen to its screenshot, so cover crops nothing', () => {
      // Both frames are `object-fit: cover`. When the frame ratio differs from
      // the image ratio, the excess is cropped — silently. The phone lost its
      // whole bottom dock that way (560/1111 vs an actual 2.2), and the
      // monitor lost a few px off the top title bar and bottom status bar.
      // Reading the real image dimensions is the only way to catch it: a
      // hand-written ratio looks perfectly fine in review.
      const cases = [
        { frame: /\.device-screen\s*\{[^}]*aspect-ratio:\s*(\d+)\s*\/\s*(\d+)/, img: 'img/desktop.webp' },
        { frame: /\.phone-screen\s*\{[^}]*aspect-ratio:\s*(\d+)\s*\/\s*(\d+)/, img: 'img/phone.webp' },
      ]

      for (const { frame, img } of cases) {
        const m = css.match(frame)
        expect(m, `no aspect-ratio found for ${img}`).toBeTruthy()
        const frameRatio = Number(m![2]) / Number(m![1])

        const bytes = readFileSync(resolve(here, img))
        // Minimal WebP VP8X/VP8 header parse: walk RIFF chunks to the image
        // payload rather than pulling in an image library for two numbers.
        const { width, height } = webpSize(bytes)
        expect(Math.abs(frameRatio - height / width), `${img} frame ${frameRatio} vs image ${height / width}`)
          .toBeLessThan(0.005)
      }
    })

    it('serves the screenshots at their native aspect', () => {
      // The pre-generated assets must keep the source ratio; downscaling must
      // not letterbox or pad them.
      expect(webpSize(readFileSync(resolve(here, 'img/desktop.webp')))).toEqual({ width: 1920, height: 1040 })
      expect(webpSize(readFileSync(resolve(here, 'img/phone.webp')))).toEqual({ width: 900, height: 1980 })
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

  describe('downloads', () => {
    // The download cards carry the asset base name; the version is appended by
    // the script. Asset names are therefore a contract with release.yml — a
    // renamed artifact leaves a button that looks fine and 404s.
    const releaseYml = readFileSync(
      resolve(here, '../../.github/workflows/release.yml'),
      'utf8',
    )

    function cards(doc: Document) {
      return Array.from(doc.querySelectorAll('a[data-asset]')).map((el) => ({
        base: el.getAttribute('data-asset') || '',
        ext: el.getAttribute('data-ext') || 'zip',
        href: el.getAttribute('href') || '',
        arch: (el.textContent || '').trim(),
        icon: (el.closest('.dl-card')?.querySelector('use')?.getAttribute('href') || '').replace(
          /^#/,
          '',
        ),
      }))
    }

    /** The platform is spelled into the artifact name; the glyph must follow it. */
    function expectedIcon(base: string): string {
      if (base.includes('windows')) return 'i-win'
      if (base.includes('darwin')) return 'i-mac'
      if (base.includes('linux')) return 'i-linux'
      if (base.includes('android')) return 'i-android'
      throw new Error(`artifact ${base} names no known platform`)
    }

    it('gives every card the icon of its platform', () => {
      // Copy-pasting a card and forgetting to swap the <use> reference is
      // invisible in review but ships a Windows glyph on the macOS row.
      const dom = new JSDOM(html)
      const doc = dom.window.document
      const symbols = new Set(Array.from(doc.querySelectorAll('symbol')).map((el) => el.id))

      for (const { base, icon } of cards(doc)) {
        expect(symbols.has(icon), `${base} references a missing symbol "${icon}"`).toBe(true)
        expect(icon, `${base} has the wrong platform icon`).toBe(expectedIcon(base))
      }
      dom.window.close()
    })

    it('keeps every architecture of one platform in the same card', () => {
      // One card per platform: macOS is two chips (Apple Silicon / Intel), not
      // two cards that look nearly identical.
      const dom = new JSDOM(html)
      const doc = dom.window.document
      const layout = Array.from(doc.querySelectorAll('.dl-card')).map((card) => ({
        platform: (card.querySelector('.dl-name')?.textContent || '').trim(),
        arches: Array.from(card.querySelectorAll('a[data-asset]')).map((a) =>
          (a.textContent || '').trim(),
        ),
      }))

      expect(layout.length).toBe(8)
      expect(layout.map((c) => `${c.platform}: ${c.arches.join('+')}`)).toEqual([
        'Windows: x64',
        'macOS: Apple Silicon+Intel',
        'Linux: x64+ARM64',
        'Android: arm64',
        'Windows: x64',
        'macOS: Apple Silicon+Intel',
        'Linux: x64+ARM64',
        'Android · Termux: arm64',
      ])
      dom.window.close()
    })

    it('keeps the packages behind the third deploy tab', () => {
      // Downloads live inside 部署 → 安装包, after npm and Docker: the tab
      // order is the install-path order, and the panel starts hidden so the
      // first thing a visitor sees is still the one-line npm command.
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const labels = Array.from(doc.querySelectorAll('.tabs .tab')).map((t) => t.getAttribute('data-tab'))
      expect(labels).toEqual(['npm', 'docker', 'binary'])

      const panel = doc.querySelector('.dl-panel[data-panel="binary"]') as HTMLElement
      expect(panel.hidden).toBe(true)
      ;(doc.querySelector('.tab[data-tab="binary"]') as HTMLElement).click()
      expect(panel.hidden).toBe(false)
      expect((doc.querySelector('[data-panel="npm"]') as HTMLElement).hidden).toBe(true)
      expect(panel.querySelectorAll('a[data-asset]').length).toBe(12)
      dom.window.close()
    })

    it('jumps straight to the package tab from the desktop section', () => {
      // The desktop block's "download the desktop app" link must land on the
      // cards, not on the npm command.
      const dom = boot('zh-CN')
      const doc = dom.window.document
      ;(doc.querySelector('[data-open-tab="binary"]') as HTMLElement).click()
      expect((doc.querySelector('.tab[data-tab="binary"]') as HTMLElement).getAttribute('aria-selected')).toBe('true')
      expect((doc.querySelector('.dl-panel[data-panel="binary"]') as HTMLElement).hidden).toBe(false)
      dom.window.close()
    })

    it('runs the server through npx, with a comment on every command', () => {
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const panel = doc.querySelector('.code-panel[data-panel="npm"]')!
      const code = panel.querySelector('code')!.textContent || ''

      expect(code).toContain('npx @xulongzhe/clawbench')
      expect(code).not.toContain('npm install -g')

      const commands = code.split('\n').filter((l) => l.trim() && !l.trim().startsWith('#'))
      expect(panel.querySelectorAll('.c-cmt').length).toBe(commands.length)
      dom.window.close()
    })

    it('keeps the mirror line out of the English page and its copy payload', () => {
      // The npm mirror only matters to users in mainland China; an English
      // visitor should neither see it nor paste it.
      const zh = boot('zh-CN')
      const zhPanel = zh.window.document.querySelector('.code-panel[data-panel="npm"]')!
      expect(zhPanel.querySelector('.zh-only')).toBeTruthy()
      expect(zhPanel.querySelector('.copy-btn')!.getAttribute('data-copy')).toContain(
        'npm config set registry',
      )

      const en = boot('en-US')
      const enPanel = en.window.document.querySelector('.code-panel[data-panel="npm"]')!
      expect(enPanel.querySelector('.copy-btn')!.getAttribute('data-copy')).toBe(
        'npx @xulongzhe/clawbench',
      )
      // jsdom does not resolve the cascade, so the hiding rule needs its own guard
      expect(css).toMatch(/\.lang-en\s+\.zh-only\s*\{[^}]*display:\s*none/)

      zh.window.close()
      en.window.close()
    })

    it('offers exactly the artifacts release.yml publishes', () => {
      const dom = new JSDOM(html)
      const assets = cards(dom.window.document)
      expect(assets.length).toBe(12)
      expect(new Set(assets.map((a) => a.base)).size).toBe(assets.length)

      for (const { base, ext } of assets) {
        // PowerShell jobs interpolate `$env:RELEASE_TAG`, bash ones
        // `${RELEASE_TAG}` — both are the same published filename.
        const published = new RegExp(
          `${base}-(?:\\$\\{RELEASE_TAG\\}|\\$env:RELEASE_TAG)\\.${ext}`,
        )
        expect(releaseYml, `${base}.${ext} is not uploaded by release.yml`).toMatch(published)
      }
      dom.window.close()
    })

    it('points at the Releases page until the version is known', () => {
      const dom = boot('zh-CN')
      const hrefs = cards(dom.window.document).map((a) => a.href)
      expect(hrefs).toEqual(
        Array(hrefs.length).fill('https://github.com/clawbench-dev/clawbench/releases/latest'),
      )
      dom.window.close()
    })

    it('rewrites every button to a direct download once the tag arrives', async () => {
      const dom = boot('zh-CN', () =>
        Promise.resolve({ ok: true, json: () => Promise.resolve({ tag_name: 'v9.9.9' }) }),
      )
      await new Promise((r) => setTimeout(r, 0))

      const hrefs = cards(dom.window.document).map((a) => a.href)
      const prefix = 'https://github.com/clawbench-dev/clawbench/releases/download/v9.9.9/'
      expect(hrefs).toContain(`${prefix}clawbench-desktop-linux-x64-v9.9.9.zip`)
      expect(hrefs).toContain(`${prefix}clawbench-android-v9.9.9.apk`)
      expect(hrefs.filter((h) => !h.startsWith(prefix))).toEqual([])
      dom.window.close()
    })

    it('keeps the Releases page when the lookup fails', async () => {
      // Offline, rate-limited (60 req/h per IP) and a non-2xx answer are all
      // the same outcome for the visitor: the fallback link must survive.
      const failing = [
        () => Promise.reject(new Error('offline')),
        () => Promise.resolve({ ok: false, json: () => Promise.resolve(null) }),
        () => Promise.resolve({ ok: true, json: () => Promise.resolve({}) }),
      ]

      for (const impl of failing) {
        const dom = boot('zh-CN', impl)
        await new Promise((r) => setTimeout(r, 0))
        const hrefs = cards(dom.window.document).map((a) => a.href)
        expect(hrefs.every((h) => h.endsWith('/releases/latest'))).toBe(true)
        dom.window.close()
      }
    })
  })

  describe('markup contracts', () => {
    it('inlines every agent logo instead of linking an <img>', () => {
      // `currentColor` inside an <img>-loaded SVG resolves against the image's
      // own document, where there is no inherited colour — the marks render
      // black on a near-black page. Inlining is the only way to tint them.
      // Counted per item so a dropped logo fails rather than silently leaving
      // a bare name.
      const dom = new JSDOM(html)
      const doc = dom.window.document
      const items = Array.from(doc.querySelectorAll('.strip-list li:not(.more)'))

      // 19 = the backend registry (internal/ai/backends) minus Qoder CN,
      // which is the same product as Qoder and reads as a duplicate here.
      // Bumping this number is the reminder to add a logo when one is added.
      expect(items.length).toBe(19)
      for (const li of items) {
        const svg = li.querySelector('svg.strip-logo')
        expect(svg, `no inline logo for "${li.textContent?.trim()}"`).toBeTruthy()
        expect(svg!.getAttribute('fill')).toBe('currentColor')
        // Decorative: the name is already next to it, so an aria-label would
        // just make screen readers announce the agent twice.
        expect(svg!.getAttribute('aria-hidden')).toBe('true')
        expect(li.querySelector('img')).toBeNull()
      }
      dom.window.close()
    })

    it('gives every inlined SVG a unique id', () => {
      // Colour logos carry <linearGradient id="..."> and reference it with
      // url(#...). Inlined into one document, two copies of the same id make
      // the later definition win, so an icon silently paints with another
      // brand's gradient. Ids are prefixed per agent to prevent that.
      const dom = new JSDOM(html)
      const doc = dom.window.document
      const ids = Array.from(doc.querySelectorAll('[id]')).map((el) => el.id)
      const dupes = ids.filter((id, i) => ids.indexOf(id) !== i)
      expect(dupes).toEqual([])
      dom.window.close()
    })

    it('pairs every hover colour layer with a mono layer', () => {
      // The two layers are stacked; a colour layer without its mono base
      // would leave a hole in the row until the pointer arrives.
      const dom = new JSDOM(html)
      const doc = dom.window.document
      const items = Array.from(doc.querySelectorAll('.strip-list li'))
      expect(items.length).toBe(19)
      for (const li of items) {
        expect(li.querySelector('svg.strip-logo-mono'), li.textContent?.trim()).toBeTruthy()
        const colour = li.querySelector('svg.strip-logo-color')
        if (colour) {
          // Colour layers must not be recoloured by the surrounding text.
          expect(colour.getAttribute('fill')).toBeNull()
        }
      }
      dom.window.close()
    })

    it('keeps fill-rule on the logos whose source declares it', () => {
      // Dropping `fill-rule="evenodd"` from the root flips which regions of a
      // compound path are filled. OpenCode's mark turns from a thick frame
      // into a solid block (or the reverse) with no other symptom.
      //
      // Not every logo declares it (VeCLI and Command Code ship without), so
      // the guard is "preserved where present", checked against the source
      // files rather than a blanket assertion that would fail on those two.
      const withEvenOdd = ['opencode', 'cursor', 'grok', 'pi', 'hermesagent', 'zhipu']
      for (const slug of withEvenOdd) {
        const src = resolve(here, '../../node_modules/@lobehub/icons-static-svg/icons', slug + '.svg')
        if (!existsSync(src)) continue
        expect(readFileSync(src, 'utf8'), slug).toMatch(/fill-rule="evenodd"/)
      }

      // The inlined copies must still carry it. Counted on the mono layer
      // only — each agent also has an optional colour layer for hover, so
      // `svg.strip-logo` alone would double-count.
      const dom = new JSDOM(html)
      const doc = dom.window.document
      const logos = Array.from(doc.querySelectorAll('svg.strip-logo-mono'))
      expect(logos.length).toBe(19)
      const kept = logos.filter((svg) => svg.getAttribute('fill-rule') === 'evenodd')
      expect(kept.length).toBeGreaterThanOrEqual(15)
      dom.window.close()
    })

    it('lists every IM backend the product supports', () => {
      // WeChat arrived after DingTalk and Feishu; a card that still names only
      // two makes the page claim less than the product does.
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const card = Array.from(doc.querySelectorAll('.feat')).find((el) =>
        el.querySelector('[data-i18n="feat.notify.d"]'),
      )!
      const text = card.textContent || ''
      for (const im of ['钉钉', '飞书', '微信']) expect(text, im).toContain(im)
      dom.window.close()
    })

    it('gives live screen share its own full-width card', () => {
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const card = doc.querySelector('.feat-wide')
      expect(card).toBeTruthy()
      expect(card!.querySelectorAll('.feat-tags li').length).toBe(3)
      // 13 cards in a 3-column grid would leave a lone card on the last row;
      // the wide card is what keeps every row full.
      expect(css).toMatch(/\.feat-wide\s*\{[^}]*grid-column:\s*1\s*\/\s*-1/)
      dom.window.close()
    })

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

    it('pauses over a card but keeps scrolling in the gaps between them', () => {
      // Pausing is bound to the individual card, not the strip. The strip is
      // ~1344x539 on a desktop viewport — binding it to the container means
      // any pointer resting mid-page freezes the loop, which reads as "the
      // marquee never started".
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const gal = doc.getElementById('gal') as HTMLElement
      const card = gal.querySelector('.gal-item') as HTMLElement

      card.dispatchEvent(new dom.window.MouseEvent('pointermove', { bubbles: true }))
      expect(gal.classList.contains('is-paused')).toBe(true)

      // Pointer over the strip itself (the gap below the cards) must resume.
      gal.dispatchEvent(new dom.window.MouseEvent('pointermove', { bubbles: true }))
      expect(gal.classList.contains('is-paused')).toBe(false)

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
