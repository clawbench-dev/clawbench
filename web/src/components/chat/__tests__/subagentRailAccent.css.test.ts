import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guards the colour of the vertical rail down the left edge of an unfolded run
 * — both flavours of it:
 *
 *   • sub-agent group — the head pill (`.chat-tool-call-group-open`) and the
 *     body rail (`.subagent-group > .subagent-group-body-wrapper`);
 *   • tool run — the strip (`.chat-tool-group.is-open > .chat-tool-group-strip`)
 *     and the body rail (`.chat-run-body`).
 *
 * Both had the same class of defect: the head could not resolve the colour the
 * body used, so the two halves of one line disagreed.
 *
 * SUB-AGENT — the head pill and the group are SIBLINGS:
 *
 *   <div class="chat-tool-call-group-open">                   ← head
 *   <div class="subagent-group" style="--subagent-accent: …">  ← body rail
 *
 * `.subagent-group` declares the pink on ITSELF, so a bare
 * `var(--subagent-accent)` in the head's rule resolves to nothing. `color-mix()`
 * on an unresolvable variable is invalid-at-computed-value-time: the whole
 * declaration is dropped and the border falls back to currentColor. On a dark
 * theme that is a near-white sliver at the head's left edge while the rail below
 * it is correctly pink — reported as "why is there a white border there?".
 *
 * TOOL RUN — same 35%-alpha grey, two different backdrops. The head carries the
 * strip's tint, the body shows the message bubble through, so a translucent
 * border composites to two visibly different greys at the seam.
 *
 * jsdom has no CSS engine, so this is a source contract.
 */

const BLOCKS = 'src/components/chat/ContentBlocks.vue'

/** The single rule whose selector is exactly `selector` (anchored on `{`). */
function rule(src: string, selector: string): string | undefined {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  for (const block of src.matchAll(/<style([^>]*)>([\s\S]*?)<\/style>/g)) {
    const m = block[2].match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))
    if (m) return m[1]
  }
  return undefined
}

const source = readWebFile(BLOCKS)

describe('sub-agent rail accent', () => {
  it('declares the group accent with the same pink the head falls back to', () => {
    // Both sides of the seam must agree on the default. If one moves, the head
    // and the rail under it stop matching on an unattributed sub-agent.
    const group = rule(source, '.subagent-group')
    expect(group, 'missing ".subagent-group"').toBeDefined()
    const declared = /--subagent-accent:\s*([^;]+);/.exec(group!)?.[1]?.trim()
    expect(declared).toBe('#ec4899')

    const head = rule(source, '.chat-tool-call-group-open')
    expect(head, 'missing ".chat-tool-call-group-open"').toBeDefined()
    // Every var() of the accent in the head rule must carry that same value as
    // its fallback — a bare `var(--subagent-accent)` is the bug.
    const refs = [...head!.matchAll(/var\(\s*--subagent-accent\s*([^)]*)\)/g)]
    expect(refs.length, 'head rule no longer reads the accent at all').toBeGreaterThan(0)
    for (const m of refs) {
      const fallback = m[1].replace(/^\s*,\s*/, '').trim()
      expect(fallback, `bare var(--subagent-accent) in: ${m[0]}`).not.toBe('')
      expect(fallback.toLowerCase()).toBe(declared)
    }
  })

  it('keeps the head rail the same width as the body rail', () => {
    // 1px head vs 2px body leaves a visible step at the seam.
    const head = rule(source, '.chat-tool-call-group-open')!
    expect(head).toMatch(/border-left-width:\s*2px;/)
    const body = rule(source, '.subagent-group > .subagent-group-body-wrapper')
    expect(body, 'missing body rail rule').toBeDefined()
    expect(body!).toMatch(/border-left:\s*2px solid/)
  })

  it('drops the head bottom border so it cannot cut the rail', () => {
    const head = rule(source, '.chat-tool-call-group-open')!
    expect(head).toMatch(/border-bottom-color:\s*transparent;/)
  })
})

describe('tool-run rail colour', () => {
  it('is declared once on the container both halves descend from', () => {
    // The strip and the body are siblings too; only .chat-tool-group reaches
    // both, so that is where the shared colour has to live.
    const container = rule(source, '.chat-tool-group')
    expect(container, 'missing ".chat-tool-group"').toBeDefined()
    expect(container!).toMatch(/--run-rail:\s*[^;]+;/)
  })

  it('is an opaque colour, not a translucent one', () => {
    // A `… / transparent` rail is composited against whatever is behind it, and
    // the head and body backdrops differ — that is what made the seam visible.
    const container = rule(source, '.chat-tool-group')!
    const value = /--run-rail:\s*([^;]+);/.exec(container)?.[1]?.trim() || ''
    expect(value).toBeTruthy()
    expect(value, '--run-rail must mix against an opaque backdrop').not.toMatch(/transparent/i)
    expect(value).toMatch(/var\(--bg-secondary\)/)
  })

  it('is consumed by BOTH the head and the body', () => {
    const head = rule(source, '.chat-tool-group.is-open > .chat-tool-group-strip')
    expect(head, 'missing open-strip rule').toBeDefined()
    expect(head!).toMatch(/border-left-color:\s*var\(--run-rail\)/)

    const body = rule(source, '.chat-run-body')
    expect(body, 'missing body rail rule').toBeDefined()
    expect(body!).toMatch(/border-left:\s*2px solid var\(--run-rail/)
    // No stray hardcoded rail colour left behind in the body rule.
    expect(body!).not.toMatch(/border-left:\s*2px solid color-mix/)
  })
})
