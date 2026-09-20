import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import i18n, { STORAGE_KEY, setLocaleCookie } from '@/i18n'
import { getNative } from '@/utils/clawbenchNative'
import { syncServerLanguage } from '@/utils/serverLanguage'

export function useLocale() {
  const { locale } = useI18n()

  const currentLocale = computed(() => locale.value as 'zh' | 'en')

  function setLocale(lang: 'zh' | 'en') {
    locale.value = lang
    localStorage.setItem(STORAGE_KEY, lang)
    setLocaleCookie(lang)
    // Persist to native prefs so native UI (splash, login page) follows the
    // in-app language even before the locale cookie is readable on cold start.
    getNative()?.setLanguage?.(lang)
    // Tell the server too, so text it persists from background goroutines
    // (the auto-continue message) is written in the language the user sees.
    // This path bypasses the settings-panel locale row, so it must sync itself.
    void syncServerLanguage(lang)
  }

  function toggleLocale() {
    setLocale(locale.value === 'zh' ? 'en' : 'zh')
  }

  const localeLabel = computed(() => {
    return locale.value === 'zh' ? 'EN' : '中'
  })

  return { currentLocale, setLocale, toggleLocale, localeLabel }
}

/** Global t function for use outside components (composables, utils) */
export function gt(key: string, params?: Record<string, unknown>): string {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return i18n.global.t(key, params as any)
}
