import { onBeforeUnmount, onMounted, ref } from 'vue'
import type { Ref } from 'vue'
import { readAppliedUIScale, UI_SCALE_EVENT } from '@/utils/uiScale'

/**
 * The applied UI scale, as a reactive dependency.
 *
 * Rem scaling resizes every element but never the viewport, so `resize` does
 * not fire and a `computed` derived from the scale has to be invalidated
 * explicitly. `applyUIScale` broadcasts `UI_SCALE_EVENT` for that.
 *
 * Lives here rather than in `utils/uiScale.ts`: that module is a leaf with zero
 * imports (a documented constraint — `useWideScreenLayout` and the applier both
 * need it, and importing `vue` there would drag reactivity into every consumer
 * and break the bare-`node` usages). The subscription is per-instance, so a
 * component that unmounts stops listening — unlike the module-level listeners
 * this replaced, which leaked the previous test's scale into the next.
 *
 * Must be called during setup (`onMounted`/`onBeforeUnmount` are used). The
 * initial value is read synchronously so a `computed` created right after this
 * call already sees the real scale rather than 1.
 */
export function useUiScale(): Ref<number> {
  const uiScale = ref(readAppliedUIScale())

  const sync = () => {
    uiScale.value = readAppliedUIScale()
  }

  onMounted(() => {
    // Re-read on mount as well: the applier may have run between setup and the
    // first render (settings load, project switch), and `ref()` above captured
    // whatever was applied at setup time.
    sync()
    window.addEventListener(UI_SCALE_EVENT, sync)
  })
  onBeforeUnmount(() => {
    window.removeEventListener(UI_SCALE_EVENT, sync)
  })

  return uiScale
}
