import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The live chat and the archived meeting record's chat show the SAME
 * conversation. They used to be two drawings of it — two markups, two copies of
 * the bubble CSS — and this file existed to catch the copies drifting. They did
 * drift: the record view rendered bare text lines long after the live panel had
 * bubbles, which reads as "the styling was lost".
 *
 * The fix is one component, so the guard is now about that: both surfaces must
 * render `ChatMessageRow`, and neither may grow its own bubble rules again.
 * jsdom applies no stylesheets, so this reads the sources.
 */

const ROOT = join(__dirname, '..', '..', '..', '..', '..')

function sourceOf(file: string): string {
  return readFileSync(join(ROOT, file), 'utf8')
}

function styleOf(file: string): string {
  const src = sourceOf(file)
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

const LIVE = 'web/src/share/LiveChatPanel.vue'
const ARCHIVE = 'web/src/components/live/MeetingDetail.vue'
const ROW = 'web/src/components/live/ChatMessageRow.vue'

describe('both chats are drawn by one component', () => {
  it('renders ChatMessageRow on both surfaces', () => {
    for (const file of [LIVE, ARCHIVE]) {
      const src = sourceOf(file)
      expect(src, `${file} must import the shared row`).toMatch(
        /import\s+ChatMessageRow\s+from\s+['"][^'"]*ChatMessageRow\.vue['"]/,
      )
      expect(src, `${file} must render <ChatMessageRow>`).toMatch(/<ChatMessageRow\b/)
    }
  })

  it('leaves the bubble to the row component', () => {
    // The whole point: a second copy of the padding/radius/background is how
    // the two surfaces drifted apart in the first place.
    for (const file of [LIVE, ARCHIVE]) {
      const css = styleOf(file)
      expect(css, `${file} must not declare a bubble`).not.toMatch(/\.(?:live-chat|meeting-chat)__(?:text|msg|row|head|who)\s*\{/)
    }
  })

  it('keeps the bubble geometry in ChatMessageRow', () => {
    const row = styleOf(ROW)
    expect(ruleDecls(row, '.chat-row__text')).toMatchObject({
      padding: 'var(--space-2) var(--space-4)',
      'border-radius': 'var(--radius-md)',
      'line-height': '1.55',
    })
  })

  it('mirrors the local user’s own message in one place', () => {
    const row = styleOf(ROW)
    expect(ruleDecls(row, '.chat-row--self')).toMatchObject({ 'flex-direction': 'row-reverse' })
    expect(ruleDecls(row, '.chat-row--self .chat-row__text').background).toContain('color-mix')
  })

  it('formats the timestamp once, through the shared formatter', () => {
    // Two local copies of "HH:MM" is the same drift in a smaller package.
    expect(sourceOf(ROW)).toMatch(/formatClockTime/)
    for (const file of [LIVE, ARCHIVE]) {
      expect(sourceOf(file)).not.toMatch(/function\s+(?:formatClock|timeOf)\s*\(/)
    }
  })
})
