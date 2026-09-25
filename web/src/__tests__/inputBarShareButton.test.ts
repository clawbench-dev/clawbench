import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the bottom input action bar must expose a "share conversation" button
 * that opens the existing SessionShareDialog.
 *
 * Sharing was previously reachable only from the left session list's context
 * menu — undiscoverable when you are looking at the conversation you want to
 * share. The button belongs in the input action bar (next to archive), NOT in
 * the chat title bar: the title bar is for session identity (rename), while the
 * action bar is where the other session actions (archive / destroy / refresh)
 * already live.
 *
 * The component files have their own mount tests, but this asserts the wiring
 * across the two-component boundary (bar emits → panel owns the dialog), which
 * no single mount test covers.
 */
describe('conversation share in the input action bar', () => {
  const BAR = 'src/components/chat/ChatInputBar.vue'
  const PANEL = 'src/components/chat/ChatPanelContent.vue'
  const APP = 'src/App.vue'

  /** Slice the `.chat-top-actions` block (the action bar) out of the template so
   *  assertions cannot match a button elsewhere in the file. Bounded by the
   *  recommendation banner that follows it. */
  function actionBarBlock(src: string): string {
    const open = src.indexOf('<div class="chat-top-actions"')
    expect(open, '.chat-top-actions must exist').toBeGreaterThan(-1)
    const close = src.indexOf('recommendation-chip', open)
    expect(close, 'action bar must be followed by the recommendation banner').toBeGreaterThan(open)
    return src.slice(open, close)
  }

  it('renders a share button inside the action bar', () => {
    const block = actionBarBlock(readWebFile(BAR))

    expect(block).toContain('@click="handleShare"')
    expect(block).toContain("t('chat.actions.shareSession')")
    expect(block).toContain('<MessageSquareShare')
  })

  /**
   * The icon must match the session list's "shared conversations" button, so the
   * two entry points to the same feature read as one. Asserting the literal tag
   * (not just "some icon") is what stops a later edit from drifting back to the
   * plain Share2 glyph.
   */
  it('uses the same icon as the session list share button', () => {
    const src = readWebFile(BAR)

    expect(src).toMatch(/import\s*\{[^}]*\bMessageSquareShare\b[^}]*\}\s*from\s*'lucide-vue-next'/)
    expect(src, 'Share2 is the wrong glyph for this button').not.toMatch(/\bShare2\b/)

    // …and the session list headers really do use that same icon, so this test
    // fails if THEY change rather than silently passing on a stale assumption.
    for (const host of ['src/components/session/SessionSidebar.vue', 'src/components/session/SessionDrawer.vue']) {
      expect(readWebFile(host), `${host} must render MessageSquareShare`).toContain('<MessageSquareShare')
    }
  })

  it('disables the share button when there is no session', () => {
    const block = actionBarBlock(readWebFile(BAR))

    // Matches the archive button's guard: without a session there is nothing to
    // share, and the handler would no-op anyway.
    expect(block).toContain(':class="{ disabled: !currentSessionId }"')
  })

  it('emits share-session only when a session exists', () => {
    const src = readWebFile(BAR)
    const start = src.indexOf('function handleShare()')
    expect(start, 'handleShare must exist').toBeGreaterThan(-1)
    const body = src.slice(start, src.indexOf('\n}', start))

    expect(body).toContain('if (!props.currentSessionId) return')
    expect(body).toContain("emit('share-session')")
    // Declared in defineEmits, or the parent listener never fires.
    expect(src).toMatch(/defineEmits\(\[[\s\S]*?'share-session'/)
  })

  it('is wired across the bar → panel boundary', () => {
    const panel = readWebFile(PANEL)

    // The panel must both listen for the emit and own the dialog; a listener
    // with no dialog would set state nothing renders.
    expect(panel).toContain('@share-session="handleOpenSessionShare"')
    expect(panel).toContain('<SessionShareDialog')
    expect(panel).toContain(':open="sessionShareOpen"')
    expect(panel).toContain(':session-id="sessionShareId"')
    expect(panel).toMatch(/import\s+SessionShareDialog\s+from\s+'@\/components\/session\/SessionShareDialog\.vue'/)
  })

  it('captures the session id when opening, and no-ops without a session', () => {
    const panel = readWebFile(PANEL)
    const start = panel.indexOf('function handleOpenSessionShare()')
    expect(start, 'handleOpenSessionShare must exist').toBeGreaterThan(-1)
    const body = panel.slice(start, panel.indexOf('\n}', start))

    expect(body).toContain('identity.currentSessionId.value')
    expect(body).toContain('if (!sid) return')
    expect(body).toContain('sessionShareId.value = sid')
    expect(body).toContain('sessionShareOpen.value = true')
  })

  it('is NOT in the chat title bar (title bar keeps only rename)', () => {
    const app = readWebFile(APP)
    const open = app.indexOf('<div class="chat-title-bar">')
    expect(open).toBeGreaterThan(-1)
    const close = app.indexOf('<TabPanel class="chat-tab-panel"', open)
    const titleBar = app.slice(open, close)

    expect(titleBar).toContain('data-action="rename-session"')
    expect(titleBar, 'share belongs in the action bar, not the header').not.toContain('share-session')
    // The old header action group must be gone entirely.
    expect(app).not.toContain('chat-title-actions')
    // And the dialog must not be mounted app-wide any more.
    expect(app).not.toContain('<SessionShareDialog')
  })

  it('has the wide-screen label in both locales', () => {
    // The label renders inside the action bar's short-label set; a missing key
    // would show the raw key string instead of a word.
    for (const locale of ['zh', 'en']) {
      const src = readWebFile(`src/i18n/locales/${locale}.ts`)
      const start = src.indexOf('wideLabels: {')
      expect(start, `wideLabels must exist in ${locale}`).toBeGreaterThan(-1)
      const block = src.slice(start, src.indexOf('},', start))
      expect(block, `wideLabels.share missing in ${locale}`).toMatch(/^\s*share:/m)
    }
  })
})
