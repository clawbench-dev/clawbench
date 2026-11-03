import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The same conversation is drawn three times — the publisher's 转写 tab, the
 * viewer's overlay, and the meeting record opened later — so it must be ONE
 * implementation. It was not: two surfaces had copies of the row markup (kept
 * in step only by this test) and the record rendered the stored artifact as a
 * `<pre>` of flattened text, with no speaker column and no avatar.
 *
 * The guard is therefore no longer "the copies agree" but "there are no
 * copies": all three render `TranscriptList`, and none of them may declare the
 * row language again. jsdom applies no stylesheets, so this reads the sources.
 */

const ROOT = join(__dirname, '..', '..', '..', '..', '..')

const SURFACES = [
  'web/src/components/live/MeetingStage.vue', // publisher, live
  'web/src/share/LiveShareView.vue', // viewer, live
  'web/src/components/live/MeetingDetail.vue', // record, after the fact
]
const LIST = 'web/src/components/live/TranscriptList.vue'

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

describe('the transcript is one component', () => {
  it('renders TranscriptList on all three surfaces', () => {
    for (const file of SURFACES) {
      const src = sourceOf(file)
      expect(src, `${file} must import the shared list`).toMatch(
        /import\s+TranscriptList\s+from\s+['"][^'"]*TranscriptList\.vue['"]/,
      )
      expect(src, `${file} must render <TranscriptList>`).toMatch(/<TranscriptList\b/)
    }
  })

  it('leaves the row language to that component', () => {
    for (const file of SURFACES) {
      const css = styleOf(file)
      expect(css, `${file} must not declare transcript rows`).not.toMatch(
        /\.(?:live-note|transcript)__(?:line|row|speaker|text|avatar)\s*\{/,
      )
    }
  })

  it('keeps the one grid that makes the speaker column work', () => {
    const list = styleOf(LIST)
    expect(ruleDecls(list, '.transcript')).toMatchObject({
      display: 'grid',
      // auto = the avatar, max-content = the widest name, 1fr = the text.
      'grid-template-columns': 'auto max-content minmax(0, 1fr)',
    })
    // Without this each row sizes its own speaker cell and every line's text
    // starts at a different x.
    expect(ruleDecls(list, '.transcript__row')).toEqual({ display: 'contents' })
  })

  it('lets each host supply its own inset instead of copying the list', () => {
    // The pane pads its surfaces; the viewer panel pads its own body.
    expect(ruleDecls(styleOf(LIST), '.transcript').padding).toBe('var(--transcript-inset)')
  })

  it('rebuilds the record’s rows from the stored artifact', () => {
    // The record has no live segments — the speaker has to come back out of
    // the WebVTT voice span, otherwise the tab can only be a wall of text.
    expect(sourceOf('web/src/components/live/MeetingDetail.vue')).toMatch(/parseVttCues/)
  })
})
