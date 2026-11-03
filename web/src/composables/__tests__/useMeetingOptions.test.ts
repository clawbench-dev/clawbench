import { beforeEach, describe, expect, it } from 'vitest'
import {
  _resetMeetingOptions,
  applyMeetingDefaults,
  DEFAULT_DEVICE,
  useMeetingOptions,
} from '../useMeetingOptions'

describe('useMeetingOptions', () => {
  beforeEach(() => {
    _resetMeetingOptions()
  })

  it('starts with 听记 and key frames on', () => {
    const { notes, shots } = useMeetingOptions()
    expect(notes.value).toBe(true)
    expect(shots.value).toBe(true)
  })

  it('seeds a new meeting from the per-device defaults', () => {
    const opts = useMeetingOptions()
    applyMeetingDefaults({ notes: false, shots: false })

    expect(opts.notes.value).toBe(false)
    expect(opts.shots.value).toBe(false)
    // Capture devices are not part of the default seed: they belong to the
    // machine's picker, not to a meeting preference.
    expect(opts.micChoice.value).toBe(DEFAULT_DEVICE)
    expect(opts.cameraChoice.value).toBe('')
  })

  it('leaves an axis alone when the default omits it', () => {
    const { notes, shots } = useMeetingOptions()
    applyMeetingDefaults({ notes: false })
    expect(notes.value).toBe(false)
    expect(shots.value).toBe(true)
  })
})
