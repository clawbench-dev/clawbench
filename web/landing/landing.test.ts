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

function jpegSize(buf: Buffer): { width: number; height: number } {
  if (buf[0] !== 0xff || buf[1] !== 0xd8) throw new Error('not a JPEG file')
  let i = 2
  while (i + 9 < buf.length) {
    if (buf[i] !== 0xff) {
      i++
      continue
    }
    const marker = buf[i + 1]
    // SOF0/1/2 and friends are the only markers carrying the frame size;
    // 0xC4/0xC8/0xCC sit in the same range but are Huffman/arithmetic tables.
    if (marker >= 0xc0 && marker <= 0xcf && marker !== 0xc4 && marker !== 0xc8 && marker !== 0xcc) {
      return { width: buf.readUInt16BE(i + 7), height: buf.readUInt16BE(i + 5) }
    }
    // Standalone markers have no length field.
    if (marker === 0x01 || marker === 0xd8 || marker === 0xd9 || (marker >= 0xd0 && marker <= 0xd7)) {
      i += 2
      continue
    }
    i += 2 + buf.readUInt16BE(i + 2)
  }
  throw new Error('no SOF marker found')
}

/** Boots the page with the script executed, at the given navigator language. */
function boot(language: string, fetchImpl?: unknown, userAgent?: string, plat?: string): JSDOM {
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

  // 平台高亮按 UA 猜，脚本在 eval 时就要读到正确的值，所以必须在这之前设好。
  if (userAgent) {
    Object.defineProperty(window.navigator, 'platform', { value: plat, configurable: true })
    Object.defineProperty(window.navigator, 'userAgent', { value: userAgent, configurable: true })
  }

  // 页面靠 scrollIntoView 把用户带到结果上。jsdom 没有这个方法，若不打桩，
  // 「滚到哪一步」这件事在测试里完全不可见——改错了也照样绿。
  const scrolledTo: string[] = []
  ;(window as unknown as { __scrolledTo: string[] }).__scrolledTo = scrolledTo
  window.Element.prototype.scrollIntoView = function (this: Element) {
    scrolledTo.push(this.id || this.className)
  }

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

    it('keeps the zoom sheen inside the phone frame corners', () => {
      // .shot-zoom::after paints a square overlay with border-radius: inherit,
      // but the button itself has no radius — so the sheen stuck out past the
      // phone's rounded corners. It has to match .showcase-shell's radius.
      expect(css).toMatch(/\.phone-zoom::after\s*\{[^}]*border-radius:\s*34px/)
      expect(css).toMatch(/\.showcase-shell\s*\{[^}]*border-radius:\s*34px/)
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

  describe('seo', () => {
    // The page is published to two hosts: Cloudflare Pages (primary) and
    // GitHub Pages (a project-page copy of the same bytes). Everything here is
    // about telling crawlers which one is the real one.
    const CANONICAL = 'https://clawbench.pages.dev/'

    it('declares the Cloudflare host as canonical', () => {
      const doc = new JSDOM(html).window.document
      expect(doc.querySelector('link[rel="canonical"]')?.getAttribute('href')).toBe(CANONICAL)
    })

    it('points og:url at the same URL as the canonical link', () => {
      // Disagreement here makes social crawlers treat the two hosts as two
      // distinct pages, which is the exact split the canonical link prevents.
      const doc = new JSDOM(html).window.document
      expect(doc.querySelector('meta[property="og:url"]')?.getAttribute('content')).toBe(CANONICAL)
    })

    it('gives social crawlers an absolute og:image that exists', () => {
      // A relative path here resolves against nothing: og:image is read by the
      // crawler, not the browser, and it does not honour <base>. The result is
      // a share card with no picture and no error anywhere.
      const doc = new JSDOM(html).window.document
      const content = doc.querySelector('meta[property="og:image"]')?.getAttribute('content') || ''

      expect(content.startsWith(CANONICAL), `og:image is not on the canonical host: ${content}`).toBe(true)
      expect(existsSync(resolve(here, content.slice(CANONICAL.length))), content).toBe(true)
    })

    it('ships the 1200×630 card and declares that size to crawlers', () => {
      // 1200×630 is the native 1.91:1 card every platform renders without
      // re-cropping. A different ratio gets centre-cropped by the feed, which
      // is how a logo loses its left edge. The declared width/height must match
      // the real pixels: when they disagree, crawlers drop the image instead of
      // laying out the card.
      const doc = new JSDOM(html).window.document
      const size = jpegSize(readFileSync(resolve(here, 'img/og-2026-11.jpg')))

      expect(size).toEqual({ width: 1200, height: 630 })
      expect(doc.querySelector('meta[property="og:image:width"]')?.getAttribute('content')).toBe(
        String(size.width),
      )
      expect(doc.querySelector('meta[property="og:image:height"]')?.getAttribute('content')).toBe(
        String(size.height),
      )
    })

    it('ships a robots.txt that advertises the sitemap', () => {
      const robots = readFileSync(resolve(here, 'robots.txt'), 'utf8')

      expect(robots).toMatch(/^User-agent: \*$/m)
      expect(robots).toMatch(/^Allow: \/$/m)
      // `Disallow: /` is a single line that removes the site from every index
      // without a single error message; it must never appear by accident.
      expect(robots).not.toMatch(/^Disallow:\s*\/\s*$/m)
      // The Sitemap line must name the same host as the canonical URL, or
      // crawlers discard it.
      expect(robots).toMatch(new RegExp(`^Sitemap: ${CANONICAL}sitemap\\.xml$`, 'm'))
    })

    it('lists exactly the canonical URL in the sitemap', () => {
      const xml = readFileSync(resolve(here, 'sitemap.xml'), 'utf8')

      expect(xml).toMatch(/^<\?xml version="1\.0" encoding="UTF-8"\?>/)
      expect(xml).toContain('xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"')

      const locs = Array.from(xml.matchAll(/<loc>([^<]+)<\/loc>/g)).map((m) => m[1])
      // Both directions matter: a missing entry means the page is undiscovered,
      // a stale one means the sitemap points at a URL that no longer exists.
      // /docs/ 是手册站点入口（各章节页靠站内互链被发现）。
      expect(locs).toEqual([CANONICAL, `${CANONICAL}docs/`])
    })

    /** Paths handed to `cp -r` by a workflow's staging step. */
    function stagedPaths(yml: string): string[] {
      const lines = yml.split('\n')
      const out: string[] = []
      for (let i = 0; i < lines.length; i++) {
        if (!/^\s*cp -r\s/.test(lines[i])) continue
        // Join bash line continuations so a wrapped `cp` is read as one command.
        let cmd = lines[i]
        while (cmd.trimEnd().endsWith('\\')) {
          i++
          cmd = `${cmd.trimEnd().slice(0, -1)} ${lines[i]}`
        }
        out.push(...Array.from(cmd.matchAll(/web\/landing\/([\w.]+)/g)).map((m) => m[1]))
      }
      return out
    }

    it('publishes the SEO files to the canonical host only', () => {
      // Both workflows enumerate what to copy instead of `cp -r web/landing`,
      // so a new top-level file is silently left behind — it 404s in
      // production while the local file looks perfectly fine.
      const cloudflare = stagedPaths(
        readFileSync(resolve(here, '../../.github/workflows/deploy-landing.yml'), 'utf8'),
      )
      const githubPages = stagedPaths(
        readFileSync(resolve(here, '../../.github/workflows/deploy-landing-pages.yml'), 'utf8'),
      )

      expect(cloudflare.sort()).toEqual(
        ['_headers', 'css', 'img', 'index.html', 'js', 'robots.txt', 'sitemap.xml'].sort(),
      )
      // The GitHub Pages copy must not carry them: the sitemap lists the
      // Cloudflare host, and a sitemap may only contain its own host's URLs.
      expect(githubPages.sort()).toEqual(['css', 'img', 'index.html', 'js'].sort())
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

      expect(layout.length).toBe(11)
      expect(layout.map((c) => `${c.platform}: ${c.arches.join('+')}`)).toEqual([
        // 服务端（第一步的安装包页签）
        'Windows: x64',
        'macOS: Apple Silicon+Intel',
        'Linux: x64+ARM64',
        'Android · Termux: arm64',
        // 客户端（第二步）：分开装是三个桌面系统的瘦客户端
        'Windows: x64',
        'macOS: Apple Silicon+Intel',
        'Linux: x64+ARM64',
        // 整合包：三个桌面系统各一份
        'Windows · 整合包: x64',
        'macOS · 整合包: Apple Silicon+Intel',
        'Linux · 整合包: x64+ARM64',
        // 手机客户端
        'Android: arm64',
      ])
      dom.window.close()
    })

    it('keeps the packages behind the fourth deploy tab', () => {
      // Downloads live inside 部署 → 安装包, after 一键 / npm / Docker: the tab
      // order is the install-path order, and the panel starts hidden so the
      // first thing a visitor sees is the one-line installer.
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const labels = Array.from(doc.querySelectorAll('.tabs .tab')).map((t) => t.getAttribute('data-tab'))
      expect(labels).toEqual(['script', 'npm', 'docker', 'binary'])

      const panel = doc.querySelector('.dl-panel[data-panel="binary"]') as HTMLElement
      expect(panel.hidden).toBe(true)
      ;(doc.querySelector('.tab[data-tab="binary"]') as HTMLElement).click()
      expect(panel.hidden).toBe(false)
      expect((doc.querySelector('[data-panel="npm"]') as HTMLElement).hidden).toBe(true)
      // 安装包页签只留服务端产物，客户端在第二步
      expect(panel.querySelectorAll('a[data-asset]').length).toBe(6)
      dom.window.close()
    })

    it('opens on the one-line installer, with one command per OS', () => {
      // 一键脚本是第一页签，也是首屏默认面板：新用户最省事的那条路不该藏在
      // 第二个页签后面。两个系统各一块代码、各自复制，粘错平台的概率就低了。
      const dom = boot('zh-CN')
      const doc = dom.window.document
      expect((doc.querySelector('.tab.is-active') as HTMLElement).dataset.tab).toBe('script')

      const panel = doc.querySelector('[data-panel="script"]') as HTMLElement
      expect(panel.hidden).toBe(false)
      const copies = Array.from(panel.querySelectorAll('.copy-btn')).map((b) => b.getAttribute('data-copy'))
      expect(copies).toEqual([
        'curl -fsSL https://clawbench.example/install.sh | bash',
        'irm https://clawbench.example/install.ps1 | iex',
      ])
      dom.window.close()
    })

    it('says the installer tracks latest and prints the installed version', () => {
      // 脚本装 latest，两次跑的结果可以不同。页面必须如实说出来并给出固定版本的
      // 办法，否则「一键安装」会被当成可复现的安装方式，而这正是它做不到的。
      // 两个语言都要有这段（英文缺 key 会静默显示中文，见文件头的说明）。
      for (const lang of ['zh-CN', 'en-US']) {
        const dom = boot(lang)
        const doc = dom.window.document
        const note = doc.querySelector('[data-i18n-html="dep.script.note"]') as HTMLElement
        expect(note, lang).toBeTruthy()
        expect((note.parentElement as HTMLElement).hidden, lang).toBe(false)
        expect(note.querySelector('code')?.textContent, lang).toContain('CLAWBENCH_VERSION')
        expect(note.querySelector('a')?.getAttribute('href'), lang).toMatch(/\/releases$/)
        expect(note.textContent || '', lang).toMatch(/最新版|latest/)
        dom.window.close()
      }
    })

    it('points the installer commands at the host that served the page', () => {
      // 两个域名各托管一份脚本副本（Cloudflare Pages + GitHub Pages）。写死
      // Cloudflare 地址时，从 GitHub Pages 访问的人拿到的命令指向另一个
      // 域名——副本更新不同步就会装出不同版本，而且没有任何迹象。
      // 命令按 location.host 生成后，用户拿到的是「他正在看的这份」。
      const cases = [
        'https://clawbench.pages.dev/',
        'https://clawbench-dev.github.io/clawbench/',
        'http://localhost:8080/',
      ]
      for (const url of cases) {
        const dom = new JSDOM(html, { url, runScripts: 'outside-only' })
        const { window } = dom
        window.matchMedia = ((query: string) => ({
          matches: false,
          media: query,
          addEventListener() {},
          removeEventListener() {},
          addListener() {},
          removeListener() {},
          dispatchEvent: () => false,
        })) as unknown as typeof window.matchMedia
        window.requestAnimationFrame = ((cb: FrameRequestCallback) =>
          setTimeout(() => cb(0), 16)) as unknown as typeof window.requestAnimationFrame
        window.cancelAnimationFrame = ((id: number) => clearTimeout(id)) as unknown as typeof window.cancelAnimationFrame
        window.eval(script)

        const host = new URL(url).host
        const doc = window.document
        const codes = Array.from(
          doc.querySelectorAll('[data-panel="script"] .copy-btn'),
        ).map((b) => b.getAttribute('data-copy') || '')

        expect(codes.length, url).toBe(2)
        expect(codes[0], url).toMatch(new RegExp(`^curl -fsSL https?://${host}/install\\.sh \\| bash$`))
        expect(codes[1], url).toMatch(new RegExp(`^irm https?://${host}/install\\.ps1 \\| iex$`))
        // 可见的代码块与复制按钮必须是同一条命令（否则看到的和复制的对不上）
        const visible = Array.from(doc.querySelectorAll('[data-panel="script"] pre code')).map(
          (el) => (el.textContent || '').trim(),
        )
        expect(visible, url).toEqual(codes)
        window.close()
      }
    })

    it('asks for the install shape before showing any download', () => {
      // 向导的第一步是「怎么装」，不是下载：一上来就把两条路的所有内容
      // 平铺在页面上，用户得先读一遍才知道哪些与自己有关。
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const steps = Array.from(doc.querySelectorAll('#deploy .step'))

      expect(steps.map((s) => s.id)).toEqual(['step-mode', 'step-server', 'step-client'])
      // 选装法那一步是提问，不是动手的步骤，所以不带序号——否则会和
      // 「1 装客户端」撞号，两个「1」并排，看着像重复了一整步。
      expect(steps[0].querySelector('.step-no')).toBeNull()
      expect(steps[0].querySelector('.step-title')?.getAttribute('data-kicker')).toBeTruthy()
      // 分开装是两步，序号 1 / 2（默认方案是整合包，先切过去再看）
      ;(doc.querySelector('.mode-card[data-mode="split"]') as HTMLElement).click()
      expect(steps.slice(1).map((s) => s.querySelector('.step-no')?.textContent)).toEqual(['1', '2'])

      // 第一步永远可见，后面两步的显隐由所选方案决定
      expect((steps[0] as HTMLElement).hidden).toBe(false)

      const modes = Array.from(doc.querySelectorAll('.mode-card'))
      expect(modes.map((m) => m.getAttribute('data-mode'))).toEqual(['integrated', 'split'])
      for (const m of modes) {
        expect(m.getAttribute('role')).toBe('radio')
        expect(m.querySelector('.mode-name')?.textContent?.trim()).toBeTruthy()
        expect(m.querySelector('.mode-desc')?.textContent?.trim()).toBeTruthy()
        // 两张卡都要有大图标：整合包是一整块，分开装是上下两块
        // （用连线串起来，因为重点就是「两台机器分开放」）。
        // 不带图标的方案卡读起来只是两段文字。
        expect(m.querySelector('.mode-graphic svg, .mode-stack svg')).toBeTruthy()
      }
      // 整合包是推荐的默认路线
      expect(modes[0].querySelector('.mode-tag')).toBeTruthy()
      // 分开装必须是「两个独立图标块 + 中间连线」，不是一个块里画两样
      // 东西——后者读起来像一个整体，表达不出「分开放」的意思
      const stack = modes[1].querySelector('.mode-stack')
      expect(stack?.querySelectorAll('.mode-ico')).toHaveLength(2)
      expect(stack?.querySelector('.mode-link')).toBeTruthy()
      // 引导标签要跟语种走：中文「先选」，英文有自己的说法
      const kicker = steps[0].querySelector('.step-title')!
      expect(kicker.getAttribute('data-kicker')).toBe('先选')
      const domEn = boot('en-US', undefined, 'Windows NT 10.0')
      expect(
        domEn.window.document.querySelector('#step-mode .step-title')?.getAttribute('data-kicker')
      ).toBe('Choose first')
      domEn.window.close()
      dom.window.close()
    })

    it('defaults every desktop visitor to integrated and phones to split', () => {
      // 整合包覆盖三个桌面系统。手机装不了桌面壳，给手机默认选一个点下去
      // 没得下的方案，等于第一步就撞墙。
      for (const [ua, plat] of [
        ['Mozilla/5.0 (Windows NT 10.0; Win64; x64)', 'Win32'],
        ['Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', 'MacIntel'],
        ['Mozilla/5.0 (X11; Linux x86_64)', 'Linux x86_64'],
      ]) {
        const dom = boot('zh-CN', undefined, ua, plat)
        const doc = dom.window.document
        expect(
          doc.querySelector('.mode-card[data-mode="integrated"]')!.getAttribute('aria-checked'),
          ua,
        ).toBe('true')
        expect((doc.getElementById('step-server') as HTMLElement).hidden, ua).toBe(true)
        // 整合包不分服务端/客户端，第二步直接下载，序号从 1 开始
        expect((doc.getElementById('step-client') as HTMLElement).hidden, ua).toBe(false)
        expect(doc.querySelector('#step-client .step-no')!.textContent, ua).toBe('1')
        dom.window.close()
      }

      const phone = boot('zh-CN', undefined, 'Mozilla/5.0 (Linux; Android 14; Pixel 8)', 'Linux armv8l')
      const phoneDoc = phone.window.document
      expect(phoneDoc.querySelector('.mode-card[data-mode="split"]')!.getAttribute('aria-checked')).toBe(
        'true',
      )
      expect((phoneDoc.getElementById('step-server') as HTMLElement).hidden).toBe(false)
      expect(phoneDoc.querySelector('#step-client .step-no')!.textContent).toBe('2')
      phone.window.close()
    })

    it('offers the integrated package on every desktop platform, and greys it out on phones', () => {
      // 整合包覆盖 Windows / macOS / Linux：三个系统的下载都在同一块里。
      const dom = boot('zh-CN', undefined, 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)', 'Win32')
      const doc = dom.window.document
      const panel = doc.querySelector('[data-mode-panel="integrated"]')!
      expect((panel as HTMLElement).hidden).toBe(false)
      expect((doc.querySelector('[data-mode-panel="split"]') as HTMLElement).hidden).toBe(true)
      expect(Array.from(panel.querySelectorAll('a[data-asset]')).map((a) => a.getAttribute('data-asset'))).toEqual(
        [
          'clawbench-desktop-windows-x64-local',
          'clawbench-desktop-darwin-arm64-local',
          'clawbench-desktop-darwin-x64-local',
          'clawbench-desktop-linux-x64-local',
          'clawbench-desktop-linux-arm64-local',
        ],
      )
      expect(panel.classList.contains('is-unavailable')).toBe(false)
      expect((panel.querySelector('.dl-card-warn') as HTMLElement).hidden).toBe(true)
      dom.window.close()

      // 手机访客选了它：卡还在（他能看到有这东西、也能改主意），
      // 但芯片灰掉并写明原因，而不是一个点下去 404 的按钮
      const phone = boot('zh-CN', undefined, 'Mozilla/5.0 (Linux; Android 14; Pixel 8)', 'Linux armv8l')
      const phoneDoc = phone.window.document
      ;(phoneDoc.querySelector('.mode-card[data-mode="integrated"]') as HTMLElement).click()
      const phonePanel = phoneDoc.querySelector('[data-mode-panel="integrated"]')!
      expect(phonePanel.classList.contains('is-unavailable')).toBe(true)
      expect((phonePanel.querySelector('.dl-card-warn') as HTMLElement).hidden).toBe(false)
      const chip = phonePanel.querySelector('a[data-asset]')!
      expect(chip.getAttribute('aria-disabled')).toBe('true')
      // 灰掉的卡必须给一条出路，否则用户卡在死路上
      expect((phonePanel.querySelector('.dl-card-altswap') as HTMLElement).hidden).toBe(false)
      phone.window.close()
    })

    it('shows the "use a browser instead" note only in the split shape', () => {
      // 整合包方案里服务端就在用户自己机器上，不存在「浏览器连另一台服务器」。
      // 这条提示若留在整合包下，会让人以为还得去连别的机器。
      const dom = boot('zh-CN', undefined, 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)', 'Win32')
      const doc = dom.window.document
      const note = doc.querySelector('.step-note[data-mode-panel="split"]') as HTMLElement
      expect(note).toBeTruthy()
      expect(note.textContent).toContain('服务器IP')

      // Windows 默认是整合包：这条必须收起来
      expect(note.hidden).toBe(true)
      // 切到分开装：出现，并且和客户端下载卡一起在第 2 步里
      ;(doc.querySelector('.mode-card[data-mode="split"]') as HTMLElement).click()
      expect(note.hidden).toBe(false)
      // 切回整合包：再次收起，而不是一直挂在页面上
      ;(doc.querySelector('.mode-card[data-mode="integrated"]') as HTMLElement).click()
      expect(note.hidden).toBe(true)
      dom.window.close()
    })

    it('switches back and forth between the two shapes', () => {
      // 选错方案不该靠刷新页面来纠正：换回第一步的按钮要真的能换。
      const dom = boot('zh-CN', undefined, 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)', 'Win32')
      const doc = dom.window.document
      const back = doc.getElementById('stepBack') as HTMLElement

      back.click()
      expect((doc.getElementById('step-client') as HTMLElement).hidden).toBe(true)
      expect((doc.getElementById('step-server') as HTMLElement).hidden).toBe(true)
      expect(back.hidden).toBe(true)

      ;(doc.querySelector('.mode-card[data-mode="split"]') as HTMLElement).click()
      expect((doc.getElementById('step-server') as HTMLElement).hidden).toBe(false)
      expect((doc.getElementById('step-client') as HTMLElement).hidden).toBe(false)
      expect((doc.querySelector('[data-mode-panel="split"]') as HTMLElement).hidden).toBe(false)
      expect(back.hidden).toBe(false)
      // 分开装是两步，第一步末尾的「下一步」指向第二步
      const next = doc.getElementById('stepNext') as HTMLElement
      expect(next.hidden).toBe(false)
      expect(next.getAttribute('href')).toBe('#step-client')

      // 整合包里那张灰卡上的「换一种装法」也要能直接切过去
      ;(doc.querySelector('.mode-card[data-mode="integrated"]') as HTMLElement).click()
      expect(next.hidden).toBe(true)
      dom.window.close()
    })

    it('ties each choice to the steps it produces', () => {
      // 选项和它下面的步骤必须能对上号：用户点了一张卡，得看得出「接下来
      // 是几步、现在是第几步」。靠这两处新标记建立联系——选项卡里的步骤
      // 预告，和承接条/步骤上的归属标签。
      const dom = boot('zh-CN', undefined, 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)', 'Win32')
      const doc = dom.window.document
      const flow = (m: string) =>
        Array.from(doc.querySelectorAll(`.mode-card[data-mode="${m}"] .mode-flow-step`)).map(
          (el) => (el.textContent || '').trim(),
        )

      // 每张卡自己就写清会走几步，不用点开才知道
      expect(flow('integrated')).toEqual(['装客户端'])
      expect(flow('split')).toEqual(['装服务端', '连客户端'])

      // 承接条一开始就跟着默认方案显示，是「选项 → 步骤」的缝合处
      const bridge = doc.getElementById('modeBridge') as HTMLElement
      expect(bridge.hidden).toBe(false)
      const bridgeKey = () =>
        doc.getElementById('modeBridgeText')!.getAttribute('data-i18n-html')
      expect(bridgeKey()).toBe('mode.bridge.integrated')

      ;(doc.querySelector('.mode-card[data-mode="split"]') as HTMLElement).click()
      expect(bridgeKey()).toBe('mode.bridge.split')
      expect(doc.getElementById('stepClientFrom')!.getAttribute('data-i18n')).toBe(
        'step.from.split2',
      )
      expect(doc.querySelector('#step-server .step-from')!.textContent?.trim()).toBe(
        '分开装 · 第 1 步，共 2 步',
      )

      // 退回选择前，承接条要收起来，不能对着空页面说「下面有两步」
      ;(doc.getElementById('stepBack') as HTMLElement).click()
      expect(bridge.hidden).toBe(true)
      dom.window.close()
    })

    it('lands on the first step of the shape you picked', () => {
      // 点「分开装」却直接落到第 2 步，等于跳过了真正的起点：用户看不到
      // 服务端怎么装，还以为自己少做了一步。点的方案不同，落脚点也不同。
      const dom = boot('zh-CN', undefined, 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)', 'Win32')
      const doc = dom.window.document
      const scrolled = () => (dom.window as unknown as { __scrolledTo: string[] }).__scrolledTo

      scrolled().length = 0
      ;(doc.querySelector('.mode-card[data-mode="split"]') as HTMLElement).click()
      expect(scrolled()).toContain('step-server')
      expect(scrolled()).not.toContain('step-client')

      // 整合包没有服务端那一步，落到装客户端就是它的起点
      scrolled().length = 0
      ;(doc.querySelector('.mode-card[data-mode="integrated"]') as HTMLElement).click()
      expect(scrolled()).toContain('step-client')

      // 回流到选择区时不应该顺手把人带到某个步骤上
      scrolled().length = 0
      ;(doc.getElementById('stepBack') as HTMLElement).click()
      expect(scrolled()).not.toContain('step-server')
      expect(scrolled()).not.toContain('step-client')
      dom.window.close()
    })

    it('keeps the bridge wording in the right language', () => {
      const dom = boot('zh-CN', undefined, 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)', 'Win32')
      const doc = dom.window.document
      const text = () => doc.getElementById('modeBridgeText')!.textContent || ''

      // 承接条里带 <strong>，所以必须走 data-i18n-html；用 data-i18n 会把
      // 标签当文字，页面上直接出现「<strong>」三个字。
      expect(text()).toContain('1 步')
      expect(doc.getElementById('modeBridgeText')!.querySelector('strong')).toBeTruthy()

      ;(doc.getElementById('langToggle') as HTMLElement).click()
      expect(text()).toContain('one step')
      expect(doc.getElementById('modeBridgeText')!.querySelector('strong')).toBeTruthy()

      ;(doc.querySelector('.mode-card[data-mode="split"]') as HTMLElement).click()
      expect(text()).toContain('Two steps')

      // 切回中文：承接条的文案要跟着方案还原，不能停在上一个方案的英文
      ;(doc.getElementById('langToggle') as HTMLElement).click()
      expect(text()).toContain('两步')
      dom.window.close()
    })

    it('relabels the second step for each shape, in both languages', () => {
      // 整合包没有服务端那一步，第二步的标题必须跟着换成「装客户端」，
      // 否则用户会以为还缺一步没做。切语种后仍要正确。
      const dom = boot('zh-CN', undefined, 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)', 'Win32')
      const doc = dom.window.document
      const title = () => doc.querySelector('#step-client .step-title')?.textContent?.trim()

      expect(title()).toBe('装客户端')
      ;(doc.getElementById('langToggle') as HTMLElement).click()
      expect(title()).toBe('Install the client')
      expect(doc.querySelector('#step-client .step-desc')?.textContent?.trim()).toBeTruthy()

      // 切回分开装、再切回中文：标题要还原成「连客户端」，不能留英文
      ;(doc.getElementById('langToggle') as HTMLElement).click()
      ;(doc.querySelector('.mode-card[data-mode="split"]') as HTMLElement).click()
      expect(title()).toBe('连客户端')
      dom.window.close()
    })

    it('keeps the tab icons when the language switches', () => {
      // data-i18n replaces textContent, so the label has to be its own span —
      // putting the key on the <button> wipes the icon on every toggle.
      const dom = boot('zh-CN')
      const doc = dom.window.document
      ;(doc.getElementById('langToggle') as HTMLElement).click()

      const tabs = Array.from(doc.querySelectorAll('.tabs .tab'))
      expect(tabs.length).toBe(4)
      for (const tab of tabs) expect(tab.querySelector('svg use')).toBeTruthy()
      expect(
        tabs[3].querySelector('[data-i18n]')?.getAttribute('data-i18n'),
      ).toBe('dep.tab.binary')
      dom.window.close()
    })

    it('keeps tab labels on one line on narrow screens', () => {
      // 四个页签在 390px 的手机上放不下。没有 flex:none / nowrap 时，flex 会把
      // 按钮压到 min-content，「一键」直接折成竖排的「一 / 键」——jsdom 没有
      // 布局，只有源码守卫能拦住这一类。
      expect(css).toMatch(/\.tab\s*\{[^}]*flex:\s*none/)
      expect(css).toMatch(/\.tab\s*\{[^}]*white-space:\s*nowrap/)
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
      expect(assets.length).toBe(17)
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
      // 中文页的退路也走镜像：国内直连 github 常常几 KB/s，退路不该比
      // 正路还慢。英文页没有国内网络问题，用官方地址。
      const mirror = 'https://gh-proxy.com/'
      const raw = 'https://github.com/clawbench-dev/clawbench/releases/latest'

      const zh = boot('zh-CN')
      const zhHrefs = cards(zh.window.document).map((a) => a.href)
      expect(zhHrefs).toEqual(Array(zhHrefs.length).fill(mirror + raw))
      zh.window.close()

      const en = boot('en-US')
      const enHrefs = cards(en.window.document).map((a) => a.href)
      expect(enHrefs).toEqual(Array(enHrefs.length).fill(raw))
      en.window.close()
    })

    it('sends Chinese visitors to the mainland mirror, English ones straight to GitHub', async () => {
      // 镜像只在中文页生效：后端 desktop_upgrade.go 的 githubReleaseMirrors
      // 是给国内用户准备的，英文访客没有必要多经一手社区代理。
      const mirror = 'https://gh-proxy.com/'
      const tag = 'v9.9.9'
      const rawPrefix = `https://github.com/clawbench-dev/clawbench/releases/download/${tag}/`

      const zh = boot('zh-CN', () =>
        Promise.resolve({ ok: true, json: () => Promise.resolve({ tag_name: tag }) }),
      )
      await new Promise((r) => setTimeout(r, 0))
      const zhHrefs = cards(zh.window.document).map((a) => a.href)
      expect(zhHrefs.length).toBe(17)
      // 每一枚芯片都走镜像，不能有漏网的直连
      expect(zhHrefs.filter((h) => !h.startsWith(mirror + rawPrefix))).toEqual([])
      expect(zhHrefs).toContain(`${mirror}${rawPrefix}clawbench-desktop-windows-x64-local-${tag}.zip`)
      expect(zhHrefs).toContain(`${mirror}${rawPrefix}clawbench-desktop-darwin-arm64-local-${tag}.zip`)
      expect(zhHrefs).toContain(`${mirror}${rawPrefix}clawbench-desktop-linux-arm64-local-${tag}.zip`)
      zh.window.close()

      const en = boot('en-US', () =>
        Promise.resolve({ ok: true, json: () => Promise.resolve({ tag_name: tag }) }),
      )
      await new Promise((r) => setTimeout(r, 0))
      const enHrefs = cards(en.window.document).map((a) => a.href)
      expect(enHrefs.filter((h) => !h.startsWith(rawPrefix))).toEqual([])
      en.window.close()
    })

    it('drops the mirror prefix when the visitor switches to English', async () => {
      // 语种可随时切换：切到英文页后芯片若还挂着镜像前缀，就等于把
      // 「仅中文页可见」的假设写成了死代码。
      const dom = boot('zh-CN', () =>
        Promise.resolve({ ok: true, json: () => Promise.resolve({ tag_name: 'v9.9.9' }) }),
      )
      await new Promise((r) => setTimeout(r, 0))
      const toggle = dom.window.document.getElementById('langToggle') as HTMLElement
      toggle.click()

      const hrefs = cards(dom.window.document).map((a) => a.href)
      expect(hrefs.filter((h) => h.includes('gh-proxy.com'))).toEqual([])
      expect(hrefs.filter((h) => !h.startsWith('https://github.com/'))).toEqual([])
      dom.window.close()
    })

    it('rewrites every button to a direct download once the tag arrives', async () => {
      const dom = boot('zh-CN', () =>
        Promise.resolve({ ok: true, json: () => Promise.resolve({ tag_name: 'v9.9.9' }) }),
      )
      await new Promise((r) => setTimeout(r, 0))

      const hrefs = cards(dom.window.document).map((a) => a.href)
      // 中文页的直链前面带镜像前缀，资产路径本身仍指向同一份文件。
      const prefix =
        'https://gh-proxy.com/https://github.com/clawbench-dev/clawbench/releases/download/v9.9.9/'
      expect(hrefs).toContain(`${prefix}clawbench-desktop-linux-x64-v9.9.9.zip`)
      expect(hrefs).toContain(`${prefix}clawbench-android-v9.9.9.apk`)
      expect(hrefs.filter((h) => !h.startsWith(prefix))).toEqual([])
      dom.window.close()
    })

    it('tucks the phone client away and keeps every package reachable', () => {
      // 三个桌面系统都直接铺在页面上；手机客户端收进折叠区，需要时再展开。
      const dom = boot('zh-CN')
      const doc = dom.window.document

      const more = doc.getElementById('dlMoreBody') as HTMLElement
      expect(more.hidden).toBe(true)
      expect(
        Array.from(more.querySelectorAll('.dl-name')).map((n) => (n.textContent || '').trim()),
      ).toEqual(['Android'])
      dom.window.close()
    })

    it('expands the other platforms on click and keeps every package reachable', () => {
      // 折叠不能折叠掉可达性：13 个包必须在 DOM 里，展开后逐个可见可点。
      const dom = boot('zh-CN')
      const doc = dom.window.document
      const btn = doc.getElementById('dlMore') as HTMLElement
      const body = doc.getElementById('dlMoreBody') as HTMLElement

      expect(btn.getAttribute('aria-expanded')).toBe('false')
      btn.click()
      expect(body.hidden).toBe(false)
      expect(btn.getAttribute('aria-expanded')).toBe('true')
      btn.click()
      expect(body.hidden).toBe(true)
      expect(btn.getAttribute('aria-expanded')).toBe('false')

      // 折叠与否，芯片总数不变
      expect(doc.querySelectorAll('a[data-asset]').length).toBe(17)
      dom.window.close()
    })

    it('labels the collapsed group for both languages', () => {
      // 折叠按钮是纯文字按钮，缺英文译文会静默显示中文（见文件头说明）。
      for (const [lang, expectText] of [
        ['zh-CN', '手机'],
        ['en-US', 'Phone'],
      ]) {
        const dom = boot(lang)
        const text = dom.window.document.getElementById('dlMore')?.textContent || ''
        expect(text, lang).toContain(expectText)
        dom.window.close()
      }
    })

    it('highlights the visitor’s own platform without hiding the others', () => {
      // 按 UA 猜系统只用来加强调，绝不能用来隐藏——猜错了代价是看不到自己
      // 该下的包。Windows 访客：两张 Windows 方案卡被标出，macOS 卡仍在
      // 文档里且没被标。
      const dom = boot('zh-CN', undefined, 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)', 'Win32')
      const doc = dom.window.document

      const current = Array.from(doc.querySelectorAll('.dl-card.is-current'))
      expect(current.length).toBeGreaterThan(0)
      expect(current.every((c) => c.getAttribute('data-os') === 'win')).toBe(true)
      // 服务端面板里的 Windows 卡同样被标出
      expect(current.some((c) => c.closest('[data-panel="binary"]'))).toBe(true)

      const mac = doc.querySelector('.dl-card[data-os="mac"]')!
      expect(mac.classList.contains('is-current')).toBe(false)
      expect(mac.querySelector('a[data-asset]')).toBeTruthy()
      dom.window.close()

      // macOS 访客拿到的是 macOS 高亮，Windows 反而没被标
      const macDom = boot('zh-CN', undefined, 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', 'MacIntel')
      const macCurrent = Array.from(macDom.window.document.querySelectorAll('.dl-card.is-current'))
      expect(macCurrent.length).toBeGreaterThan(0)
      expect(macCurrent.every((c) => c.getAttribute('data-os') === 'mac')).toBe(true)
      macDom.window.close()
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

      // 17 = the backend registry (internal/ai/backends) minus the built-in
      // agent (no logo of its own) and Qoder CN (same product as Qoder and
      // reads as a duplicate here).
      // Bumping this number is the reminder to add a logo when one is added.
      expect(items.length).toBe(17)
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

    it('pairs every colour layer with a mono layer', () => {
      // Both layers sit in the same box: the mono one is the fallback under
      // the colour mark and the only mark for brands that have no colour
      // version at all.
      const dom = new JSDOM(html)
      const doc = dom.window.document
      const items = Array.from(doc.querySelectorAll('.strip-list li'))
      expect(items.length).toBe(17)
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
      // Not every logo declares it (Command Code ships without), so the guard
      // is "preserved where present", checked against the source files rather
      // than a blanket assertion that would fail on it.
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
      expect(logos.length).toBe(17)
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
