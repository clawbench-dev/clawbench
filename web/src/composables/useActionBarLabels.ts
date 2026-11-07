import { onBeforeUnmount, onMounted, ref } from 'vue'
import type { Ref } from 'vue'

/**
 * Sub-pixel rounding tolerance, in CSS px.
 *
 * `scrollWidth`/`clientWidth` are integers, so a row that fits EXACTLY can
 * report one pixel of overflow. Without the tolerance the labels would never
 * appear on such a row.
 */
const OVERFLOW_TOLERANCE_PX = 2

/** How long to wait after a size change before measuring. */
const DEFAULT_DEBOUNCE_MS = 50

/**
 * Spare room required before labels are turned back ON, in CSS px.
 *
 * A bar that is exactly at the boundary would otherwise flip on every
 * measurement (one pixel of rounding, a scrollbar appearing, a width
 * transition): labels on → too wide → labels off → room again → labels on.
 * Turning them on only with real slack, and off as soon as they overflow,
 * gives the state a dead zone it cannot oscillate inside.
 */
const TURN_ON_SLACK_PX = 12

export interface ActionBarLabelsOptions {
  /**
   * Runs after every measurement, synchronously.
   *
   * The chat action bar uses it to recompute its own overflow/fade/drag state,
   * which depends on the same measurement.
   */
  onMeasured?: () => void
  /** Override the settle delay (tests use 0). */
  debounceMs?: number
}

/**
 * Show an icon row's labels only when the row can hold them.
 *
 * The labels are always rendered (`.action-label`, hidden by CSS) so this can
 * force them on for one synchronous measurement — measuring the current,
 * label-less state would always conclude "there is room". When they do not fit,
 * the row falls back to icons only rather than overflowing a strip whose
 * scrollbar is hidden.
 *
 * Callers must put `.show-labels` on the bar element (bound to `showLabels`) and
 * give every button a `<span class="action-label">`. Bars with a flexible
 * leading block (a title or breadcrumb) additionally pin that block to its
 * natural width under `.measure-labels`, so "does the row overflow" answers
 * "do the LABELS fit" instead of being absorbed by shrinking the title.
 *
 * Design notes and the full contract: docs/spec/client/design-guide.md
 * 「图标按钮：空间够就带上文字标签」.
 */
export function useActionBarLabels(
  getEl: () => HTMLElement | null,
  opts: ActionBarLabelsOptions = {},
) {
  const showLabels = ref(false)
  let observer: ResizeObserver | null = null
  let timer: ReturnType<typeof setTimeout> | null = null

  function measure(): void {
    const el = getEl()
    if (!el) return
    el.classList.add('measure-labels')
    const scrollWidth = el.scrollWidth
    const clientWidth = el.clientWidth
    el.classList.remove('measure-labels')

    if (clientWidth <= 0) {
      // Not laid out yet (hidden pane, jsdom): assume there is room and let the
      // resize observer correct it once the pane is shown.
      showLabels.value = true
    } else {
      // Asymmetric on purpose — see TURN_ON_SLACK_PX.
      showLabels.value = showLabels.value
        ? scrollWidth <= clientWidth + OVERFLOW_TOLERANCE_PX
        : scrollWidth <= clientWidth - TURN_ON_SLACK_PX
    }
    opts.onMeasured?.()
  }

  function scheduleMeasure(): void {
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => {
      timer = null
      measure()
    }, opts.debounceMs ?? DEFAULT_DEBOUNCE_MS)
  }

  onMounted(() => {
    measure()
    const el = getEl()
    if (typeof ResizeObserver !== 'undefined' && el) {
      observer = new ResizeObserver(() => scheduleMeasure())
      observer.observe(el)
    }
  })

  onBeforeUnmount(() => {
    observer?.disconnect()
    observer = null
    if (timer) clearTimeout(timer)
    timer = null
  })

  return { showLabels, measure, scheduleMeasure }
}

/**
 * The ref type every adopter passes in. Exported so call sites can annotate the
 * element without repeating `Ref<HTMLElement | null>`.
 */
export type ActionBarElementRef = Ref<HTMLElement | null>
