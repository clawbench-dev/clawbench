import { describe, expect, it } from 'vitest'
import { buildHtmlPreviewSrc } from '@/utils/htmlPreview'

/**
 * buildHtmlPreviewSrc is the single source of truth for how an HTML document is
 * loaded into a preview iframe (shared by the full-screen FileViewer and the
 * quick code-link preview pane). Its two rules — real URL for project-relative
 * files, empty string (→ srcdoc fallback) for external files — are load-bearing;
 * see the module header for why.
 */
describe('buildHtmlPreviewSrc', () => {
  it('builds a project-relative URL with a cache-buster', () => {
    const url = buildHtmlPreviewSrc('public/index.html', 1234)
    expect(url).toBe('/api/fs/raw/public/index.html?t=1234')
  })

  it('returns empty for an absolute (external) path so the caller falls back to srcdoc', () => {
    // The URL form would resolve relative references against the project root
    // and silently load the WRONG file when one of the same name exists there.
    expect(buildHtmlPreviewSrc('/tmp/external/index.html', 1234)).toBe('')
  })

  it('returns empty for an empty path', () => {
    expect(buildHtmlPreviewSrc('', 1234)).toBe('')
  })

  it('changes the URL when the timestamp changes (cache-buster)', () => {
    expect(buildHtmlPreviewSrc('a.html', 1)).not.toBe(buildHtmlPreviewSrc('a.html', 2))
  })
})
