import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the chat title bar must show a skeleton — not a placeholder string —
 * while a session switch is in flight.
 *
 * Before this, switching sessions cleared the identity and the agent line fell
 * back to the literal "AI 对话" / "AI Chat", which flashed for the duration of
 * the fetch. The bar now stands in with skeleton blocks and the fallback is
 * gone entirely.
 *
 * App.vue is the whole application and has no mount test, so this is asserted
 * at the source level (same approach as chatTitleRenameButton.test.ts).
 */
describe('chat title bar session-switch skeleton', () => {
  const APP = 'src/App.vue'

  /** Slice the `.chat-title-bar` block out of the template so assertions
   *  cannot match a skeleton elsewhere in App.vue. */
  function titleBarBlock(src: string): string {
    const open = src.indexOf('<div class="chat-title-bar">')
    expect(open, '.chat-title-bar must exist').toBeGreaterThan(-1)
    const close = src.indexOf('<TabPanel class="chat-tab-panel"', open)
    expect(close, '.chat-title-bar must be followed by the chat TabPanel').toBeGreaterThan(open)
    return src.slice(open, close)
  }

  it('renders skeleton blocks gated on the switching flag', () => {
    const block = titleBarBlock(readWebFile(APP))

    expect(block).toContain('v-if="switching"')
    expect(block).toContain('chat-title-skeleton-avatar')
    expect(block).toContain('chat-title-skeleton-title')
    // …and the real header content lives in the v-else branch, so the two never
    // render together (which would stack a skeleton under the real title).
    expect(block).toContain('<template v-else>')
  })

  it('imports the shared switching ref from useChatSession', () => {
    const src = readWebFile(APP)
    expect(src).toMatch(
      /import\s*\{[^}]*\bswitching\b[^}]*\}\s*from\s*'\.\/composables\/useChatSession\.ts'/,
    )
  })

  it('gates the agent title line on a known agent (no placeholder fallback)', () => {
    const block = titleBarBlock(readWebFile(APP))
    // The agent span is only rendered when currentAgentId is set. Previously it
    // rendered unconditionally and the composable supplied "AI 对话" when empty.
    expect(block).toContain('v-else-if="sessionIdentity.currentAgentId.value"')
  })

  it('removes the AI 对话 / AI Chat fallback from both locales', () => {
    for (const locale of ['zh', 'en']) {
      const src = readWebFile(`src/i18n/locales/${locale}.ts`)
      // The key must be gone — a stale key would be dead, and a re-added
      // fallback would resurrect the flash.
      expect(src, `${locale} must not define chat.session.aiDialog`).not.toContain('aiDialog')
    }
  })
})
