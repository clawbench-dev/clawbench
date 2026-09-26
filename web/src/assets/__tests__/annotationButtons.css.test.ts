import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Cascade contract for the inert-path styling in assets/annotation-buttons.css.
 *
 * Why this exists: an inert path (`chat-file-path-inert`, added in issue #501
 * for verified-missing paths and glob patterns) must look *different* from a
 * working chip. The live chip fill is set by ANCESTOR-scoped rules that outrank
 * a bare `.chat-file-path-inert`:
 *
 *   .chat-message.assistant .chat-file-path   (0,3,0)
 *   .chat-message.user .chat-file-path        (0,3,0)
 *   .markdown-body .chat-file-path            (0,2,0)
 *   .table-row-value .chat-file-path          (0,2,0)
 *   .chat-file-path[data-file-path]           (0,2,0)  <- cursor:pointer
 *   .chat-file-path[data-file-path]:hover     (0,3,0)  <- 18% fill
 *
 * Inert elements deliberately KEEP `data-file-path` (so a later re-verify pass
 * can re-mark them) and stay inside the same ancestors, so all of the above
 * match them. With a plain `background: none` / `cursor: help` the inert rule
 * lost the cascade and the missing path kept the live chip's fill and pointer
 * cursor — exactly the "looks identical to a working link" symptom issue #501
 * set out to kill. The declarations that must win therefore carry
 * `!important`.
 *
 * jsdom has no CSS engine (no specificity resolution, no computed style), so a
 * rendered assertion cannot catch this. This is a source-sniffing test — the
 * same pattern as countBadge.css.test.ts / flashReducedMotion.css.test.ts —
 * but instead of just grepping for `!important` it resolves the real cascade:
 * it collects every rule that can match an inert element, computes specificity,
 * and asserts the inert declaration wins. That way the guard stays honest if a
 * competitor is added, renamed, or moved.
 *
 * The CSS under test lives in two roots: assets/*.css is inside the vitest
 * source root (web/src) and is `?raw`-imported; web/css/*.css is outside it and
 * is read off disk. The cwd differs between a bare `vitest` run (web/) and
 * scripts/vitest-run.sh (repo root), so both are probed.
 */

function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

// ── Mini CSS cascade resolver ───────────────────────────────────────────────
//
// Deliberately narrow: it understands exactly the selector grammar used by the
// rules in play (descendant combinators, element names, classes, `[attr]`
// presence, and the `:hover` pseudo-class). It is not a general CSS engine.

interface El {
  tag: string
  classes: string[]
  attrs?: string[]
  hover?: boolean
}

interface Rule {
  /** Compound selector parts, left → right. */
  parts: Compound[]
  decls: string
  order: number
}

interface Compound {
  tag: string | null
  classes: string[]
  attrs: string[]
  hover: boolean
}

/** Parse one compound selector (`a.foo.bar[data-x]:hover`) into its parts. */
function parseCompound(text: string): Compound {
  const hover = /:hover\b/.test(text)
  const withoutPseudo = text.replace(/:[a-zA-Z-]+(\([^)]*\))?/g, '')
  const tagMatch = withoutPseudo.match(/^[a-zA-Z][\w-]*/)
  return {
    tag: tagMatch ? tagMatch[0].toLowerCase() : null,
    classes: [...withoutPseudo.matchAll(/\.([\w-]+)/g)].map((m) => m[1]),
    attrs: [...withoutPseudo.matchAll(/\[([\w-]+)/g)].map((m) => m[1]),
    hover,
  }
}

/** Split a selector into compound parts; null when unsupported (e.g. @media). */
function parseSelector(selector: string): Compound[] | null {
  const s = selector.trim()
  if (!s || s.includes('@') || /[>+~,()]/.test(s)) return null
  const parts = s.split(/\s+/).map(parseCompound)
  return parts.length > 0 ? parts : null
}

function matchesCompound(c: Compound, el: El): boolean {
  if (c.tag && c.tag !== el.tag.toLowerCase()) return false
  if (c.classes.some((cl) => !el.classes.includes(cl))) return false
  if (c.attrs.some((a) => !(el.attrs ?? []).includes(a))) return false
  if (c.hover && !el.hover) return false
  return true
}

/** Right-to-left descendant matching; `chain` is root → target. */
function selectorMatches(parts: Compound[], chain: El[]): boolean {
  let i = chain.length - 1
  let p = parts.length - 1
  if (i < 0 || !matchesCompound(parts[p], chain[i])) return false
  p--
  i--
  while (p >= 0) {
    let found = false
    while (i >= 0) {
      if (matchesCompound(parts[p], chain[i])) {
        found = true
        i--
        break
      }
      i--
    }
    if (!found) return false
    p--
  }
  return true
}

/** [ids, classes+attrs+pseudo, elements]. */
function specificity(parts: Compound[]): [number, number, number] {
  let b = 0
  let c = 0
  for (const p of parts) {
    b += p.classes.length + p.attrs.length + (p.hover ? 1 : 0)
    if (p.tag) c += 1
  }
  return [0, b, c]
}

function compareSpec(a: [number, number, number], b: [number, number, number]): number {
  for (let i = 0; i < 3; i++) {
    if (a[i] !== b[i]) return a[i] - b[i]
  }
  return 0
}

function declValue(decls: string, prop: string): string | null {
  const m = decls.match(new RegExp(`(?:^|;)\\s*${prop}\\s*:\\s*([^;]+)`))
  return m ? m[1].trim() : null
}

/**
 * Resolve which declaration wins for `prop` on the target element.
 * `!important` outranks normal declarations; otherwise specificity, then order.
 */
function resolve(
  rules: Rule[],
  chain: El[],
  prop: string,
): { value: string; important: boolean } | null {
  let best: { value: string; important: boolean; spec: [number, number, number]; order: number } | null =
    null
  for (const rule of rules) {
    if (!selectorMatches(rule.parts, chain)) continue
    const raw = declValue(rule.decls, prop)
    if (raw === null) continue
    const important = /!important/.test(raw)
    const value = raw.replace(/\s*!important\s*$/, '').trim()
    const spec = specificity(rule.parts)
    if (
      !best ||
      (important && !best.important) ||
      (important === best.important &&
        (compareSpec(spec, best.spec) > 0 ||
          (compareSpec(spec, best.spec) === 0 && rule.order > best.order)))
    ) {
      best = { value, important, spec, order: rule.order }
    }
  }
  return best ? { value: best.value, important: best.important } : null
}

// ── Collect the rules ───────────────────────────────────────────────────────

const GLOBAL_SOURCES = [
  'src/assets/annotation-buttons.css',
  'src/assets/code-viewer.css',
  'css/markdown-common.css',
]

/** ChatMessageItem.vue has an unscoped <style> block that also styles chips. */
function readChatMessageItemGlobalCss(): string {
  const src = readWebFile('src/components/chat/ChatMessageItem.vue')
  const blocks = [...src.matchAll(/<style([^>]*)>([\s\S]*?)<\/style>/g)]
  // Concatenate only the unscoped blocks (no `scoped` attribute).
  return blocks
    .filter((b) => !/\bscoped\b/.test(b[1]))
    .map((b) => b[2])
    .join('\n')
}

function collectRules(): Rule[] {
  const sheets = [
    ...GLOBAL_SOURCES.map((p) => ({ path: p, css: readWebFile(p) })),
    { path: 'ChatMessageItem.vue(<style>)', css: readChatMessageItemGlobalCss() },
  ]
  const rules: Rule[] = []
  let order = 0
  for (const { css } of sheets) {
    const clean = css.replace(/\/\*[\s\S]*?\*\//g, '')
    for (const m of clean.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
      for (const one of m[1].split(',')) {
        const parts = parseSelector(one)
        if (!parts) continue
        rules.push({ parts, decls: m[2], order: order++ })
      }
    }
  }
  return rules
}

const rules = collectRules()

/**
 * Root → target chains for the surfaces that render an inert path.
 *
 * Two DOM shapes occur:
 *  - verified-missing: the element KEEPS `chat-file-path` + `data-file-path`
 *    (so re-verify can re-mark it) and gains the inert class.
 *  - glob pattern: `markInertLink` fires before the annotation class is added,
 *    so the element is a BARE `chat-file-path-inert` with no `chat-file-path`
 *    and no `data-file-path` — the generic anchor rules are its only styling.
 */
const SURFACES: { name: string; chain: El[] }[] = [
  {
    name: 'assistant chat <code>',
    chain: [
      { tag: 'div', classes: ['chat-message', 'assistant'] },
      { tag: 'code', classes: ['chat-file-path', 'chat-file-path-inert'], attrs: ['data-file-path', 'data-path-type'] },
    ],
  },
  {
    name: 'assistant chat <a>',
    chain: [
      { tag: 'div', classes: ['chat-message', 'assistant'] },
      { tag: 'a', classes: ['chat-file-path', 'chat-file-path-inert'], attrs: ['data-file-path', 'data-path-type'] },
    ],
  },
  {
    name: 'user chat <code>',
    chain: [
      { tag: 'div', classes: ['chat-message', 'user'] },
      { tag: 'code', classes: ['chat-file-path', 'chat-file-path-inert'], attrs: ['data-file-path', 'data-path-type'] },
    ],
  },
  {
    name: 'markdown body <code>',
    chain: [
      { tag: 'div', classes: ['markdown-body'] },
      { tag: 'code', classes: ['chat-file-path', 'chat-file-path-inert'], attrs: ['data-file-path', 'data-path-type'] },
    ],
  },
  {
    name: 'table row value <code>',
    chain: [
      { tag: 'div', classes: ['table-row-value'] },
      { tag: 'code', classes: ['chat-file-path', 'chat-file-path-inert'], attrs: ['data-file-path', 'data-path-type'] },
    ],
  },
  {
    // Glob pattern in assistant chat: a BARE inert anchor. No `chat-file-path`
    // class means no chip fill rule matches, but the generic link rules
    // (`.chat-message.assistant a` / `:hover`) still do — this is the shape
    // that makes the `a.`-prefixed selector necessary.
    name: 'glob-pattern <a> (bare inert)',
    chain: [
      { tag: 'div', classes: ['chat-message', 'assistant'] },
      { tag: 'a', classes: ['chat-file-path-inert'] },
    ],
  },
  {
    // Glob pattern in a user bubble: `.chat-message.user a` is (0,3,0) with a
    // white colour, and `:hover` at (0,3,0) would underline it.
    name: 'glob-pattern <a> in user bubble',
    chain: [
      { tag: 'div', classes: ['chat-message', 'user'] },
      { tag: 'a', classes: ['chat-file-path-inert'] },
    ],
  },
  {
    // The code viewer's own annotation class. Nothing creates it today, but
    // verifyFilePaths still re-marks `.code-file-path[data-file-path]` and the
    // inert selector names `.code-file-path.chat-file-path-inert` explicitly.
    // code-viewer.css loads AFTER annotation-buttons.css, so on a specificity
    // tie its `.code-file-path:hover` (0,2,0) would win by source order.
    name: 'code viewer <span>.code-file-path',
    chain: [
      { tag: 'pre', classes: ['code-viewer'] },
      { tag: 'span', classes: ['code-file-path', 'chat-file-path-inert'], attrs: ['data-file-path', 'data-path-type'] },
    ],
  },
]

function chainWithHover(chain: El[]): El[] {
  const out = chain.map((e) => ({ ...e }))
  out[out.length - 1].hover = true
  return out
}

describe('inert path wins the background cascade on every surface', () => {
  it('finds the competing chip rules it is meant to guard', () => {
    // Sanity: the ancestor-scoped fill rules must actually be present, else the
    // resolver would trivially pass while the real cascade went untested.
    const fills = rules.filter(
      (r) =>
        declValue(r.decls, 'background') !== null &&
        r.parts.some((p) => p.classes.includes('chat-file-path')),
    )
    expect(fills.length).toBeGreaterThanOrEqual(3)
  })

  it('every surface resolves background to none (no live chip fill)', () => {
    const offenders: string[] = []
    for (const { name, chain } of SURFACES) {
      const won = resolve(rules, chain, 'background')
      if (!won || won.value !== 'none') {
        offenders.push(`${name}: background => ${won ? won.value : '(none declared)'}`)
      }
    }
    expect(offenders, `inert path kept the live chip fill:\n${offenders.join('\n')}`).toEqual([])
  })

  it('the winning background declaration is !important', () => {
    // Guards the mechanism, not just the outcome: an ancestor rule added later
    // with equal-or-higher specificity would silently take back the fill
    // without !important.
    const offenders: string[] = []
    for (const { name, chain } of SURFACES) {
      const won = resolve(rules, chain, 'background')
      if (!won?.important) offenders.push(`${name}: background=${won?.value} (not !important)`)
    }
    expect(offenders, offenders.join('\n')).toEqual([])
  })

  it('hover keeps only a faint tint, never a live-chip fill', () => {
    const offenders: string[] = []
    // Live-chip fills that must NOT win on hover: the chat chip's 18% hover
    // fill, the code viewer's yellow hover, plus the resting fills
    // (--bg-secondary / the user-bubble overlay) that a higher-specificity
    // ancestor rule would otherwise contribute.
    const liveFills = /18%|var\(--bg-secondary\)|rgba\(0, 0, 0, 0\.15\)|rgba\(255, 230, 0|rgba\(230, 126, 34/
    for (const { name, chain } of SURFACES) {
      const won = resolve(rules, chainWithHover(chain), 'background')
      if (!won || liveFills.test(won.value)) {
        offenders.push(`${name}: hover background => ${won ? won.value : '(none declared)'}`)
      }
      if (won && !won.important) offenders.push(`${name}: hover background not !important`)
    }
    expect(offenders, offenders.join('\n')).toEqual([])
  })
})

describe('inert path wins the cursor cascade', () => {
  it('resolves cursor to help, not the live pointer affordance', () => {
    // Inert elements keep `data-file-path`, so `.chat-file-path[data-file-path]
    // { cursor: pointer }` matches them; the state must deny that affordance.
    const offenders: string[] = []
    for (const { name, chain } of SURFACES) {
      const won = resolve(rules, chain, 'cursor')
      if (!won || won.value !== 'help') {
        offenders.push(`${name}: cursor => ${won ? won.value : '(none declared)'}`)
      }
    }
    expect(offenders, `inert path kept the pointer cursor:\n${offenders.join('\n')}`).toEqual([])
  })

  it('the winning cursor declaration is !important', () => {
    const offenders: string[] = []
    for (const { name, chain } of SURFACES) {
      const won = resolve(rules, chain, 'cursor')
      if (!won?.important) offenders.push(`${name}: cursor=${won?.value} (not !important)`)
    }
    expect(offenders, offenders.join('\n')).toEqual([])
  })
})

describe('inert path wins the colour cascade', () => {
  it('resolves colour to the muted token, never the link accent', () => {
    // The generic link rules paint anchors accent/white and are MORE specific
    // than `a.chat-file-path-inert`:
    //   .markdown-body a           (0,1,1) vs a.chat-file-path-inert (0,1,1)
    //   .chat-message.assistant a  (0,2,1)  <- strictly wins without !important
    //   .chat-message.user a       (0,2,1)  <- white on the blue bubble
    // A bare inert anchor (glob pattern) is the shape most exposed to these.
    const offenders: string[] = []
    const linkColours = /var\(--accent-color\)|#b8daff|#9dc5f0/
    for (const { name, chain } of SURFACES) {
      for (const [state, c] of [
        ['rest', chain],
        ['hover', chainWithHover(chain)],
      ] as const) {
        const won = resolve(rules, c, 'color')
        if (!won || linkColours.test(won.value)) {
          offenders.push(`${name} (${state}): color => ${won ? won.value : '(none declared)'}`)
        }
        if (won && !won.important) {
          offenders.push(`${name} (${state}): color=${won.value} (not !important)`)
        }
      }
    }
    expect(offenders, `inert path kept the link colour:\n${offenders.join('\n')}`).toEqual([])
  })
})

describe('inert path stays visibly non-interactive', () => {
  it('resolves text-decoration to none, never the link underline', () => {
    // The generic link rules underline on hover (`.markdown-body a:hover`,
    // `.chat-message.assistant a:hover` at (0,2,0); `.chat-message.user a:hover`
    // at (0,3,0)). An inert <a> must not pick that up — the dashed border is the
    // only underline the state is allowed to show.
    const offenders: string[] = []
    for (const { name, chain } of SURFACES) {
      for (const [state, c] of [
        ['rest', chain],
        ['hover', chainWithHover(chain)],
      ] as const) {
        const won = resolve(rules, c, 'text-decoration')
        if (!won || won.value !== 'none') {
          offenders.push(`${name} (${state}): text-decoration => ${won ? won.value : '(none declared)'}`)
        }
      }
    }
    expect(offenders, `inert path picked up a link underline:\n${offenders.join('\n')}`).toEqual([])
  })

  it('keeps the dashed underline and drops text-decoration', () => {
    const css = readWebFile('web/src/assets/annotation-buttons.css')
    const rule = css.match(/a\.chat-file-path-inert,[\s\S]*?\{([^}]*)\}/)
    expect(rule, 'the inert rule must exist').not.toBeNull()
    const decls = rule![1]
    expect(decls).toMatch(/border-bottom:\s*1px dashed/)
    expect(decls).toMatch(/text-decoration:\s*none/)
  })

  it('still mutes the colour with !important', () => {
    const css = readWebFile('web/src/assets/annotation-buttons.css')
    const decls = css.match(/a\.chat-file-path-inert,[\s\S]*?\{([^}]*)\}/)![1]
    const color = declValue(decls, 'color')
    expect(color).toMatch(/!important/)
  })
})
