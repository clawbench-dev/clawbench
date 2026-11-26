/**
 * Guard: the desktop annotation overlay offers the two capture actions.
 *
 * The overlay is a standalone document with an inline script, so it cannot be
 * mounted in a test. Its buttons are the only way to keep a frame or start a
 * recording while the annotations are on screen: the meeting footer is under
 * this full-screen window, and closing it erases the ink. Without them, every
 * frame captured from the footer is blank of annotations — the bug this exists
 * to prevent.
 */
import { describe, expect, it } from 'vitest'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

function findFile(candidates: string[]): string {
  for (const p of candidates) {
    if (existsSync(p)) return p
  }
  throw new Error(`none of these exist (cwd=${process.cwd()}):\n${candidates.join('\n')}`)
}

const html = readFileSync(
  findFile([
    resolve(process.cwd(), 'desktop/assets/annotate.html'),
    resolve(process.cwd(), 'assets/annotate.html'),
  ]),
  'utf8',
)

describe('annotate overlay: capture actions', () => {
  it('has 留存画面 and 录制 on the toolbar', () => {
    expect(html).toContain('id="shot"')
    expect(html).toContain('id="record"')
  })

  it('reports the clicks to the host instead of doing them itself', () => {
    // The page owns no meeting state: the app window keeps the frame and runs
    // the recorder, exactly as it does for the footer's buttons.
    expect(html).toContain('host.shot()')
    expect(html).toContain('host.record()')
  })

  it('shows whether a recording is running, and hides what would do nothing', () => {
    expect(html).toContain('host.onToolbar')
    expect(html).toContain("'停止'")
    expect(html).toContain("'录制'")
    // Same rule as the footer: a button that would silently no-op is hidden.
    expect(html).toContain('shotBtn.hidden')
    expect(html).toContain('recordBtn.hidden')
  })
})
