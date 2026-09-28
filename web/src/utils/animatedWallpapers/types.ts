/**
 * Contract for an animated wallpaper style.
 *
 * A style owns exactly two things: the list of parameters the settings panel
 * should expose, and a pure `draw` that paints one frame. It owns NO lifecycle —
 * the rAF loop, the 30fps cap, the visibility pause, the ResizeObserver and the
 * reduced-motion static frame all live in `AnimatedWallpaper.vue`, so a new style
 * cannot forget them (and cannot leak a loop on project switch).
 *
 * Adding a style = one module exporting an `AnimatedStyle` + registering it in
 * `index.ts` + its i18n labels. Nothing else.
 */

/** A numeric slider exposed in the settings panel. */
export interface SliderSpec {
  kind: 'slider'
  /** Stable key inside the style's param bag (also the persisted object key). */
  key: string
  /** i18n key for the row label. */
  labelKey: string
  /** i18n key for the row description (optional). */
  descriptionKey?: string
  min: number
  max: number
  step: number
  /** Value used when the device has no stored value (and by the reset button). */
  defaultValue: number
  /**
   * How the slider value is rendered on the right. The prototype sliders all
   * express themselves as a multiplier of the 100 baseline.
   */
  format: 'multiplier' | 'percent'
}

/** An on/off option exposed in the settings panel. */
export interface SwitchSpec {
  kind: 'switch'
  key: string
  labelKey: string
  descriptionKey?: string
  defaultValue: boolean
}

/** One tunable exposed in the settings panel. */
export type ParamSpec = SliderSpec | SwitchSpec

/** A resolved parameter value. */
export type ParamValue = number | boolean

/** Everything a style needs to paint one frame. */
export interface FrameContext {
  /** The 2D context, already transform-scaled to CSS pixels. */
  ctx: CanvasRenderingContext2D
  /** Canvas size in CSS pixels (not backing pixels). */
  cssW: number
  cssH: number
  /** Backing-store scale factor, for styles that sample in backing units. */
  scale: number
  /** Animation time in seconds (already zeroed on start). */
  time: number
  /** Time-scale multiplier for THIS style (from the shared speed slider). */
  speed: number
  /** Resolved parameter values, defaults already filled in. */
  params: Record<string, ParamValue>
}

export interface AnimatedStyle {
  /** Stable id, persisted in localStorage — never rename an existing one. */
  id: string
  /** i18n key for the style's display name in the picker. */
  labelKey: string
  /** Sliders to expose, in display order. */
  params: ParamSpec[]
  /**
   * Time-scale bounds for this style, applied to the shared 10–100 speed slider.
   *
   * Per-style because the prototypes disagree: both `test/psp-wave` and
   * `test/ps3-wave` clamp to 0.25–2.5, while the XMB style already shipped in the
   * app uses 0.2–2.0. Changing XMB's bounds would change the feel of an existing
   * wallpaper, so each style keeps its own.
   */
  speedRange: [number, number]
  /**
   * Paint one frame. Must be pure: no DOM queries, no timers, no state outside
   * `frame`. Called at up to 30fps, and once for the reduced-motion static frame.
   *
   * Implementations MUST guard against NaN coordinates — canvas silently
   * discards a path containing NaN (no throw, no warning), which presents as
   * "the style vanished" with a clean console.
   */
  draw(frame: FrameContext): void
}
