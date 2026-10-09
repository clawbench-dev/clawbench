import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The AskUserQuestion option indicator must be a CSS-DRAWN box, never a glyph.
 *
 * The defect this pins: the indicator used to be a character — ◯/● for the
 * radio, ☐/☑ for the checkbox — and the two states rendered at different sizes.
 * The user reported the selected and unselected circles as visibly mismatched.
 *
 * The root cause is that a glyph's rendered size is a property of the FONT, so
 * no pair can be kept size-matched across fonts. Measured ink size at 40pt:
 *
 *              DejaVu Sans    Noto Sans CJK
 *   ◯ U+25EF      41px            38px
 *   ○ U+25CB      31px            36px
 *   ● U+25CF      31px            36px
 *
 * So a swap that looks correct in one font is invisible in another — exactly
 * the trap a previous "fix" fell into (it swapped ◉→● believing ◯ shared ●'s
 * glyph box, when ◯ is the *large* variant). A fixed box is identical in every
 * font by construction, which is why the markup now emits an empty span.
 *
 * Guards:
 *   1. No circle/box glyph may appear in the card renderer or its fixtures.
 *   2. The renderer must emit the empty CSS-variant span.
 *   3. The global sheet must own the box geometry (size + ring), so both
 *      consumers agree and neither scoped block can desync one state.
 *   4. The outer box must NOT change size between states — that is the whole
 *      point of the fix.
 */
describe('ask-option indicator is CSS-drawn, not a glyph', () => {
  const RENDERER = 'src/utils/renderToolDetail.ts'
  const GLOBAL_CSS = 'css/components.css'
  const CONTENT_BLOCKS = 'src/components/chat/ContentBlocks.vue'
  const DRAWER = 'src/components/chat/ToolDetailDrawer.vue'

  /** The glyphs that must never come back into the card markup. */
  const FORBIDDEN_GLYPHS = ['◯', '○', '●', '☐', '☑']

  it('emits an empty variant span, with no glyph inside', () => {
    const src = readWebFile(RENDERER)
    expect(src).toContain('ask-option-indicator--radio')
    expect(src).toContain('ask-option-indicator--checkbox')
    // The rendered span is empty: `<span class="ask-option-indicator ..."></span>`.
    expect(src).toMatch(/<span class="ask-option-indicator \$\{indicatorVariant\}"><\/span>/)
  })

  it('never emits a circle or ballot-box glyph in the ask-card renderer', () => {
    const src = readWebFile(RENDERER)
    // Scope to renderAskUserQuestion's body: renderTodoWrite (a different tool's
    // static list) legitimately still uses ○ as its pending icon, and this guard
    // is about the ask card only.
    const start = src.indexOf('function renderAskUserQuestion(')
    expect(start).toBeGreaterThan(-1)
    const end = src.indexOf('\nfunction ', start + 1)
    const body = src.slice(start, end === -1 ? undefined : end)
    // Comments may mention the glyphs (they document why they were removed);
    // strip line comments before checking, so only real code is scanned.
    const code = body
      .split('\n')
      .map((line) => line.replace(/\/\/.*$/, ''))
      .join('\n')
    for (const glyph of FORBIDDEN_GLYPHS) {
      expect(code).not.toContain(glyph)
    }
  })

  it('draws the box globally with a fixed size', () => {
    const css = readWebFile(GLOBAL_CSS)
    const rule = css.match(/\n\.ask-option-indicator\s*\{([^}]*)\}/)
    if (!rule) throw new Error('missing .ask-option-indicator rule in components.css')
    const body = rule[1]
    expect(body).toMatch(/width:\s*16px/)
    expect(body).toMatch(/height:\s*16px/)
    expect(body).toMatch(/border:\s*1\.5px solid currentColor/)
  })

  it('grows an inner dot / check without resizing the outer box', () => {
    const css = readWebFile(GLOBAL_CSS)
    // The selected state is painted by a pseudo-element, so the outer box's
    // width/height are untouched — the two states cannot diverge in size.
    const radioSelected = css.match(
      /\.ask-question-option\.selected \.ask-option-indicator--radio::after\s*\{([^}]*)\}/,
    )
    if (!radioSelected) throw new Error('missing selected radio ::after rule')
    expect(radioSelected[1]).toContain('opacity: 1')

    const checkboxSelected = css.match(
      /\.ask-question-option\.selected \.ask-option-indicator--checkbox\s*\{([^}]*)\}/,
    )
    if (!checkboxSelected) throw new Error('missing selected checkbox rule')
    expect(checkboxSelected[1]).toContain('background: currentColor')
  })

  it.each([CONTENT_BLOCKS, DRAWER])('%s keeps only colour in its scoped rule', (file) => {
    const src = readWebFile(file)
    // A scoped selector outranks the global class, so re-declaring geometry
    // there would silently desync the two states again. Only colour is allowed.
    const scoped = src.match(/\n[^\n{]*\.ask-option-indicator\s*\{([^}]*)\}/g) ?? []
    expect(scoped.length).toBeGreaterThan(0)
    for (const rule of scoped) {
      expect(rule).not.toMatch(/width:|height:|border:|border-radius:|font-size:/)
    }
  })
})
