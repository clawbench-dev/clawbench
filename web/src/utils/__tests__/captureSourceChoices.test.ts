import { describe, expect, it } from 'vitest'
import {
  ANDROID_CAPTURE_SOURCE,
  BROWSER_CAPTURE_SOURCE,
  buildCaptureChoices,
  captureSourceIdOf,
} from '../captureSourceChoices'
import type { CaptureSource } from '@/composables/useLiveShare'

/**
 * Which tiles the picture picker offers, and what a tile means when published.
 *
 * The create page and the running meeting ask the same question, so the answer
 * is computed once. The two hosts without a source list are the ones that used
 * to be re-implemented per screen.
 */

const labels = { entireScreen: 'Entire screen', browserSource: 'Share screen' }

const list: CaptureSource[] = [
  { id: 'screen:0:0', name: 'Entire Screen', kind: 'screen', thumbnail: 'data:x' },
  { id: 'window:1:0', name: 'Terminal', kind: 'window', thumbnail: '' },
]

describe('buildCaptureChoices', () => {
  it('offers the enumerated screens and windows when there are any', () => {
    expect(buildCaptureChoices({ list, android: false, browser: true, ...labels })).toEqual(list)
  })

  it('gives Android one sentinel tile: its consent dialog picks the source', () => {
    expect(buildCaptureChoices({ list, android: true, browser: true, ...labels })).toEqual([
      { id: ANDROID_CAPTURE_SOURCE, name: 'Entire screen', kind: 'screen', thumbnail: '' },
    ])
  })

  it('gives a plain browser one tile for its own picker', () => {
    expect(buildCaptureChoices({ list: [], android: false, browser: true, ...labels })).toEqual([
      { id: BROWSER_CAPTURE_SOURCE, name: 'Share screen', kind: 'screen', thumbnail: '' },
    ])
  })

  it('offers nothing when the host can neither list nor capture', () => {
    // The create form then shows its "capture is unsupported" warning, and the
    // running meeting falls back to the camera group.
    expect(buildCaptureChoices({ list: [], android: false, browser: false, ...labels })).toEqual([])
  })
})

describe('captureSourceIdOf', () => {
  it('publishes the tile id for a real source', () => {
    expect(captureSourceIdOf('window:1:0')).toBe('window:1:0')
  })

  it('publishes an empty id for the browser tile', () => {
    // Its dialog decides; arming a declaration with a fake id would leave the
    // one-shot source armed for the next capture.
    expect(captureSourceIdOf(BROWSER_CAPTURE_SOURCE)).toBe('')
  })

  it('keeps "share nothing" empty', () => {
    expect(captureSourceIdOf('')).toBe('')
  })
})
