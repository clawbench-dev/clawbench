import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { localConfig, setLocalConfig } from '@/composables/useSettingsConfig'
import { THEME_IDS, getThemeLabelKey, getThemePreviewColor, isDarkTheme, resolveThemeId } from '@/utils/themeMeta'
import { applyThemeWithReveal, headerThemeOrigin, originFromElement } from '@/utils/themeReveal'
import { Moon, Sun } from 'lucide-vue-next'

/**
 * State machine behind the quick theme picker (the Palette button).
 *
 * Two hosts render it — the app header (`components/common/AppHeader.vue`) and
 * the public share topbar (`share/ShareThemeButton.vue`). The rows themselves
 * are duplicated markup (as the terminal toolbar picker already is), but the
 * *behaviour* must not be: it carries three non-obvious rules that would
 * silently regress if each host re-implemented them.
 *
 *  1. The reveal only starts once the popup has finished LEAVING
 *     (`after-leave`). Starting it in the click handler puts the still-fading
 *     menu into the transition's "new" snapshot, so the two animations overlap
 *     and the menu appears to be wiped away.
 *  2. The change goes through `setLocalConfig('theme', …)` — that is what
 *     persists the choice and runs the side effect that flips `data-theme` /
 *     `data-theme-base` / `data-hljs-theme` synchronously. Writing
 *     localStorage directly would leave the document on the old palette.
 *  3. The wipe starts from the button the user pressed. `headerThemeOrigin()`
 *     (which looks up `.theme-quick-toggle`) is the fallback so a host whose
 *     button is not mounted still wipes from the header's top-right corner.
 *
 * `'auto'` is a first-class option (it follows the system scheme) and is never
 * replaced by its resolved id — see `applyStoredTheme`'s note in
 * useSettingsConfig.
 */
export function useThemePicker(hooks: { onBeforeOpen?: () => void } = {}) {
  const { t } = useI18n()

  /** Anchor for the teleported popup. */
  const btnRef = ref<HTMLElement | null>(null)
  const menuOpen = ref(false)
  /** Chosen value, applied once the popup has finished closing. */
  const pendingValue = ref<string | null>(null)

  const currentValue = computed(() => String(localConfig.theme ?? 'auto'))
  const autoPreview = computed(() => getThemePreviewColor(resolveThemeId('auto')))

  const options = computed(() => [
    { label: t('settings.items.themeAuto'), value: 'auto' },
    ...THEME_IDS.map(id => ({ label: t(getThemeLabelKey(id)), value: id })),
  ])

  function close() {
    menuOpen.value = false
  }

  function toggle() {
    if (menuOpen.value) {
      close()
      return
    }
    hooks.onBeforeOpen?.()
    menuOpen.value = true
  }

  function selectTheme(value: string) {
    // Close first; the reveal runs from `onMenuAfterLeave`.
    pendingValue.value = value
    close()
  }

  /** Bound to the popup's `after-leave`. */
  function onMenuAfterLeave() {
    const value = pendingValue.value
    if (value === null) return
    pendingValue.value = null
    applyThemeWithReveal(() => setLocalConfig('theme', value), {
      origin: originFromElement(btnRef.value) ?? headerThemeOrigin(),
    })
  }

  /** Per-row preview colours consumed by assets/theme-picker.css. */
  function previewStyle(value: string) {
    const c = value === 'auto' ? autoPreview.value : getThemePreviewColor(value)
    if (!c) return undefined
    return { '--tterm-preview-bg': c.bg, '--tterm-preview-accent': c.accent }
  }

  function baseIcon(value: string) {
    const resolved = value === 'auto' ? resolveThemeId('auto') : value
    return isDarkTheme(resolved) ? Moon : Sun
  }

  return {
    btnRef,
    menuOpen,
    currentValue,
    options,
    toggle,
    close,
    selectTheme,
    onMenuAfterLeave,
    previewStyle,
    baseIcon,
  }
}
