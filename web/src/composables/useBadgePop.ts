import { nextTick, ref, watch, type Ref } from 'vue'

/**
 * The dock badge "pop" — a one-shot animation replayed whenever the number
 * behind a badge changes.
 *
 * The badge's SOURCE lives in the plugin runtime (`resolveDockBadge`), so the
 * animation belongs to whoever renders the badge, not to App.vue's tab list:
 * previously every surface (wide dock, narrow inline, popup, chat) needed its
 * own ref plus a watch on a shared count map, and a new badge surface had to
 * re-derive all of it.
 *
 * Usage: `const { popping, onBadgeAnimEnd } = useBadgePop(() => count.value)`
 * then `:class="{ 'dock-badge-pop': popping }" @animationend="onBadgeAnimEnd"`.
 *
 * The flip false→true (re-arm in the next tick) is what restarts the CSS
 * animation when the element stays mounted across the change.
 */
export function useBadgePop(count: () => number): {
  popping: Ref<boolean>
  onBadgeAnimEnd: () => void
} {
  const popping = ref(false)

  watch(count, () => {
    popping.value = false
    // Same trick as the previous per-tab map: the class must be ABSENT for one
    // render before it is re-applied, or the animation does not restart. A
    // microtask is not enough — Vue would batch both mutations into one patch.
    void nextTick(() => {
      popping.value = true
    })
  })

  function onBadgeAnimEnd(): void {
    popping.value = false
  }

  return { popping, onBadgeAnimEnd }
}
