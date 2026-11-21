import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring guards for the two pieces that make a panel's "put this into the
 * chat" work without knowing App.vue:
 *
 *  - `v-chat-drag` must be registered, or every panel using it renders a plain
 *    element that never starts a drag (Vue does not error on an unknown
 *    directive in a production build — it silently ignores it).
 *  - the bridge's reveal handler must be injected by App.vue, or `quote()`
 *    stages the card but the user stays on a panel where they cannot see it.
 *
 * These are source-level on purpose: mounting App.vue is not viable, and the
 * failure mode of a missing registration is invisible rather than an exception.
 */
describe('chat bridge wiring', () => {
  it('registers v-chat-drag', () => {
    const main = readWebFile('src/main.ts')
    expect(main).toContain("app.directive('chat-drag', ChatDragDirective)")
  })

  it('lets App.vue reveal the chat pane for the bridge', () => {
    const app = readWebFile('src/App.vue')
    expect(app).toContain('setChatRevealHandler(() => switchTab(\'chat\'))')
  })

  it('has no bespoke KB quote hand-off left', () => {
    const app = readWebFile('src/App.vue')
    const kb = readWebFile('src/components/kb/KnowledgePanel.vue')
    expect(app).not.toContain('kbPendingQuote')
    expect(kb).not.toContain('requestStageKBQuote')
    expect(kb).toContain('chatBridge.quote(')
  })
})
