/**
 * Which tiles the picture picker offers, and what each one means when it is
 * published.
 *
 * The same choice is asked twice — when creating a meeting and again, live, in
 * the running meeting's 基础配置 tab — and both must offer exactly the same
 * tiles. The two hosts that have no source list are the ones worth naming:
 * Android decides the source in its own consent dialog, and a plain browser's
 * picker IS the source, so each gets one synthetic tile instead of an empty
 * grid.
 */
import type { CaptureSource } from '@/composables/useLiveShare'

/** A browser has no list: this tile stands for "whatever its picker returns". */
export const BROWSER_CAPTURE_SOURCE = 'browser:screen'

/** Android has no list either: the system dialog decides what is mirrored. */
export const ANDROID_CAPTURE_SOURCE = 'android:screen'

export interface CaptureChoiceInput {
  /** What the native host enumerated; empty when it cannot or will not. */
  list: CaptureSource[]
  /** This host captures through a native projection (no list at all). */
  android: boolean
  /** This host can capture, but only through the browser's own picker. */
  browser: boolean
  /** Localised name for Android's single tile. */
  entireScreen: string
  /** Localised name for the browser's tile. */
  browserSource: string
}

/**
 * The tiles to draw, in one place.
 *
 * Order matters: Android wins over any list (it has none), and a real list wins
 * over the browser tile — a native host that also runs in a browser-like
 * context must offer its windows, not a generic "share screen".
 */
export function buildCaptureChoices(input: CaptureChoiceInput): CaptureSource[] {
  if (input.android) {
    return [{ id: ANDROID_CAPTURE_SOURCE, name: input.entireScreen, kind: 'screen', thumbnail: '' }]
  }
  if (input.list.length > 0) return input.list
  if (input.browser) {
    return [{ id: BROWSER_CAPTURE_SOURCE, name: input.browserSource, kind: 'screen', thumbnail: '' }]
  }
  return []
}

/**
 * The id to hand to the capture layer.
 *
 * The browser tile has none: its dialog decides, and passing a fake id would
 * arm a one-shot declaration no host ever answers.
 */
export function captureSourceIdOf(choice: string): string {
  return choice === BROWSER_CAPTURE_SOURCE ? '' : choice
}
