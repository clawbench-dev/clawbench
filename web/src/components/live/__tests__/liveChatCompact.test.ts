import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The live chat used to be a bordered card inside a bordered pane, with a
 * rule above the composer and a bordered input inside it — three frames for
 * one conversation, and two insets (the pane's 16dp plus the list's own 10dp)
 * that pushed the chat out of line with the transcript beside it.
 *
 * The compact redesign removes the frames and keeps the bubble. jsdom applies
 * no stylesheets, so these read the sources: without them the borders come
 * back silently the next time someone "tidies up" the CSS.
 */

const ROOT = join(__dirname, '..', '..', '..', '..', '..')

function styleOf(file: string): string {
  const src = readFileSync(join(ROOT, file), 'utf8')
  const start = src.indexOf('<style')
  if (start < 0) throw new Error(`${file} has no <style> block`)
  return src.slice(start)
}

/** `var(--space-3, 6px)` and `var(--space-3)` are the same decision. */
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

describe('live chat is compact and flat', () => {
  const chat = styleOf('web/src/share/LiveChatPanel.vue')
  const row = styleOf('web/src/components/live/ChatMessageRow.vue')
  const stage = styleOf('web/src/components/live/MeetingStage.vue')

  it('draws no frame around the conversation', () => {
    const inline = ruleDecls(stage, '.live-chat-inline')
    expect(inline.border).toBeUndefined()
    expect(inline['border-radius']).toBeUndefined()
  })

  it('leaves the inset to the pane instead of adding a second one', () => {
    expect(ruleDecls(chat, '.live-chat__list')).toMatchObject({ padding: '0' })
    const compose = ruleDecls(chat, '.live-chat__compose')
    expect(compose.padding).toBe('var(--space-4) 0 0')
    expect(compose['border-top']).toBeUndefined()
  })

  it('keeps the bubble, tightened', () => {
    expect(ruleDecls(row, '.chat-row__text')).toMatchObject({
      padding: 'var(--space-2) var(--space-4)',
      'line-height': '1.55',
    })
  })

  it('flattens the input without dropping the focus ring', () => {
    const input = ruleDecls(chat, '.live-chat__input')
    expect(input.border).toBe('none')
    // A background STEP would vanish in the 15 themes where secondary and
    // tertiary are within 1.12 of each other; the fill must be a text mix.
    expect(input.background).toContain('color-mix')
    expect(ruleDecls(chat, '.live-chat__input:focus')['box-shadow']).toContain('var(--accent-color)')
  })

  it('lays each message out as avatar + body, mirrored for the local user', () => {
    expect(ruleDecls(row, '.chat-row')).toMatchObject({ display: 'flex', 'align-items': 'flex-start' })
    expect(ruleDecls(row, '.chat-row--self')).toMatchObject({ 'flex-direction': 'row-reverse' })
    expect(ruleDecls(row, '.chat-row__body')).toMatchObject({
      display: 'flex',
      'flex-direction': 'column',
      'max-width': '82%',
    })
  })

  it('renders the avatar for every message', () => {
    // The row is the single place a message is drawn (live chat and the
    // archived record both use it), so the avatar lives there.
    expect(readFileSync(join(ROOT, 'web/src/components/live/ChatMessageRow.vue'), 'utf8')).toMatch(
      /<SpeakerAvatar[^>]*class="chat-row__avatar"/,
    )
  })
})
