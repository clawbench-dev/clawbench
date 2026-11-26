import { installPromiseWithResolversPolyfill } from './utils/polyfills.ts'

// Must run before any module that depends on Promise.withResolvers (e.g. pdfjs-dist).
installPromiseWithResolversPolyfill()

// Self-hosted open-source font @font-face declarations (lazy — files download
// only when the chosen family is actually used in rendered text).
import '@/assets/self-hosted-fonts.css'

// Theme picker rows (app-header + terminal toolbars). Global because both
// pickers are teleported to <body>; see the file header for the design.
import '@/assets/theme-picker.css'

// Wide-dock icon labels. Global because the chip is teleported to <body>.
import '@/assets/dock-tooltip.css'

import { createApp } from 'vue'
import App from './App.vue'
import i18n from './i18n'
import { LongPressDirective } from './directives/longPress.ts'
import { RunningSweepDirective } from './directives/runningSweep.ts'
import { DockTipDirective } from './directives/dockTooltip.ts'
import { ChatDragDirective } from './directives/chatDrag.ts'
import { configureMarkedRenderer } from './utils/markedConfig.ts'
import { appLog } from './utils/appLog.ts'
import { installAuthRedirectInterceptor } from './utils/authExpiry.ts'
import { registerPwaServiceWorker } from './utils/pwaServiceWorker.ts'
import { createSingleTabGuard } from './composables/useSingleTab.ts'
import SingleTabBlocked from './components/common/SingleTabBlocked.vue'
import { installLocalMediaFallback } from './utils/localMediaFallback.ts'
import { installMediaFullscreenGuard } from './utils/mediaFullscreenGuard.ts'
import { installInlineZoom } from './utils/inlineZoom.ts'
import { installSvgMediaAutoFit } from './utils/svgMediaFit.ts'
import { installChunkLoadRecovery } from './utils/chunkLoadRecovery.ts'
import { renderCrashRecovery } from './utils/renderCrashRecovery.ts'

configureMarkedRenderer()

/**
 * Best-effort display name for the component that threw. SFCs compiled by
 * `@vitejs/plugin-vue` carry `__name`; hand-written components use `name`.
 * Anonymous ones return '' — the log then has no `<...>` suffix.
 */
function componentName(type: unknown): string {
  if (!type || (typeof type !== 'object' && typeof type !== 'function')) return ''
  const t = type as { name?: unknown; __name?: unknown }
  if (typeof t.name === 'string' && t.name) return t.name
  if (typeof t.__name === 'string' && t.__name) return t.__name
  return ''
}

/**
 * `Child<Parent<App>` — the component and the ancestors whose patch threw.
 *
 * The failing component is the one whose update aborted; its ancestors are the
 * only ones that can still be patched into a working state, so knowing both
 * narrows down which subtree got stuck. Capped: a deep chain is noise.
 */
function componentChain(instance: { type?: unknown; parent?: unknown } | null): string {
  const names: string[] = []
  let cur: { type?: unknown; parent?: unknown } | null = instance
  while (cur && names.length < 6) {
    const n = componentName(cur.type)
    names.push(n || '?')
    cur = (cur.parent as typeof cur) ?? null
  }
  return names.join('<')
}

// Observe every /api/* response for a 401 (expired session cookie). When armed
// by App.vue after auth succeeds, a 401 redirects to /login.
installAuthRedirectInterceptor()

// Replace a locally-served image that 404s with a labelled placeholder instead
// of the browser's silent broken-image glyph. Local images are rewritten to
// /api/fs/* with no existence check, so a missing file previously left an
// unexplained gap (issue #501). Document-level capture listener, installed once.
installLocalMediaFallback()

// A media element that fails to load while it holds HTML5 fullscreen would be
// hidden but stay the fullscreen element, leaving the top-layer <html>
// swallowing every pointer event (the UI looks closed but is unclickable).
// Exit fullscreen on that error. Document-level capture listener, installed once.
installMediaFullscreenGuard()
installInlineZoom()
// Diagram widths are written as resolved pixels (that is what breaks the
// figure↔media sizing cycle), so they have to be re-measured when the space
// available changes — window resize, or a side panel collapsing.
installSvgMediaAutoFit()

// A page left open across a rebuild references chunk hashes that no longer
// exist, so every later lazy import 404s. Without this hook those failures are
// silent for components built with a bare `defineAsyncComponent` (Vue keeps
// rendering the loading component forever) — the pane spins until the user
// restarts the app. Installed before the app mounts so no lazy import can miss
// it; see utils/chunkLoadRecovery.ts.
installChunkLoadRecovery()

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
    // completely inert until it takes over.
    appLog.w('SingleTab', 'another tab owns the app — showing blocked screen')
    const blocked = createApp(SingleTabBlocked)
    blocked.use(i18n)
    blocked.mount('#app')

    // The owner went away (closed, reloaded, navigated off): reload so this tab
    // can mount the app. Without this the screen stayed put and the user had to
    // refresh by hand, which read as "it takes a while to become usable".
    guard.whenOwnerReleases(() => {
      appLog.i('SingleTab', 'owner released ownership — reloading to take over')
      window.location.reload()
    })

    // Restored from the back/forward cache: the tab was frozen, so it observed
    // none of the ownership traffic while suspended. Re-evaluate; if the slot
    // is free now, reload into the app.
    window.addEventListener('pageshow', (e) => {
      if (!e.persisted) return
      void guard.acquire().then((canTakeOver) => {
        if (canTakeOver) {
          appLog.i('SingleTab', 'slot free after bfcache restore — reloading to take over')
          window.location.reload()
        }
      })
    })

    window.addEventListener('pagehide', () => guard.dispose(), { once: true })
    return
  }

  // This tab owns the app. Release on every pagehide — including entry to the
  // back/forward cache (persisted=true). A frozen tab cannot run code, so if it
  // kept the lock it would block a waiting tab for as long as the browser keeps
  // the page cached (up to ~10 minutes), which is the Web Locks counterpart of
  // the ghost-owner bug this guard exists to prevent. Releasing lets a waiting
  // tab take over at once; the pageshow handler below re-claims the slot when
  // the page is restored, so a lone tab (the common case) still owns it on
  // return.
  window.addEventListener('pagehide', () => guard.release())
  window.addEventListener('beforeunload', () => guard.release())

  // Restored from the back/forward cache: another tab may have taken the slot
  // while this one was frozen. Re-validate and fall back to the blocked screen
  // (via a reload, which re-runs the gate) if we lost it.
  window.addEventListener('pageshow', (e) => {
    if (!e.persisted) return
    void guard.acquire().then((stillOwns) => {
      if (!stillOwns) {
        appLog.w('SingleTab', 'ownership was taken while frozen — reloading')
        window.location.reload()
      }
    })
  })

  const app = createApp(App)
  app.use(i18n)
  app.directive('long-press', LongPressDirective)
  app.directive('running-sweep', RunningSweepDirective)
  app.directive('dock-tip', DockTipDirective)
  app.directive('chat-drag', ChatDragDirective)

  // Capture Vue component errors (render, lifecycle, event handlers).
  //
  // The component name matters: without it a report only says "a render threw",
  // which is not actionable — the stack is minified and the first frame is a
  // vendor bundle. `info === 'component update'` additionally means Vue's own
  // patcher threw, which leaves that component's tree half-patched; from then
  // on every re-render of it throws again and its subtree never repaints (the
  // "every tab click does nothing" freeze). renderCrashRecovery reloads once the
  // same error repeats instead of leaving the app dead.
  app.config.errorHandler = (err, instance, info) => {
    try {
      const msg = err instanceof Error ? (err.stack || err.message) : String(err)
      const name = componentName((instance as { type?: unknown } | null)?.type)
      const chain = componentChain(instance as { type?: unknown; parent?: unknown } | null)
      appLog.e('Vue', `${msg} [${info}]${name ? ` <${name}>` : ''}`)
      if (info === 'component update') {
        appLog.e('Vue', `update failed in ${chain || '?'} — the subtree can no longer repaint`)
        renderCrashRecovery.note(err instanceof Error ? err.message : String(err), name)
      }
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
