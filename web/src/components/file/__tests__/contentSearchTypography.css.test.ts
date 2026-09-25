import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Contract: the content-search result text is as legible as the file browser
 * it sits next to.
 *
 * Why this exists: the match lines (`.cs-match`) — the primary content of the
 * panel — were `--font-size-xs` (11px) in a monospace face, while a file-browser
 * row (`.file-item`) is `--font-size-md` (13px). At 11px mono the lines read as
 * thin and hard to scan. The two surfaces live in the same panel and are reached
 * from the same toolbar, so their body text must agree.
 *
 * jsdom has no CSS engine, so this is a source-level check: read the tokens from
 * variables.css and assert the selectors use them. Comparing token NAMES (not
 * resolved px) keeps this honest when the scale is retuned — what matters is
 * that the two panels keep pointing at the same step.
 */

const dialog = readWebFile('src/components/file/ContentSearchDialog.vue')
const browser = readWebFile('src/components/file/FileManagerContent.vue')

/**
 * Declarations of the rule whose selector is EXACTLY `selector`.
 *
 * The selector must start at a rule boundary (after `}` or a newline) and be
 * followed by `{`. Without that, `.file-item` would match the earlier
 * `.file-item.ctx-highlight {` / `.file-item.cut-item,` rules and read their
 * declarations instead — the wrong rule entirely.
 */
function declsOf(src: string, selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = src.match(new RegExp(`(?:^|[}\\n])\\s*${escaped}\\s*\\{([^}]*)\\}`, 'm'))
  expect(m, `${selector} rule must exist`).not.toBeNull()
  return m![1]
}

/** The `font-size` value of a rule, e.g. `var(--font-size-md)`. */
function fontSizeOf(src: string, selector: string): string {
  const m = declsOf(src, selector).match(/font-size:\s*([^;]+);/)
  expect(m, `${selector} must declare a font-size`).not.toBeNull()
  return m![1].trim()
}

/** Resolve a `var(--token)` to its px value in variables.css. */
function tokenPx(token: string): number {
  const name = token.match(/var\((--[\w-]+)\)/)?.[1]
  expect(name, `${token} should be a var() reference`).toBeTruthy()
  const css = readWebFile('css/variables.css')
  const m = css.match(new RegExp(`${name}:\\s*(\\d+(?:\\.\\d+)?)px`))
  expect(m, `${name} must be defined in px`).not.toBeNull()
  return Number(m![1])
}

describe('content-search result text matches the file browser', () => {
  it('a match line uses the same size as a file-browser row', () => {
    // The core of the reported problem: 11px mono was too small/thin.
    expect(fontSizeOf(dialog, '.cs-match')).toBe(fontSizeOf(browser, '.file-item'))
  })

  it('a file group header uses the same size as a file-browser row', () => {
    // The header IS a file name row (icon + name + count), so it belongs at the
    // body size rather than a step below it.
    expect(fontSizeOf(dialog, '.cs-file-head')).toBe(fontSizeOf(browser, '.file-item'))
  })

  it('the match line is at least the browser row size in px', () => {
    // Guards a same-name-but-retuned token: if --font-size-md were dropped below
    // the browser's, "same token" would stop meaning "same size".
    expect(tokenPx(fontSizeOf(dialog, '.cs-match')))
      .toBeGreaterThanOrEqual(tokenPx(fontSizeOf(browser, '.file-item')))
  })

  it('secondary text stays smaller than the body, mirroring .file-meta', () => {
    // Directory, count and summary are supporting metadata. They should be a
    // step down from the body — but must not be the body's own size, or the
    // hierarchy collapses and every line competes for attention.
    const body = tokenPx(fontSizeOf(dialog, '.cs-match'))
    for (const sel of ['.cs-file-dir', '.cs-file-count']) {
      expect(tokenPx(fontSizeOf(dialog, sel)), `${sel} should be below body`).toBeLessThan(body)
    }
    expect(fontSizeOf(dialog, '.cs-file-dir')).toBe(fontSizeOf(browser, '.file-meta'))
  })

  it('the match line number shares the code size and is muted by opacity', () => {
    // The code viewer's gutter does the same: same size as the code, dimmed with
    // --opacity-muted. Shrinking the number instead would misalign it against
    // the line it labels.
    const lineDecls = declsOf(dialog, '.cs-match-line')
    expect(lineDecls).not.toMatch(/font-size:/)
    expect(lineDecls).toMatch(/opacity:\s*var\(--opacity-muted\)/)
  })

  it('no result text drops back to the dense-meta size', () => {
    // --font-size-xs is documented for "dense meta: tool cards, file paths,
    // diffs" — not for the rows a user reads to pick a result.
    for (const sel of ['.cs-match', '.cs-file-head']) {
      expect(fontSizeOf(dialog, sel), `${sel} must not use --font-size-xs`)
        .not.toContain('--font-size-xs')
    }
  })
})

describe('header scope suffix reads as secondary', () => {
  /** Declarations of `.bs-header-title .cs-header-scope`. */
  function suffixDecls(): string {
    const m = dialog.match(
      /\.bs-header-title\s+\.cs-header-scope\s*\{([^}]*)\}/,
    )
    expect(m, '.bs-header-title .cs-header-scope rule must exist').not.toBeNull()
    return m![1]
  }

  it('is muted and lighter than the prefix, not the same emphasis', () => {
    // The prefix names the tool; the suffix is the current setting. Rendering
    // it at the title's own colour/weight would make it read as part of the
    // name rather than a value that changes.
    const decls = suffixDecls()
    expect(decls).toMatch(/color:\s*var\(--text-muted\)/)
    expect(decls).toMatch(/font-weight:\s*var\(--font-weight-normal\)/)
  })

  it('is smaller than the title it sits in', () => {
    // .bs-header-title is --font-size-lg; the suffix must step down from it.
    const m = suffixDecls().match(/font-size:\s*([^;]+);/)
    expect(m, 'the suffix must declare a font-size').not.toBeNull()
    expect(tokenPx(m![1].trim())).toBeLessThan(tokenPx('var(--font-size-lg)'))
  })

  it('scopes itself under .bs-header-title so it can beat the title colour', () => {
    // .bs-header-title sets --text-primary at the same specificity as a bare
    // class, and it lives in BottomSheet's GLOBAL stylesheet while this rule is
    // scoped — so the descendant form is what makes the muted colour win.
    expect(dialog).toMatch(/\.bs-header-title\s+\.cs-header-scope\s*\{/)
  })
})
