import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The archived chat (meeting record → 聊天 tab) and the live chat panel show
 * the same conversation, so they must draw the same message bubble. They did
 * not: the record view rendered bare text lines, which reads as "the styling
 * was lost" even though every line was there.
 *
 * The two rules live in different files, and jsdom applies no stylesheets, so
 * no component test can see the drift. This reads the sources instead and
 * compares the declarations — change one, change both.
 */

const ROOT = join(__dirname, '..', '..', '..', '..', '..')

function styleOf(file: string): string {
  const src = readFileSync(join(ROOT, file), 'utf8')
  const start = src.indexOf('<style')
  if (start < 0) throw new Error(`${file} has no <style> block`)
  return src.slice(start)
}

/**
 * Drop `var()` fallbacks: `var(--space-3, 6px)` and `var(--space-3)` are the
 * same decision. The fallbacks exist only because the share SPA can be loaded
 * without the token sheet, so they are not part of the parity contract.
 *
 * A plain regex cannot do this — `rgba(255, 255, 255, 0.06)` inside a fallback
 * contains the closing paren the regex would stop at.
 */
function stripFallbacks(value: string): string {
  let out = ''
  let i = 0
  for (;;) {
    const at = value.indexOf('var(', i)
    if (at < 0) return out + value.slice(i)
    out += value.slice(i, at)
    let depth = 0
    let j = at + 3
    for (; j < value.length; j++) {
      if (value[j] === '(') depth++
      else if (value[j] === ')' && --depth === 0) break
    }
    out += `var(${value.slice(at + 4, j).split(',')[0].trim()})`
    i = j + 1
  }
}

/** Declarations of one rule, as `prop: value`. */
function ruleDecls(css: string, selector: string): Record<string, string> {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))
  if (!m) throw new Error(`rule ${selector} not found`)
  const out: Record<string, string> = {}
  for (const decl of m[1].split(';')) {
    const i = decl.indexOf(':')
    if (i < 0) continue
    const prop = decl.slice(0, i).trim()
    const value = stripFallbacks(decl.slice(i + 1).trim()).replace(/\s+/g, ' ')
    if (prop) out[prop] = value
  }
  return out
}

describe('archived chat bubble matches the live chat panel', () => {
  const live = styleOf('web/src/share/LiveChatPanel.vue')
  const archived = styleOf('web/src/components/live/MeetingDetail.vue')

  it('draws the message text with the same bubble declarations', () => {
    expect(ruleDecls(archived, '.meeting-chat__text')).toEqual(ruleDecls(live, '.live-chat__text'))
  })

  it('keeps the sender/time line and the gap between messages', () => {
    expect(ruleDecls(archived, '.meeting-chat__head')).toEqual(ruleDecls(live, '.live-chat__meta'))
    expect(ruleDecls(archived, '.meeting-chat')).toMatchObject({
      display: 'flex',
      'flex-direction': 'column',
      gap: 'var(--space-4)',
    })
  })
})
