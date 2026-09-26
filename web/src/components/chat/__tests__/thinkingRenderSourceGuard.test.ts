import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Guard test: the two thinking-render paths must share ONE source string.
 *
 * A thinking block can carry both a lazy-loaded prefix (think_id →
 * chat_thinking) and live deltas (`text`). Two paths render it:
 *
 *   1. getThinkingHtml      — the template path, every render
 *   2. flushBlockHtml       — the throttled batch path, every ~300ms
 *
 * Regression: flushBlockHtml rendered `block.text` ALONE while the template path
 * rendered prefix+text. The two alternate, so a block with a prefix lost it for
 * one frame and got it back the next — the user saw the block blank and refill
 * in a loop ("flashes every few seconds, looks like it clears and reloads, no
 * new text appears").
 *
 * A behavioural test cannot catch a recurrence: flushBlockHtml runs inside a
 * requestAnimationFrame scheduler, so jsdom never reaches it, and a unit test of
 * the shared helper passes regardless of what the call sites pass. Only the
 * call sites matter, so this asserts them directly.
 *
 * Source-sniffing test, matching the pattern in wallpaperThumbSize.css.test.ts.
 * The SFC lives under the vitest source root, but cwd differs between a bare
 * `vitest` run (web/) and the scripts/vitest-run.sh wrapper (repo root), so
 * probe both.
 */

function readComponent(): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(
        join(base, 'src/components/chat/ContentBlocks.vue'),
        'utf8',
      )
    } catch {
      // try the next candidate
    }
  }
  throw new Error('ContentBlocks.vue not found from cwd: ' + process.cwd())
}

const src = readComponent()

describe('thinking render paths share one source (source guard)', () => {
  it('flushBlockHtml renders the shared thinking source, not block.text', () => {
    // Anchor on the flush loop's thinking branch: it must compute its source via
    // thinkingSource(block) and render THAT string.
    const marker = '} else if (block.type === \'thinking\') {'
    const start = src.indexOf(marker)
    expect(start, 'the thinking branch of flushBlockHtml must still exist').toBeGreaterThan(-1)
    // Take the branch body up to the next branch/loop boundary.
    const branch = src.slice(start, start + 1200)

    expect(
      branch,
      'flushBlockHtml must derive the thinking source from thinkingSource(block)',
    ).toContain('thinkingSource(block)')

    expect(
      branch,
      'flushBlockHtml must render the shared source — rendering block.text drops the lazy-loaded prefix and makes the block flash',
    ).not.toMatch(/renderMarkdownHtml\(\s*block\.text\s*,/)
  })

  it('getThinkingHtml renders the shared thinking source', () => {
    const start = src.indexOf('function getThinkingHtml(')
    expect(start, 'getThinkingHtml must still exist').toBeGreaterThan(-1)
    const fn = src.slice(start, start + 900)
    expect(fn, 'getThinkingHtml must go through thinkingSource(block)').toContain('thinkingSource(block)')
  })

  it('thinkingSource stitches the cached prefix onto the live deltas', () => {
    const start = src.indexOf('function thinkingSource(')
    expect(start, 'thinkingSource must still exist').toBeGreaterThan(-1)
    const fn = src.slice(start, start + 500)
    expect(fn, 'must delegate to the shared helper so both paths agree').toContain('thinkingRenderSource(')
    expect(fn, 'must pass the lazy-loaded prefix from the thinking cache').toContain('cachedText(')
  })
})
