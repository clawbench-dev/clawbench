import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * Plain-text annotation is the meeting transcript's equivalent of the markdown
 * preview pipeline: the same two passes, over escaped text instead of rendered
 * markdown. These tests pin the parts that make it safe and useful — the escape
 * step, the order of the passes, and the anonymous-share refusal.
 */

let shareMode = false
vi.mock('@/share/shareMode', () => ({
  isShareMode: () => shareMode,
}))

import { annotatePlainText } from '@/composables/usePlainTextAnnotation'

beforeEach(() => {
  shareMode = false
})

describe('annotatePlainText', () => {
  it('annotates a file path with the shared chip contract', () => {
    const { html, detectedPaths } = annotatePlainText('入口在 web/src/main.ts')
    expect(detectedPaths).toContain('web/src/main.ts')
    expect(html).toContain('class="chat-file-path"')
    expect(html).toContain('data-file-path="web/src/main.ts"')
    // The open button is what the "jump to file" affordance is.
    expect(html).toContain('class="chat-file-open-btn')
    // The utterance itself is untouched.
    expect(html).toContain('入口在 ')
  })

  it('carries the line target through to the chip', () => {
    const { html } = annotatePlainText('见 web/src/main.ts:42-48')
    expect(html).toContain('data-line-start="42"')
    expect(html).toContain('data-line-end="48"')
  })

  it('links a bare URL, and does it BEFORE the path pass', () => {
    const { html } = annotatePlainText('参考 github.com/foo/bar 的实现')
    expect(html).toContain('class="md-external-link"')
    expect(html).toContain('data-href="https://github.com/foo/bar"')
    // The URL tail must not also become a file chip (`com/foo/bar`).
    expect(html).not.toContain('chat-file-path')
  })

  it('escapes the text before annotating it', () => {
    const { html } = annotatePlainText('<img src=x onerror="alert(1)"> 与 docs/a.md')
    expect(html).not.toContain('<img')
    expect(html).toContain('&lt;img')
    // …and the path in the same utterance is still found.
    expect(html).toContain('data-file-path="docs/a.md"')
  })

  it('leaves a path as text on an anonymous share page', () => {
    shareMode = true
    const { html, detectedPaths } = annotatePlainText('入口在 web/src/main.ts，参考 github.com/foo/bar')
    // The file viewer and batch-exists are auth-protected: a chip could only
    // fail on click, so it must not be drawn at all.
    expect(detectedPaths).toEqual([])
    expect(html).not.toContain('chat-file-path')
    // URLs are still clickable — the share SPA installs the same click layer.
    expect(html).toContain('class="md-external-link"')
  })

  it('returns nothing for empty text', () => {
    expect(annotatePlainText('')).toEqual({ html: '', detectedPaths: [] })
  })
})
