import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import i18n, { STORAGE_KEY, setLocaleCookie } from '@/i18n'

export function useLocale() {
  const { locale } = useI18n()

  const currentLocale = computed(() => locale.value as 'zh' | 'en')

  function setLocale(lang: 'zh' | 'en') {
    locale.value = lang
    localStorage.setItem(STORAGE_KEY, lang)
    setLocaleCookie(lang)
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
  return i18n.global.t(key, params as any)
}
