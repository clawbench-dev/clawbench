import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { nextTick } from 'vue'
import { _resetShareThemeSyncForTest, installShareThemeSync, shareThemeId } from '@/share/shareTheme'

/**
 * The share SPA mounts no App.vue, so it has to wire two theme-change effects
 * itself:
 *
 *   - the reactive theme id that iframe-hosted viewers read through
 *     provide/inject (OpenApiPreview picks Swagger UI colours from it), and
 *   - a mermaid re-render, because a diagram's colours are baked into the SVG
 *     by `mermaid.initialize({theme})` and never follow CSS.
 *
 * Both are silent failures when missing: the page looks right until the reader
 * switches theme, at which point one card keeps the old palette.
 */

const { initMermaidFn, reRenderMermaidFn } = vi.hoisted(() => ({
  initMermaidFn: vi.fn(async () => {}),
  reRenderMermaidFn: vi.fn(async () => {}),
}))

vi.mock('@/utils/mermaid', () => ({
  initMermaid: initMermaidFn,
  reRenderMermaid: reRenderMermaidFn,
}))

/** Let the dynamic import inside the listener resolve. */
async function settle() {
  // `vi.waitFor` polls until the callback stops throwing — a fixed number of
  // microtask ticks is not enough for a dynamically imported chunk.
  await vi.waitFor(() => {
    expect(initMermaidFn).toHaveBeenCalled()
  })
  await nextTick()
}

beforeEach(() => {
  _resetShareThemeSyncForTest()
  initMermaidFn.mockClear()
  reRenderMermaidFn.mockClear()
  document.documentElement.setAttribute('data-theme', 'gruvbox-dark')
  shareThemeId.value = 'gruvbox-dark'
})

afterEach(() => {
  _resetShareThemeSyncForTest()
})

describe('installShareThemeSync', () => {
  it('seeds the reactive id from the attribute set by the bootstrap script', async () => {
    document.documentElement.setAttribute('data-theme', 'nord')
    vi.resetModules()
    const mod = await import('@/share/shareTheme')
    expect(mod.shareThemeId.value).toBe('nord')
  })

  it('updates the reactive id and re-renders diagrams on a theme change', async () => {
    installShareThemeSync()
    window.dispatchEvent(new CustomEvent('clawbench-theme-change', { detail: 'dracula' }))
    await settle()

    expect(shareThemeId.value).toBe('dracula')
    expect(initMermaidFn).toHaveBeenCalledTimes(1)
    expect(reRenderMermaidFn).toHaveBeenCalledTimes(1)
  })

  it('does not re-apply the theme attributes itself', async () => {
    // The dispatcher (setLocalConfig's side effect) is the single authority for
    // the attributes. If this module also wrote them, a caller that dispatches
    // without persisting would silently look persisted.
    document.documentElement.setAttribute('data-theme', 'gruvbox-dark')
    installShareThemeSync()
    window.dispatchEvent(new CustomEvent('clawbench-theme-change', { detail: 'nord' }))
    await settle()

    expect(document.documentElement.getAttribute('data-theme')).toBe('gruvbox-dark')
  })

  it('ignores an empty or missing detail without breaking the id', async () => {
    installShareThemeSync()
    window.dispatchEvent(new CustomEvent('clawbench-theme-change', { detail: '' }))
    await settle()
    expect(shareThemeId.value).toBe('gruvbox-dark')

    window.dispatchEvent(new CustomEvent('clawbench-theme-change'))
    await settle()
    expect(shareThemeId.value).toBe('gruvbox-dark')
  })

  it('is idempotent — a second install must not double-render', async () => {
    installShareThemeSync()
    installShareThemeSync()
    window.dispatchEvent(new CustomEvent('clawbench-theme-change', { detail: 'nord' }))
    await settle()

    expect(initMermaidFn).toHaveBeenCalledTimes(1)
    expect(reRenderMermaidFn).toHaveBeenCalledTimes(1)
  })

  it('swallows a mermaid load failure (no diagram on the page)', async () => {
    initMermaidFn.mockRejectedValueOnce(new Error('chunk failed'))
    installShareThemeSync()
    window.dispatchEvent(new CustomEvent('clawbench-theme-change', { detail: 'nord' }))
    await settle()

    // The theme id still updated — only the optional re-render failed.
    expect(shareThemeId.value).toBe('nord')
  })
})
