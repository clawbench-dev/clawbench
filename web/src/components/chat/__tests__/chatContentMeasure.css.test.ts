import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The chat pane has ONE reading measure, and the shapes it produces are the
 * mirror image of "is there a gutter?":
 *
 *   pane ≤ measure  → no side gutter: full-bleed band, flat composer strip
 *   pane > measure  → side gutter:   rounded reply card, composer dock island
 *
 * Why this is guarded: the pane used to be full-bleed. On a 2560px monitor a
 * reply line ran the whole width, which is unreadable, and the composer was a
 * 2500px-wide input box. Capping only the message column would have left the
 * input and the stacked cards (plan / team / queued) at full width, so the
 * screen would show three different left edges.
 *
 * The rules that make it work, each easy to break in isolation:
 *
 *  1. The measure is a TOKEN (`--chat-content-max-width`); the centring is
 *     derived from it by gutter tokens. A consumer that re-derives
 *     `(100% - 900px) / 2` itself drifts the moment the measure changes.
 *  2. Each gutter floors at the inset its box ALREADY had, so a pane narrower
 *     than the measure looks exactly as before: the message area stays flush to
 *     both edges (floor `0px`), the composer keeps its 8px inset, the stacked
 *     cards keep their 10px.
 *  3. It is spent as PADDING or MARGIN, never `max-width` + `margin: auto`.
 *     The stacked cards are `width: auto` inside a stretch flex column; a
 *     `max-width` that stops binding would silently drop their gutter.
 *  4. It is spent on the SCROLLER, not on the list inside it. The list's `100%`
 *     is narrowed by the scrollbar, so centring there leaves every message 5px
 *     left of the input box.
 *  5. The SHAPES flip on a CONTAINER query, not a viewport media query: the
 *     split view can hand the chat pane any width regardless of the window, and
 *     the threshold is `calc(60rem + 1px)` — 60rem IS the measure, since `--dp`
 *     is 1rem/15. A px threshold would stop following the UI scale.
 *  6. Square corners at full width, rounded corners once there is a gutter.
 *     A rounded band touching both edges looks clipped; a square band ending in
 *     mid-air looks hard-cut. Each shape is correct only for its own case.
 *  7. The chat TITLE BAR stays full width. It is window chrome (its bottom
 *     border and the rename affordance belong to the pane), not content.
 *
 * jsdom has no CSS engine, so these are source-contract checks (same pattern as
 * chatCompositingBoundary.css.test.ts). The container-query behaviour itself is
 * covered by e2e/specs/chat-reading-measure.spec.ts, which measures real boxes.
 */

const VARS = readWebFile('css/variables.css')
const LIST = readWebFile('src/components/chat/ChatMessageList.vue')
const ITEM = readWebFile('src/components/chat/ChatMessageItem.vue')
const INPUT = readWebFile('src/components/chat/ChatInputBar.vue')
const PLAN = readWebFile('src/components/chat/PlanPanel.vue')
const TEAM = readWebFile('src/components/chat/TeamPanel.vue')
const QUEUED = readWebFile('src/components/chat/QueuedMessageBar.vue')
const PANEL = readWebFile('src/components/chat/ChatPanelContent.vue')
const APP = readWebFile('src/App.vue')
const SHARE = readWebFile('src/share/SessionShareView.vue')

const stripComments = (s: string): string => s.replace(/\/\*[\s\S]*?\*\//g, '')

/** Declarations of the first rule whose selector is exactly `selector`. */
function decls(source: string, selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  // Anchored at a line start (multiline): a rule may follow `}` or open the
  // style block directly, so `^` without `m` would miss the first rule of a
  // component.
  const m = stripComments(source).match(
    new RegExp(`^\\s*${escaped}\\s*\\{([^}]*)\\}`, 'm'),
  )
  expect(m, `rule ${selector} must exist`).not.toBeNull()
  return m![1]
}

/** The body of the `@container chat-pane (…) { … }` block that holds `inner`. */
function containerBlock(source: string, inner: string): string {
  const code = stripComments(source)
  // Walk every container block, brace-matched, and return the first whose body
  // contains the inner selector. A plain regex would stop at the first `}` and
  // silently return a truncated body that can never match.
  const marker = '@container chat-pane'
  let from = 0
  for (;;) {
    const at = code.indexOf(marker, from)
    if (at === -1) break
    const open = code.indexOf('{', at)
    let depth = 0
    let end = -1
    for (let i = open; i < code.length; i++) {
      if (code[i] === '{') depth++
      else if (code[i] === '}') {
        depth--
        if (depth === 0) { end = i; break }
      }
    }
    const body = code.slice(open + 1, end)
    if (body.includes(inner)) return body
    from = end + 1
  }
  expect.unreachable(`no @container chat-pane block declares ${inner}`)
}

const WIDE_QUERY = 'calc(60rem + 1px)'

describe('chat reading measure', () => {
  it('declares the measure as a scaled token, not a literal px', () => {
    // A literal px cap would stop following the UI scale, so at 200% the column
    // would physically shrink relative to everything else on screen.
    const m = stripComments(VARS).match(/--chat-content-max-width:\s*([^;]+);/)
    expect(m, '--chat-content-max-width must be declared').not.toBeNull()
    expect(m![1]).toMatch(/^calc\(\s*\d+\s*\*\s*var\(--dp\)\s*\)$/)
  })

  it('matches the share page conversation column', () => {
    // The share page reuses ChatMessageItem and documents itself as mirroring
    // the chat area's measure. If the two numbers diverge, the same thread
    // reads at two different widths in-app and when shared.
    const share = stripComments(SHARE).match(
      /\.session-share-column\s*\{[^}]*max-width:\s*calc\(\s*(\d+)\s*\*\s*var\(--dp\)\s*\)/,
    )
    expect(share, 'the share column must keep its scaled measure').not.toBeNull()

    const chat = stripComments(VARS).match(
      /--chat-content-max-width:\s*calc\(\s*(\d+)\s*\*\s*var\(--dp\)\s*\)/,
    )
    expect(chat).not.toBeNull()
    expect(chat![1], 'chat and share must agree on the reading measure').toBe(share![1])
  })

  it('derives the message gutter from the measure, with a zero floor', () => {
    const m = stripComments(VARS).match(/--chat-content-inset:\s*([^;]+);/)
    expect(m, '--chat-content-inset must be declared').not.toBeNull()
    const value = m![1]
    // It must be the half-slack of the measure, not a second hard-coded number.
    expect(value).toMatch(/\(\s*100%\s*-\s*var\(--chat-content-max-width\)\s*\)\s*\/\s*2/)
    // The floor is `0px`, NOT `0`: a bare 0 is a <number>, so `max()` would drop
    // the whole declaration at computed-value time and the pane would lose its
    // inset entirely. See fontDrivenScaling.test.ts.
    expect(value).toMatch(/^max\(\s*0px\s*,/)
  })

  it('floors the composer and card gutters at their original insets', () => {
    // Narrow panes must look untouched. The composer always sat --space-4 in
    // and the stacked cards --space-5; a shared floor would silently change one
    // of them.
    for (const [name, space] of [
      ['--chat-content-gutter', '--space-4'],
      ['--chat-content-card-gutter', '--space-5'],
    ] as const) {
      const m = stripComments(VARS).match(new RegExp(`${name}:\\s*([^;]+);`))
      expect(m, `${name} must be declared`).not.toBeNull()
      expect(m![1]).toMatch(
        new RegExp(`^max\\(\\s*var\\(${space}\\)\\s*,\\s*\\(100% - var\\(--chat-content-max-width\\)\\)\\s*/\\s*2\\s*\\)$`),
      )
    }
  })
})

describe('every content box spends the shared gutter', () => {
  it('the message scroller is inset by it as PADDING, not margin', () => {
    // Padding keeps the scroller's own box at the pane's full width, so its
    // scrollbar is painted at the SESSION AREA's right edge. With
    // `margin-inline` the box narrowed and the bar moved inwards, ending up
    // glued to the message column — reported as "the scrollbar is in the wrong
    // place". The bar belongs to the pane, the gutter to the column.
    const scroller = decls(LIST, '.chat-messages')
    expect(scroller).toMatch(/padding:[^;]*var\(--chat-content-inset\)/)
    expect(
      scroller,
      'margin would move the scrollbar inwards, off the pane edge',
    ).not.toMatch(/margin-inline|margin:\s*[^;]*--chat-content-inset/)

    const list = decls(LIST, '.chat-messages-list')
    expect(
      list,
      'the list must not add its own inset — the scroller already provides it',
    ).not.toMatch(/--chat-content-(?:inset|max-width|gutter)/)
  })

  it('pays the scrollbar lane back on the END side only', () => {
    // A classic scrollbar is taken out of THIS element's layout box, so the
    // content box ends one bar-width short of the composer island's right edge
    // and the column drifts ~5px left of centre — reported as "the messages and
    // the input box are not aligned".
    //
    // The start side must NOT subtract it: doing so would shift the column LEFT
    // of the composer instead (verified in all three engines).
    const scroller = decls(LIST, '.chat-messages')
    const padding = scroller.match(/padding:\s*([^;]+);/)
    expect(padding, 'the scroller must declare padding').not.toBeNull()
    const parts = padding![1].trim()

    // Four-value form: top | end | bottom | start.
    expect(parts, 'the end padding must subtract the lane').toMatch(
      /var\(--space-6\)\s+max\(0px,\s*var\(--chat-content-inset\)\s*-\s*var\(--chat-scrollbar-lane\)\)\s+var\(--space-6\)\s+var\(--chat-content-inset\)$/,
    )
    // The start side carries the bare inset, with no lane subtraction.
    const startSide = parts.split(/\s+(?![^(]*\))/).pop()!
    expect(startSide, 'the start side must not subtract the lane').toBe(
      'var(--chat-content-inset)',
    )

    // And the lane must be reserved up front. Without `stable` the bar only
    // takes width once the thread overflows, so the column's right edge jumps
    // 10px the moment a conversation grows past one screen — the alignment
    // would be correct only for long threads.
    expect(scroller, 'the bar lane must be reserved before it appears').toMatch(
      /scrollbar-gutter:\s*stable/,
    )
  })

  it('measures the lane at runtime instead of hard-coding a bar width', () => {
    // The width is 0 on every overlay-scrollbar platform (macOS/iOS default,
    // most Android WebViews, headless Chromium), 10 for a classic bar, 4 on a
    // coarse pointer — and WebKit's `scrollbar-gutter: stable` only reserves it
    // once the content overflows. A CSS-declared constant is therefore wrong on
    // some platform no matter which number is picked, so the component measures
    // `offsetWidth - clientWidth` (exactly the box's layout loss) and writes the
    // variable itself.
    expect(stripComments(VARS), 'the token must have a 0px pre-measurement default').toMatch(
      /--chat-scrollbar-lane:\s*0px;/,
    )
    // …and no CSS rule may try to guess it (that is the bug this replaced).
    expect(
      stripComments(VARS),
      'a hard-coded bar width cannot be right on every platform',
    ).not.toMatch(/@supports selector\(::-webkit-scrollbar\)/)

    const code = stripComments(LIST)
    expect(code, 'the scroller must measure its own lane').toMatch(
      /offsetWidth\s*-\s*el\.clientWidth/,
    )
    expect(code, 'and publish it on the element the padding reads it from').toMatch(
      /setProperty\(\s*['"]--chat-scrollbar-lane['"]/,
    )
    // Re-measure on mount, on scroller resize, and when content growth makes the
    // bar appear (the WebKit case).
    expect(code, 'a ResizeObserver must keep it current').toMatch(
      /new ResizeObserver\(\(\)\s*=>\s*syncScrollbarLane\(\)\)/,
    )
    expect(code, 'the content-growth observer must re-sync it too').toMatch(
      /onContentGrown\(\)[\s\S]{0,400}?syncScrollbarLane\(\)/,
    )
    expect(code, 'the observer must be torn down with the component').toMatch(
      /scrollbarLaneObserver\?\.disconnect\(\)/,
    )
  })

  it('the composer block is inset by it', () => {
    // Padding on the wrapper, so the top hairline (inset box-shadow) still runs
    // edge to edge in the narrow case — a divider that stops mid-pane reads as
    // a broken border.
    const wrapper = decls(INPUT, '.chat-input-wrapper')
    expect(wrapper).toMatch(/padding:[^;]*var\(--chat-content-gutter\)/)
    // And the wrapper must stay full width for that hairline to reach the pane.
    expect(wrapper).not.toMatch(/max-width:\s*var\(--chat-content-max-width\)/)
  })

  it('the stacked cards are inset by it', () => {
    for (const [name, source, selector] of [
      ['plan', PLAN, '.plan-panel'],
      ['team', TEAM, '.team-panel'],
      ['queued', QUEUED, '.queued-bar'],
    ] as const) {
      const rule = decls(source, selector)
      expect(rule, `${name}: the card must use the shared card gutter`).toMatch(
        /margin:\s*0\s+var\(--chat-content-card-gutter\)/,
      )
      // A max-width cap would be inert here: the card is `width: auto` inside a
      // stretch flex column, so the moment the cap stops binding the gutter
      // disappears too.
      expect(rule, `${name}: use the gutter, not a max-width cap`).not.toMatch(
        /max-width/,
      )
    }
  })

  it('no consumer re-derives the centring formula by hand', () => {
    // The drift this prevents: five copies of `(100% - 900px) / 2` that stop
    // agreeing the day the measure changes.
    const offenders: string[] = []
    for (const [name, source] of [
      ['ChatMessageList.vue', LIST],
      ['ChatInputBar.vue', INPUT],
      ['PlanPanel.vue', PLAN],
      ['TeamPanel.vue', TEAM],
      ['QueuedMessageBar.vue', QUEUED],
    ] as const) {
      if (/\(\s*100%\s*-\s*var\(--chat-content-max-width\)\s*\)/.test(stripComments(source))) {
        offenders.push(name)
      }
    }
    expect(
      offenders,
      'the half-slack formula lives in the tokens only; consumers use the token',
    ).toEqual([])
  })
})

describe('the wide layout is a CONTAINER query, not a viewport one', () => {
  it('declares the chat pane as an inline-size container', () => {
    // The split view hands the chat pane its width independently of the window,
    // so a `@media` breakpoint would flip the shapes at the wrong moment.
    // `inline-size` (not `size`): height must stay content/flex driven, and
    // size containment would zero it.
    const panel = decls(PANEL, '.chat-panel-content')
    expect(panel).toMatch(/container-type:\s*inline-size/)
    expect(panel).toMatch(/container-name:\s*chat-pane/)
    expect(panel, 'size containment would collapse the panel height').not.toMatch(
      /container-type:\s*size/,
    )
  })

  it('uses the measure itself as the threshold, in rem', () => {
    // 60rem = 900 design px because --dp is 1rem/15, and rem follows the ROOT
    // font size inside a container query (verified in all three engines), so
    // the threshold tracks the UI scale. A px literal would not.
    expect(stripComments(ITEM)).toContain(`@container chat-pane (min-width: ${WIDE_QUERY})`)
    expect(stripComments(INPUT)).toContain(`@container chat-pane (min-width: ${WIDE_QUERY})`)

    // And 60rem must actually be the measure: 900 / 15 = 60.
    const measure = stripComments(VARS).match(
      /--chat-content-max-width:\s*calc\(\s*(\d+)\s*\*\s*var\(--dp\)\s*\)/,
    )
    expect(measure).not.toBeNull()
    expect(Number(measure![1]) / 15, 'the rem threshold must equal the measure').toBe(60)
  })

  it('never gates the wide shapes on a viewport media query', () => {
    // A `@media (min-width: …)` here would fire on window width while the pane
    // is narrow (split view) — exactly the mismatch this replaced.
    for (const [name, source] of [
      ['ChatMessageItem.vue', ITEM],
      ['ChatInputBar.vue', INPUT],
    ] as const) {
      const code = stripComments(source)
      for (const m of code.matchAll(/@media[^{]*min-width[^{]*\{/g)) {
        expect(m[0], `${name} must not gate shapes on the viewport`).not.toMatch(
          /min-width:\s*(?:calc\(\s*)?\d+(?:\.\d+)?rem/,
        )
      }
    }
  })
})

describe('the shapes mirror the gutter', () => {
  it('keeps the assistant bubble square while it is full-bleed', () => {
    // At or below the measure there is no side gutter, so the band touches both
    // edges. Rounding it there would look clipped by the pane.
    const card = decls(ITEM, '.chat-message.assistant .msg-card')
    expect(card, 'the base band must stay square').toMatch(/border-radius:\s*0\s*;/)
  })

  it('rounds the assistant bubble once there is a gutter', () => {
    // A square band ending in mid-air reads as a hard-cut slab; the corner is
    // what makes the narrower edge look deliberate.
    const wide = containerBlock(ITEM, '.chat-message.assistant .msg-card')
    expect(wide).toMatch(/border-radius:\s*var\(--radius-(?:md|lg)\)/)
  })

  it('turns the composer into a bottom-docked island once there is a gutter', () => {
    const wide = containerBlock(INPUT, '.chat-input-wrapper')
    // Inset on both sides by the SAME gutter as the message column, so the two
    // columns share their edges.
    expect(wide).toMatch(/margin:\s*0\s+var\(--chat-content-gutter\)/)
    // Docked to the pane's bottom edge: top corners rounded, bottom square.
    // Rounding all four would leave the island visibly floating.
    expect(wide).toMatch(
      /border-radius:\s*var\(--radius-lg\)\s+var\(--radius-lg\)\s+0\s+0/,
    )
    expect(wide, 'the bottom edge is the pane edge, so no border there').toMatch(
      /border-bottom:\s*none/,
    )
    // Its own top border replaces the full-width hairline.
    expect(wide).toMatch(/box-shadow:\s*none/)
  })

  it('tints the island with a text mix, not a fixed background step', () => {
    // 15 of the 36 themes have a secondary↔tertiary contrast under 1.12
    // (ayu-dark: 1.062), so an island on `--bg-tertiary` disappears in them.
    // Mixing --text-primary into --bg-secondary is the direction-guaranteed
    // recipe from design-guide 红线 4.
    const wide = containerBlock(INPUT, '.chat-input-wrapper')
    expect(wide).toContain('color-mix(')
    expect(wide).toContain('var(--text-primary)')
    expect(wide).toContain('var(--bg-secondary)')
    for (const bad of ['--bg-tertiary', '--bg-elevated']) {
      expect(wide, `${bad} is not a reliable ramp across themes`).not.toContain(bad)
    }
    // It must still honour the wallpaper's alpha, or the island becomes the one
    // opaque block floating over a translucent UI.
    expect(wide).toContain('var(--panel-alpha)')
  })
})

describe('chrome stays full width', () => {
  it('leaves the chat title bar full width', () => {
    // Window chrome, not content: its bottom border and the rename affordance
    // belong to the pane, and a title bar that stops mid-pane looks broken.
    const bar = decls(APP, '.chat-title-bar')
    expect(bar).not.toMatch(/--chat-content-(?:inset|max-width|gutter|card-gutter)/)
    expect(bar).not.toMatch(/max-width/)
  })
})
