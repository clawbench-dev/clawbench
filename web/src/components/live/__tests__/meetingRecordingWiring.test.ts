import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring guards for the meeting's screen-recording switch.
 *
 * The behaviour lives in `useMeetingRecorder` (covered by its own tests); what
 * cannot be observed there is the contract with the stage and the meeting:
 *
 *   - the button only exists while there is a picture, and it is the switch
 *     (start when idle, stop when running);
 *   - the recorded source follows the CURRENT picture, exactly like key frames,
 *     so a member who takes the screen is what gets recorded;
 *   - the meeting finalises the recording BEFORE it tears down the audio graph
 *     and the capture streams. Reversing that order silently truncates the file
 *     (the AudioContext close cuts the last chunk), and no unit test can see it
 *     because it is an ordering property of one function.
 */
describe('meeting recording wiring', () => {
  const stage = readWebFile('src/components/live/MeetingStage.vue')
  const share = readWebFile('src/composables/useLiveShare.ts')

  /** The function's text, up to its column-0 closing brace. */
  function functionBody(source: string, name: string): string {
    const start = source.indexOf(`function ${name}(`)
    expect(start, `${name} not found`).toBeGreaterThan(-1)
    const end = source.indexOf('\n}\n', start)
    return source.slice(start, end === -1 ? undefined : end)
  }

  it('offers a record switch only while a picture exists', () => {
    expect(stage).toContain('data-action="record"')
    expect(stage).toContain('v-if="canRecord"')
    expect(stage).toContain('@click="toggleRecording"')
    // Same source rule as key frames: no picture, nothing to record.
    expect(stage).toContain('const canRecord = shotSourceReady')
  })

  it('toggles between start and stop, and shows the elapsed time', () => {
    const body = functionBody(stage, 'toggleRecording')
    expect(body).toContain('share.stopRecording()')
    expect(body).toContain('share.startRecording()')
    expect(stage).toContain('data-role="record-time"')
    expect(stage).toContain('recordingClock')
  })

  it('records the picture the audience sees, with the ink on top', () => {
    expect(share).toContain('screenSharer.value ? remoteScreenStream.value : previewStream.value')
    expect(share).toContain('getStrokes: () => inkMirror.strokes.value')
    expect(share).toContain('getAudio: recordingAudioStream')
  })

  it('finalises the recording before the meeting tears its media down', () => {
    const stop = share.slice(share.indexOf('async function stop(): Promise<void> {'))
    const recorderStop = stop.indexOf('await useMeetingRecorder().stop()')
    // The CALL, not the comment above it that also names the function.
    const reset = stop.indexOf('\n    resetSession()\n')
    expect(recorderStop).toBeGreaterThan(-1)
    expect(reset).toBeGreaterThan(-1)
    expect(recorderStop).toBeLessThan(reset)
  })

  it('feeds the recorder the mixer in p2p mode and its own bus in relay mode', () => {
    expect(share).toContain('mixer?.recordStream()')
    expect(share).toContain('relayRecordDest = audioCtx.createMediaStreamDestination()')
  })
})
