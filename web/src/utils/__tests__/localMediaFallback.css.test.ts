import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The failed-image placeholder is only half a fix unless the ORIGINAL <img> is
 * actually hidden.
 *
 * localMediaFallback.ts keeps the broken <img> in the DOM (so the fallback is
 * reversible when the file later appears) and stamps it `local-media-hidden`.
 * That class used to be hidden by a single descendant rule:
 *
 *   .image-block-wrapper .lightbox-img-wrap img.local-media-hidden { display: none }
 *
 * which only matches the rendered-markdown FIGURE. Every other surface that
 * reuses the same fallback — the full-screen image viewer, the media preview
 * card, the share SPA, a wallpaper — was left with the browser's broken-image
 * glyph (and its alt text) sitting next to the placeholder.
 *
 * The contract: the hide must be ancestor-INDEPENDENT. Because a bare
 * `img.local-media-hidden` (0-1-1) cannot outrank the figure's own
 * `.image-block-wrapper .lightbox-img-wrap img` (0-2-1), the rule must be
 * `!important` — a specificity tie would silently bring the glyph back.
 *
 * jsdom has no cascade, so this is a source-contract check (same pattern as
 * MarkdownPreviewSvgFit.css.test.ts / chatInlineImageSizing.css.test.ts).
 */

const stripComments = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, '')

const css = stripComments(readWebFile('css/media-block.css'))

interface Rule {
  selector: string
  decls: string
}

/** Every declaration block, with each selector of a comma list reported alone. */
function rulesOf(source: string): Rule[] {
  const out: Rule[] = []
  for (const m of source.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    for (const selector of m[1].split(',').map((s) => s.replace(/\s+/g, ' ').trim())) {
      if (selector) out.push({ selector, decls: m[2] })
    }
  }
  return out
}

/** Rules that hide a failed-media element. */
function hideRules(): Rule[] {
  return rulesOf(css).filter(
    (r) => /\.local-media-hidden\b/.test(r.selector) && /display\s*:\s*none/.test(r.decls),
  )
}

describe('failed-media hide rule (media-block.css contract)', () => {
  it('hides the broken <img> with a rule that needs no particular ancestor', () => {
    const rules = hideRules()
    expect(rules.length, 'a hide rule for .local-media-hidden must exist').toBeGreaterThan(0)

    // A descendant combinator (`A B`) means the rule only fires inside A. The
    // fallback is used on surfaces with no figure at all, so at least one hide
    // rule must be a plain, context-free selector.
    const global = rules.filter((r) => !/\s/.test(r.selector))
    expect(
      global.map((r) => r.selector),
      'the hide must not require an ancestor (e.g. .image-block-wrapper …)',
    ).not.toEqual([])
  })

  it('uses !important so no higher-specificity display rule can resurrect the glyph', () => {
    // `.image-block-wrapper .lightbox-img-wrap img` sets display: inline-block
    // at specificity 0-2-1, which outranks a bare `img.local-media-hidden`
    // (0-1-1). Without !important the figure keeps its broken image.
    const important = hideRules().filter((r) => /display\s*:\s*none\s*!important/.test(r.decls))
    expect(
      important.map((r) => r.selector),
      'a context-free hide rule must use display: none !important',
    ).not.toEqual([])
  })

  it('still covers the rendered-markdown figure', () => {
    // The figure case is the one the class was introduced for; the generalized
    // rule must keep hiding it (either via the global rule or a scoped one).
    const rules = hideRules()
    expect(rules.length).toBeGreaterThan(0)
  })
})

/** [classes+attrs+pseudo, elements] — ids are not used in this file. */
function specOf(selector: string): [number, number] {
  const classes =
    (selector.match(/\.[\w-]+/g) ?? []).length +
    (selector.match(/\[[^\]]*\]/g) ?? []).length +
    (selector.match(/(?<!:):(?!:)[\w-]+/g) ?? []).length
  const elements =
    (selector.match(/(?:^|[\s>+~])[a-zA-Z][\w-]*/g) ?? []).length +
    (selector.match(/::[\w-]+/g) ?? []).length
  return [classes, elements]
}

/**
 * The declaration that wins `prop` for an element carrying `targetClass`.
 *
 * Only the class itself is matched — no ancestor simulation. That is enough
 * because a competitor's power to win is fully described by its specificity
 * plus source order: an ancestor-scoped rule (`A .x`) still has to outrank (or
 * tie-and-follow) the rule under test. Ties resolve to the later rule, which is
 * what the browser does.
 */
function winnerFor(targetClass: string, prop: string, opts: { exclude?: RegExp } = {}) {
  let best: { selector: string; value: string; spec: [number, number] } | null = null
  // Source order: a rule that ties on specificity replaces the earlier winner,
  // which is exactly how the cascade breaks ties.
  for (const m of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    for (const raw of m[1].split(',')) {
      const selector = raw.replace(/\s+/g, ' ').trim()
      // The rightmost compound is the element the rule styles; `targetClass`
      // must be one of its classes.
      const target = selector.split(/[\s>+~]+/).pop() ?? ''
      if (!target.split('.').includes(targetClass)) continue
      if (opts.exclude?.test(selector)) continue
      const decl = new RegExp(`(?:^|;)\\s*${prop}\\s*:\\s*([^;]+)`).exec(m[2])
      if (!decl) continue
      const spec = specOf(selector)
      if (!best || spec[0] > best.spec[0] || (spec[0] === best.spec[0] && spec[1] >= best.spec[1])) {
        best = { selector, value: decl[1].trim(), spec }
      }
    }
  }
  return best
}

describe('failed media draws exactly ONE frame (media-block.css contract)', () => {
  /**
   * A failed image in chat / share keeps the figure wrapper and gets the
   * placeholder mounted INSIDE it (localMediaFallback.ts inserts after the
   * <img>, which lives in `.lightbox-img-wrap`). Both used to draw a frame, so
   * the two boxes sat flush and read as a single element with two borders and
   * two corner radii — measured --radius-sm (6px) solid outside vs --radius-md
   * (10px) dashed inside. The fix drops the FIGURE's frame so the placeholder's
   * dashed border is the only one.
   *
   * jsdom has no cascade, so this resolves specificity + source order by hand
   * rather than grepping for the rule — a newly added higher-specificity
   * `border` on the figure must fail here instead of silently restoring the
   * double frame.
   */
  it('the figure stops drawing its own border when its media is missing', () => {
    const border = winnerFor('image-block-missing', 'border')
    expect(border, 'a rule targeting .image-block-missing must set the border').not.toBeNull()
    expect(border!.value, `figure border came back via ${border!.selector}`).toBe('none')
  })

  it('and stops rounding the figure, so the inner card is not clipped', () => {
    // The base figure sets `overflow: hidden`. Leaving its 6px radius on would
    // clip the placeholder's 10px corners even with the border gone — the
    // radius mismatch would survive as a squared-off inner card.
    const radius = winnerFor('image-block-missing', 'border-radius')
    expect(radius, 'a rule targeting .image-block-missing must set border-radius').not.toBeNull()
    expect(radius!.value, `figure radius came back via ${radius!.selector}`).toBe('0')
  })

  it('the placeholder keeps the dashed frame it is now the sole owner of', () => {
    // Guarding only the figure's removal would pass just as well if BOTH frames
    // were deleted, leaving a borderless smudge. The non-fill variant is the
    // one mounted inside a figure; `--fill` (viewers with nothing else to show)
    // deliberately has no frame of its own.
    const border = winnerFor('media-load-error', 'border', { exclude: /--fill/ })
    expect(border, 'the placeholder must still declare a border').not.toBeNull()
    expect(border!.value, `placeholder frame lost: ${border!.value}`).toMatch(/dashed/)
  })
})
