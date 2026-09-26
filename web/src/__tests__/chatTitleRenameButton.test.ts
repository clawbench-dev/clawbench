import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the chat header must expose an explicit "rename session" icon.
 *
 * The session title in the chat title bar was already clickable, but that is a
 * hidden affordance — especially on touch, where there is no hover tooltip to
 * reveal it. This adds a visible pencil button at the right end of the bar.
 *
 * App.vue is the whole application and has no mount test, and the icon is a
 * component tag rather than a literal in the bundle, so this is asserted at the
 * source level (same approach as dragDropHighlightScope.test.ts).
 */
describe('chat title bar rename button', () => {
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

  it('renders a rename button in the chat title bar', () => {
    const block = titleBarBlock(readWebFile(APP))

    expect(block).toContain('data-action="rename-session"')
    expect(block).toContain('class="chat-title-edit-btn"')
  })

  it('wires the button to the existing rename handler', () => {
    const block = titleBarBlock(readWebFile(APP))

    expect(block).toContain('@click.stop="handleRenameSession"')
  })

  it('uses the pencil icon and labels it for accessibility', () => {
    const src = readWebFile(APP)
    const block = titleBarBlock(src)

    expect(block).toContain('<PencilLine')
    // The icon alone is not a label — a title is required for screen readers
    // and desktop hover. Reuses the same tooltip key as the clickable title.
    expect(block).toContain(':title="t(\'chat.sessionRename.tooltip\')"')
    // …and the icon must actually be imported, or the template fails to render.
    expect(src).toMatch(/import\s*\{[^}]*\bPencilLine\b[^}]*\}\s*from\s*'lucide-vue-next'/)
  })

  it('only shows the button when a session is selected', () => {
    const block = titleBarBlock(readWebFile(APP))

    expect(block).toContain('v-if="sessionIdentity.currentSessionId.value"')
  })

  /**
   * The button is intentionally low-key: the clickable title text next to it is
   * the primary affordance, so the icon must NOT compete for attention at rest.
   * It takes the accent colour only on hover/focus.
   */
  describe('visual restraint', () => {
    /** The `.chat-title-edit-btn` base rule (up to its first closing brace). */
    function baseRule(src: string): string {
      const start = src.indexOf('.chat-title-edit-btn {')
      expect(start, '.chat-title-edit-btn base rule must exist').toBeGreaterThan(-1)
      const end = src.indexOf('}', start)
      expect(end).toBeGreaterThan(start)
      return src.slice(start, end)
    }

    it('is muted at rest rather than accent-coloured', () => {
      const rule = baseRule(readWebFile(APP))

      expect(rule).toContain('color: var(--text-muted')
      expect(rule, 'rest state must not use the accent colour').not.toContain('color: var(--accent-color')
    })

    it('takes the accent colour on hover', () => {
      const src = readWebFile(APP)
      const hover = src.indexOf('.chat-title-edit-btn:hover')

      expect(hover, 'hover rule must exist').toBeGreaterThan(-1)
      const end = src.indexOf('}', hover)
      expect(src.slice(hover, end)).toContain('color: var(--accent-color')
    })

    // Without this the button stays permanently low-contrast for keyboard users,
    // who never trigger :hover.
    it('reveals itself on keyboard focus too', () => {
      const src = readWebFile(APP)
      const focus = src.indexOf('.chat-title-edit-btn:focus-visible')

      expect(focus, 'focus-visible rule must exist').toBeGreaterThan(-1)
      const end = src.indexOf('}', focus)
      const rule = src.slice(focus, end)
      expect(rule).toContain('color: var(--accent-color')
      expect(rule).toContain('outline')
    })
  })
})
