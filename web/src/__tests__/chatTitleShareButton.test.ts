import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the chat header must expose a "share session" icon that opens the
 * existing SessionShareDialog.
 *
 * Sharing was previously reachable only from the left session list's context
 * menu — undiscoverable when you are looking at the conversation you want to
 * share. This adds a visible button to the chat title bar.
 *
 * App.vue is the whole application and has no mount test, and the icon is a
 * component tag rather than a literal in the bundle, so this is asserted at the
 * source level (same approach as chatTitleRenameButton.test.ts).
 */
describe('chat title bar share button', () => {
  const APP = 'src/App.vue'

  /** Slice the `.chat-title-bar` block out of the template so the assertions
   *  cannot accidentally match a button elsewhere in App.vue. The title bar is
   *  immediately followed by the chat TabPanel, which bounds the slice. */
  function titleBarBlock(src: string): string {
    const open = src.indexOf('<div class="chat-title-bar">')
    expect(open, '.chat-title-bar must exist').toBeGreaterThan(-1)
    const close = src.indexOf('<TabPanel class="chat-tab-panel"', open)
    expect(close, '.chat-title-bar must be followed by the chat TabPanel').toBeGreaterThan(open)
    return src.slice(open, close)
  }

  it('renders a share button in the chat title bar', () => {
    const block = titleBarBlock(readWebFile(APP))

    expect(block).toContain('data-action="share-session"')
    expect(block).toContain('@click.stop="openSessionShareDialog"')
  })

  it('labels the button for accessibility', () => {
    const block = titleBarBlock(readWebFile(APP))

    expect(block).toContain('<Share2')
    // The icon alone is not a label — a title is required for screen readers
    // and desktop hover.
    expect(block).toContain(':title="t(\'sessionShare.button\')"')
    // …and the icon must actually be imported, or the template fails to render.
    expect(readWebFile(APP)).toMatch(/import\s*\{[^}]*\bShare2\b[^}]*\}\s*from\s*'lucide-vue-next'/)
  })

  it('only shows the button when a session is selected', () => {
    const block = titleBarBlock(readWebFile(APP))

    expect(block).toContain('v-if="sessionIdentity.currentSessionId.value"')
  })

  it('mounts the SessionShareDialog wired to the button state', () => {
    const src = readWebFile(APP)

    // Without the mount, the button would set state that nothing renders.
    expect(src).toContain('<SessionShareDialog')
    expect(src).toContain(':open="sessionShareOpen"')
    expect(src).toContain(':session-id="sessionShareId"')
    expect(src).toMatch(/import\s+SessionShareDialog\s+from\s+'\.\/components\/session\/SessionShareDialog\.vue'/)
  })

  it('captures the session id when opening, and no-ops without a session', () => {
    const src = readWebFile(APP)
    const start = src.indexOf('function openSessionShareDialog()')
    expect(start, 'openSessionShareDialog must exist').toBeGreaterThan(-1)
    const end = src.indexOf('\n}', start)
    const body = src.slice(start, end)

    // Guarding on the id keeps the dialog from opening onto an empty target.
    expect(body).toContain('sessionIdentity.currentSessionId.value')
    expect(body).toContain('if (!sid) return')
    expect(body).toContain('sessionShareId.value = sid')
    expect(body).toContain('sessionShareOpen.value = true')
  })

  it('registers the dialog as a swipe-to-close overlay', () => {
    const src = readWebFile(APP)

    // The share modal stacks above the drawers; without registering it here a
    // back-swipe on mobile closes something behind it instead of the dialog.
    expect(src).toContain('{ open: () => sessionShareOpen.value')
  })

  /**
   * Both header buttons share `.chat-title-edit-btn`, whose base rule no longer
   * carries `margin-left: auto` (that moved to the `.chat-title-actions` group).
   * Leaving it on the button would push only the FIRST button to the edge and
   * split the pair apart.
   */
  it('pins the action group, not the individual button, to the right edge', () => {
    const src = readWebFile(APP)

    const groupStart = src.indexOf('.chat-title-actions {')
    expect(groupStart, '.chat-title-actions rule must exist').toBeGreaterThan(-1)
    const groupRule = src.slice(groupStart, src.indexOf('}', groupStart))
    expect(groupRule).toContain('margin-left: auto')

    const btnStart = src.indexOf('.chat-title-edit-btn {')
    const btnRule = src.slice(btnStart, src.indexOf('}', btnStart))
    expect(btnRule, 'margin-left:auto on the button splits the group').not.toContain('margin-left: auto')
  })
})
