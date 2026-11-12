/**
 * The fallback download button must not wear the browser's button chrome.
 *
 * `.share-download-btn` was an `<a href download>` until the download grew an
 * in-product progress bar, when it became a `<button>` (the progress bar needs
 * a click handler, not a navigation). A `<button>` brings its own UA styling:
 * a 2px OUTSET border framing the accent fill, and `font: 13.33px Arial` for
 * the label. The rule kept `text-decoration: none` from its `<a>` days but
 * never reset the border, so every un-previewable share showed an orange chip
 * inside a grey bevel — and the label in a different typeface from the page.
 *
 * jsdom applies no stylesheets, so a rendered assertion cannot see this; the
 * rule is read from the source instead, the same way the other CSS guards in
 * this directory work.
 */
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/** Repo root: this file lives at web/src/share/__tests__/. */
const ROOT = join(__dirname, '..', '..', '..', '..')
const VIEW = 'web/src/share/ShareView.vue'

function declarations(css: string, selector: string): Record<string, string> {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = css.match(new RegExp(`\\n${escaped}\\s*\\{([^}]*)\\}`))
  if (!m) throw new Error(`rule ${selector} not found`)
  const out: Record<string, string> = {}
  for (const decl of m[1].split(';')) {
    const i = decl.indexOf(':')
    if (i < 0) continue
    out[decl.slice(0, i).trim()] = decl.slice(i + 1).trim()
  }
  return out
}

const source = readFileSync(join(ROOT, VIEW), 'utf8')
const style = source.slice(source.indexOf('<style'))

describe('share fallback download button', () => {
  it('renders as a <button> driven by the progress-bar download', () => {
    expect(source).toContain('class="share-download-btn" type="button" @click="handleDownload"')
  })

  it('resets the UA button border', () => {
    const decls = declarations(style, '.share-download-btn')
    expect(decls.border).toBe('none')
  })

  it('uses the page typeface rather than the UA button font', () => {
    const decls = declarations(style, '.share-download-btn')
    expect(decls['font-family']).toBe('inherit')
  })
})
