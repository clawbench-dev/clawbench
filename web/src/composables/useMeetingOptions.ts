/**
 * The capture options a meeting shares between its two surfaces.
 *
 * They are adjustable BEFORE the meeting (create page) and DURING it (the
 * "声音与听记" block in the stage's basic tab), and both places must show the
 * same values — a checkbox that resets when you walk from one screen to the
 * other is worse than having only one of them.
 *
 * Module-level refs, like every other singleton in this folder. The create page
 * seeds them from the host's capabilities (system audio is Windows-only, the
 * camera may not exist at all) the first time it opens; the stage only reads
 * and writes them.
 */
import { ref } from 'vue'

/** Capture the publisher's microphone. */
const microphone = ref(true)
/** Capture system loopback audio. Impossible without a screen share. */
const systemAudio = ref(false)
/** Run AI 听记 (transcription + minutes) for this meeting. */
const notes = ref(true)
/** Capture the publisher's camera (video meetings only). */
const camera = ref(false)

export function useMeetingOptions() {
  return { microphone, systemAudio, notes, camera }
}

/** @internal Test-only: restore the defaults. */
export function _resetMeetingOptions(): void {
  microphone.value = true
  systemAudio.value = false
  notes.value = true
  camera.value = false
}
