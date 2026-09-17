import { installPromiseWithResolversPolyfill } from './utils/polyfills.ts'

// Must run before any module that depends on Promise.withResolvers (e.g. pdfjs-dist).
installPromiseWithResolversPolyfill()

// Self-hosted open-source font @font-face declarations (lazy — files download
// only when the chosen family is actually used in rendered text).
import '@/assets/self-hosted-fonts.css'

import { createApp } from 'vue'
import App from './App.vue'
import i18n from './i18n'
import { LongPressDirective } from './directives/longPress.ts'
import { configureMarkedRenderer } from './utils/markedConfig.ts'
import { appLog } from './utils/appLog.ts'
import { installAuthRedirectInterceptor } from './utils/authExpiry.ts'
import { registerPwaServiceWorker } from './utils/pwaServiceWorker.ts'

configureMarkedRenderer()

// Observe every /api/* response for a 401 (expired session cookie). When armed
// by App.vue after auth succeeds, a 401 redirects to /login.
installAuthRedirectInterceptor()

const app = createApp(App)
app.use(i18n)
app.directive('long-press', LongPressDirective)

// Capture Vue component errors (render, lifecycle, event handlers)
app.config.errorHandler = (err, _instance, info) => {
  try {
    const msg = err instanceof Error ? (err.stack || err.message) : String(err)
    appLog.e('Vue', `${msg} [${info}]`)
  } catch {
    appLog.e('Vue', 'Failed to log error:', err)
  }
}

// Capture uncaught JS errors (non-Vue). Guard e.message to skip resource errors.
window.addEventListener('error', (e) => {
  if (e.message) {
    const s = e.error?.stack ? e.error.stack : `${e.message} at ${e.filename}:${e.lineno}`
    appLog.e('JS.Uncaught', s)
  }
})

// Capture resource loading failures (img/script/link 404s, etc.)
// Resource error events fire on the element and do NOT bubble, so capture phase is required.
window.addEventListener('error', (e) => {
  if (e.target && e.target !== window) {
    const el = e.target as HTMLElement
    const tag = el.tagName || '?'
    const src = el.getAttribute('src') || el.getAttribute('href') || ''
    if (src) {
      appLog.e('JS.Resource', `Failed to load <${tag}> src=${src}`)
    }
  }
}, true)

// Capture unhandled Promise rejections with safe serialization
window.addEventListener('unhandledrejection', (e) => {
  try {
    const r = e.reason
    const msg = r instanceof Error ? (r.stack || r.message) :
      (typeof r === 'object' && r ? JSON.stringify(r) : String(r))
    appLog.e('JS.Promise', msg)
  } catch (err) {
    // Prevent infinite loop — last-resort logging
    appLog.e('JS.Promise', 'Failed to serialize rejection:', err)
  }
})

app.mount('#app')

// Register the PWA service worker, subject to the gates in the helper (secure
// context, top-level frame, not the native app, and /sw.js actually served as
// JS). The worker exists only to keep the app installable — it never writes to
// Cache Storage, so it cannot serve a stale app shell or intercept /api/*.
void registerPwaServiceWorker()
