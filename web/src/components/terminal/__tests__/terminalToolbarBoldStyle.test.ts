import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Android WebView terminal virtual keys/symbols must match mobile browser
 * weight, using the SAME mechanism as chat bold (markdown-common.css):
 * - Keys are font-weight: 700 (shortcuts 800), symbols font-weight: 700
 * - In app/WebView only, add a thin uniform -webkit-text-stroke (hard outline)
 * - Never reintroduce blurred text-shadow (0 0 Npx) or x-only offsets, which
 *   look fuzzy / leave horizontal strokes unchanged respectively.
 */
describe('terminal toolbar bold style (Android WebView vs browser parity)', () => {
  const vue = readFileSync(
    resolve(__dirname, '../TerminalPanelContent.vue'),
    'utf8',
  )

  it('uses bold weight for virtual keys and symbols', () => {
    expect(vue).toMatch(/\.toolbar-btn\s*\{[\s\S]*?font-weight:\s*700;/)
    expect(vue).toMatch(/\.toolbar-btn\.btn-symbol\s*\{[\s\S]*?font-weight:\s*700;/)
    expect(vue).toMatch(/\.toolbar-btn\.shortcut\s*\{[\s\S]*?font-weight:\s*800;/)
  })

  it('applies a thin uniform -webkit-text-stroke only under data-app-mode', () => {
    expect(vue).toMatch(
      /\[data-app-mode\] \.toolbar-btn\s*\{[\s\S]*?-webkit-text-stroke:\s*0\.12px currentColor;/,
    )
    // Shortcut keys use a slightly lighter stroke (smaller font).
    expect(vue).toMatch(
      /\[data-app-mode\] \.toolbar-btn\.shortcut\s*\{[\s\S]*?-webkit-text-stroke:\s*0\.1px currentColor;/,
    )
  })

  it('does not use blurred or x-only text-shadow compensation', () => {
    // Soft glow (blur radius) looks fuzzy on GPU-composited WebView layers.
    expect(vue).not.toMatch(/text-shadow:\s*0 0 (1|0\.8)px currentColor/)
    // x-only offsets would leave horizontal strokes unchanged (anisotropic).
    expect(vue).not.toMatch(/text-shadow:\s*0?\.\d+px 0 0 currentColor/)
    expect(vue).not.toMatch(/text-shadow:\s*-?0\.\d+px 0 0 currentColor/)
  })
})
