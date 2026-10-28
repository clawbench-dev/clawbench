import { ref } from 'vue'
import { isDarkTheme } from '@/utils/themeMeta'

/**
 * Live theme id for the public share SPA.
 *
 * `setLocalConfig('theme', …)` already rewrites `data-theme` /
 * `data-theme-base` / `data-hljs-theme` on <html> and dispatches
 * `clawbench-theme-change`, so CSS-driven colours (tokens, hljs, diff tints)
 * follow a switch with no help. Two things cannot:
 *
 *   - **Components that read the theme as data.** OpenApiPreview injects
 *     `'theme'` to pick Swagger UI colours; a one-shot `getAttribute` at mount
 *     would leave the iframe on the old palette forever.
 *   - **Mermaid.** Diagrams are pre-rendered SVG whose colours are baked in by
 *     `mermaid.initialize({theme})`; they must be re-rendered, exactly as
 *     App.vue does for the app.
 *
 * The app gets both for free (it has App.vue's listener + provide). The share
 * SPA mounts neither, which is why this module exists. It is installed once
 * from share/main.ts and lives for the page's lifetime.
 */

/** Current resolved theme id; updated on every switch. */
export const shareThemeId = ref(
  typeof document === 'undefined' ? 'gruvbox-dark' : (document.documentElement.getAttribute('data-theme') || 'gruvbox-dark'),
)

/** Whether the current theme is dark (reactive; used by iframe-hosted viewers). */
export const shareThemeIsDark = () => isDarkTheme(shareThemeId.value)

/** Guards against double installation (the entry is evaluated once, but tests
 *  and future call sites may run this more than once). */
let installed = false

/**
 * Follow theme changes for the lifetime of the page. Idempotent.
 *
 * Deliberately does NOT re-apply the attributes — the dispatcher
 * (`setLocalConfig`'s side effect) already did, and re-applying here would make
 * this module a second authority for the same fact.
 */
export function installShareThemeSync(): void {
  if (typeof window === 'undefined' || installed) return
  installed = true
  window.addEventListener('clawbench-theme-change', (e: Event) => {
    const resolved = (e as CustomEvent<string>).detail
    if (typeof resolved === 'string' && resolved) shareThemeId.value = resolved
    // Re-initialise + re-render diagrams with the new palette. Dynamic import
    // keeps mermaid (a large, lazily-loaded dependency) out of the initial
    // share bundle — the same reason App.vue imports it here rather than at the
    // top of the module.
    void import('@/utils/mermaid')
      .then(async ({ initMermaid, reRenderMermaid }) => {
        await initMermaid()
        await reRenderMermaid()
      })
      .catch(() => { /* mermaid never loaded on this page — nothing to re-render */ })
  })
}

/** Test-only: allow a re-install (module-level guard is not resettable). */
export function _resetShareThemeSyncForTest(): void {
  installed = false
}
