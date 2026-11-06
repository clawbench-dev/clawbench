import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'
import { UI_SCALE_EVENT, readAppliedUIScale } from '@/utils/uiScale'

/**
 * True while the viewport is at most `maxDesignPx` DESIGN pixels wide.
 *
 * A CSS media query cannot answer this question once the interface scale is in
 * play: `@media (max-width: 520px)` compares against the window's CSS width,
 * while scaling grows every element without widening the window. At 1.33× a
 * 640px window only has ~480 design px of room, so a query that still says
 * "wide" keeps the labels that no longer fit.
 *
 * Scaling does not fire `resize`, so `UI_SCALE_EVENT` is the second signal.
 * Both are cheap and idempotent; re-evaluating on either is simpler than
 * tracking which one moved.
 *
 * Evaluated once during setup as well as on mount, so the first paint already
 * has the right value (these are layout decisions, not progressive
 * enhancement).
 */
export function useDesignWidthAtMost(maxDesignPx: number): Ref<boolean> {
  const matches = ref(false)

  const evaluate = (): void => {
    if (typeof window === 'undefined') return // SSR / non-browser
    const scale = readAppliedUIScale()
    const designWidth = window.innerWidth / (scale > 0 ? scale : 1)
    matches.value = designWidth <= maxDesignPx
  }

  evaluate()

  onMounted(() => {
    evaluate()
    window.addEventListener('resize', evaluate)
    window.addEventListener(UI_SCALE_EVENT, evaluate)
  })

  onBeforeUnmount(() => {
    window.removeEventListener('resize', evaluate)
    window.removeEventListener(UI_SCALE_EVENT, evaluate)
  })

  return matches
}
