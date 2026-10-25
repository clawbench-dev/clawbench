import { describe, it, expect, beforeAll, vi } from 'vitest'
import { configureMarkedRenderer } from '@/utils/markedConfig'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer'

/**
 * Chat-pipeline half of the non-image-embed repair.
 *
 * `nonImageEmbed.test.ts` covers the unit (classify / downgrade); this file
 * covers the WIRING — that the chat render path actually calls it, with real
 * marked, in both the streaming (`skipEnhancements`) and final renders.
 *
 * Message 65399 is the regression: the AI wrote
 * `![设备编排方案](/proj/docs/screenshots/_hero_layouts.html)`, the browser could
 * not decode HTML as an image, and the user saw the "failed to load" placeholder
 * where a link belonged. The same message also carried a table whose delimiter
 * row had fewer columns than its header — that one is intentionally NOT handled
 * here: it is malformed GFM and stays literal text (decided 2026-10-25).
 */

vi.mock('@/composables/useAppMode', () => ({
  useAppMode: () => ({ isAppMode: { value: false } }),
}))
vi.mock('@/composables/usePortForward', () => ({
  usePortForward: () => ({
    sshInfo: { value: { enabled: false } },
    transportAllowsH2: { value: false },
  }),
}))

const parse = (html: string) => new DOMParser().parseFromString(html, 'text/html')

describe('non-image embeds in the chat pipeline', () => {
  beforeAll(() => configureMarkedRenderer())

  it('downgrades an .html embed to a link (final render)', () => {
    const md = '方案页已更新。\n\n![设备编排方案（允许遮挡 PC）](/proj/docs/screenshots/_hero_layouts.html)\n\n后面还有正文。'
    const doc = parse(renderMarkdownHtml(md))
    expect(doc.querySelector('img')).toBeNull()
    const a = doc.querySelector('a[href]')!
    expect(a.getAttribute('href')).toBe('/proj/docs/screenshots/_hero_layouts.html')
    expect(a.textContent).toBe('设备编排方案（允许遮挡 PC）')
    // No leftover media figure shell (view/attach buttons over nothing).
    expect(doc.querySelector('.image-block-wrapper')).toBeNull()
  })

  it('downgrades it during streaming too (skipEnhancements)', () => {
    // Mid-stream the message is still growing; the placeholder is just as wrong
    // there, and the enhancement block is skipped in that mode.
    const md = '![布局页](/proj/docs/screenshots/_hero_layouts.html)'
    const doc = parse(renderMarkdownHtml(md, { skipEnhancements: true }))
    expect(doc.querySelector('img')).toBeNull()
    expect(doc.querySelector('a[href]')!.getAttribute('href'))
      .toBe('/proj/docs/screenshots/_hero_layouts.html')
  })

  it('still renders a real image embed as a figure', () => {
    const md = '![产品图](/proj/docs/screenshots/product_hero.png)'
    const doc = parse(renderMarkdownHtml(md))
    expect(doc.querySelector('img')).not.toBeNull()
    expect(doc.querySelector('.image-block-wrapper')).not.toBeNull()
  })

  it('unwraps backticks around a real image so the request is not 404', () => {
    // The AI copying the prompt placeholder verbatim. marked percent-encodes
    // the backticks into the src, so the browser asks for `%60…%60`.
    const md = '![图](`.clawbench/generated/a.png`)'
    const doc = parse(renderMarkdownHtml(md))
    const img = doc.querySelector('img')!
    expect(img.getAttribute('src')).not.toContain('%60')
    expect(img.getAttribute('src')).toContain('a.png')
  })
})
