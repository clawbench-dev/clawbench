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
    // reads as broken. It also needs a picture: with nobody sharing there is
    // nothing to grab (behaviour pinned in MeetingStage.shotButton.test.ts).
    expect(stage).toContain('v-if="shotsOn && shotSourceReady"')
    expect(stage).toContain('@click="captureShot"')
  })

  it('routes the button through the manual reason', () => {
    const body = functionBody('captureShot')
    expect(body).toContain("captureNow('manual')")
  })

  it('ends the annotation on every close path, so the audience is wiped too', () => {
    // Dropping the mirror only cleaned the host's own screenshot copy: the
    // strokes stayed on every viewer's screen, where the host no longer has a
    // surface (and therefore no 清空 button) to remove them.
    const close = functionBody('closeAnnotate')
    expect(close).toContain('share.endAnnotation()')

    // The native overlay can be dismissed from inside itself (its close button,
    // Esc), so its IPC callback is a close path of its own.
    const closedHandler = stage.slice(stage.indexOf('onAnnotateClosed'))
    expect(closedHandler.slice(0, closedHandler.indexOf('})'))).toContain('share.endAnnotation()')
  })
})
