/**
 * The fallback download control is a LINK carrying the direct download URL.
 *
 * It has to be an `<a href>` rather than a `<button>`: the href IS the file, so
 * the browser's own right-click menu offers "copy link address" / "save link
 * as". The click handler still drives the in-product progress bar, which is why
 * the control cannot simply navigate.
 *
 * Because it is an `<a>`, the UA chrome (`border` / `font`) no longer applies —
 * but the accent chip is painted by this rule, and `text-decoration: none` is
 * load-bearing again (a bare link would underline its label). jsdom applies no
 * stylesheets, so these are read from the source, the same way the other CSS
 * guards in this directory work.
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

describe('share fallback download control', () => {
  it('renders as a link carrying the direct download URL', () => {
    expect(source).toContain('class="share-download-btn" :href="downloadUrl" @click.prevent="handleDownload"')
  })

  it('does not underline the label like a bare link', () => {
    const decls = declarations(style, '.share-download-btn')
    expect(decls['text-decoration']).toBe('none')
  })

  it('paints the accent chip itself instead of relying on UA styling', () => {
    const decls = declarations(style, '.share-download-btn')
    expect(decls.background).toBe('var(--accent-color, #0969da)')
    expect(decls['font-family']).toBe('inherit')
  })
})
