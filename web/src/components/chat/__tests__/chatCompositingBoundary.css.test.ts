import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Android WebView's compositor mis-draws a composited layer whose size changes:
 * it stretches the stale texture into the new delta instead of repainting it.
 * Every ghost this project chased (phantom metadata text, the dock disc in the
 * gap above the composer, the 9px `--bg-tertiary` band and the 4px squashed
 * meta bar below an action bar) was that one failure, each time re-introduced
 * by a per-element `will-change` / `backdrop-filter` "fix".
 *
 * The durable rule is structural:
 *
 *   1. Exactly ONE compositing boundary in the chat tree, on a
 *      stable-size container: `.chat-panel-content`.
 *   2. ZERO layer promotion inside it — no `will-change`, no
 *      `backdrop-filter`, on anything whose height changes at turn end
 *      (message rows, bubbles, meta bars, tool cards, load hints).
 *
 * If a future bug tempts someone to "just add will-change" to a row/meta bar,
 * this test fails and points at the panel boundary instead. Adding a layer
 * there is what caused the ghost in the first place.
 *
 * jsdom has no CSS engine, so this is a source-contract check (same pattern as
 * chatBoldStyle.test.ts / chatRowLayerIsolation.css.test.ts).
 */
describe('chat compositing boundary (Android WebView GPU ghost)', () => {
  const chatDir = resolve(__dirname, '..')
  const panelSource = readFileSync(resolve(chatDir, 'ChatPanelContent.vue'), 'utf8')
  const itemSource = readFileSync(resolve(chatDir, 'ChatMessageItem.vue'), 'utf8')

  /** Drop CSS comments so prose can never satisfy (or trip) a scan. */
  function stripComments(source: string): string {
    return source.replace(/\/\*[\s\S]*?\*\//g, '')
  }

  function decls(selectorPattern: RegExp, source: string): string {
    const m = source.match(selectorPattern)
    expect(m, `${selectorPattern} rule must exist`).not.toBeNull()
    return stripComments(m![1])
  }

  it('establishes the single boundary on the stable-size panel', () => {
    const rule = decls(/:root\[data-app-mode\]\s+\.chat-panel-content\s*\{([^}]*)\}/, panelSource)
    // The panel's height is the pane's height: it does NOT change when a reply
    // finishes, so its layer is never resized on the ghost-triggering path.
    expect(rule).toMatch(/will-change:\s*transform/)
    expect(rule).toMatch(/contain:\s*paint/)
  })

  it('keeps the panel boundary scoped to native hosts', () => {
    // A permanent layer on desktop costs subpixel text antialiasing and there
    // is no compositor bug to work around.
    const bare = panelSource.match(/^\.chat-panel-content\s*\{([^}]*)\}/m)
    expect(bare, 'the base .chat-panel-content rule must exist').not.toBeNull()
    // Strip comments first, like `decls()` above: the rule's own doc block
    // mentions `contain: paint` when explaining the app-mode boundary that
    // follows it, and prose must neither satisfy nor trip a declaration scan.
    const declsOnly = stripComments(bare![1])
    expect(declsOnly).not.toMatch(/will-change/)
    expect(declsOnly).not.toMatch(/contain:\s*paint/)
  })

  /**
   * No layer promotion inside the boundary, with exactly one documented
   * exception: the row's table-copy-menu escape hatch. That rule runs only
   * while a fixed-position menu is open, when the row does NOT change height
   * (the menu is out of flow), and it is required so the menu keeps its
   * containing block after `contain: paint` is lifted.
   */
  it('has no will-change inside the boundary except the table-menu escape hatch', () => {
    const offenders: string[] = []
    for (const file of readdirSync(chatDir).filter((f) => f.endsWith('.vue'))) {
      // Comments are documentation; only real declarations count.
      const src = stripComments(readFileSync(resolve(chatDir, file), 'utf8'))
      const count = (src.match(/will-change/g) ?? []).length
      if (count === 0) continue

      if (file === 'ChatPanelContent.vue') {
        // The single boundary itself: exactly one declaration, in the
        // app-mode panel rule asserted above.
        const panelDecl = (
          src.match(/:root\[data-app-mode\]\s+\.chat-panel-content\s*\{([^}]*)\}/)?.[1] ?? ''
        ).match(/will-change/g)?.length ?? 0
        if (count !== panelDecl) offenders.push(`${file} (unexpected will-change)`)
      } else if (file === 'ChatMessageItem.vue') {
        // Only the :has(.is-open) escape rule may carry it.
        const escapeDecl =
          src
            .match(/:root\[data-app-mode\]\s+\.chat-message:has\(\.table-block-copy-menu\.is-open\)\s*\{([^}]*)\}/)?.[1]
            ?.match(/will-change/g)?.length ?? 0
        if (count !== escapeDecl) offenders.push(`${file} (unexpected will-change)`)
      } else {
        offenders.push(file)
      }
    }
    expect(
      offenders,
      'will-change promotes the element to a GPU layer. A promoted element that ' +
      'changes height at turn end is exactly what this WebView mis-draws as a ' +
      'stale strip. Keep the chat tree uncomposited; the single boundary is ' +
      '.chat-panel-content in ChatPanelContent.vue.',
    ).toEqual([])
  })

  /**
   * `backdrop-filter` also forces a layer. The "all messages loaded" pill
   * appears/disappears ~2s after opening a session and was the original
   * trigger of the message-to-message phantom text. It now uses a near-opaque
   * background instead.
   */
  it('has no backdrop-filter inside the boundary', () => {
    const offenders: string[] = []
    for (const file of readdirSync(chatDir).filter((f) => f.endsWith('.vue'))) {
      const src = stripComments(readFileSync(resolve(chatDir, file), 'utf8'))
      if (/backdrop-filter/.test(src)) offenders.push(file)
    }
    expect(
      offenders,
      'backdrop-filter forces its element onto a compositing layer; inside the ' +
      'chat panel that reintroduces the resize-ghost. Use a near-opaque ' +
      'background instead.',
    ).toEqual([])
  })

  it('keeps the row itself uncomposited but clipped', () => {
    // Belt-and-braces against a regression of the exact rule that produced the
    // 4px squashed meta bar: the row must never be promoted, only clipped.
    const rule = decls(/:root\[data-app-mode\]\s+\.chat-message\s*\{([^}]*)\}/, itemSource)
    expect(rule).toMatch(/contain:\s*paint/)
    expect(rule).not.toMatch(/will-change/)
    expect(rule).not.toMatch(/transform\s*:/)
  })
})
