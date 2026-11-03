import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Both meeting screens end a one-line muted status strip with a word-level
 * action: the post-meeting artifact bar (重新识别 / 取消) and the running
 * meeting's shot bar (暂停截图 / 继续截图).
 *
 * The live toggle used to carry `meeting-link` — a class that only exists in
 * MeetingDetail's scoped block — so nothing styled it and the browser's filled
 * rectangle appeared next to the muted text. The shape now lives once in
 * `assets/meeting-text-btn.css` and both buttons wear it; this pins the parity
 * so the two cannot drift back apart.
 *
 * jsdom applies no stylesheets, so the checks are at the source.
 */

const SHARED = 'src/assets/meeting-text-btn.css'
const shared = readWebFile(SHARED)

function sourceOf(rel: string): string {
  return readWebFile(rel)
}

const artifactBar = sourceOf('src/components/live/MeetingArtifactBar.vue')
const stage = sourceOf('src/components/live/MeetingStage.vue')

describe('meeting text button is one shared shape', () => {
  it('holds the shape in the shared sheet, keyed to the interface scale', () => {
    expect(shared).toContain('.meeting-text-btn {')
    expect(shared).toContain('height: calc(26 * var(--dp))')
    expect(shared).toContain('color: var(--accent-color)')
    expect(shared).toContain('background: transparent')
    expect(shared).toContain('font-size: var(--font-size-xs)')
    // Hover tints, it does not raise a surface out of the line.
    expect(shared).toContain('background: var(--bg-hover)')
  })

  it('is worn by both callers, which both import the sheet', () => {
    for (const [name, src] of [
      ['MeetingArtifactBar.vue', artifactBar],
      ['MeetingStage.vue', stage],
    ] as const) {
      expect(src, `${name} must import the shared sheet`).toContain(
        "import '@/assets/meeting-text-btn.css'",
      )
      expect(src, `${name} must wear the shared class`).toContain('meeting-text-btn')
    }
  })

  it('keeps the live toggle from re-declaring the shape', () => {
    // A scoped rule would out-weigh the global single class and silently
    // fork the shape again (design guide red line 2).
    const toggle = stage.match(/\.live-shot-bar__toggle\s*\{([^}]*)\}/)
    expect(toggle, 'toggle rule not found').not.toBeNull()
    expect(toggle![1]).not.toMatch(/border|background|color|padding|height|font/)
    expect(toggle![1]).toContain('margin-left: auto')
  })

  it('does not borrow a class scoped to a third component', () => {
    const template = stage.slice(0, stage.indexOf('<script'))
    expect(template).not.toContain('meeting-link')
  })
})
