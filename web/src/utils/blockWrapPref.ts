import { ref } from 'vue'
import { gt } from '@/composables/useLocale'

/**
 * Default word-wrap mode for Markdown blocks — tables AND code blocks
 * (设置 → 外观 → 显示).
 *
 * Lives in its own leaf module because the value is read by
 * `useCodeBlockHeader.annotateCodeBlockHeadersIn` /
 * `annotateTableBlockHeadersIn`, which run outside any component (they build
 * HTML strings). Importing `useSettingsConfig` there would drag the whole
 * settings store — and its module-level side effects — into every bundle that
 * renders markdown, including the public share SPA.
 *
 * Persistence is `localStorage` only (a per-device display preference, like
 * UI scale). `useSettingsConfig` owns the write; this module owns the read and
 * the live re-application.
 */

/**
 * Same key `setLocalConfig('tableWrap', …)` writes (LOCAL_PREFIX + key). The
 * key keeps its original name so preferences saved before code blocks shared
 * this setting keep working.
 */
export const BLOCK_WRAP_STORAGE_KEY = 'clawbench-settings-tableWrap'

/** `null` until the first read, so localStorage is touched once per session. */
let cached: boolean | null = null

/**
 * Bumped on every change. Rendered markdown is cached by
 * `ContentBlocks.getBlockHtml`; the cache key has to include this or a cached
 * block would come back with the previous default's wrapper classes.
 */
export const blockWrapVersion = ref(0)

/** The default wrap mode for a block that has not been toggled by hand. */
export function getDefaultBlockWrap(): boolean {
  if (cached === null) {
    let value = true
    try {
      const raw = localStorage.getItem(BLOCK_WRAP_STORAGE_KEY)
      if (raw !== null) value = raw !== 'false'
    } catch {
      // Private mode / disabled storage: fall back to the shipped default.
    }
    cached = value
  }
  return cached
}

/**
 * Re-apply a wrap mode to every block already in the document, buttons
 * included. Static block caching means a re-render is not guaranteed after the
 * setting changes, so without this the toggle would only affect blocks rendered
 * afterwards.
 *
 * Tables carry a `word-wrap` / `no-wrap` class pair; code blocks only have
 * `word-wrap` (absence of it is "no wrap"), so they must not be given a class
 * no rule consumes.
 */
export function applyBlockWrapToRenderedBlocks(wrap: boolean): void {
  if (typeof document === 'undefined') return

  const tableLabel = gt(wrap ? 'tableBlock.wrapOn' : 'tableBlock.wrapOff')
  for (const wrapper of Array.from(document.querySelectorAll<HTMLElement>('.table-block-wrapper'))) {
    wrapper.classList.toggle('word-wrap', wrap)
    wrapper.classList.toggle('no-wrap', !wrap)
    const btn = wrapper.querySelector<HTMLElement>('.table-block-wrap-btn')
    if (!btn) continue
    btn.classList.toggle('is-wrapped', wrap)
    btn.setAttribute('title', tableLabel)
    btn.setAttribute('aria-label', tableLabel)
  }

  const codeLabel = gt(wrap ? 'codeBlock.wrapOn' : 'codeBlock.wrapOff')
  for (const wrapper of Array.from(document.querySelectorAll<HTMLElement>('.code-block-wrapper'))) {
    wrapper.classList.toggle('word-wrap', wrap)
    const btn = wrapper.querySelector<HTMLElement>('.code-block-wrap-btn')
    if (!btn) continue
    btn.classList.toggle('is-wrapped', wrap)
    btn.setAttribute('title', codeLabel)
    btn.setAttribute('aria-label', codeLabel)
  }
}

/** Update the in-memory default and apply it to the blocks on screen. */
export function setDefaultBlockWrap(wrap: boolean): void {
  if (cached !== wrap) blockWrapVersion.value++
  cached = wrap
  try {
    localStorage.setItem(BLOCK_WRAP_STORAGE_KEY, JSON.stringify(wrap))
  } catch {
    // Persisting is best-effort; the in-memory value still applies.
  }
  applyBlockWrapToRenderedBlocks(wrap)
}

/** @internal Reset the memoised value — for tests only. */
export function _resetBlockWrapPref(): void {
  cached = null
  blockWrapVersion.value = 0
}
