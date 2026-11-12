import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync } from 'fs'
import { resolve } from 'path'
import { buildMarkdownPreviewDom } from '@/composables/useMarkdownRenderPipeline'
import { configureMarkedRenderer } from '@/utils/markedConfig'
import { setShareToken } from '@/share/shareMode'

/**
 * Fixture-driven regression test for "图片地址里有中文 → 图片出不来".
 *
 * The scenario is a real document shape (a Chinese-named assets folder holding
 * Chinese-named screenshots), so the fixture is a real .md on disk rather than
 * a string literal: `使用说明.md` is what a person opens in the app and shares,
 * and this file renders exactly that document through the real pipeline.
 *
 * Root cause it guards: `marked` percent-encodes the destination it writes into
 * <img src>, so the path arrives as `%E6%88%AA%E5%9B%BE.png`. Encoding that form
 * again turns `%E6` into `%25E6`, and the backend then looks for a file whose
 * name literally starts with `%E6%88%AA` — 404, blank image.
 *
 * The doc's OWN directory being Chinese is irrelevant (that path is built from
 * real filesystem names, never from a marked-encoded src); only the image
 * address matters, which is why every assertion here is about the <img> URLs.
 */

configureMarkedRenderer()

const DIR = __dirname
const DOC_REL = 'test/markdown-chinese-path/使用说明.md'
const DOC_ABS = resolve(DIR, '使用说明.md')

/**
 * Every image the fixture embeds, in document order, as a path relative to the
 * fixture dir. `control.png` appears twice on purpose (once as the English-name
 * control, once inline mid-paragraph).
 */
const EMBEDS = [
  'clawbench配置使用.assets/截图.png',
  'clawbench配置使用.assets/架构图 说明.png',
  'clawbench配置使用.assets/control.png',
  '../images/img_cute_cat.svg',
  'clawbench配置使用.assets/control.png',
]

function render(opts: { path: string; share: boolean }): string {
  const content = readFileSync(DOC_ABS, 'utf-8')
  if (opts.share) setShareToken('fixturetoken')
  try {
    return buildMarkdownPreviewDom({ content, path: opts.path }, { isWideScreen: true, imageTimestamp: 7 }).html
  } finally {
    setShareToken(null)
  }
}

function imgSrcs(html: string): string[] {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  return Array.from(doc.querySelectorAll('img[src]')).map(img => img.getAttribute('src') || '')
}

describe('fixture: 中文路径图片', () => {
  it('the fixture references files that actually exist', () => {
    // A fixture whose images were deleted would still "pass" every URL check
    // below while showing nothing at all — so pin the disk state too.
    for (const rel of new Set(EMBEDS)) {
      expect(existsSync(resolve(DIR, rel)), rel).toBe(true)
    }
  })

  it('preview mode: every image gets a served URL, none double-encoded', () => {
    const html = render({ path: DOC_REL, share: false })
    const srcs = imgSrcs(html)

    expect(srcs).toHaveLength(EMBEDS.length)
    for (const src of srcs) {
      expect(src, 'every local image must go through a file endpoint').toMatch(/^\/api\/fs\/(raw|thumb)/)
      expect(src, `double-encoded: ${src}`).not.toContain('%25')
    }

    // The Chinese-named screenshot resolves to its real, single-encoded path.
    expect(html).toContain('test/markdown-chinese-path/clawbench%E9%85%8D%E7%BD%AE%E4%BD%BF%E7%94%A8.assets/%E6%88%AA%E5%9B%BE.png')
    // A space in the filename is encoded as %20, not left raw.
    expect(html).toContain('%E6%9E%B6%E6%9E%84%E5%9B%BE%20%E8%AF%B4%E6%98%8E.png')
  })

  it('share mode: ?path= carries the real filename, not an encoded one', () => {
    // The share SPA always renders with an absolute document path, so every
    // image goes through the `?path=` branch — the exact place the second
    // encoding happened.
    const html = render({ path: DOC_ABS, share: true })
    const srcs = imgSrcs(html)

    expect(srcs).toHaveLength(EMBEDS.length)
    for (const src of srcs) {
      expect(src, `unexpected endpoint: ${src}`).toContain('/api/share/fixturetoken/local')
      expect(src, `double-encoded: ${src}`).not.toContain('%25')
    }

    const shot = srcs.find(s => s.includes('%E6%88%AA%E5%9B%BE.png'))
    expect(shot, 'the Chinese-named screenshot must appear').toBeTruthy()
    expect(decodeURIComponent(shot!)).toContain(
      resolve(DIR, 'clawbench配置使用.assets', '截图.png')
    )
  })
})
