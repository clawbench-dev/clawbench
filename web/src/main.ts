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
import { createSingleTabGuard } from './composables/useSingleTab.ts'
import SingleTabBlocked from './components/common/SingleTabBlocked.vue'

configureMarkedRenderer()

// Observe every /api/* response for a 401 (expired session cookie). When armed
// by App.vue after auth succeeds, a 401 redirects to /login.
installAuthRedirectInterceptor()

// ── Single-tab gate ─────────────────────────────────────────────────────────
// Only one tab may run the app. The server keys a client's WebSocket
// subscription by the `client_id` kept in localStorage — shared by every tab of
// this origin — so two tabs repeatedly replace each other's connection, and
// each replacement triggers a full state resync. That storm (measured: 1,402
// reconnects / 17 min, ~2,900 requests/min) made every page slow to open.
//
// The check runs BEFORE mounting the app, so a second tab opens no WebSocket
// and issues no API requests at all. See useSingleTab.ts for the protocol.
async function bootstrap() {
  const guard = createSingleTabGuard()
  let ownsTab
  try {
    ownsTab = await guard.acquire()
  } catch (err) {
    // Never let the guard itself block startup.
    appLog.e('SingleTab', `acquire failed: ${err instanceof Error ? err.message : String(err)}`)
    ownsTab = true
  }

  if (!ownsTab) {
    // Render only the blocking screen. This intentionally mounts a minimal app
    // with no store, no router and no global event listeners, so the tab is
    // completely inert until the user reloads.
    appLog.w('SingleTab', 'another tab owns the app — showing blocked screen')
    const blocked = createApp(SingleTabBlocked)
    blocked.use(i18n)
    blocked.mount('#app')
    // Ownership can still be handed over (the other tab closed or reloaded);
    // take it so a reload is not strictly required, but never auto-swap the
    // screen out from under the user — the button is the explicit action.
    guard.whenOwnerReleases(() => {
      appLog.i('SingleTab', 'owner released ownership; reload to take over')
    })
    window.addEventListener('pagehide', () => guard.dispose(), { once: true })
    return
  }

  // This tab owns the app: hand ownership to a waiting tab on unload.
  window.addEventListener('pagehide', () => guard.release(), { once: true })
  window.addEventListener('beforeunload', () => guard.release(), { once: true })

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
}

void bootstrap()
