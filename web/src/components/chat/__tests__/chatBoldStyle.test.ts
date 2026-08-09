import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Android WebView chat bold must match mobile browser darkness/weight:
 * - Keep font-weight: bold + --text-bold (darker than body text)
 * - In app/WebView only, use tiny horizontal zero-blur text-shadow (hard stroke)
 * - Never reintroduce soft blur shadows (`0 0 Npx`) or weight-600 overrides
 *   that wash bold out lighter than Chrome.
 */
describe('chat bold style (Android WebView vs browser parity)', () => {
  const markdownCss = readFileSync(
    resolve(__dirname, '../../../../css/markdown-common.css'),
    'utf8',
  )
  const messageItem = readFileSync(
    resolve(__dirname, '../ChatMessageItem.vue'),
    'utf8',
  )

  it('uses bold weight + --text-bold for assistant/markdown strong', () => {
    expect(markdownCss).toMatch(
      /\.chat-message\.assistant strong,[\s\S]*?font-weight:\s*bold;[\s\S]*?color:\s*var\(--text-bold\);/,
    )
  })

  it('applies tiny horizontal zero-blur text-shadow only under data-app-mode', () => {
    expect(markdownCss).toContain('[data-app-mode] .chat-message.assistant strong')
    expect(markdownCss).toMatch(
      /\[data-app-mode\][\s\S]*?text-shadow:\s*[\s\S]*?0\.12px 0 0 currentColor[\s\S]*?-0\.12px 0 0 currentColor/,
    )
    expect(markdownCss).not.toMatch(/text-shadow:[\s\S]*?0 0\.\d+px 0 currentColor/)
    expect(markdownCss).not.toMatch(/text-shadow:[\s\S]*?0 -0\.\d+px 0 currentColor/)
    // Soft glow (blur radius) looks fuzzy on GPU-composited WebView layers
    expect(markdownCss).not.toMatch(/text-shadow:\s*0 0 1px currentColor/)
    expect(markdownCss).not.toMatch(/text-shadow:\s*0 0 0\.8px currentColor/)
  })

  it('does not override chat strong with lighter weight or text-shadow:none in ChatMessageItem', () => {
    // Guard against regressing to weight-600 / color-mix / text-shadow:none overrides
    expect(messageItem).not.toMatch(/\.chat-message strong[\s\S]{0,80}font-weight:\s*600/)
    expect(messageItem).not.toMatch(/:root\[data-app-mode\] \.chat-message strong[\s\S]{0,120}text-shadow:\s*none/)
    expect(messageItem).not.toMatch(/color-mix\(in srgb, var\(--text-primary\)/)
  })

  it('keeps the Android GPU ghost will-change rule on chat messages', () => {
    expect(messageItem).toContain(':root[data-app-mode] .chat-message')
    expect(messageItem).toContain('will-change: transform')
  })
})
