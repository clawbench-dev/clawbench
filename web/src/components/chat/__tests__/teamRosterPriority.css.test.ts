import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guards for the team roster's name/type priority.
 *
 * The roster row is a 4-track grid: `status | identity | tools | tokens`. The
 * two numeric tracks are FIXED on purpose — that alignment is what makes the
 * roster readable — so the identity track is the only one that flexes, and the
 * name-vs-type priority has to be settled INSIDE it.
 *
 * The contract, in the order the row gives up space:
 *
 *   1. the agent TYPE truncates first (`flex-shrink: 999` vs the name's 0-ish),
 *   2. on a narrow panel the type disappears entirely (container query),
 *   3. the NAME truncates only after the type is gone,
 *   4. the NAME is never removed from the DOM.
 *
 * Why the ordering matters: nearly every member of an auto-team is
 * `general-purpose`, so the type is close to constant across the roster while
 * the name is the only thing telling two rows apart. An earlier version removed
 * the type unconditionally (commit 227347304) to stop it eating the name's
 * width; that lost real information for teams that DO mix types. The rule now
 * is "type yields first", not "type is absent".
 *
 * jsdom has no CSS engine (no layout, no container queries), so this is a
 * source contract. The behavioural half — that the type is in the DOM at all —
 * is covered in TeamPanel.test.ts.
 */

type Decl = Record<string, string>

/**
 * Extract the declaration block for `selector` at the TOP LEVEL of a stylesheet.
 *
 * Rules nested inside an at-rule (`@container`, `@media`) are stripped first.
 * `.team-member__agent-type` legitimately appears in BOTH places — the base rule
 * carries the shrink priority, the container query carries `display: none` —
 * and matching the nested one first would compare the wrong declarations.
 */
function declsOf(css: string, selector: string): Decl | null {
  // Comments first: a `/* … @container … */` mention would otherwise be taken
  // for a real at-rule and swallow the rules that follow it.
  const topLevel = stripAtRules(css.replace(/\/\*[\s\S]*?\*\//g, ''))
  // Anchor on the selector at the start of a line so `.team-member__name` does
  // not match `.team-member__name--foo`, and escape the class dots.
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const re = new RegExp(`(?:^|\\n)\\s*${escaped}\\s*\\{([^}]*)\\}`, 'm')
  const m = re.exec(topLevel)
  if (!m) return null
  // Strip /* … */ before splitting: a comment inside the block would otherwise
  // be parsed as part of the following declaration.
  const body = m[1].replace(/\/\*[\s\S]*?\*\//g, '')
  const out: Decl = {}
  for (const line of body.split(';')) {
    const idx = line.indexOf(':')
    if (idx < 0) continue
    out[line.slice(0, idx).trim()] = line.slice(idx + 1).trim()
  }
  return out
}

/** Remove every `@rule { ... }` block, brace-matched. */
function stripAtRules(css: string): string {
  let out = ''
  let i = 0
  while (i < css.length) {
    const at = css.indexOf('@', i)
    if (at < 0) {
      out += css.slice(i)
      break
    }
    out += css.slice(i, at)
    const brace = css.indexOf('{', at)
    if (brace < 0) break
    let depth = 0
    let j = brace
    for (; j < css.length; j++) {
      if (css[j] === '{') depth++
      else if (css[j] === '}') {
        depth--
        if (depth === 0) break
      }
    }
    i = j + 1
  }
  return out
}

function teamPanelSource(): string {
  return readWebFile('src/components/chat/TeamPanel.vue')
}

describe('TeamPanel roster — name/type priority', () => {
  it('keeps the name and type in ONE identity track', () => {
    // Two separate grid tracks would give the type its own fixed share, which
    // cannot then be yielded to the name. The wrapper is what makes the
    // priority expressible in CSS at all.
    const src = teamPanelSource()
    const ident = declsOf(src, '.team-member__ident')
    expect(ident, '.team-member__ident must exist').not.toBeNull()
    expect(ident!.display).toBe('flex')
    // Without min-width:0 the children cannot shrink below their content width,
    // so nothing would ever truncate and the row would overflow instead.
    expect(ident!['min-width']).toBe('0')
  })

  it('makes the type shrink far faster than the name', () => {
    const src = teamPanelSource()
    const type = declsOf(src, '.team-member__agent-type')
    const name = declsOf(src, '.team-member__name')
    expect(type).not.toBeNull()
    expect(name).not.toBeNull()

    // `flex: <grow> <shrink> <basis>` — the SHRINK is the second component.
    const typeShrink = Number(type!.flex?.split(/\s+/)[1] ?? '1')
    const nameShrink = Number(name!.flex?.split(/\s+/)[1] ?? '1')
    expect(typeShrink).toBeGreaterThan(nameShrink * 10)

    // A long type must not claim the name's space even when it would fit.
    expect(type!['max-width']).toBeTruthy()
    // Both must be able to truncate rather than overflow the track.
    expect(type!['text-overflow']).toBe('ellipsis')
    expect(name!['text-overflow']).toBe('ellipsis')
  })

  it('removes the type entirely on a narrow panel, never the name', () => {
    const src = teamPanelSource()

    // The container query must target the TYPE only.
    const cq = /@container\s+team-roster\s*\(max-width:\s*\d+px\)\s*\{([\s\S]*?)\n\}/.exec(src)
    expect(cq, 'a team-roster container query must exist').not.toBeNull()
    const body = cq![1]
    expect(body).toContain('.team-member__agent-type')
    expect(body).toContain('display: none')
    // Hiding the NAME here would defeat the whole point.
    expect(body).not.toContain('.team-member__name')
    expect(body).not.toContain('.team-member__ident')

    // The query only works if an ancestor establishes the container.
    const roster = declsOf(src, '.team-expanded__roster')
    expect(roster!['container-type']).toBe('inline-size')
    expect(roster!['container-name']).toBe('team-roster')
  })

  it('never removes the name from the row markup', () => {
    // A `v-if` on the name would make it vanish on narrow panels — the one
    // failure mode this whole arrangement exists to prevent.
    const src = teamPanelSource()
    const nameTag = /<span[^>]*class="team-member__name"[^>]*>/.exec(src)
    expect(nameTag, '.team-member__name must be in the roster template').not.toBeNull()
    expect(nameTag![0]).not.toContain('v-if')
    expect(nameTag![0]).not.toContain('v-show')
  })
})
