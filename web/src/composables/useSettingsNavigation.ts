import { ref, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsConfig } from '@/composables/useSettingsConfig'
import { useToast } from '@/composables/useToast'

const MAX_POLL_ATTEMPTS = 60 // 2 minutes at 2s interval
const POLL_INTERVAL_MS = 2000

/**
 * Shared composable for settings page navigation, restart logic, and state.
 * Used by SettingsPage.vue to avoid code duplication.
 */
export function useSettingsNavigation() {
  const { t } = useI18n()
  const { loadConfig, restartServer } = useSettingsConfig()
  const toast = useToast()

  const navStack = ref<string[]>([])
  const restartDialogVisible = ref(false)
  const changedColdFields = ref<string[]>([])
  const needsRestart = ref(false)
  const restarting = ref(false)
  const restartingOverlay = ref(false)

  // Track the poll timer for cleanup
  let pollTimer: ReturnType<typeof setInterval> | null = null

  const currentCategory = ref<string | null>(null)
  const beforeReset = ref<(() => boolean) | null>(null)

  function setBeforeResetGuard(fn: (() => boolean) | null) {
    beforeReset.value = fn
  }

  // Update currentCategory whenever navStack changes
  function pushNav(categoryId: string) {
    navStack.value.push(categoryId)
    currentCategory.value = categoryId
  }

  function popNav() {
    if (navStack.value.length > 0) {
      navStack.value.pop()
      currentCategory.value = navStack.value.length > 0
        ? navStack.value[navStack.value.length - 1]
        : null
    }
  }

  function resetState() {
    if (beforeReset.value && !beforeReset.value()) return  // guard says don't reset
    navStack.value = []
    currentCategory.value = null
    needsRestart.value = false
    restarting.value = false
    restartingOverlay.value = false
    restartDialogVisible.value = false
    changedColdFields.value = []
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  function handleRestartNeeded(fields: string[]) {
    changedColdFields.value = fields
    needsRestart.value = true
    restartDialogVisible.value = true
  }

  function pollUntilServerUp() {
    restartingOverlay.value = true
    let attempts = 0

    pollTimer = setInterval(async () => {
      attempts++
      if (attempts >= MAX_POLL_ATTEMPTS) {
        clearInterval(pollTimer!)
        pollTimer = null
        restartingOverlay.value = false
        restarting.value = false
        toast.show(t('settings.restartTimeout'), { icon: '⚠️', type: 'error', duration: 5000 })
        return
      }

      try {
        const resp = await fetch('/api/agents', { method: 'GET' })
        if (resp.ok) {
          clearInterval(pollTimer!)
          pollTimer = null
          restartingOverlay.value = false
          restarting.value = false
          window.location.reload()
        }
      } catch {
        // Server not up yet, keep polling
      }
    }, POLL_INTERVAL_MS)
  }

  async function handleRestart() {
    restartDialogVisible.value = false
    restarting.value = true
    try {
      await restartServer()
      needsRestart.value = false
      // Server is shutting down — start polling until it comes back
      pollUntilServerUp()
    } catch {
      restarting.value = false
    }
  }

  // Cleanup on unmount
  onUnmounted(() => {
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  })

  return {
    t,
    loadConfig,
    restartServer,
    navStack,
    currentCategory,
    pushNav,
    popNav,
    resetState,
    restartDialogVisible,
    changedColdFields,
    needsRestart,
    restarting,
    restartingOverlay,
    handleRestartNeeded,
    handleRestart,
    setBeforeResetGuard,
  }
}
