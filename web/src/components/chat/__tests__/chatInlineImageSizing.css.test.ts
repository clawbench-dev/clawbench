import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Chat inline images must fill their `.image-block-wrapper` figure with no
 * band above/below and no left/right crop.
 *
 * Two defects lived in the `img.chat-img` rule (markdown-common.css):
 *
 *  1. `margin: var(--space-2) 0` (= 4px) left a 4px band at the top AND bottom
 *     inside the figure. The shared cancel rule in media-block.css
 *     (`.image-block-wrapper .lightbox-img-wrap img`, specificity 0-2-1) was
 *     written for exactly this — its comment says so — but the chat rule is
 *     0-3-1, so the cancel silently lost and only chat kept the band. Measured
 *     in Chrome: figure 200x208 with the image 200x200, gap 4px top + 4px
 *     bottom. The file preview (no competing rule) was correctly flush.
 *
 *  2. `height: 200px` + `object-fit: cover` cropped every non-square image to
 *     a square. Measured: a 16:9 source rendered at ratio 1.0 (should be 1.778)
 *     and a 2:1 source at 1.0 (should be 2.0).
 *
 * The trap this guards: `contain` alone is NOT enough — with a fixed
 * `height: 200px` it letterboxes the picture INSIDE a 200x200 box, so the band
 * stays (now un-collapsible). `height` must be `auto` so the img element hugs
 * the picture. jsdom has no layout engine, so these are source-contract checks
 * (the same pattern markdownFirstBlockMargin / chatVhtmlStyleScope use).
 */

/** Strip comments — the files document the very selectors they define. */
const stripComments = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, '')

const markdownCss = stripComments(readWebFile('css/markdown-common.css'))
const mediaBlockCss = stripComments(readWebFile('css/media-block.css'))

interface Rule {
  selector: string
  decls: string
}

/** Every declaration block, with each selector of a comma list reported alone. */
function rulesOf(css: string): Rule[] {
  const out: Rule[] = []
  for (const m of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    for (const selector of m[1].split(',').map((s) => s.replace(/\s+/g, ' ').trim())) {
      if (selector) out.push({ selector, decls: m[2] })
    }
  }
  return out
}

/**
 * Specificity as [ids, classes, elements]. Class count includes attribute and
 * pseudo-class selectors; `::before` style pseudo-elements count as elements.
 */
function specificity(selector: string): [number, number, number] {
  const ids = (selector.match(/#[\w-]+/g) ?? []).length
  const classes =
    (selector.match(/\.[\w-]+/g) ?? []).length +
    (selector.match(/\[[^\]]*\]/g) ?? []).length +
    (selector.match(/(?<!:):(?!:)[\w-]+/g) ?? []).length
  const elements =
    (selector.match(/(?:^|[\s>+~])[a-zA-Z][\w-]*/g) ?? []).length +
    (selector.match(/::[\w-]+/g) ?? []).length
  return [ids, classes, elements]
}

/** Lexicographic specificity comparison. */
function outranks(a: [number, number, number], b: [number, number, number]): boolean {
  for (let i = 0; i < 3; i++) {
    if (a[i] !== b[i]) return a[i] > b[i]
  }
  return false
}

/**
 * The rule that sizes the chat inline image (`.chat-img` inside a bubble).
 *
 * It is a two-selector list (`.chat-message.assistant img.chat-img,
 * .chat-message.user img.chat-img`), so `rulesOf` reports it twice with
 * identical declarations. Return the shared declarations plus the
 * highest-specificity selector, which is what the margin cancel must outrank.
 */
function chatImgRule(): { decls: string; selector: string } {
  const found = rulesOf(markdownCss).filter(
    (r) => /\.chat-img\b/.test(r.selector) && /max-height|height|object-fit/.test(r.decls),
  )
  expect(found.length, 'a .chat-img sizing rule must exist').toBeGreaterThan(0)

  const decls = new Set(found.map((r) => r.decls.replace(/\s+/g, ' ').trim()))
  expect(decls.size, 'the .chat-img selector list must share one declaration block').toBe(1)

  const strongest = found.reduce((best, r) =>
    outranks(specificity(r.selector), specificity(best.selector)) ? r : best,
  )
  return { decls: found[0].decls, selector: strongest.selector }
}

/** Rules that zero an image's margin inside a block figure. */
function figureMarginCancels(): Rule[] {
  return rulesOf(mediaBlockCss).filter(
    (r) => /\.image-block-wrapper\b/.test(r.selector) && /\bimg\b/.test(r.selector) && /margin\s*:\s*0/.test(r.decls),
  )
}

describe('chat inline image sizing (markdown-common.css + media-block.css)', () => {
  it('does not pin a fixed height (contain + fixed height would letterbox)', () => {
    const decls = chatImgRule().decls
    // `height: 200px` is the defect: with `contain` it keeps a 200x200 box and
    // the band survives inside it. Match the `height` property only — the
    // deliberate `max-height` cap is asserted separately below.
    expect(decls, 'height must be auto, not a fixed px value').toMatch(/(?<!max-)height\s*:\s*auto/)
    expect(decls, 'no fixed pixel height may remain').not.toMatch(/(?<!max-)height\s*:\s*\d+px/)
  })

  it('scales the image without cropping it', () => {
    // `cover` cropped 16:9 and 2:1 sources down to 1:1 (measured). The ratio
    // must be preserved, which needs `contain` once height is auto.
    expect(chatImgRule().decls).toMatch(/object-fit\s*:\s*contain/)
    expect(chatImgRule().decls).not.toMatch(/object-fit\s*:\s*cover/)
  })

  it('keeps a height cap so a tall image cannot run away', () => {
    expect(chatImgRule().decls).toMatch(/max-height\s*:\s*\d/)
  })

  it('cancels the 4px band with a selector that actually outranks the chat rule', () => {
    const chat = chatImgRule()
    const chatSpec = specificity(chat.selector)

    const cancels = figureMarginCancels()
    expect(cancels.length, 'a figure-scoped margin:0 rule must exist').toBeGreaterThan(0)

    // The cancel must apply to chat bubbles AND beat the chat rule on
    // specificity — a tie would be decided by stylesheet load order, which is
    // not part of the contract and silently flipped the band on before.
    const winning = cancels.filter(
      (r) => /\.chat-message\b/.test(r.selector) && outranks(specificity(r.selector), chatSpec),
    )
    expect(
      winning.map((r) => r.selector),
      `the chat margin cancel must outrank ${chat.selector} (${chatSpec.join('-')})`,
    ).not.toEqual([])
  })

  it('leaves the shared file-preview cancel intact', () => {
    // The generic 0-2-1 rule is what keeps the file preview flush; it must not
    // be replaced by a chat-only selector.
    const generic = figureMarginCancels().filter((r) => !/\.chat-message\b/.test(r.selector))
    expect(generic.length, 'the generic figure margin cancel must remain').toBeGreaterThan(0)
  })
})
