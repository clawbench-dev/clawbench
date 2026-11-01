/**
 * The viewer's chat overlay must inset its own content.
 *
 * `LiveChatPanel` is deliberately flat — no padding of its own — because the
 * meeting panel already insets everything it hosts by 16dp, and a second inset
 * there pushed the chat out of line with the transcript beside it (see
 * `components/live/__tests__/liveChatCompact.test.ts`).
 *
 * The share viewer's overlay has no such pane: it is a floating card over the
 * video. Reusing the flat component there left the bubbles, the input and the
 * send button flush against the border. The inset therefore belongs to the
 * card, and this test pins that pairing: the wrapper exists, it actually wraps
 * the chat, and its padding is non-zero on both axes.
 *
 * jsdom applies no stylesheets, so this reads the sources — a screenshot alone
 * would not catch the next "tidy up" that drops the padding again.
 */
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/** Repo root: this file lives at web/src/share/__tests__/. */
const ROOT = join(__dirname, '..', '..', '..', '..')
const VIEW = 'web/src/share/LiveShareView.vue'

function read(rel: string): string {
  return readFileSync(join(ROOT, rel), 'utf8')
}

/** `var(--space-4, 8px)` and `var(--space-4)` are the same decision. */
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

function ruleDecls(css: string, selector: string): Record<string, string> {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = css.match(new RegExp(`\\n${escaped}\\s*\\{([^}]*)\\}`))
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

function styleOf(src: string): string {
  const start = src.indexOf('<style')
  if (start < 0) throw new Error(`${VIEW} has no <style> block`)
  return src.slice(start)
}

describe('share viewer chat overlay insets its content', () => {
  const src = read(VIEW)

  it('wraps the chat in the inset element, inside the chat card', () => {
    // Anchored on the card so the class cannot be parked on an unrelated div.
    const card = src.slice(src.indexOf('data-panel="chat"'))
    const end = card.indexOf('<!-- Who is speaking')
    expect(end).toBeGreaterThan(0)
    const body = card.slice(0, end)
    expect(body).toMatch(/<div class="live-chat-panel__body">\s*<LiveChatPanel/)
  })

  it('gives that wrapper a non-zero padding on both axes', () => {
    const padding = ruleDecls(styleOf(src), '.live-chat-panel__body').padding
    expect(padding).toBeDefined()
    // `1` = all sides, `2` = vertical horizontal, `4` = t r b l.
    const parts = padding.split(' ')
    expect([1, 2, 3, 4]).toContain(parts.length)
    expect(parts.every((p) => p !== '0')).toBe(true)
  })
})
