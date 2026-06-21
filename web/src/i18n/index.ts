import { createI18n } from 'vue-i18n'
import zh from './locales/zh'
import en from './locales/en'

const STORAGE_KEY = 'clawbench-locale'
const BASE_COOKIE_KEY = 'clawbench-locale'

/** Returns the port-scoped cookie name: cb{port}_clawbench-locale for non-default ports,
 *  or plain clawbench-locale for port 20000 / empty (backward compatible). */
function scopedCookieKey(): string {
  const port = window.location.port ? parseInt(window.location.port, 10) : 0
  if (port && port !== 20000) return `cb${port}_${BASE_COOKIE_KEY}`
  return BASE_COOKIE_KEY
}

function detectLocale(): string {
  const saved = localStorage.getItem(STORAGE_KEY)
  if (saved && ['zh', 'en'].includes(saved)) return saved
  const nav = navigator.language || ''
  return nav.startsWith('zh') ? 'zh' : 'en'
}

/** Sync locale to cookie so SSE (EventSource) can send it to backend */
export function setLocaleCookie(lang: string) {
  const key = scopedCookieKey()
  document.cookie = `${key}=${lang};path=/;max-age=31536000;samesite=strict`
}

const i18n = createI18n({
  legacy: false,
  locale: detectLocale(),
  fallbackLocale: 'zh',
  messages: { zh, en },
})

// Set cookie on initial load so SSE connections pick up the locale
setLocaleCookie(i18n.global.locale.value as string)

export default i18n
export { STORAGE_KEY }
