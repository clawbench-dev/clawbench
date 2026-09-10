import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { annotateMediaBlocks, armMermaidFigure, IMAGE_VIEW_ICON_SVG } from '@/utils/mediaBlockFactory'
import { setShareToken } from '@/share/shareMode'

/**
 * Unified media-block factory tests:
 * every inline media (raster <img> / bare inline <svg>) is lifted into a
 * `.image-block-wrapper` figure with a header view button; local images
 * (data-attach-src) get attach/open buttons outside share mode; mermaid
 * containers get armed via armMermaidFigure. Idempotent on re-application.
 */
describe('annotateMediaBlocks', () => {
  beforeEach(() => {
    setShareToken(null)
  })
  afterEach(() => {
    setShareToken(null)
  })

  it('lifts a solo <img> inside <p> into a bordered figure with a view button', () => {
    const html = '<p><img src="/api/local-file/a.png" alt="a"></p>'
    const out = annotateMediaBlocks(html)
    expect(out).toContain('<div class="image-block-wrapper">')
    expect(out).toContain('image-block-header')
    expect(out).toContain('image-block-view-btn')
    expect(out).toMatch(/<span class="lightbox-img-wrap"><img[^>]*class="[^"]*lightbox-img[^"]*"[^>]*><\/span>/)
    // No wrapper <p> remains around a solo image.
    expect(out).not.toMatch(/<p>\s*<\/p>/)
  })

  it('stamps the lightbox-img marker without duplicating it', () => {
    const html = '<p><img src="x.png" class="chat-img lightbox-img"></p>'
    const out = annotateMediaBlocks(html)
    expect(out).toMatch(/class="chat-img lightbox-img"/)
    expect(out).not.toContain('lightbox-img lightbox-img')
  })

  it('splits a mid-paragraph image into before-p + figure + after-p', () => {
    const html = '<p>before <img src="x.png"> after</p>'
    const out = annotateMediaBlocks(html)
    expect(out).toMatch(/<p>before <\/p>/)
    expect(out).toMatch(/<div class="image-block-wrapper">/)
    expect(out).toMatch(/<p> after<\/p>/)
    expect(out.indexOf('before')).toBeLessThan(out.indexOf('image-block-wrapper'))
    expect(out.indexOf('image-block-wrapper')).toBeLessThan(out.indexOf('after'))
  })

  it('keeps multiple media in one paragraph in their original order', () => {
    const out = annotateMediaBlocks('<p><img src="a.png"><img src="b.png"><img src="c.png"></p>')
    const order = [...out.matchAll(/src="([abc])\.png"/g)].map(m => m[1])
    expect(order).toEqual(['a', 'b', 'c'])
    expect(out.match(/<div class="image-block-wrapper">/g)).toHaveLength(3)
  })

  it('keeps media + text in one paragraph in order (image-text-image)', () => {
    const out = annotateMediaBlocks('<p><img src="a.png"> mid <img src="b.png"></p>')
    const order = [...out.matchAll(/src="([ab])\.png"/g)].map(m => m[1])
    expect(order).toEqual(['a', 'b'])
    expect(out.indexOf('mid')).toBeGreaterThan(out.indexOf('a.png'))
    expect(out.indexOf('mid')).toBeLessThan(out.indexOf('b.png'))
  })

  it('does not lift an image inside <a> — the link must survive re-parse', () => {
    // A block <div> inside <a> is hoisted out by the HTML parser, emptying the
    // link. Keep linked media inline (marker only) and leave the <a> intact.
    const out = annotateMediaBlocks('<p>see <a href="https://ex.com"><img src="a.png"></a> end</p>')
    expect(out).not.toContain('image-block-wrapper')
    const host = document.createElement('div')
    host.innerHTML = out // same path as Vue v-html
    const a = host.querySelector('a')
    expect(a?.getAttribute('href')).toBe('https://ex.com')
    expect(a?.querySelector('img')).not.toBeNull()
    expect(a?.querySelector('img')?.classList.contains('lightbox-img')).toBe(true)
  })

  it('moves the host block source line onto the figure (line-anchor scroll)', () => {
    // markedConfig stamps data-source-line on the paragraph. When the paragraph
    // is fully dissolved (solo media), the attribute must move to the figure —
    // else media-containing paragraphs cannot be located by scrollRenderedToLine.
    const solo = annotateMediaBlocks('<p data-source-line="7"><img src="a.png"></p>')
    expect(solo).toContain('data-source-line="7"')
    // Exactly once (no duplicate on wrapper + leading <p>).
    expect(solo.match(/data-source-line="7"/g)).toHaveLength(1)
  })

  it('keeps the source line on the LEADING <p> for mid-paragraph media', () => {
    // The line must stay at the paragraph top (where the text starts), not move
    // down to the figure — otherwise restore drifts to the bottom of the block.
    const out = annotateMediaBlocks('<p data-source-line="3">see <img src="a.png"></p>')
    const host = document.createElement('div')
    host.innerHTML = out
    const leadingP = host.querySelector('p')
    expect(leadingP?.getAttribute('data-source-line')).toBe('3')
    // The figure itself must not duplicate the line.
    expect(host.querySelector('.image-block-wrapper')?.getAttribute('data-source-line')).toBeNull()
  })

  it('inserts the figure in place inside li/td without splitting', () => {
    const html = '<ul><li><img src="x.png"></li></ul><table><tbody><tr><td><img src="y.png"></td></tr></tbody></table>'
    const out = annotateMediaBlocks(html)
    expect(out.match(/<div class="image-block-wrapper">/g)).toHaveLength(2)
    // li/td may not contain a block div structurally but browsers accept it;
    // the key assertion is that both images moved into figures.
    expect(out).toMatch(/<li><div class="image-block-wrapper">/)
    expect(out).toMatch(/<td><div class="image-block-wrapper">/)
  })

  it('adds attach/open buttons only for local images outside share mode', () => {
    const html = '<img src="/api/local-file/a.png" data-attach-src="a.png">'
    const out = annotateMediaBlocks(html)
    expect(out).toContain('image-block-attach-btn')
    expect(out).toContain('image-block-open-btn')
  })

  it('omits attach/open buttons in share mode', () => {
    setShareToken('tok-share')
    const html = '<img src="/api/local-file/a.png" data-attach-src="a.png">'
    const out = annotateMediaBlocks(html)
    expect(out).toContain('image-block-view-btn')
    expect(out).not.toContain('image-block-attach-btn')
    expect(out).not.toContain('image-block-open-btn')
  })

  it('lifts a bare inline svg into a lightbox-svg-wrap cell', () => {
    const html = '<p><svg viewBox="0 0 10 10" class="lightbox-svg"><rect></rect></svg></p>'
    const out = annotateMediaBlocks(html)
    expect(out).toContain('<div class="image-block-wrapper">')
    expect(out).toContain('lightbox-svg-wrap')
    expect(out).toContain('<svg viewBox="0 0 10 10" class="lightbox-svg">')
    expect(out).toContain('image-block-view-btn')
  })

  it('does not lift svg nested inside a/button (pipeline UI icons)', () => {
    const html = '<button class="chat-file-open-btn"><svg viewBox="0 0 24 24"><path></path></svg></button>'
    expect(annotateMediaBlocks(html)).toBe(html)
  })

  it('is idempotent — re-applying does not double-wrap', () => {
    const html = '<p><img src="x.png"></p>'
    const once = annotateMediaBlocks(html)
    const twice = annotateMediaBlocks(once)
    expect(twice).toBe(once)
    expect(twice.match(/class="image-block-wrapper"/g)).toHaveLength(1)
  })

  it('passes through empty html', () => {
    expect(annotateMediaBlocks('')).toBe('')
  })
})

describe('armMermaidFigure', () => {
  function cleanupFigure(container: HTMLElement) {
    // armMermaidFigure inserts a wrapper BEFORE the container and moves the
    // container inside it — removing only the container would orphan the
    // wrapper in document.body. Remove the wrapper (container's parent).
    container.parentElement?.remove()
    container.remove()
  }

  it('wraps a mermaid container in a figure with a view button only by default', () => {
    const container = document.createElement('div')
    container.className = 'mermaid'
    container.innerHTML = '<svg></svg>'
    document.body.appendChild(container)
    try {
      armMermaidFigure(container, { attach: false })
      expect(container.parentElement?.classList.contains('image-block-wrapper')).toBe(true)
      const wrapper = container.parentElement!
      expect(wrapper.querySelector('.image-block-view-btn')).not.toBeNull()
      expect(wrapper.querySelector('.image-block-attach-btn')).toBeNull()
    } finally {
      cleanupFigure(container)
    }
  })

  it('adds a dual-class attach button when attach:true (file preview)', () => {
    const container = document.createElement('div')
    container.className = 'mermaid'
    document.body.appendChild(container)
    try {
      armMermaidFigure(container, { attach: true })
      const wrapper = container.parentElement!
      const btn = wrapper.querySelector('.image-block-attach-btn')
      expect(btn).not.toBeNull()
      expect(btn!.classList.contains('mermaid-block-attach-btn')).toBe(true)
      expect(wrapper.querySelector('.image-block-view-btn')).not.toBeNull()
    } finally {
      cleanupFigure(container)
    }
  })

  it('is idempotent on the same container', () => {
    const container = document.createElement('div')
    container.className = 'mermaid'
    document.body.appendChild(container)
    try {
      armMermaidFigure(container, { attach: false })
      armMermaidFigure(container, { attach: true }) // guard ignores attach toggle
      expect(container.parentElement?.classList.contains('image-block-wrapper')).toBe(true)
      expect(document.querySelectorAll('.image-block-wrapper')).toHaveLength(1)
    } finally {
      cleanupFigure(container)
    }
  })
})

describe('IMAGE_VIEW_ICON_SVG', () => {
  it('exports a maximize glyph', () => {
    expect(IMAGE_VIEW_ICON_SVG).toContain('<svg')
    expect(IMAGE_VIEW_ICON_SVG).toContain('</svg>')
  })
})
