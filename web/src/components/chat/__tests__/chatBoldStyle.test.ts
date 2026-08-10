import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Android WebView chat bold must match mobile browser darkness/weight:
 * - Keep font-weight: bold + --text-bold (darker than body text)
 * - In app/WebView only, use a thin uniform -webkit-text-stroke (hard outline)
 * - Never reintroduce blurred text-shadow (0 0 Npx) or x-only offsets, which
 *   look fuzzy / leave horizontal strokes unchanged respectively.
 */
describe('chat bold style (Android WebView vs browser parity)', () => {
  const markdownCss = readFileSync(
    resolve(__dirname, '../../../../css/markdown-common.css'),
    'utf8',
  )

  it('uses bold weight + --text-bold for assistant/markdown strong', () => {
    expect(markdownCss).toMatch(
      /\.chat-message\.assistant strong,[\s\S]*?font-weight:\s*bold;[\s\S]*?color:\s*var\(--text-bold\);/,
    )
  })

  it('applies a thin uniform -webkit-text-stroke only under data-app-mode', () => {
    expect(markdownCss).toContain('[data-app-mode] .chat-message.assistant strong')
    expect(markdownCss).toMatch(
      /\[data-app-mode\][\s\S]*?\.chat-message\.assistant strong,[\s\S]*?-webkit-text-stroke:\s*0\.12px currentColor;/,
    )
    // User-message bold uses a slightly lighter stroke.
    expect(markdownCss).toMatch(
      /\[data-app-mode\] \.chat-message\.user strong,[\s\S]*?-webkit-text-stroke:\s*0\.1px currentColor;/,
    )
  })

  it('does not use blurred or x-only text-shadow compensation', () => {
    // Soft glow (blur radius) looks fuzzy on GPU-composited WebView layers.
    expect(markdownCss).not.toMatch(/text-shadow:\s*0 0 (1|0\.8)px currentColor/)
    // x-only offsets would leave horizontal strokes unchanged (anisotropic).
    expect(markdownCss).not.toMatch(/text-shadow:\s*0?\.\d+px 0 0 currentColor/)
    expect(markdownCss).not.toMatch(/text-shadow:\s*-?0\.\d+px 0 0 currentColor/)
  })
})
