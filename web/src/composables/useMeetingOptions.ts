/**
 * The capture options a meeting shares between its two surfaces.
 *
 * They are adjustable BEFORE the meeting (create page) and DURING it (the
 * 画面与声音 block in the stage's 基础配置 tab), and both places must show the
 * same values — a choice that resets when you walk from one screen to the other
 * is worse than having only one of them.
 *
 * Each axis is ONE value, not a boolean plus a device id: `''` is "nothing on
 * this axis" (muted / no camera), `DEFAULT_DEVICE` is the system default, and
 * anything else is a `deviceId`. Splitting it in two is how the two halves drift
 * apart — the picture says one thing and the captured stream does another.
 *
 * Module-level refs, like every other singleton in this folder.
 */
import { ref } from 'vue'

/** "Use whatever the system picks" — distinct from `''`, which means off. */
export const DEFAULT_DEVICE = 'default'

/** Which input to publish; `''` mutes the microphone. */
const micChoice = ref<string>(DEFAULT_DEVICE)
/** Which camera to publish; `''` sends no camera picture. */
const cameraChoice = ref<string>('')
/** Run AI 听记 (transcription + minutes) for this meeting. */
const notes = ref(true)

export function useMeetingOptions() {
  return { micChoice, cameraChoice, notes }
}

/** @internal Test-only: restore the defaults. */
export function _resetMeetingOptions(): void {
  micChoice.value = DEFAULT_DEVICE
  cameraChoice.value = ''
  notes.value = true
}
