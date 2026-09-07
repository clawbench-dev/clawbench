import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Guard tests for the prefers-reduced-motion handling of the flash family.
 *
 * The flash animations (line-flash / search-match-flash / msg-highlight-flash)
 * previously ran unconditionally; reduced-motion users got the full blink
 * anyway. Each stylesheet that defines a flash animation must now kill the
 * animation under `prefers-reduced-motion` while keeping the static color
 * feedback (per the project's "keep functional feedback, drop decorative
 * motion" convention).
 *
 * These are source-sniffing tests (jsdom has no CSS engine / matchMedia), the
 * same pattern ChatMessageList.test.ts uses for its CSS assertions.
 */

async function rawCss(path: string): Promise<string> {
  const mod = await import(`${path}?raw`)
  return String(mod.default)
}

// css/share-chrome.css lives outside the vitest source root (web/src), so it
// cannot be ?raw-imported from a test file — read it off disk instead. The
// working directory differs between a bare `vitest` run (web/) and the
// scripts/vitest-run.sh wrapper (repo root), so probe both.
function readShareChromeCss(): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, 'css/share-chrome.css'), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error('css/share-chrome.css not found from cwd: ' + process.cwd())
}
const shareChromeCss = readShareChromeCss()

describe('flash animations respect prefers-reduced-motion', () => {
  it('code-viewer.css kills line/copy/char flash animation under reduced motion', async () => {
    const css = await rawCss('@/assets/code-viewer.css')
    expect(css).toContain('@media (prefers-reduced-motion: reduce)')
    expect(css).toMatch(/\.line-flash,\s*\.copy-flash\s*{[\s\S]*?animation: none !important/)
    // The static tint replaces the blink so the jump feedback survives.
    expect(css).toContain('background: color-mix(in srgb, var(--accent-color, #0066cc) 30%, transparent)')
    expect(css).toMatch(/\.char-flash-delete\s*{[\s\S]*?animation: none !important/)
    expect(css).toMatch(/\.char-flash-add\s*{[\s\S]*?animation: none !important/)
  })

  it('code-viewer.css keeps the canonical flash duration in --flash-duration', async () => {
    const css = await rawCss('@/assets/code-viewer.css')
    expect(css).toContain('--flash-duration: 0.7s')
    expect(css).toContain('animation: line-flash var(--flash-duration, 0.7s) ease-out forwards')
  })

  it('search-bar.css kills the search-match flash animation under reduced motion', async () => {
    const css = await rawCss('@/assets/search-bar.css')
    expect(css).toContain('@media (prefers-reduced-motion: reduce)')
    expect(css).toMatch(/\.cm-searchMatch-flash,\s*\.search-match-flash\s*{[\s\S]*?animation: none !important/)
    expect(css).toContain('animation: line-flash var(--flash-duration, 0.7s) ease-out 1')
  })

  it('share-chrome.css (share SPA + HTML export) mirrors the reduced-motion handling', async () => {
    expect(shareChromeCss).toContain('@media (prefers-reduced-motion: reduce)')
    expect(shareChromeCss).toMatch(/\.line-flash\s*{[\s\S]*?animation: none !important/)
    expect(shareChromeCss).toContain('@keyframes line-flash')
    expect(shareChromeCss).toContain('--flash-duration: 0.7s')
  })

  it('ChatMessageList.vue scoped message-highlight flash honors reduced motion per bubble role', async () => {
    const mod = await import('@/components/chat/ChatMessageList.vue?raw')
    const source = String(mod.default)
    expect(source).toContain('@media (prefers-reduced-motion: reduce)')
    expect(source).toContain('animation: msg-highlight-flash var(--flash-duration, 0.7s) ease-out 1')
    // Reduced-motion static tints keep each bubble role's own base background.
    expect(source).toContain('background-color: color-mix(in srgb, var(--accent-color) 65%, var(--user-msg-color))')
    expect(source).toContain('background-color: color-mix(in srgb, var(--accent-color) 35%, var(--bg-tertiary))')
  })
})
