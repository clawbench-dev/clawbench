import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The manual key-frame trigger is the only one the user can invoke, and the
 * only reason that bypasses both the interval and the duplicate check. It has
 * no other entry point: without this button the `manual` path is unreachable
 * and a frame the automatic triggers missed can never be kept.
 *
 * The handler lives inside `<script setup>`, so the WIRING is what is pinned
 * here — the button exists, it is only shown while capture runs, and it calls
 * `captureNow('manual')`. The scheduler rules themselves are covered by
 * `useMeetingShots.test.ts` and `shotCapture.test.ts`.
 */
describe('meeting key-frame wiring', () => {
  const stage = readWebFile('src/components/live/MeetingStage.vue')

  function functionBody(name: string): string {
    const start = stage.indexOf(`function ${name}(`)
    expect(start, `${name} not found in MeetingStage.vue`).toBeGreaterThan(-1)
    const body = stage.slice(start)
    return body.slice(0, body.indexOf('\n}'))
  }

  it('offers a manual capture button while frames are being kept', () => {
    expect(stage).toContain('data-action="shot-now"')
    // Hidden when capture is off or paused: a button that silently does nothing
    // reads as broken.
    expect(stage).toContain('v-if="shotsOn"')
    expect(stage).toContain('@click="captureShot"')
  })

  it('routes the button through the manual reason', () => {
    const body = functionBody('captureShot')
    expect(body).toContain("captureNow('manual')")
  })

  it('clears the composited ink when the annotation window goes away', () => {
    // A leftover stroke would be drawn onto the next frame, in a place the
    // presenter already erased.
    expect(stage).toContain('share.clearInkMirror()')
    const close = functionBody('closeAnnotate')
    expect(close).toContain('share.clearInkMirror()')
  })
})
