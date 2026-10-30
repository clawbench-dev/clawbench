import { gt } from '@/composables/useLocale'

/**
 * Default word-wrap mode for Markdown tables (设置 → 外观 → 显示).
 *
 * Lives in its own leaf module because the value is read by
 * `useCodeBlockHeader.annotateTableBlockHeadersIn`, which runs outside any
 * component (it builds HTML strings). Importing `useSettingsConfig` there would
 * drag the whole settings store — and its module-level side effects — into every
 * bundle that renders markdown, including the public share SPA.
 *
 * Persistence is `localStorage` only (a per-device display preference, like
 * UI scale). `useSettingsConfig` owns the write; this module owns the read and
 * the live re-application.
 */

/** Same key `setLocalConfig('tableWrap', …)` writes (LOCAL_PREFIX + key). */
export const TABLE_WRAP_STORAGE_KEY = 'clawbench-settings-tableWrap'

/** `null` until the first read, so localStorage is touched once per session. */
let cached: boolean | null = null

/** The default wrap mode for a table that has not been toggled by hand. */
export function getDefaultTableWrap(): boolean {
  if (cached === null) {
    let value = true
    try {
      const raw = localStorage.getItem(TABLE_WRAP_STORAGE_KEY)
      if (raw !== null) value = raw !== 'false'
    } catch {
      // Private mode / disabled storage: fall back to the shipped default.
    }
    cached = value
  }
  return cached
}

/**
 * Re-apply a wrap mode to every table already in the document, buttons
 * included. Static block caching means a re-render is not guaranteed after the
 * setting changes, so without this the toggle would only affect tables rendered
 * afterwards.
 */
export function applyTableWrapToRenderedTables(wrap: boolean): void {
  if (typeof document === 'undefined') return
  const label = gt(wrap ? 'tableBlock.wrapOn' : 'tableBlock.wrapOff')
  for (const wrapper of Array.from(document.querySelectorAll<HTMLElement>('.table-block-wrapper'))) {
    wrapper.classList.toggle('word-wrap', wrap)
    wrapper.classList.toggle('no-wrap', !wrap)
    const btn = wrapper.querySelector<HTMLElement>('.table-block-wrap-btn')
    if (!btn) continue
    btn.classList.toggle('is-wrapped', wrap)
    btn.setAttribute('title', label)
    btn.setAttribute('aria-label', label)
  }
}

/** Update the in-memory default and apply it to the tables on screen. */
export function setDefaultTableWrap(wrap: boolean): void {
  cached = wrap
  try {
    localStorage.setItem(TABLE_WRAP_STORAGE_KEY, JSON.stringify(wrap))
  } catch {
    // Persisting is best-effort; the in-memory value still applies.
  }
  applyTableWrapToRenderedTables(wrap)
}

/** @internal Reset the memoised value — for tests only. */
export function _resetTableWrapPref(): void {
  cached = null
}
